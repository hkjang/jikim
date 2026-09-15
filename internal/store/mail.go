package store

import (
	"context"
	"errors"
	"strings"

	"github.com/hkjang/jikim/internal/mail"
)

// MailConfig reads the relay configuration. A missing row is the
// switched-off default; the password comes from its own encrypted row and is
// never part of what the settings API returns.
func (s *Store) MailConfig(ctx context.Context) (mail.Config, error) {
	setting, err := s.GetSetting(ctx, mail.SettingKey, false)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return mail.Config{}, err
	}
	password := ""
	secret, err := s.GetSetting(ctx, mail.PasswordSettingKey, true)
	if err == nil {
		password, _ = secret.Value["value"].(string)
	} else if !errors.Is(err, ErrNotFound) {
		return mail.Config{}, err
	}
	return mail.ReadConfig(setting.Value, password), nil
}

// UserEmails is the one directory lookup mail borrows: account id to
// address. Inactive accounts and accounts without an address are left out,
// so the caller sends nothing to them.
func (s *Store) UserEmails(ctx context.Context, ids []string) (map[string]string, error) {
	addresses := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return addresses, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id, email FROM users WHERE id = ANY($1) AND active = true AND email <> ''`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		addresses[id] = strings.TrimSpace(email)
	}
	return addresses, rows.Err()
}

// ActiveUserIDsByRole lists who should hear about a pending approval: the
// active accounts holding one of the reviewer roles.
func (s *Store) ActiveUserIDsByRole(ctx context.Context, roles []string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM users WHERE role = ANY($1) AND active = true ORDER BY username`, roles)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) RecordMailDelivery(ctx context.Context, delivery mail.Delivery) error {
	var actorID *string
	if strings.TrimSpace(delivery.ActorID) != "" {
		actorID = &delivery.ActorID
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO mail_deliveries
		(id,event,recipient,subject,resource,actor_id,status,attempts,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,'queued',0,$7,$7)`,
		delivery.ID, delivery.Event, delivery.Recipient, delivery.Subject, delivery.Resource, actorID, delivery.CreatedAt)
	return mapError(err)
}

func (s *Store) CompleteMailDelivery(ctx context.Context, id string, attempts int, cause error) error {
	status, message := mail.StatusSent, ""
	if cause != nil {
		status, message = mail.StatusFailed, strings.TrimSpace(cause.Error())
		if len(message) > 500 {
			message = message[:500]
		}
	}
	command, err := s.pool.Exec(ctx, `UPDATE mail_deliveries SET status=$2,
		attempts=GREATEST(attempts,$3),error_message=$4,updated_at=now() WHERE id=$1`,
		id, status, max(attempts, 1), message)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListMailDeliveries lists what went out, newest first.
func (s *Store) ListMailDeliveries(ctx context.Context, status string, limit, offset int) ([]mail.Delivery, error) {
	if status != "" && status != mail.StatusQueued && status != mail.StatusSent && status != mail.StatusFailed {
		return nil, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT id,event,recipient,subject,resource,coalesce(actor_id,''),status,
		attempts,error_message,created_at,updated_at FROM mail_deliveries
		WHERE ($1='' OR status=$1) ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`,
		status, boundedLimit(limit), max(offset, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]mail.Delivery, 0)
	for rows.Next() {
		var item mail.Delivery
		if err := rows.Scan(&item.ID, &item.Event, &item.Recipient, &item.Subject, &item.Resource, &item.ActorID,
			&item.Status, &item.Attempts, &item.ErrorMessage, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
