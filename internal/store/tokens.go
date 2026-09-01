package store

import (
	"context"
	"time"
)

type TokenView struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Username  string     `json:"username"`
	Kind      string     `json:"kind"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	LastSeen  time.Time  `json:"last_seen_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func (s *Store) ListTokens(ctx context.Context, actorID string, all bool, limit, offset int) ([]TokenView, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.user_id,u.username,s.kind,s.name,s.created_at,
        s.expires_at,s.last_seen_at,s.revoked_at FROM sessions s JOIN users u ON u.id=s.user_id
        WHERE ($1=true OR s.user_id=$2) ORDER BY s.created_at DESC LIMIT $3 OFFSET $4`,
		all, actorID, boundedLimit(limit), max(offset, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TokenView, 0)
	for rows.Next() {
		var item TokenView
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.Kind, &item.Name,
			&item.CreatedAt, &item.ExpiresAt, &item.LastSeen, &item.RevokedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RevokeSessionByID(ctx context.Context, id, actorID string, all bool) error {
	command, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now()
        WHERE id=$1 AND revoked_at IS NULL AND ($2=true OR user_id=$3)`, id, all, actorID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
