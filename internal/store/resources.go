package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hkjang/jikim/internal/ids"
	"github.com/hkjang/jikim/internal/model"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListApplications(ctx context.Context, limit, offset int) ([]model.Application, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.id,a.name,a.description,a.owner,a.environment,a.criticality,a.repository,
		(SELECT count(*) FROM secrets s WHERE s.application_id=a.id AND s.deleted_at IS NULL),a.created_at,a.updated_at
		FROM applications a ORDER BY a.name LIMIT $1 OFFSET $2`, boundedLimit(limit), max(offset, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Application, 0)
	for rows.Next() {
		var item model.Application
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Owner, &item.Environment,
			&item.Criticality, &item.Repository, &item.SecretCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetApplication(ctx context.Context, id string) (model.Application, error) {
	var item model.Application
	err := s.pool.QueryRow(ctx, `SELECT a.id,a.name,a.description,a.owner,a.environment,a.criticality,a.repository,
		(SELECT count(*) FROM secrets s WHERE s.application_id=a.id AND s.deleted_at IS NULL),a.created_at,a.updated_at
		FROM applications a WHERE a.id=$1`, id).Scan(&item.ID, &item.Name, &item.Description,
		&item.Owner, &item.Environment, &item.Criticality, &item.Repository, &item.SecretCount, &item.CreatedAt, &item.UpdatedAt)
	return item, mapError(err)
}

func (s *Store) PutApplication(ctx context.Context, id string, input model.Application) (model.Application, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return model.Application{}, ErrInvalid
	}
	if id == "" {
		var err error
		id, err = ids.UUID()
		if err != nil {
			return model.Application{}, err
		}
	}
	if input.Environment == "" {
		input.Environment = "DEV"
	}
	if input.Criticality == "" {
		input.Criticality = "Normal"
	}
	var item model.Application
	err := s.pool.QueryRow(ctx, `INSERT INTO applications
        (id,name,description,owner,environment,criticality,repository)
        VALUES($1,$2,$3,$4,$5,$6,$7)
        ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,
        owner=excluded.owner,environment=excluded.environment,criticality=excluded.criticality,
        repository=excluded.repository,updated_at=now()
        RETURNING id,name,description,owner,environment,criticality,repository,created_at,updated_at`,
		id, input.Name, input.Description, input.Owner, input.Environment, input.Criticality, input.Repository).Scan(
		&item.ID, &item.Name, &item.Description, &item.Owner, &item.Environment,
		&item.Criticality, &item.Repository, &item.CreatedAt, &item.UpdatedAt)
	return item, mapError(err)
}

func (s *Store) DeleteApplication(ctx context.Context, id string) error {
	command, err := s.pool.Exec(ctx, `DELETE FROM applications WHERE id=$1`, id)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListPolicies(ctx context.Context, limit, offset int) ([]model.Policy, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,name,description,rules,version,created_at,updated_at
        FROM policies ORDER BY name LIMIT $1 OFFSET $2`, boundedLimit(limit), max(offset, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Policy, 0)
	for rows.Next() {
		var item model.Policy
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Rules,
			&item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetPolicy(ctx context.Context, id string) (model.Policy, error) {
	var item model.Policy
	err := s.pool.QueryRow(ctx, `SELECT id,name,description,rules,version,created_at,updated_at
        FROM policies WHERE id=$1`, id).Scan(&item.ID, &item.Name, &item.Description,
		&item.Rules, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	return item, mapError(err)
}

func (s *Store) PutPolicy(ctx context.Context, id string, input model.Policy) (model.Policy, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || !json.Valid(input.Rules) {
		return model.Policy{}, ErrInvalid
	}
	var parsed model.PolicyRules
	if err := json.Unmarshal(input.Rules, &parsed); err != nil {
		return model.Policy{}, ErrInvalid
	}
	for _, rule := range parsed.Paths {
		if strings.TrimSpace(rule.Path) == "" {
			return model.Policy{}, ErrInvalid
		}
		for _, capability := range rule.Capabilities {
			switch capability {
			case "create", "read", "update", "delete", "list", "rotate", "encrypt", "decrypt":
			default:
				return model.Policy{}, fmt.Errorf("%w: 알 수 없는 capability %s", ErrInvalid, capability)
			}
		}
	}
	if id == "" {
		var err error
		id, err = ids.UUID()
		if err != nil {
			return model.Policy{}, err
		}
	}
	var item model.Policy
	err := s.pool.QueryRow(ctx, `INSERT INTO policies(id,name,description,rules)
        VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET name=excluded.name,
        description=excluded.description,rules=excluded.rules,version=policies.version+1,updated_at=now()
        RETURNING id,name,description,rules,version,created_at,updated_at`, id, input.Name,
		input.Description, input.Rules).Scan(&item.ID, &item.Name, &item.Description,
		&item.Rules, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	return item, mapError(err)
}

func (s *Store) DeletePolicy(ctx context.Context, id string) error {
	command, err := s.pool.Exec(ctx, `DELETE FROM policies WHERE id=$1`, id)
	if err != nil {
		return mapError(err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetPolicyUsers(ctx context.Context, policyID string, userIDs []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists string
	if err := tx.QueryRow(ctx, `SELECT id FROM policies WHERE id=$1 FOR SHARE`, policyID).Scan(&exists); err != nil {
		return mapError(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_policies WHERE policy_id=$1`, policyID); err != nil {
		return err
	}
	seen := make(map[string]bool, len(userIDs))
	for _, userID := range userIDs {
		if seen[userID] {
			continue
		}
		seen[userID] = true
		if _, err := tx.Exec(ctx, `INSERT INTO user_policies(user_id,policy_id) VALUES($1,$2)`, userID, policyID); err != nil {
			return mapError(err)
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) PolicyUsers(ctx context.Context, policyID string) ([]model.User, error) {
	var exists string
	if err := s.pool.QueryRow(ctx, `SELECT id FROM policies WHERE id=$1`, policyID).Scan(&exists); err != nil {
		return nil, mapError(err)
	}
	rows, err := s.pool.Query(ctx, userSelect+` JOIN user_policies up ON up.user_id=users.id
		WHERE up.policy_id=$1 ORDER BY username`, policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]model.User, 0)
	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) CanAccess(ctx context.Context, user model.User, secretPath, capability string) (bool, error) {
	return canAccessWith(ctx, s.pool, user, secretPath, capability)
}

type policyQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func canAccessWith(ctx context.Context, querier policyQuerier, user model.User, secretPath, capability string) (bool, error) {
	switch user.Role {
	case "admin", "manager":
		return true, nil
	case "auditor":
		return capability == "list", nil
	}
	rows, err := querier.Query(ctx, `SELECT p.rules FROM policies p
        JOIN user_policies up ON up.policy_id=p.id WHERE up.user_id=$1`, user.ID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return false, err
		}
		var rules model.PolicyRules
		if json.Unmarshal(raw, &rules) != nil {
			continue
		}
		for _, rule := range rules.Paths {
			if !pathMatches(rule.Path, secretPath) {
				continue
			}
			for _, allowed := range rule.Capabilities {
				if allowed == capability {
					return true, nil
				}
			}
		}
	}
	return false, rows.Err()
}

func pathMatches(pattern, value string) bool {
	pattern = strings.Trim(pattern, "/")
	value = strings.Trim(value, "/")
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == value
}
