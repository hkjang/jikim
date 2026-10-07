package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
)

// Turning MCP SSO on is refused unless the OIDC sign-in that verifies its
// tokens is configured. That judgement needs the stored OIDC settings, and a
// read that could not run says nothing about whether they are there: reporting
// it as "turn OIDC on first" sends the administrator to a settings screen that
// is already correct, and the pgx error text that explains the refusal carries
// DSN and SQLSTATE detail straight into the response body.
type mcpOAuthSaveCase struct {
	name       string
	cfg        store.OIDCConfig
	err        error
	wantStatus int
	wantCode   string
}

func TestUpdateSettingsReportsOIDCReadOutageAsServerFault(t *testing.T) {
	for _, test := range []mcpOAuthSaveCase{
		{"storage outage", store.OIDCConfig{}, driverFailure, http.StatusInternalServerError, "internal_error"},
		{"never configured", store.OIDCConfig{}, store.ErrNotFound, http.StatusBadRequest, "mcp_oauth_requires_oidc"},
		{"oidc turned off", store.OIDCConfig{IssuerURL: "https://keycloak.example/realms/jikim"}, nil,
			http.StatusBadRequest, "mcp_oauth_requires_oidc"},
		{"oidc on without issuer", store.OIDCConfig{Enabled: true}, nil,
			http.StatusBadRequest, "mcp_oauth_requires_oidc"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := quietServer()
			logs := captureWebhookLog(server)
			server.sessionResolver = func(context.Context, string) (model.Session, error) {
				return model.Session{User: model.User{ID: "u1", Username: "admin", Role: "admin"}}, nil
			}
			server.oidcConfigLoader = func(context.Context) (store.OIDCConfig, error) {
				return test.cfg, test.err
			}
			request := httptest.NewRequest(http.MethodPatch, "https://jikim.example/api/v1/settings",
				strings.NewReader(`{"mcp":{"oauth":{"enabled":true}}}`))
			request.Header.Set("X-Vault-Token", "hvs.session-token")
			response := httptest.NewRecorder()
			// The production wrapper for PATCH /api/v1/settings (server.go routes).
			server.requestID(server.requireRoles(http.HandlerFunc(server.updateSettings), "admin")).
				ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status=%d (원하는 값 %d) body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			code, message := decodeErrorCode(t, response.Body.String())
			if code != test.wantCode {
				t.Fatalf("code=%q (원하는 값 %q) message=%q", code, test.wantCode, message)
			}
			if test.wantCode == "mcp_oauth_requires_oidc" {
				if message != "MCP SSO(OAuth)를 켜려면 Keycloak OIDC 연결이 켜져 있고 Issuer URL이 있어야 합니다" {
					t.Fatalf("기존 문구가 바뀌었습니다: %q", message)
				}
				return
			}
			if message != "요청을 처리하지 못했습니다" {
				t.Fatalf("message=%q", message)
			}
			assertNoDriverDetail(t, response.Body.String())
			assertSingleStoreErrorLog(t, logs)
		})
	}
}
