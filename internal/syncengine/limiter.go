package syncengine

import (
	"context"
	"sync"
	"time"
)

// limiter is the one thing every worker passes through before it calls a
// handler, so the outbound rate is a property of the engine and not of the
// number of workers: doubling the pool makes batches start sooner, never makes
// the remote see twice the traffic.
//
// It is a token bucket refilled from the injectable clock rather than from a
// background goroutine, so a test advances time and the bucket fills.
type limiter struct {
	clock Clock

	mu     sync.Mutex
	rate   float64 // tokens per second; zero or less means unlimited
	burst  float64
	tokens float64
	last   time.Time
	// taken counts the tokens ever handed out, which is what a throughput test
	// asserts on.
	taken int
}

// newLimiter builds a limiter. A rate of zero or less means no limit at all.
func newLimiter(clock Clock, rate float64, burst int) *limiter {
	l := &limiter{clock: clock, last: clock.Now()}
	l.setRate(rate, burst)
	// A fresh limiter starts full: the first burst of a run is not something to
	// throttle, the sustained rate after it is.
	l.mu.Lock()
	l.tokens = l.burst
	l.mu.Unlock()
	return l
}

// setRate changes the rate and the burst on a running limiter. The bucket keeps
// whatever it holds, clamped to the new burst, so lowering the rate takes
// effect immediately instead of after the old bucket drains.
func (l *limiter) setRate(rate float64, burst int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill(l.clock.Now())
	l.rate = rate
	b := float64(burst)
	if b <= 0 {
		b = 1
		if rate > 1 {
			b = rate
		}
	}
	l.burst = b
	if l.tokens > b {
		l.tokens = b
	}
}

// rateOf reports the configured rate.
func (l *limiter) rateOf() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rate
}

// refill adds the tokens that elapsed time earned. Called with mu held.
func (l *limiter) refill(now time.Time) {
	if l.last.IsZero() {
		l.last = now
		return
	}
	elapsed := now.Sub(l.last)
	if elapsed <= 0 {
		return
	}
	l.last = now
	if l.rate <= 0 {
		return
	}
	l.tokens += elapsed.Seconds() * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
}

// reserve takes a token if one is available and reports zero, or reports how
// long the caller must wait for one. It never reserves the future: waiting
// callers race for the next token, which is what keeps the total rate correct
// however many of them there are.
func (l *limiter) reserve() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	l.refill(now)
	if l.rate <= 0 {
		l.taken++
		return 0
	}
	if l.tokens >= 1 {
		l.tokens--
		l.taken++
		return 0
	}
	need := (1 - l.tokens) / l.rate
	d := time.Duration(need * float64(time.Second))
	if d <= 0 {
		d = time.Nanosecond
	}
	return d
}

// Wait blocks until the limiter grants a token or ctx is done. The wait is a
// timer on the engine's clock, so a fake clock drives it exactly as it drives
// the debounce and the retry ladder.
func (l *limiter) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err //nolint:wrapcheck // a cancelled context is reported verbatim
		}
		d := l.reserve()
		if d <= 0 {
			return nil
		}
		ready := make(chan struct{})
		timer := l.clock.AfterFunc(d, func() { close(ready) })
		select {
		case <-ready:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err() //nolint:wrapcheck // a cancelled context is reported verbatim
		}
	}
}

// count reports how many tokens the limiter has handed out.
func (l *limiter) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.taken
}
