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

// oidcExchangeCase is one pair of answers from the two storage reads the code
// exchange makes, with the reply the client must get. A read that could not run
// says nothing about whether the login code expired or the account is disabled,
// so it may not be reported as either.
type oidcExchangeCase struct {
	name         string
	consumeErr   error
	user         model.User
	userErr      error
	wantStatus   int
	wantCode     string
	wantMessage  string
	wantErrorLog bool
}

func TestOIDCExchangeReportsStoreOutageAsServerFault(t *testing.T) {
	for _, test := range []oidcExchangeCase{
		{
			name:         "login code read outage",
			consumeErr:   driverFailure,
			wantStatus:   http.StatusInternalServerError,
			wantCode:     "internal_error",
			wantMessage:  "요청을 처리하지 못했습니다",
			wantErrorLog: true,
		},
		{
			name:         "user read outage",
			userErr:      driverFailure,
			wantStatus:   http.StatusInternalServerError,
			wantCode:     "internal_error",
			wantMessage:  "요청을 처리하지 못했습니다",
			wantErrorLog: true,
		},
		{
			name:        "login code expired or already used",
			consumeErr:  store.ErrUnauthorized,
			wantStatus:  http.StatusUnauthorized,
			wantCode:    "invalid_login_code",
			wantMessage: "로그인 코드가 만료되었거나 이미 사용되었습니다",
		},
		{
			name:        "user no longer exists",
			userErr:     store.ErrNotFound,
			wantStatus:  http.StatusUnauthorized,
			wantCode:    "inactive_user",
			wantMessage: "사용자 계정이 비활성화되었습니다",
		},
		{
			name:        "user deactivated",
			user:        model.User{ID: "u-1", Active: false},
			wantStatus:  http.StatusUnauthorized,
			wantCode:    "inactive_user",
			wantMessage: "사용자 계정이 비활성화되었습니다",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := quietServer()
			logs := captureWebhookLog(server)
			server.oidcCodeConsumer = func(context.Context, string) (string, error) {
				return "u-1", test.consumeErr
			}
			server.userLoader = func(context.Context, string) (model.User, error) {
				return test.user, test.userErr
			}
			request := httptest.NewRequest(http.MethodPost, "https://jikim.example/api/v1/oidc/exchange",
				strings.NewReader(`{"code":"login-code"}`))
			response := httptest.NewRecorder()
			server.requestID(http.HandlerFunc(server.oidcExchange)).ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status=%d (원하는 값 %d) body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			code, message := decodeErrorCode(t, response.Body.String())
			if code != test.wantCode {
				t.Fatalf("code=%q (원하는 값 %q) message=%q", code, test.wantCode, message)
			}
			if message != test.wantMessage {
				t.Fatalf("message=%q (원하는 값 %q)", message, test.wantMessage)
			}
			assertNoDriverDetail(t, response.Body.String())
			if test.wantErrorLog {
				assertSingleStoreErrorLog(t, logs)
				return
			}
			assertNoStoreErrorLog(t, logs)
		})
	}
}

// A refusal the operator cannot act on must not reach the ERROR log: an expired
// code or a disabled account is the handler working as designed.
func assertNoStoreErrorLog(t *testing.T, logs *syncBuffer) {
	t.Helper()
	for _, record := range webhookWarnings(t, logs) {
		if record["level"] == "ERROR" {
			t.Fatalf("거절에 ERROR 가 남았습니다: %v", record)
		}
	}
}
