package oauth

import (
	"sync"
	"time"

	"github.com/ribeirogab/zapgate/internal/clock"
)

const (
	maxPasswordFailures = 5
	failureWindow       = 15 * time.Minute
)

type passwordLimiter struct {
	clock    clock.Clock
	mu       sync.Mutex
	failures map[string][]time.Time
}

func newPasswordLimiter(clk clock.Clock) *passwordLimiter {
	return &passwordLimiter{clock: clk, failures: map[string][]time.Time{}}
}

func (l *passwordLimiter) reserve(ip string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	l.forgetExpired(now)
	recent := l.recent(ip, now)
	if len(recent) >= maxPasswordFailures {
		return recent[len(recent)-maxPasswordFailures].Add(failureWindow).Sub(now), false
	}
	l.failures[ip] = append(recent, now)
	return 0, true
}

func (l *passwordLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
}

func (l *passwordLimiter) recent(ip string, now time.Time) []time.Time {
	var kept []time.Time
	for _, at := range l.failures[ip] {
		if now.Sub(at) < failureWindow {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, ip)
		return nil
	}
	l.failures[ip] = kept
	return kept
}

func (l *passwordLimiter) forgetExpired(now time.Time) {
	for ip := range l.failures {
		l.recent(ip, now)
	}
}
