package syncengine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// statusError is a stand-in for the typed errors a real API client returns. The
// engine must reach it through the interfaces it declares itself, never through
// an import of the client.
type statusError struct {
	status int
	header string
	msg    string
}

// Error renders the failure.
func (e *statusError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return fmt.Sprintf("remote said %d", e.status)
}

// StatusCode implements StatusCoder.
func (e *statusError) StatusCode() int { return e.status }

// RetryAfterHeader implements RetryAfterHeaderProvider.
func (e *statusError) RetryAfterHeader() string { return e.header }

// timeoutError is a transport failure.
type timeoutError struct{}

func (timeoutError) Error() string   { return "dial tcp: i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// TestClassify is the table of every class the engine distinguishes.
func TestClassify(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		err        error
		want       ErrorClass
		retryAfter time.Duration
	}{
		{"rate limited", &statusError{status: 429}, ClassRetryable, 0},
		{"server error", &statusError{status: 503}, ClassRetryable, 0},
		{"gateway timeout", &statusError{status: 504}, ClassRetryable, 0},
		{"request timeout", &statusError{status: 408}, ClassRetryable, 0},
		{"unauthorized", &statusError{status: 401}, ClassTerminal, 0},
		{"forbidden", &statusError{status: 403}, ClassTerminal, 0},
		{"not found", &statusError{status: 404}, ClassTerminal, 0},
		{"validation", &statusError{status: 400}, ClassTerminal, 0},
		{"terminal sentinel", Terminal(errors.New("bad payload")), ClassTerminal, 0},
		{"retry sentinel", fmt.Errorf("%w: flaky", ErrRetry), ClassRetryable, 0},
		{"transport failure", timeoutError{}, ClassRetryable, 0},
		{"plain error", errors.New("something went wrong"), ClassRetryable, 0},
		{"cancelled", context.Canceled, ClassCancelled, 0},
		{"deadline", context.DeadlineExceeded, ClassRetryable, 0},
		{"no handler", fmt.Errorf("%w: gone", ErrNoHandler), ClassTerminal, 0},
		{"retry-after seconds", &statusError{status: 429, header: "12"}, ClassRetryable, 12 * time.Second},
		{
			"retry-after http date",
			&statusError{status: 503, header: now.Add(90 * time.Second).Format(time.RFC1123)},
			ClassRetryable,
			90 * time.Second,
		},
		{"retry-after in the past", &statusError{status: 429, header: now.Add(-time.Minute).Format(time.RFC1123)}, ClassRetryable, 0},
		{"retry-after nonsense", &statusError{status: 429, header: "soon"}, ClassRetryable, 0},
		{"wrapped retry-after", RetryAfter(errors.New("throttled"), 3*time.Second), ClassRetryable, 3 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.err, now)
			if got.Class != tc.want {
				t.Fatalf("class is %s, want %s", got.Class, tc.want)
			}
			if got.RetryAfter != tc.retryAfter {
				t.Fatalf("retry-after is %s, want %s", got.RetryAfter, tc.retryAfter)
			}
		})
	}

	t.Run("a net.Error is still classified without a status", func(t *testing.T) {
		var netErr net.Error = timeoutError{}
		if got := Classify(netErr, now); got.Class != ClassRetryable {
			t.Fatalf("class is %s, want %s", got.Class, ClassRetryable)
		}
	})
}

// TestRetryPolicyDelay pins the ladder and the Retry-After override.
func TestRetryPolicyDelay(t *testing.T) {
	t.Parallel()

	p := RetryPolicy{Jitter: -1}.withDefaults()
	p.Jitter = -1 // jitter off, so the ladder is exact
	want := []time.Duration{
		500 * time.Millisecond,
		1500 * time.Millisecond,
		4500 * time.Millisecond,
		13500 * time.Millisecond,
		DefaultRetryMax,
		DefaultRetryMax,
	}
	for i, d := range want {
		if got := p.Delay(i+1, 0, nil); got != d {
			t.Fatalf("delay after attempt %d is %s, want %s", i+1, got, d)
		}
	}

	t.Run("retry-after always wins", func(t *testing.T) {
		if got := p.Delay(1, 7*time.Second, nil); got != 7*time.Second {
			t.Fatalf("delay is %s, want 7s", got)
		}
	})

	t.Run("jitter stays inside the band and never exceeds the cap", func(t *testing.T) {
		jittered := RetryPolicy{Jitter: 0.2}.withDefaults()
		for _, r := range []float64{0, 0.25, 0.5, 0.75, 0.999} {
			got := jittered.Delay(2, 0, func() float64 { return r })
			low := time.Duration(float64(1500*time.Millisecond) * 0.8)
			high := time.Duration(float64(1500*time.Millisecond) * 1.2)
			if got < low || got > high {
				t.Fatalf("jittered delay %s is outside [%s, %s]", got, low, high)
			}
		}
	})
}

// TestRetryLadderOnARunningEngine drives the real thing: a handler that fails
// twice and then succeeds, with every delay stepped on the fake clock.
func TestRetryLadderOnARunningEngine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	h := newFakeHandler()
	h.script = []func(context.Context, []Job) error{
		func(context.Context, []Job) error { return &statusError{status: 503} },
		func(context.Context, []Job) error { return &statusError{status: 503} },
	}
	e, clock := newTestEngine(ctx, t, Options{
		Rate:    -1,
		Workers: 1,
		Retry:   RetryPolicy{MaxAttempts: 5, Base: 500 * time.Millisecond, Max: time.Minute, Jitter: -1},
	})
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)

	job := mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
	h.waitEntered(t, 1)
	waitFor(ctx, t, e, "the first failure to be recorded", func() bool {
		return e.jobs[job.ID].job.LastError != nil
	})

	got, _ := e.Job(job.ID)
	if got.State != StateQueued || got.Attempts != 1 {
		t.Fatalf("after one failure: state %s, attempts %d; want queued and 1", got.State, got.Attempts)
	}
	if want := clock.Now().Add(500 * time.Millisecond); !got.NextAttempt.Equal(want) {
		t.Fatalf("NextAttempt is %s, want %s", got.NextAttempt, want)
	}
	if got.LastError.Class != ClassRetryable || got.LastError.Attempt != 1 {
		t.Fatalf("error record is %+v, want a retryable record for attempt 1", got.LastError)
	}

	// Second attempt, 500 ms later.
	clock.Advance(500 * time.Millisecond)
	h.waitEntered(t, 1)
	waitFor(ctx, t, e, "the second failure to be recorded", func() bool {
		return e.jobs[job.ID].job.Attempts == 2 && e.jobs[job.ID].job.State == StateQueued
	})
	got, _ = e.Job(job.ID)
	if want := clock.Now().Add(1500 * time.Millisecond); !got.NextAttempt.Equal(want) {
		t.Fatalf("the second delay is not 1.5s: NextAttempt %s, want %s", got.NextAttempt, want)
	}

	// Third attempt succeeds.
	clock.Advance(1500 * time.Millisecond)
	h.waitEntered(t, 1)
	waitFor(ctx, t, e, "the job to finish", func() bool { return e.jobs[job.ID].job.State == StateDone })

	got, _ = e.Job(job.ID)
	if got.Attempts != 3 {
		t.Fatalf("the job took %d attempts, want 3", got.Attempts)
	}
	if !got.NextAttempt.IsZero() {
		t.Fatalf("a finished job still carries NextAttempt %s", got.NextAttempt)
	}
	if c := e.Pending(); c.Done != 1 || c.Failed != 0 {
		t.Fatalf("counts are %+v, want one done job", c)
	}
}

// TestTerminalErrorFailsOnTheFirstAttempt covers the other half of the
// classifier: a rejected token must not be retried five times.
func TestTerminalErrorFailsOnTheFirstAttempt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	h := newFakeHandler()
	h.fallback = func(context.Context, []Job) error {
		return &statusError{status: 401, msg: "unauthorized"}
	}
	e, clock := newTestEngine(ctx, t, Options{Rate: -1, Workers: 1})
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)

	job := mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
	waitFor(ctx, t, e, "the job to fail", func() bool { return e.jobs[job.ID].job.State == StateFailed })

	// Nothing is scheduled: a terminal failure arms no retry at all, so moving
	// the clock on by an hour changes nothing.
	clock.Advance(time.Hour)
	waitFor(ctx, t, e, "no retry to be armed", func() bool { return e.jobs[job.ID].retry == nil })
	got, _ := e.Job(job.ID)
	if got.Attempts != 1 {
		t.Fatalf("a terminal error consumed %d attempts, want 1", got.Attempts)
	}
	if got.LastError == nil || got.LastError.Class != ClassTerminal {
		t.Fatalf("error record is %+v, want a terminal record", got.LastError)
	}
	if dl := e.DeadLetter(); len(dl) != 1 || dl[0].ID != job.ID {
		t.Fatalf("the dead-letter list holds %d entries, want the failed job", len(dl))
	}
	if n := h.callCount(); n != 1 {
		t.Fatalf("the handler ran %d times for a terminal error, want 1", n)
	}
}

// TestDeadLetter covers exhaustion, the bound, retrying from the list and
// clearing it.
func TestDeadLetter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a job that exhausts its attempts is kept with its record", func(t *testing.T) {
		h := newFakeHandler()
		h.fallback = func(context.Context, []Job) error { return &statusError{status: 500} }
		e, clock := newTestEngine(ctx, t, Options{
			Rate:    -1,
			Workers: 1,
			Retry:   RetryPolicy{MaxAttempts: 3, Base: time.Second, Max: time.Minute, Jitter: -1},
		})
		mustRegister(t, e, kindImport, h)
		mustStart(ctx, t, e)

		job := mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
		for i := 0; i < 3; i++ {
			waitFor(ctx, t, e, "the attempt to be recorded", func() bool {
				r := e.jobs[job.ID]
				return r.job.Attempts == i+1 && r.job.State != StateRunning
			})
			if i < 2 {
				clock.Advance(time.Minute)
			}
		}
		got, _ := e.Job(job.ID)
		if got.State != StateFailed || got.Attempts != 3 {
			t.Fatalf("state %s after %d attempts, want failed after 3", got.State, got.Attempts)
		}
		if got.LastError == nil || got.LastError.Attempt != 3 {
			t.Fatalf("error record is %+v, want the third attempt", got.LastError)
		}
		if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
			t.Fatal("the timestamps were lost")
		}

		t.Run("and can be retried from the list", func(t *testing.T) {
			h.mu.Lock()
			h.fallback = nil // succeed from now on
			h.mu.Unlock()
			if err := e.RetryDeadLetter(job.ID); err != nil {
				t.Fatalf("retry dead letter: %v", err)
			}
			waitFor(ctx, t, e, "the retried job to finish", func() bool {
				return e.jobs[job.ID].job.State == StateDone
			})
			if len(e.DeadLetter()) != 0 {
				t.Fatal("the job is still in the dead-letter list after a successful retry")
			}
		})

		t.Run("and an unknown id is refused", func(t *testing.T) {
			if err := e.RetryDeadLetter("nobody"); !errors.Is(err, ErrUnknownJob) {
				t.Fatalf("got %v, want ErrUnknownJob", err)
			}
			if err := e.RetryDeadLetter(job.ID); !errors.Is(err, ErrNotRetryable) {
				t.Fatalf("got %v, want ErrNotRetryable", err)
			}
		})
	})

	t.Run("the list is bounded and evictions are counted", func(t *testing.T) {
		h := newFakeHandler()
		h.fallback = func(context.Context, []Job) error { return Terminal(errors.New("nope")) }
		e, _ := newTestEngine(ctx, t, Options{Rate: -1, Workers: 1, DeadLetterMax: 3})
		mustRegister(t, e, kindImport, h)
		mustStart(ctx, t, e)

		for i := 0; i < 5; i++ {
			mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: fmt.Sprintf("k%d", i)})
		}
		waitFor(ctx, t, e, "every job to fail", func() bool { return e.evicted == 2 })
		if n := len(e.DeadLetter()); n != 3 {
			t.Fatalf("the dead-letter list holds %d entries, want 3", n)
		}
		if got := e.Snapshot().DeadLetterEvicted; got != 2 {
			t.Fatalf("the snapshot reports %d evictions, want 2", got)
		}
	})

	t.Run("clearing forgets the failures", func(t *testing.T) {
		h := newFakeHandler()
		h.fallback = func(context.Context, []Job) error { return Terminal(errors.New("nope")) }
		e, _ := newTestEngine(ctx, t, Options{Rate: -1, Workers: 1})
		mustRegister(t, e, kindImport, h)
		mustStart(ctx, t, e)

		for i := 0; i < 2; i++ {
			mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: fmt.Sprintf("k%d", i)})
		}
		waitFor(ctx, t, e, "both jobs to fail", func() bool { return len(e.dead) == 2 })
		if n := e.ClearDeadLetter(); n != 2 {
			t.Fatalf("clear reported %d, want 2", n)
		}
		if c := e.Pending(); c.Failed != 0 {
			t.Fatalf("counts after clearing are %+v, want no failures", c)
		}
	})
}

// TestErrorRecordsAreRedacted is the security assertion: a token that leaked
// into an error string must not reach the record the UI and the journal read.
func TestErrorRecordsAreRedacted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const token = "perm:root.workspace.9f8e7d6c5b4a"
	h := newFakeHandler()
	h.fallback = func(context.Context, []Job) error {
		return Terminal(fmt.Errorf(
			"POST https://bob:%s@youtrack.example.com/api/issues failed: token=%s (Bearer %s)",
			token, token, token))
	}
	e, _ := newTestEngine(ctx, t, Options{Rate: -1, Workers: 1})
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)

	job := mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
	waitFor(ctx, t, e, "the job to fail", func() bool { return e.jobs[job.ID].job.State == StateFailed })

	got, _ := e.Job(job.ID)
	if got.LastError == nil {
		t.Fatal("no error record was kept")
	}
	if strings.Contains(got.LastError.Message, token) {
		t.Fatalf("the token reached the error record: %s", got.LastError.Message)
	}
	if !strings.Contains(got.LastError.Message, redacted) {
		t.Fatalf("nothing was redacted: %s", got.LastError.Message)
	}
	if !strings.Contains(got.LastError.Message, "youtrack.example.com") {
		t.Fatalf("redaction ate the useful part of the message: %s", got.LastError.Message)
	}
}

// TestRedactString covers the redactor on its own, including the shapes a
// payload uses.
func TestRedactString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      string
		absent  string
		present string
	}{
		{"url credentials", "https://user:s3cr3t@host/x", "s3cr3t", "host"},
		{"bearer", `Authorization: Bearer abc.def`, "abc.def", redacted},
		{"query parameter", "GET /api?token=abc123&fields=id", "abc123", "fields=id"},
		{"json pair", `{"apiKey": "abc123", "id": "PRJ-1"}`, "abc123", "PRJ-1"},
		{"nothing to redact", "plain failure", "", "plain failure"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactString(tc.in)
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Fatalf("%q survived redaction in %q", tc.absent, got)
			}
			if !strings.Contains(got, tc.present) {
				t.Fatalf("%q is missing from %q", tc.present, got)
			}
		})
	}
}

// TestFailingHandlerUnderConcurrency proves the queue's core promise: whatever
// the handlers do, every job is accounted for exactly once.
func TestFailingHandlerUnderConcurrency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	h := HandlerFunc(func(_ context.Context, batch []Job) error {
		// Every job fails once and succeeds on its second attempt, so retries
		// and successes interleave across four workers.
		for _, j := range batch {
			if j.Attempts < 2 {
				return &statusError{status: 500}
			}
		}
		return nil
	})
	e, clock := newTestEngine(ctx, t, Options{
		Rate:      -1,
		Workers:   4,
		BatchSize: 1,
		Retry:     RetryPolicy{MaxAttempts: 2, Base: time.Millisecond, Max: time.Second, Jitter: -1},
	})
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)

	const total = 60
	for i := 0; i < total; i++ {
		mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: fmt.Sprintf("k%d", i%5)})
	}

	// A driver goroutine plays the part of time passing, so the backoff ladder
	// runs at the speed of the test rather than at the speed of a clock.
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
			clock.Advance(10 * time.Millisecond)
			runtime.Gosched()
		}
	}()

	waitFor(ctx, t, e, "every job to settle", func() bool {
		c := e.countsLocked()
		return c.Done+c.Failed == total
	})
	close(stop)
	driver.Wait()

	c := e.Pending()
	if c.Total() != total {
		t.Fatalf("the engine remembers %d jobs, want %d", c.Total(), total)
	}
	if c.Done != total {
		t.Fatalf("counts are %+v, want every job done on its second attempt", c)
	}
}
