package syncengine

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestOnJobSeesEachJobsTransitionsInOrder checks that OnJob hears the state
// changes of one job in the order they happened. Every state change is
// announced off the engine lock, so before GIT-US-0146 the goroutine that
// queued a job — an Enqueue, or a RetryDeadLetter — could be overtaken by the
// worker that picked the job up at once, and an observer heard `running`
// before `queued`. That is what made the server's
// TestSyncJobRetryFromTheDeadLetter flake under load.
func TestOnJobSeesEachJobsTransitionsInOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var (
		mu   sync.Mutex
		seen = map[string][]State{}
	)
	onJob := func(job Job) {
		mu.Lock()
		seen[job.ID] = append(seen[job.ID], job.State)
		mu.Unlock()
	}

	h := newFakeHandler()
	// The first run of every job fails terminally, so each job is also
	// dead-lettered and retried: the retry is the second path that re-queues a
	// job a worker may pick up before the retry has announced it.
	var (
		failedMu sync.Mutex
		failed   = map[string]bool{}
	)
	h.fallback = func(_ context.Context, batch []Job) error {
		failedMu.Lock()
		defer failedMu.Unlock()
		if failed[batch[0].ID] {
			return nil
		}
		failed[batch[0].ID] = true
		return fmt.Errorf("%w: first run", ErrTerminal)
	}
	e, err := New(Options{
		Clock:     newFakeClock(),
		Rate:      -1,
		Workers:   8,
		Debounce:  -1,
		BatchSize: 1,
		OnJob:     onJob,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)

	const producers, each = 8, 25
	var wg sync.WaitGroup
	for p := range producers {
		wg.Go(func() {
			for i := range each {
				job := mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: fmt.Sprintf("p%d-%d", p, i)})
				waitFor(ctx, t, e, "job "+job.ID+" to fail", func() bool {
					r := e.jobs[job.ID]
					return r != nil && r.dead
				})
				if err := e.RetryDeadLetter(job.ID); err != nil {
					t.Errorf("retry %s: %v", job.ID, err)
				}
			}
		})
	}
	wg.Wait()
	if err := e.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := e.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	want := []State{StateQueued, StateRunning, StateFailed, StateQueued, StateRunning, StateDone}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != producers*each {
		t.Fatalf("OnJob heard about %d jobs, want %d", len(seen), producers*each)
	}
	for id, states := range seen {
		if fmt.Sprint(states) != fmt.Sprint(want) {
			t.Errorf("job %s: OnJob heard %v, want %v", id, states, want)
		}
	}
}
