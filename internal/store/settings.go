package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/model"
	"github.com/jackc/pgx/v5"
)

var allowedSettingKeys = map[string]bool{
	"workflow":                    true,
	"service":                     true,
	"oidc":                        true,
	"oidc_client_secret":          true,
	"ai":                          true,
	"ai_api_key":                  true,
	"security":                    true,
	"notifications":               true,
	"notification_webhook":        true,
	"notification_webhook_secret": true,
}

func SensitiveSetting(key string) bool {
	return key == "oidc_client_secret" || key == "ai_api_key" ||
		key == "notification_webhook" || key == "notification_webhook_secret"
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

func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	if !SensitiveSetting(key) {
		return fmt.Errorf("%w: 민감 설정만 삭제할 수 있습니다", ErrInvalid)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM settings WHERE key=$1`, key)
	return err
}

func (s *Store) SettingConfigured(ctx context.Context, key string) (bool, error) {
	if !SensitiveSetting(key) {
		return false, fmt.Errorf("%w: 민감 설정 키가 아닙니다", ErrInvalid)
	}
	var configured bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM settings WHERE key=$1 AND sensitive=true
		AND ciphertext IS NOT NULL AND octet_length(ciphertext)>0)`, key).Scan(&configured)
	return configured, err
}

func ValidateSetting(key string, value map[string]any) error {
	if !allowedSettingKeys[key] || SensitiveSetting(key) {
		return fmt.Errorf("%w: 허용되지 않은 일반 설정 키", ErrInvalid)
	}
	return validateSetting(key, value)
}

func ValidateWebhookURL(raw string, allowInsecure bool) error {
	if err := validateIntegrationURL(raw, allowInsecure, true); err != nil {
		return fmt.Errorf("%w: webhook URL: %v", ErrInvalid, err)
	}
	return nil
}

func validateSetting(key string, value map[string]any) error {
	switch key {
	case "oidc_client_secret", "ai_api_key":
		secret, ok := value["value"].(string)
		if !ok || strings.TrimSpace(secret) == "" {
			return fmt.Errorf("%w: 민감 설정 값이 비어 있습니다", ErrInvalid)
		}
	case "notification_webhook":
		webhook, ok := value["value"].(string)
		if !ok || strings.TrimSpace(webhook) == "" {
			return fmt.Errorf("%w: Webhook URL이 비어 있습니다", ErrInvalid)
		}
	case "notification_webhook_secret":
		secret, ok := value["value"].(string)
		if !ok || len(secret) < 32 {
			return fmt.Errorf("%w: Webhook 서명 키는 32자 이상이어야 합니다", ErrInvalid)
		}
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
		enabled, err := optionalBool(value, "enabled")
		if err != nil {
			return err
		}
		allowInsecure, err := optionalBool(value, "allow_insecure_http")
		if err != nil {
			return err
		}
		issuer, _ := value["issuer_url"].(string)
		clientID, _ := value["client_id"].(string)
		redirectURL, _ := value["redirect_url"].(string)
		if enabled && (strings.TrimSpace(issuer) == "" || strings.TrimSpace(clientID) == "" || strings.TrimSpace(redirectURL) == "") {
			return fmt.Errorf("%w: OIDC를 사용하려면 issuer_url, client_id, redirect_url이 필요합니다", ErrInvalid)
		}
		if issuer != "" {
			if err := validateIntegrationURL(issuer, allowInsecure, false); err != nil {
				return fmt.Errorf("%w: issuer_url: %v", ErrInvalid, err)
			}
		}
		if redirectURL != "" {
			if err := validateIntegrationURL(redirectURL, allowInsecure, false); err != nil {
				return fmt.Errorf("%w: redirect_url: %v", ErrInvalid, err)
			}
		}
		if rawScopes, ok := value["scopes"]; ok {
			scopes, ok := rawScopes.([]any)
			if !ok {
				return fmt.Errorf("%w: scopes는 문자열 배열이어야 합니다", ErrInvalid)
			}
			hasOpenID := false
			for _, raw := range scopes {
				scope, ok := raw.(string)
				if !ok || strings.TrimSpace(scope) == "" {
					return fmt.Errorf("%w: scopes는 빈 값이 없는 문자열 배열이어야 합니다", ErrInvalid)
				}
				hasOpenID = hasOpenID || scope == "openid"
			}
			if enabled && !hasOpenID {
				return fmt.Errorf("%w: OIDC scopes에는 openid가 필요합니다", ErrInvalid)
			}
		}
	case "ai":
		enabled, err := optionalBool(value, "enabled")
		if err != nil {
			return err
		}
		allowInsecure, err := optionalBool(value, "allow_insecure_http")
		if err != nil {
			return err
		}
		baseURL, _ := value["base_url"].(string)
		modelName, _ := value["model"].(string)
		if enabled && (strings.TrimSpace(baseURL) == "" || strings.TrimSpace(modelName) == "") {
			return fmt.Errorf("%w: AI를 사용하려면 base_url과 model이 필요합니다", ErrInvalid)
		}
		if baseURL != "" {
			if err := validateIntegrationURL(baseURL, allowInsecure, true); err != nil {
				return fmt.Errorf("%w: AI base_url: %v", ErrInvalid, err)
			}
		}
		if err := optionalIntInRange(value, "max_tokens", 1, 262144); err != nil {
			return err
		}
		if err := optionalIntInRange(value, "timeout_seconds", 10, 3600); err != nil {
			return err
		}
		if raw, ok := value["temperature"]; ok {
			temperature, ok := raw.(float64)
			if !ok || temperature < 0 || temperature > 2 {
				return fmt.Errorf("%w: temperature는 0~2여야 합니다", ErrInvalid)
			}
		}
		if authType, ok := value["auth_type"].(string); ok && authType != "" && authType != "bearer" && authType != "api-key" && authType != "none" {
			return fmt.Errorf("%w: auth_type은 bearer, api-key, none 중 하나여야 합니다", ErrInvalid)
		}
	case "notifications":
		if _, err := optionalBool(value, "enabled"); err != nil {
			return err
		}
		if _, err := optionalBool(value, "allow_insecure_http"); err != nil {
			return err
		}
		if rawEvents, ok := value["events"]; ok {
			events, ok := rawEvents.([]any)
			if !ok {
				return fmt.Errorf("%w: events는 문자열 배열이어야 합니다", ErrInvalid)
			}
			for _, raw := range events {
				event, ok := raw.(string)
				if !ok || !supportedWebhookEvents[event] {
					return fmt.Errorf("%w: 지원하지 않는 webhook event", ErrInvalid)
				}
			}
		}
	case "security":
		// 잘못된 타입을 조용히 버리면 보안 설정이 적용된 것처럼 보이므로 거부합니다.
		if _, err := optionalBool(value, "allow_local_login"); err != nil {
			return err
		}
		if _, err := optionalBool(value, "require_password_change"); err != nil {
			return err
		}
		if err := optionalIntInRange(value, "session_timeout_minutes", 5, 1440); err != nil {
			return err
		}
		if err := optionalIntInRange(value, "password_min_length", 12, 128); err != nil {
			return err
		}
		if err := optionalIntInRange(value, "audit_retention_days", 1, 3650); err != nil {
			return err
		}
		if err := optionalString(value, "allowed_networks"); err != nil {
			return err
		}
	case "service":
		if err := optionalString(value, "service_name"); err != nil {
			return err
		}
		if err := optionalString(value, "default_language"); err != nil {
			return err
		}
		if err := optionalString(value, "timezone"); err != nil {
			return err
		}
	}
	return nil
}

var supportedWebhookEvents = map[string]bool{
	"approval.requested": true,
	"approval.approved":  true,
	"approval.rejected":  true,
	"secret.created":     true,
	"secret.updated":     true,
	"secret.rotated":     true,
	"secret.deleted":     true,
	"rotation.failed":    true,
}

func SupportedWebhookEvents() []string {
	return []string{"approval.requested", "approval.approved", "approval.rejected", "secret.created", "secret.updated", "secret.rotated", "secret.deleted", "rotation.failed"}
}

func optionalBool(value map[string]any, key string) (bool, error) {
	raw, ok := value[key]
	if !ok {
		return false, nil
	}
	result, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("%w: %s는 boolean이어야 합니다", ErrInvalid, key)
	}
	return result, nil
}

func optionalString(value map[string]any, key string) error {
	raw, ok := value[key]
	if !ok {
		return nil
	}
	if _, ok := raw.(string); !ok {
		return fmt.Errorf("%w: %s는 문자열이어야 합니다", ErrInvalid, key)
	}
	return nil
}

func optionalIntInRange(value map[string]any, key string, minimum, maximum int) error {
	raw, ok := value[key]
	if !ok {
		return nil
	}
	number, ok := numberAsInt(raw)
	if !ok {
		return fmt.Errorf("%w: %s는 정수여야 합니다", ErrInvalid, key)
	}
	if number < minimum || number > maximum {
		return fmt.Errorf("%w: %s는 %d~%d여야 합니다", ErrInvalid, key, minimum, maximum)
	}
	return nil
}

func validateIntegrationURL(raw string, allowInsecure, allowQuery bool) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("http(s) URL 형식이 아닙니다")
	}
	if u.User != nil || u.Fragment != "" || (!allowQuery && u.RawQuery != "") {
		return errors.New("사용자 정보, fragment 또는 query를 포함할 수 없습니다")
	}
	if u.Scheme == "http" && !allowInsecure {
		host := strings.Trim(strings.ToLower(u.Hostname()), "[]")
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("HTTPS가 필요합니다(HTTP 사용 시 allow_insecure_http를 명시하세요)")
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
	Enabled           bool
	BaseURL           string
	Model             string
	MaxTokens         int
	Temperature       float64
	APIKey            string
	TimeoutSeconds    int
	AuthType          string
	AllowInsecureHTTP bool
}

func (s *Store) AIConfig(ctx context.Context) (AIConfig, error) {
	cfg := AIConfig{MaxTokens: 4096, Temperature: 0.2, TimeoutSeconds: 600, AuthType: "bearer"}
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
	if value, ok := setting.Value["auth_type"].(string); ok && value != "" {
		cfg.AuthType = value
	}
	cfg.AllowInsecureHTTP, _ = setting.Value["allow_insecure_http"].(bool)
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
	Enabled           bool
	IssuerURL         string
	ClientID          string
	ClientSecret      string
	Scopes            []string
	UsernameClaim     string
	GroupClaim        string
	RoleClaim         string
	RedirectURL       string
	AllowInsecureHTTP bool
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
	cfg.AllowInsecureHTTP, _ = setting.Value["allow_insecure_http"].(bool)
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

type WebhookConfig struct {
	Enabled           bool
	URL               string
	SigningSecret     string
	Events            map[string]bool
	AllowInsecureHTTP bool
}

func (s *Store) WebhookConfig(ctx context.Context) (WebhookConfig, error) {
	cfg := WebhookConfig{Events: make(map[string]bool)}
	setting, err := s.GetSetting(ctx, "notifications", false)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return WebhookConfig{}, err
	}
	if err == nil {
		cfg.Enabled, _ = setting.Value["enabled"].(bool)
		cfg.AllowInsecureHTTP, _ = setting.Value["allow_insecure_http"].(bool)
		if rawEvents, ok := setting.Value["events"].([]any); ok {
			for _, raw := range rawEvents {
				if event, ok := raw.(string); ok && supportedWebhookEvents[event] {
					cfg.Events[event] = true
				}
			}
		}
	}
	if len(cfg.Events) == 0 {
		for _, event := range SupportedWebhookEvents() {
			cfg.Events[event] = true
		}
	}
	webhook, err := s.GetSetting(ctx, "notification_webhook", true)
	if err == nil {
		cfg.URL, _ = webhook.Value["value"].(string)
	} else if !errors.Is(err, ErrNotFound) {
		return WebhookConfig{}, err
	}
	secret, err := s.GetSetting(ctx, "notification_webhook_secret", true)
	if err == nil {
		cfg.SigningSecret, _ = secret.Value["value"].(string)
	} else if !errors.Is(err, ErrNotFound) {
		return WebhookConfig{}, err
	}
	if cfg.URL != "" {
		if err := validateIntegrationURL(cfg.URL, cfg.AllowInsecureHTTP, true); err != nil {
			return WebhookConfig{}, fmt.Errorf("%w: webhook URL: %v", ErrInvalid, err)
		}
	}
	return cfg, nil
}

func settingUpdatedAt() time.Time { return time.Now().UTC() }

var _ = pgx.ErrNoRows
