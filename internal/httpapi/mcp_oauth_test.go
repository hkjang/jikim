package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
)

// MCP 를 개인 키 없이 Keycloak 토큰으로.
//
// The authorization flow itself — PKCE, the redirect, the code exchange — is
// Keycloak's and the client's. What is this server's is the resource-server
// half of the specification, and that is what these tests hold it to: it says
// where the authorization server is, it turns a 401 into a pointer there, and
// it accepts exactly the tokens that server issued for this resource, for a
// person jikim already knows, with the reach a key would have and no more.

// fakeIdP serves discovery and a JWKS for one RSA key, and signs tokens with
// it — the parts of Keycloak a resource server actually talks to.
type fakeIdP struct {
	server *httptest.Server
	key    *rsa.PrivateKey
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdP{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                idp.server.URL,
			"authorization_endpoint":                idp.server.URL + "/authorize",
			"token_endpoint":                        idp.server.URL + "/token",
			"jwks_uri":                              idp.server.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: key.Public(), KeyID: "realm-key", Algorithm: "RS256", Use: "sig"},
		}})
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (idp *fakeIdP) sign(t *testing.T, payload map[string]any) string {
	t.Helper()
	return signJWT(t, jose.SigningKey{Algorithm: jose.RS256, Key: idp.key}, "realm-key", payload)
}

func signJWT(t *testing.T, signingKey jose.SigningKey, kid string, payload map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(signingKey, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign(encoded)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// accessToken is what Keycloak hands an MCP client after the person signed
// in: signed by the realm key, issued by the issuer, for an audience.
func (idp *fakeIdP) accessToken(t *testing.T, audience any, extra map[string]any) string {
	t.Helper()
	payload := map[string]any{
		"iss": idp.server.URL, "aud": audience, "sub": "subject-mcp", "azp": "claude-mcp",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"typ": "Bearer", "preferred_username": "ssomember",
	}
	for key, value := range extra {
		payload[key] = value
	}
	return idp.sign(t, payload)
}

const (
	mcpTestResource = "https://jikim.example/mcp"
	listToolsBody   = `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	encryptBody     = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"transit.encrypt","arguments":{"key":"app","plaintext":"aGk="}}}`
)

var ssoMember = model.User{ID: "user-sso", Username: "ssomember", Role: "user", Active: true, AuthSource: "oidc"}

// mcpOAuthServer is a server with no database: settings, the key lookup and
// the account lookup all go through seams, and the fake IdP is real.
func mcpOAuthServer(t *testing.T, cfg store.MCPOAuthConfig) (*Server, http.Handler) {
	t.Helper()
	server := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), httpClient: http.DefaultClient}
	server.mcpOAuthLoader = func(context.Context) (store.MCPOAuthConfig, error) { return cfg, nil }
	server.sessionResolver = func(_ context.Context, token string) (model.Session, error) {
		if token == "hvs.personal-key" {
			return model.Session{ID: "key-1", Kind: "api", User: model.User{ID: "user-key", Username: "keyholder", Role: "user", Active: true}}, nil
		}
		return model.Session{}, store.ErrUnauthorized
	}
	server.oidcUserFinder = func(_ context.Context, issuer, subject string) (model.User, error) {
		if issuer == cfg.Issuer && subject == "subject-mcp" {
			return ssoMember, nil
		}
		return model.User{}, store.ErrNotFound
	}
	server.transitAuthorizer = func(context.Context, model.User, string, string) (bool, error) { return true, nil }
	server.transitEncryptor = func(context.Context, string, string, string) (string, error) { return "vault:v1:x", nil }
	mux := http.NewServeMux()
	server.routes(mux)
	return server, mux
}

func ssoConfig(idp *fakeIdP) store.MCPOAuthConfig {
	return store.MCPOAuthConfig{Enabled: true, OIDCEnabled: true, Issuer: idp.server.URL,
		Resource: mcpTestResource, Scopes: []string{store.MCPScopeRead}}
}

func mcpWith(handler http.Handler, bearer, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "https://jikim.example/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", mcpProtocolLatest)
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func getMetadata(handler http.Handler, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "https://jikim.example"+path, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func toolText(t *testing.T, recorder *httptest.ResponseRecorder) (string, bool) {
	t.Helper()
	var response struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	if len(response.Result.Content) == 0 {
		return "", response.Result.IsError
	}
	return response.Result.Content[0].Text, response.Result.IsError
}

// Off by default: a deployment without SSO advertises nothing, and a token
// is refused exactly the way a bad key is — no new words, no pointer.
func TestMCPOAuthOffChangesNothing(t *testing.T) {
	idp := newFakeIdP(t)
	cfg := ssoConfig(idp)
	cfg.Enabled = false
	_, handler := mcpOAuthServer(t, cfg)

	for _, path := range []string{mcpOAuthMetadataPath, mcpOAuthMetadataPath + "/mcp"} {
		if got := getMetadata(handler, path); got.Code != http.StatusNotFound {
			t.Fatalf("metadata served with SSO off at %s: %d %s", path, got.Code, got.Body.String())
		}
	}
	refused := mcpWith(handler, idp.accessToken(t, mcpTestResource, nil), listToolsBody)
	if refused.Code != http.StatusUnauthorized || refused.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("token with SSO off: %d header=%q", refused.Code, refused.Header().Get("WWW-Authenticate"))
	}
	if !strings.Contains(refused.Body.String(), "세션이 만료되었거나 올바르지 않습니다") {
		t.Fatalf("a refusal with SSO off must read like the key refusal: %s", refused.Body.String())
	}
	if noToken := mcpWith(handler, "", listToolsBody); noToken.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("a 401 pointed at an authorization server that is not configured: %q", noToken.Header().Get("WWW-Authenticate"))
	}
	if key := mcpWith(handler, "hvs.personal-key", listToolsBody); key.Code != http.StatusOK {
		t.Fatalf("key refused: %d %s", key.Code, key.Body.String())
	}
}

// Switched on without an issuer to verify against, the switch is dormant.
func TestMCPOAuthDormantWithoutOIDC(t *testing.T) {
	idp := newFakeIdP(t)
	cfg := ssoConfig(idp)
	cfg.OIDCEnabled = false
	_, handler := mcpOAuthServer(t, cfg)
	if got := getMetadata(handler, mcpOAuthMetadataPath+"/mcp"); got.Code != http.StatusNotFound {
		t.Fatalf("metadata served while dormant: %d", got.Code)
	}
	if refused := mcpWith(handler, idp.accessToken(t, mcpTestResource, nil), listToolsBody); refused.Code != http.StatusUnauthorized || refused.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("dormant switch accepted a token or advertised: %d %q", refused.Code, refused.Header().Get("WWW-Authenticate"))
	}
}

// RFC 9728: the resource names itself and its authorization server as a bare
// document, and the 401 points at it — on the MCP path only.
func TestARefusedMCPClientIsToldWhereToSignIn(t *testing.T) {
	idp := newFakeIdP(t)
	_, handler := mcpOAuthServer(t, ssoConfig(idp))

	for _, path := range []string{mcpOAuthMetadataPath, mcpOAuthMetadataPath + "/mcp"} {
		got := getMetadata(handler, path)
		if got.Code != http.StatusOK {
			t.Fatalf("metadata %s: %d %s", path, got.Code, got.Body.String())
		}
		if got.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("metadata %s is not readable by a browser client", path)
		}
		var metadata struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
			BearerMethods        []string `json:"bearer_methods_supported"`
			Scopes               []string `json:"scopes_supported"`
			Data                 any      `json:"data"`
		}
		if err := json.Unmarshal(got.Body.Bytes(), &metadata); err != nil {
			t.Fatalf("decode %s: %v", got.Body.String(), err)
		}
		if metadata.Data != nil {
			t.Error("metadata is wrapped in the product envelope; an OAuth library will not find it")
		}
		if metadata.Resource != mcpTestResource {
			t.Errorf("resource %q", metadata.Resource)
		}
		if len(metadata.AuthorizationServers) != 1 || metadata.AuthorizationServers[0] != idp.server.URL {
			t.Errorf("authorization_servers %v", metadata.AuthorizationServers)
		}
		if len(metadata.BearerMethods) != 1 || metadata.BearerMethods[0] != "header" || len(metadata.Scopes) == 0 {
			t.Errorf("bearer_methods_supported %v scopes_supported %v", metadata.BearerMethods, metadata.Scopes)
		}
	}

	refusal := mcpWith(handler, "", listToolsBody)
	if refusal.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer: %d", refusal.Code)
	}
	header := refusal.Header().Get("WWW-Authenticate")
	want := `resource_metadata="https://jikim.example/.well-known/oauth-protected-resource/mcp"`
	if !strings.HasPrefix(header, `Bearer realm="jikim"`) || !strings.Contains(header, want) || strings.Contains(header, "error=") {
		t.Errorf("WWW-Authenticate %q does not point at the metadata", header)
	}
	if staleKey := mcpWith(handler, "hvs.revoked", listToolsBody); !strings.Contains(staleKey.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Errorf("a refused key on /mcp should still point at the metadata: %q", staleKey.Header().Get("WWW-Authenticate"))
	}

	// REST keeps its plain 401: a pointer there sends browsers the wrong way.
	rest := httptest.NewRequest(http.MethodGet, "https://jikim.example/api/v1/me", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rest)
	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("WWW-Authenticate") != "" {
		t.Errorf("REST 401 carries the MCP challenge: %d %q", recorder.Code, recorder.Header().Get("WWW-Authenticate"))
	}
}

// With no explicit resource, the identifier comes from the OIDC callback the
// administrator already wrote down — never from the Host header while that
// exists.
func TestMCPResourceDerivesFromCallbackBeforeHost(t *testing.T) {
	idp := newFakeIdP(t)
	cfg := ssoConfig(idp)
	cfg.Resource = ""
	cfg.RedirectURL = "https://vault.corp.example/api/v1/oidc/callback"
	_, handler := mcpOAuthServer(t, cfg)
	request := httptest.NewRequest(http.MethodGet, "http://evil.example"+mcpOAuthMetadataPath+"/mcp", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var metadata struct {
		Resource string `json:"resource"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &metadata); err != nil || metadata.Resource != "https://vault.corp.example/mcp" {
		t.Fatalf("resource %q (err %v), want the callback origin", metadata.Resource, err)
	}
	if got := mcpWith(handler, "", listToolsBody).Header().Get("WWW-Authenticate"); !strings.Contains(got, `resource_metadata="https://vault.corp.example/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("challenge %q", got)
	}
}

// A token Keycloak issued for this resource opens MCP for the account the web
// sign-in registered, with the scope ceiling the administrator set.
func TestAKeycloakTokenOpensMCPForARegisteredAccount(t *testing.T) {
	idp := newFakeIdP(t)
	_, handler := mcpOAuthServer(t, ssoConfig(idp))
	token := idp.accessToken(t, mcpTestResource, nil)

	opened := mcpWith(handler, token, listToolsBody)
	if opened.Code != http.StatusOK || !strings.Contains(opened.Body.String(), `"tools"`) {
		t.Fatalf("a token for this resource was refused: %d %s", opened.Code, opened.Body.String())
	}
	// Keycloak 26 puts only "account" in aud; the administrator lists the
	// client id and the token passes on azp, with no mapper at all.
	cfg := ssoConfig(idp)
	cfg.Audiences = []string{"claude-mcp"}
	_, listed := mcpOAuthServer(t, cfg)
	if got := mcpWith(listed, idp.accessToken(t, "account", nil), listToolsBody); got.Code != http.StatusOK {
		t.Fatalf("azp in the allowed list was refused: %d %s", got.Code, got.Body.String())
	}
	// A refused key still works the way it always did next to a token.
	if key := mcpWith(handler, "hvs.personal-key", listToolsBody); key.Code != http.StatusOK {
		t.Fatalf("key refused with SSO on: %d %s", key.Code, key.Body.String())
	}
}

// A token minted for another application in the realm must not open this
// one, and the refusal has to say what was seen and what to list.
func TestATokenForAnotherResourceIsRefusedWithTheFix(t *testing.T) {
	idp := newFakeIdP(t)
	_, handler := mcpOAuthServer(t, ssoConfig(idp))
	refused := mcpWith(handler, idp.accessToken(t, []string{"account", "https://other.example/mcp"}, map[string]any{"azp": "other-app"}), listToolsBody)
	if refused.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", refused.Code, refused.Body.String())
	}
	body := refused.Body.String()
	for _, want := range []string{"other-app", "https://other.example/mcp", mcpTestResource, "허용 대상"} {
		if !strings.Contains(body, want) {
			t.Errorf("refusal %s does not mention %q", body, want)
		}
	}
	if header := refused.Header().Get("WWW-Authenticate"); !strings.Contains(header, `error="invalid_token"`) {
		t.Errorf("challenge %q lacks error=invalid_token", header)
	}
}

// Everything a Keycloak realm can hand over that is not a bearer access token
// for this server: each is refused, none is a server fault.
func TestMCPOAuthRejectsTokensThatAreNotAccessTokensForUs(t *testing.T) {
	idp := newFakeIdP(t)
	_, handler := mcpOAuthServer(t, ssoConfig(idp))
	other := newFakeIdP(t)
	hmac := jose.SigningKey{Algorithm: jose.HS256, Key: []byte("0123456789abcdef0123456789abcdef")}

	cases := map[string]string{
		"expired":        idp.accessToken(t, mcpTestResource, map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}),
		"not yet valid":  idp.accessToken(t, mcpTestResource, map[string]any{"nbf": time.Now().Add(time.Hour).Unix()}),
		"other issuer":   other.accessToken(t, mcpTestResource, nil),
		"id token":       idp.accessToken(t, mcpTestResource, map[string]any{"typ": "ID"}),
		"bound with cnf": idp.accessToken(t, mcpTestResource, map[string]any{"cnf": map[string]any{"jkt": "thumb"}}),
		"no subject":     idp.accessToken(t, mcpTestResource, map[string]any{"sub": ""}),
		"hs256": signJWT(t, hmac, "realm-key", map[string]any{"iss": idp.server.URL, "aud": mcpTestResource, "sub": "subject-mcp",
			"exp": time.Now().Add(time.Hour).Unix(), "typ": "Bearer"}),
		"not a jwt": "not.a.jwt",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			refused := mcpWith(handler, token, listToolsBody)
			if refused.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", refused.Code, refused.Body.String())
			}
			if strings.Contains(refused.Body.String(), idp.server.URL) {
				t.Fatalf("refusal leaks the issuer address: %s", refused.Body.String())
			}
		})
	}
}

// The token can be perfect and still open nothing: signing in to the web is
// what registers an account, and a machine presenting a token is not that.
func TestMCPOAuthNeverCreatesAnAccount(t *testing.T) {
	idp := newFakeIdP(t)
	server, handler := mcpOAuthServer(t, ssoConfig(idp))
	lookups := 0
	server.oidcUserFinder = func(context.Context, string, string) (model.User, error) {
		lookups++
		return model.User{}, store.ErrNotFound
	}
	refused := mcpWith(handler, idp.accessToken(t, mcpTestResource, nil), listToolsBody)
	if refused.Code != http.StatusUnauthorized || !strings.Contains(refused.Body.String(), "먼저 웹으로") {
		t.Fatalf("status=%d body=%s", refused.Code, refused.Body.String())
	}
	if lookups != 1 {
		t.Fatalf("lookups=%d", lookups)
	}
	// A lookup that could not run is an outage, not a refusal.
	server.oidcUserFinder = func(context.Context, string, string) (model.User, error) {
		return model.User{}, driverFailure
	}
	outage := mcpWith(handler, idp.accessToken(t, mcpTestResource, nil), listToolsBody)
	if outage.Code != http.StatusInternalServerError || strings.Contains(outage.Body.String(), "SQLSTATE") {
		t.Fatalf("status=%d body=%s", outage.Code, outage.Body.String())
	}
}

// A valid token is an MCP credential only. REST, the OpenBao surface and the
// management API keep taking keys and sessions alone.
func TestMCPOAuthTokenOpensNothingButMCP(t *testing.T) {
	idp := newFakeIdP(t)
	_, handler := mcpOAuthServer(t, ssoConfig(idp))
	token := idp.accessToken(t, mcpTestResource, nil)
	for _, path := range []string{"/api/v1/me", "/api/v1/secrets", "/api/v1/settings"} {
		request := httptest.NewRequest(http.MethodGet, "https://jikim.example"+path, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s opened with an SSO token: %d %s", path, recorder.Code, recorder.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodGet, "https://jikim.example/v1/auth/token/lookup-self", nil)
	request.Header.Set("X-Vault-Token", token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusOK {
		t.Errorf("OpenBao surface opened with an SSO token: %s", recorder.Body.String())
	}
}

// The administrator's scope list is a ceiling: a token gets the read tools by
// default and Transit only when the list says so, narrowed further when the
// token itself carries this vocabulary.
func TestMCPOAuthScopesAreACeiling(t *testing.T) {
	idp := newFakeIdP(t)
	_, readOnly := mcpOAuthServer(t, ssoConfig(idp))
	token := idp.accessToken(t, mcpTestResource, nil)

	if text, isError := toolText(t, mcpWith(readOnly, token, encryptBody)); !isError || !strings.Contains(text, store.MCPScopeTransit) {
		t.Fatalf("transit ran under a read-only ceiling: isError=%v text=%q", isError, text)
	}
	if text, isError := toolText(t, mcpWith(readOnly, "hvs.personal-key", encryptBody)); isError {
		t.Fatalf("a key was narrowed by the SSO ceiling: %q", text)
	}

	cfg := ssoConfig(idp)
	cfg.Scopes = []string{store.MCPScopeRead, store.MCPScopeTransit}
	_, widened := mcpOAuthServer(t, cfg)
	if text, isError := toolText(t, mcpWith(widened, token, encryptBody)); isError {
		t.Fatalf("transit refused with mcp:transit granted: %q", text)
	}
	// Keycloak taught this vocabulary: the token asks for read only, so the
	// intersection drops Transit even though the ceiling allows it.
	narrowed := idp.accessToken(t, mcpTestResource, map[string]any{"scope": "openid mcp:read"})
	if text, isError := toolText(t, mcpWith(widened, narrowed, encryptBody)); !isError {
		t.Fatalf("token scope did not narrow the grant: %q", text)
	}
}

func TestMCPAudienceAndScopeRules(t *testing.T) {
	if !mcpAudienceAccepted(mcpTestResource, nil, []string{"account", mcpTestResource}, "") {
		t.Error("resource in aud must pass without a list")
	}
	if mcpAudienceAccepted(mcpTestResource, nil, []string{"account"}, "claude-mcp") {
		t.Error("account-only aud with an unlisted azp must not pass")
	}
	if !mcpAudienceAccepted(mcpTestResource, []string{"claude-mcp"}, []string{"account"}, "claude-mcp") {
		t.Error("listed azp must pass")
	}
	if mcpAudienceAccepted(mcpTestResource, []string{""}, []string{"account"}, "") {
		t.Error("an empty entry must never match an empty azp")
	}
	if got := mcpGrantedScopes([]string{"mcp:read", "mcp:transit"}, "openid profile"); len(got) != 2 {
		t.Errorf("a token without the vocabulary keeps the ceiling: %v", got)
	}
	if got := mcpGrantedScopes([]string{"mcp:read"}, "mcp:transit"); len(got) != 0 {
		t.Errorf("a token asking beyond the ceiling gets nothing extra: %v", got)
	}
	for _, token := range []string{"hvs.personal-key", "jks.session", "a.b", "a..b", ".b.c"} {
		if looksLikeJWT(token) {
			t.Errorf("%q must not look like a JWT", token)
		}
	}
}
