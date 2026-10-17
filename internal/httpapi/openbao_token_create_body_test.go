package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hkjang/jikim/internal/model"
)

// baoTokenCreateHarness drives the real route and withBaoAuth with no store:
// an admin session is injected through sessionResolver and session creation
// through sessionCreator, so a rejected request can be told apart from an
// accepted one by whether the creator ran at all.
type baoTokenCreateHarness struct {
	mux   *http.ServeMux
	calls int
	name  string
	ttl   time.Duration
}

func newBaoTokenCreateHarness() *baoTokenCreateHarness {
	harness := &baoTokenCreateHarness{mux: http.NewServeMux()}
	server := quietServer()
	server.sessionResolver = func(context.Context, string) (model.Session, error) {
		return model.Session{ID: "session-1", User: model.User{ID: "user-1", Username: "admin", Role: "admin"}}, nil
	}
	server.sessionCreator = func(_ context.Context, _, _, name, _ string, ttl time.Duration) (string, model.Session, error) {
		harness.calls++
		harness.name = name
		harness.ttl = ttl
		return "hvs.child-token", model.Session{ID: "session-2"}, nil
	}
	server.openBaoRoutes(harness.mux)
	return harness
}

// send posts body to auth/token/create. chunked models a request whose length
// the server could not determine (Transfer-Encoding: chunked), which Go reports
// as ContentLength == -1; httptest.NewRequest fills the length in from the
// reader, so it has to be cleared explicitly.
func (h *baoTokenCreateHarness) send(body string, chunked bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest("POST", "/v1/auth/token/create", strings.NewReader(body))
	request.Header.Set("X-Vault-Token", "hvs.session-token")
	if chunked {
		request.ContentLength = -1
	}
	response := httptest.NewRecorder()
	h.mux.ServeHTTP(response, request)
	return response
}

// A body the server cannot measure is still a body. Dropping it unread let a
// caller ask for a one-hour token and receive the twelve-hour default, and let
// a ttl the server would otherwise refuse succeed as that same default.
func TestBaoTokenCreateReadsBodyWithoutContentLength(t *testing.T) {
	rejections := []struct {
		name string
		body string
		want string
	}{
		{"invalid_ttl", `{"ttl":"99999h"}`, `{"errors":["invalid ttl"]}`},
		{"zero_ttl", `{"ttl":"0s"}`, `{"errors":["invalid ttl"]}`},
		{"unparsable_ttl", `{"ttl":"soon"}`, `{"errors":["invalid ttl"]}`},
		{"broken_json", `{"ttl":`, `{"errors":["invalid request body"]}`},
		{"two_json_values", `{} {}`, `{"errors":["request body must contain a single JSON value"]}`},
	}
	for _, rejection := range rejections {
		for _, chunked := range []bool{false, true} {
			label := rejection.name + "/content_length"
			if chunked {
				label = rejection.name + "/chunked"
			}
			t.Run(label, func(t *testing.T) {
				harness := newBaoTokenCreateHarness()
				response := harness.send(rejection.body, chunked)
				if response.Code != http.StatusBadRequest {
					t.Fatalf("status=%d body=%s", response.Code, strings.TrimSpace(response.Body.String()))
				}
				if got := strings.TrimSpace(response.Body.String()); got != rejection.want {
					t.Fatalf("body=%s want %s", got, rejection.want)
				}
				if harness.calls != 0 {
					t.Fatalf("session creator ran %d times for a rejected request", harness.calls)
				}
			})
		}
	}
}

// The accepted body has to reach session creation, not just avoid rejection.
func TestBaoTokenCreateAppliesBodyWithoutContentLength(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		label := "content_length"
		if chunked {
			label = "chunked"
		}
		t.Run(label, func(t *testing.T) {
			harness := newBaoTokenCreateHarness()
			response := harness.send(`{"ttl":"1h","display_name":"ci"}`, chunked)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, strings.TrimSpace(response.Body.String()))
			}
			if lease := decodeBaoAuth(t, response.Body.Bytes())["lease_duration"]; lease != float64(3600) {
				t.Fatalf("lease_duration=%v want 3600", lease)
			}
			if harness.calls != 1 {
				t.Fatalf("session creator ran %d times, want 1", harness.calls)
			}
			if harness.name != "ci" || harness.ttl != time.Hour {
				t.Fatalf("session created with name=%q ttl=%s, want \"ci\" and 1h", harness.name, harness.ttl)
			}
		})
	}
}

// OpenBao accepts auth/token/create with no body at all, and so must this
// server — including when the absent body arrives with no measurable length.
func TestBaoTokenCreateWithoutBodyKeepsDefaultTTL(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		label := "content_length_zero"
		if chunked {
			label = "chunked_empty"
		}
		t.Run(label, func(t *testing.T) {
			harness := newBaoTokenCreateHarness()
			response := harness.send("", chunked)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, strings.TrimSpace(response.Body.String()))
			}
			if harness.calls != 1 {
				t.Fatalf("session creator ran %d times, want 1", harness.calls)
			}
			if harness.name != "" || harness.ttl != 12*time.Hour {
				t.Fatalf("session created with name=%q ttl=%s, want \"\" and 12h", harness.name, harness.ttl)
			}
		})
	}
}

// Role rejection stays ahead of the body: a reader without permission must not
// have its body decoded or get a body-shaped error instead of 403.
func TestBaoTokenCreateRejectsReaderBeforeReadingBody(t *testing.T) {
	mux := http.NewServeMux()
	server := quietServer()
	server.sessionResolver = func(context.Context, string) (model.Session, error) {
		return model.Session{ID: "session-1", User: model.User{ID: "user-1", Role: "reader"}}, nil
	}
	server.sessionCreator = func(context.Context, string, string, string, string, time.Duration) (string, model.Session, error) {
		t.Fatal("session created for a reader")
		return "", model.Session{}, nil
	}
	server.openBaoRoutes(mux)
	request := httptest.NewRequest("POST", "/v1/auth/token/create", strings.NewReader(`{"ttl":`))
	request.Header.Set("X-Vault-Token", "hvs.session-token")
	request.ContentLength = -1
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, strings.TrimSpace(response.Body.String()))
	}
	if got := strings.TrimSpace(response.Body.String()); got != `{"errors":["permission denied"]}` {
		t.Fatalf("body=%s want permission denied", got)
	}
}

func decodeBaoAuth(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var envelope struct {
		Auth map[string]any `json:"auth"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode response: %v body=%s", err, body)
	}
	if envelope.Auth == nil {
		t.Fatalf("no auth block in %s", body)
	}
	return envelope.Auth
}
