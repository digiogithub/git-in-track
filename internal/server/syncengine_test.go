package server

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/syncengine"
)

// testJobKind is the kind the tests of this package register a handler for. No
// real kind is used: this package owns the engine's lifecycle, never the work.
const testJobKind syncengine.Kind = "test.job"

// newEngineServer builds a server whose engine dispatches without waiting for
// the coalescing window, which is what a test wants, and lets one caller tune
// the rest of the options.
func newEngineServer(t *testing.T, tune func(*Options)) *Server {
	t.Helper()

	opts := Options{
		Token:      "test-token",
		Version:    "0.0.1-test",
		Workspace:  "test",
		SyncEngine: SyncEngine{Debounce: -1, DrainTimeout: 2 * time.Second},
	}
	if tune != nil {
		tune(&opts)
	}
	s, err := New(opts)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s
}

// startEngine registers a handler, brings the engine up and stops it on
// cleanup. It returns a channel carrying every job the handler was given.
func startEngine(t *testing.T, s *Server, handle func(batch []syncengine.Job) error) {
	t.Helper()

	if err := s.RegisterSyncHandler(testJobKind, syncengine.HandlerFunc(
		func(_ context.Context, batch []syncengine.Job) error { return handle(batch) },
	)); err != nil {
		t.Fatalf("RegisterSyncHandler(): %v", err)
	}
	if err := s.sync.start(t.Context()); err != nil {
		t.Fatalf("start the engine: %v", err)
	}
	t.Cleanup(func() { _ = s.sync.close(context.WithoutCancel(t.Context())) })
}

func TestSyncEngineSettingsValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings SyncEngine
		wantErr  string
	}{
		{name: "defaults", settings: SyncEngine{}.withDefaults()},
		{name: "no workers", settings: SyncEngine{}.withDefaults().with(func(s *SyncEngine) { s.Workers = 0 }), wantErr: "workers"},
		{name: "too many workers", settings: SyncEngine{}.withDefaults().with(func(s *SyncEngine) { s.Workers = 1000 }), wantErr: "workers"},
		{name: "no batch", settings: SyncEngine{}.withDefaults().with(func(s *SyncEngine) { s.BatchSize = 0 }), wantErr: "batchSize"},
		{name: "rate zero", settings: SyncEngine{}.withDefaults().with(func(s *SyncEngine) { s.Rate = 0 }), wantErr: "rate"},
		{name: "rate unlimited", settings: SyncEngine{}.withDefaults().with(func(s *SyncEngine) { s.Rate = -1 })},
		{name: "attempts", settings: SyncEngine{}.withDefaults().with(func(s *SyncEngine) { s.MaxAttempts = 99 }), wantErr: "maxAttempts"},
		{name: "retention", settings: SyncEngine{}.withDefaults().with(func(s *SyncEngine) { s.Retention = -time.Hour }), wantErr: "retention"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.settings.Validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Validate() = %v, want no error", err)
			case tt.wantErr == "":
				return
			case err == nil:
				t.Fatalf("Validate() accepted %+v", tt.settings)
			}
			var field *settingsError
			if !errors.As(err, &field) || field.field != tt.wantErr {
				t.Fatalf("Validate() = %v, want a failure naming %q", err, tt.wantErr)
			}
		})
	}
}

// with returns a copy of the settings with one field changed, so the table
// above reads as "the defaults, except".
func (o SyncEngine) with(change func(*SyncEngine)) SyncEngine {
	change(&o)
	return o
}

func TestSyncEngineDefaults(t *testing.T) {
	t.Parallel()

	s := newEngineServer(t, func(o *Options) { o.SyncEngine = SyncEngine{} })
	got := s.sync.current()
	if got.Workers != 2 || got.BatchSize != 20 || got.Rate != 5 || got.MaxAttempts != 5 {
		t.Fatalf("defaults = %+v, want 2 workers, batch 20, 5 req/s, 5 attempts", got)
	}
	if got.Retention != 7*24*time.Hour {
		t.Errorf("retention = %v, want 7 days", got.Retention)
	}
}

func TestSyncEngineRejectsImpossibleOptions(t *testing.T) {
	t.Parallel()

	if _, err := New(Options{Token: "t", SyncEngine: SyncEngine{Workers: -1}}); err == nil {
		t.Fatal("New() accepted a negative worker count")
	}
}

func TestSyncEngineStartsIdleAndPublishesNothing(t *testing.T) {
	t.Parallel()

	s := newEngineServer(t, nil)
	client := newHubClient()
	s.hub.register(client)

	if err := s.sync.start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !s.sync.running() {
		t.Fatal("the engine reports itself as stopped after start")
	}
	if counts := s.sync.engine.Pending(); counts.Total() != 0 {
		t.Fatalf("an engine with no integration holds %d jobs", counts.Total())
	}
	select {
	case ev := <-client.events:
		t.Fatalf("an idle engine published %q", ev.Type)
	default:
	}
	if err := s.sync.close(context.WithoutCancel(t.Context())); err != nil {
		t.Fatalf("close: %v", err)
	}
	if s.sync.running() {
		t.Fatal("the engine reports itself as running after close")
	}
	// Close is idempotent: a second one waits for the first and reports the
	// same result rather than closing a closed engine.
	if err := s.sync.close(context.WithoutCancel(t.Context())); err != nil {
		t.Fatalf("the second close: %v", err)
	}
}

func TestSyncEngineDrainsAndLeaksNoGoroutines(t *testing.T) {
	// Not parallel: it counts goroutines.
	before := goroutineCount(t, 0)

	s := newEngineServer(t, nil)
	done := make(chan struct{}, 8)
	startEngine(t, s, func(batch []syncengine.Job) error {
		for range batch {
			done <- struct{}{}
		}
		return nil
	})
	for i := 0; i < 4; i++ {
		if _, err := s.sync.engine.Enqueue(t.Context(), syncengine.Request{Kind: testJobKind}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	// The shutdown drains rather than abandoning: every job that was in flight
	// has run by the time close returns.
	if err := s.sync.close(context.WithoutCancel(t.Context())); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(done) != 4 {
		t.Fatalf("the drain ran %d of 4 jobs", len(done))
	}
	if after := goroutineCount(t, before); after > before {
		t.Fatalf("goroutines = %d, want at most %d", after, before)
	}
}

// goroutineCount reads the goroutine count, waiting briefly for it to fall back
// to `want` so that a worker still on its way out is not reported as a leak.
func goroutineCount(t *testing.T, want int) int {
	t.Helper()

	count := runtime.NumGoroutine()
	for i := 0; i < 50 && count > want; i++ {
		time.Sleep(10 * time.Millisecond)
		count = runtime.NumGoroutine()
	}
	return count
}

func TestRegisterSyncHandlerIsTheRegistrationPoint(t *testing.T) {
	t.Parallel()

	s := newEngineServer(t, nil)
	handler := syncengine.HandlerFunc(func(context.Context, []syncengine.Job) error { return nil })

	if err := s.RegisterSyncHandler(testJobKind, handler); err != nil {
		t.Fatalf("RegisterSyncHandler(): %v", err)
	}
	if err := s.RegisterSyncHandler(testJobKind, handler); err == nil {
		t.Fatal("registering one kind twice must be refused")
	}
	if s.SyncEngineHandle() == nil {
		t.Fatal("SyncEngineHandle() is nil")
	}
	// Nothing is registered for an unknown kind, so nothing can be enqueued for
	// it: a feature's jobs cannot exist before the feature does.
	if err := s.sync.start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.sync.close(context.WithoutCancel(t.Context())) })
	if _, err := s.sync.engine.Enqueue(t.Context(), syncengine.Request{Kind: "nobody.handles.this"}); err == nil {
		t.Fatal("an unregistered kind was accepted")
	}
}
