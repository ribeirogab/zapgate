package webhooks

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	jitterRatio   = 0.2
	maxRetryAfter = time.Hour
)

var retrySchedule = []time.Duration{
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	time.Hour,
	2 * time.Hour,
	4 * time.Hour,
	6 * time.Hour,
	8 * time.Hour,
}

func retriesLeft(attempts int) bool {
	return attempts >= 1 && attempts <= len(retrySchedule)
}

func (d *Dispatcher) retryDelay(attempts int, result Result, at time.Time) time.Duration {
	if wait, ok := askedWait(result, at); ok {
		return wait
	}
	return jittered(retrySchedule[attempts-1], d.deps.Random())
}

func jittered(delay time.Duration, random float64) time.Duration {
	return delay + time.Duration(float64(delay)*jitterRatio*(2*random-1))
}

func askedWait(result Result, at time.Time) (time.Duration, bool) {
	if !result.AsksToWait() {
		return 0, false
	}
	value := strings.TrimSpace(result.RetryAfter)
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(min(seconds, int(maxRetryAfter/time.Second))) * time.Second, true
	}
	if date, err := http.ParseTime(value); err == nil {
		return min(max(date.Sub(at), 0), maxRetryAfter), true
	}
	return 0, false
}
