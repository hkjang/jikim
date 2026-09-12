package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hkjang/jikim/internal/store"
	"golang.org/x/oauth2"
)

func TestSilentOIDCRequestRequiresAdministratorOptIn(t *testing.T) {
	silent := httptest.NewRequest(http.MethodGet, "https://jikim.example/api/v1/oidc/login?prompt=none", nil)
	plain := httptest.NewRequest(http.MethodGet, "https://jikim.example/api/v1/oidc/login", nil)
	if silentOIDCRequest(store.OIDCConfig{Enabled: true}, silent) {
		t.Fatal("prompt=none was honoured while auto_login is off")
	}
	if !silentOIDCRequest(store.OIDCConfig{Enabled: true, AutoLogin: true}, silent) {
		t.Fatal("prompt=none was ignored although auto_login is on")
	}
	if silentOIDCRequest(store.OIDCConfig{Enabled: true, AutoLogin: true}, plain) {
		t.Fatal("an ordinary login became silent without prompt=none")
	}
}

func TestOIDCAuthCodeOptionsAddPromptNoneOnlyForSilentAttempts(t *testing.T) {
	config := oauth2.Config{ClientID: "jikim", Endpoint: oauth2.Endpoint{AuthURL: "https://id.example/auth"}, RedirectURL: "https://jikim.example/api/v1/oidc/callback"}
	base := oidcState{State: "state", Nonce: "nonce", Verifier: "verifier"}
	for _, test := range []struct {
		name   string
		silent bool
	}{{"silent", true}, {"interactive", false}} {
		state := base
		state.Silent = test.silent
		target, err := url.Parse(config.AuthCodeURL(state.State, oidcAuthCodeOptions(state)...))
		if err != nil {
			t.Fatal(err)
		}
		query := target.Query()
		if got := query.Get("prompt") == "none"; got != test.silent {
			t.Fatalf("%s: prompt=none present=%v", test.name, got)
		}
		if query.Get("nonce") != "nonce" || query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
			t.Fatalf("%s: PKCE or nonce parameters missing: %s", test.name, target.RawQuery)
		}
	}
}

func TestSafeOIDCReturnToAcceptsOnlySameOriginPaths(t *testing.T) {
	for _, value := range []string{"/dashboard", "/secrets/abc?tab=versions", "/audit#top"} {
		if got := safeOIDCReturnTo(value); got != value {
			t.Fatalf("safeOIDCReturnTo(%q)=%q", value, got)
		}
	}
	for _, value := range []string{
		"", "dashboard", "//evil.example/x", "/\\evil.example", "https://evil.example/", "javascript:alert(1)",
		"/dashboard\r\nSet-Cookie: x=y", "/login", "/login?sso=none", "/oidc/callback?code=x",
	} {
		if got := safeOIDCReturnTo(value); got != "" {
			t.Fatalf("safeOIDCReturnTo(%q)=%q, want empty", value, got)
		}
	}
}

func sealedStateOpener(t *testing.T, sealed string, state oidcState) func(string, any) error {
	t.Helper()
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return func(value string, dst any) error {
		if value != sealed {
			return store.ErrInvalid
		}
		return json.Unmarshal(encoded, dst)
	}
}

func TestOIDCCallbackSendsSilentRefusalToLoginWithMarker(t *testing.T) {
	for _, test := range []struct {
		name     string
		returnTo string
		want     string
	}{
		{"root", "", "/login?sso=none"},
		{"deep link", "/secrets/abc?tab=versions", "/login?return_to=%2Fsecrets%2Fabc%3Ftab%3Dversions&sso=none"},
		{"tampered return", "//evil.example/", "/login?sso=none"},
	} {
		state := oidcState{State: "abc", Silent: true, IssuedAt: time.Now().UTC(), ReturnTo: test.returnTo}
		server := &Server{oidcStateOpener: sealedStateOpener(t, "sealed", state)}
		request := httptest.NewRequest(http.MethodGet, "https://jikim.example/api/v1/oidc/callback?error=login_required&state=abc", nil)
		request.AddCookie(&http.Cookie{Name: "jikim_oidc_state", Value: "sealed"})
		response := httptest.NewRecorder()
		server.oidcCallback(response, request)

		if response.Code != http.StatusFound {
			t.Fatalf("%s: status=%d body=%s", test.name, response.Code, response.Body.String())
		}
		if location := response.Header().Get("Location"); location != test.want {
			t.Fatalf("%s: silent refusal location=%q want %q", test.name, location, test.want)
		}
		if cookie := response.Header().Get("Set-Cookie"); !strings.Contains(cookie, "jikim_oidc_state=") || !strings.Contains(cookie, "Max-Age=0") {
			t.Fatalf("%s: OIDC state cookie was not cleared: %q", test.name, cookie)
		}
	}
}

func TestOIDCCallbackKeepsInteractiveProviderErrorsOnCallbackScreen(t *testing.T) {
	for _, test := range []struct {
		name   string
		state  oidcState
		cookie string
		query  string
	}{
		{"interactive attempt", oidcState{State: "abc", IssuedAt: time.Now().UTC()}, "sealed", "state=abc"},
		{"state mismatch", oidcState{State: "abc", Silent: true, IssuedAt: time.Now().UTC()}, "sealed", "state=other"},
		{"unknown cookie", oidcState{State: "abc", Silent: true, IssuedAt: time.Now().UTC()}, "forged", "state=abc"},
		{"expired state", oidcState{State: "abc", Silent: true, IssuedAt: time.Now().UTC().Add(-11 * time.Minute)}, "sealed", "state=abc"},
		{"no cookie", oidcState{State: "abc", Silent: true, IssuedAt: time.Now().UTC()}, "", "state=abc"},
	} {
		server := &Server{oidcStateOpener: sealedStateOpener(t, "sealed", test.state)}
		request := httptest.NewRequest(http.MethodGet, "https://jikim.example/api/v1/oidc/callback?error=login_required&"+test.query, nil)
		if test.cookie != "" {
			request.AddCookie(&http.Cookie{Name: "jikim_oidc_state", Value: test.cookie})
		}
		response := httptest.NewRecorder()
		server.oidcCallback(response, request)
		if response.Code != http.StatusFound {
			t.Fatalf("%s: status=%d", test.name, response.Code)
		}
		target, err := url.Parse(response.Header().Get("Location"))
		if err != nil || target.Path != "/oidc/callback" || target.Query().Get("error") != "oidc_provider_error" {
			t.Fatalf("%s: location=%q", test.name, response.Header().Get("Location"))
		}
	}
}

func TestOIDCCallbackOpenerFailureIsNotSilent(t *testing.T) {
	server := &Server{oidcStateOpener: func(string, any) error { return errors.New("boom") }}
	request := httptest.NewRequest(http.MethodGet, "https://jikim.example/api/v1/oidc/callback?error=login_required&state=abc", nil)
	request.AddCookie(&http.Cookie{Name: "jikim_oidc_state", Value: "sealed"})
	response := httptest.NewRecorder()
	server.oidcCallback(response, request)
	if location := response.Header().Get("Location"); !strings.HasPrefix(location, "/oidc/callback?") {
		t.Fatalf("location=%q", location)
	}
}
