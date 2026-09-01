package httpapi

import (
	"testing"
	"time"
)

func TestAIRequestLimiterConcurrencyAndWindow(t *testing.T) {
	limiter := newAIRequestLimiter()
	now := time.Unix(1_700_000_000, 0)
	limiter.now = func() time.Time { return now }
	if ok, _ := limiter.acquire("u1"); !ok {
		t.Fatal("first request rejected")
	}
	if ok, _ := limiter.acquire("u1"); !ok {
		t.Fatal("second concurrent request rejected")
	}
	if ok, _ := limiter.acquire("u1"); ok {
		t.Fatal("concurrency limit was not enforced")
	}
	limiter.release("u1")
	if ok, _ := limiter.acquire("u1"); !ok {
		t.Fatal("released slot was not reusable")
	}
	limiter.release("u1")
	limiter.release("u1")
	for i := 3; i < limiter.maxPerMinute; i++ {
		if ok, _ := limiter.acquire("u1"); !ok {
			t.Fatalf("request %d unexpectedly rejected", i+1)
		}
		limiter.release("u1")
	}
	if ok, _ := limiter.acquire("u1"); ok {
		t.Fatal("minute limit was not enforced")
	}
	now = now.Add(time.Minute)
	if ok, _ := limiter.acquire("u1"); !ok {
		t.Fatal("minute window did not reset")
	}
}
