package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/hkjang/jikim/internal/model"
	"github.com/jackc/pgx/v5"
)

// MCP SSO(OAuth) — the settings under the "mcp" key, shaped so the dotted
// names an operator reads in the standard (mcp.oauth.enabled, mcp.oauth.resource,
// mcp.oauth.audience, mcp.oauth.scopes) are the JSON path of the stored value.
// Everything about the authorization server itself is reused from "oidc".

// MCPScopeRead covers the tools that only read metadata; MCPScopeTransit covers
// the two Transit tools. A personal key carries no scope and keeps the user's
// full reach; an SSO token is held to the ceiling the administrator sets here.
const (
	MCPScopeRead    = "mcp:read"
	MCPScopeTransit = "mcp:transit"
)

var supportedMCPScopes = map[string]bool{MCPScopeRead: true, MCPScopeTransit: true}

// SupportedMCPScopes is the scope vocabulary this server understands.
func SupportedMCPScopes() []string { return []string{MCPScopeRead, MCPScopeTransit} }

type MCPOAuthConfig struct {
	// Enabled is mcp.oauth.enabled. Off by default; it stays effectively off
	// while OIDC is not configured, because the issuer is what verifies tokens.
	Enabled bool
	// Resource is mcp.oauth.resource: the identifier this deployment claims for
	// its MCP endpoint (RFC 8707). Empty means derive it from the OIDC callback
	// origin, which is the only public address jikim already knows.
	Resource string
	// Audiences is mcp.oauth.audience split on whitespace: aud or azp values an
	// administrator accepts without an Audience mapper.
	Audiences []string
	// Scopes is mcp.oauth.scopes split on whitespace and limited to the
	// vocabulary above.
	Scopes []string

	// Reused from the web sign-in configuration: the issuer verifies tokens
	// and links accounts, the callback origin is the public address.
	OIDCEnabled bool
	Issuer      string
	RedirectURL string
}

// Active reports whether SSO tokens are actually accepted: the switch is on,
// the web sign-in is configured, and there is an issuer to verify against.
func (c MCPOAuthConfig) Active() bool {
	return c.Enabled && c.OIDCEnabled && c.Issuer != ""
}

// InactiveReason says which condition keeps a switched-on configuration
// dormant, for the log line the operator will look for.
func (c MCPOAuthConfig) InactiveReason() string {
	switch {
	case !c.Enabled:
		return "mcp.oauth.enabled is off"
	case !c.OIDCEnabled:
		return "oidc.enabled is off"
	case c.Issuer == "":
		return "oidc.issuer_url is empty"
	default:
		return ""
	}
}

// ReadMCPOAuth turns the stored "mcp" value into the OAuth half of the config.
func ReadMCPOAuth(value map[string]any) MCPOAuthConfig {
	cfg := MCPOAuthConfig{Scopes: []string{MCPScopeRead}}
	oauth, _ := value["oauth"].(map[string]any)
	cfg.Enabled, _ = oauth["enabled"].(bool)
	if raw, ok := oauth["resource"].(string); ok {
		cfg.Resource = strings.TrimSpace(raw)
	}
	if raw, ok := oauth["audience"].(string); ok {
		cfg.Audiences = strings.Fields(raw)
	}
	if raw, ok := oauth["scopes"].(string); ok && strings.TrimSpace(raw) != "" {
		cfg.Scopes = nil
		for _, scope := range strings.Fields(raw) {
			if supportedMCPScopes[scope] {
				cfg.Scopes = append(cfg.Scopes, scope)
			}
		}
	}
	return cfg
}

func (s *Store) MCPOAuthConfig(ctx context.Context) (MCPOAuthConfig, error) {
	setting, err := s.GetSetting(ctx, "mcp", false)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return MCPOAuthConfig{}, err
	}
	cfg := ReadMCPOAuth(setting.Value)
	// The plain "oidc" row, not OIDCConfig: this runs on every MCP call and
	// the client secret it would decrypt is never needed to verify a token.
	oidc, err := s.GetSetting(ctx, "oidc", false)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return MCPOAuthConfig{}, err
	}
	cfg.OIDCEnabled, _ = oidc.Value["enabled"].(bool)
	issuer, _ := oidc.Value["issuer_url"].(string)
	cfg.Issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	redirectURL, _ := oidc.Value["redirect_url"].(string)
	cfg.RedirectURL = strings.TrimSpace(redirectURL)
	return cfg, nil
}

// validateMCPSetting refuses a shape that would silently read as "off" or as a
// resource nobody can match: a mistyped switch, a scope outside the vocabulary,
// or an identifier that is not an absolute http(s) URL.
func validateMCPSetting(value map[string]any) error {
	raw, ok := value["oauth"]
	if !ok {
		return nil
	}
	oauth, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: oauth는 객체여야 합니다", ErrInvalid)
	}
	if _, err := optionalBool(oauth, "enabled"); err != nil {
		return err
	}
	for _, key := range []string{"resource", "audience", "scopes"} {
		if err := optionalString(oauth, key); err != nil {
			return err
		}
	}
	if resource, _ := oauth["resource"].(string); strings.TrimSpace(resource) != "" {
		if err := validateMCPResource(resource); err != nil {
			return fmt.Errorf("%w: resource: %v", ErrInvalid, err)
		}
	}
	if scopes, _ := oauth["scopes"].(string); strings.TrimSpace(scopes) != "" {
		for _, scope := range strings.Fields(scopes) {
			if !supportedMCPScopes[scope] {
				return fmt.Errorf("%w: 지원하지 않는 MCP scope %q (지원: %s)", ErrInvalid, scope, strings.Join(SupportedMCPScopes(), " "))
			}
		}
	}
	return nil
}

// validateMCPResource checks the identifier, not a connection: nothing is ever
// fetched from it, so plain http is allowed for an internal deployment, but it
// must be an absolute URL a token's aud can equal byte for byte.
func validateMCPResource(raw string) error {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || target.Host == "" || (target.Scheme != "https" && target.Scheme != "http") {
		return errors.New("http(s) URL 형식이 아닙니다")
	}
	if target.User != nil || target.Fragment != "" || target.RawQuery != "" {
		return errors.New("사용자 정보, fragment 또는 query를 포함할 수 없습니다")
	}
	return nil
}

// UserByOIDCSubject is the lookup an SSO token is allowed: the active account
// the web sign-in already linked to this issuer and subject. It never creates
// one — signing in to the web is what provisions an account, and a machine
// presenting a token is not the moment to decide who somebody is.
func (s *Store) UserByOIDCSubject(ctx context.Context, issuer, subject string) (model.User, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" || strings.TrimSpace(subject) == "" {
		return model.User{}, ErrInvalid
	}
	user, err := scanUser(s.pool.QueryRow(ctx, userSelect+
		` WHERE auth_source='oidc' AND external_issuer=$1 AND external_subject=$2 AND active=true`, issuer, subject))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("사용자 조회 실패: %w", err)
	}
	return user, nil
}
