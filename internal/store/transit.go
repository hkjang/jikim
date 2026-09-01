package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/cryptox"
	"github.com/hkjang/jikim/internal/model"
	"github.com/jackc/pgx/v5"
)

func (s *Store) TransitEncrypt(ctx context.Context, name, plaintextBase64, actorID string) (string, error) {
	if err := ValidateSecretPath(name); err != nil || strings.Contains(name, "/") {
		return "", ErrInvalid
	}
	plaintext, err := base64.StdEncoding.DecodeString(plaintextBase64)
	if err != nil {
		return "", fmt.Errorf("%w: plaintext는 base64여야 합니다", ErrInvalid)
	}
	version, key, err := s.transitKey(ctx, name, 0, actorID, true)
	if err != nil {
		return "", err
	}
	cipher, err := cryptox.New(key)
	if err != nil {
		return "", err
	}
	encrypted, nonce, err := cipher.Encrypt(plaintext, []byte(fmt.Sprintf("transit:%s:v%d", name, version)))
	if err != nil {
		return "", err
	}
	payload := append(append([]byte(nil), nonce...), encrypted...)
	return fmt.Sprintf("vault:v%d:%s", version, base64.RawStdEncoding.EncodeToString(payload)), nil
}

func (s *Store) TransitDecrypt(ctx context.Context, name, encoded string) (string, error) {
	parts := strings.SplitN(encoded, ":", 3)
	if len(parts) != 3 || parts[0] != "vault" || !strings.HasPrefix(parts[1], "v") {
		return "", fmt.Errorf("%w: ciphertext 형식", ErrInvalid)
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[1], "v"))
	if err != nil || version < 1 {
		return "", ErrInvalid
	}
	payload, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(payload) <= 12 {
		return "", ErrInvalid
	}
	storedVersion, key, err := s.transitKey(ctx, name, version, "", false)
	if err != nil {
		return "", err
	}
	if storedVersion != version {
		return "", fmt.Errorf("%w: 지원하지 않는 키 버전", ErrInvalid)
	}
	cipher, err := cryptox.New(key)
	if err != nil {
		return "", err
	}
	plaintext, err := cipher.Decrypt(payload[12:], payload[:12], []byte(fmt.Sprintf("transit:%s:v%d", name, version)))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(plaintext), nil
}

func (s *Store) TransitPermission(ctx context.Context, name, operation string) (bool, error) {
	if operation != "encrypt" && operation != "decrypt" && operation != "rotate" && operation != "manage" {
		return false, ErrInvalid
	}
	var permissions []byte
	err := s.pool.QueryRow(ctx, `SELECT permissions FROM transit_keys WHERE name=$1`, name).Scan(&permissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	var values map[string]bool
	if err := json.Unmarshal(permissions, &values); err != nil {
		return false, err
	}
	return values[operation], nil
}

func (s *Store) transitKey(ctx context.Context, name string, requestedVersion int, actorID string, create bool) (int, []byte, error) {
	var version int
	var encrypted, nonce []byte
	err := s.pool.QueryRow(ctx, `SELECT tkv.version,tkv.encrypted_key,tkv.nonce FROM transit_keys tk
        JOIN transit_key_versions tkv ON tkv.key_name=tk.name
        WHERE tk.name=$1 AND tkv.version=CASE WHEN $2::int=0 THEN tk.version ELSE $2 END`, name, requestedVersion).Scan(&version, &encrypted, &nonce)
	if err == nil {
		key, decryptErr := s.master.Decrypt(encrypted, nonce, []byte(fmt.Sprintf("transit-key:%s:v%d", name, version)))
		return version, key, decryptErr
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, err
	}
	if !create || actorID == "" {
		return 0, nil, ErrNotFound
	}
	key, err := cryptox.RandomKey()
	if err != nil {
		return 0, nil, err
	}
	version = 1
	encrypted, nonce, err = s.master.Encrypt(key, []byte(fmt.Sprintf("transit-key:%s:v%d", name, version)))
	if err != nil {
		return 0, nil, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `INSERT INTO transit_keys(name,version,encrypted_key,nonce,created_by)
        VALUES($1,1,$2,$3,$4) ON CONFLICT(name) DO NOTHING`, name, encrypted, nonce, actorID)
	if err != nil {
		return 0, nil, mapError(err)
	}
	if command.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO transit_key_versions(key_name,version,encrypted_key,nonce)
            VALUES($1,1,$2,$3)`, name, encrypted, nonce); err != nil {
			return 0, nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, err
	}
	// A concurrent creator may have won; always reload the canonical key.
	err = s.pool.QueryRow(ctx, `SELECT tkv.version,tkv.encrypted_key,tkv.nonce FROM transit_keys tk
        JOIN transit_key_versions tkv ON tkv.key_name=tk.name AND tkv.version=tk.version WHERE tk.name=$1`, name).Scan(&version, &encrypted, &nonce)
	if err != nil {
		return 0, nil, mapError(err)
	}
	key, err = s.master.Decrypt(encrypted, nonce, []byte(fmt.Sprintf("transit-key:%s:v%d", name, version)))
	return version, key, err
}

type KeyInventoryItem struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Algorithm   string         `json:"algorithm"`
	Version     int            `json:"version"`
	Status      string         `json:"status"`
	Owner       string         `json:"owner"`
	OwnerUserID string         `json:"owner_user_id,omitempty"`
	Permissions map[string]any `json:"permissions"`
	CreatedAt   time.Time      `json:"created_at"`
}

func (s *Store) ListKeyInventory(ctx context.Context, actorID string, all bool) ([]KeyInventoryItem, error) {
	rows, err := s.pool.Query(ctx, `SELECT uk.id,u.username,uk.user_id,uk.version,uk.active,uk.permissions,uk.created_at
        FROM user_keys uk JOIN users u ON u.id=uk.user_id WHERE ($1=true OR uk.user_id=$2)
        ORDER BY u.username,uk.version DESC`, all, actorID)
	if err != nil {
		return nil, err
	}
	items := make([]KeyInventoryItem, 0)
	for rows.Next() {
		var item KeyInventoryItem
		var active bool
		var permissions []byte
		if err := rows.Scan(&item.ID, &item.Owner, &item.OwnerUserID, &item.Version, &active, &permissions, &item.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		item.Name = item.Owner + "-personal"
		item.Type = "personal"
		item.Algorithm = "AES-256-GCM"
		item.Status = "retired"
		if active {
			item.Status = "active"
		}
		_ = json.Unmarshal(permissions, &item.Permissions)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	transitRows, err := s.pool.Query(ctx, `SELECT tk.name,tk.version,u.username,tk.permissions,tk.created_at
        FROM transit_keys tk JOIN users u ON u.id=tk.created_by ORDER BY tk.name`)
	if err != nil {
		return nil, err
	}
	defer transitRows.Close()
	for transitRows.Next() {
		var item KeyInventoryItem
		var permissions []byte
		if err := transitRows.Scan(&item.Name, &item.Version, &item.Owner, &permissions, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.ID = item.Name
		item.Type = "transit"
		item.Algorithm = "AES-256-GCM"
		item.Status = "active"
		_ = json.Unmarshal(permissions, &item.Permissions)
		items = append(items, item)
	}
	return items, transitRows.Err()
}

func (s *Store) CreateTransitKey(ctx context.Context, name, actorID string, permissions map[string]any) (KeyInventoryItem, error) {
	name = strings.TrimSpace(name)
	if err := ValidateSecretPath(name); err != nil || strings.Contains(name, "/") {
		return KeyInventoryItem{}, ErrInvalid
	}
	if len(permissions) == 0 {
		permissions = map[string]any{"encrypt": true, "decrypt": true, "rotate": true, "manage": true}
	}
	clean, err := cleanTransitPermissions(permissions)
	if err != nil {
		return KeyInventoryItem{}, err
	}
	key, err := cryptox.RandomKey()
	if err != nil {
		return KeyInventoryItem{}, err
	}
	ciphertext, nonce, err := s.master.Encrypt(key, []byte(fmt.Sprintf("transit-key:%s:v1", name)))
	if err != nil {
		return KeyInventoryItem{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return KeyInventoryItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO transit_keys(name,version,encrypted_key,nonce,permissions,created_by)
        VALUES($1,1,$2,$3,$4,$5)`, name, ciphertext, nonce, mustJSON(clean), actorID)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO transit_key_versions(key_name,version,encrypted_key,nonce) VALUES($1,1,$2,$3)`, name, ciphertext, nonce)
	}
	if err != nil {
		return KeyInventoryItem{}, mapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return KeyInventoryItem{}, err
	}
	user, err := s.GetUser(ctx, actorID)
	if err != nil {
		return KeyInventoryItem{}, err
	}
	return KeyInventoryItem{ID: name, Name: name, Type: "transit", Algorithm: "AES-256-GCM", Version: 1,
		Status: "active", Owner: user.Username, OwnerUserID: actorID, Permissions: boolMapAny(clean), CreatedAt: time.Now().UTC()}, nil
}

func (s *Store) RotateTransitKey(ctx context.Context, name string) (KeyInventoryItem, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return KeyInventoryItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int
	var ownerID, owner string
	var permissions []byte
	err = tx.QueryRow(ctx, `SELECT tk.version,tk.created_by,u.username,tk.permissions FROM transit_keys tk
        JOIN users u ON u.id=tk.created_by WHERE tk.name=$1 FOR UPDATE`, name).Scan(&version, &ownerID, &owner, &permissions)
	if err != nil {
		return KeyInventoryItem{}, mapError(err)
	}
	version++
	key, err := cryptox.RandomKey()
	if err != nil {
		return KeyInventoryItem{}, err
	}
	ciphertext, nonce, err := s.master.Encrypt(key, []byte(fmt.Sprintf("transit-key:%s:v%d", name, version)))
	if err != nil {
		return KeyInventoryItem{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transit_key_versions(key_name,version,encrypted_key,nonce) VALUES($1,$2,$3,$4)`, name, version, ciphertext, nonce); err != nil {
		return KeyInventoryItem{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE transit_keys SET version=$2,encrypted_key=$3,nonce=$4,updated_at=now() WHERE name=$1`, name, version, ciphertext, nonce); err != nil {
		return KeyInventoryItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return KeyInventoryItem{}, err
	}
	var permissionMap map[string]any
	_ = json.Unmarshal(permissions, &permissionMap)
	return KeyInventoryItem{ID: name, Name: name, Type: "transit", Algorithm: "AES-256-GCM", Version: version,
		Status: "active", Owner: owner, OwnerUserID: ownerID, Permissions: permissionMap, CreatedAt: time.Now().UTC()}, nil
}

func (s *Store) UpdateTransitPermissions(ctx context.Context, name string, permissions map[string]any) (KeyInventoryItem, error) {
	clean, err := cleanTransitPermissions(permissions)
	if err != nil {
		return KeyInventoryItem{}, err
	}
	var item KeyInventoryItem
	var raw []byte
	err = s.pool.QueryRow(ctx, `UPDATE transit_keys SET permissions=$2,updated_at=now() WHERE name=$1
        RETURNING name,version,permissions,created_at`, name, mustJSON(clean)).Scan(&item.Name, &item.Version, &raw, &item.CreatedAt)
	if err != nil {
		return KeyInventoryItem{}, mapError(err)
	}
	item.ID, item.Type, item.Algorithm, item.Status = item.Name, "transit", "AES-256-GCM", "active"
	_ = json.Unmarshal(raw, &item.Permissions)
	return item, nil
}

func cleanTransitPermissions(input map[string]any) (map[string]bool, error) {
	result := make(map[string]bool)
	for name, raw := range input {
		if name != "encrypt" && name != "decrypt" && name != "rotate" && name != "manage" {
			return nil, ErrInvalid
		}
		value, ok := raw.(bool)
		if !ok {
			return nil, ErrInvalid
		}
		result[name] = value
	}
	return result, nil
}

func boolMapAny(input map[string]bool) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

var _ model.User
