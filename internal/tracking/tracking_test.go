package tracking

import (
	"strings"
	"testing"
	"time"
)

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// A fresh install must not change: no setting means no snippet anywhere.
func TestDefaultsAreOff(t *testing.T) {
	config := ReadConfig(nil)
	if config.Enabled || config.Active("/dashboard") || config.Snippet("n") != "" {
		t.Fatalf("empty setting produced tracking: %+v", config)
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("empty setting rejected: %v", err)
	}
	scripts, connects, images := config.PolicySources()
	if len(scripts)+len(connects)+len(images) != 0 {
		t.Fatalf("empty setting widened the policy: %v %v %v", scripts, connects, images)
	}
}

func TestMomentoProxySnippetStaysOnThisOrigin(t *testing.T) {
	config := ReadConfig(map[string]any{
		"enabled": true, "provider": "momento",
		"momento_url": "https://momento.corp.example", "momento_site_id": "jikim-prod",
	})
	if !config.MomentoProxy {
		t.Fatal("proxy must be the default for Momento")
	}
	snippet := config.Snippet("abc123")
	for _, want := range []string{`src="/momento/tracker.js"`, `data-endpoint="/momento"`, `data-site-id="jikim-prod"`, `data-environment="prd"`, `data-contract-version="1"`, `nonce="abc123"`} {
		if !strings.Contains(snippet, want) {
			t.Fatalf("snippet lost %s: %s", want, snippet)
		}
	}
	if strings.Contains(snippet, "momento.corp.example") {
		t.Fatalf("proxied snippet named the collector: %s", snippet)
	}
	scripts, connects, images := config.PolicySources()
	if len(scripts)+len(connects)+len(images) != 0 {
		t.Fatalf("proxied Momento widened the policy: %v %v %v", scripts, connects, images)
	}
	if !config.ProxyActive() {
		t.Fatal("proxy not active")
	}
}

func TestMomentoDirectSnippetAddsTheCollectorOrigin(t *testing.T) {
	config := ReadConfig(map[string]any{
		"enabled": true, "provider": "momento", "momento_proxy": false, "momento_environment": "stg",
		"momento_url": "https://momento.corp.example/", "momento_site_id": "jikim",
	})
	snippet := config.Snippet("n1")
	if !strings.Contains(snippet, `src="https://momento.corp.example/tracker.js"`) || strings.Contains(snippet, "data-endpoint") {
		t.Fatalf("direct snippet wrong: %s", snippet)
	}
	if !strings.Contains(snippet, `data-environment="stg"`) {
		t.Fatalf("environment lost: %s", snippet)
	}
	scripts, connects, images := config.PolicySources()
	for _, group := range [][]string{scripts, connects, images} {
		if !contains(group, "https://momento.corp.example") {
			t.Fatalf("collector origin missing from policy: %v", group)
		}
	}
	if config.ProxyActive() {
		t.Fatal("proxy should be off")
	}
}

func TestEveryScriptTagGetsTheNonce(t *testing.T) {
	config := ReadConfig(map[string]any{"enabled": true, "provider": "custom", "custom_snippet": `<SCRIPT src="https://t.example/a.js"></SCRIPT>
<script>window.t=1</script>
<script nonce="keep">x()</script>`})
	snippet := config.Snippet("r4nd0m")
	if strings.Count(snippet, `nonce="r4nd0m"`) != 2 {
		t.Fatalf("expected two nonces added: %s", snippet)
	}
	if !strings.Contains(snippet, `nonce="keep"`) {
		t.Fatalf("existing nonce overwritten: %s", snippet)
	}
	if strings.Contains(snippet, `nonce="keep" nonce=`) {
		t.Fatalf("nonce duplicated: %s", snippet)
	}
	if config.Snippet("") != strings.TrimSpace(config.CustomSnippet) {
		t.Fatal("empty nonce should leave the snippet alone")
	}
}

func TestSnippetOriginsReadTheAddressesATrackerWritesDown(t *testing.T) {
	snippet := `<script src="https://momento.corp.example/tracker.js"></script>
<script>window.__t={endpoint:"https://momento.corp.example/collect/v1/events",pixel:'https://pixel.corp.example/p.gif?id=1'};
var httpOnly=true; fetch("http://10.0.0.5:9000/x")</script>`
	origins := SnippetOrigins(snippet)
	want := []string{"https://momento.corp.example", "https://pixel.corp.example", "http://10.0.0.5:9000"}
	if len(origins) != len(want) {
		t.Fatalf("origins=%v", origins)
	}
	for _, origin := range want {
		if !contains(origins, origin) {
			t.Fatalf("missing %s in %v", origin, origins)
		}
	}
	config := ReadConfig(map[string]any{"enabled": true, "provider": "custom", "custom_snippet": snippet, "allowed_hosts": "https://extra.example, https://cdn.example\nhttps://extra.example"})
	scripts, _, _ := config.PolicySources()
	for _, origin := range append(want, "https://extra.example", "https://cdn.example") {
		if !contains(scripts, origin) {
			t.Fatalf("policy missing %s: %v", origin, scripts)
		}
	}
}

func TestAdminScreensAreSkippedUnlessAsked(t *testing.T) {
	config := ReadConfig(map[string]any{"enabled": true, "provider": "ga4", "measurement_id": "G-1"})
	for _, path := range []string{"/admin/settings", "/admin/users", "/infrastructure/cluster", "/access/authentication"} {
		if config.Active(path) {
			t.Fatalf("admin page %s tracked without include_admin", path)
		}
	}
	for _, path := range []string{"/dashboard", "/login", "/secrets/1", "/access/policies"} {
		if !config.Active(path) {
			t.Fatalf("visitor page %s not tracked", path)
		}
	}
	config.IncludeAdmin = true
	if !config.Active("/admin/settings") {
		t.Fatal("include_admin ignored")
	}
}

func TestValidateRefusesWhatCannotWork(t *testing.T) {
	bad := []map[string]any{
		{"provider": "piwik"},
		{"enabled": true, "provider": "none"},
		{"enabled": true, "provider": "momento", "momento_url": "https://m.example"},
		{"enabled": true, "provider": "momento", "momento_url": "http://m.example", "momento_site_id": "s"},
		{"enabled": true, "provider": "momento", "momento_url": "https://user:pw@m.example", "momento_site_id": "s"},
		{"enabled": true, "provider": "ga4"},
		{"enabled": true, "provider": "matomo", "matomo_url": "https://m.example"},
		{"enabled": true, "provider": "custom", "custom_snippet": "   "},
		{"provider": "custom", "custom_snippet": strings.Repeat("x", MaxSnippetBytes+1)},
		{"matomo_url": "not a url"},
	}
	for index, values := range bad {
		if err := ReadConfig(values).Validate(); err == nil {
			t.Fatalf("case %d accepted: %v", index, values)
		}
	}
	good := []map[string]any{
		{},
		{"enabled": false, "provider": "momento"},
		{"enabled": true, "provider": "momento", "momento_url": "https://m.example", "momento_site_id": "s"},
		{"enabled": true, "provider": "momento", "momento_url": "http://m.example", "momento_site_id": "s", "allow_insecure_http": true},
		{"enabled": true, "provider": "momento", "momento_url": "http://localhost:3000", "momento_site_id": "s"},
		{"enabled": true, "provider": "gtm", "measurement_id": "GTM-1"},
		{"enabled": true, "provider": "matomo", "matomo_url": "https://m.example/matomo", "matomo_site_id": "3"},
		{"enabled": true, "provider": "custom", "custom_snippet": strings.Repeat("y", MaxSnippetBytes)},
	}
	for index, values := range good {
		if err := ReadConfig(values).Validate(); err != nil {
			t.Fatalf("case %d rejected: %v (%v)", index, err, values)
		}
	}
}

func TestInjectPlacesTheSnippetWhereAsked(t *testing.T) {
	page := []byte("<!doctype html><html><head><title>x</title></head><body><div id=\"root\"></div></body></html>")
	head := string(Inject(page, "<script nonce=\"n\">1</script>", "head"))
	if !strings.Contains(head, "</title><script nonce=\"n\">1</script>\n</head>") {
		t.Fatalf("head placement wrong: %s", head)
	}
	body := string(Inject(page, "<script>2</script>", "body"))
	if !strings.Contains(body, "</div><script>2</script>\n</body>") {
		t.Fatalf("body placement wrong: %s", body)
	}
	bare := string(Inject([]byte("<p>no tags"), "<script>3</script>", "head"))
	if !strings.HasSuffix(bare, "<script>3</script>\n") {
		t.Fatalf("fallback placement wrong: %s", bare)
	}
}

func TestRecorderKeepsDistinctOriginsNotCounts(t *testing.T) {
	recorder := NewRecorder()
	moment := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	recorder.now = func() time.Time { return moment }
	recorder.Record("https://momento.corp.example/collect/v1/events", "connect-src", "https://jikim.example/dashboard")
	recorder.Record("https://momento.corp.example/collect/v1/events?x=1", "connect-src https://x", "https://jikim.example/secrets")
	recorder.Record("chrome-extension://abc/x.js", "script-src", "/")
	recorder.Record("data", "img-src", "/")
	recorder.Record("", "img-src", "/")
	recorder.Record("https://momento.corp.example/tracker.js", "", "/")
	items := recorder.List(Config{})
	if len(items) != 1 {
		t.Fatalf("items=%+v", items)
	}
	if items[0].Origin != "https://momento.corp.example" || items[0].Directive != "connect-src" || items[0].Count != 3 || items[0].Page != "/" {
		t.Fatalf("item=%+v", items[0])
	}
	if items[0].Allowed {
		t.Fatal("origin marked allowed without configuration")
	}
	allowed := recorder.List(ReadConfig(map[string]any{"allowed_hosts": "https://momento.corp.example/"}))
	if !allowed[0].Allowed {
		t.Fatal("allowed origin not marked")
	}
	recorder.Forget()
	if len(recorder.List(Config{})) != 0 {
		t.Fatal("forget kept items")
	}
}

func TestRecorderEvictsTheOldestBeyondTheCap(t *testing.T) {
	recorder := NewRecorder()
	tick := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	recorder.now = func() time.Time { tick = tick.Add(time.Second); return tick }
	for index := 0; index < MaxViolations+5; index++ {
		recorder.Record("https://host"+strings.Repeat("x", index%3)+"-"+string(rune('a'+index%26))+strings.Repeat("z", index/26)+".example/p", "script-src", "/")
	}
	items := recorder.List(Config{})
	if len(items) != MaxViolations {
		t.Fatalf("recorder kept %d items", len(items))
	}
	for _, item := range items {
		if item.Origin == "https://host-a.example" {
			t.Fatal("oldest entry survived eviction")
		}
	}
}

func TestWildcardPolicyEntriesMarkViolationsAllowed(t *testing.T) {
	recorder := NewRecorder()
	recorder.Record("https://region1.google-analytics.com/g/collect", "connect-src", "/")
	items := recorder.List(ReadConfig(map[string]any{"enabled": true, "provider": "ga4", "measurement_id": "G-1"}))
	if len(items) != 1 || !items[0].Allowed {
		t.Fatalf("wildcard entry not matched: %+v", items)
	}
}

func TestAddAllowedHostKeepsOrderAndSkipsDuplicates(t *testing.T) {
	list, ok := AddAllowedHost("", "https://a.example/path?x=1")
	if !ok || list != "https://a.example" {
		t.Fatalf("list=%q ok=%v", list, ok)
	}
	list, _ = AddAllowedHost(list, "https://b.example")
	if list != "https://a.example, https://b.example" {
		t.Fatalf("list=%q", list)
	}
	again, _ := AddAllowedHost(list, "HTTPS://A.EXAMPLE/")
	if again != list {
		t.Fatalf("duplicate added: %q", again)
	}
	if _, ok := AddAllowedHost(list, "chrome-extension://x"); ok {
		t.Fatal("non-http origin accepted")
	}
}
