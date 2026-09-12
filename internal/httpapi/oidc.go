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
	// Silent records that this attempt was started with prompt=none, so the
	// callback can tell a provider refusal (no session yet) from a real failure.
	Silent bool `json:"silent,omitempty"`
	// ReturnTo is the same-origin SPA path a deep-linked visitor came from.
	ReturnTo string `json:"return_to,omitempty"`
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
		"scopes": cfg.Scopes, "login_url": "/api/v1/oidc/login", "auto_login": cfg.Enabled && cfg.AutoLogin,
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
			AllowInsecureHTTP      bool     `json:"allow_insecure_http,omitempty"`
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
		if len(input.Scopes) > 0 {
			cfg.Scopes = append([]string(nil), input.Scopes...)
		}
		if strings.TrimSpace(input.RedirectURL) != "" {
			cfg.RedirectURL = strings.TrimSpace(input.RedirectURL)
		}
		cfg.AllowInsecureHTTP = input.AllowInsecureHTTP
		// The secret is deliberately accepted only to validate form shape; discovery does not transmit it.
		_ = input.ClientSecret
	}
	if cfg.IssuerURL == "" || cfg.ClientID == "" {
		writeError(w, r, http.StatusBadRequest, "invalid_oidc_config", "Issuer URL과 Client ID가 필요합니다")
		return
	}
	scopes := make([]any, 0, len(cfg.Scopes))
	for _, scope := range cfg.Scopes {
		scopes = append(scopes, scope)
	}
	if err := store.ValidateSetting("oidc", map[string]any{"enabled": true, "issuer_url": cfg.IssuerURL,
		"client_id": cfg.ClientID, "redirect_url": cfg.RedirectURL, "allow_insecure_http": cfg.AllowInsecureHTTP,
		"scopes": scopes}); err != nil {
		s.storeError(w, r, err)
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
	if err := validateOIDCRuntimeConfig(cfg); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "invalid_oidc_config", "저장된 OIDC 설정이 보안 정책에 맞지 않습니다")
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
	backendRedirect, ok := validOIDCBackendRedirect(cfg.RedirectURL)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "invalid_oidc_config", "OIDC callback Redirect URL이 설정되지 않았거나 올바르지 않습니다")
		return
	}
	stateValue := oidcState{State: state, Nonce: nonce, Verifier: verifier,
		FrontendRedirect: frontendRedirect, BackendRedirect: backendRedirect, IssuedAt: time.Now().UTC(),
		Silent: silentOIDCRequest(cfg, r), ReturnTo: safeOIDCReturnTo(r.URL.Query().Get("return_to"))}
	sealed, err := s.store.SealOIDCState(stateValue)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "jikim_oidc_state", Value: sealed, Path: "/api/v1/oidc/",
		HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	oauthConfig := oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
		Endpoint: provider.Endpoint(), RedirectURL: backendRedirect, Scopes: oidcScopes(cfg.Scopes)}
	authURL := oauthConfig.AuthCodeURL(state, oidcAuthCodeOptions(stateValue)...)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// silentOIDCRequest reports whether this login should be started with
// prompt=none. The query parameter alone is not enough: unless the
// administrator enabled auto_login, the request is silently downgraded to an
// ordinary login so nobody can change the flow by editing the address.
func silentOIDCRequest(cfg store.OIDCConfig, r *http.Request) bool {
	return cfg.AutoLogin && r.URL.Query().Get("prompt") == "none"
}

// oidcAuthCodeOptions builds the authorization request parameters. prompt=none
// asks the provider to answer from an existing session only; it never renders
// a screen, so either a code comes straight back or error=login_required does.
func oidcAuthCodeOptions(state oidcState) []oauth2.AuthCodeOption {
	challenge := sha256.Sum256([]byte(state.Verifier))
	options := []oauth2.AuthCodeOption{oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("nonce", state.Nonce),
		oauth2.SetAuthURLParam("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:])),
		oauth2.SetAuthURLParam("code_challenge_method", "S256")}
	if state.Silent {
		options = append(options, oauth2.SetAuthURLParam("prompt", "none"))
	}
	return options
}

// safeOIDCReturnTo keeps only a same-origin SPA path: it must start with "/"
// and not with "//", and must not parse as anything carrying a scheme or host.
// Anything else collapses to "" so the login flow can never bounce a visitor
// off-site.
func safeOIDCReturnTo(value string) string {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") ||
		strings.HasPrefix(value, "/\\") || strings.ContainsAny(value, "\r\n") {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil {
		return ""
	}
	// The callback and login screens are never a place to return to; landing
	// there again is the most common source of a redirect loop.
	if parsed.Path == "/login" || parsed.Path == "/oidc/callback" {
		return ""
	}
	return value
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if providerError := r.URL.Query().Get("error"); providerError != "" {
		// A silent attempt that comes back with login_required is not a
		// failure: the provider simply had no session. Send the browser to the
		// login screen with the marker it uses to stop retrying, so a signed-out
		// visitor is not bounced between the provider and the app in a loop.
		if state, silent := s.silentOIDCAttempt(r); silent {
			clearOIDCStateCookie(w, r)
			http.Redirect(w, r, silentRefusalLocation(state.ReturnTo), http.StatusFound)
			return
		}
		redirectOIDCCallbackError(w, r, "oidc_provider_error", "OIDC 공급자가 로그인을 완료하지 않았습니다")
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
	if err := validateOIDCRuntimeConfig(cfg); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "invalid_oidc_config", "저장된 OIDC 설정이 보안 정책에 맞지 않습니다")
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
	clearOIDCStateCookie(w, r)
	target, _ := url.Parse(state.FrontendRedirect)
	query := target.Query()
	query.Set("code", loginCode)
	if returnTo := safeOIDCReturnTo(state.ReturnTo); returnTo != "" {
		query.Set("return_to", returnTo)
	}
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// silentOIDCAttempt reports whether the state cookie on this callback belongs
// to a prompt=none attempt. The state itself is validated by the caller only on
// the success path; here a missing, expired, or mismatched state simply means
// "not silent", which falls through to the ordinary provider error screen.
func (s *Server) silentOIDCAttempt(r *http.Request) (oidcState, bool) {
	cookie, err := r.Cookie("jikim_oidc_state")
	if err != nil || cookie.Value == "" {
		return oidcState{}, false
	}
	var state oidcState
	if err := s.openOIDCState(cookie.Value, &state); err != nil {
		return oidcState{}, false
	}
	if subtle.ConstantTimeCompare([]byte(state.State), []byte(r.URL.Query().Get("state"))) != 1 {
		return oidcState{}, false
	}
	if !state.Silent || time.Since(state.IssuedAt) > 10*time.Minute {
		return oidcState{}, false
	}
	return state, true
}

// silentRefusalLocation is the login screen with the marker that stops the
// browser from retrying. The deep link travels along so a manual login still
// lands where the visitor was headed.
func silentRefusalLocation(returnTo string) string {
	target := &url.URL{Path: "/login"}
	query := url.Values{"sso": {"none"}}
	if returnTo = safeOIDCReturnTo(returnTo); returnTo != "" {
		query.Set("return_to", returnTo)
	}
	target.RawQuery = query.Encode()
	return target.String()
}

func (s *Server) openOIDCState(sealed string, value any) error {
	if s.oidcStateOpener != nil {
		return s.oidcStateOpener(sealed, value)
	}
	return s.store.OpenOIDCState(sealed, value)
}

// oidcLogout terminates the local session before sending OIDC users to the
// provider's RP-initiated logout endpoint. A provider outage must never keep a
// local jikim session alive.
func (s *Server) oidcLogout(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r)
	token, _ := r.Context().Value(tokenKey).(string)
	if token != "" {
		_ = s.store.RevokeToken(r.Context(), token)
	}
	clearSessionCookie(w, r)
	w.Header().Set("Cache-Control", "no-store")

	if session.User.AuthSource != "oidc" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	cfg, err := s.store.OIDCConfig(r.Context())
	if err != nil || strings.TrimSpace(cfg.IssuerURL) == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if err := validateOIDCRuntimeConfig(cfg); err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	provider, err := s.oidcProvider(r.Context(), cfg.IssuerURL)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	var discovery oidcDiscovery
	if err := provider.Claims(&discovery); err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	target, err := oidcLogoutURL(discovery.EndSessionEndpoint, cfg.ClientID, cfg.RedirectURL)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
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

func validateOIDCRuntimeConfig(cfg store.OIDCConfig) error {
	scopes := make([]any, 0, len(oidcScopes(cfg.Scopes)))
	for _, scope := range oidcScopes(cfg.Scopes) {
		scopes = append(scopes, scope)
	}
	return store.ValidateSetting("oidc", map[string]any{
		"enabled":             cfg.Enabled,
		"issuer_url":          cfg.IssuerURL,
		"client_id":           cfg.ClientID,
		"redirect_url":        cfg.RedirectURL,
		"scopes":              scopes,
		"allow_insecure_http": cfg.AllowInsecureHTTP,
	})
}

func safeFrontendRedirect(value string) bool {
	u, err := url.Parse(value)
	return err == nil && !u.IsAbs() && u.Host == "" && u.Path == "/oidc/callback"
}

func normalizeFrontendRedirect(r *http.Request, value string) (string, bool) {
	if safeFrontendRedirect(value) {
		return "/oidc/callback", true
	}
	target, err := url.Parse(value)
	if err != nil || !target.IsAbs() || target.Host == "" || target.Path != "/oidc/callback" || target.User != nil ||
		(target.Scheme != "http" && target.Scheme != "https") {
		return "", false
	}
	expectedScheme := "http"
	if requestIsHTTPS(r) {
		expectedScheme = "https"
	}
	if !strings.EqualFold(target.Scheme, expectedScheme) || !strings.EqualFold(target.Host, r.Host) {
		return "", false
	}
	// The browser-facing exchange code is always returned to a relative SPA
	// route. The untrusted Host header and caller-supplied absolute origin are
	// never retained in OIDC state.
	return "/oidc/callback", true
}

func validOIDCBackendRedirect(value string) (string, bool) {
	target, err := url.Parse(strings.TrimSpace(value))
	if err != nil || !target.IsAbs() || target.Host == "" || target.User != nil ||
		(target.Scheme != "http" && target.Scheme != "https") || target.Path != "/api/v1/oidc/callback" ||
		target.RawQuery != "" || target.Fragment != "" {
		return "", false
	}
	return target.String(), true
}

func redirectOIDCCallbackError(w http.ResponseWriter, r *http.Request, code, description string) {
	clearOIDCStateCookie(w, r)
	target := &url.URL{Path: "/oidc/callback"}
	query := target.Query()
	query.Set("error", code)
	query.Set("error_description", description)
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func clearOIDCStateCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "jikim_oidc_state", Value: "", Path: "/api/v1/oidc/",
		HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(0, 0)})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "jikim_session", Value: "", Path: "/", HttpOnly: true,
		Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(0, 0)})
}

func oidcLogoutURL(endpoint, clientID, configuredCallback string) (string, error) {
	target, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || target.Host == "" || target.User != nil || (target.Scheme != "http" && target.Scheme != "https") {
		return "", errors.New("invalid end session endpoint")
	}
	query := target.Query()
	if clientID = strings.TrimSpace(clientID); clientID != "" {
		query.Set("client_id", clientID)
	}
	// A post-logout redirect is emitted only from an administrator-configured
	// absolute callback. It is never constructed from the request Host header.
	if configured, ok := validOIDCBackendRedirect(configuredCallback); ok {
		callback, _ := url.Parse(configured)
		login := &url.URL{Scheme: callback.Scheme, Host: callback.Host, Path: "/login"}
		query.Set("post_logout_redirect_uri", login.String())
	}
	target.RawQuery = query.Encode()
	target.Fragment = ""
	return target.String(), nil
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
	roles := oidcMappedRoles(claims, claimName)
	return leastPrivilegeOIDCRole(roles)
}

func oidcMappedRoles(claims map[string]any, claimName string) map[string]bool {
	mapped := make(map[string]bool)
	if claimName == "" {
		return mapped
	}
	var value any = claims
	for _, part := range strings.Split(claimName, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return mapped
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
		switch role {
		case "jikim-admin":
			mapped["admin"] = true
		case "jikim-manager":
			mapped["manager"] = true
		case "jikim-auditor":
			mapped["auditor"] = true
		case "jikim-user":
			mapped["user"] = true
		}
	}
	return mapped
}

func leastPrivilegeOIDCRole(roles map[string]bool) string {
	if len(roles) == 0 {
		return ""
	}
	// Multiple recognized mappings are ambiguous. Resolve them to the least
	// privileged local role instead of depending on claim array order.
	if len(roles) > 1 {
		return "user"
	}
	for role := range roles {
		return role
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
	mapped := make(map[string]bool)
	for _, claimName := range []string{roleClaim, groupClaim} {
		if strings.TrimSpace(claimName) == "" {
			continue
		}
		configured = true
		for role := range oidcMappedRoles(claims, claimName) {
			mapped[role] = true
		}
	}
	if role := leastPrivilegeOIDCRole(mapped); role != "" {
		return role, true
	}
	if configured {
		// Removed or unrecognized IdP mappings must not leave stale privileges.
		return "user", true
	}
	return "", false
}
