package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/store"
)

func failLogin(t *testing.T, server *Server, username string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"pw"}`, username)))
	response := httptest.NewRecorder()
	server.login(response, request)
	return response
}

// The failure table is bounded, and going over the bound evicts entries. A
// lockout that can be evicted is not a lockout: the same client that locked a
// target account could flood the table with failures against other usernames
// until the target's entry was pushed out and the five attempts came back.
func TestLoginLockoutSurvivesFailureFloodFromSameAddress(t *testing.T) {
	server := quietServer()
	server.loginLimiter = newLoginRateLimiter()
	server.loginLimiter.capacity = 100
	server.authenticator = func(context.Context, string, string) (model.User, error) {
		return model.User{}, store.ErrUnauthorized
	}

	for i := 0; i < server.loginLimiter.limit; i++ {
		if response := failLogin(t, server, "target"); response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d body=%s", i+1, response.Code, response.Body.String())
		}
	}
	if response := failLogin(t, server, "target"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("target was not locked: status=%d body=%s", response.Code, response.Body.String())
	}

	// httptest requests all carry the same RemoteAddr, so every flood entry
	// shares the target's IP and differs only in username.
	for i := 0; i < 2_000; i++ {
		if response := failLogin(t, server, fmt.Sprintf("flood-%d", i)); response.Code != http.StatusUnauthorized {
			t.Fatalf("flood %d status=%d body=%s", i, response.Code, response.Body.String())
		}
	}

	response := failLogin(t, server, "target")
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("flood evicted the lockout: status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Retry-After") == "" {
		t.Fatal("locked response lacks Retry-After")
	}
	if got := len(server.loginLimiter.failures); got > server.loginLimiter.capacity {
		t.Fatalf("failure table grew past capacity: %d > %d", got, server.loginLimiter.capacity)
	}
}
