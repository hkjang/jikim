package model

import (
	"encoding/json"
	"time"
)

type User struct {
	ID                 string    `json:"id"`
	Username           string    `json:"username"`
	DisplayName        string    `json:"display_name"`
	Email              string    `json:"email,omitempty"`
	Role               string    `json:"role"`
	Active             bool      `json:"active"`
	PersonalKeyVersion int       `json:"personal_key_version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Session struct {
	ID        string    `json:"id"`
	User      User      `json:"user"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Application struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Owner       string    `json:"owner"`
	Environment string    `json:"environment"`
	Criticality string    `json:"criticality"`
	Repository  string    `json:"repository,omitempty"`
	SecretCount int64     `json:"secret_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Policy struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Rules       json.RawMessage `json:"rules"`
	Version     int             `json:"version"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type PathRule struct {
	Path         string   `json:"path"`
	Capabilities []string `json:"capabilities"`
}

type PolicyRules struct {
	Paths []PathRule `json:"paths"`
}

type Secret struct {
	ID             string         `json:"id"`
	Path           string         `json:"path"`
	Description    string         `json:"description"`
	ApplicationID  *string        `json:"application_id,omitempty"`
	OwnerUserID    *string        `json:"owner_user_id,omitempty"`
	Tags           []string       `json:"tags"`
	RiskScore      int            `json:"risk_score"`
	CurrentVersion int            `json:"current_version"`
	Version        int            `json:"version,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	Application    string         `json:"application,omitempty"`
	Environment    string         `json:"environment,omitempty"`
	Owner          string         `json:"owner,omitempty"`
	Status         string         `json:"status"`
	CreatedBy      string         `json:"created_by"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type SecretWrite struct {
	Path          string         `json:"path"`
	Description   string         `json:"description"`
	ApplicationID *string        `json:"application_id,omitempty"`
	OwnerUserID   *string        `json:"owner_user_id,omitempty"`
	Tags          []string       `json:"tags"`
	Data          map[string]any `json:"data"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type Approval struct {
	ID          string          `json:"id"`
	Action      string          `json:"action"`
	Resource    string          `json:"resource"`
	RequesterID string          `json:"requester_id"`
	ApproverID  *string         `json:"approver_id,omitempty"`
	Status      string          `json:"status"`
	Comment     string          `json:"comment,omitempty"`
	Payload     json.RawMessage `json:"-"`
	CreatedAt   time.Time       `json:"created_at"`
	ResolvedAt  *time.Time      `json:"resolved_at,omitempty"`
}

type AuditEvent struct {
	ID         int64          `json:"-"`
	EventID    string         `json:"id"`
	RequestID  string         `json:"request_id"`
	UserID     *string        `json:"user_id,omitempty"`
	Username   string         `json:"username,omitempty"`
	Actor      string         `json:"actor,omitempty"`
	Action     string         `json:"action"`
	Resource   string         `json:"resource"`
	Method     string         `json:"method"`
	Path       string         `json:"path"`
	StatusCode int            `json:"status_code"`
	Success    bool           `json:"success"`
	Result     string         `json:"result"`
	RemoteIP   string         `json:"remote_ip"`
	IP         string         `json:"ip"`
	UserAgent  string         `json:"user_agent"`
	Details    map[string]any `json:"details,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type Setting struct {
	Key        string         `json:"key"`
	Value      map[string]any `json:"value,omitempty"`
	Sensitive  bool           `json:"sensitive"`
	Configured bool           `json:"configured"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type Dashboard struct {
	Secrets          int64        `json:"secrets"`
	Applications     int64        `json:"applications"`
	Users            int64        `json:"users"`
	Policies         int64        `json:"policies"`
	Keys             int64        `json:"keys"`
	PendingApprovals int64        `json:"pending_approvals"`
	HighRiskSecrets  int64        `json:"high_risk_secrets"`
	RecentAudit      []AuditEvent `json:"recent_audit"`
}

type UserKey struct {
	ID          string         `json:"id"`
	UserID      string         `json:"user_id"`
	Version     int            `json:"version"`
	Permissions map[string]any `json:"permissions"`
	Active      bool           `json:"active"`
	CreatedAt   time.Time      `json:"created_at"`
}
