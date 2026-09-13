package syncengine

import "time"

// Clock is the engine's only source of time. Everything that waits — the
// debounce window, the retry ladder, the rate limiter, the journal's write
// coalescing — goes through it, so a test can drive the whole engine by
// advancing a fake instead of sleeping.
type Clock interface {
	// Now reports the current time.
	Now() time.Time
	// AfterFunc runs f once, after d has elapsed on this clock. A non-positive
	// d runs f as soon as possible.
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is a pending [Clock.AfterFunc] callback.
type Timer interface {
	// Stop cancels the callback. It reports whether it was stopped before it
	// ran; false means the callback has already fired or already been stopped,
	// which is what the caller needs to know to keep its wait-group balanced.
	Stop() bool
}

// realClock is the default clock, backed by the time package.
type realClock struct{}

// SystemClock is the real clock. It is the default for an [Engine] built
// without one.
var SystemClock Clock = realClock{}

// Now reports the wall-clock time.
func (realClock) Now() time.Time { return time.Now() }

// AfterFunc schedules f on the runtime timer wheel.
func (realClock) AfterFunc(d time.Duration, f func()) Timer {
	if d < 0 {
		d = 0
	}
	return time.AfterFunc(d, f)
}
