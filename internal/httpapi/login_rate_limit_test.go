package httpapi

import (
	"testing"
	"time"
)

func TestLoginRateLimiterBlocksResetsAndExpires(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	limiter := newLoginRateLimiter()
	limiter.now = func() time.Time { return now }
	limiter.limit = 2
	limiter.window = time.Minute

	limiter.failed("client")
	if blocked, _ := limiter.blocked("client"); blocked {
		t.Fatal("blocked before reaching failure limit")
	}
	limiter.failed("client")
	if blocked, _ := limiter.blocked("client"); !blocked {
		t.Fatal("not blocked at failure limit")
	}
	limiter.succeeded("client")
	if blocked, _ := limiter.blocked("client"); blocked {
		t.Fatal("successful login did not reset failures")
	}
	limiter.failed("client")
	limiter.failed("client")
	now = now.Add(time.Minute)
	if blocked, _ := limiter.blocked("client"); blocked {
		t.Fatal("expired failure window remained blocked")
	}
}
