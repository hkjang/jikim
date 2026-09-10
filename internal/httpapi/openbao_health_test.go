package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func healthRequest(target string) *http.Request {
	return httptest.NewRequest(http.MethodGet, target, nil)
}

// A jikim that cannot reach PostgreSQL can serve no secret and no transit key.
// Reporting "unsealed and active" keeps a load balancer routing traffic to it
// and hides the outage from the standard OpenBao probe.
func TestOpenBaoHealthReportsStorageOutageAsSealed(t *testing.T) {
	server := &Server{storagePinger: func(context.Context) error {
		return errors.New(`failed to connect to "host=postgres.internal user=jikim_app": SQLSTATE 08006`)
	}}
	response := httptest.NewRecorder()
	server.baoHealth(response, healthRequest("/v1/sys/health"))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"sealed":true`) {
		t.Fatalf("outage was not reported as sealed: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "postgres.internal") || strings.Contains(response.Body.String(), "SQLSTATE") {
		t.Fatalf("driver detail leaked to the client: %s", response.Body.String())
	}
}

func TestOpenBaoHealthStaysActiveWhenStorageAnswers(t *testing.T) {
	server := &Server{storagePinger: func(context.Context) error { return nil }}
	response := httptest.NewRecorder()
	server.baoHealth(response, healthRequest("/v1/sys/health"))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{`"sealed":false`, `"initialized":true`, `"standby":false`, `"cluster_name":"jikim"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("health response lost %s: %s", want, body)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("health response became cacheable: %q", response.Header().Get("Cache-Control"))
	}
}

// Operators encode the expected probe status in the request, so a jikim behind
// a load balancer configured the OpenBao way keeps answering with the codes
// that configuration asked for.
func TestOpenBaoHealthHonoursStatusCodeOverrides(t *testing.T) {
	for _, test := range []struct {
		name       string
		target     string
		pingErr    error
		wantStatus int
	}{
		{"sealedcode override", "/v1/sys/health?sealedcode=200", driverFailure, http.StatusOK},
		{"activecode override", "/v1/sys/health?activecode=204", nil, http.StatusNoContent},
		{"empty override keeps the default", "/v1/sys/health?sealedcode=", driverFailure, http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := &Server{storagePinger: func(context.Context) error { return test.pingErr }}
			response := httptest.NewRecorder()
			server.baoHealth(response, healthRequest(test.target))
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

// A probe configured with a value jikim cannot use is a request error, not a
// silently ignored parameter that leaves the operator believing it took effect.
func TestOpenBaoHealthRejectsUnusableStatusCode(t *testing.T) {
	for _, target := range []string{
		"/v1/sys/health?sealedcode=abc",
		"/v1/sys/health?sealedcode=99",
		"/v1/sys/health?activecode=600",
	} {
		server := &Server{storagePinger: func(context.Context) error { return nil }}
		response := httptest.NewRecorder()
		server.baoHealth(response, healthRequest(target))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
}

// The OpenBao-shaped liveness probe is the twin of /healthz and /readyz, which
// are already unaudited. Auditing it writes one row per poll, including through
// the outage the probe exists to report.
func TestAuditablePathSkipsProbesAndKeepsAPICalls(t *testing.T) {
	for path, want := range map[string]bool{
		"/v1/sys/health":                false,
		"/api/v1/audit":                 false,
		"/healthz":                      false,
		"/readyz":                       false,
		"/":                             false,
		"/v1/secret/data/api":           true,
		"/v1/auth/userpass/login/hyeon": true,
		"/api/v1/secrets":               true,
		"/mcp":                          true,
	} {
		if got := auditablePath(path); got != want {
			t.Fatalf("auditablePath(%q)=%t want %t", path, got, want)
		}
	}
}
