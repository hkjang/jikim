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
	search := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
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
        (SELECT count(*) FROM secrets WHERE deleted_at IS NULL AND risk_score>=61)`).Scan(
		&dashboard.Secrets, &dashboard.Applications, &dashboard.Users, &dashboard.Policies,
		&dashboard.Keys, &dashboard.PendingApprovals, &dashboard.HighRiskSecrets)
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
