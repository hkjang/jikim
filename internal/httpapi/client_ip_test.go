package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hkjang/jikim/internal/store"
)

func newTrustedServer(t *testing.T, raw string) *Server {
	t.Helper()
	prefixes, err := store.ParseTrustedProxies(raw)
	if err != nil {
		t.Fatalf("ParseTrustedProxies(%q) error = %v", raw, err)
	}
	server := &Server{}
	server.trustedProxies.value.Store(&prefixes)
	server.trustedProxies.expiresAt.Store(time.Now().Add(time.Hour).UnixNano())
	return server
}

func TestClientIPIgnoresForwardedHeaderWithoutTrustedProxies(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest("GET", "/api/v1/me", nil)
	request.RemoteAddr = "203.0.113.9:41000"
	request.Header.Set("X-Forwarded-For", "198.51.100.7")
	if got := server.clientIP(request); got != "203.0.113.9" {
		t.Fatalf("clientIP() = %q, want the peer address", got)
	}
}

func TestClientIPUsesForwardedHeaderFromTrustedProxy(t *testing.T) {
	server := newTrustedServer(t, "10.10.0.0/16, 192.0.2.10")
	cases := []struct {
		name      string
		remote    string
		forwarded []string
		want      string
	}{
		{"single hop", "10.10.0.5:5000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"single address entry", "192.0.2.10:5000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"chained proxies", "10.10.0.5:5000", []string{"198.51.100.7, 10.10.0.9", "192.0.2.10"}, "198.51.100.7"},
		{"client sent forgery", "10.10.0.5:5000", []string{"198.51.100.7, 203.0.113.9"}, "203.0.113.9"},
		{"ipv6 with port", "10.10.0.5:5000", []string{"[2001:db8::1]:9000"}, "2001:db8::1"},
		{"ipv4 mapped", "10.10.0.5:5000", []string{"::ffff:198.51.100.7"}, "198.51.100.7"},
		{"untrusted peer", "203.0.113.9:41000", []string{"198.51.100.7"}, "203.0.113.9"},
		{"only trusted hops", "10.10.0.5:5000", []string{"10.10.0.9"}, "10.10.0.5"},
		{"malformed hop", "10.10.0.5:5000", []string{"unknown"}, "10.10.0.5"},
		{"missing header", "10.10.0.5:5000", nil, "10.10.0.5"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/api/v1/me", nil)
			request.RemoteAddr = testCase.remote
			for _, value := range testCase.forwarded {
				request.Header.Add("X-Forwarded-For", value)
			}
			if got := server.clientIP(request); got != testCase.want {
				t.Fatalf("clientIP() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestLoginRateKeySeparatesForwardedClients(t *testing.T) {
	server := newTrustedServer(t, "10.10.0.0/16")
	first := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	first.RemoteAddr = "10.10.0.5:5000"
	first.Header.Set("X-Forwarded-For", "198.51.100.7")
	second := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	second.RemoteAddr = "10.10.0.5:5000"
	second.Header.Set("X-Forwarded-For", "198.51.100.8")
	if server.loginRateKey(first, "admin") == server.loginRateKey(second, "admin") {
		t.Fatal("forwarded clients behind one proxy share a login rate limit key")
	}
}
