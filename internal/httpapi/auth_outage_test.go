package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
)

func quietServer() *Server {
	return &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func tokenRequest(method, path string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("X-Vault-Token", "hvs.session-token")
	return request
}

// A session lookup that could not run says nothing about the token. Reporting
// it as an expired session logs a valid user out and hides the outage from
// retry logic, and the driver detail must never reach the client.
func TestSessionLookupOutageIsServerFault(t *testing.T) {
	server := quietServer()
	server.sessionResolver = func(context.Context, string) (model.Session, error) {
		return model.Session{}, driverFailure
	}
	response := httptest.NewRecorder()
	server.withAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran with an unresolved session")
	})).ServeHTTP(response, tokenRequest("GET", "/api/v1/me"))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "postgres.internal") || strings.Contains(response.Body.String(), "SQLSTATE") {
		t.Fatalf("driver detail leaked to the client: %s", response.Body.String())
	}
}

func TestExpiredSessionStaysUnauthorized(t *testing.T) {
	server := quietServer()
	server.sessionResolver = func(context.Context, string) (model.Session, error) {
		return model.Session{}, store.ErrUnauthorized
	}
	response := httptest.NewRecorder()
	server.withAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran with an expired session")
	})).ServeHTTP(response, tokenRequest("GET", "/api/v1/me"))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

// The guest probe answers "not authenticated" for a token that no longer
// resolves. An outage has no answer, so returning a guest would park the SPA on
// the login screen while the database is down.
func TestSessionStatusSeparatesGuestFromOutage(t *testing.T) {
	server := quietServer()
	server.sessionResolver = func(context.Context, string) (model.Session, error) {
		return model.Session{}, driverFailure
	}
	response := httptest.NewRecorder()
	server.sessionStatus(response, tokenRequest("GET", "/api/v1/session"))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("outage status=%d body=%s", response.Code, response.Body.String())
	}

	server.sessionResolver = func(context.Context, string) (model.Session, error) {
		return model.Session{}, store.ErrUnauthorized
	}
	response = httptest.NewRecorder()
	server.sessionStatus(response, tokenRequest("GET", "/api/v1/session"))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"authenticated":false`) {
		t.Fatalf("guest status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBaoSessionFailureSeparatesDenialFromServerFault(t *testing.T) {
	status, message := baoSessionFailure(driverFailure)
	if status != http.StatusInternalServerError || message != "failed to look up token" {
		t.Fatalf("outage status=%d message=%q", status, message)
	}
	status, message = baoSessionFailure(store.ErrUnauthorized)
	if status != http.StatusForbidden || message != "permission denied" {
		t.Fatalf("denial status=%d message=%q", status, message)
	}
}

func TestBaoAuthOutageIsServerFault(t *testing.T) {
	server := quietServer()
	server.sessionResolver = func(context.Context, string) (model.Session, error) {
		return model.Session{}, driverFailure
	}
	response := httptest.NewRecorder()
	server.withBaoAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran with an unresolved token")
	})).ServeHTTP(response, tokenRequest("GET", "/v1/secret/data/app"))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "postgres.internal") || strings.Contains(response.Body.String(), "SQLSTATE") {
		t.Fatalf("driver detail leaked to the client: %s", response.Body.String())
	}
}

// The login rate limiter counts rejected credentials. An outage is not a
// rejected credential: counting it locks the account out of the login window
// for a failure the user did not cause.
func TestLoginOutageIsNotCountedAsFailedAttempt(t *testing.T) {
	for _, openBao := range []bool{false, true} {
		server := quietServer()
		server.loginLimiter = newLoginRateLimiter()
		server.authenticator = func(context.Context, string, string) (model.User, error) {
			return model.User{}, driverFailure
		}
		response := httptest.NewRecorder()
		var request *http.Request
		if openBao {
			request = httptest.NewRequest("POST", "/v1/auth/userpass/login/hyeon", strings.NewReader(`{"password":"pw"}`))
			request.SetPathValue("username", "hyeon")
			server.baoUserpassLogin(response, request)
		} else {
			request = httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"hyeon","password":"pw"}`))
			server.login(response, request)
		}
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("openbao=%t status=%d body=%s", openBao, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "postgres.internal") || strings.Contains(response.Body.String(), "SQLSTATE") {
			t.Fatalf("openbao=%t driver detail leaked: %s", openBao, response.Body.String())
		}
		if blocked, _ := server.loginLimiter.blocked(loginRateKey(request, "hyeon")); blocked {
			t.Fatalf("openbao=%t outage blocked the account", openBao)
		}
		if len(server.loginLimiter.failures) != 0 {
			t.Fatalf("openbao=%t outage recorded a failed attempt: %#v", openBao, server.loginLimiter.failures)
		}
	}
}

func TestRejectedCredentialStillCountsAsFailedAttempt(t *testing.T) {
	server := quietServer()
	server.loginLimiter = newLoginRateLimiter()
	server.authenticator = func(context.Context, string, string) (model.User, error) {
		return model.User{}, store.ErrUnauthorized
	}
	request := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"hyeon","password":"pw"}`))
	response := httptest.NewRecorder()
	server.login(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(server.loginLimiter.failures) != 1 {
		t.Fatalf("rejected credential was not counted: %#v", server.loginLimiter.failures)
	}
}
