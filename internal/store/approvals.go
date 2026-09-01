package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hkjang/jikim/internal/ids"
	"github.com/hkjang/jikim/internal/model"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ApprovalEnabled(ctx context.Context) (bool, error) {
	cfg, err := s.ApprovalConfig(ctx)
	return cfg.Enabled, err
}

type ApprovalConfig struct {
	Enabled           bool     `json:"approval_enabled"`
	Targets           []string `json:"targets"`
	ReviewerRole      string   `json:"reviewer_role"`
	FourEyes          bool     `json:"four_eyes"`
	RequiredApprovals int      `json:"required_approvals"`
}

func (s *Store) ApprovalConfig(ctx context.Context) (ApprovalConfig, error) {
	result := ApprovalConfig{Targets: []string{"secret_write", "secret_delete"}, ReviewerRole: "manager", FourEyes: true, RequiredApprovals: 1}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value_json FROM settings WHERE key='workflow' AND sensitive=false`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	result.FourEyes = true
	result.RequiredApprovals = 1
	if result.ReviewerRole != "admin" && result.ReviewerRole != "manager" {
		result.ReviewerRole = "manager"
	}
	return result, nil
}

func (s *Store) ApprovalRequired(ctx context.Context, target string) (bool, error) {
	cfg, err := s.ApprovalConfig(ctx)
	if err != nil || !cfg.Enabled {
		return false, err
	}
	for _, configured := range cfg.Targets {
		if configured == target {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) CreateApproval(ctx context.Context, action, resource, requesterID string, payload any) (model.Approval, error) {
	id, err := ids.UUID()
	if err != nil {
		return model.Approval{}, err
	}
	ciphertext, nonce, err := s.master.EncryptJSON(payload, []byte("approval:"+id))
	if err != nil {
		return model.Approval{}, err
	}
	var approval model.Approval
	err = s.pool.QueryRow(ctx, `INSERT INTO approval_requests
        (id,action,resource,requester_id,payload_ciphertext,payload_nonce)
        VALUES($1,$2,$3,$4,$5,$6)
        RETURNING id,action,resource,requester_id,approver_id,status,comment,created_at,resolved_at`,
		id, action, resource, requesterID, ciphertext, nonce).Scan(&approval.ID, &approval.Action,
		&approval.Resource, &approval.RequesterID, &approval.ApproverID, &approval.Status,
		&approval.Comment, &approval.CreatedAt, &approval.ResolvedAt)
	return approval, mapError(err)
}

func (s *Store) ListApprovals(ctx context.Context, status string, limit, offset int) ([]model.Approval, error) {
	return s.ListApprovalsFor(ctx, status, "", true, limit, offset)
}

func (s *Store) ListApprovalsFor(ctx context.Context, status, requesterID string, all bool, limit, offset int) ([]model.Approval, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,action,resource,requester_id,approver_id,status,comment,created_at,resolved_at
		FROM approval_requests WHERE ($1='' OR status=$1) AND ($2=true OR requester_id=$3)
		ORDER BY created_at DESC LIMIT $4 OFFSET $5`,
		status, all, requesterID, boundedLimit(limit), max(offset, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Approval, 0)
	for rows.Next() {
		var item model.Approval
		if err := rows.Scan(&item.ID, &item.Action, &item.Resource, &item.RequesterID,
			&item.ApproverID, &item.Status, &item.Comment, &item.CreatedAt, &item.ResolvedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ResolveApproval(ctx context.Context, id, approverID, decision, comment string) (model.Approval, error) {
	if decision != "approved" && decision != "rejected" {
		return model.Approval{}, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return model.Approval{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var approval model.Approval
	var ciphertext, nonce []byte
	err = tx.QueryRow(ctx, `SELECT id,action,resource,requester_id,approver_id,status,comment,
        payload_ciphertext,payload_nonce,created_at,resolved_at FROM approval_requests
        WHERE id=$1 FOR UPDATE`, id).Scan(&approval.ID, &approval.Action, &approval.Resource,
		&approval.RequesterID, &approval.ApproverID, &approval.Status, &approval.Comment,
		&ciphertext, &nonce, &approval.CreatedAt, &approval.ResolvedAt)
	if err != nil {
		return model.Approval{}, mapError(err)
	}
	if approval.Status != "pending" {
		return model.Approval{}, fmt.Errorf("%w: 이미 처리된 요청입니다", ErrConflict)
	}
	if approval.RequesterID == approverID {
		return model.Approval{}, ErrRequesterMatch
	}
	if decision == "approved" {
		requester, requesterErr := scanUser(tx.QueryRow(ctx, userSelect+` WHERE id=$1`, approval.RequesterID))
		if requesterErr != nil || !requester.Active {
			return model.Approval{}, ErrForbidden
		}
		capability := ""
		switch approval.Action {
		case "secret.put":
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM secrets WHERE path=$1 AND deleted_at IS NULL)`, approval.Resource).Scan(&exists); err != nil {
				return model.Approval{}, err
			}
			capability = "create"
			if exists {
				capability = "update"
			}
		case "secret.delete":
			capability = "delete"
		default:
			return model.Approval{}, fmt.Errorf("%w: 지원하지 않는 승인 작업", ErrInvalid)
		}
		allowed, accessErr := canAccessWith(ctx, tx, requester, approval.Resource, capability)
		if accessErr != nil {
			return model.Approval{}, accessErr
		}
		if !allowed {
			return model.Approval{}, ErrForbidden
		}
		switch approval.Action {
		case "secret.put":
			var input model.SecretWrite
			if err := s.master.DecryptJSON(ciphertext, nonce, []byte("approval:"+id), &input); err != nil {
				return model.Approval{}, err
			}
			if _, err := s.putSecretTx(ctx, tx, input, approval.RequesterID); err != nil {
				return model.Approval{}, err
			}
		case "secret.delete":
			command, err := tx.Exec(ctx, `UPDATE secrets SET deleted_at=now(),updated_at=now() WHERE path=$1 AND deleted_at IS NULL`, approval.Resource)
			if err != nil {
				return model.Approval{}, err
			}
			if command.RowsAffected() == 0 {
				return model.Approval{}, ErrNotFound
			}
		}
	}
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `UPDATE approval_requests SET status=$2,approver_id=$3,comment=$4,resolved_at=$5 WHERE id=$1`,
		id, decision, approverID, comment, now)
	if err != nil {
		return model.Approval{}, err
	}
	approval.Status = decision
	approval.ApproverID = &approverID
	approval.Comment = comment
	approval.ResolvedAt = &now
	if err := tx.Commit(ctx); err != nil {
		return model.Approval{}, err
	}
	return approval, nil
}

func approvalPayloadJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}
