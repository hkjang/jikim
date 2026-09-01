package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/hkjang/jikim/internal/ids"
	"github.com/jackc/pgx/v5"
)

func (s *Store) SealOIDCState(value any) (string, error) {
	ciphertext, nonce, err := s.master.EncryptJSON(value, []byte("oidc-state:v1"))
	if err != nil {
		return "", err
	}
	payload := append(append([]byte(nil), nonce...), ciphertext...)
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func (s *Store) OpenOIDCState(encoded string, value any) error {
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) <= 12 {
		return ErrInvalid
	}
	plaintext, err := s.master.Decrypt(payload[12:], payload[:12], []byte("oidc-state:v1"))
	if err != nil {
		return ErrInvalid
	}
	if err := json.Unmarshal(plaintext, value); err != nil {
		return ErrInvalid
	}
	return nil
}

func (s *Store) CreateOIDCLoginCode(ctx context.Context, userID string) (string, error) {
	plain, hash, err := ids.Token("jko.")
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO oidc_login_codes(code_hash,user_id,expires_at)
        VALUES($1,$2,$3)`, hash, userID, time.Now().UTC().Add(2*time.Minute))
	if err != nil {
		return "", mapError(err)
	}
	return plain, nil
}

func (s *Store) ConsumeOIDCLoginCode(ctx context.Context, plain string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID string
	err = tx.QueryRow(ctx, `DELETE FROM oidc_login_codes WHERE code_hash=$1 AND expires_at>now()
        RETURNING user_id`, ids.HashToken(plain)).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrUnauthorized
		}
		return "", err
	}
	_, _ = tx.Exec(ctx, `DELETE FROM oidc_login_codes WHERE expires_at<=now()`)
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return userID, nil
}
