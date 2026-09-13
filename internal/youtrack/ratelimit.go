package youtrack

import (
	"context"
	"sync"
	"time"
)

// limiter is a token bucket with a single monotonically advancing next slot.
// Every caller reserves the next free slot under the mutex and then sleeps
// outside it, so N concurrent goroutines are spread over N slots rather than
// bursting. A nil limiter, or one with a non-positive interval, never waits.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time

	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// newLimiter builds a limiter for rate requests per second. A non-positive
// rate disables throttling.
func newLimiter(rate float64, now func() time.Time, sleep func(context.Context, time.Duration) error) *limiter {
	l := &limiter{now: now, sleep: sleep}
	if rate > 0 {
		l.interval = time.Duration(float64(time.Second) / rate)
	}
	return l
}

// reserve claims the next slot and returns how long the caller must wait.
func (l *limiter) reserve() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	slot := now
	if l.next.After(slot) {
		slot = l.next
	}
	l.next = slot.Add(l.interval)
	return slot.Sub(now)
}

// wait blocks until this caller's slot is due, or until ctx is done.
func (l *limiter) wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return ctxErr(err)
	}
	if l == nil || l.interval <= 0 {
		return nil
	}
	if d := l.reserve(); d > 0 {
		return l.sleep(ctx, d)
	}
	return nil
}
