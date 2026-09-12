package tracking

import (
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxViolations bounds the recorder. A blocked request repeats on every page
// view, so what matters is which origins are blocked, not how many times — a
// small ring of distinct origins is enough to fix a snippet.
const MaxViolations = 100

// Violation is one origin the content security policy refused, kept with the
// directive that refused it so the console can say what to allow.
type Violation struct {
	Origin    string    `json:"origin"`
	Directive string    `json:"directive"`
	Page      string    `json:"page"`
	Count     int       `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Allowed   bool      `json:"allowed"`
}

// Recorder collects the policy violations browsers report. It is deliberately
// in memory: the reports are a live troubleshooting aid for the person pasting
// a snippet, not an audit record, and an unauthenticated endpoint must not be
// able to grow the database.
type Recorder struct {
	mutex      sync.Mutex
	violations map[string]*Violation
	now        func() time.Time
}

func NewRecorder() *Recorder {
	return &Recorder{violations: make(map[string]*Violation), now: time.Now}
}

// Record notes one blocked request. Anything that is not an http origin, such
// as a browser extension or a data: URL, is ignored because allowing it is
// neither possible nor useful.
func (r *Recorder) Record(blockedURI, directive, page string) {
	origin := originOf(blockedURI)
	if origin == "" {
		return
	}
	directive = strings.TrimSpace(strings.ToLower(directive))
	if index := strings.IndexByte(directive, ' '); index > 0 {
		directive = directive[:index]
	}
	if directive == "" {
		directive = "connect-src"
	}
	page = strings.TrimSpace(page)
	if len(page) > 512 {
		page = page[:512]
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	key := directive + " " + origin
	if existing, found := r.violations[key]; found {
		existing.Count++
		existing.LastSeen = r.now()
		existing.Page = page
		return
	}
	if len(r.violations) >= MaxViolations {
		r.evictOldest()
	}
	moment := r.now()
	r.violations[key] = &Violation{Origin: origin, Directive: directive, Page: page, Count: 1, FirstSeen: moment, LastSeen: moment}
}

func (r *Recorder) evictOldest() {
	var oldestKey string
	var oldest time.Time
	for key, violation := range r.violations {
		if oldestKey == "" || violation.LastSeen.Before(oldest) {
			oldestKey, oldest = key, violation.LastSeen
		}
	}
	delete(r.violations, oldestKey)
}

// List returns the blocked origins, most recent first, marking the ones the
// current configuration already allows so a fixed snippet stops nagging.
func (r *Recorder) List(config Config) []Violation {
	allowed := make(map[string]struct{})
	scripts, connects, images := config.PolicySources()
	for _, group := range [][]string{scripts, connects, images} {
		for _, origin := range group {
			allowed[strings.ToLower(strings.TrimSuffix(origin, "/"))] = struct{}{}
		}
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	items := make([]Violation, 0, len(r.violations))
	for _, violation := range r.violations {
		copied := *violation
		_, known := allowed[copied.Origin]
		copied.Allowed = known || matchesWildcard(copied.Origin, allowed)
		items = append(items, copied)
	}
	sort.Slice(items, func(first, second int) bool {
		if items[first].LastSeen.Equal(items[second].LastSeen) {
			return items[first].Origin < items[second].Origin
		}
		return items[first].LastSeen.After(items[second].LastSeen)
	})
	return items
}

// Forget drops the recorded violations, which is what an administrator does
// after fixing a snippet to check whether anything is still blocked.
func (r *Recorder) Forget() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.violations = make(map[string]*Violation)
}

// matchesWildcard covers policy entries such as https://*.google-analytics.com.
func matchesWildcard(origin string, allowed map[string]struct{}) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	for pattern := range allowed {
		star := strings.Index(pattern, "*.")
		if star < 0 {
			continue
		}
		if strings.HasPrefix(origin, pattern[:star]) && strings.HasSuffix(parsed.Host, pattern[star+1:]) {
			return true
		}
	}
	return false
}

// AddAllowedHost appends an origin to the allow list, leaving the existing
// entries and their order alone. It returns "" when the origin is not an
// http(s) address, because such a thing cannot be allowed.
func AddAllowedHost(existing, origin string) (string, bool) {
	origin = originOf(origin)
	if origin == "" {
		return existing, false
	}
	for _, host := range SplitHosts(existing) {
		if strings.EqualFold(strings.TrimSuffix(host, "/"), origin) {
			return existing, true
		}
	}
	if strings.TrimSpace(existing) == "" {
		return origin, true
	}
	return strings.TrimSpace(existing) + ", " + origin, true
}
