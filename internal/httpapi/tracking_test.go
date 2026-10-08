package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hkjang/jikim/internal/model"
	"github.com/hkjang/jikim/internal/tracking"
)

// shippedPolicy is the header every screen carried before tracking existed.
// A fresh install, and an install that switched tracking off again, must send
// exactly this.
const shippedPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self' data:; frame-ancestors 'none'"

const shellPage = "<!doctype html>\n<html lang=\"ko\">\n<head>\n<meta charset=\"UTF-8\" />\n<title>jikim</title>\n</head>\n<body>\n<div id=\"root\"></div>\n<script type=\"module\" src=\"/assets/index.js\"></script>\n</body>\n</html>\n"

var noncePattern = regexp.MustCompile(`'nonce-([A-Za-z0-9_-]+)'`)

// trackingServer builds a Server that serves a throwaway SPA shell and reads
// its tracking configuration from the given loader instead of PostgreSQL.
func trackingServer(t *testing.T, config tracking.Config, loadErr error) (*Server, http.Handler) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(shellPage), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "asset.js"), []byte("console.log(1)"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := quietServer()
	server.violations = tracking.NewRecorder()
	server.storagePinger = func(context.Context) error { return nil }
	server.trackingLoader = func(context.Context) (tracking.Config, error) { return config, loadErr }
	server.static = &spaHandler{root: root, fileServer: http.FileServer(http.Dir(root)), decorate: server.decorateIndex}
	mux := http.NewServeMux()
	server.routes(mux)
	return server, server.securityHeaders(mux)
}

func momentoConfig() tracking.Config {
	return tracking.ReadConfig(map[string]any{
		"enabled": true, "provider": "momento",
		"momento_url": "https://momento.corp.example", "momento_site_id": "jikim-prod",
	})
}

func getPage(handler http.Handler, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func TestFreshInstallServesTheShippedPolicyAndNoSnippet(t *testing.T) {
	_, handler := trackingServer(t, tracking.ReadConfig(nil), nil)
	for _, path := range []string{"/", "/dashboard", "/login", "/admin/settings"} {
		response := getPage(handler, path)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, response.Code)
		}
		if got := response.Header().Get("Content-Security-Policy"); got != shippedPolicy {
			t.Fatalf("%s policy changed on a fresh install:\n%s", path, got)
		}
		if strings.Contains(response.Body.String(), "nonce=") || strings.Contains(response.Body.String(), "tracker.js") {
			t.Fatalf("%s carried a snippet while tracking is off: %s", path, response.Body.String())
		}
	}
	// The proxy path is closed too: nothing is exposed until Momento is on.
	response := getPage(handler, "/momento/tracker.js")
	if response.Code != http.StatusNotFound {
		t.Fatalf("proxy answered %d while tracking is off", response.Code)
	}
}

func TestEnabledTrackingCarriesANoncedSnippetAndAMatchingPolicy(t *testing.T) {
	_, handler := trackingServer(t, momentoConfig(), nil)
	first := getPage(handler, "/dashboard")
	second := getPage(handler, "/dashboard")
	for _, response := range []*httptest.ResponseRecorder{first, second} {
		policy := response.Header().Get("Content-Security-Policy")
		match := noncePattern.FindStringSubmatch(policy)
		if match == nil {
			t.Fatalf("policy has no nonce: %s", policy)
		}
		scriptSrc := strings.SplitN(strings.SplitN(policy, "script-src ", 2)[1], ";", 2)[0]
		if strings.Contains(scriptSrc, "unsafe-inline") || strings.Contains(scriptSrc, "unsafe-eval") {
			t.Fatalf("script-src must not be loosened: %s", policy)
		}
		if !strings.HasSuffix(policy, "; report-uri "+cspReportPath) {
			t.Fatalf("policy lacks report-uri: %s", policy)
		}
		if strings.Contains(policy, "momento.corp.example") {
			t.Fatalf("proxied Momento leaked the collector into the policy: %s", policy)
		}
		body := response.Body.String()
		wantTag := `<script nonce="` + match[1] + `" async src="/momento/tracker.js" data-site-id="jikim-prod"`
		if !strings.Contains(body, wantTag) {
			t.Fatalf("snippet missing or nonce mismatched:\npolicy=%s\nbody=%s", policy, body)
		}
		if !strings.Contains(body, `data-endpoint="/momento"`) {
			t.Fatalf("proxied snippet lacks data-endpoint: %s", body)
		}
		head := strings.SplitN(body, "</head>", 2)[0]
		if !strings.Contains(head, wantTag) {
			t.Fatalf("head placement put the snippet elsewhere: %s", body)
		}
	}
	if noncePattern.FindStringSubmatch(first.Header().Get("Content-Security-Policy"))[1] == noncePattern.FindStringSubmatch(second.Header().Get("Content-Security-Policy"))[1] {
		t.Fatal("nonce was reused across requests")
	}
}

func TestCustomSnippetOriginsAndBodyPlacement(t *testing.T) {
	config := tracking.ReadConfig(map[string]any{
		"enabled": true, "provider": "custom", "placement": "body",
		"custom_snippet": `<script src="https://t.corp.example/t.js"></script><script>window.__t={e:"https://c.corp.example/collect"}</script>`,
		"allowed_hosts":  "https://pixel.corp.example",
	})
	_, handler := trackingServer(t, config, nil)
	response := getPage(handler, "/secrets")
	policy := response.Header().Get("Content-Security-Policy")
	scriptSrc := strings.SplitN(strings.SplitN(policy, "script-src ", 2)[1], ";", 2)[0]
	connectSrc := strings.SplitN(strings.SplitN(policy, "connect-src ", 2)[1], ";", 2)[0]
	imgSrc := strings.SplitN(strings.SplitN(policy, "img-src ", 2)[1], ";", 2)[0]
	for _, origin := range []string{"https://t.corp.example", "https://c.corp.example", "https://pixel.corp.example"} {
		for name, directive := range map[string]string{"script-src": scriptSrc, "connect-src": connectSrc, "img-src": imgSrc} {
			if !strings.Contains(directive, origin) {
				t.Fatalf("%s lacks %s: %s", name, origin, policy)
			}
		}
	}
	body := response.Body.String()
	nonce := noncePattern.FindStringSubmatch(policy)[1]
	if strings.Count(body, `nonce="`+nonce+`"`) != 2 {
		t.Fatalf("both script tags need the nonce: %s", body)
	}
	tail := strings.SplitN(body, "</head>", 2)[1]
	if !strings.Contains(tail, `nonce="`+nonce+`" src="https://t.corp.example/t.js"`) || !strings.Contains(strings.SplitN(tail, "</body>", 2)[0], "t.corp.example") {
		t.Fatalf("body placement wrong: %s", body)
	}
}

func TestAdminScreensStayUntrackedUnlessIncluded(t *testing.T) {
	_, handler := trackingServer(t, momentoConfig(), nil)
	response := getPage(handler, "/admin/settings")
	if got := response.Header().Get("Content-Security-Policy"); got != shippedPolicy {
		t.Fatalf("admin page policy widened without include_admin: %s", got)
	}
	if strings.Contains(response.Body.String(), "tracker.js") {
		t.Fatal("admin page carried the snippet without include_admin")
	}
	config := momentoConfig()
	config.IncludeAdmin = true
	_, handler = trackingServer(t, config, nil)
	if !strings.Contains(getPage(handler, "/admin/settings").Body.String(), "tracker.js") {
		t.Fatal("include_admin did not track the admin page")
	}
}

func TestNonPageResponsesGetTheNarrowPolicy(t *testing.T) {
	_, handler := trackingServer(t, momentoConfig(), nil)
	for _, path := range []string{"/api/v1/version", "/healthz", "/readyz", "/v1/sys/health", "/api/openapi.json"} {
		response := getPage(handler, path)
		if got := response.Header().Get("Content-Security-Policy"); got != apiPolicy {
			t.Fatalf("%s policy=%q", path, got)
		}
		if strings.Contains(response.Body.String(), "tracker.js") {
			t.Fatalf("%s carried the snippet", path)
		}
	}
	// A static asset is not the shell: no snippet, no nonce, the shipped policy.
	asset := getPage(handler, "/asset.js")
	if asset.Code != http.StatusOK || asset.Header().Get("Content-Security-Policy") != shippedPolicy || strings.Contains(asset.Body.String(), "nonce") {
		t.Fatalf("asset status=%d policy=%q body=%s", asset.Code, asset.Header().Get("Content-Security-Policy"), asset.Body.String())
	}
}

// A settings outage must not take the login screen down with it: the page is
// served without tracking and with the shipped policy.
func TestSettingsOutageServesThePageWithoutTracking(t *testing.T) {
	_, handler := trackingServer(t, momentoConfig(), errors.New(`failed to connect to "host=postgres.internal": SQLSTATE 08006`))
	response := getPage(handler, "/dashboard")
	if response.Code != http.StatusOK || response.Header().Get("Content-Security-Policy") != shippedPolicy {
		t.Fatalf("status=%d policy=%s", response.Code, response.Header().Get("Content-Security-Policy"))
	}
	if strings.Contains(response.Body.String(), "tracker.js") || strings.Contains(response.Body.String(), "postgres.internal") {
		t.Fatalf("outage response wrong: %s", response.Body.String())
	}
}

func TestPolicyReportsAreRecordedOnceAndListedForAdministrators(t *testing.T) {
	server, handler := trackingServer(t, momentoConfig(), nil)
	report := `{"csp-report":{"blocked-uri":"https://pixel.corp.example/p.gif","effective-directive":"img-src","document-uri":"https://jikim.example/dashboard"}}`
	for index := 0; index < 3; index++ {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, cspReportPath, strings.NewReader(report))
		request.Header.Set("Content-Type", "application/csp-report")
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("report status=%d", response.Code)
		}
	}
	garbage := httptest.NewRecorder()
	handler.ServeHTTP(garbage, httptest.NewRequest(http.MethodPost, cspReportPath, strings.NewReader("not json")))
	if garbage.Code != http.StatusNoContent {
		t.Fatalf("garbage report status=%d", garbage.Code)
	}
	if auditablePath(cspReportPath) {
		t.Fatal("browser policy reports must not fill the audit log")
	}

	// Listing is an administrator's job.
	anonymous := getPage(handler, "/api/v1/tracking/violations")
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list status=%d", anonymous.Code)
	}
	server.sessionResolver = func(context.Context, string) (model.Session, error) {
		return model.Session{User: model.User{ID: "u1", Username: "admin", Role: "admin"}}, nil
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, tokenRequest(http.MethodGet, "/api/v1/tracking/violations"))
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Count(body, `"origin":"https://pixel.corp.example"`) != 1 || !strings.Contains(body, `"directive":"img-src"`) || !strings.Contains(body, `"count":3`) || !strings.Contains(body, `"allowed":false`) {
		t.Fatalf("list body=%s", body)
	}

	cleared := httptest.NewRecorder()
	handler.ServeHTTP(cleared, tokenRequest(http.MethodDelete, "/api/v1/tracking/violations"))
	if cleared.Code != http.StatusNoContent {
		t.Fatalf("clear status=%d", cleared.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, tokenRequest(http.MethodGet, "/api/v1/tracking/violations"))
	if !strings.Contains(response.Body.String(), `"data":[]`) {
		t.Fatalf("list after clear=%s", response.Body.String())
	}
}

// The report endpoint is unauthenticated because a browser posts without
// credentials, but while tracking is off no browser has been given the
// report-uri at all — so anything arriving there is injected from outside and
// must not reach the administrator's blocked-origins list.
func TestPolicyReportsAreOnlyRecordedWhileTrackingIsOn(t *testing.T) {
	const report = `{"csp-report":{"blocked-uri":"https://pixel.corp.example/p.gif","effective-directive":"img-src","document-uri":"https://jikim.example/dashboard"}}`
	postReports := func(t *testing.T, handler http.Handler, times int) {
		t.Helper()
		for index := 0; index < times; index++ {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, cspReportPath, strings.NewReader(report))
			request.Header.Set("Content-Type", "application/csp-report")
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("report %d status=%d", index, response.Code)
			}
			if response.Body.Len() != 0 {
				t.Fatalf("report %d answered with a body: %s", index, response.Body.String())
			}
		}
	}
	listViolations := func(t *testing.T, server *Server, handler http.Handler) string {
		t.Helper()
		server.sessionResolver = func(context.Context, string) (model.Session, error) {
			return model.Session{User: model.User{ID: "u1", Username: "admin", Role: "admin"}}, nil
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, tokenRequest(http.MethodGet, "/api/v1/tracking/violations"))
		if response.Code != http.StatusOK {
			t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
		}
		return response.Body.String()
	}

	t.Run("tracking off records nothing", func(t *testing.T) {
		server, handler := trackingServer(t, tracking.ReadConfig(nil), nil)
		postReports(t, handler, 5)
		if body := listViolations(t, server, handler); !strings.Contains(body, `"data":[]`) {
			t.Fatalf("a report reached the list while tracking is off: %s", body)
		}
	})

	t.Run("tracking on records once per origin", func(t *testing.T) {
		server, handler := trackingServer(t, momentoConfig(), nil)
		postReports(t, handler, 3)
		body := listViolations(t, server, handler)
		if strings.Count(body, `"origin":"https://pixel.corp.example"`) != 1 || !strings.Contains(body, `"count":3`) {
			t.Fatalf("list body=%s", body)
		}
	})

	t.Run("settings outage records nothing", func(t *testing.T) {
		server, handler := trackingServer(t, momentoConfig(), errors.New(`failed to connect to "host=postgres.internal": SQLSTATE 08006`))
		postReports(t, handler, 2)
		if body := listViolations(t, server, handler); !strings.Contains(body, `"data":[]`) {
			t.Fatalf("a report was recorded while the settings read failed: %s", body)
		}
	})
}

func TestCSPReportBodySizeBoundary(t *testing.T) {
	const report = `{"csp-report":{"blocked-uri":"https://pixel.corp.example/p.gif","effective-directive":"img-src","document-uri":"https://jikim.example/dashboard"}}`
	boundary := report + strings.Repeat(" ", maxCSPReportBytes-len(report))
	for _, tc := range []struct {
		name      string
		body      string
		wantItems int
	}{
		{"normal report", report, 1},
		{"exactly 8192 bytes", boundary, 1},
		{"8193 bytes of valid JSON", boundary + " ", 0},
		{"larger valid JSON", boundary + strings.Repeat(" ", 1024), 0},
		{"invalid suffix after 8192 bytes", boundary + "x", 0},
	} {
		for _, unknownLength := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/unknown_length=%t", tc.name, unknownLength), func(t *testing.T) {
				server, handler := trackingServer(t, momentoConfig(), nil)
				request := httptest.NewRequest(http.MethodPost, cspReportPath, strings.NewReader(tc.body))
				request.Header.Set("Content-Type", "application/csp-report")
				if unknownLength {
					request.ContentLength = -1
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
					t.Fatalf("report status=%d body=%s", response.Code, response.Body.String())
				}

				server.sessionResolver = func(context.Context, string) (model.Session, error) {
					return model.Session{User: model.User{ID: "u1", Username: "admin", Role: "admin"}}, nil
				}
				listed := httptest.NewRecorder()
				handler.ServeHTTP(listed, tokenRequest(http.MethodGet, "/api/v1/tracking/violations"))
				if listed.Code != http.StatusOK {
					t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
				}
				var result struct {
					Data []tracking.Violation `json:"data"`
				}
				if err := json.Unmarshal(listed.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Data) != tc.wantItems {
					t.Fatalf("recorded reports=%d, want %d (body bytes=%d, content length=%d)", len(result.Data), tc.wantItems, len(tc.body), request.ContentLength)
				}
				if tc.wantItems == 1 {
					item := result.Data[0]
					if item.Count != 1 || item.Origin != "https://pixel.corp.example" || item.Directive != "img-src" {
						t.Fatalf("unexpected recorded report: %+v", item)
					}
				}
			})
		}
	}
}

func TestAllowingAnOriginRefusesWhatCannotBeAllowed(t *testing.T) {
	server, handler := trackingServer(t, momentoConfig(), nil)
	server.sessionResolver = func(context.Context, string) (model.Session, error) {
		return model.Session{User: model.User{ID: "u1", Username: "admin", Role: "admin"}}, nil
	}
	for _, origin := range []string{"", "chrome-extension://abc", "data:", "javascript:alert(1)"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/tracking/violations/allow", strings.NewReader(`{"origin":"`+origin+`"}`))
		request.Header.Set("X-Vault-Token", "hvs.session-token")
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("origin %q status=%d body=%s", origin, response.Code, response.Body.String())
		}
	}
}

// The proxy is what keeps a Momento install off the policy entirely: the
// browser talks to /momento/* here and the server forwards it, minus the
// session cookie, and hands back the collector's answer minus its cookies.
func TestMomentoProxyForwardsWithoutCredentials(t *testing.T) {
	var seenPath, seenCookie, seenMethod, seenBody string
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath, seenCookie, seenMethod = r.URL.Path, r.Header.Get("Cookie"), r.Method
		raw := make([]byte, 64)
		n, _ := r.Body.Read(raw)
		seenBody = string(raw[:n])
		w.Header().Set("Set-Cookie", "collector=1")
		w.Header().Set("Content-Type", "application/javascript")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("ok"))
	}))
	defer collector.Close()
	config := tracking.ReadConfig(map[string]any{
		"enabled": true, "provider": "momento", "allow_insecure_http": true,
		"momento_url": collector.URL + "/base/", "momento_site_id": "jikim",
	})
	server, handler := trackingServer(t, config, nil)
	server.httpClient = http.DefaultClient

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/momento/collect/v1/events", strings.NewReader(`{"e":1}`))
	request.Header.Set("Cookie", "jikim_session=secret")
	request.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || response.Body.String() != "ok" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if seenPath != "/base/collect/v1/events" || seenMethod != http.MethodPost || seenBody != `{"e":1}` {
		t.Fatalf("collector saw path=%s method=%s body=%s", seenPath, seenMethod, seenBody)
	}
	if seenCookie != "" {
		t.Fatalf("session cookie forwarded to the collector: %s", seenCookie)
	}
	if response.Header().Get("Set-Cookie") != "" {
		t.Fatal("collector cookie handed to the browser under our origin")
	}
	if response.Header().Get("Content-Security-Policy") != apiPolicy {
		t.Fatalf("proxy response policy=%q", response.Header().Get("Content-Security-Policy"))
	}

	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, httptest.NewRequest(http.MethodDelete, "/momento/collect", nil))
	if denied.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE through the proxy status=%d", denied.Code)
	}

	config.MomentoProxy = false
	_, handler = trackingServer(t, config, nil)
	if got := getPage(handler, "/momento/tracker.js").Code; got != http.StatusNotFound {
		t.Fatalf("proxy stayed open with momento_proxy off: %d", got)
	}
}

func TestTrackingViolationAllowanceMatchesDirectivePolicy(t *testing.T) {
	for _, tc := range []struct {
		name, origin, directive, policyDirective, source string
		allowed                                          bool
	}{
		{"connect-only origin cannot load scripts", "https://analytics.google.com", "script-src", "script-src", "https://analytics.google.com", false},
		{"script element uses script policy", "https://analytics.google.com", "script-src-elem", "script-src", "https://analytics.google.com", false},
		{"script origin allows elements", "https://www.googletagmanager.com", "script-src-elem", "script-src", "https://www.googletagmanager.com", true},
		{"connection remains allowed", "https://analytics.google.com", "connect-src", "connect-src", "https://analytics.google.com", true},
		{"image remains allowed", "https://www.googletagmanager.com", "img-src", "img-src", "https://www.googletagmanager.com", true},
		{"wildcard only allows connections", "https://region1.google-analytics.com", "img-src", "img-src", "https://*.google-analytics.com", false},
		{"wildcard connection remains allowed", "https://region1.google-analytics.com", "connect-src", "connect-src", "https://*.google-analytics.com", true},
		{"unconfigured directive remains blocked", "https://www.googletagmanager.com", "frame-src", "default-src", "https://www.googletagmanager.com", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := tracking.ReadConfig(map[string]any{"enabled": true, "provider": "ga4", "measurement_id": "G-1"})
			server, handler := trackingServer(t, config, nil)
			page := getPage(handler, "/dashboard")
			if page.Code != http.StatusOK {
				t.Fatalf("page status=%d", page.Code)
			}
			var sources []string
			for _, directive := range strings.Split(page.Header().Get("Content-Security-Policy"), ";") {
				fields := strings.Fields(directive)
				if len(fields) > 0 && fields[0] == tc.policyDirective {
					sources = fields[1:]
				}
			}
			found := false
			for _, source := range sources {
				if source == tc.source {
					found = true
				}
			}
			if found != tc.allowed {
				t.Fatalf("unexpected actual policy: %v", sources)
			}
			report := fmt.Sprintf(`{"csp-report":{"blocked-uri":%q,"effective-directive":%q,"document-uri":"https://jikim.example/dashboard"}}`, tc.origin+"/resource", tc.directive)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, cspReportPath, strings.NewReader(report)))
			if response.Code != http.StatusNoContent {
				t.Fatalf("report status=%d", response.Code)
			}
			server.sessionResolver = func(context.Context, string) (model.Session, error) {
				return model.Session{User: model.User{ID: "u1", Role: "admin"}}, nil
			}
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, tokenRequest(http.MethodGet, "/api/v1/tracking/violations"))
			var result struct {
				Data []tracking.Violation `json:"data"`
			}
			if response.Code != http.StatusOK {
				t.Fatalf("list status=%d", response.Code)
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Data) != 1 {
				t.Fatalf("violations=%+v", result.Data)
			}
			if result.Data[0].Allowed != tc.allowed {
				t.Fatalf("directive=%s origin=%s allowed=%v, want %v", tc.directive, tc.origin, result.Data[0].Allowed, tc.allowed)
			}
		})
	}
}
