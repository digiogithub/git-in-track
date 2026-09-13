package syncengine

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"testing"
	"time"
)

// fakeClock is a clock a test drives by hand. Advancing it fires every timer
// whose deadline has passed, in deadline order, so the debounce, the retry
// ladder, the rate limiter and the journal's write coalescing all run exactly
// when the test says they do and never a moment sooner.
//
// Callbacks run on the goroutine that advances the clock. That is what makes a
// test deterministic: when Advance returns, everything it fired has finished.
type fakeClock struct {
	mu      sync.Mutex
	cond    *sync.Cond
	now     time.Time
	timers  []*fakeTimer
	nextSeq uint64
}

// fakeTimer is one pending callback.
type fakeTimer struct {
	clock    *fakeClock
	deadline time.Time
	seq      uint64
	fn       func()
	fired    bool
	stopped  bool
}

// newFakeClock starts a clock at a fixed, readable instant.
func newFakeClock() *fakeClock {
	c := &fakeClock{now: time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// Now reports the fake time.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// AfterFunc registers a callback for the fake deadline.
func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	if d < 0 {
		d = 0
	}
	c.mu.Lock()
	c.nextSeq++
	t := &fakeTimer{clock: c, deadline: c.now.Add(d), seq: c.nextSeq, fn: f}
	c.timers = append(c.timers, t)
	c.cond.Broadcast()
	c.mu.Unlock()
	return t
}

// Stop cancels a pending callback.
func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.fired || t.stopped {
		return false
	}
	t.stopped = true
	t.clock.remove(t)
	return true
}

// remove drops a timer from the pending list. Called with the clock locked.
func (c *fakeClock) remove(target *fakeTimer) {
	out := c.timers[:0]
	for _, t := range c.timers {
		if t != target {
			out = append(out, t)
		}
	}
	c.timers = out
}

// Advance moves the clock forward, firing everything that comes due on the way.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	c.advanceTo(target)
}

// advanceTo fires every timer due at or before target, one at a time, moving
// the clock to each deadline first so a callback that reads Now sees the moment
// it was scheduled for.
func (c *fakeClock) advanceTo(target time.Time) {
	for {
		c.mu.Lock()
		var next *fakeTimer
		for _, t := range c.timers {
			if t.deadline.After(target) {
				continue
			}
			if next == nil || t.deadline.Before(next.deadline) ||
				(t.deadline.Equal(next.deadline) && t.seq < next.seq) {
				next = t
			}
		}
		if next == nil {
			c.now = target
			c.mu.Unlock()
			return
		}
		c.now = next.deadline
		next.fired = true
		c.remove(next)
		fn := next.fn
		c.mu.Unlock()
		fn()
	}
}

// BlockUntil waits until at least n timers are pending. It is how a test waits
// for goroutines it cannot see — a worker blocked on the rate limiter, say —
// without sleeping.
func (c *fakeClock) BlockUntil(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.timers) < n {
		c.cond.Wait()
	}
}

// pending reports how many timers are armed.
func (c *fakeClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

// fakeHandler is a scriptable handler. It records every batch it received and
// answers according to a script indexed by call number, so a test can say
// "fail twice, then succeed" without a single sleep.
type fakeHandler struct {
	mu sync.Mutex
	// script answers call number i (0-based). A call past the end of the script
	// uses fallback.
	script []func(ctx context.Context, batch []Job) error
	// fallback answers every call the script does not cover. Nil means success.
	fallback func(ctx context.Context, batch []Job) error
	calls    int
	batches  [][]Job
	// entered is signaled with one token per handler entry, so a test can wait
	// for the handler to be running without polling.
	entered chan struct{}
	// release, when non-nil, blocks every call until the test closes or feeds
	// it, which is how a test gets a job to sit in the running state.
	release chan struct{}
	// ignoreCancel makes the release wait deaf to the context, so a test can
	// have a handler succeed after its job was cancelled and check that the
	// result is dropped rather than applied.
	ignoreCancel bool
}

// newFakeHandler builds a handler that succeeds.
func newFakeHandler() *fakeHandler {
	return &fakeHandler{entered: make(chan struct{}, 1024)}
}

// Handle records the batch and answers from the script.
func (h *fakeHandler) Handle(ctx context.Context, batch []Job) error {
	h.mu.Lock()
	n := h.calls
	h.calls++
	h.batches = append(h.batches, append([]Job(nil), batch...))
	var fn func(context.Context, []Job) error
	if n < len(h.script) {
		fn = h.script[n]
	} else {
		fn = h.fallback
	}
	release, ignoreCancel := h.release, h.ignoreCancel
	h.mu.Unlock()

	select {
	case h.entered <- struct{}{}:
	default:
	}

	if release != nil {
		if ignoreCancel {
			<-release
		} else {
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if fn == nil {
		return nil
	}
	return fn(ctx, batch)
}

// callCount reports how many times the handler ran.
func (h *fakeHandler) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

// received returns the batches the handler saw, oldest first.
func (h *fakeHandler) received() [][]Job {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([][]Job, len(h.batches))
	copy(out, h.batches)
	return out
}

// waitEntered blocks until the handler has been entered n times in total.
func (h *fakeHandler) waitEntered(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-h.entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("the handler was not entered %d times (got %d)", n, h.callCount())
		}
	}
}

// waitFor blocks until cond holds, on the engine's own condition variable
// rather than on a poll loop: every state change broadcasts, so the test wakes
// exactly when something happened and never sleeps. cond is evaluated with the
// engine lock held, so it may read the engine's fields directly.
//
// The five-second deadline is a failure guard for a test that would otherwise
// hang, not a wait in the happy path.
func waitFor(parent context.Context, t *testing.T, e *Engine, what string, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() {
		e.mu.Lock()
		e.cond.Broadcast()
		e.mu.Unlock()
	})
	defer stop()

	e.mu.Lock()
	defer e.mu.Unlock()
	for !cond() {
		if ctx.Err() != nil {
			t.Fatalf("timed out waiting for %s", what)
		}
		e.cond.Wait()
	}
}

// ids renders a batch as its job ids, sorted, for a readable assertion.
func ids(batch []Job) []string {
	out := make([]string, 0, len(batch))
	for _, j := range batch {
		out = append(out, j.ID)
	}
	sort.Strings(out)
	return out
}

// payload builds a JSON payload for a test job.
func payload(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

// newTestEngine builds an engine on the fake clock with the debounce disabled
// and no journal, which is what most scheduler tests want.
func newTestEngine(parent context.Context, t *testing.T, opts Options) (*Engine, *fakeClock) {
	t.Helper()
	clock := newFakeClock()
	opts.Clock = clock
	if opts.Debounce == 0 {
		opts.Debounce = -1
	}
	e, err := New(opts)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()
		_ = e.Close(ctx)
	})
	return e, clock
}
