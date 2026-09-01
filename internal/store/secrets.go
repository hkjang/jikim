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
	"github.com/jackc/pgx/v5/pgconn"
)

var secretPathPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,511}$`)

// ErrCheckAndSet is returned when an OpenBao KV v2 write supplies a CAS value
// that does not match the key's current version.
var ErrCheckAndSet = errors.New("check-and-set parameter did not match the current version")

type OpenBaoKVVersion struct {
	Version        int
	CreatedTime    time.Time
	DeletionTime   *time.Time
	Destroyed      bool
	CustomMetadata map[string]any
}

type OpenBaoKVMetadata struct {
	CurrentVersion int
	OldestVersion  int
	CreatedTime    time.Time
	UpdatedTime    time.Time
	CustomMetadata map[string]any
	Versions       []OpenBaoKVVersion
}

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

// PutOpenBaoSecret creates a KV v2 version without replacing jikim's
// application, owner, description, tag, or risk metadata. CAS is evaluated
// while the secret row is locked in the same serializable transaction as the
// version insert.
func (s *Store) PutOpenBaoSecret(ctx context.Context, path string, data map[string]any, actorID string, cas *int) (model.Secret, error) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if err := ValidateSecretPath(path); err != nil {
		return model.Secret{}, err
	}
	if data == nil {
		return model.Secret{}, fmt.Errorf("%w: secret data가 비어 있습니다", ErrInvalid)
	}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		secret, err := s.putOpenBaoSecretOnce(ctx, path, data, actorID, cas)
		if err == nil {
			return secret, nil
		}
		if cas != nil || (!isPostgresSerialization(err) && !errors.Is(err, ErrConflict)) {
			return model.Secret{}, err
		}
		lastErr = err
		if ctx.Err() != nil {
			return model.Secret{}, ctx.Err()
		}
	}
	return model.Secret{}, lastErr
}

func (s *Store) putOpenBaoSecretOnce(ctx context.Context, path string, data map[string]any, actorID string, cas *int) (model.Secret, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return model.Secret{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var secretID string
	var currentVersion int
	err = tx.QueryRow(ctx, `SELECT id,current_version FROM secrets WHERE path=$1 FOR UPDATE`, path).Scan(&secretID, &currentVersion)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		if openBaoCASStorageConflict(cas, err) {
			return model.Secret{}, ErrCheckAndSet
		}
		return model.Secret{}, mapError(err)
	}
	if err := validateOpenBaoCAS(cas, currentVersion, exists); err != nil {
		return model.Secret{}, err
	}

	metadata := map[string]any{}
	if !exists {
		secretID, err = ids.UUID()
		if err != nil {
			return model.Secret{}, err
		}
		input := model.SecretWrite{Path: path, Data: data}
		_, err = tx.Exec(ctx, `INSERT INTO secrets
			(id,path,risk_score,current_version,created_by) VALUES($1,$2,$3,0,$4)`,
			secretID, path, riskScore(input), actorID)
		if err != nil && openBaoCASStorageConflict(cas, err) {
			return model.Secret{}, ErrCheckAndSet
		}
		currentVersion = 0
	} else {
		if currentVersion > 0 {
			var raw []byte
			if err := tx.QueryRow(ctx, `SELECT metadata FROM secret_versions WHERE secret_id=$1 AND version=$2`,
				secretID, currentVersion).Scan(&raw); err != nil {
				if openBaoCASStorageConflict(cas, err) {
					return model.Secret{}, ErrCheckAndSet
				}
				return model.Secret{}, mapError(err)
			}
			if err := json.Unmarshal(raw, &metadata); err != nil {
				return model.Secret{}, err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE secrets SET deleted_at=NULL,updated_at=now() WHERE id=$1`, secretID)
	}
	if err != nil {
		if openBaoCASStorageConflict(cas, err) {
			return model.Secret{}, ErrCheckAndSet
		}
		return model.Secret{}, mapError(err)
	}
	secret, err := s.insertSecretVersionTx(ctx, tx, secretID, currentVersion,
		model.SecretWrite{Path: path, Data: data, Metadata: metadata}, actorID)
	if err != nil {
		if openBaoCASStorageConflict(cas, err) {
			return model.Secret{}, ErrCheckAndSet
		}
		return model.Secret{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		if openBaoCASStorageConflict(cas, err) {
			return model.Secret{}, ErrCheckAndSet
		}
		return model.Secret{}, err
	}
	return secret, nil
}

func validateOpenBaoCAS(cas *int, currentVersion int, exists bool) error {
	if cas == nil {
		return nil
	}
	if *cas < 0 {
		return ErrCheckAndSet
	}
	if (!exists && *cas == 0) || (exists && *cas == currentVersion) {
		return nil
	}
	return ErrCheckAndSet
}

func openBaoCASStorageConflict(cas *int, err error) bool {
	if cas == nil || err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" || pgErr.Code == "40001"
}

func isPostgresSerialization(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40001"
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
	return s.insertSecretVersionTx(ctx, tx, secretID, currentVersion, input, actorID)
}

func (s *Store) insertSecretVersionTx(ctx context.Context, tx pgx.Tx, secretID string, currentVersion int, input model.SecretWrite, actorID string) (model.Secret, error) {
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
	var versionCreatedAt, secretCreatedAt time.Time
	query := `SELECT s.id,s.path,s.description,s.application_id,s.owner_user_id,s.tags,s.risk_score,
        s.current_version,sv.version,sv.metadata,sv.ciphertext,sv.nonce,sv.encryption_key_id,
        sv.key_version,sv.created_by,sv.created_at,s.created_at,s.updated_at,
        uk.user_id,uk.encrypted_key,uk.nonce
        FROM secrets s JOIN secret_versions sv ON sv.secret_id=s.id
		JOIN user_keys uk ON uk.id=sv.encryption_key_id AND COALESCE((uk.permissions->>'decrypt')::boolean,false)=true
		WHERE s.path=$1 AND s.deleted_at IS NULL AND sv.destroyed=false AND sv.deletion_time IS NULL
		AND sv.version=CASE WHEN $2::int=0 THEN s.current_version ELSE $2 END`
	err := q.QueryRow(ctx, query, path, version).Scan(&secret.ID, &secret.Path, &secret.Description,
		&secret.ApplicationID, &secret.OwnerUserID, &tags, &secret.RiskScore, &secret.CurrentVersion,
		&secret.Version, &metadata, &ciphertext, &nonce, &keyID, &keyVersion,
		&secret.CreatedBy, &versionCreatedAt, &secretCreatedAt, &secret.UpdatedAt,
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
	secret.CreatedAt = secretCreatedAt
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
	return s.listSecretsFiltered(ctx, nil, search, environment, limit, offset)
}

// ListSecretsAuthorizedFiltered applies list capability filtering before
// LIMIT/OFFSET. HTTP callers must use this method for user-visible pagination;
// filtering an already paginated ListSecretsFiltered result can produce short
// or empty pages even when later authorized rows exist.
func (s *Store) ListSecretsAuthorizedFiltered(ctx context.Context, user model.User, search, environment string, limit, offset int) ([]SecretListItem, error) {
	return s.listSecretsFiltered(ctx, &user, search, environment, limit, offset)
}

func (s *Store) listSecretsFiltered(ctx context.Context, user *model.User, search, environment string, limit, offset int) ([]SecretListItem, error) {
	environment = strings.ToUpper(strings.TrimSpace(environment))
	if environment != "" && environment != "DEV" && environment != "STG" && environment != "PRD" {
		return nil, fmt.Errorf("%w: environment는 DEV, STG, PRD 중 하나여야 합니다", ErrInvalid)
	}
	bypassAuthorization := user == nil
	userID, role := "", ""
	if user != nil {
		userID, role = user.ID, user.Role
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
		AND ($4::boolean OR $5 IN ('admin','manager','auditor') OR EXISTS (
			SELECT 1 FROM user_policies up JOIN policies p ON p.id=up.policy_id
			CROSS JOIN LATERAL jsonb_array_elements(
				CASE WHEN jsonb_typeof(p.rules->'paths')='array' THEN p.rules->'paths' ELSE '[]'::jsonb END
			) AS policy_rule
			WHERE up.user_id=$6
			AND COALESCE(policy_rule->'capabilities','[]'::jsonb) ? 'list'
			AND (
				trim(both '/' from policy_rule->>'path')='*'
				OR (right(trim(both '/' from policy_rule->>'path'),1)='*'
					AND left(s.path,length(trim(both '/' from policy_rule->>'path'))-1)=
						left(trim(both '/' from policy_rule->>'path'),length(trim(both '/' from policy_rule->>'path'))-1))
				OR s.path=trim(both '/' from policy_rule->>'path')
			)
		))
		ORDER BY s.path LIMIT $7 OFFSET $8`, strings.TrimSpace(search), like, environment,
		bypassAuthorization, role, userID, boundedLimit(limit), max(offset, 0))
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
	rows, err := s.pool.Query(ctx, `SELECT sv.version,sv.created_by,sv.created_at,sv.deletion_time,sv.destroyed,sv.metadata
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
		var deletionTime *time.Time
		var destroyed bool
		var metadata []byte
		if err := rows.Scan(&version, &createdBy, &createdAt, &deletionTime, &destroyed, &metadata); err != nil {
			return nil, err
		}
		var meta map[string]any
		_ = json.Unmarshal(metadata, &meta)
		status := "active"
		if deletionTime != nil {
			status = "deleted"
		}
		if destroyed {
			status = "destroyed"
		}
		result = append(result, map[string]any{"version": version, "created_by": createdBy,
			"created_at": createdAt, "deletion_time": deletionTime, "destroyed": destroyed,
			"metadata": meta, "status": status})
	}
	return result, rows.Err()
}

func (s *Store) OpenBaoKVMetadata(ctx context.Context, path string) (OpenBaoKVMetadata, error) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if err := ValidateSecretPath(path); err != nil {
		return OpenBaoKVMetadata{}, err
	}
	var result OpenBaoKVMetadata
	var secretID string
	if err := s.pool.QueryRow(ctx, `SELECT id,current_version,created_at,updated_at
		FROM secrets WHERE path=$1 AND deleted_at IS NULL`, path).Scan(
		&secretID, &result.CurrentVersion, &result.CreatedTime, &result.UpdatedTime); err != nil {
		return OpenBaoKVMetadata{}, mapError(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT version,created_at,deletion_time,destroyed,metadata
		FROM secret_versions WHERE secret_id=$1 ORDER BY version`, secretID)
	if err != nil {
		return OpenBaoKVMetadata{}, err
	}
	defer rows.Close()
	result.Versions = make([]OpenBaoKVVersion, 0)
	for rows.Next() {
		var version OpenBaoKVVersion
		var raw []byte
		if err := rows.Scan(&version.Version, &version.CreatedTime, &version.DeletionTime,
			&version.Destroyed, &raw); err != nil {
			return OpenBaoKVMetadata{}, err
		}
		version.CustomMetadata = map[string]any{}
		if err := json.Unmarshal(raw, &version.CustomMetadata); err != nil {
			return OpenBaoKVMetadata{}, err
		}
		if version.Version == result.CurrentVersion && len(version.CustomMetadata) > 0 {
			result.CustomMetadata = cloneAnyMap(version.CustomMetadata)
		}
		result.Versions = append(result.Versions, version)
	}
	if err := rows.Err(); err != nil {
		return OpenBaoKVMetadata{}, err
	}
	if len(result.Versions) == 0 {
		return OpenBaoKVMetadata{}, ErrNotFound
	}
	// This implementation does not prune versions automatically, so OpenBao's
	// oldest_version watermark remains zero.
	result.OldestVersion = 0
	return result, nil
}

func cloneAnyMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
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
	return s.mutateOpenBaoVersions(ctx, path, versions, "destroy")
}

func (s *Store) SoftDeleteLatestOpenBaoVersion(ctx context.Context, path string) error {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if err := ValidateSecretPath(path); err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	secretID, currentVersion, err := lockOpenBaoSecret(ctx, tx, path)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE secret_versions SET deletion_time=COALESCE(deletion_time,now())
		WHERE secret_id=$1 AND version=$2 AND destroyed=false`, secretID, currentVersion)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SoftDeleteOpenBaoVersions(ctx context.Context, path string, versions []int) error {
	return s.mutateOpenBaoVersions(ctx, path, versions, "delete")
}

func (s *Store) UndeleteOpenBaoVersions(ctx context.Context, path string, versions []int) error {
	return s.mutateOpenBaoVersions(ctx, path, versions, "undelete")
}

func (s *Store) DeleteOpenBaoMetadata(ctx context.Context, path string) error {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if err := ValidateSecretPath(path); err != nil {
		return err
	}
	command, err := s.pool.Exec(ctx, `DELETE FROM secrets WHERE path=$1`, path)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil

}

func (s *Store) mutateOpenBaoVersions(ctx context.Context, path string, versions []int, operation string) error {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if err := ValidateSecretPath(path); err != nil {
		return err
	}
	clean, err := normalizeOpenBaoVersions(versions)
	if err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	secretID, _, err := lockOpenBaoSecret(ctx, tx, path)
	if err != nil {
		return err
	}
	switch operation {
	case "delete":
		_, err = tx.Exec(ctx, `UPDATE secret_versions SET deletion_time=COALESCE(deletion_time,now())
			WHERE secret_id=$1 AND version=ANY($2) AND destroyed=false`, secretID, clean)
	case "undelete":
		_, err = tx.Exec(ctx, `UPDATE secret_versions SET deletion_time=NULL
			WHERE secret_id=$1 AND version=ANY($2) AND destroyed=false AND deletion_time IS NOT NULL`, secretID, clean)
	case "destroy":
		_, err = tx.Exec(ctx, `UPDATE secret_versions SET destroyed=true,deletion_time=NULL,
			ciphertext='\x'::bytea,nonce='\x'::bytea
			WHERE secret_id=$1 AND version=ANY($2) AND destroyed=false`, secretID, clean)
	default:
		return ErrInvalid
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func lockOpenBaoSecret(ctx context.Context, tx pgx.Tx, path string) (string, int, error) {
	var secretID string
	var currentVersion int
	err := tx.QueryRow(ctx, `SELECT id,current_version FROM secrets
		WHERE path=$1 AND deleted_at IS NULL FOR UPDATE`, path).Scan(&secretID, &currentVersion)
	if err != nil {
		return "", 0, mapError(err)
	}
	return secretID, currentVersion, nil
}

func normalizeOpenBaoVersions(versions []int) ([]int, error) {
	if len(versions) == 0 {
		return nil, fmt.Errorf("%w: versions가 필요합니다", ErrInvalid)
	}
	seen := make(map[int]bool, len(versions))
	result := make([]int, 0, len(versions))
	for _, version := range versions {
		if version < 1 {
			continue
		}
		if !seen[version] {
			seen[version] = true
			result = append(result, version)
		}
	}
	sort.Ints(result)
	return result, nil
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
