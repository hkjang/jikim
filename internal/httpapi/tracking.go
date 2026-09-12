package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/tracking"
)

// cspReportPath is where browsers post the requests the content security
// policy refused. It is unauthenticated because the browser sends the report
// without credentials, and it keeps nothing but a bounded list of origins in
// memory.
const cspReportPath = "/api/v1/tracking/csp-report"

// maxCSPReportBytes keeps an unauthenticated endpoint from being used to push
// large bodies at the server.
const maxCSPReportBytes = 8 * 1024

// maxMomentoProxyBytes bounds one forwarded event batch. A page view is a few
// hundred bytes; the limit exists so the proxy cannot be used as a relay.
const maxMomentoProxyBytes = 64 * 1024

// basePagePolicy is the policy every screen has always shipped with. Tracking
// adds to it per request; it never replaces or loosens it.
const (
	basePagePolicy = "default-src 'self'; script-src 'self'%s; style-src 'self' 'unsafe-inline'; img-src 'self' data:%s; connect-src 'self'%s; font-src 'self' data:; frame-ancestors 'none'"
	// apiPolicy is for responses no browser renders: JSON, SSE, the proxy.
	// Nothing in them may load anything, so the policy says so.
	apiPolicy = "default-src 'none'; frame-ancestors 'none'"
)

type nonceKey struct{}

// requestNonce returns the per-request script nonce the page handler minted,
// or "" for requests that never render a page.
func requestNonce(r *http.Request) string {
	value, _ := r.Context().Value(nonceKey{}).(string)
	return value
}

func newNonce() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// pagePath reports whether a request path is one a browser renders as a
// screen. Everything else gets the narrow policy.
func pagePath(path string) bool {
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/v1/") || path == "/mcp" ||
		path == "/healthz" || path == "/readyz" {
		return false
	}
	return path != tracking.MomentoProxyPrefix && !strings.HasPrefix(path, tracking.MomentoProxyPrefix+"/")
}

// pagePolicy keeps the strict page policy and adds only what the configured
// snippet needs: the nonce for its inline code and the origins it talks to.
func pagePolicy(config tracking.Config, path, nonce string) string {
	if !config.Active(path) || nonce == "" {
		return defaultPagePolicy()
	}
	scripts, connects, images := config.PolicySources()
	scripts = append([]string{"'nonce-" + nonce + "'"}, scripts...)
	policy := fmt.Sprintf(basePagePolicy, join(scripts), join(images), join(connects))
	// While tracking is on, ask the browser to say what it refused. That
	// report is what turns a console error into a one-click fix.
	return policy + "; report-uri " + cspReportPath
}

// defaultPagePolicy is the policy with nothing added: the one every screen
// gets while tracking is off, byte for byte what shipped before tracking.
func defaultPagePolicy() string {
	return fmt.Sprintf(basePagePolicy, "", "", "")
}

func join(sources []string) string {
	if len(sources) == 0 {
		return ""
	}
	return " " + strings.Join(sources, " ")
}

// trackingConfig reads the tracking setting. A storage failure is treated as
// "tracking off" so an outage never keeps the login screen from loading.
func (s *Server) trackingConfig(ctx context.Context) tracking.Config {
	if s.trackingLoader == nil {
		return tracking.ReadConfig(nil)
	}
	config, err := s.trackingLoader(ctx)
	if err != nil {
		s.logger.Warn("방문 추적 설정을 읽지 못해 이번 응답은 추적 없이 보냅니다", "error", err)
		return tracking.ReadConfig(nil)
	}
	return config
}

// decorateIndex is applied to the SPA shell only. It mints the nonce, sets the
// page policy that names it, and injects the snippet carrying it, so the
// header and the markup always agree.
func (s *Server) decorateIndex(w http.ResponseWriter, r *http.Request, page []byte) []byte {
	config := s.trackingConfig(r.Context())
	if !config.Active(r.URL.Path) {
		return page
	}
	nonce := newNonce()
	if nonce == "" {
		return page
	}
	*r = *r.WithContext(context.WithValue(r.Context(), nonceKey{}, nonce))
	w.Header().Set("Content-Security-Policy", pagePolicy(config, r.URL.Path, nonce))
	return tracking.Inject(page, config.Snippet(nonce), config.Placement)
}

type cspReport struct {
	Report struct {
		BlockedURI         string `json:"blocked-uri"`
		ViolatedDirective  string `json:"violated-directive"`
		EffectiveDirective string `json:"effective-directive"`
		DocumentURI        string `json:"document-uri"`
	} `json:"csp-report"`
}

// receiveCSPReport records what a browser refused to load. Reports are always
// answered with 204 so a misbehaving page never sees an error from us.
func (s *Server) receiveCSPReport(w http.ResponseWriter, r *http.Request) {
	defer w.WriteHeader(http.StatusNoContent)
	if s.violations == nil {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCSPReportBytes))
	if err != nil || len(body) == 0 {
		return
	}
	var report cspReport
	if json.Unmarshal(body, &report) != nil {
		return
	}
	directive := report.Report.EffectiveDirective
	if directive == "" {
		directive = report.Report.ViolatedDirective
	}
	s.violations.Record(report.Report.BlockedURI, directive, report.Report.DocumentURI)
}

// listTrackingViolations shows the administrator which addresses the policy
// is blocking, so a snippet can be fixed without reading the browser console.
func (s *Server) listTrackingViolations(w http.ResponseWriter, r *http.Request) {
	items := []tracking.Violation{}
	if s.violations != nil {
		items = s.violations.List(s.trackingConfig(r.Context()))
	}
	writeData(w, http.StatusOK, items)
}

// clearTrackingViolations forgets the recorded reports, which is how an
// administrator checks whether a change actually fixed the snippet.
func (s *Server) clearTrackingViolations(w http.ResponseWriter, r *http.Request) {
	if s.violations != nil {
		s.violations.Forget()
	}
	w.WriteHeader(http.StatusNoContent)
}

// allowTrackingOrigin adds one blocked origin to the allow list. It is the
// one-click fix for the reports listed above.
func (s *Server) allowTrackingOrigin(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Origin string `json:"origin"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	config, err := s.trackingLoader(r.Context())
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	hosts, ok := tracking.AddAllowedHost(config.AllowedHosts, input.Origin)
	if !ok {
		writeError(w, r, http.StatusBadRequest, "invalid_origin", "허용할 출처는 http(s) 주소여야 합니다")
		return
	}
	session, _ := sessionFrom(r)
	if err := s.store.PatchSetting(r.Context(), "tracking", map[string]any{"allowed_hosts": hosts}, session.User.ID); err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"allowed_hosts": hosts})
}

// momentoProxy forwards /momento/* to the collector so the browser only ever
// talks to this origin. It answers 404 unless Momento is on with the proxy
// chosen, so a fresh install exposes nothing.
func (s *Server) momentoProxy(w http.ResponseWriter, r *http.Request) {
	config := s.trackingConfig(r.Context())
	if !config.ProxyActive() {
		writeError(w, r, http.StatusNotFound, "not_found", "요청한 경로를 찾을 수 없습니다")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "허용되지 않은 요청 방식입니다")
		return
	}
	target, err := url.Parse(strings.TrimRight(strings.TrimSpace(config.MomentoURL), "/"))
	if err != nil || target.Host == "" {
		writeError(w, r, http.StatusBadGateway, "bad_gateway", "Momento 수집기 주소가 올바르지 않습니다")
		return
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.SetXForwarded()
			request.Out.URL.Path = target.Path + strings.TrimPrefix(r.URL.Path, tracking.MomentoProxyPrefix)
			request.Out.URL.RawPath = ""
			request.Out.Host = target.Host
			// The collector is not this application: it must not see the
			// session cookie or a bearer token the browser attached.
			request.Out.Header.Del("Cookie")
			request.Out.Header.Del("Authorization")
			request.Out.Header.Del("X-Vault-Token")
		},
		Transport:     s.httpClient.Transport,
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.logger.Warn("Momento 수집기에 전달하지 못했습니다", "error", err, "request_id", requestIDFrom(r))
			writeError(w, r, http.StatusBadGateway, "bad_gateway", "Momento 수집기에 연결할 수 없습니다")
		},
		ModifyResponse: func(response *http.Response) error {
			// Whatever the collector says, the browser must not get cookies
			// or a policy from another origin under our name.
			response.Header.Del("Set-Cookie")
			response.Header.Del("Content-Security-Policy")
			return nil
		},
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMomentoProxyBytes)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	proxy.ServeHTTP(w, r.WithContext(ctx))
}
