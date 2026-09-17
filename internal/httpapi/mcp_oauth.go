package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
	"golang.org/x/oauth2"
)

// MCP 를 SSO 로 — 개인 키 없이, Keycloak 이 발급한 액세스 토큰으로.
//
// The MCP authorization specification (2025-06-18 and later) is OAuth 2.1: the
// MCP server is a *resource server* that publishes where its authorization
// server is (RFC 9728), and a client refused with 401 reads that document,
// sends the person through Keycloak with PKCE, and comes back with an access
// token whose audience (RFC 8707) is this server. Nothing about issuing tokens
// happens here. This file answers two questions only: where is the
// authorization server, and is this token one it issued for us.
//
// The key stays. A token from SSO is a second door into the same room: it
// authenticates an account the web sign-in already registered, is accepted on
// /mcp and nowhere else, and is held to the scope ceiling the administrator
// set. It never creates an account and never reads a role out of a claim.

const (
	mcpOAuthMetadataPath = "/.well-known/oauth-protected-resource"
	mcpOAuthRealm        = "jikim"
)

// mcpOAuthSigningAlgs is the asymmetric set a Keycloak realm signs with. HS*
// would let anyone who knows the shared secret mint a token, and "none" is not
// a signature; go-oidc refuses both when they are not listed here.
var mcpOAuthSigningAlgs = []string{
	gooidc.RS256, gooidc.RS384, gooidc.RS512,
	gooidc.ES256, gooidc.ES384, gooidc.ES512,
	gooidc.PS256, gooidc.PS384, gooidc.PS512,
}

// mcpOAuthRefusal is a token refusal whose message is meant for the person
// behind the MCP client: it says what was seen and what to change.
type mcpOAuthRefusal struct {
	code    string
	message string
	cause   error
}

func (e mcpOAuthRefusal) Error() string {
	if e.cause != nil {
		return e.code + ": " + e.cause.Error()
	}
	return e.code + ": " + e.message
}

func (e mcpOAuthRefusal) Unwrap() error { return e.cause }

func refuseOAuth(code, message string, cause error) error {
	return mcpOAuthRefusal{code: code, message: message, cause: cause}
}

// mcpOAuthConfig reads the settings through the seam tests use to run without
// a database.
func (s *Server) mcpOAuthConfig(ctx context.Context) (store.MCPOAuthConfig, error) {
	if s.mcpOAuthLoader != nil {
		return s.mcpOAuthLoader(ctx)
	}
	return s.store.MCPOAuthConfig(ctx)
}

// mcpResource is the identifier this deployment claims for its MCP endpoint:
// what the metadata advertises and what a token's aud must name. It comes
// from configuration first — mcp.oauth.resource, then the origin of the OIDC
// callback, which is the public address the administrator already wrote down
// — and from the request only when neither exists, because anyone can set a
// Host header.
func mcpResource(cfg store.MCPOAuthConfig, r *http.Request) string {
	if cfg.Resource != "" {
		return cfg.Resource
	}
	if callback, err := url.Parse(cfg.RedirectURL); err == nil && callback.IsAbs() && callback.Host != "" {
		return callback.Scheme + "://" + callback.Host + "/mcp"
	}
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/mcp"
}

// mcpMetadataURL is where a refused client is sent to learn the above.
func mcpMetadataURL(cfg store.MCPOAuthConfig, r *http.Request) string {
	resource := mcpResource(cfg, r)
	base := resource
	if parsed, err := url.Parse(resource); err == nil && parsed.Host != "" {
		base = parsed.Scheme + "://" + parsed.Host
	}
	return strings.TrimSuffix(base, "/") + mcpOAuthMetadataPath + "/mcp"
}

// oauthProviders caches discovery per issuer. Discovery is a round trip to
// Keycloak and the JWKS behind it verifies every token; doing that per request
// would put Keycloak's latency in front of every MCP call. go-oidc refetches
// the key set on an unknown key id, so key rotation needs no invalidation.
type oauthProviders struct {
	mu       sync.Mutex
	byIssuer map[string]*gooidc.Provider
}

func (s *Server) mcpOAuthProvider(ctx context.Context, issuer string) (*gooidc.Provider, error) {
	s.oauthProviders.mu.Lock()
	defer s.oauthProviders.mu.Unlock()
	if provider := s.oauthProviders.byIssuer[issuer]; provider != nil {
		return provider, nil
	}
	// Discovery must outlive this request: the provider keeps the context for
	// later key fetches.
	ctx = context.WithoutCancel(ctx)
	if s.httpClient != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, s.httpClient)
	}
	provider, err := gooidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	if s.oauthProviders.byIssuer == nil {
		s.oauthProviders.byIssuer = map[string]*gooidc.Provider{}
	}
	s.oauthProviders.byIssuer[issuer] = provider
	return provider, nil
}

// looksLikeJWT is the cheap shape test that separates a key from a token. A
// jikim key is a prefix and one base64url blob ("hvs.…", "jks.…") — one dot —
// so a three-part value can only be a JWT.
func looksLikeJWT(token string) bool {
	parts := strings.Split(token, ".")
	return len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != ""
}

// mcpOAuthClaims are the parts of a Keycloak access token this server checks
// by hand, on top of what go-oidc verifies (signature, iss, exp, nbf).
type mcpOAuthClaims struct {
	Type         string          `json:"typ"`
	AuthorizedTo string          `json:"azp"`
	Scope        string          `json:"scope"`
	Confirmation json.RawMessage `json:"cnf"`
}

// oauthSession turns a bearer access token into the session an MCP call runs
// as, or says exactly why it will not.
func (s *Server) oauthSession(ctx context.Context, r *http.Request, cfg store.MCPOAuthConfig, token string) (model.Session, error) {
	provider, err := s.mcpOAuthProvider(ctx, cfg.Issuer)
	if err != nil {
		s.logger.Warn("mcp oauth discovery failed", "issuer", cfg.Issuer, "error", err)
		return model.Session{}, refuseOAuth("discovery_failed",
			"Keycloak 발급자 정보를 읽지 못해 SSO 토큰을 확인할 수 없습니다. 잠시 후 다시 시도하거나 관리자에게 알리세요", nil)
	}
	// Signature, issuer, expiry and nbf. The audience is checked below by hand
	// because more than one value is acceptable and the library compares one.
	verified, err := provider.Verifier(&gooidc.Config{SkipClientIDCheck: true, SupportedSigningAlgs: mcpOAuthSigningAlgs}).Verify(ctx, token)
	if err != nil {
		return model.Session{}, refuseOAuth("invalid_token",
			"SSO 액세스 토큰이 유효하지 않습니다(서명·발급자·만료). 클라이언트에서 다시 로그인하세요", err)
	}
	var claims mcpOAuthClaims
	if err := verified.Claims(&claims); err != nil {
		return model.Session{}, refuseOAuth("invalid_token", "SSO 토큰의 claim을 읽을 수 없습니다", err)
	}
	// An ID token proves a login happened; it is not an API credential, and
	// Keycloak marks the difference in typ.
	if strings.EqualFold(claims.Type, "ID") {
		return model.Session{}, refuseOAuth("invalid_token",
			"ID 토큰은 MCP 자격이 아닙니다. 액세스 토큰(typ=Bearer)을 보내세요", nil)
	}
	// A cnf claim binds the token to a key (DPoP, mTLS) this server cannot
	// check; accepting it as a plain bearer would drop that binding.
	if len(claims.Confirmation) > 0 && string(claims.Confirmation) != "null" {
		return model.Session{}, refuseOAuth("invalid_token",
			"소지자 증명(cnf)이 묶인 토큰은 받지 않습니다. 일반 Bearer 액세스 토큰을 보내세요", nil)
	}
	if strings.TrimSpace(verified.Subject) == "" {
		return model.Session{}, refuseOAuth("invalid_token", "SSO 토큰에 sub가 없습니다", nil)
	}
	// Whom the token was minted for. Measured against a real Keycloak 26: an
	// access token issued to a client carries that client in azp and
	// aud=["account"] — the client id is not in aud. So the binding checked is
	// "aud names this resource, or aud/azp is a value the administrator
	// listed". Either is the token being for this deployment rather than one
	// passed through from another application in the realm.
	resource := mcpResource(cfg, r)
	if !mcpAudienceAccepted(resource, cfg.Audiences, verified.Audience, claims.AuthorizedTo) {
		return model.Session{}, refuseOAuth("invalid_token", fmt.Sprintf(
			"SSO 토큰이 이 서버를 위해 발급된 것이 아닙니다(aud=%v, azp=%q). 관리자가 MCP SSO 설정의 허용 대상에 %q를 적거나, Keycloak 클라이언트에 Audience 매퍼로 %q를 넣어야 합니다",
			verified.Audience, claims.AuthorizedTo, mcpAudienceHint(verified.Audience, claims.AuthorizedTo), resource), nil)
	}
	// The same account the web sign-in linked, without the provisioning half.
	user, err := s.lookupOIDCUser(ctx, cfg.Issuer, verified.Subject)
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) {
		return model.Session{}, refuseOAuth("unknown_account",
			"이 SSO 계정은 jikim에 등록되지 않았거나 비활성입니다. 먼저 웹으로 한 번 로그인하세요", nil)
	}
	if err != nil {
		return model.Session{}, err
	}
	return model.Session{Kind: "oauth", Name: "MCP SSO", User: user, ExpiresAt: verified.Expiry,
		Scopes: mcpGrantedScopes(cfg.Scopes, claims.Scope)}, nil
}

func (s *Server) lookupOIDCUser(ctx context.Context, issuer, subject string) (model.User, error) {
	if s.oidcUserFinder != nil {
		return s.oidcUserFinder(ctx, issuer, subject)
	}
	return s.store.UserByOIDCSubject(ctx, issuer, subject)
}

// mcpAudienceAccepted: the resource identifier in aud is the mapper path; a
// listed value in aud or azp is the compatibility path.
func mcpAudienceAccepted(resource string, accepted, audience []string, azp string) bool {
	if slices.Contains(audience, resource) {
		return true
	}
	bound := append(slices.Clone(audience), azp)
	return slices.ContainsFunc(bound, func(value string) bool {
		return value != "" && slices.Contains(accepted, value)
	})
}

// mcpAudienceHint picks the value the operator should list: the client the
// token was issued to, or failing that whatever aud carried.
func mcpAudienceHint(audience []string, azp string) string {
	if azp != "" {
		return azp
	}
	for _, value := range audience {
		if value != "" && value != "account" {
			return value
		}
	}
	return "<client-id>"
}

// mcpGrantedScopes is the administrator's ceiling, narrowed to the token's own
// scope claim when Keycloak was taught this vocabulary. A token that carries
// none of it gets the ceiling as is — nobody has to configure Keycloak scopes
// before MCP works.
func mcpGrantedScopes(ceiling []string, tokenScope string) []string {
	granted := slices.Clone(ceiling)
	if granted == nil {
		granted = []string{}
	}
	requested := make([]string, 0)
	for _, scope := range strings.Fields(tokenScope) {
		if strings.HasPrefix(scope, "mcp:") {
			requested = append(requested, scope)
		}
	}
	if len(requested) == 0 {
		return granted
	}
	intersection := make([]string, 0, len(granted))
	for _, scope := range granted {
		if slices.Contains(requested, scope) {
			intersection = append(intersection, scope)
		}
	}
	return intersection
}

// mcpToolScope is the scope a tool needs from an SSO principal. Keys carry no
// scope and are not consulted here.
func mcpToolScope(tool string) string {
	switch tool {
	case "transit.encrypt", "transit.decrypt":
		return store.MCPScopeTransit
	default:
		return store.MCPScopeRead
	}
}

// mcpScopeAllows reports whether the session may call the tool. Only an OAuth
// session carries scopes; every other session keeps the user's full reach.
func mcpScopeAllows(session model.Session, tool string) bool {
	if session.Kind != "oauth" {
		return true
	}
	return slices.Contains(session.Scopes, mcpToolScope(tool))
}

// withMCPAuth is withAuth for the MCP endpoint: the same Authorization header
// can carry a key or an SSO access token, and a refusal points at the
// metadata so an OAuth client knows where to sign in. REST keeps withAuth —
// a WWW-Authenticate pointer on a REST 401 sends browsers the wrong way.
func (s *Server) withMCPAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg, err := s.mcpOAuthConfig(r.Context())
		if err != nil {
			s.storeError(w, r, err)
			return
		}
		active := cfg.Active()
		token := requestToken(r)
		if token == "" {
			if active {
				w.Header().Set("WWW-Authenticate", mcpChallenge(cfg, r, ""))
			}
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "로그인이 필요합니다")
			return
		}
		// Only the Authorization header carries an SSO token; the cookie and
		// X-Vault-Token stay what they are today.
		bearer := bearerToken(r)
		if looksLikeJWT(bearer) && cfg.Enabled && !active {
			// Switched on but dormant: say why in the log, once somebody
			// actually tries a token, rather than on every key request.
			s.logger.Warn("mcp oauth is switched on but dormant", "reason", cfg.InactiveReason(), "request_id", requestIDFrom(r))
		}
		if active && looksLikeJWT(bearer) {
			session, err := s.oauthSession(r.Context(), r, cfg, bearer)
			var refusal mcpOAuthRefusal
			if errors.As(err, &refusal) {
				s.logger.Info("mcp oauth token refused", "reason", refusal.Error(), "request_id", requestIDFrom(r))
				w.Header().Set("WWW-Authenticate", mcpChallenge(cfg, r, "invalid_token"))
				writeError(w, r, http.StatusUnauthorized, "unauthorized", refusal.message)
				return
			}
			if err != nil {
				s.storeError(w, r, err)
				return
			}
			ctx := context.WithValue(r.Context(), sessionKey, session)
			*r = *r.WithContext(ctx)
			next.ServeHTTP(w, r)
			return
		}
		// A key, a cookie, or — with SSO off — anything else: exactly the
		// answer the endpoint always gave, so a deployment without SSO learns
		// nothing new from this code path.
		challenge := ""
		if active {
			challenge = mcpChallenge(cfg, r, "invalid_token")
		}
		s.withAuth(next).ServeHTTP(&mcpChallengeWriter{ResponseWriter: w, challenge: challenge}, r)
	})
}

// mcpChallenge is the header that turns a 401 into an invitation: the MCP
// client reads resource_metadata and starts the OAuth flow from there.
func mcpChallenge(cfg store.MCPOAuthConfig, r *http.Request, errorCode string) string {
	header := fmt.Sprintf(`Bearer realm=%q, resource_metadata=%q`, mcpOAuthRealm, mcpMetadataURL(cfg, r))
	if errorCode != "" {
		header += fmt.Sprintf(`, error=%q`, errorCode)
	}
	return header
}

// mcpChallengeWriter adds the challenge to a 401 that withAuth produces for a
// key that no longer resolves, without touching any other status.
type mcpChallengeWriter struct {
	http.ResponseWriter
	challenge string
}

func (w *mcpChallengeWriter) WriteHeader(status int) {
	if status == http.StatusUnauthorized && w.challenge != "" {
		w.Header().Set("WWW-Authenticate", w.challenge)
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *mcpChallengeWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *mcpChallengeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// requireOIDCForMCPOAuth checks, at save time, that the OIDC sign-in the
// tokens will be verified against is on: the values being saved alongside
// when the same request carries them, the stored ones otherwise.
func (s *Server) requireOIDCForMCPOAuth(ctx context.Context, incoming map[string]any) error {
	cfg, err := s.store.OIDCConfig(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	enabled, issuer := cfg.Enabled, cfg.IssuerURL
	if incoming != nil {
		if value, ok := incoming["enabled"].(bool); ok {
			enabled = value
		}
		if value, ok := incoming["issuer_url"].(string); ok {
			issuer = value
		}
	}
	if !enabled || strings.TrimSpace(issuer) == "" {
		return errors.New("MCP SSO(OAuth)를 켜려면 Keycloak OIDC 연결이 켜져 있고 Issuer URL이 있어야 합니다")
	}
	return nil
}

// protectedResourceMetadata is RFC 9728: the document a refused MCP client
// reads to find the authorization server. Public by design — it says where to
// sign in, not who is signed in — and a bare document rather than the {data:…}
// envelope, because the reader is an OAuth client library.
func (s *Server) protectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.mcpOAuthConfig(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if !cfg.Active() {
		writeError(w, r, http.StatusNotFound, "mcp_oauth_disabled", "이 서버의 MCP는 SSO 토큰을 받지 않습니다. 개인 API 토큰을 사용하세요")
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, mcpMetadataDocument(cfg, r))
}

func mcpMetadataDocument(cfg store.MCPOAuthConfig, r *http.Request) map[string]any {
	scopes := cfg.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return map[string]any{
		"resource":                 mcpResource(cfg, r),
		"authorization_servers":    []string{cfg.Issuer},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         scopes,
		"resource_name":            "jikim MCP",
	}
}
