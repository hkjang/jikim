package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type WebhookDelivery struct {
	ID             string     `json:"id"`
	EventType      string     `json:"event_type"`
	Resource       string     `json:"resource,omitempty"`
	ActorID        *string    `json:"actor_id,omitempty"`
	RequestID      string     `json:"request_id,omitempty"`
	Status         string     `json:"status"`
	AttemptCount   int        `json:"attempt_count"`
	ResponseStatus *int       `json:"response_status,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	LastAttemptAt  *time.Time `json:"last_attempt_at,omitempty"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	Payload        []byte     `json:"-"`
}

func (s *Store) CreateWebhookDelivery(ctx context.Context, id, eventType, resource string, actorID *string, requestID string, payload any) (WebhookDelivery, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return WebhookDelivery{}, err
	}
	var item WebhookDelivery
	err = s.pool.QueryRow(ctx, `INSERT INTO webhook_deliveries
		(id,event_type,resource,actor_id,request_id,payload)
		VALUES($1,$2,$3,$4,$5,$6)
		RETURNING id,event_type,resource,actor_id,request_id,status,attempt_count,
		response_status,last_error,created_at,last_attempt_at,delivered_at,payload`,
		id, eventType, resource, actorID, requestID, raw).Scan(&item.ID, &item.EventType,
		&item.Resource, &item.ActorID, &item.RequestID, &item.Status, &item.AttemptCount,
		&item.ResponseStatus, &item.LastError, &item.CreatedAt, &item.LastAttemptAt,
		&item.DeliveredAt, &item.Payload)
	return item, mapError(err)
}

func (s *Store) WebhookDelivery(ctx context.Context, id string) (WebhookDelivery, error) {
	var item WebhookDelivery
	err := s.pool.QueryRow(ctx, `SELECT id,event_type,resource,actor_id,request_id,status,
		attempt_count,response_status,last_error,created_at,last_attempt_at,delivered_at,payload
		FROM webhook_deliveries WHERE id=$1`, id).Scan(&item.ID, &item.EventType,
		&item.Resource, &item.ActorID, &item.RequestID, &item.Status, &item.AttemptCount,
		&item.ResponseStatus, &item.LastError, &item.CreatedAt, &item.LastAttemptAt,
		&item.DeliveredAt, &item.Payload)
	return item, mapError(err)
}

func (s *Store) CompleteWebhookDelivery(ctx context.Context, id string, statusCode int, deliveryErr error) error {
	status := "delivered"
	lastError := ""
	if deliveryErr != nil {
		status = "failed"
		lastError = strings.TrimSpace(deliveryErr.Error())
		if len(lastError) > 500 {
			lastError = lastError[:500]
		}
	}
	var responseStatus *int
	if statusCode > 0 {
		responseStatus = &statusCode
	}
	command, err := s.pool.Exec(ctx, `UPDATE webhook_deliveries SET status=$2,
		attempt_count=attempt_count+1,response_status=$3,last_error=$4,last_attempt_at=now(),
		delivered_at=CASE WHEN $2='delivered' THEN now() ELSE delivered_at END WHERE id=$1`,
		id, status, responseStatus, lastError)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListWebhookDeliveries(ctx context.Context, status string, limit, offset int) ([]WebhookDelivery, error) {
	if status != "" && status != "pending" && status != "delivered" && status != "failed" {
		return nil, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT id,event_type,resource,actor_id,request_id,status,
		attempt_count,response_status,last_error,created_at,last_attempt_at,delivered_at
		FROM webhook_deliveries WHERE ($1='' OR status=$1)
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`, status, boundedLimit(limit), max(offset, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]WebhookDelivery, 0)
	for rows.Next() {
		var item WebhookDelivery
		if err := rows.Scan(&item.ID, &item.EventType, &item.Resource, &item.ActorID,
			&item.RequestID, &item.Status, &item.AttemptCount, &item.ResponseStatus,
			&item.LastError, &item.CreatedAt, &item.LastAttemptAt, &item.DeliveredAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
