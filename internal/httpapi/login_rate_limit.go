package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	loginFailureLimit  = 5
	loginFailureWindow = 5 * time.Minute
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
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{
		failures: make(map[string]loginFailureEntry),
		now:      time.Now,
		limit:    loginFailureLimit,
		window:   loginFailureWindow,
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
	if len(l.failures) > 10_000 {
		for existingKey, existing := range l.failures {
			if !existing.expiresAt.After(now) {
				delete(l.failures, existingKey)
			}
		}
		for existingKey := range l.failures {
			if len(l.failures) <= 10_000 {
				break
			}
			if existingKey != key {
				delete(l.failures, existingKey)
			}
		}
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
