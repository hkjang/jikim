// Package tracking renders the visitor tracking snippet an administrator
// configured and the content security policy sources it needs.
//
// jikim ships with script-src 'self', so a pasted snippet would be refused by
// the browser without a word to the administrator. This package produces both
// halves of the answer — the markup to inject and the policy additions — with
// a per-request nonce so the inline code runs without loosening the policy for
// anything else.
package tracking

import (
	"fmt"
	"html"
	"net/url"
	"strings"
)

const (
	ProviderNone    = "none"
	ProviderMomento = "momento"
	ProviderGA4     = "ga4"
	ProviderGTM     = "gtm"
	ProviderMatomo  = "matomo"
	ProviderCustom  = "custom"

	// MaxSnippetBytes bounds a pasted snippet. A tracker loader is a few
	// hundred bytes; anything larger is a sign the wrong thing was pasted.
	MaxSnippetBytes = 8 * 1024

	// MomentoProxyPrefix is the same-origin path the server forwards to the
	// Momento collector, so the browser never talks to another origin.
	MomentoProxyPrefix = "/momento"
)

var Providers = []string{ProviderNone, ProviderMomento, ProviderGA4, ProviderGTM, ProviderMatomo, ProviderCustom}

type Config struct {
	Enabled            bool
	Provider           string
	MomentoURL         string
	MomentoSiteID      string
	MomentoEnvironment string
	MomentoProxy       bool
	MeasurementID      string
	MatomoURL          string
	MatomoSiteID       string
	CustomSnippet      string
	AllowedHosts       string
	IncludeAdmin       bool
	Placement          string
	AllowInsecureHTTP  bool
}

// ReadConfig maps the stored `tracking` setting onto the configuration. A
// missing or mistyped value falls back to the default, which is always the
// switched-off one.
func ReadConfig(values map[string]any) Config {
	config := Config{Provider: ProviderNone, Placement: "head", MomentoProxy: true, MomentoEnvironment: "prd"}
	config.Enabled, _ = values["enabled"].(bool)
	config.Provider = strings.ToLower(stringValue(values, "provider", ProviderNone))
	config.MomentoURL = stringValue(values, "momento_url", "")
	config.MomentoSiteID = stringValue(values, "momento_site_id", "")
	config.MomentoEnvironment = stringValue(values, "momento_environment", "prd")
	if raw, ok := values["momento_proxy"].(bool); ok {
		config.MomentoProxy = raw
	}
	config.MeasurementID = stringValue(values, "measurement_id", "")
	config.MatomoURL = stringValue(values, "matomo_url", "")
	config.MatomoSiteID = stringValue(values, "matomo_site_id", "")
	config.CustomSnippet, _ = values["custom_snippet"].(string)
	config.AllowedHosts = stringValue(values, "allowed_hosts", "")
	config.IncludeAdmin, _ = values["include_admin"].(bool)
	config.Placement = strings.ToLower(stringValue(values, "placement", "head"))
	if config.Placement != "body" {
		config.Placement = "head"
	}
	config.AllowInsecureHTTP, _ = values["allow_insecure_http"].(bool)
	return config
}

// Active reports whether the page at path should carry the snippet. The
// administrative screens are left out unless asked for, because console
// traffic is rarely the visitor data anybody wants to measure.
func (c Config) Active(path string) bool {
	if !c.Enabled || c.Provider == ProviderNone || c.Provider == "" {
		return false
	}
	if !c.IncludeAdmin && AdminPath(path) {
		return false
	}
	return strings.TrimSpace(c.Snippet("")) != ""
}

// AdminPath reports the screens only an administrator opens.
func AdminPath(path string) bool {
	return strings.HasPrefix(path, "/admin/") || path == "/admin" ||
		strings.HasPrefix(path, "/infrastructure/") || path == "/infrastructure" ||
		path == "/access/authentication"
}

// ProxyActive reports whether /momento/* should be forwarded to the collector.
func (c Config) ProxyActive() bool {
	return c.Enabled && c.Provider == ProviderMomento && c.MomentoProxy && strings.TrimSpace(c.MomentoURL) != ""
}

// Validate reports what is missing or wrong for the chosen provider. The
// snippet size and the provider name are checked even while tracking is off,
// so a bad value is refused at save time rather than discovered at switch-on.
func (c Config) Validate() error {
	if !validProvider(c.Provider) {
		return fmt.Errorf("provider는 %s 중 하나여야 합니다", strings.Join(Providers, ", "))
	}
	if len(c.CustomSnippet) > MaxSnippetBytes {
		return fmt.Errorf("custom_snippet은 %d바이트를 넘을 수 없습니다", MaxSnippetBytes)
	}
	for _, field := range []struct{ name, value string }{{"momento_url", c.MomentoURL}, {"matomo_url", c.MatomoURL}} {
		if strings.TrimSpace(field.value) == "" {
			continue
		}
		if err := validateCollectorURL(field.value, c.AllowInsecureHTTP); err != nil {
			return fmt.Errorf("%s: %v", field.name, err)
		}
	}
	if !c.Enabled {
		return nil
	}
	switch c.Provider {
	case ProviderNone:
		return fmt.Errorf("추적을 켜려면 provider를 선택해야 합니다")
	case ProviderMomento:
		if strings.TrimSpace(c.MomentoURL) == "" || strings.TrimSpace(c.MomentoSiteID) == "" {
			return fmt.Errorf("momento_url과 momento_site_id가 필요합니다")
		}
	case ProviderGA4, ProviderGTM:
		if strings.TrimSpace(c.MeasurementID) == "" {
			return fmt.Errorf("measurement_id가 필요합니다")
		}
	case ProviderMatomo:
		if strings.TrimSpace(c.MatomoURL) == "" || strings.TrimSpace(c.MatomoSiteID) == "" {
			return fmt.Errorf("matomo_url과 matomo_site_id가 필요합니다")
		}
	case ProviderCustom:
		if strings.TrimSpace(c.CustomSnippet) == "" {
			return fmt.Errorf("custom_snippet이 비어 있습니다")
		}
	}
	return nil
}

func validProvider(provider string) bool {
	for _, known := range Providers {
		if provider == known {
			return true
		}
	}
	return false
}

// validateCollectorURL follows the rule the other integrations use: an http(s)
// origin, no credentials, and plain HTTP only when the administrator opted in
// (loopback excepted).
func validateCollectorURL(raw string, allowInsecure bool) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return fmt.Errorf("http(s) URL 형식이 아닙니다")
	}
	if parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return fmt.Errorf("사용자 정보, fragment 또는 query를 포함할 수 없습니다")
	}
	if parsed.Scheme == "http" && !allowInsecure {
		host := strings.ToLower(parsed.Hostname())
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return fmt.Errorf("HTTPS가 필요합니다(HTTP 사용 시 allow_insecure_http를 명시하세요)")
		}
	}
	return nil
}

// Snippet renders the markup to inject. The nonce is applied to every script
// tag so the policy can stay at script-src 'self' plus that one nonce.
func (c Config) Snippet(nonce string) string {
	switch c.Provider {
	case ProviderMomento:
		site := html.EscapeString(strings.TrimSpace(c.MomentoSiteID))
		base := strings.TrimRight(strings.TrimSpace(c.MomentoURL), "/")
		if site == "" || base == "" {
			return ""
		}
		environment := html.EscapeString(strings.TrimSpace(c.MomentoEnvironment))
		if environment == "" {
			environment = "prd"
		}
		if c.MomentoProxy {
			// Through the proxy the loader and the events both stay on this
			// origin, so the policy needs no new source at all.
			return withNonce(fmt.Sprintf(`<script async src="%s/tracker.js" data-site-id="%s" data-environment="%s" data-contract-version="1" data-endpoint="%s"></script>`,
				MomentoProxyPrefix, site, environment, MomentoProxyPrefix), nonce)
		}
		return withNonce(fmt.Sprintf(`<script async src="%s/tracker.js" data-site-id="%s" data-environment="%s" data-contract-version="1"></script>`,
			html.EscapeString(base), site, environment), nonce)
	case ProviderGA4:
		id := html.EscapeString(strings.TrimSpace(c.MeasurementID))
		if id == "" {
			return ""
		}
		return withNonce(fmt.Sprintf(`<script async src="https://www.googletagmanager.com/gtag/js?id=%s"></script>
<script>window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}gtag('js',new Date());gtag('config','%s');</script>`, id, id), nonce)
	case ProviderGTM:
		id := html.EscapeString(strings.TrimSpace(c.MeasurementID))
		if id == "" {
			return ""
		}
		return withNonce(fmt.Sprintf(`<script>(function(w,d,s,l,i){w[l]=w[l]||[];w[l].push({'gtm.start':new Date().getTime(),event:'gtm.js'});var f=d.getElementsByTagName(s)[0],j=d.createElement(s),dl=l!='dataLayer'?'&l='+l:'';j.async=true;j.src='https://www.googletagmanager.com/gtm.js?id='+i+dl;f.parentNode.insertBefore(j,f);})(window,document,'script','dataLayer','%s');</script>`, id), nonce)
	case ProviderMatomo:
		base := strings.TrimRight(strings.TrimSpace(c.MatomoURL), "/")
		site := html.EscapeString(strings.TrimSpace(c.MatomoSiteID))
		if base == "" || site == "" {
			return ""
		}
		return withNonce(fmt.Sprintf(`<script>var _paq=window._paq=window._paq||[];_paq.push(['trackPageView']);_paq.push(['enableLinkTracking']);(function(){var u="%s/";_paq.push(['setTrackerUrl',u+'matomo.php']);_paq.push(['setSiteId','%s']);var d=document,g=d.createElement('script'),s=d.getElementsByTagName('script')[0];g.async=true;g.src=u+'matomo.js';s.parentNode.insertBefore(g,s);})();</script>`, html.EscapeString(base), site), nonce)
	case ProviderCustom:
		return withNonce(strings.TrimSpace(c.CustomSnippet), nonce)
	}
	return ""
}

// withNonce adds the nonce to every script tag that does not already carry
// one, which is what lets a pasted snippet run under a strict policy unchanged.
func withNonce(snippet, nonce string) string {
	if nonce == "" || snippet == "" {
		return snippet
	}
	var builder strings.Builder
	remaining := snippet
	for {
		index := strings.Index(strings.ToLower(remaining), "<script")
		if index < 0 {
			builder.WriteString(remaining)
			return builder.String()
		}
		end := index + len("<script")
		builder.WriteString(remaining[:end])
		tag := remaining[end:]
		if closing := strings.Index(tag, ">"); closing >= 0 {
			tag = tag[:closing]
		}
		if !strings.Contains(strings.ToLower(tag), "nonce=") {
			builder.WriteString(` nonce="` + html.EscapeString(nonce) + `"`)
		}
		remaining = remaining[end:]
	}
}

// Inject places the snippet just before the closing tag the placement names,
// falling back to the end of the document when that tag is missing.
func Inject(page []byte, snippet, placement string) []byte {
	marker := "</head>"
	if placement == "body" {
		marker = "</body>"
	}
	text := string(page)
	index := strings.LastIndex(strings.ToLower(text), marker)
	if index < 0 {
		return []byte(text + "\n" + snippet + "\n")
	}
	return []byte(text[:index] + snippet + "\n" + text[index:])
}

// PolicySources lists the extra origins the snippet needs, derived from the
// provider so the common setups need no policy knowledge at all.
func (c Config) PolicySources() (scripts []string, connects []string, images []string) {
	add := func(origin string) {
		scripts = append(scripts, origin)
		connects = append(connects, origin)
		images = append(images, origin)
	}
	switch c.Provider {
	case ProviderMomento:
		if !c.MomentoProxy {
			if origin := originOf(c.MomentoURL); origin != "" {
				add(origin)
			}
		}
	case ProviderGA4, ProviderGTM:
		scripts = append(scripts, "https://www.googletagmanager.com")
		connects = append(connects, "https://www.google-analytics.com", "https://analytics.google.com", "https://*.google-analytics.com")
		images = append(images, "https://www.google-analytics.com", "https://www.googletagmanager.com")
	case ProviderMatomo:
		if origin := originOf(c.MatomoURL); origin != "" {
			add(origin)
		}
	case ProviderCustom:
		// A pasted snippet names the addresses it loads and reports to, so
		// those origins are allowed without anybody reading a policy error.
		for _, origin := range SnippetOrigins(c.CustomSnippet) {
			add(origin)
		}
	}
	for _, host := range SplitHosts(c.AllowedHosts) {
		add(host)
	}
	return scripts, connects, images
}

// SplitHosts reads the comma, space or newline separated allow list.
func SplitHosts(raw string) []string {
	hosts := make([]string, 0, 2)
	for _, host := range strings.FieldsFunc(raw, func(letter rune) bool {
		return letter == ',' || letter == ' ' || letter == '\n' || letter == '\r' || letter == '\t'
	}) {
		if trimmed := strings.TrimSpace(host); trimmed != "" {
			hosts = append(hosts, trimmed)
		}
	}
	return hosts
}

// SnippetOrigins lists every http(s) origin written into a snippet: the script
// it loads, the endpoint it posts to, the pixel it requests.
func SnippetOrigins(snippet string) []string {
	origins := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	lowered := strings.ToLower(snippet)
	for index := 0; index < len(snippet); {
		start := strings.Index(lowered[index:], "http")
		if start < 0 {
			break
		}
		start += index
		end := start
		for end < len(snippet) && !isURLBoundary(snippet[end]) {
			end++
		}
		index = end
		origin := originOf(snippet[start:end])
		if origin == "" {
			continue
		}
		if _, duplicate := seen[origin]; duplicate {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	return origins
}

// isURLBoundary reports the characters that cannot appear in a URL written
// inside HTML or JavaScript, which is where each address ends.
func isURLBoundary(letter byte) bool {
	switch letter {
	case '"', '\'', '`', '<', '>', ' ', '\t', '\n', '\r', ')', ',', ';', '\\', '+':
		return true
	}
	return false
}

// originOf reduces a URL to scheme://host, which is the shape a policy source
// takes. Anything that is not an http(s) address yields "".
func originOf(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return scheme + "://" + strings.ToLower(parsed.Host)
}

func stringValue(values map[string]any, key, fallback string) string {
	if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
