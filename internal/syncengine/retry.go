package syncengine

import (
	"fmt"
	"math/rand/v2"
	"time"
)

// The retry ladder the engine ships with. It is the same shape the git sync
// already uses (internal/gitops/sync.go, defaultBackoff): 500 ms, then 1.5 s,
// then 4 s, then the cap. Five attempts is enough to ride out a restart of the
// remote and short enough that a genuinely broken job surfaces within a minute.
const (
	DefaultMaxAttempts = 5
	DefaultRetryBase   = 500 * time.Millisecond
	DefaultRetryMax    = 30 * time.Second
	DefaultRetryJitter = 0.2
)

// RetryPolicy decides when — and whether — a failed job runs again.
//
// The delay grows by a factor of three from Base, which reproduces the
// 500 ms / 1.5 s / 4.5 s ladder, is capped at Max, and is then spread by
// Jitter so a hundred jobs failing against the same remote at the same instant
// do not all come back at the same instant either.
type RetryPolicy struct {
	// MaxAttempts is how many handler calls a job may take part in before it is
	// dead-lettered. Zero means DefaultMaxAttempts; one means never retry.
	MaxAttempts int
	// Base is the first delay. Zero means DefaultRetryBase.
	Base time.Duration
	// Max caps the delay however many attempts have failed. Zero means
	// DefaultRetryMax.
	Max time.Duration
	// Jitter is the fraction of the computed delay the actual delay may deviate
	// by, in [0, 1]. Zero means DefaultRetryJitter; a negative value disables
	// jitter, which is what a test that asserts an exact ladder wants.
	Jitter float64
}

// withDefaults fills the zero fields.
func (p RetryPolicy) withDefaults() RetryPolicy {
	if p.MaxAttempts == 0 {
		p.MaxAttempts = DefaultMaxAttempts
	}
	if p.Base == 0 {
		p.Base = DefaultRetryBase
	}
	if p.Max == 0 {
		p.Max = DefaultRetryMax
	}
	if p.Jitter == 0 {
		p.Jitter = DefaultRetryJitter
	}
	return p
}

// validate reports a policy that cannot be honored.
func (p RetryPolicy) validate() error {
	switch {
	case p.MaxAttempts < 0:
		return fmt.Errorf("%w: retry MaxAttempts must not be negative", ErrInvalidOption)
	case p.Base < 0:
		return fmt.Errorf("%w: retry Base must not be negative", ErrInvalidOption)
	case p.Max < 0:
		return fmt.Errorf("%w: retry Max must not be negative", ErrInvalidOption)
	case p.Jitter > 1:
		return fmt.Errorf("%w: retry Jitter must be at most 1", ErrInvalidOption)
	}
	return nil
}

// backoff is the jitter-free delay before attempt number attempt+1, given that
// `attempt` attempts have already failed. It is exported through Delay; kept
// separate so tests can assert the ladder without the jitter.
func (p RetryPolicy) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := p.Base
	for i := 1; i < attempt; i++ {
		d *= 3
		if d >= p.Max {
			return p.Max
		}
	}
	if d > p.Max {
		return p.Max
	}
	return d
}

// Delay is how long to wait before the next attempt of a job that has already
// failed `attempt` times. A Retry-After carried by the error always wins: the
// remote knows when it will serve us again and the ladder does not.
//
// randFloat supplies the jitter and is injected so a test can make the result
// exact; nil means math/rand.
func (p RetryPolicy) Delay(attempt int, retryAfter time.Duration, randFloat func() float64) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	d := p.backoff(attempt)
	if p.Jitter <= 0 || d <= 0 {
		return d
	}
	if randFloat == nil {
		randFloat = rand.Float64
	}
	// A symmetric spread around the computed delay: (1 ± jitter).
	factor := 1 + p.Jitter*(2*randFloat()-1)
	out := time.Duration(float64(d) * factor)
	if out < 0 {
		return 0
	}
	if out > p.Max {
		return p.Max
	}
	return out
}

// exhausted reports whether a job that has now failed `attempts` times has run
// out of attempts.
func (p RetryPolicy) exhausted(attempts int) bool {
	return attempts >= p.MaxAttempts
}
