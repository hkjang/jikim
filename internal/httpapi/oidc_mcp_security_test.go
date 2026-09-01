package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
)

func TestOIDCRoleRequiresDedicatedExactMapping(t *testing.T) {
	tests := []struct {
		name   string
		claims map[string]any
		want   string
	}{
		{name: "bare admin denied", claims: map[string]any{"roles": []any{"admin"}}, want: ""},
		{name: "arbitrary admin group denied", claims: map[string]any{"roles": []any{"/org/admin"}}, want: ""},
		{name: "prefixed group path is not exact", claims: map[string]any{"roles": []any{"/org/jikim-admin"}}, want: ""},
		{name: "uppercase dedicated role is not exact", claims: map[string]any{"roles": []any{"JIKIM-ADMIN"}}, want: ""},
		{name: "whitespace dedicated role is not exact", claims: map[string]any{"roles": []any{" jikim-admin "}}, want: ""},
		{name: "dedicated admin accepted", claims: map[string]any{"roles": []any{"jikim-admin"}}, want: "admin"},
		{name: "dedicated auditor accepted", claims: map[string]any{"roles": "jikim-auditor"}, want: "auditor"},
		{name: "conflicting mappings become least privilege", claims: map[string]any{"roles": []any{"jikim-admin", "jikim-manager"}}, want: "user"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := oidcRole(test.claims, "roles"); got != test.want {
				t.Fatalf("oidcRole()=%q want %q", got, test.want)
			}
		})
	}

	claims := map[string]any{
		"realm":  map[string]any{"roles": []any{"jikim-admin"}},
		"groups": []any{"jikim-auditor"},
	}
	if got, sync := synchronizedOIDCRoleClaims(claims, "realm.roles", "groups"); !sync || got != "user" {
		t.Fatalf("conflicting role/group claims must downgrade: role=%q sync=%v", got, sync)
	}
	if got, sync := synchronizedOIDCRoleClaims(map[string]any{"roles": []any{"admin"}}, "roles", ""); !sync || got != "user" {
		t.Fatalf("unrecognized configured claim must downgrade: role=%q sync=%v", got, sync)
	}
}

func TestOIDCProviderErrorRedirectsToRelativeSPA(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodGet, "https://jikim.example/api/v1/oidc/callback?error=access_denied&error_description=provider-detail", nil)
	response := httptest.NewRecorder()
	server.oidcCallback(response, request)

	if response.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	location := response.Header().Get("Location")
	target, err := url.Parse(location)
	if err != nil || target.IsAbs() || target.Host != "" || target.Path != "/oidc/callback" {
		t.Fatalf("unsafe callback location %q", location)
	}
	if target.Query().Get("error") != "oidc_provider_error" || strings.Contains(location, "provider-detail") {
		t.Fatalf("provider detail leaked or safe code missing: %q", location)
	}
	if cookie := response.Header().Get("Set-Cookie"); !strings.Contains(cookie, "jikim_oidc_state=") || !strings.Contains(cookie, "Max-Age=0") {
		t.Fatalf("OIDC state cookie was not cleared: %q", cookie)
	}
}

func TestOIDCFrontendRedirectNeverRetainsAbsoluteOrigin(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://jikim.example/api/v1/oidc/login", nil)
	for _, candidate := range []string{
		"/oidc/callback",
		"https://jikim.example/oidc/callback",
	} {
		got, ok := normalizeFrontendRedirect(request, candidate)
		if !ok || got != "/oidc/callback" {
			t.Errorf("normalizeFrontendRedirect(%q)=(%q,%v)", candidate, got, ok)
		}
	}
	for _, candidate := range []string{"//attacker.example/oidc/callback", "https://attacker.example/oidc/callback?error=injected", "https://jikim.example/other", "javascript:alert(1)"} {
		if got, ok := normalizeFrontendRedirect(request, candidate); ok || got != "" {
			t.Errorf("unsafe redirect accepted: %q -> %q", candidate, got)
		}
	}
	if redirect, ok := validOIDCBackendRedirect("https://jikim.example/api/v1/oidc/callback"); !ok || redirect == "" {
		t.Fatal("configured absolute backend callback was rejected")
	}
	for _, candidate := range []string{"", "/api/v1/oidc/callback", "https://jikim.example/other", "https://user@jikim.example/api/v1/oidc/callback"} {
		if redirect, ok := validOIDCBackendRedirect(candidate); ok || redirect != "" {
			t.Errorf("unsafe backend redirect accepted: %q", candidate)
		}
	}
}

func TestOIDCLogoutURLUsesOnlyConfiguredCallbackOrigin(t *testing.T) {
	got, err := oidcLogoutURL(
		"https://keycloak.example/realms/security/protocol/openid-connect/logout?existing=1",
		"jikim-client",
		"https://jikim.example/api/v1/oidc/callback",
	)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(got)
	if target.Query().Get("client_id") != "jikim-client" || target.Query().Get("existing") != "1" {
		t.Fatalf("logout query missing: %q", got)
	}
	if target.Query().Get("post_logout_redirect_uri") != "https://jikim.example/login" {
		t.Fatalf("unexpected post logout redirect: %q", got)
	}
	withoutConfiguredOrigin, err := oidcLogoutURL("https://keycloak.example/logout", "jikim-client", "/api/v1/oidc/callback")
	if err != nil {
		t.Fatal(err)
	}
	withoutTarget, _ := url.Parse(withoutConfiguredOrigin)
	if withoutTarget.Query().Has("post_logout_redirect_uri") {
		t.Fatalf("relative/untrusted callback became an absolute logout redirect: %q", withoutConfiguredOrigin)
	}
}

func TestMCPNegotiatesSupportedProtocolVersions(t *testing.T) {
	server := &Server{}
	for _, requested := range []string{mcpProtocolLatest, mcpProtocolLegacy, "2024-11-05"} {
		t.Run(requested, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + requested + `","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
			response := performMCP(t, server, body, "", "")
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var envelope struct {
				Result struct {
					ProtocolVersion string `json:"protocolVersion"`
				} `json:"result"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			want := requested
			if !supportedMCPProtocols[requested] {
				want = mcpProtocolLatest
			}
			if envelope.Result.ProtocolVersion != want {
				t.Fatalf("negotiated=%q want %q", envelope.Result.ProtocolVersion, want)
			}
		})
	}
}

func TestMCPToolSchemasAreValidObjects(t *testing.T) {
	encoded, err := json.Marshal(mcpTools())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"required":null`) {
		t.Fatalf("tool schema contains invalid null required: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"name":"access.check"`) || strings.Contains(string(encoded), `"user_id"`) {
		t.Fatalf("self-only access.check schema missing or exposes user override: %s", encoded)
	}
}

func TestMCPTransportGuardsAndNotifications(t *testing.T) {
	server := &Server{}
	toolsList := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`

	missingVersion := performMCP(t, server, toolsList, "", "")
	if missingVersion.Code != http.StatusBadRequest || !strings.Contains(missingVersion.Body.String(), "MCP-Protocol-Version") {
		t.Fatalf("missing protocol header accepted: status=%d body=%s", missingVersion.Code, missingVersion.Body.String())
	}

	wrongOrigin := performMCP(t, server, toolsList, mcpProtocolLatest, "https://attacker.example")
	if wrongOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin request accepted: status=%d body=%s", wrongOrigin.Code, wrongOrigin.Body.String())
	}

	sameOrigin := performMCP(t, server, toolsList, mcpProtocolLatest, "https://jikim.example")
	if sameOrigin.Code != http.StatusOK || !strings.Contains(sameOrigin.Body.String(), "dashboard.get") {
		t.Fatalf("same-origin tools/list failed: status=%d body=%s", sameOrigin.Code, sameOrigin.Body.String())
	}

	for _, notification := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/custom","params":{"ignored":true}}`,
	} {
		response := performMCP(t, server, notification, mcpProtocolLatest, "")
		if response.Code != http.StatusAccepted || response.Body.Len() != 0 {
			t.Fatalf("notification produced a response: status=%d body=%q", response.Code, response.Body.String())
		}
	}

	request := httptest.NewRequest(http.MethodGet, "https://jikim.example/mcp", nil)
	response := httptest.NewRecorder()
	server.mcpGET(response, request)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET contract: status=%d allow=%q", response.Code, response.Header().Get("Allow"))
	}

	missingContentType := httptest.NewRequest(http.MethodPost, "https://jikim.example/mcp", strings.NewReader(toolsList))
	missingContentType.Header.Set("MCP-Protocol-Version", mcpProtocolLatest)
	missingContentType.Header.Set("Accept", "application/json, text/event-stream")
	unsupportedResponse := httptest.NewRecorder()
	server.mcp(unsupportedResponse, missingContentType)
	if unsupportedResponse.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("missing Content-Type accepted: status=%d body=%s", unsupportedResponse.Code, unsupportedResponse.Body.String())
	}

	jsonOnly := httptest.NewRequest(http.MethodPost, "https://jikim.example/mcp", strings.NewReader(toolsList))
	jsonOnly.Header.Set("Content-Type", "application/json; charset=utf-8")
	jsonOnly.Header.Set("Accept", "application/json")
	jsonOnly.Header.Set("MCP-Protocol-Version", mcpProtocolLatest)
	notAcceptableResponse := httptest.NewRecorder()
	server.mcp(notAcceptableResponse, jsonOnly)
	if notAcceptableResponse.Code != http.StatusNotAcceptable {
		t.Fatalf("incomplete Accept accepted: status=%d body=%s", notAcceptableResponse.Code, notAcceptableResponse.Body.String())
	}

	initializeWithoutAccept := httptest.NewRequest(http.MethodPost, "https://jikim.example/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy","version":"1"}}}`))
	initializeWithoutAccept.Header.Set("Content-Type", "application/json")
	initializeResponse := httptest.NewRecorder()
	server.mcp(initializeResponse, initializeWithoutAccept)
	if initializeResponse.Code != http.StatusNotAcceptable {
		t.Fatalf("initialize without dual Accept was accepted: status=%d body=%s", initializeResponse.Code, initializeResponse.Body.String())
	}
}

func TestMCPInvalidJSONAndParamsUseJSONRPCErrors(t *testing.T) {
	server := &Server{}
	malformed := performMCP(t, server, `{"jsonrpc":`, mcpProtocolLatest, "")
	if malformed.Code != http.StatusBadRequest || !strings.Contains(malformed.Body.String(), `"code":-32700`) {
		t.Fatalf("malformed JSON response: status=%d body=%s", malformed.Code, malformed.Body.String())
	}

	missingInitializeFields := performMCP(t, server, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`, "", "")
	if missingInitializeFields.Code != http.StatusOK || !strings.Contains(missingInitializeFields.Body.String(), `"code":-32602`) {
		t.Fatalf("invalid initialize params response: %s", missingInitializeFields.Body.String())
	}

	wrongToolType := performMCP(t, server, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"secrets.metadata","arguments":{"path":7}}}`, mcpProtocolLatest, "")
	if wrongToolType.Code != http.StatusOK || !strings.Contains(wrongToolType.Body.String(), `"code":-32602`) {
		t.Fatalf("invalid tool args response: %s", wrongToolType.Body.String())
	}

	wrongListParams := performMCP(t, server, `{"jsonrpc":"2.0","id":3,"method":"tools/list","params":[]}`, mcpProtocolLatest, "")
	if wrongListParams.Code != http.StatusOK || !strings.Contains(wrongListParams.Body.String(), `"code":-32602`) {
		t.Fatalf("invalid tools/list params response: %s", wrongListParams.Body.String())
	}
}

func TestMCPSecretMetadataRequiresReadCapability(t *testing.T) {
	capability := ""
	server := &Server{secretAuthorizer: func(_ context.Context, _ model.User, _ string, requested string) (bool, error) {
		capability = requested
		return false, nil
	}}
	body := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"secrets.metadata","arguments":{"path":"prod/payment/database"}}}`
	response := performAuthenticatedMCP(t, server, body)
	if capability != "read" {
		t.Fatalf("secrets.metadata checked %q capability, want read", capability)
	}
	if !strings.Contains(response.Body.String(), `"isError":true`) {
		t.Fatalf("denied metadata call did not return a tool error: %s", response.Body.String())
	}
}

func TestMCPTransitDecryptAuditIsIdentifiedAndFailClosed(t *testing.T) {
	var audited model.AuditEvent
	server := &Server{
		transitAuthorizer: func(context.Context, model.User, string, string) (bool, error) { return true, nil },
		transitDecryptor: func(context.Context, string, string) (string, error) {
			return "c2VjcmV0LXBsYWludGV4dA==", nil
		},
		auditRecorder: func(_ context.Context, event model.AuditEvent) error {
			audited = event
			return errors.New("audit unavailable")
		},
	}
	body := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"transit.decrypt","arguments":{"key":"customer","ciphertext":"vault:v1:cipher"}}}`
	response := performAuthenticatedMCP(t, server, body)
	if strings.Contains(response.Body.String(), "c2VjcmV0LXBsYWludGV4dA==") || !strings.Contains(response.Body.String(), "감사 로그") {
		t.Fatalf("plaintext was disclosed after audit failure: %s", response.Body.String())
	}
	if audited.Action != "mcp.transit.decrypt" || audited.Resource != "transit/customer" || audited.Details["tool"] != "transit.decrypt" {
		t.Fatalf("tool-level audit was not identified: %#v", audited)
	}
}

func TestMCPAccessCheckIsRestrictedToCurrentSession(t *testing.T) {
	server := &Server{store: &store.Store{}}
	body := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"access.check","arguments":{"path":"prod/payment","capability":"read"}}}`
	request := httptest.NewRequest(http.MethodPost, "https://jikim.example/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", mcpProtocolLatest)
	request = request.WithContext(context.WithValue(request.Context(), sessionKey, model.Session{User: model.User{
		ID: "admin-1", Username: "admin", Role: "admin", Active: true,
	}}))
	response := httptest.NewRecorder()
	server.mcp(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"allowed":true`) ||
		!strings.Contains(response.Body.String(), `"user_id":"admin-1"`) {
		t.Fatalf("self access check failed: status=%d body=%s", response.Code, response.Body.String())
	}

	withOtherUser := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"access.check","arguments":{"user_id":"victim","path":"prod/payment","capability":"read"}}}`
	denied := performAuthenticatedMCP(t, server, withOtherUser)
	if !strings.Contains(denied.Body.String(), `"code":-32602`) {
		t.Fatalf("access.check accepted user_id override: %s", denied.Body.String())
	}
}

func performMCP(t *testing.T, server *Server, body, protocol, origin string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "https://jikim.example/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if protocol != "" {
		request.Header.Set("MCP-Protocol-Version", protocol)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	server.mcp(response, request)
	return response
}

func performAuthenticatedMCP(t *testing.T, server *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "https://jikim.example/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", mcpProtocolLatest)
	request = request.WithContext(context.WithValue(request.Context(), sessionKey, model.Session{User: model.User{
		ID: "user-1", Username: "tester", Role: "user", Active: true,
	}}))
	response := httptest.NewRecorder()
	server.mcp(response, request)
	return response
}
