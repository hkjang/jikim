package httpapi

import (
	"fmt"
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

// When the table holds nothing but live lockouts, the one that would have
// expired first goes — not whichever the map iteration happened to visit.
func TestLoginRateLimiterEvictsEarliestExpiringLockoutWhenFull(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	limiter := newLoginRateLimiter()
	limiter.now = func() time.Time { return now }
	limiter.limit = 2
	limiter.window = time.Hour
	if limiter.capacity != 10_000 {
		t.Fatalf("default capacity=%d, want 10000", limiter.capacity)
	}

	for i := 0; i < limiter.capacity; i++ {
		limiter.failures[fmt.Sprintf("locked-%d", i)] = loginFailureEntry{
			count:     limiter.limit,
			expiresAt: now.Add(time.Minute + time.Duration(i)*time.Second),
		}
	}
	limiter.failed("new")

	if got := len(limiter.failures); got != limiter.capacity {
		t.Fatalf("len(failures)=%d, want %d", got, limiter.capacity)
	}
	if _, ok := limiter.failures["locked-0"]; ok {
		t.Fatal("earliest-expiring lockout survived while the table was over capacity")
	}
	if entry, ok := limiter.failures["new"]; !ok || entry.count != 1 {
		t.Fatalf("just-failed key was not kept: %#v ok=%t", entry, ok)
	}
	for i := 1; i < limiter.capacity; i++ {
		if _, ok := limiter.failures[fmt.Sprintf("locked-%d", i)]; !ok {
			t.Fatalf("locked-%d was evicted ahead of the earliest-expiring entry", i)
		}
	}
}
