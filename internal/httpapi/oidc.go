package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/hkjang/jikim/internal/ids"
	"github.com/hkjang/jikim/internal/store"
	"golang.org/x/oauth2"
)

type oidcState struct {
	State            string    `json:"state"`
	Nonce            string    `json:"nonce"`
	Verifier         string    `json:"verifier"`
	FrontendRedirect string    `json:"frontend_redirect"`
	BackendRedirect  string    `json:"backend_redirect"`
	IssuedAt         time.Time `json:"issued_at"`
}

type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURL               string `json:"jwks_uri"`
	UserInfoEndpoint      string `json:"userinfo_endpoint"`
	EndSessionEndpoint    string `json:"end_session_endpoint,omitempty"`
}

func (s *Server) oidcPublicConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.OIDCConfig(r.Context())
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{
		"enabled": cfg.Enabled, "issuer_url": cfg.IssuerURL, "client_id": cfg.ClientID,
		"scopes": cfg.Scopes, "login_url": "/api/v1/oidc/login",
	})
}

func (s *Server) oidcTest(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.OIDCConfig(r.Context())
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.storeError(w, r, err)
		return
	}
	if r.ContentLength != 0 {
		var input struct {
			Enabled                bool     `json:"enabled,omitempty"`
			IssuerURL              string   `json:"issuer_url"`
			ClientID               string   `json:"client_id"`
			ClientSecret           string   `json:"client_secret"`
			Scopes                 []string `json:"scopes,omitempty"`
			UsernameClaim          string   `json:"username_claim,omitempty"`
			GroupClaim             string   `json:"group_claim,omitempty"`
			RoleClaim              string   `json:"role_claim,omitempty"`
			RedirectURL            string   `json:"redirect_url,omitempty"`
			ClientSecretConfigured bool     `json:"client_secret_configured,omitempty"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if strings.TrimSpace(input.IssuerURL) != "" {
			cfg.IssuerURL = strings.TrimSpace(input.IssuerURL)
		}
		if strings.TrimSpace(input.ClientID) != "" {
			cfg.ClientID = strings.TrimSpace(input.ClientID)
		}
		// The secret is deliberately accepted only to validate form shape; discovery does not transmit it.
		_ = input.ClientSecret
	}
	if cfg.IssuerURL == "" || cfg.ClientID == "" {
		writeError(w, r, http.StatusBadRequest, "invalid_oidc_config", "Issuer URL과 Client ID가 필요합니다")
		return
	}
	provider, err := s.oidcProvider(r.Context(), cfg.IssuerURL)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "oidc_discovery_failed", "OIDC Discovery에 실패했습니다")
		return
	}
	var discovery oidcDiscovery
	if err := provider.Claims(&discovery); err != nil {
		writeError(w, r, http.StatusBadGateway, "oidc_discovery_failed", "OIDC Discovery 응답을 확인할 수 없습니다")
		return
	}
	writeData(w, http.StatusOK, map[string]any{"connected": true, "issuer": cfg.IssuerURL, "discovery": discovery})
}

func (s *Server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.OIDCConfig(r.Context())
	if err != nil || !cfg.Enabled || cfg.IssuerURL == "" || cfg.ClientID == "" {
		writeError(w, r, http.StatusServiceUnavailable, "oidc_disabled", "OIDC 로그인이 설정되지 않았습니다")
		return
	}
	frontendRedirect := r.URL.Query().Get("redirect_uri")
	if frontendRedirect == "" {
		frontendRedirect = "/oidc/callback"
	}
	frontendRedirect, ok := normalizeFrontendRedirect(r, frontendRedirect)
	if !ok {
		writeError(w, r, http.StatusBadRequest, "invalid_redirect", "프런트엔드 redirect_uri가 올바르지 않습니다")
		return
	}
	provider, err := s.oidcProvider(r.Context(), cfg.IssuerURL)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "oidc_discovery_failed", "OIDC 공급자에 연결할 수 없습니다")
		return
	}
	state, _, err := ids.Token("")
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	nonce, _, err := ids.Token("")
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	verifier, _, err := ids.Token("")
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	backendRedirect := cfg.RedirectURL
	if backendRedirect == "" {
		backendRedirect = requestOrigin(r) + "/api/v1/oidc/callback"
	}
	stateValue := oidcState{State: state, Nonce: nonce, Verifier: verifier,
		FrontendRedirect: frontendRedirect, BackendRedirect: backendRedirect, IssuedAt: time.Now().UTC()}
	sealed, err := s.store.SealOIDCState(stateValue)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "jikim_oidc_state", Value: sealed, Path: "/api/v1/oidc/",
		HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	oauthConfig := oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
		Endpoint: provider.Endpoint(), RedirectURL: backendRedirect, Scopes: oidcScopes(cfg.Scopes)}
	challenge := sha256.Sum256([]byte(verifier))
	authURL := oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:])),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"))
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if providerError := r.URL.Query().Get("error"); providerError != "" {
		writeError(w, r, http.StatusUnauthorized, "oidc_provider_error", "OIDC 공급자가 로그인을 거부했습니다")
		return
	}
	cookie, err := r.Cookie("jikim_oidc_state")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_oidc_state", "OIDC 상태 쿠키가 없습니다")
		return
	}
	var state oidcState
	if err := s.store.OpenOIDCState(cookie.Value, &state); err != nil || time.Since(state.IssuedAt) > 10*time.Minute || time.Since(state.IssuedAt) < -time.Minute {
		writeError(w, r, http.StatusBadRequest, "invalid_oidc_state", "OIDC 상태가 만료되었거나 올바르지 않습니다")
		return
	}
	if subtle.ConstantTimeCompare([]byte(state.State), []byte(r.URL.Query().Get("state"))) != 1 {
		writeError(w, r, http.StatusBadRequest, "invalid_oidc_state", "OIDC state 검증에 실패했습니다")
		return
	}
	cfg, err := s.store.OIDCConfig(r.Context())
	if err != nil || !cfg.Enabled {
		writeError(w, r, http.StatusServiceUnavailable, "oidc_disabled", "OIDC 로그인이 비활성화되었습니다")
		return
	}
	provider, err := s.oidcProvider(r.Context(), cfg.IssuerURL)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, "oidc_discovery_failed", "OIDC 공급자에 연결할 수 없습니다")
		return
	}
	oauthConfig := oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
		Endpoint: provider.Endpoint(), RedirectURL: state.BackendRedirect, Scopes: oidcScopes(cfg.Scopes)}
	ctx := context.WithValue(r.Context(), oauth2.HTTPClient, s.httpClient)
	token, err := oauthConfig.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(state.Verifier))
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "oidc_exchange_failed", "OIDC 인증 코드를 교환하지 못했습니다")
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		writeError(w, r, http.StatusUnauthorized, "oidc_missing_id_token", "OIDC ID Token이 없습니다")
		return
	}
	idToken, err := provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "oidc_invalid_id_token", "OIDC ID Token 검증에 실패했습니다")
		return
	}
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		writeError(w, r, http.StatusUnauthorized, "oidc_invalid_claims", "OIDC Claim을 확인할 수 없습니다")
		return
	}
	claimNonce, _ := claims["nonce"].(string)
	if subtle.ConstantTimeCompare([]byte(state.Nonce), []byte(claimNonce)) != 1 {
		writeError(w, r, http.StatusUnauthorized, "oidc_invalid_nonce", "OIDC nonce 검증에 실패했습니다")
		return
	}
	subject, _ := claims["sub"].(string)
	username := claimString(claims, cfg.UsernameClaim)
	if username == "" {
		username = firstString(claims, "preferred_username", "email", "name", "sub")
	}
	email, _ := claims["email"].(string)
	displayName := firstString(claims, "name", "preferred_username", "email")
	user, err := s.store.UpsertOIDCUser(r.Context(), cfg.IssuerURL, subject, username, email, displayName)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if role, synchronize := synchronizedOIDCRoleClaims(claims, cfg.RoleClaim, cfg.GroupClaim); synchronize {
		if role != user.Role {
			user, err = s.store.UpdateUser(r.Context(), user.ID, store.UserInput{Role: role})
			if err != nil {
				s.storeError(w, r, err)
				return
			}
		}
	}
	loginCode, err := s.store.CreateOIDCLoginCode(r.Context(), user.ID)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "jikim_oidc_state", Value: "", Path: "/api/v1/oidc/",
		HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	target, _ := url.Parse(state.FrontendRedirect)
	query := target.Query()
	query.Set("code", loginCode)
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (s *Server) oidcExchange(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	userID, err := s.store.ConsumeOIDCLoginCode(r.Context(), input.Code)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "invalid_login_code", "로그인 코드가 만료되었거나 이미 사용되었습니다")
		return
	}
	user, err := s.store.GetUser(r.Context(), userID)
	if err != nil || !user.Active {
		writeError(w, r, http.StatusUnauthorized, "inactive_user", "사용자 계정이 비활성화되었습니다")
		return
	}
	security, err := s.store.SecurityConfig(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	ttl := time.Duration(security.SessionTimeoutMinutes) * time.Minute
	token, session, err := s.store.CreateSession(r.Context(), user.ID, "session", "OIDC 로그인", user.ID, ttl)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	setSessionCookie(w, r, token, session.ExpiresAt)
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "expires_at": session.ExpiresAt})
}

func (s *Server) oidcProvider(ctx context.Context, issuer string) (*gooidc.Provider, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, s.httpClient)
	return gooidc.NewProvider(ctx, strings.TrimRight(issuer, "/"))
}

func oidcScopes(configured []string) []string {
	seen := map[string]bool{"openid": true}
	result := []string{"openid"}
	for _, scope := range configured {
		scope = strings.TrimSpace(scope)
		if scope != "" && !seen[scope] {
			seen[scope] = true
			result = append(result, scope)
		}
	}
	if len(result) == 1 {
		result = append(result, "profile", "email")
	}
	return result
}

func safeFrontendRedirect(value string) bool {
	u, err := url.Parse(value)
	return err == nil && !u.IsAbs() && u.Host == "" && u.Path == "/oidc/callback"
}

func normalizeFrontendRedirect(r *http.Request, value string) (string, bool) {
	if safeFrontendRedirect(value) {
		return value, true
	}
	target, err := url.Parse(value)
	if err != nil || !target.IsAbs() || target.Path != "/oidc/callback" || target.User != nil {
		return "", false
	}
	origin, err := url.Parse(requestOrigin(r))
	if err != nil || !strings.EqualFold(target.Scheme, origin.Scheme) || !strings.EqualFold(target.Host, origin.Host) {
		return "", false
	}
	return target.String(), true
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func firstString(claims map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := claims[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func claimString(claims map[string]any, claimName string) string {
	if strings.TrimSpace(claimName) == "" {
		return ""
	}
	var value any = claims
	for _, part := range strings.Split(claimName, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = object[part]
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func oidcRole(claims map[string]any, claimName string) string {
	if claimName == "" {
		return ""
	}
	var value any = claims
	for _, part := range strings.Split(claimName, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = object[part]
	}
	roles := make([]string, 0)
	switch raw := value.(type) {
	case string:
		roles = append(roles, raw)
	case []any:
		for _, item := range raw {
			if role, ok := item.(string); ok {
				roles = append(roles, role)
			}
		}
	}
	for _, role := range roles {
		normalized := strings.Trim(strings.ToLower(strings.TrimSpace(role)), "/")
		if slash := strings.LastIndex(normalized, "/"); slash >= 0 {
			normalized = normalized[slash+1:]
		}
		switch normalized {
		case "admin", "jikim-admin":
			return "admin"
		case "manager", "jikim-manager":
			return "manager"
		case "auditor", "jikim-auditor":
			return "auditor"
		case "user", "jikim-user":
			return "user"
		}
	}
	return ""
}

func synchronizedOIDCRole(claims map[string]any, claimName string) (string, bool) {
	if strings.TrimSpace(claimName) == "" {
		return "", false
	}
	role := oidcRole(claims, claimName)
	if role == "" {
		// A removed or unrecognized IdP role must not leave a stale privileged local role.
		role = "user"
	}
	return role, true
}

func synchronizedOIDCRoleClaims(claims map[string]any, roleClaim, groupClaim string) (string, bool) {
	configured := false
	for _, claimName := range []string{roleClaim, groupClaim} {
		if strings.TrimSpace(claimName) == "" {
			continue
		}
		configured = true
		if role := oidcRole(claims, claimName); role != "" {
			return role, true
		}
	}
	if configured {
		// Removed or unrecognized IdP mappings must not leave stale privileges.
		return "user", true
	}
	return "", false
}
