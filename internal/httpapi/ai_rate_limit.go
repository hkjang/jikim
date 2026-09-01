package httpapi

import (
	"sync"
	"time"
)

type aiRequestState struct {
	windowStart time.Time
	requests    int
	active      int
}

type aiRequestLimiter struct {
	mu            sync.Mutex
	users         map[string]aiRequestState
	maxPerMinute  int
	maxConcurrent int
	now           func() time.Time
}

func newAIRequestLimiter() *aiRequestLimiter {
	return &aiRequestLimiter{users: make(map[string]aiRequestState), maxPerMinute: 30,
		maxConcurrent: 2, now: time.Now}
}

func (l *aiRequestLimiter) acquire(userID string) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	state := l.users[userID]
	if state.windowStart.IsZero() || now.Sub(state.windowStart) >= time.Minute {
		state.windowStart = now
		state.requests = 0
	}
	if state.active >= l.maxConcurrent {
		return false, "동시에 실행할 수 있는 AI 요청 수를 초과했습니다"
	}
	if state.requests >= l.maxPerMinute {
		return false, "분당 AI 요청 한도를 초과했습니다"
	}
	state.active++
	state.requests++
	l.users[userID] = state
	if len(l.users) > 4096 {
		for id, candidate := range l.users {
			if candidate.active == 0 && now.Sub(candidate.windowStart) > 2*time.Minute {
				delete(l.users, id)
			}
		}
	}
	return true, ""
}

func (l *aiRequestLimiter) release(userID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, ok := l.users[userID]
	if !ok {
		return
	}
	if state.active > 0 {
		state.active--
	}
	l.users[userID] = state
}
