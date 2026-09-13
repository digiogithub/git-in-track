package syncengine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

// kindImport is the kind every test registers unless it needs two.
const kindImport Kind = "import"

// TestRegister covers the handler registry.
func TestRegister(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	e, _ := newTestEngine(ctx, t, Options{Rate: -1})

	t.Run("registers a kind once", func(t *testing.T) {
		if err := e.Register(kindImport, newFakeHandler()); err != nil {
			t.Fatalf("register: %v", err)
		}
	})
	t.Run("refuses a duplicate kind", func(t *testing.T) {
		err := e.Register(kindImport, newFakeHandler())
		if !errors.Is(err, ErrDuplicateKind) {
			t.Fatalf("second register: got %v, want ErrDuplicateKind", err)
		}
	})
	t.Run("refuses an empty kind and a nil handler", func(t *testing.T) {
		if err := e.Register("", newFakeHandler()); !errors.Is(err, ErrInvalidOption) {
			t.Fatalf("empty kind: got %v, want ErrInvalidOption", err)
		}
		if err := e.Register("other", nil); !errors.Is(err, ErrInvalidOption) {
			t.Fatalf("nil handler: got %v, want ErrInvalidOption", err)
		}
	})
	t.Run("refuses a job for an unregistered kind", func(t *testing.T) {
		_, err := e.Enqueue(ctx, Request{Kind: "nobody"})
		if !errors.Is(err, ErrNoHandler) {
			t.Fatalf("enqueue: got %v, want ErrNoHandler", err)
		}
	})
}

// TestTransitionGuard pins the state machine: the documented edges and nothing
// else.
func TestTransitionGuard(t *testing.T) {
	t.Parallel()

	states := []State{StateQueued, StateRunning, StateDone, StateFailed, StateCancelled}
	allowed := map[State]map[State]bool{
		StateQueued:    {StateRunning: true, StateCancelled: true},
		StateRunning:   {StateDone: true, StateFailed: true, StateCancelled: true, StateQueued: true},
		StateDone:      {},
		StateFailed:    {StateQueued: true},
		StateCancelled: {},
	}
	now := time.Now()
	for _, from := range states {
		for _, to := range states {
			if from == to {
				continue
			}
			name := fmt.Sprintf("%s to %s", from, to)
			t.Run(name, func(t *testing.T) {
				r := &jobRecord{job: Job{State: from}}
				err := transition(r, to, now)
				want := allowed[from][to]
				if want && err != nil {
					t.Fatalf("%s: got %v, want the transition to be allowed", name, err)
				}
				if !want {
					if !errors.Is(err, ErrBadTransition) {
						t.Fatalf("%s: got %v, want ErrBadTransition", name, err)
					}
					if r.job.State != from {
						t.Fatalf("%s: state moved to %s on a refused transition", name, r.job.State)
					}
				}
			})
		}
	}
}

// TestCoalescing proves that jobs of one kind and key are handed to the handler
// together, that different keys never merge, and that a full batch does not
// wait for the debounce.
func TestCoalescing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("same kind and key batch, different keys do not", func(t *testing.T) {
		h := newFakeHandler()
		e, clock := newTestEngine(ctx, t, Options{Debounce: 100 * time.Millisecond, Rate: -1, Workers: 2})
		mustRegister(t, e, kindImport, h)
		mustStart(ctx, t, e)

		mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
		mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
		mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "B"})

		if n := h.callCount(); n != 0 {
			t.Fatalf("the handler ran %d times before the debounce elapsed", n)
		}
		clock.Advance(100 * time.Millisecond)
		h.waitEntered(t, 2)

		sizes := map[int]int{}
		for _, b := range h.received() {
			sizes[len(b)]++
			key := b[0].Key
			for _, j := range b {
				if j.Key != key {
					t.Fatalf("a batch mixed keys %q and %q", key, j.Key)
				}
			}
		}
		if sizes[2] != 1 || sizes[1] != 1 {
			t.Fatalf("got batch sizes %v, want one batch of 2 and one of 1", sizes)
		}
	})

	t.Run("a full batch fires without waiting", func(t *testing.T) {
		h := newFakeHandler()
		e, _ := newTestEngine(ctx, t, Options{Debounce: time.Hour, BatchSize: 3, Rate: -1, Workers: 1})
		mustRegister(t, e, kindImport, h)
		mustStart(ctx, t, e)

		for i := 0; i < 3; i++ {
			mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
		}
		h.waitEntered(t, 1)
		if got := len(h.received()[0]); got != 3 {
			t.Fatalf("the batch that fired at the cap held %d jobs, want 3", got)
		}
	})

	t.Run("the debounce ceiling stops a batch being postponed forever", func(t *testing.T) {
		h := newFakeHandler()
		e, clock := newTestEngine(ctx, t, Options{
			Debounce:    time.Second,
			MaxDebounce: 2500 * time.Millisecond,
			BatchSize:   1000,
			Rate:        -1,
			Workers:     1,
		})
		mustRegister(t, e, kindImport, h)
		mustStart(ctx, t, e)

		// A steady stream re-arms the debounce every time, but the ceiling
		// still makes the batch fire.
		mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
		for _, step := range []time.Duration{900, 900, 700} {
			clock.Advance(step * time.Millisecond)
			if h.callCount() == 0 {
				mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
			}
		}
		h.waitEntered(t, 1)
	})
}

// TestEnqueueDetachesCallerContext proves the point of the engine: an HTTP
// response closing must not cancel the work it caused.
func TestEnqueueDetachesCallerContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	type ctxKey string
	const key ctxKey = "request-id"

	seen := make(chan error, 1)
	value := make(chan any, 1)
	h := HandlerFunc(func(ctx context.Context, batch []Job) error {
		seen <- ctx.Err()
		value <- ctx.Value(key)
		return nil
	})

	e, clock := newTestEngine(ctx, t, Options{Debounce: 50 * time.Millisecond, Rate: -1, Workers: 1})
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)

	caller, cancel := context.WithCancel(context.WithValue(ctx, key, "req-7"))
	mustEnqueue(caller, t, e, Request{Kind: kindImport, Key: "A"})
	// The caller goes away, exactly as a closed HTTP response does.
	cancel()

	clock.Advance(50 * time.Millisecond)
	if err := <-seen; err != nil {
		t.Fatalf("the handler context was cancelled by the caller: %v", err)
	}
	if v := <-value; v != "req-7" {
		t.Fatalf("the caller's values did not survive the detachment: got %v", v)
	}
}

// TestSharedLimiter proves the rate is a property of the engine, not of the
// number of workers, and does it on the fake clock.
func TestSharedLimiter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	h := newFakeHandler()
	e, clock := newTestEngine(ctx, t, Options{Rate: 2, Burst: 2, Workers: 4})
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)

	const jobs = 6
	for i := 0; i < jobs; i++ {
		mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: fmt.Sprintf("k%d", i)})
	}

	// The burst lets exactly two through, and no more until time passes.
	h.waitEntered(t, 2)
	// Every worker that could not get a token is parked on a clock timer.
	clock.BlockUntil(1)
	if n := h.callCount(); n > 2 {
		t.Fatalf("the limiter let %d jobs through on a burst of 2", n)
	}

	if got := e.limiter.count(); got > 2 {
		t.Fatalf("the limiter handed out %d tokens on a burst of 2", got)
	}

	// One token every 500 ms, whatever the pool size is: the count may lag a
	// worker's wake-up, but it can never run ahead of the tokens issued.
	for i := 1; i <= 8; i++ {
		clock.Advance(500 * time.Millisecond)
		if n := e.limiter.count(); n > 2+i {
			t.Fatalf("after %d tokens' worth of time the limiter handed out %d", 2+i, n)
		}
		if n := h.callCount(); n > 2+i {
			t.Fatalf("after %d tokens' worth of time %d jobs had run", 2+i, n)
		}
	}

	// And everything does eventually run: the limiter delays work, it never
	// drops it.
	stop := make(chan struct{})
	var driver sync.WaitGroup
	driver.Add(1)
	go func() {
		defer driver.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			clock.Advance(500 * time.Millisecond)
			runtime.Gosched()
		}
	}()
	waitFor(ctx, t, e, "every job to run", func() bool { return e.countsLocked().Done == jobs })
	close(stop)
	driver.Wait()
}

// TestSetWorkersTakesEffectOnARunningEngine covers the runtime knobs.
func TestSetWorkersTakesEffectOnARunningEngine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	h := newFakeHandler()
	h.release = make(chan struct{})
	e, _ := newTestEngine(ctx, t, Options{Rate: -1, Workers: 1})
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)

	for i := 0; i < 3; i++ {
		mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: fmt.Sprintf("k%d", i)})
	}
	// One worker, one job in flight.
	h.waitEntered(t, 1)
	waitFor(ctx, t, e, "one batch running", func() bool { return e.running == 1 })

	if err := e.SetWorkers(3); err != nil {
		t.Fatalf("set workers: %v", err)
	}
	// The new pool picks up the two batches still queued.
	h.waitEntered(t, 2)
	waitFor(ctx, t, e, "three batches running", func() bool { return e.running == 3 })

	if got := e.Snapshot().Workers; got != 3 {
		t.Fatalf("the snapshot reports %d workers, want 3", got)
	}
	close(h.release)
	waitFor(ctx, t, e, "every job done", func() bool { return e.countsLocked().Done == 3 })
}

// TestCancel covers a queued job, a running job whose result must be dropped,
// and an id nobody knows.
func TestCancel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a queued job leaves the queue", func(t *testing.T) {
		h := newFakeHandler()
		e, clock := newTestEngine(ctx, t, Options{Debounce: time.Hour, Rate: -1, Workers: 1})
		mustRegister(t, e, kindImport, h)
		mustStart(ctx, t, e)

		job := mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
		if !e.Cancel(job.ID) {
			t.Fatal("canceling a queued job reported nothing to cancel")
		}
		clock.Advance(2 * time.Hour)
		if n := h.callCount(); n != 0 {
			t.Fatalf("a cancelled job still reached the handler %d times", n)
		}
		got, _ := e.Job(job.ID)
		if got.State != StateCancelled {
			t.Fatalf("state is %s, want %s", got.State, StateCancelled)
		}
	})

	t.Run("a running job is cancelled and its result dropped", func(t *testing.T) {
		h := newFakeHandler()
		h.release = make(chan struct{})
		// The handler succeeds after the cancellation: the engine must keep the
		// newer decision and not resurrect the job as done.
		h.ignoreCancel = true
		e, _ := newTestEngine(ctx, t, Options{Rate: -1, Workers: 1})
		mustRegister(t, e, kindImport, h)
		mustStart(ctx, t, e)

		job := mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
		h.waitEntered(t, 1)
		if !e.Cancel(job.ID) {
			t.Fatal("canceling a running job reported nothing to cancel")
		}
		close(h.release)
		waitFor(ctx, t, e, "the batch to finish", func() bool { return e.running == 0 })

		got, _ := e.Job(job.ID)
		if got.State != StateCancelled {
			t.Fatalf("the superseded result was applied: state is %s", got.State)
		}
	})

	t.Run("an unknown id is a no-op", func(t *testing.T) {
		e, _ := newTestEngine(ctx, t, Options{Rate: -1})
		if e.Cancel("nobody") {
			t.Fatal("canceling an unknown id reported success")
		}
	})
}

// TestFlush covers draining and the deadline.
func TestFlush(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("drains what is pending", func(t *testing.T) {
		h := newFakeHandler()
		e, _ := newTestEngine(ctx, t, Options{Debounce: time.Hour, Rate: -1, Workers: 2})
		mustRegister(t, e, kindImport, h)
		mustStart(ctx, t, e)

		for i := 0; i < 5; i++ {
			mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: fmt.Sprintf("k%d", i)})
		}
		if err := e.Flush(ctx); err != nil {
			t.Fatalf("flush: %v", err)
		}
		if c := e.Pending(); c.Queued != 0 || c.Running != 0 || c.Done != 5 {
			t.Fatalf("after the flush the counts are %+v, want 5 done", c)
		}
	})

	t.Run("returns when its context expires instead of blocking forever", func(t *testing.T) {
		h := newFakeHandler()
		h.release = make(chan struct{})
		h.ignoreCancel = true
		e, _ := newTestEngine(ctx, t, Options{Rate: -1, Workers: 1})
		mustRegister(t, e, kindImport, h)
		mustStart(ctx, t, e)

		mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
		h.waitEntered(t, 1)

		deadline, cancel := context.WithCancel(ctx)
		cancel()
		if err := e.Flush(deadline); !errors.Is(err, context.Canceled) {
			t.Fatalf("flush: got %v, want context.Canceled", err)
		}
		close(h.release)
	})

	t.Run("refuses to wait on an engine that was never started", func(t *testing.T) {
		e, _ := newTestEngine(ctx, t, Options{Rate: -1})
		if err := e.Flush(ctx); !errors.Is(err, ErrNotStarted) {
			t.Fatalf("flush: got %v, want ErrNotStarted", err)
		}
	})
}

// TestCloseIsIdempotent covers the shutdown contract.
func TestCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	h := newFakeHandler()
	clock := newFakeClock()
	e, err := New(Options{Clock: clock, Rate: -1, Workers: 2, Debounce: -1})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)
	for i := 0; i < 4; i++ {
		mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: fmt.Sprintf("k%d", i)})
	}

	if err := e.Close(ctx); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := e.Close(ctx); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := e.Enqueue(ctx, Request{Kind: kindImport}); !errors.Is(err, ErrClosed) {
		t.Fatalf("enqueue after close: got %v, want ErrClosed", err)
	}
	if n := clock.pending(); n != 0 {
		t.Fatalf("%d timers were still armed after close", n)
	}
	if c := e.Pending(); c.Done != 4 {
		t.Fatalf("close did not drain: counts are %+v", c)
	}
}

// TestConcurrentEnqueueCancelClose is the stress test: many goroutines
// enqueueing, canceling and reading while the pool works, then a close that
// must leave nothing behind. It is the test that earns -race.
func TestConcurrentEnqueueCancelClose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	h := newFakeHandler()
	clock := newFakeClock()
	e, err := New(Options{
		Clock:     clock,
		Rate:      -1,
		Workers:   4,
		Debounce:  -1,
		BatchSize: 5,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)

	const producers, each = 8, 40
	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				job, err := e.Enqueue(ctx, Request{Kind: kindImport, Key: fmt.Sprintf("k%d", i%7)})
				if err != nil {
					if errors.Is(err, ErrClosed) {
						return
					}
					t.Errorf("enqueue: %v", err)
					return
				}
				if i%5 == 0 {
					e.Cancel(job.ID)
				}
				if i%11 == 0 {
					_ = e.Snapshot()
					_ = e.Pending()
				}
			}
		}(p)
	}
	wg.Wait()

	if err := e.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	counts := e.Pending()
	if total := counts.Total(); total != producers*each {
		t.Fatalf("the engine remembers %d jobs, want %d — a job was lost or duplicated",
			total, producers*each)
	}
	if counts.Queued != 0 || counts.Running != 0 {
		t.Fatalf("jobs were left in flight after close: %+v", counts)
	}
	if n := clock.pending(); n != 0 {
		t.Fatalf("%d timers were still armed after close", n)
	}
}

// TestSnapshotIsACopy makes sure a caller cannot reach into the engine's own
// records through the snapshot the REST layer will serve.
func TestSnapshotIsACopy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	h := newFakeHandler()
	e, _ := newTestEngine(ctx, t, Options{Debounce: time.Hour, Rate: -1, Workers: 1})
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)

	job := mustEnqueue(ctx, t, e, Request{
		Kind:    kindImport,
		Key:     "A",
		Payload: payload(t, map[string]string{"issue": "PRJ-1"}),
	})
	snap := e.Snapshot()
	if len(snap.Jobs) != 1 {
		t.Fatalf("the snapshot holds %d jobs, want 1", len(snap.Jobs))
	}
	snap.Jobs[0].State = StateDone
	snap.Jobs[0].Payload[0] = 'X'

	got, _ := e.Job(job.ID)
	if got.State != StateQueued {
		t.Fatalf("the snapshot aliased the engine's record: state is %s", got.State)
	}
	if string(got.Payload) != `{"issue":"PRJ-1"}` {
		t.Fatalf("the snapshot aliased the payload: %s", got.Payload)
	}
	if !reflect.DeepEqual(ids(snap.Jobs), []string{job.ID}) {
		t.Fatalf("the snapshot lists %v, want %v", ids(snap.Jobs), []string{job.ID})
	}
}

// TestEnqueueWithAnIDIsIdempotent covers the caller-supplied id.
func TestEnqueueWithAnIDIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	h := newFakeHandler()
	e, _ := newTestEngine(ctx, t, Options{Debounce: time.Hour, Rate: -1, Workers: 1})
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)

	first := mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A", ID: "import-PRJ"})
	second := mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A", ID: "import-PRJ"})
	if first.ID != second.ID {
		t.Fatalf("ids differ: %s and %s", first.ID, second.ID)
	}
	if c := e.Pending(); c.Queued != 1 {
		t.Fatalf("the same id was enqueued twice: %+v", c)
	}
}

// mustRegister registers a handler or fails the test.
func mustRegister(t *testing.T, e *Engine, kind Kind, h Handler) {
	t.Helper()
	if err := e.Register(kind, h); err != nil {
		t.Fatalf("register %s: %v", kind, err)
	}
}

// mustStart starts an engine or fails the test.
func mustStart(ctx context.Context, t *testing.T, e *Engine) {
	t.Helper()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
}

// mustEnqueue enqueues a job or fails the test.
func mustEnqueue(ctx context.Context, t *testing.T, e *Engine, req Request) Job {
	t.Helper()
	job, err := e.Enqueue(ctx, req)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return job
}
