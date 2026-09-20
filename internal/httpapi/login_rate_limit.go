package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	loginFailureLimit    = 5
	loginFailureWindow   = 5 * time.Minute
	loginFailureCapacity = 10_000
)

type loginFailureEntry struct {
	count     int
	expiresAt time.Time
}

type loginRateLimiter struct {
	mu       sync.Mutex
	failures map[string]loginFailureEntry
	now      func() time.Time
	limit    int
	window   time.Duration
	capacity int
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{
		failures: make(map[string]loginFailureEntry),
		now:      time.Now,
		limit:    loginFailureLimit,
		window:   loginFailureWindow,
		capacity: loginFailureCapacity,
	}
}

func (l *loginRateLimiter) blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	entry, ok := l.failures[key]
	if !ok {
		return false, 0
	}
	if !entry.expiresAt.After(now) {
		delete(l.failures, key)
		return false, 0
	}
	if entry.count < l.limit {
		return false, 0
	}
	return true, entry.expiresAt.Sub(now)
}

func (l *loginRateLimiter) failed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	entry := l.failures[key]
	if !entry.expiresAt.After(now) {
		entry = loginFailureEntry{expiresAt: now.Add(l.window)}
	}
	entry.count++
	l.failures[key] = entry
	if len(l.failures) > l.capacity {
		l.evict(key, now)
	}
}

// evict brings the table back under capacity without giving up a lockout it
// does not have to. Expired entries go first, then keys that have not reached
// the limit yet; only when the table holds nothing but live lockouts do those
// go, earliest expiry first. Picking victims by map order would let a client
// that locked one account flood failures against other usernames until the
// lockout it wanted gone was evicted. The key that just failed always stays.
func (l *loginRateLimiter) evict(keep string, now time.Time) {
	for existingKey, existing := range l.failures {
		if !existing.expiresAt.After(now) {
			delete(l.failures, existingKey)
		}
	}
	for existingKey, existing := range l.failures {
		if len(l.failures) <= l.capacity {
			return
		}
		if existingKey != keep && existing.count < l.limit {
			delete(l.failures, existingKey)
		}
	}
	if len(l.failures) <= l.capacity {
		return
	}
	locked := make([]string, 0, len(l.failures))
	for existingKey := range l.failures {
		if existingKey != keep {
			locked = append(locked, existingKey)
		}
	}
	sort.Slice(locked, func(i, j int) bool {
		return l.failures[locked[i]].expiresAt.Before(l.failures[locked[j]].expiresAt)
	})
	for _, existingKey := range locked {
		if len(l.failures) <= l.capacity {
			return
		}
		delete(l.failures, existingKey)
	}
}

func (l *loginRateLimiter) succeeded(key string) {
	l.mu.Lock()
	delete(l.failures, key)
	l.mu.Unlock()
}

func loginRateKey(r *http.Request, username string) string {
	return remoteIP(r) + "\x00" + strings.ToLower(strings.TrimSpace(username))
}

func (s *Server) rejectRateLimitedLogin(w http.ResponseWriter, r *http.Request, key string, openBao bool) bool {
	if s.loginLimiter == nil {
		return false
	}
	blocked, retryAfter := s.loginLimiter.blocked(key)
	if !blocked {
		return false
	}
	seconds := int((retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
	if openBao {
		baoError(w, http.StatusTooManyRequests, "too many login attempts; retry later")
	} else {
		writeError(w, r, http.StatusTooManyRequests, "login_rate_limited", "로그인 시도가 너무 많습니다. 잠시 후 다시 시도하세요")
	}
	return true
}
