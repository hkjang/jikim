package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/cryptox"
	"github.com/hkjang/jikim/internal/ids"
	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/password"
	"github.com/jackc/pgx/v5"
)

func (s *Store) Authenticate(ctx context.Context, username, rawPassword string) (model.User, error) {
	var user model.User
	var hash *string
	err := s.pool.QueryRow(ctx, `SELECT id, username, display_name, email, role, active, auth_source,
		personal_key_version, (SELECT max(s.created_at) FROM sessions s WHERE s.user_id=users.id AND s.kind='session'),
		created_at, updated_at, password_hash
		FROM users WHERE lower(username)=lower($1)`, strings.TrimSpace(username)).Scan(
		&user.ID, &user.Username, &user.DisplayName, &user.Email, &user.Role,
		&user.Active, &user.AuthSource, &user.PersonalKeyVersion, &user.LastLoginAt,
		&user.CreatedAt, &user.UpdatedAt, &hash)
	encoded := ""
	if hash != nil {
		encoded = *hash
	}
	// Always run one full PBKDF2 verification, including absent users and NULL/malformed hashes.
	verified := password.Verify(encoded, rawPassword)
	if err != nil || hash == nil || !user.Active || !verified {
		return model.User{}, ErrUnauthorized
	}
	return user, nil
}

func (s *Store) CreateSession(ctx context.Context, userID, kind, name, createdBy string, ttl time.Duration) (string, model.Session, error) {
	prefix := "jks."
	if kind == "openbao" || kind == "api" {
		prefix = "hvs."
	}
	plain, hash, err := ids.Token(prefix)
	if err != nil {
		return "", model.Session{}, err
	}
	id, err := ids.UUID()
	if err != nil {
		return "", model.Session{}, err
	}
	if ttl <= 0 || ttl > 30*24*time.Hour {
		ttl = 12 * time.Hour
	}
	expires := time.Now().UTC().Add(ttl)
	var creator *string
	if createdBy != "" {
		creator = &createdBy
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO sessions
        (token_hash,id,user_id,kind,name,created_by,expires_at)
        VALUES($1,$2,$3,$4,$5,$6,$7)`, hash, id, userID, kind, name, creator, expires)
	if err != nil {
		return "", model.Session{}, mapError(err)
	}
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return "", model.Session{}, err
	}
	return plain, model.Session{ID: id, User: user, Kind: kind, Name: name, CreatedAt: time.Now().UTC(), ExpiresAt: expires}, nil
}

func (s *Store) SessionByToken(ctx context.Context, plain string) (model.Session, error) {
	hash := ids.HashToken(plain)
	var session model.Session
	err := s.pool.QueryRow(ctx, `SELECT s.id, s.kind, s.name, s.created_at, s.expires_at,
		u.id, u.username, u.display_name, u.email, u.role, u.active, u.auth_source,
		u.personal_key_version,
		(SELECT max(login.created_at) FROM sessions login WHERE login.user_id=u.id AND login.kind='session'),
		u.created_at, u.updated_at
		FROM sessions s JOIN users u ON u.id=s.user_id
        WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at > now() AND u.active=true`, hash).Scan(
		&session.ID, &session.Kind, &session.Name, &session.CreatedAt, &session.ExpiresAt,
		&session.User.ID, &session.User.Username, &session.User.DisplayName, &session.User.Email,
		&session.User.Role, &session.User.Active, &session.User.AuthSource,
		&session.User.PersonalKeyVersion, &session.User.LastLoginAt,
		&session.User.CreatedAt, &session.User.UpdatedAt)
	if err != nil {
		return model.Session{}, ErrUnauthorized
	}
	_, _ = s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at=now() WHERE token_hash=$1`, hash)
	return session, nil
}

func (s *Store) RevokeToken(ctx context.Context, plain string) error {
	command, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, ids.HashToken(plain))
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetUser(ctx context.Context, id string) (model.User, error) {
	user, err := scanUser(s.pool.QueryRow(ctx, userSelect+` WHERE id=$1`, id))
	return user, mapError(err)
}

func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]model.User, error) {
	rows, err := s.pool.Query(ctx, userSelect+` ORDER BY username LIMIT $1 OFFSET $2`, boundedLimit(limit), max(offset, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]model.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

type UserInput struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	Role        string `json:"role"`
	Active      *bool  `json:"active,omitempty"`
}

func validRole(role string) bool {
	switch role {
	case "admin", "manager", "user", "auditor":
		return true
	default:
		return false
	}
}

func (s *Store) CreateUser(ctx context.Context, input UserInput) (model.User, error) {
	input.Username = strings.TrimSpace(input.Username)
	if len(input.Username) < 3 || !validRole(input.Role) {
		return model.User{}, ErrInvalid
	}
	hash, err := password.Hash(input.Password)
	if err != nil {
		return model.User{}, err
	}
	id, err := ids.UUID()
	if err != nil {
		return model.User{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	user, err := scanUser(tx.QueryRow(ctx, `INSERT INTO users
	        (id,username,display_name,email,password_hash,role,active)
	        VALUES($1,$2,$3,$4,$5,$6,true)
			RETURNING id,username,display_name,email,role,active,auth_source,personal_key_version,
			NULL::timestamptz,created_at,updated_at`,
		id, input.Username, strings.TrimSpace(input.DisplayName), strings.TrimSpace(input.Email), hash, input.Role))
	if err != nil {
		return model.User{}, mapError(err)
	}
	if err := s.ensureUserKeyTx(ctx, tx, user.ID, 1); err != nil {
		return model.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.User{}, err
	}
	return user, nil
}

func (s *Store) UpdateUser(ctx context.Context, id string, input UserInput) (model.User, error) {
	if input.Role != "" && !validRole(input.Role) {
		return model.User{}, ErrInvalid
	}
	current, err := s.GetUser(ctx, id)
	if err != nil {
		return model.User{}, err
	}
	if strings.TrimSpace(input.DisplayName) != "" {
		current.DisplayName = strings.TrimSpace(input.DisplayName)
	}
	if input.Email != "" {
		current.Email = strings.TrimSpace(input.Email)
	}
	if input.Role != "" {
		current.Role = input.Role
	}
	if input.Active != nil {
		current.Active = *input.Active
	}
	user, err := scanUser(s.pool.QueryRow(ctx, `UPDATE users SET display_name=$2,email=$3,role=$4,active=$5,updated_at=now()
			WHERE id=$1 RETURNING id,username,display_name,email,role,active,auth_source,personal_key_version,
			(SELECT max(s.created_at) FROM sessions s WHERE s.user_id=users.id AND s.kind='session'),created_at,updated_at`,
		id, current.DisplayName, current.Email, current.Role, current.Active))
	return user, mapError(err)
}

func (s *Store) UpdateUserAsAdmin(ctx context.Context, id, actorID string, input UserInput) (model.User, error) {
	if err := validateAdminActor(id, actorID); err != nil {
		return model.User{}, err
	}
	if input.Role != "" && !validRole(input.Role) {
		return model.User{}, ErrInvalid
	}
	var passwordHash *string
	if input.Password != "" {
		hash, err := password.Hash(input.Password)
		if err != nil {
			return model.User{}, err
		}
		passwordHash = &hash
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return model.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, queryErr := tx.Query(ctx, `SELECT id FROM users WHERE role='admin' AND active=true ORDER BY id FOR UPDATE`)
	if queryErr != nil {
		return model.User{}, queryErr
	}
	activeAdmins := 0
	for rows.Next() {
		activeAdmins++
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		rows.Close()
		return model.User{}, rowsErr
	}
	rows.Close()
	current, err := scanUser(tx.QueryRow(ctx, userSelect+` WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return model.User{}, mapError(err)
	}
	if strings.TrimSpace(input.DisplayName) != "" {
		current.DisplayName = strings.TrimSpace(input.DisplayName)
	}
	if input.Email != "" {
		current.Email = strings.TrimSpace(input.Email)
	}
	newRole := current.Role
	if input.Role != "" {
		newRole = input.Role
	}
	newActive := current.Active
	if input.Active != nil {
		newActive = *input.Active
	}
	if err := validateActiveAdminInvariant(current, newRole, newActive, activeAdmins); err != nil {
		return model.User{}, err
	}
	current.Role = newRole
	current.Active = newActive
	user, err := scanUser(tx.QueryRow(ctx, `UPDATE users SET display_name=$2,email=$3,role=$4,active=$5,
		password_hash=COALESCE($6,password_hash),auth_source=CASE WHEN $6::text IS NULL THEN auth_source ELSE 'local' END,
		external_issuer=CASE WHEN $6::text IS NULL THEN external_issuer ELSE NULL END,
		external_subject=CASE WHEN $6::text IS NULL THEN external_subject ELSE NULL END,updated_at=now()
		WHERE id=$1 RETURNING id,username,display_name,email,role,active,auth_source,personal_key_version,
		(SELECT max(s.created_at) FROM sessions s WHERE s.user_id=users.id AND s.kind='session'),created_at,updated_at`,
		id, current.DisplayName, current.Email, current.Role, current.Active, passwordHash))
	if err != nil {
		return model.User{}, mapError(err)
	}
	if passwordHash != nil {
		if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id); err != nil {
			return model.User{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.User{}, err
	}
	return user, nil
}

func validateAdminActor(targetID, actorID string) error {
	if targetID == actorID {
		return fmt.Errorf("%w: 자신의 계정은 개인화 페이지에서만 변경할 수 있습니다", ErrInvalid)
	}
	return nil
}

func validateActiveAdminInvariant(current model.User, newRole string, newActive bool, activeAdmins int) error {
	if current.Role == "admin" && current.Active && (newRole != "admin" || !newActive) && activeAdmins <= 1 {
		return fmt.Errorf("%w: 최소 한 명의 활성 관리자가 필요합니다", ErrInvalid)
	}
	return nil
}

func (s *Store) UpdateProfile(ctx context.Context, id, displayName, email string) (model.User, error) {
	user, err := scanUser(s.pool.QueryRow(ctx, `UPDATE users SET display_name=$2,email=$3,updated_at=now()
			WHERE id=$1 RETURNING id,username,display_name,email,role,active,auth_source,personal_key_version,
			(SELECT max(s.created_at) FROM sessions s WHERE s.user_id=users.id AND s.kind='session'),created_at,updated_at`,
		id, strings.TrimSpace(displayName), strings.TrimSpace(email)))
	return user, mapError(err)
}

func (s *Store) ChangePassword(ctx context.Context, id, currentPassword, newPassword string, adminReset bool) error {
	hash, err := password.Hash(newPassword)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var oldHash *string
	if err := tx.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&oldHash); err != nil {
		return mapError(err)
	}
	if !adminReset && (oldHash == nil || !password.Verify(*oldHash, currentPassword)) {
		return ErrUnauthorized
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET password_hash=$2,auth_source='local',updated_at=now() WHERE id=$1`, id, hash); err != nil {
		return mapError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteUser(ctx context.Context, id, actorID string) error {
	if id == actorID {
		return fmt.Errorf("%w: 자신의 계정은 비활성화할 수 없습니다", ErrInvalid)
	}
	command, err := s.pool.Exec(ctx, `UPDATE users SET active=false,updated_at=now() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, _ = s.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id)
	return nil
}

func (s *Store) UpsertOIDCUser(ctx context.Context, issuer, subject, username, email, displayName string) (model.User, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" || subject == "" || username == "" {
		return model.User{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	user, err := scanUser(tx.QueryRow(ctx, userSelect+` WHERE auth_source='oidc' AND external_issuer=$1 AND external_subject=$2 FOR UPDATE`, issuer, subject))
	if errors.Is(err, pgx.ErrNoRows) {
		id, idErr := ids.UUID()
		if idErr != nil {
			return model.User{}, idErr
		}
		user, err = scanUser(tx.QueryRow(ctx, `INSERT INTO users
			(id,username,display_name,email,role,active,auth_source,external_issuer,external_subject)
			VALUES($1,$2,$3,$4,'user',true,'oidc',$5,$6)
			RETURNING id,username,display_name,email,role,active,auth_source,personal_key_version,
			NULL::timestamptz,created_at,updated_at`,
			id, username, displayName, email, issuer, subject))
		if err == nil {
			err = s.ensureUserKeyTx(ctx, tx, user.ID, 1)
		}
	} else if err == nil {
		user, err = scanUser(tx.QueryRow(ctx, `UPDATE users SET email=$2,display_name=$3,updated_at=now()
			WHERE id=$1 RETURNING id,username,display_name,email,role,active,auth_source,personal_key_version,
			(SELECT max(s.created_at) FROM sessions s WHERE s.user_id=users.id AND s.kind='session'),created_at,updated_at`,
			user.ID, email, displayName))
	}
	if err != nil {
		return model.User{}, mapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.User{}, err
	}
	return user, nil
}

func (s *Store) ensureUserKeyTx(ctx context.Context, tx pgx.Tx, userID string, version int) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_keys WHERE user_id=$1 AND version=$2)`, userID, version).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	key, err := cryptox.RandomKey()
	if err != nil {
		return err
	}
	aad := []byte(fmt.Sprintf("user-key:%s:%d", userID, version))
	ciphertext, nonce, err := s.master.Encrypt(key, aad)
	if err != nil {
		return err
	}
	id, err := ids.UUID()
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_keys(id,user_id,version,encrypted_key,nonce) VALUES($1,$2,$3,$4,$5)`,
		id, userID, version, ciphertext, nonce)
	return mapError(err)
}

func (s *Store) ListUserKeys(ctx context.Context, userID string, all bool) ([]model.UserKey, error) {
	query := `SELECT id,user_id,version,permissions,active,created_at FROM user_keys`
	args := []any{}
	if !all {
		query += ` WHERE user_id=$1`
		args = append(args, userID)
	}
	query += ` ORDER BY user_id,version DESC`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]model.UserKey, 0)
	for rows.Next() {
		var key model.UserKey
		var permissions []byte
		if err := rows.Scan(&key.ID, &key.UserID, &key.Version, &permissions, &key.Active, &key.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(permissions, &key.Permissions)
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) RotateUserKey(ctx context.Context, keyID, actorID string, canManageAll bool) (model.UserKey, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return model.UserKey{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID string
	var permissionsJSON []byte
	var active bool
	if err := tx.QueryRow(ctx, `SELECT user_id,permissions,active FROM user_keys WHERE id=$1 FOR UPDATE`, keyID).Scan(&userID, &permissionsJSON, &active); err != nil {
		return model.UserKey{}, mapError(err)
	}
	if userID != actorID && !canManageAll {
		return model.UserKey{}, ErrForbidden
	}
	var permissions map[string]bool
	_ = json.Unmarshal(permissionsJSON, &permissions)
	if (!active || !permissions["rotate"]) && !canManageAll {
		return model.UserKey{}, ErrForbidden
	}
	var version int
	if err := tx.QueryRow(ctx, `UPDATE users SET personal_key_version=personal_key_version+1,updated_at=now()
        WHERE id=$1 RETURNING personal_key_version`, userID).Scan(&version); err != nil {
		return model.UserKey{}, mapError(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE user_keys SET active=false WHERE user_id=$1`, userID); err != nil {
		return model.UserKey{}, err
	}
	if err := s.ensureUserKeyTx(ctx, tx, userID, version); err != nil {
		return model.UserKey{}, err
	}
	var key model.UserKey
	var keyPermissionsJSON []byte
	if err := tx.QueryRow(ctx, `SELECT id,user_id,version,permissions,active,created_at
        FROM user_keys WHERE user_id=$1 AND version=$2`, userID, version).Scan(
		&key.ID, &key.UserID, &key.Version, &keyPermissionsJSON, &key.Active, &key.CreatedAt); err != nil {
		return model.UserKey{}, err
	}
	_ = json.Unmarshal(keyPermissionsJSON, &key.Permissions)
	if err := tx.Commit(ctx); err != nil {
		return model.UserKey{}, err
	}
	return key, nil
}

func (s *Store) UpdateKeyPermissions(ctx context.Context, keyID string, permissions map[string]any, actorID string, manageAll bool) (model.UserKey, error) {
	clean, err := cleanUserKeyPermissions(permissions)
	if err != nil {
		return model.UserKey{}, err
	}
	encoded, _ := json.Marshal(clean)
	var key model.UserKey
	var data []byte
	err = s.pool.QueryRow(ctx, `UPDATE user_keys SET permissions=$2 WHERE id=$1 AND active=true AND (user_id=$3 OR $4=true)
        RETURNING id,user_id,version,permissions,active,created_at`, keyID, encoded, actorID, manageAll).Scan(
		&key.ID, &key.UserID, &key.Version, &data, &key.Active, &key.CreatedAt)
	if err != nil {
		return model.UserKey{}, mapError(err)
	}
	_ = json.Unmarshal(data, &key.Permissions)
	return key, nil
}

func cleanUserKeyPermissions(permissions map[string]any) (map[string]bool, error) {
	allowed := map[string]bool{"encrypt": true, "decrypt": true, "rotate": true}
	clean := make(map[string]bool)
	for name, raw := range permissions {
		value, ok := raw.(bool)
		if !ok || !allowed[name] {
			return nil, ErrInvalid
		}
		clean[name] = value
	}
	// Historical secret versions depend on their wrapping key remaining decryptable.
	if !clean["decrypt"] {
		return nil, fmt.Errorf("%w: 개인 키의 decrypt 권한은 제거할 수 없습니다", ErrInvalid)
	}
	return clean, nil
}

func (s *Store) userDataKey(ctx context.Context, userID string, version int) (string, []byte, error) {
	var id string
	var encrypted, nonce []byte
	err := s.pool.QueryRow(ctx, `SELECT id,encrypted_key,nonce FROM user_keys WHERE user_id=$1 AND version=$2`, userID, version).Scan(&id, &encrypted, &nonce)
	if err != nil {
		return "", nil, mapError(err)
	}
	key, err := s.master.Decrypt(encrypted, nonce, []byte(fmt.Sprintf("user-key:%s:%d", userID, version)))
	return id, key, err
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 500 {
		return 500
	}
	return limit
}
