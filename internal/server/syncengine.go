package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/digiogithub/git-in-track/internal/syncengine"
)

// The background job engine of the companion, story GIT-US-0084
// (docs/07-cli-and-api.md section 4.1, docs/02-architecture.md).
//
// internal/syncengine schedules, batches, rate-limits, retries and journals;
// it knows nothing about HTTP. This file is the whole of the seam between the
// two: it turns the server's configuration into engine options, brings the
// engine up and down on the same path as the watcher and the tunnel, and hangs
// the WebSocket observer of GIT-US-0074 off Options.OnJob.
//
// No job handler lives here. Registering one is a single call to
// Server.RegisterSyncHandler, which must happen before Server.Start because
// Start replays the journal and a replayed job whose kind has no handler is
// work the engine cannot do.

// The shipped defaults of the engine, as documented for `gintrack serve`. They
// mirror internal/syncengine's own so that the two cannot drift.
const (
	// DefaultSyncWorkers is the size of the worker pool.
	DefaultSyncWorkers = syncengine.DefaultWorkers
	// DefaultSyncBatchSize caps how many jobs one handler call receives.
	DefaultSyncBatchSize = syncengine.DefaultBatchSize
	// DefaultSyncRate is the shared outbound limit, in jobs per second.
	DefaultSyncRate = syncengine.DefaultRate
	// DefaultSyncMaxAttempts is how many attempts a job takes before it is
	// dead-lettered.
	DefaultSyncMaxAttempts = syncengine.DefaultMaxAttempts
	// DefaultSyncRetention is how long a finished job is kept.
	DefaultSyncRetention = syncengine.DefaultRetention
	// DefaultSyncDrainTimeout is the grace period a shutdown drains the queue
	// for before journalling whatever is left. It is bounded on purpose: a
	// process that will not exit is worse than a job that runs again next time.
	DefaultSyncDrainTimeout = 5 * time.Second
)

// The ranges the settings are validated against, at startup and on every
// PATCH /api/v1/sync/settings. They are deliberately generous: the point is to
// refuse a value that cannot work (zero workers, a negative rate), not to
// second-guess an operator who knows their tracker.
const (
	maxSyncWorkers     = 64
	maxSyncBatchSize   = 500
	maxSyncRate        = 1000
	maxSyncMaxAttempts = 20
)

// SyncEngine is the configuration of the background job engine. The zero value
// is valid and means the documented defaults.
//
// It lives here rather than in internal/config only because the configuration
// file has no `sync.engine` section yet; the shape is the one that section will
// take, so moving it is a rename (see the report on GIT-T-0175).
type SyncEngine struct {
	// Workers is the size of the pool. Zero means DefaultSyncWorkers.
	Workers int
	// BatchSize caps one handler call. Zero means DefaultSyncBatchSize.
	BatchSize int
	// Rate is the shared outbound limit in jobs per second. Zero means
	// DefaultSyncRate; a negative value removes the limit.
	Rate float64
	// MaxAttempts is the retry budget of a job. Zero means
	// DefaultSyncMaxAttempts; one means never retry.
	MaxAttempts int
	// Retention is how long a finished job is kept before it is pruned. Zero
	// means DefaultSyncRetention.
	Retention time.Duration
	// DrainTimeout bounds the shutdown drain. Zero means
	// DefaultSyncDrainTimeout.
	DrainTimeout time.Duration
	// CacheDir is where the queue journal is written. Empty disables
	// persistence, which is what a test and a companion with no cache directory
	// want.
	CacheDir string
	// Debounce is the coalescing window. Zero means the engine's own default; a
	// negative value dispatches every job immediately, which is what the tests
	// of this package want.
	Debounce time.Duration
}

// withDefaults fills the zero fields.
func (o SyncEngine) withDefaults() SyncEngine {
	if o.Workers == 0 {
		o.Workers = DefaultSyncWorkers
	}
	if o.BatchSize == 0 {
		o.BatchSize = DefaultSyncBatchSize
	}
	if o.Rate == 0 {
		o.Rate = DefaultSyncRate
	}
	if o.MaxAttempts == 0 {
		o.MaxAttempts = DefaultSyncMaxAttempts
	}
	if o.Retention == 0 {
		o.Retention = DefaultSyncRetention
	}
	if o.DrainTimeout == 0 {
		o.DrainTimeout = DefaultSyncDrainTimeout
	}
	return o
}

// Validate reports the first setting that cannot be honored, naming the key the
// operator typed so that the message is actionable on the command line and in
// a problem document alike.
func (o SyncEngine) Validate() error {
	switch {
	case o.Workers < 1 || o.Workers > maxSyncWorkers:
		return &settingsError{field: "workers", message: fmt.Sprintf("must be between 1 and %d", maxSyncWorkers)}
	case o.BatchSize < 1 || o.BatchSize > maxSyncBatchSize:
		return &settingsError{field: "batchSize", message: fmt.Sprintf("must be between 1 and %d", maxSyncBatchSize)}
	case o.Rate == 0 || o.Rate > maxSyncRate:
		return &settingsError{field: "rate", message: fmt.Sprintf(
			"must be greater than 0 and at most %d requests per second; a negative value removes the limit", maxSyncRate)}
	case o.MaxAttempts < 1 || o.MaxAttempts > maxSyncMaxAttempts:
		return &settingsError{field: "maxAttempts", message: fmt.Sprintf("must be between 1 and %d", maxSyncMaxAttempts)}
	case o.Retention < 0:
		return &settingsError{field: "retention", message: "must not be negative"}
	case o.DrainTimeout < 0:
		return &settingsError{field: "drainTimeout", message: "must not be negative"}
	}
	return nil
}

// engineOptions renders the settings as the engine's own options, with the
// observer that publishes sync.job.* wired into OnJob.
func (o SyncEngine) engineOptions(observer *syncObserver) syncengine.Options {
	return syncengine.Options{
		Workers:   o.Workers,
		BatchSize: o.BatchSize,
		Rate:      o.Rate,
		Debounce:  o.Debounce,
		Retry:     syncengine.RetryPolicy{MaxAttempts: o.MaxAttempts},
		Retention: o.Retention,
		CacheDir:  o.CacheDir,
		OnJob:     observer.onJob,
	}
}

// syncState owns the engine for the life of the server: the settings in force,
// the engine itself and the observer that turns its transitions into events.
//
// The engine is built in New rather than in Start so that a handler can be
// registered — and the REST surface can answer with an empty queue — before
// anything is listening.
type syncState struct {
	// mu guards settings and started. The engine has its own lock and is
	// immutable once built.
	mu       sync.RWMutex
	settings SyncEngine
	started  bool

	engine   *syncengine.Engine
	observer *syncObserver
}

// newSyncState builds the engine from the server options.
func newSyncState(opts Options, observer *syncObserver) (*syncState, error) {
	settings := opts.SyncEngine.withDefaults()
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	engine, err := syncengine.New(settings.engineOptions(observer))
	if err != nil {
		return nil, fmt.Errorf("build the sync engine: %w", err)
	}
	return &syncState{settings: settings, engine: engine, observer: observer}, nil
}

// current reports the settings in force.
func (s *syncState) current() SyncEngine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// running reports whether the engine has been started and not yet closed. The
// REST surface answers `sync_engine_not_running` on the strength of it.
func (s *syncState) running() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started
}

// start brings the pool up. Every handler must already be registered: the
// engine replays its journal here, and a replayed job whose kind has no handler
// stays queued forever.
func (s *syncState) start(ctx context.Context) error {
	if err := s.engine.Start(ctx); err != nil {
		return fmt.Errorf("start the sync engine: %w", err)
	}
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	return nil
}

// close drains the queue for the configured grace period and then stops,
// journalling whatever did not finish. ctx must already be detached from the
// shutdown that is waiting for it, or the drain would be cancelled by the very
// signal it exists to survive.
//
// It is idempotent, because Engine.Close is.
func (s *syncState) close(ctx context.Context) error {
	grace := s.current().DrainTimeout
	drain, cancel := context.WithTimeout(ctx, grace)
	defer cancel()

	s.mu.Lock()
	s.started = false
	s.mu.Unlock()

	if err := s.engine.Close(drain); err != nil {
		return fmt.Errorf("close the sync engine: %w", err)
	}
	return nil
}

// applyPatch folds the engine half of a settings patch into the running engine.
// Workers, the batch size and the rate take effect at once; the retry budget is
// fixed when the engine is built, so it is recorded and applies to the next
// start (docs/07 section 5.5).
func (s *syncState) applyPatch(patch syncEnginePatch) error {
	s.mu.Lock()
	next := s.settings
	if patch.Workers != nil {
		next.Workers = *patch.Workers
	}
	if patch.BatchSize != nil {
		next.BatchSize = *patch.BatchSize
	}
	if patch.Rate != nil {
		next.Rate = *patch.Rate
	}
	if patch.MaxAttempts != nil {
		next.MaxAttempts = *patch.MaxAttempts
	}
	// A patch is validated as written: zero means "the default" only when the
	// settings are built, never when a caller spells it out — a pool of no
	// workers cannot run anything and a rate of zero is not "unlimited".
	if err := next.Validate(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.settings = next
	s.mu.Unlock()

	// Applied outside the lock: the engine takes its own, and holding both in
	// one order here and the other order in a callback is how deadlocks start.
	if err := s.engine.SetWorkers(next.Workers); err != nil {
		return fmt.Errorf("apply the worker count: %w", err)
	}
	if err := s.engine.SetBatchSize(next.BatchSize); err != nil {
		return fmt.Errorf("apply the batch size: %w", err)
	}
	if err := s.engine.SetRate(next.Rate, 0); err != nil {
		return fmt.Errorf("apply the rate limit: %w", err)
	}
	return nil
}

// persist writes the engine settings back to the configuration file, so that a
// change made in the UI survives a restart.
//
// It reports false today and writes nothing: the configuration file has no
// `sync.engine` section yet (GIT-T-0175 leaves that half to the owner of
// internal/config). The contract around it is already the one the git settings
// use — reload, replace one section, save, report whether the file took it — so
// that filling it in is a three-line change here.
func (s *syncState) persist() (bool, error) {
	return false, nil
}

// RegisterSyncHandler registers the handler for one kind of background job.
//
// **This is the registration point.** A feature that owns a job kind —
// `youtrack.import`, the comment push, the knowledge-base publish — adds one
// call here, before Start:
//
//	if err := srv.RegisterSyncHandler("youtrack.import", importer); err != nil { ... }
//
// It must be called before [Server.Start], which replays the journal: a job
// restored from a previous run whose kind has no handler can never be
// dispatched. Registering the same kind twice is refused.
func (s *Server) RegisterSyncHandler(kind syncengine.Kind, h syncengine.Handler) error {
	if err := s.sync.engine.Register(kind, h); err != nil {
		return fmt.Errorf("register the %s job handler: %w", kind, err)
	}
	return nil
}

// SyncEngineHandle exposes the engine to the packages that own a job kind, so
// that a feature can enqueue its own work without this package learning what
// that work is. It is never nil.
func (s *Server) SyncEngineHandle() *syncengine.Engine { return s.sync.engine }

// startSyncEngine brings the engine up as part of Server.Start, beside the
// watcher and the tunnel. A failure is logged and never fatal: a companion that
// cannot journal its queue still serves the repositories, which is the whole
// product.
func (s *Server) startSyncEngine(ctx context.Context) {
	if err := s.sync.start(ctx); err != nil {
		s.log.Warn("the sync engine did not start", "error", err)
	}
}

// stopSyncEngine drains and stops the engine. It is deferred next to
// s.git.close, with a context detached from the shutdown.
func (s *Server) stopSyncEngine(ctx context.Context) {
	if err := s.sync.close(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		s.log.Warn("the sync engine did not stop cleanly", "error", err)
	}
}
