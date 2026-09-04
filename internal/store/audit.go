package store

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/hkjang/jikim/internal/model"
)

func (s *Store) RecordAudit(ctx context.Context, event model.AuditEvent) error {
	if event.Details == nil {
		event.Details = map[string]any{}
	}
	details, _ := json.Marshal(event.Details)
	_, err := s.pool.Exec(ctx, `INSERT INTO audit_logs
        (request_id,user_id,username,action,resource,method,path,status_code,success,remote_ip,user_agent,details)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, event.RequestID, event.UserID,
		event.Username, event.Action, event.Resource, event.Method, event.Path, event.StatusCode,
		event.Success, event.RemoteIP, event.UserAgent, details)
	return err
}

func (s *Store) ListAudit(ctx context.Context, query, result string, limit, offset int) ([]model.AuditEvent, error) {
	search := "%" + escapeLike(strings.ToLower(strings.TrimSpace(query))) + "%"
	rows, err := s.pool.Query(ctx, `SELECT id,request_id,user_id,username,action,resource,method,path,
		status_code,success,remote_ip,user_agent,details,created_at FROM audit_logs
		WHERE ($1='' OR lower(username) LIKE $2 OR lower(action) LIKE $2 OR lower(resource) LIKE $2)
		AND ($3='' OR ($3='success' AND success=true) OR ($3='denied' AND status_code IN (401,403))
		OR ($3='failed' AND success=false AND status_code NOT IN (401,403)))
		ORDER BY created_at DESC LIMIT $4 OFFSET $5`, strings.TrimSpace(query), search, result, boundedLimit(limit), max(offset, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AuditEvent, 0)
	for rows.Next() {
		var item model.AuditEvent
		var details []byte
		if err := rows.Scan(&item.ID, &item.RequestID, &item.UserID, &item.Username, &item.Action,
			&item.Resource, &item.Method, &item.Path, &item.StatusCode, &item.Success,
			&item.RemoteIP, &item.UserAgent, &details, &item.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(details, &item.Details)
		item.EventID = strconv.FormatInt(item.ID, 10)
		item.Actor = item.Username
		item.IP = item.RemoteIP
		item.Result = "failed"
		if item.StatusCode == 401 || item.StatusCode == 403 {
			item.Result = "denied"
		} else if item.Success {
			item.Result = "success"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Dashboard(ctx context.Context) (model.Dashboard, error) {
	var dashboard model.Dashboard
	err := s.pool.QueryRow(ctx, `SELECT
        (SELECT count(*) FROM secrets WHERE deleted_at IS NULL),
        (SELECT count(*) FROM applications),
		(SELECT count(*) FROM users WHERE active=true),
		(SELECT count(*) FROM policies),
		((SELECT count(*) FROM user_keys WHERE active=true) + (SELECT count(*) FROM transit_keys)),
		(SELECT count(*) FROM approval_requests WHERE status='pending'),
        (SELECT count(*) FROM secrets WHERE deleted_at IS NULL AND risk_score>=61),
		(SELECT count(*) FROM secrets WHERE deleted_at IS NULL AND risk_score<31),
		(SELECT count(*) FROM secrets WHERE deleted_at IS NULL AND risk_score BETWEEN 31 AND 60),
		(SELECT (100-COALESCE(round(avg(risk_score)),0))::int FROM secrets WHERE deleted_at IS NULL)`).Scan(
		&dashboard.Secrets, &dashboard.Applications, &dashboard.Users, &dashboard.Policies,
		&dashboard.Keys, &dashboard.PendingApprovals, &dashboard.HighRiskSecrets,
		&dashboard.HealthySecrets, &dashboard.AttentionSecrets, &dashboard.SecurityScore)
	if err != nil {
		return model.Dashboard{}, err
	}
	recent, err := s.ListAudit(ctx, "", "", 10, 0)
	if err != nil {
		return model.Dashboard{}, err
	}
	dashboard.RecentAudit = recent
	return dashboard, nil
}

// UserDashboard returns only inventory that the user can discover with the
// list capability. Global user, policy and key counts must not leak through a
// dashboard endpoint that is available to every authenticated account.
func (s *Store) UserDashboard(ctx context.Context, userID string) (model.Dashboard, error) {
	var dashboard model.Dashboard
	err := s.pool.QueryRow(ctx, `WITH visible AS (
		SELECT s.risk_score,s.application_id FROM secrets s
		WHERE s.deleted_at IS NULL AND EXISTS (
			SELECT 1 FROM user_policies up JOIN policies p ON p.id=up.policy_id
			CROSS JOIN LATERAL jsonb_array_elements(
				CASE WHEN jsonb_typeof(p.rules->'paths')='array' THEN p.rules->'paths' ELSE '[]'::jsonb END
			) AS policy_rule
			WHERE up.user_id=$1
			AND COALESCE(policy_rule->'capabilities','[]'::jsonb) ? 'list'
			AND (
				trim(both '/' from policy_rule->>'path')='*'
				OR (right(trim(both '/' from policy_rule->>'path'),1)='*'
					AND left(s.path,length(trim(both '/' from policy_rule->>'path'))-1)=
						left(trim(both '/' from policy_rule->>'path'),length(trim(both '/' from policy_rule->>'path'))-1))
				OR s.path=trim(both '/' from policy_rule->>'path')
			)
		)
	)
	SELECT
		(SELECT count(*) FROM visible),
		(SELECT count(DISTINCT application_id) FROM visible WHERE application_id IS NOT NULL),
		1,
		(SELECT count(*) FROM user_policies WHERE user_id=$1),
		(SELECT count(*) FROM user_keys WHERE user_id=$1 AND active=true),
		0,
		(SELECT count(*) FROM visible WHERE risk_score>=61),
		(SELECT count(*) FROM visible WHERE risk_score<31),
		(SELECT count(*) FROM visible WHERE risk_score BETWEEN 31 AND 60),
		(SELECT (100-COALESCE(round(avg(risk_score)),0))::int FROM visible)`, userID).Scan(
		&dashboard.Secrets, &dashboard.Applications, &dashboard.Users, &dashboard.Policies,
		&dashboard.Keys, &dashboard.PendingApprovals, &dashboard.HighRiskSecrets,
		&dashboard.HealthySecrets, &dashboard.AttentionSecrets, &dashboard.SecurityScore)
	if err != nil {
		return model.Dashboard{}, err
	}
	dashboard.RecentAudit = []model.AuditEvent{}
	return dashboard, nil
}

func (s *Store) CountPendingApprovalsForUser(ctx context.Context, userID string) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM approval_requests
		WHERE status='pending' AND requester_id=$1`, userID).Scan(&count)
	return count, err
}
