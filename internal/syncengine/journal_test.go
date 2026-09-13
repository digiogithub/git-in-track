package syncengine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newJournalEngine builds an engine that persists to dir, on a fake clock.
func newJournalEngine(t *testing.T, dir string, clock *fakeClock, opts Options) *Engine {
	t.Helper()
	opts.CacheDir = dir
	opts.Clock = clock
	if opts.Rate == 0 {
		opts.Rate = -1
	}
	e, err := New(opts)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return e
}

// readJournal reads and decodes the journal file.
func readJournal(t *testing.T, dir string) (journalDoc, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, JournalName))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	var doc journalDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode journal: %v\n%s", err, raw)
	}
	return doc, raw
}

// TestJournalWrites covers the coalescing window and the tmp-plus-rename
// discipline.
func TestJournalWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dir := t.TempDir()
	clock := newFakeClock()
	e := newJournalEngine(t, dir, clock, Options{
		Debounce:     time.Hour,
		JournalFlush: 200 * time.Millisecond,
		Workers:      1,
	})
	mustRegister(t, e, kindImport, newFakeHandler())
	mustStart(ctx, t, e)

	// A burst of transitions: one write, not one write per transition.
	for i := 0; i < 10; i++ {
		mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: fmt.Sprintf("k%d", i)})
	}
	if n := e.journal.writeCount(); n != 0 {
		t.Fatalf("the journal was written %d times before its window elapsed", n)
	}
	clock.Advance(200 * time.Millisecond)
	if n := e.journal.writeCount(); n != 1 {
		t.Fatalf("a burst of 10 transitions produced %d writes, want 1", n)
	}

	doc, _ := readJournal(t, dir)
	if len(doc.Jobs) != 10 {
		t.Fatalf("the journal holds %d jobs, want 10", len(doc.Jobs))
	}
	if doc.Version != journalVersion {
		t.Fatalf("the journal version is %d, want %d", doc.Version, journalVersion)
	}
	if _, err := os.Stat(filepath.Join(dir, JournalName+".tmp")); err == nil {
		t.Fatal("the temporary file survived the rename")
	}

	t.Run("a temporary file left by a crash is never read", func(t *testing.T) {
		// This is what a crash between the write and the rename leaves behind:
		// a half-written sibling, and a previous journal that is still whole.
		tmp := filepath.Join(dir, JournalName+".tmp")
		if err := os.WriteFile(tmp, []byte(`{"version":1,"jobs":[{"id":"hal`), 0o600); err != nil {
			t.Fatalf("write the interrupted temporary file: %v", err)
		}
		loaded := e.journal.load()
		if len(loaded.Jobs) != 10 {
			t.Fatalf("the journal read back %d jobs, want the 10 of the last complete write", len(loaded.Jobs))
		}
		_ = os.Remove(tmp)
	})

	t.Run("close writes one last time", func(t *testing.T) {
		before := e.journal.writeCount()
		if err := e.Close(ctx); err != nil {
			t.Fatalf("close: %v", err)
		}
		if after := e.journal.writeCount(); after <= before {
			t.Fatalf("close did not write the journal: %d writes before, %d after", before, after)
		}
	})
}

// TestJournalNeverHoldsACredential is the security assertion for the file on
// disk: neither a payload nor an error text may carry a token into it.
func TestJournalNeverHoldsACredential(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const token = "perm:root.workspace.9f8e7d6c5b4a"
	dir := t.TempDir()
	clock := newFakeClock()
	e := newJournalEngine(t, dir, clock, Options{Debounce: -1, Workers: 1})

	h := newFakeHandler()
	h.fallback = func(context.Context, []Job) error {
		return Terminal(fmt.Errorf("POST https://bob:%s@youtrack.example.com/api failed", token))
	}
	mustRegister(t, e, kindImport, h)
	mustStart(ctx, t, e)

	mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A", Payload: payload(t, map[string]any{
		"issue": "PRJ-1",
		"token": token,
		"nested": map[string]any{
			"apiKey": token,
			"url":    "https://bob:" + token + "@youtrack.example.com",
		},
	})})
	waitFor(ctx, t, e, "the job to fail", func() bool { return e.countsLocked().Failed == 1 })
	if err := e.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, raw := readJournal(t, dir)
	if strings.Contains(string(raw), token) {
		t.Fatalf("the token reached the journal:\n%s", raw)
	}
	if !strings.Contains(string(raw), "PRJ-1") {
		t.Fatalf("redaction ate the bookkeeping:\n%s", raw)
	}
	if !strings.Contains(string(raw), redacted) {
		t.Fatalf("nothing was redacted:\n%s", raw)
	}
}

// TestReplay covers the restart: queued work comes back, a job that was running
// when the process died goes round again with its attempt count, failures stay
// failures and what is too old to keep is dropped on the way in.
func TestReplay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("queued jobs survive a restart and run", func(t *testing.T) {
		dir := t.TempDir()
		clock := newFakeClock()

		first := newJournalEngine(t, dir, clock, Options{Debounce: time.Hour, Workers: 1})
		mustRegister(t, first, kindImport, newFakeHandler())
		// Never started: nothing runs, and the journal keeps what was enqueued.
		a := mustEnqueue(ctx, t, first, Request{Kind: kindImport, Key: "A"})
		b := mustEnqueue(ctx, t, first, Request{Kind: kindImport, Key: "B"})
		if err := first.Close(ctx); err != nil {
			t.Fatalf("close: %v", err)
		}

		h := newFakeHandler()
		second := newJournalEngine(t, dir, clock, Options{Debounce: -1, Workers: 2})
		t.Cleanup(func() { _ = second.Close(ctx) })
		mustRegister(t, second, kindImport, h)
		mustStart(ctx, t, second)

		waitFor(ctx, t, second, "the replayed jobs to finish", func() bool {
			return second.countsLocked().Done == 2
		})
		for _, id := range []string{a.ID, b.ID} {
			if _, ok := second.Job(id); !ok {
				t.Fatalf("job %s was lost across the restart", id)
			}
		}
	})

	t.Run("a job that was running is re-queued with its attempts", func(t *testing.T) {
		dir := t.TempDir()
		clock := newFakeClock()
		writeJournal(t, dir, journalDoc{
			Version: journalVersion,
			Jobs: []Job{
				{
					ID: "j-000001", Kind: kindImport, Key: "A", State: StateRunning,
					Attempts: 2, CreatedAt: clock.Now(), UpdatedAt: clock.Now(),
				},
				{
					ID: "j-000002", Kind: kindImport, Key: "B", State: StateFailed,
					Attempts: 5, CreatedAt: clock.Now(), UpdatedAt: clock.Now(),
					LastError: &ErrorRecord{Attempt: 5, Class: ClassRetryable, Message: "boom"},
				},
				{
					ID: "j-000003", Kind: kindImport, Key: "C", State: StateDone,
					Attempts: 1, CreatedAt: clock.Now(), UpdatedAt: clock.Now().Add(-8 * 24 * time.Hour),
				},
				{
					ID: "j-000004", Kind: kindImport, Key: "D", State: StateDone,
					Attempts: 1, CreatedAt: clock.Now(), UpdatedAt: clock.Now().Add(-time.Hour),
				},
			},
			DeadLetter: []string{"j-000002"},
		})

		// A long debounce keeps the replayed work in its batch, so the test can
		// read the state the replay produced rather than race the workers.
		e := newJournalEngine(t, dir, clock, Options{Debounce: time.Hour, Workers: 1})
		t.Cleanup(func() { _ = e.Close(ctx) })
		mustRegister(t, e, kindImport, newFakeHandler())
		mustStart(ctx, t, e)

		running, ok := e.Job("j-000001")
		if !ok {
			t.Fatal("the job that was running was lost")
		}
		if running.State != StateQueued {
			t.Fatalf("state is %s, want %s", running.State, StateQueued)
		}
		if running.Attempts != 2 {
			t.Fatalf("attempts are %d, want the 2 the journal recorded", running.Attempts)
		}

		failed, ok := e.Job("j-000002")
		if !ok || failed.State != StateFailed || failed.LastError == nil {
			t.Fatalf("the failed job did not survive: %+v", failed)
		}
		if dl := e.DeadLetter(); len(dl) != 1 || dl[0].ID != "j-000002" {
			t.Fatalf("the dead-letter list holds %v, want j-000002", ids(dlJobs(dl)))
		}
		if _, ok := e.Job("j-000003"); ok {
			t.Fatal("a done job older than the retention window was replayed")
		}
		if _, ok := e.Job("j-000004"); !ok {
			t.Fatal("a recent done job was dropped")
		}

		// The id sequence continues past what was replayed.
		fresh := mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "E"})
		if fresh.ID != "j-000005" {
			t.Fatalf("the next id is %s, want j-000005", fresh.ID)
		}
	})

	t.Run("a corrupt journal is moved aside and the engine still starts", func(t *testing.T) {
		dir := t.TempDir()
		clock := newFakeClock()
		if err := os.WriteFile(filepath.Join(dir, JournalName), []byte(`{"version":1,"jobs":[{`), 0o600); err != nil {
			t.Fatalf("write the corrupt journal: %v", err)
		}

		e := newJournalEngine(t, dir, clock, Options{Debounce: time.Hour, Workers: 1})
		t.Cleanup(func() { _ = e.Close(ctx) })
		mustRegister(t, e, kindImport, newFakeHandler())
		if err := e.Start(ctx); err != nil {
			t.Fatalf("a corrupt journal must not stop the engine starting: %v", err)
		}
		if c := e.Pending(); c.Total() != 0 {
			t.Fatalf("the engine started with %+v, want an empty queue", c)
		}
		matches, err := filepath.Glob(filepath.Join(dir, JournalName+".corrupt-*"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("the damaged journal was not moved aside: %v %v", matches, err)
		}
	})

	t.Run("a journal from an unknown version is moved aside", func(t *testing.T) {
		dir := t.TempDir()
		clock := newFakeClock()
		writeJournal(t, dir, journalDoc{Version: journalVersion + 99, Jobs: []Job{
			{ID: "j-000001", Kind: kindImport, State: StateQueued},
		}})

		e := newJournalEngine(t, dir, clock, Options{Debounce: time.Hour, Workers: 1})
		t.Cleanup(func() { _ = e.Close(ctx) })
		mustRegister(t, e, kindImport, newFakeHandler())
		mustStart(ctx, t, e)
		if c := e.Pending(); c.Total() != 0 {
			t.Fatalf("the engine replayed a journal it does not understand: %+v", c)
		}
		matches, _ := filepath.Glob(filepath.Join(dir, JournalName+".version-*"))
		if len(matches) != 1 {
			t.Fatalf("the journal was not moved aside: %v", matches)
		}
	})
}

// TestPrune covers the retention window at start-up and on a long run.
func TestPrune(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a long run sweeps done jobs and keeps failures", func(t *testing.T) {
		h := newFakeHandler()
		h.script = []func(context.Context, []Job) error{
			nil, // the first batch succeeds
			func(context.Context, []Job) error { return Terminal(fmt.Errorf("nope")) },
		}
		e, clock := newTestEngine(ctx, t, Options{
			Rate:          -1,
			Workers:       1,
			Retention:     7 * 24 * time.Hour,
			PruneInterval: time.Hour,
		})
		mustRegister(t, e, kindImport, h)
		mustStart(ctx, t, e)

		mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "A"})
		waitFor(ctx, t, e, "the first job to finish", func() bool { return e.countsLocked().Done == 1 })
		mustEnqueue(ctx, t, e, Request{Kind: kindImport, Key: "B"})
		waitFor(ctx, t, e, "the second job to fail", func() bool { return e.countsLocked().Failed == 1 })

		clock.Advance(8 * 24 * time.Hour)
		waitFor(ctx, t, e, "the done job to be pruned", func() bool { return e.countsLocked().Done == 0 })

		if c := e.Pending(); c.Failed != 1 {
			t.Fatalf("pruning took the failure with it: %+v", c)
		}
		if len(e.DeadLetter()) != 1 {
			t.Fatal("the dead-letter entry was pruned")
		}
	})

	t.Run("the retention window is validated", func(t *testing.T) {
		if _, err := New(Options{Retention: -time.Hour}); err == nil {
			t.Fatal("a negative retention window was accepted")
		}
		if _, err := New(Options{PruneInterval: -time.Hour}); err == nil {
			t.Fatal("a negative prune interval was accepted")
		}
	})
}

// writeJournal writes a journal document verbatim, which is how a test stages
// the state a previous run left behind.
func writeJournal(t *testing.T, dir string, doc journalDoc) {
	t.Helper()
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("encode journal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, JournalName), raw, 0o600); err != nil {
		t.Fatalf("write journal: %v", err)
	}
}

// dlJobs is a readability helper for a dead-letter assertion.
func dlJobs(jobs []Job) []Job { return jobs }
