package server

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/syncengine"
)

// fakeClock is a clock a test advances by hand, so that the progress throttle
// is asserted without a wall-clock sleep.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// observerHarness is a hub, a listening client and the observer under test.
type observerHarness struct {
	hub      *Hub
	client   *hubClient
	observer *syncObserver
	clock    *fakeClock
}

func newObserverHarness(t *testing.T) *observerHarness {
	t.Helper()

	clock := &fakeClock{now: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)}
	hub := newHub("test", clock.Now)
	client := newHubClient()
	hub.register(client)
	return &observerHarness{hub: hub, client: client, observer: newSyncObserver(hub, clock.Now), clock: clock}
}

// topics drains the client queue and returns the topics it received.
func (h *observerHarness) topics() []string {
	var out []string
	for {
		select {
		case ev := <-h.client.events:
			out = append(out, ev.Type)
		default:
			return out
		}
	}
}

// drain returns every event the client received.
func (h *observerHarness) drain() []Event {
	var out []Event
	for {
		select {
		case ev := <-h.client.events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

func TestSyncObserverPublishesOneEventPerTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		job  syncengine.Job
		want string
	}{
		{
			name: "queued",
			job:  syncengine.Job{ID: "job_1", Kind: testJobKind, State: syncengine.StateQueued},
			want: eventSyncJobQueued,
		},
		{
			name: "started",
			job:  syncengine.Job{ID: "job_1", Kind: testJobKind, State: syncengine.StateRunning, Attempts: 1},
			want: eventSyncJobStarted,
		},
		{
			name: "done",
			job:  syncengine.Job{ID: "job_1", Kind: testJobKind, State: syncengine.StateDone, Attempts: 1},
			want: eventSyncJobDone,
		},
		{
			name: "cancelled is the end of a job, on the same topic as done",
			job:  syncengine.Job{ID: "job_1", Kind: testJobKind, State: syncengine.StateCancelled},
			want: eventSyncJobDone,
		},
		{
			name: "failed",
			job: syncengine.Job{
				ID: "job_1", Kind: testJobKind, State: syncengine.StateFailed, Attempts: 5,
				LastError: &syncengine.ErrorRecord{Attempt: 5, Class: syncengine.ClassTerminal, Message: "the remote said no"},
			},
			want: eventSyncJobFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newObserverHarness(t)
			h.observer.onJob(tt.job)
			got := h.drain()
			if len(got) != 1 || got[0].Type != tt.want {
				t.Fatalf("topics = %+v, want exactly %q", got, tt.want)
			}
			data, ok := got[0].Data.(syncJobEventData)
			if !ok {
				t.Fatalf("payload = %T, want syncJobEventData", got[0].Data)
			}
			if data.ID != tt.job.ID || data.Kind != string(tt.job.Kind) || data.State != string(tt.job.State) {
				t.Fatalf("payload = %+v", data)
			}
			if tt.job.LastError != nil && data.Error != tt.job.LastError.Message {
				t.Fatalf("error = %q, want the redacted message", data.Error)
			}
		})
	}
}

func TestSyncObserverCarriesNoPayloadAndNoCredential(t *testing.T) {
	t.Parallel()

	h := newObserverHarness(t)
	h.observer.onJob(syncengine.Job{
		ID: "job_1", Kind: testJobKind, Key: "INBX",
		Payload: []byte(`{"token":"perm:super-secret"}`),
		State:   syncengine.StateQueued,
	})
	events := h.drain()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	// The payload is the one field of a job this layer cannot vouch for, so it
	// is simply not in the event shape at all.
	data, ok := events[0].Data.(syncJobEventData)
	if !ok {
		t.Fatalf("payload = %T", events[0].Data)
	}
	if strings.Contains(data.Error+data.Key+data.Kind+data.ID, "perm:") {
		t.Fatalf("a credential reached the event payload: %+v", data)
	}
}

func TestSyncObserverCoalescesProgress(t *testing.T) {
	t.Parallel()

	h := newObserverHarness(t)
	const jobs = 1000

	// A thousand jobs of one batch, queued and then finished one by one. The
	// client queue is drained as we go, because the point of the assertion is
	// how many progress events the observer decided to publish, not the hub's
	// own back-pressure policy, which is asserted elsewhere.
	var progress, terminal int
	count := func() {
		for _, topic := range h.topics() {
			switch topic {
			case eventSyncJobProgress:
				progress++
			case eventSyncJobDone, eventSyncJobFailed:
				terminal++
			}
		}
	}
	for i := 0; i < jobs; i++ {
		id := "job_" + strconv.Itoa(i)
		h.observer.onJob(syncengine.Job{ID: id, Kind: testJobKind, Key: "batch", State: syncengine.StateQueued})
		count()
	}
	for i := 0; i < jobs; i++ {
		id := "job_" + strconv.Itoa(i)
		h.observer.onJob(syncengine.Job{ID: id, Kind: testJobKind, Key: "batch", State: syncengine.StateDone, Attempts: 1})
		count()
		// Time only moves every hundred jobs, which is what lets the throttle
		// be asserted rather than raced.
		if i%100 == 0 {
			h.clock.advance(syncProgressInterval)
		}
	}
	count()
	if terminal != jobs {
		t.Fatalf("terminal events = %d, want one per job (%d)", terminal, jobs)
	}
	if progress == 0 || progress > 20 {
		t.Fatalf("progress events = %d, want a bounded handful for %d jobs", progress, jobs)
	}
}

func TestSyncObserverNeverThrottlesATerminalEvent(t *testing.T) {
	t.Parallel()

	h := newObserverHarness(t)
	// Two jobs in one group, finishing back to back with no time passing: the
	// progress event between them may be suppressed, the two terminal events
	// may not.
	for _, id := range []string{"job_1", "job_2"} {
		h.observer.onJob(syncengine.Job{ID: id, Kind: testJobKind, Key: "batch", State: syncengine.StateQueued})
	}
	h.topics()
	h.observer.onJob(syncengine.Job{ID: "job_1", Kind: testJobKind, Key: "batch", State: syncengine.StateDone, Attempts: 1})
	h.observer.onJob(syncengine.Job{
		ID: "job_2", Kind: testJobKind, Key: "batch", State: syncengine.StateFailed, Attempts: 5,
		LastError: &syncengine.ErrorRecord{Attempt: 5, Message: "gave up"},
	})

	var done, failed int
	for _, topic := range h.topics() {
		switch topic {
		case eventSyncJobDone:
			done++
		case eventSyncJobFailed:
			failed++
		}
	}
	if done != 1 || failed != 1 {
		t.Fatalf("done = %d, failed = %d, want one of each", done, failed)
	}
}

func TestSyncObserverCountsTheGroup(t *testing.T) {
	t.Parallel()

	h := newObserverHarness(t)
	for _, id := range []string{"job_1", "job_2", "job_3"} {
		h.observer.onJob(syncengine.Job{ID: id, Kind: testJobKind, Key: "batch", State: syncengine.StateQueued})
	}
	h.topics()
	h.clock.advance(syncProgressInterval)
	h.observer.onJob(syncengine.Job{ID: "job_1", Kind: testJobKind, Key: "batch", State: syncengine.StateDone, Attempts: 1})

	var progress *syncJobEventData
	for _, ev := range h.drain() {
		if ev.Type != eventSyncJobProgress {
			continue
		}
		data, ok := ev.Data.(syncJobEventData)
		if !ok {
			t.Fatalf("payload = %T", ev.Data)
		}
		progress = &data
	}
	if progress == nil {
		t.Fatal("no progress event while the batch still had work left")
	}
	if progress.Processed != 1 || progress.Total != 3 {
		t.Fatalf("progress = %d/%d, want 1/3", progress.Processed, progress.Total)
	}
}

func TestSyncJobEventsReachTheEventStreamAndTheReplayRing(t *testing.T) {
	t.Parallel()

	s := newEngineServer(t, nil)
	client := newHubClient()
	client.subscribe(syncJobTopics())
	s.hub.register(client)

	ran := make(chan struct{}, 1)
	startEngine(t, s, func([]syncengine.Job) error {
		ran <- struct{}{}
		return nil
	})
	if _, err := s.sync.engine.Enqueue(t.Context(), syncengine.Request{Kind: testJobKind}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := s.sync.engine.Flush(t.Context()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	<-ran

	// Every state change is announced off the engine lock, so the goroutine
	// that enqueued the job may reach its `queued` emit after the batch that
	// Flush started has already announced `started` and `done`. The contract
	// is that all three arrive, not that they arrive in state order; drain
	// until both ends of the job's life are on the stream.
	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for !seen[eventSyncJobDone] || !seen[eventSyncJobQueued] {
		select {
		case ev := <-client.events:
			seen[ev.Type] = true
		case <-deadline:
			t.Fatalf("topics seen = %v, want the job announced as queued and done", seen)
		}
	}
	// A client that reconnects with `since` is served the same events from the
	// hub's replay ring, which is the contract every other topic inherits.
	replayed, ok := s.hub.since(0)
	if !ok || len(replayed) == 0 {
		t.Fatalf("since(0) = %d events, ok=%v", len(replayed), ok)
	}
	// The ring keeps publish order, which is the order the emits reached the
	// hub, so it is asserted as a set: the same topics the live stream carried,
	// nothing dropped and nothing invented.
	replayedTopics := map[string]bool{}
	for _, ev := range replayed {
		replayedTopics[ev.Type] = true
	}
	for _, topic := range []string{eventSyncJobQueued, eventSyncJobDone} {
		if !replayedTopics[topic] {
			t.Fatalf("the replay lacks %q: %v", topic, replayedTopics)
		}
	}
}
