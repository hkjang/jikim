package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/model"
	"github.com/jackc/pgx/v5"
)

var allowedSettingKeys = map[string]bool{
	"workflow":             true,
	"service":              true,
	"oidc":                 true,
	"oidc_client_secret":   true,
	"ai":                   true,
	"ai_api_key":           true,
	"security":             true,
	"notifications":        true,
	"notification_webhook": true,
}

func SensitiveSetting(key string) bool {
	return key == "oidc_client_secret" || key == "ai_api_key" || key == "notification_webhook"
}

func (s *Store) PutSetting(ctx context.Context, key string, value map[string]any, actorID string) error {
	if !allowedSettingKeys[key] {
		return fmt.Errorf("%w: 허용되지 않은 설정 키", ErrInvalid)
	}
	if err := validateSetting(key, value); err != nil {
		return err
	}
	if SensitiveSetting(key) {
		ciphertext, nonce, err := s.master.EncryptJSON(value, []byte("setting:"+key))
		if err != nil {
			return err
		}
		_, err = s.pool.Exec(ctx, `INSERT INTO settings(key,value_json,ciphertext,nonce,sensitive,updated_by,updated_at)
            VALUES($1,NULL,$2,$3,true,$4,now()) ON CONFLICT(key) DO UPDATE SET
            value_json=NULL,ciphertext=excluded.ciphertext,nonce=excluded.nonce,sensitive=true,
            updated_by=excluded.updated_by,updated_at=now()`, key, ciphertext, nonce, actorID)
		return mapError(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO settings(key,value_json,ciphertext,nonce,sensitive,updated_by,updated_at)
        VALUES($1,$2,NULL,NULL,false,$3,now()) ON CONFLICT(key) DO UPDATE SET
        value_json=excluded.value_json,ciphertext=NULL,nonce=NULL,sensitive=false,
        updated_by=excluded.updated_by,updated_at=now()`, key, encoded, actorID)
	return mapError(err)
}

func (s *Store) PatchSetting(ctx context.Context, key string, patch map[string]any, actorID string) error {
	if SensitiveSetting(key) {
		return s.PutSetting(ctx, key, patch, actorID)
	}
	merged := make(map[string]any)
	existing, err := s.GetSetting(ctx, key, false)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	for name, value := range existing.Value {
		merged[name] = value
	}
	for name, value := range patch {
		merged[name] = value
	}
	return s.PutSetting(ctx, key, merged, actorID)
}

func validateSetting(key string, value map[string]any) error {
	switch key {
	case "workflow":
		if raw, ok := value["approval_enabled"]; ok {
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("%w: approval_enabled는 boolean이어야 합니다", ErrInvalid)
			}
		}
		if raw, ok := value["reviewer_role"]; ok {
			role, ok := raw.(string)
			if !ok || (role != "admin" && role != "manager") {
				return fmt.Errorf("%w: reviewer_role은 admin 또는 manager여야 합니다", ErrInvalid)
			}
		}
		if rawTargets, ok := value["targets"]; ok {
			values, ok := rawTargets.([]any)
			if !ok {
				return fmt.Errorf("%w: targets는 문자열 배열이어야 합니다", ErrInvalid)
			}
			for _, raw := range values {
				target, ok := raw.(string)
				if !ok || (target != "secret_write" && target != "secret_delete") {
					return fmt.Errorf("%w: 지원하지 않는 승인 target", ErrInvalid)
				}
			}
		}
	case "oidc":
		if raw, ok := value["issuer_url"].(string); ok && raw != "" {
			u, err := url.Parse(raw)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return fmt.Errorf("%w: issuer_url이 올바르지 않습니다", ErrInvalid)
			}
		}
	case "ai":
		if raw, ok := value["base_url"].(string); ok && raw != "" {
			u, err := url.Parse(raw)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return fmt.Errorf("%w: AI base_url이 올바르지 않습니다", ErrInvalid)
			}
		}
		if raw, ok := numberAsInt(value["max_tokens"]); ok && (raw < 1 || raw > 262144) {
			return fmt.Errorf("%w: max_tokens는 1~262144여야 합니다", ErrInvalid)
		}
		if raw, ok := numberAsInt(value["timeout_seconds"]); ok && (raw < 10 || raw > 3600) {
			return fmt.Errorf("%w: timeout_seconds는 10~3600이어야 합니다", ErrInvalid)
		}
	case "security":
		if raw, ok := numberAsInt(value["session_timeout_minutes"]); ok && (raw < 5 || raw > 1440) {
			return fmt.Errorf("%w: session_timeout_minutes는 5~1440이어야 합니다", ErrInvalid)
		}
		if raw, ok := numberAsInt(value["password_min_length"]); ok && (raw < 12 || raw > 128) {
			return fmt.Errorf("%w: password_min_length는 12~128이어야 합니다", ErrInvalid)
		}
		if raw, ok := numberAsInt(value["audit_retention_days"]); ok && (raw < 1 || raw > 3650) {
			return fmt.Errorf("%w: audit_retention_days는 1~3650이어야 합니다", ErrInvalid)
		}
	}
	return nil
}

func numberAsInt(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case float64:
		return int(n), n == float64(int(n))
	case json.Number:
		v, err := n.Int64()
		return int(v), err == nil
	default:
		return 0, false
	}
}

func (s *Store) GetSetting(ctx context.Context, key string, reveal bool) (model.Setting, error) {
	var setting model.Setting
	var raw, ciphertext, nonce []byte
	err := s.pool.QueryRow(ctx, `SELECT key,value_json,ciphertext,nonce,sensitive,updated_at FROM settings WHERE key=$1`, key).Scan(
		&setting.Key, &raw, &ciphertext, &nonce, &setting.Sensitive, &setting.UpdatedAt)
	if err != nil {
		return model.Setting{}, mapError(err)
	}
	if setting.Sensitive {
		setting.Configured = len(ciphertext) > 0
		if reveal && setting.Configured {
			if err := s.master.DecryptJSON(ciphertext, nonce, []byte("setting:"+key), &setting.Value); err != nil {
				return model.Setting{}, err
			}
		}
	} else {
		setting.Configured = true
		if err := json.Unmarshal(raw, &setting.Value); err != nil {
			return model.Setting{}, err
		}
	}
	return setting, nil
}

func (s *Store) ListSettings(ctx context.Context) ([]model.Setting, error) {
	rows, err := s.pool.Query(ctx, `SELECT key,value_json,ciphertext,nonce,sensitive,updated_at FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := make([]model.Setting, 0)
	for rows.Next() {
		var item model.Setting
		var raw, ciphertext, nonce []byte
		if err := rows.Scan(&item.Key, &raw, &ciphertext, &nonce, &item.Sensitive, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if item.Sensitive {
			item.Configured = len(ciphertext) > 0
		} else {
			item.Configured = true
			_ = json.Unmarshal(raw, &item.Value)
		}
		settings = append(settings, item)
	}
	return settings, rows.Err()
}

type PublicSettings struct {
	ApprovalEnabled   bool   `json:"approval_enabled"`
	ReviewerRole      string `json:"reviewer_role"`
	OIDCEnabled       bool   `json:"oidc_enabled"`
	AIEnabled         bool   `json:"ai_enabled"`
	LocalLoginEnabled bool   `json:"local_login_enabled"`
	ServiceName       string `json:"service_name"`
	Version           string `json:"version"`
}

func (s *Store) PublicSettings(ctx context.Context, version string) (PublicSettings, error) {
	result := PublicSettings{ServiceName: "jikim", Version: version, LocalLoginEnabled: true, ReviewerRole: "manager"}
	workflow, err := s.GetSetting(ctx, "workflow", false)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return result, err
	}
	if raw, ok := workflow.Value["approval_enabled"].(bool); ok {
		result.ApprovalEnabled = raw
	}
	if raw, ok := workflow.Value["reviewer_role"].(string); ok && (raw == "admin" || raw == "manager") {
		result.ReviewerRole = raw
	}
	oidc, err := s.GetSetting(ctx, "oidc", false)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return result, err
	}
	if raw, ok := oidc.Value["enabled"].(bool); ok {
		result.OIDCEnabled = raw
	}
	ai, err := s.GetSetting(ctx, "ai", false)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return result, err
	}
	if raw, ok := ai.Value["enabled"].(bool); ok {
		result.AIEnabled = raw
	}
	service, err := s.GetSetting(ctx, "service", false)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return result, err
	}
	if raw, ok := service.Value["service_name"].(string); ok && strings.TrimSpace(raw) != "" {
		result.ServiceName = strings.TrimSpace(raw)
	}
	security, err := s.SecurityConfig(ctx)
	if err != nil {
		return result, err
	}
	result.LocalLoginEnabled = security.AllowLocalLogin
	return result, nil
}

type SecurityConfig struct {
	AllowLocalLogin       bool
	SessionTimeoutMinutes int
	RequirePasswordChange bool
	PasswordMinLength     int
	AuditRetentionDays    int
}

func (s *Store) SecurityConfig(ctx context.Context) (SecurityConfig, error) {
	result := SecurityConfig{AllowLocalLogin: true, SessionTimeoutMinutes: 720, PasswordMinLength: 12, AuditRetentionDays: 180}
	setting, err := s.GetSetting(ctx, "security", false)
	if errors.Is(err, ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if value, ok := setting.Value["allow_local_login"].(bool); ok {
		result.AllowLocalLogin = value
	}
	if value, ok := numberAsInt(setting.Value["session_timeout_minutes"]); ok {
		result.SessionTimeoutMinutes = value
	}
	if result.SessionTimeoutMinutes < 5 {
		result.SessionTimeoutMinutes = 5
	}
	if result.SessionTimeoutMinutes > 1440 {
		result.SessionTimeoutMinutes = 1440
	}
	result.RequirePasswordChange, _ = setting.Value["require_password_change"].(bool)
	if value, ok := numberAsInt(setting.Value["password_min_length"]); ok {
		result.PasswordMinLength = value
	}
	if result.PasswordMinLength < 12 {
		result.PasswordMinLength = 12
	}
	if result.PasswordMinLength > 128 {
		result.PasswordMinLength = 128
	}
	if value, ok := numberAsInt(setting.Value["audit_retention_days"]); ok {
		result.AuditRetentionDays = value
	}
	if result.AuditRetentionDays < 1 {
		result.AuditRetentionDays = 1
	}
	if result.AuditRetentionDays > 3650 {
		result.AuditRetentionDays = 3650
	}
	return result, nil
}

type AIConfig struct {
	Enabled        bool
	BaseURL        string
	Model          string
	MaxTokens      int
	Temperature    float64
	APIKey         string
	TimeoutSeconds int
}

func (s *Store) AIConfig(ctx context.Context) (AIConfig, error) {
	cfg := AIConfig{MaxTokens: 4096, Temperature: 0.2, TimeoutSeconds: 600}
	setting, err := s.GetSetting(ctx, "ai", false)
	if errors.Is(err, ErrNotFound) {
		return cfg, nil
	}
	if err != nil {
		return AIConfig{}, err
	}
	cfg.Enabled, _ = setting.Value["enabled"].(bool)
	cfg.BaseURL, _ = setting.Value["base_url"].(string)
	cfg.Model, _ = setting.Value["model"].(string)
	if n, ok := numberAsInt(setting.Value["max_tokens"]); ok {
		cfg.MaxTokens = n
	}
	if n, ok := setting.Value["temperature"].(float64); ok {
		cfg.Temperature = n
	}
	if n, ok := numberAsInt(setting.Value["timeout_seconds"]); ok {
		cfg.TimeoutSeconds = n
	}
	secret, err := s.GetSetting(ctx, "ai_api_key", true)
	if err == nil {
		cfg.APIKey, _ = secret.Value["value"].(string)
	} else if !errors.Is(err, ErrNotFound) {
		return AIConfig{}, err
	}
	if cfg.MaxTokens < 1 {
		cfg.MaxTokens = 1
	}
	if cfg.MaxTokens > 262144 {
		cfg.MaxTokens = 262144
	}
	if cfg.TimeoutSeconds < 10 {
		cfg.TimeoutSeconds = 10
	}
	if cfg.TimeoutSeconds > 3600 {
		cfg.TimeoutSeconds = 3600
	}
	return cfg, nil
}

type OIDCConfig struct {
	Enabled       bool
	IssuerURL     string
	ClientID      string
	ClientSecret  string
	Scopes        []string
	UsernameClaim string
	GroupClaim    string
	RoleClaim     string
	RedirectURL   string
}

func (s *Store) OIDCConfig(ctx context.Context) (OIDCConfig, error) {
	setting, err := s.GetSetting(ctx, "oidc", false)
	if err != nil {
		return OIDCConfig{}, err
	}
	cfg := OIDCConfig{}
	cfg.Enabled, _ = setting.Value["enabled"].(bool)
	cfg.IssuerURL, _ = setting.Value["issuer_url"].(string)
	cfg.ClientID, _ = setting.Value["client_id"].(string)
	cfg.UsernameClaim, _ = setting.Value["username_claim"].(string)
	cfg.GroupClaim, _ = setting.Value["group_claim"].(string)
	cfg.RoleClaim, _ = setting.Value["role_claim"].(string)
	cfg.RedirectURL, _ = setting.Value["redirect_url"].(string)
	if raw, ok := setting.Value["scopes"].([]any); ok {
		for _, scope := range raw {
			if value, ok := scope.(string); ok {
				cfg.Scopes = append(cfg.Scopes, value)
			}
		}
	}
	secret, err := s.GetSetting(ctx, "oidc_client_secret", true)
	if err == nil {
		cfg.ClientSecret, _ = secret.Value["value"].(string)
	} else if !errors.Is(err, ErrNotFound) {
		return OIDCConfig{}, err
	}
	return cfg, nil
}

func settingUpdatedAt() time.Time { return time.Now().UTC() }

var _ = pgx.ErrNoRows
