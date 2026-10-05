package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hkjang/jikim/internal/store"
)

// oidcOutageCase is one answer from the OIDC settings loader and the reply the
// client must get for it. A loader that could not run says nothing about
// whether SSO is configured, so it may not be reported as "not configured".
type oidcOutageCase struct {
	name       string
	cfg        store.OIDCConfig
	err        error
	wantStatus int
	wantCode   string
}

func decodeErrorCode(t *testing.T, body string) (string, string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("응답을 읽지 못했습니다: %v (%s)", err, body)
	}
	return envelope.Error.Code, envelope.Error.Message
}

func assertNoDriverDetail(t *testing.T, body string) {
	t.Helper()
	for _, fragment := range []string{"postgres.internal", "SQLSTATE", "jikim_app"} {
		if strings.Contains(body, fragment) {
			t.Fatalf("드라이버 원문이 응답으로 샜습니다: %s", body)
		}
	}
}

// A settings read that failed is a storage outage, not an administrator who
// never turned SSO on. Reporting it as oidc_disabled sends the operator back to
// a settings screen that is already correct and tells the client not to retry.
func TestOIDCLoginReportsConfigOutageAsServerFault(t *testing.T) {
	for _, test := range []oidcOutageCase{
		{"storage outage", store.OIDCConfig{}, driverFailure, http.StatusInternalServerError, "internal_error"},
		{"never configured", store.OIDCConfig{}, store.ErrNotFound, http.StatusServiceUnavailable, "oidc_disabled"},
		{"turned off", store.OIDCConfig{Enabled: false}, nil, http.StatusServiceUnavailable, "oidc_disabled"},
		{"enabled but incomplete", store.OIDCConfig{Enabled: true}, nil, http.StatusServiceUnavailable, "oidc_disabled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := quietServer()
			logs := captureWebhookLog(server)
			server.oidcConfigLoader = func(context.Context) (store.OIDCConfig, error) {
				return test.cfg, test.err
			}
			response := httptest.NewRecorder()
			server.requestID(http.HandlerFunc(server.oidcLogin)).ServeHTTP(response,
				httptest.NewRequest(http.MethodGet, "https://jikim.example/api/v1/oidc/login", nil))

			if response.Code != test.wantStatus {
				t.Fatalf("status=%d (원하는 값 %d) body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			code, message := decodeErrorCode(t, response.Body.String())
			if code != test.wantCode {
				t.Fatalf("code=%q (원하는 값 %q) message=%q", code, test.wantCode, message)
			}
			if test.wantCode == "oidc_disabled" && message != "OIDC 로그인이 설정되지 않았습니다" {
				t.Fatalf("기존 문구가 바뀌었습니다: %q", message)
			}
			if test.wantCode == "internal_error" {
				if message != "요청을 처리하지 못했습니다" {
					t.Fatalf("message=%q", message)
				}
				assertNoDriverDetail(t, response.Body.String())
				assertSingleStoreErrorLog(t, logs)
			}
		})
	}
}

// The callback makes the same judgement after the state cookie has already been
// accepted, so a storage outage there must not be reported as SSO being off.
func TestOIDCCallbackReportsConfigOutageAsServerFault(t *testing.T) {
	for _, test := range []oidcOutageCase{
		{"storage outage", store.OIDCConfig{}, driverFailure, http.StatusInternalServerError, "internal_error"},
		{"never configured", store.OIDCConfig{}, store.ErrNotFound, http.StatusServiceUnavailable, "oidc_disabled"},
		{"turned off", store.OIDCConfig{Enabled: false}, nil, http.StatusServiceUnavailable, "oidc_disabled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := quietServer()
			logs := captureWebhookLog(server)
			server.oidcStateOpener = sealedStateOpener(t, "sealed",
				oidcState{State: "abc", IssuedAt: time.Now().UTC()})
			server.oidcConfigLoader = func(context.Context) (store.OIDCConfig, error) {
				return test.cfg, test.err
			}
			request := httptest.NewRequest(http.MethodGet,
				"https://jikim.example/api/v1/oidc/callback?state=abc&code=auth-code", nil)
			request.AddCookie(&http.Cookie{Name: "jikim_oidc_state", Value: "sealed"})
			response := httptest.NewRecorder()
			server.requestID(http.HandlerFunc(server.oidcCallback)).ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status=%d (원하는 값 %d) body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			code, message := decodeErrorCode(t, response.Body.String())
			if code != test.wantCode {
				t.Fatalf("code=%q (원하는 값 %q) message=%q", code, test.wantCode, message)
			}
			if test.wantCode == "oidc_disabled" && message != "OIDC 로그인이 비활성화되었습니다" {
				t.Fatalf("기존 문구가 바뀌었습니다: %q", message)
			}
			if test.wantCode == "internal_error" {
				if message != "요청을 처리하지 못했습니다" {
					t.Fatalf("message=%q", message)
				}
				assertNoDriverDetail(t, response.Body.String())
				assertSingleStoreErrorLog(t, logs)
			}
		})
	}
}

func assertSingleStoreErrorLog(t *testing.T, logs *syncBuffer) {
	t.Helper()
	var errorRecords []map[string]any
	for _, record := range webhookWarnings(t, logs) {
		if record["level"] == "ERROR" {
			errorRecords = append(errorRecords, record)
		}
	}
	if len(errorRecords) != 1 {
		t.Fatalf("ERROR 가 한 줄이 아닙니다: %v", errorRecords)
	}
	if detail, _ := errorRecords[0]["error"].(string); !strings.Contains(detail, "SQLSTATE 28P01") {
		t.Fatalf("로그에 실패 원인이 없습니다: %v", errorRecords[0]["error"])
	}
	if requestID, _ := errorRecords[0]["request_id"].(string); strings.TrimSpace(requestID) == "" {
		t.Fatalf("request_id 가 비어 있습니다: %v", errorRecords[0])
	}
}
