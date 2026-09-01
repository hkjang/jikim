package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/cryptox"
	"github.com/hkjang/jikim/internal/ids"
	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/password"
	"github.com/hkjang/jikim/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound       = errors.New("대상을 찾을 수 없습니다")
	ErrConflict       = errors.New("이미 존재하는 값입니다")
	ErrUnauthorized   = errors.New("인증 정보가 올바르지 않습니다")
	ErrForbidden      = errors.New("권한이 없습니다")
	ErrInvalid        = errors.New("요청 값이 올바르지 않습니다")
	ErrRequesterMatch = errors.New("요청자와 승인자는 달라야 합니다")
)

type Store struct {
	pool   *pgxpool.Pool
	master *cryptox.Cipher
}

func New(ctx context.Context, dsn string, master *cryptox.Cipher) (*Store, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("PostgreSQL DSN: %w", err)
	}
	config.MaxConns = 20
	config.MinConns = 1
	config.MaxConnIdleTime = 5 * time.Minute
	config.MaxConnLifetime = time.Hour
	config.HealthCheckPeriod = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("PostgreSQL 연결: %w", err)
	}
	return &Store{pool: pool, master: master}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext('jikim_schema_migrations'))`); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('jikim_schema_migrations'))`)
	}()
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version text PRIMARY KEY,
        applied_at timestamptz NOT NULL DEFAULT now()
    )`); err != nil {
		return err
	}
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var applied bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		sql, err := migrations.Files.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(sql)); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, name)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("migration %s commit: %w", name, err)
		}
	}
	return nil
}

func (s *Store) BootstrapAdmin(ctx context.Context, username, rawPassword string) (model.User, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return model.User{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	user, err := scanUser(tx.QueryRow(ctx, userSelect+` WHERE lower(username)=lower($1) FOR UPDATE`, username))
	created := false
	if errors.Is(err, pgx.ErrNoRows) {
		hash, hashErr := password.Hash(rawPassword)
		if hashErr != nil {
			return model.User{}, false, hashErr
		}
		id, idErr := ids.UUID()
		if idErr != nil {
			return model.User{}, false, idErr
		}
		user, err = scanUser(tx.QueryRow(ctx, `INSERT INTO users
            (id, username, display_name, password_hash, role, active)
            VALUES($1,$2,$2,$3,'admin',true)
            RETURNING id, username, display_name, email, role, active, personal_key_version, created_at, updated_at`, id, username, hash))
		created = true
	} else if err == nil && (user.Role != "admin" || !user.Active) {
		return model.User{}, false, errors.New("BOOTSTRAP_ADMIN과 같은 기존 계정이 관리자 활성 상태가 아닙니다; 자동 승격하지 않습니다")
	}
	if err != nil {
		return model.User{}, false, mapError(err)
	}
	if err := s.ensureUserKeyTx(ctx, tx, user.ID, user.PersonalKeyVersion); err != nil {
		return model.User{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.User{}, false, err
	}
	return user, created, nil
}

const userSelect = `SELECT id, username, display_name, email, role, active,
    personal_key_version, created_at, updated_at FROM users`

type rowScanner interface{ Scan(...any) error }

func scanUser(row rowScanner) (model.User, error) {
	var user model.User
	err := row.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email, &user.Role,
		&user.Active, &user.PersonalKeyVersion, &user.CreatedAt, &user.UpdatedAt)
	return user, err
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %s", ErrConflict, pgErr.ConstraintName)
		case "23503", "23514", "22P02":
			return fmt.Errorf("%w: %s", ErrInvalid, pgErr.Message)
		}
	}
	return err
}

func marshalJSON(value any) ([]byte, error) {
	if value == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(value)
}
