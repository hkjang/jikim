package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hkjang/jikim/internal/model"
)

// revokeServer wires the real requestID middleware and withAuth in front of a
// handler, with only the two seams a revocation path needs: the session lookup
// and the revocation itself. Everything else stays production code.
func revokeServer(t *testing.T, authSource string, revokeErr error) (*Server, *syncBuffer, *[]string) {
	t.Helper()
	server := quietServer()
	logs := captureWebhookLog(server)
	server.sessionResolver = func(context.Context, string) (model.Session, error) {
		return model.Session{User: model.User{ID: "user-1", Role: "admin", AuthSource: authSource}}, nil
	}
	revoked := &[]string{}
	server.sessionRevoker = func(_ context.Context, token string) error {
		*revoked = append(*revoked, token)
		return revokeErr
	}
	return server, logs, revoked
}

func revokeWarnings(t *testing.T, logs *syncBuffer) []map[string]any {
	t.Helper()
	var warnings []map[string]any
	for _, record := range webhookWarnings(t, logs) {
		if record["level"] == "WARN" {
			warnings = append(warnings, record)
		}
	}
	return warnings
}

// A revocation that failed leaves the token valid until its TTL expires, so the
// only trace the operator can get is a log line. The response contract must not
// change: the client still has to drop its cookie.
func assertRevocationWarning(t *testing.T, logs *syncBuffer, source string) {
	t.Helper()
	warnings := revokeWarnings(t, logs)
	if len(warnings) != 1 {
		t.Fatalf("경고가 한 줄이 아닙니다: %v", warnings)
	}
	warning := warnings[0]
	if warning["source"] != source {
		t.Fatalf("source=%v (원하는 값 %q)", warning["source"], source)
	}
	if message, _ := warning["error"].(string); !strings.Contains(message, "SQLSTATE 28P01") {
		t.Fatalf("error 필드에 실패 원인이 없습니다: %v", warning["error"])
	}
	if requestID, _ := warning["request_id"].(string); strings.TrimSpace(requestID) == "" {
		t.Fatalf("request_id 가 비어 있습니다: %v", warning)
	}
	if strings.Contains(logs.String(), "hvs.session-token") {
		t.Fatalf("로그에 세션 토큰이 들어 있습니다: %s", logs.String())
	}
}

func assertSessionCookieCleared(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name != "jikim_session" {
			continue
		}
		if cookie.MaxAge >= 0 {
			t.Fatalf("세션 쿠키가 삭제되지 않았습니다: MaxAge=%d", cookie.MaxAge)
		}
		return
	}
	t.Fatalf("jikim_session 쿠키 삭제 지시가 없습니다: %v", response.Result().Cookies())
}

func TestLogoutWarnsWhenTokenRevocationFails(t *testing.T) {
	server, logs, revoked := revokeServer(t, "local", driverFailure)
	response := httptest.NewRecorder()
	server.requestID(server.withAuth(http.HandlerFunc(server.logout))).
		ServeHTTP(response, tokenRequest("POST", "/api/v1/auth/logout"))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(*revoked) != 1 {
		t.Fatalf("폐기 호출 횟수=%d", len(*revoked))
	}
	assertSessionCookieCleared(t, response)
	assertRevocationWarning(t, logs, "logout")
}

func TestLogoutStaysQuietWhenTokenRevocationSucceeds(t *testing.T) {
	server, logs, revoked := revokeServer(t, "local", nil)
	response := httptest.NewRecorder()
	server.requestID(server.withAuth(http.HandlerFunc(server.logout))).
		ServeHTTP(response, tokenRequest("POST", "/api/v1/auth/logout"))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(*revoked) != 1 {
		t.Fatalf("폐기 호출 횟수=%d", len(*revoked))
	}
	assertSessionCookieCleared(t, response)
	if warnings := revokeWarnings(t, logs); len(warnings) != 0 {
		t.Fatalf("성공 경로에 경고가 있습니다: %v", warnings)
	}
}

func TestBaoRevokeSelfWarnsWhenTokenRevocationFails(t *testing.T) {
	server, logs, revoked := revokeServer(t, "local", driverFailure)
	response := httptest.NewRecorder()
	server.requestID(server.withAuth(http.HandlerFunc(server.baoRevokeSelf))).
		ServeHTTP(response, tokenRequest("POST", "/v1/auth/token/revoke-self"))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(*revoked) != 1 {
		t.Fatalf("폐기 호출 횟수=%d", len(*revoked))
	}
	assertRevocationWarning(t, logs, "openbao_revoke_self")
}

// A session that did not come from OIDC never reaches the provider lookup, so
// this exercises oidcLogout's revocation without a configured issuer.
func TestOIDCLogoutWarnsWhenTokenRevocationFails(t *testing.T) {
	server, logs, revoked := revokeServer(t, "local", driverFailure)
	response := httptest.NewRecorder()
	server.requestID(server.withAuth(http.HandlerFunc(server.oidcLogout))).
		ServeHTTP(response, tokenRequest("POST", "/api/v1/auth/oidc/logout"))

	if response.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if location := response.Header().Get("Location"); location != "/login" {
		t.Fatalf("Location=%q", location)
	}
	if len(*revoked) != 1 {
		t.Fatalf("폐기 호출 횟수=%d", len(*revoked))
	}
	assertSessionCookieCleared(t, response)
	assertRevocationWarning(t, logs, "oidc_logout")
}
