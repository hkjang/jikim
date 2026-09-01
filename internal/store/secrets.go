package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/cryptox"
	"github.com/hkjang/jikim/internal/ids"
	"github.com/hkjang/jikim/internal/model"
	"github.com/jackc/pgx/v5"
)

var secretPathPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,511}$`)

func ValidateSecretPath(value string) error {
	if !secretPathPattern.MatchString(value) || strings.Contains(value, "//") || strings.Contains(value, "..") || strings.HasSuffix(value, "/") {
		return fmt.Errorf("%w: secret 경로 형식이 올바르지 않습니다", ErrInvalid)
	}
	return nil
}

func riskScore(input model.SecretWrite) int {
	score := 0
	lower := strings.ToLower(input.Path)
	if strings.Contains(lower, "prod") || strings.Contains(lower, "prd") {
		score += 20
	}
	if input.OwnerUserID == nil || *input.OwnerUserID == "" {
		score += 15
	}
	if input.ApplicationID == nil || *input.ApplicationID == "" {
		score += 10
	}
	for key := range input.Data {
		name := strings.ToLower(key)
		if strings.Contains(name, "root") || strings.Contains(name, "admin") || strings.Contains(name, "private") {
			score += 20
			break
		}
	}
	if score > 100 {
		return 100
	}
	return score
}

func (s *Store) PutSecret(ctx context.Context, input model.SecretWrite, actorID string) (model.Secret, error) {
	input.Path = strings.Trim(strings.TrimSpace(input.Path), "/")
	if err := ValidateSecretPath(input.Path); err != nil {
		return model.Secret{}, err
	}
	if len(input.Data) == 0 {
		return model.Secret{}, fmt.Errorf("%w: secret data가 비어 있습니다", ErrInvalid)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return model.Secret{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	secret, err := s.putSecretTx(ctx, tx, input, actorID)
	if err != nil {
		return model.Secret{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Secret{}, err
	}
	return secret, nil
}

func (s *Store) putSecretTx(ctx context.Context, tx pgx.Tx, input model.SecretWrite, actorID string) (model.Secret, error) {
	var secretID string
	var currentVersion int
	err := tx.QueryRow(ctx, `SELECT id,current_version FROM secrets WHERE path=$1 FOR UPDATE`, input.Path).Scan(&secretID, &currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		secretID, err = ids.UUID()
		if err != nil {
			return model.Secret{}, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO secrets
            (id,path,description,application_id,owner_user_id,tags,risk_score,current_version,created_by)
            VALUES($1,$2,$3,$4,$5,$6,$7,0,$8)`, secretID, input.Path, input.Description,
			input.ApplicationID, input.OwnerUserID, mustJSON(input.Tags), riskScore(input), actorID)
		currentVersion = 0
	} else if err == nil {
		_, err = tx.Exec(ctx, `UPDATE secrets SET description=$2,application_id=$3,owner_user_id=$4,
            tags=$5,risk_score=$6,deleted_at=NULL,updated_at=now() WHERE id=$1`, secretID, input.Description,
			input.ApplicationID, input.OwnerUserID, mustJSON(input.Tags), riskScore(input))
	}
	if err != nil {
		return model.Secret{}, mapError(err)
	}
	version := currentVersion + 1
	var keyVersion int
	if err := tx.QueryRow(ctx, `SELECT personal_key_version FROM users WHERE id=$1`, actorID).Scan(&keyVersion); err != nil {
		return model.Secret{}, mapError(err)
	}
	keyID, dataKey, err := s.userDataKeyTx(ctx, tx, actorID, keyVersion)
	if err != nil {
		return model.Secret{}, err
	}
	dataCipher, err := cryptox.New(dataKey)
	if err != nil {
		return model.Secret{}, err
	}
	plaintext, err := json.Marshal(input.Data)
	if err != nil {
		return model.Secret{}, err
	}
	aad := []byte(fmt.Sprintf("secret:%s:%d", secretID, version))
	ciphertext, nonce, err := dataCipher.Encrypt(plaintext, aad)
	if err != nil {
		return model.Secret{}, err
	}
	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	_, err = tx.Exec(ctx, `INSERT INTO secret_versions
        (secret_id,version,ciphertext,nonce,encryption_key_id,key_version,metadata,created_by)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, secretID, version, ciphertext, nonce, keyID,
		keyVersion, mustJSON(metadata), actorID)
	if err != nil {
		return model.Secret{}, mapError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE secrets SET current_version=$2,updated_at=now() WHERE id=$1`, secretID, version)
	if err != nil {
		return model.Secret{}, err
	}
	return s.getSecretTx(ctx, tx, input.Path, version)
}

func (s *Store) GetSecret(ctx context.Context, path string, version int) (model.Secret, error) {
	return s.getSecretRow(ctx, s.pool, strings.Trim(path, "/"), version)
}

func (s *Store) SecretPathByID(ctx context.Context, idOrPath string) (string, error) {
	var path string
	err := s.pool.QueryRow(ctx, `SELECT path FROM secrets WHERE (id=$1 OR path=$1) AND deleted_at IS NULL`, idOrPath).Scan(&path)
	return path, mapError(err)
}

func (s *Store) SecretExistsByPath(ctx context.Context, path string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM secrets WHERE path=$1 AND deleted_at IS NULL)`, strings.Trim(path, "/")).Scan(&exists)
	return exists, err
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Store) getSecretTx(ctx context.Context, tx pgx.Tx, path string, version int) (model.Secret, error) {
	return s.getSecretRow(ctx, tx, path, version)
}

func (s *Store) getSecretRow(ctx context.Context, q queryer, path string, version int) (model.Secret, error) {
	var secret model.Secret
	var tags, metadata, ciphertext, nonce, wrappedKey, keyNonce []byte
	var keyUserID, keyID string
	var keyVersion int
	query := `SELECT s.id,s.path,s.description,s.application_id,s.owner_user_id,s.tags,s.risk_score,
        s.current_version,sv.version,sv.metadata,sv.ciphertext,sv.nonce,sv.encryption_key_id,
        sv.key_version,sv.created_by,sv.created_at,s.created_at,s.updated_at,
        uk.user_id,uk.encrypted_key,uk.nonce
        FROM secrets s JOIN secret_versions sv ON sv.secret_id=s.id
		JOIN user_keys uk ON uk.id=sv.encryption_key_id AND COALESCE((uk.permissions->>'decrypt')::boolean,false)=true
        WHERE s.path=$1 AND s.deleted_at IS NULL AND sv.destroyed=false
        AND sv.version=CASE WHEN $2::int=0 THEN s.current_version ELSE $2 END`
	err := q.QueryRow(ctx, query, path, version).Scan(&secret.ID, &secret.Path, &secret.Description,
		&secret.ApplicationID, &secret.OwnerUserID, &tags, &secret.RiskScore, &secret.CurrentVersion,
		&secret.Version, &metadata, &ciphertext, &nonce, &keyID, &keyVersion,
		&secret.CreatedBy, &secret.CreatedAt, &secret.CreatedAt, &secret.UpdatedAt,
		&keyUserID, &wrappedKey, &keyNonce)
	if err != nil {
		return model.Secret{}, mapError(err)
	}
	dataKey, err := s.master.Decrypt(wrappedKey, keyNonce, []byte(fmt.Sprintf("user-key:%s:%d", keyUserID, keyVersion)))
	if err != nil {
		return model.Secret{}, err
	}
	dataCipher, err := cryptox.New(dataKey)
	if err != nil {
		return model.Secret{}, err
	}
	plaintext, err := dataCipher.Decrypt(ciphertext, nonce, []byte(fmt.Sprintf("secret:%s:%d", secret.ID, secret.Version)))
	if err != nil {
		return model.Secret{}, err
	}
	if err := json.Unmarshal(tags, &secret.Tags); err != nil {
		return model.Secret{}, err
	}
	if err := json.Unmarshal(metadata, &secret.Metadata); err != nil {
		return model.Secret{}, err
	}
	if err := json.Unmarshal(plaintext, &secret.Data); err != nil {
		return model.Secret{}, err
	}
	secret.Application, _ = secret.Metadata["application"].(string)
	secret.Environment, _ = secret.Metadata["environment"].(string)
	secret.Owner, _ = secret.Metadata["owner"].(string)
	secret.Status = "active"
	return secret, nil
}

func (s *Store) userDataKeyTx(ctx context.Context, tx pgx.Tx, userID string, version int) (string, []byte, error) {
	var id string
	var encrypted, nonce []byte
	err := tx.QueryRow(ctx, `SELECT id,encrypted_key,nonce FROM user_keys
        WHERE user_id=$1 AND version=$2 AND COALESCE((permissions->>'encrypt')::boolean,false)=true`, userID, version).Scan(&id, &encrypted, &nonce)
	if err != nil {
		return "", nil, mapError(err)
	}
	key, err := s.master.Decrypt(encrypted, nonce, []byte(fmt.Sprintf("user-key:%s:%d", userID, version)))
	return id, key, err
}

type SecretListItem struct {
	ID             string    `json:"id"`
	Path           string    `json:"path"`
	Description    string    `json:"description"`
	ApplicationID  *string   `json:"application_id,omitempty"`
	OwnerUserID    *string   `json:"owner_user_id,omitempty"`
	Tags           []string  `json:"tags"`
	RiskScore      int       `json:"risk_score"`
	CurrentVersion int       `json:"current_version"`
	Version        int       `json:"version"`
	Application    string    `json:"application,omitempty"`
	Environment    string    `json:"environment,omitempty"`
	Owner          string    `json:"owner,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (s *Store) ListSecrets(ctx context.Context, search string, limit, offset int) ([]SecretListItem, error) {
	return s.ListSecretsFiltered(ctx, search, "", limit, offset)
}

func (s *Store) ListSecretsFiltered(ctx context.Context, search, environment string, limit, offset int) ([]SecretListItem, error) {
	environment = strings.ToUpper(strings.TrimSpace(environment))
	if environment != "" && environment != "DEV" && environment != "STG" && environment != "PRD" {
		return nil, fmt.Errorf("%w: environment는 DEV, STG, PRD 중 하나여야 합니다", ErrInvalid)
	}
	like := "%" + strings.ToLower(strings.TrimSpace(search)) + "%"
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.path,s.description,s.application_id,s.owner_user_id,s.tags,
		s.risk_score,s.current_version,s.created_at,s.updated_at,
		COALESCE(a.name,sv.metadata->>'application',''),COALESCE(sv.metadata->>'environment',a.environment,''),
		COALESCE(NULLIF(ou.display_name,''),ou.username,sv.metadata->>'owner','')
		FROM secrets s LEFT JOIN applications a ON a.id=s.application_id
		LEFT JOIN users ou ON ou.id=s.owner_user_id
		LEFT JOIN secret_versions sv ON sv.secret_id=s.id AND sv.version=s.current_version
		WHERE s.deleted_at IS NULL
		AND ($1='' OR lower(s.path) LIKE $2 OR lower(s.description) LIKE $2
			OR lower(COALESCE(a.name,sv.metadata->>'application','')) LIKE $2
			OR lower(COALESCE(NULLIF(ou.display_name,''),ou.username,sv.metadata->>'owner','')) LIKE $2
			OR lower(s.tags::text) LIKE $2)
		AND ($3='' OR upper(COALESCE(sv.metadata->>'environment',a.environment,''))=$3)
		ORDER BY s.path LIMIT $4 OFFSET $5`, strings.TrimSpace(search), like, environment, boundedLimit(limit), max(offset, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SecretListItem, 0)
	for rows.Next() {
		var item SecretListItem
		var tags []byte
		if err := rows.Scan(&item.ID, &item.Path, &item.Description, &item.ApplicationID,
			&item.OwnerUserID, &tags, &item.RiskScore, &item.CurrentVersion,
			&item.CreatedAt, &item.UpdatedAt, &item.Application, &item.Environment, &item.Owner); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(tags, &item.Tags)
		item.Version = item.CurrentVersion
		item.Status = "active"
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SecretVersions(ctx context.Context, path string) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `SELECT sv.version,sv.created_by,sv.created_at,sv.destroyed,sv.metadata
        FROM secret_versions sv JOIN secrets s ON s.id=sv.secret_id
        WHERE s.path=$1 ORDER BY sv.version DESC`, strings.Trim(path, "/"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var version int
		var createdBy string
		var createdAt time.Time
		var destroyed bool
		var metadata []byte
		if err := rows.Scan(&version, &createdBy, &createdAt, &destroyed, &metadata); err != nil {
			return nil, err
		}
		var meta map[string]any
		_ = json.Unmarshal(metadata, &meta)
		result = append(result, map[string]any{"version": version, "created_by": createdBy,
			"created_at": createdAt, "destroyed": destroyed, "metadata": meta})
	}
	return result, rows.Err()
}

func (s *Store) DeleteSecret(ctx context.Context, path string) error {
	command, err := s.pool.Exec(ctx, `UPDATE secrets SET deleted_at=now(),updated_at=now() WHERE path=$1 AND deleted_at IS NULL`, strings.Trim(path, "/"))
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DestroySecretVersions(ctx context.Context, path string, versions []int) error {
	if len(versions) == 0 {
		return ErrInvalid
	}
	command, err := s.pool.Exec(ctx, `UPDATE secret_versions SET destroyed=true,ciphertext='\x'::bytea,nonce='\x'::bytea
        WHERE secret_id=(SELECT id FROM secrets WHERE path=$1) AND version=ANY($2)`, strings.Trim(path, "/"), versions)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListSecretChildren(ctx context.Context, prefix string) ([]string, error) {
	prefix = strings.Trim(prefix, "/")
	like := "%"
	if prefix != "" {
		like = prefix + "/%"
	}
	rows, err := s.pool.Query(ctx, `SELECT path FROM secrets WHERE deleted_at IS NULL AND path LIKE $1 ORDER BY path`, like)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	for rows.Next() {
		var full string
		if err := rows.Scan(&full); err != nil {
			return nil, err
		}
		rest := full
		if prefix != "" {
			rest = strings.TrimPrefix(full, prefix+"/")
		}
		parts := strings.SplitN(rest, "/", 2)
		name := parts[0]
		if len(parts) == 2 {
			name += "/"
		}
		seen[name] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, rows.Err()
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}
