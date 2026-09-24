package syncengine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The shipped defaults. Two workers and five requests a second are deliberately
// modest: the companion runs on a laptop next to the editor, and a background
// sync that saturates either the CPU or the remote's rate limit is a worse
// neighbor than one that takes a minute longer.
const (
	DefaultWorkers   = 2
	DefaultBatchSize = 20
	DefaultRate      = 5.0
	// DefaultDebounce is the window jobs sharing a kind and a key are coalesced
	// over, the same two seconds the commit debounce uses.
	DefaultDebounce = 2 * time.Second
	// DefaultMaxDebounce caps how long a batch may keep being postponed by
	// further jobs arriving for the same key.
	DefaultMaxDebounce = 15 * time.Second
	// DefaultDeadLetterMax is how many failed jobs are kept before the oldest
	// are evicted.
	DefaultDeadLetterMax = 100
	// DefaultPruneInterval is how often done jobs older than the retention
	// window are swept out of a long-running engine.
	DefaultPruneInterval = time.Hour
)

// Handler does the work of one batch of jobs.
//
// Every job in batch shares a Kind and a Key: that is what "batch" means here.
// The slice is a copy, so mutating it changes nothing the engine will look at
// again.
//
// Returning nil marks every job in the batch done. Returning an error applies
// to the whole batch: each job in it records the error, and each retries or
// fails according to [Classify] and the [RetryPolicy]. A handler that can
// partially succeed should therefore make its per-item work idempotent and let
// the whole batch run again, rather than trying to report which half worked.
//
// Handle must be idempotent: the same job is handed to it again after a
// retryable error, after a restart that replayed the journal, and when a user
// retries it from the dead-letter list. See the package documentation.
//
// Handle must respect ctx: it is cancelled when the job is cancelled and when
// the engine is closed with an expired deadline. It is *not* cancelled when the
// HTTP request that enqueued the job goes away — that detachment is the point
// of the engine.
type Handler interface {
	Handle(ctx context.Context, batch []Job) error
}

// HandlerFunc adapts a function to [Handler].
type HandlerFunc func(ctx context.Context, batch []Job) error

// Handle calls f.
func (f HandlerFunc) Handle(ctx context.Context, batch []Job) error { return f(ctx, batch) }

// Request is what a caller enqueues.
type Request struct {
	// Kind selects the handler. Required, and a handler must already be
	// registered for it.
	Kind Kind
	// Key is the coalescing key. Jobs sharing a kind and a key are handed to
	// the handler together.
	Key string
	// Payload is opaque to the engine. Keep it small and free of content and of
	// credentials: it is written to the journal.
	Payload json.RawMessage
	// ID makes the enqueue idempotent. When it names a job that is already
	// queued or running, nothing new is created and that job is returned. Empty
	// means the engine allocates an id.
	ID string
}

// Options configures an [Engine]. The zero value is valid: every field has a
// documented default.
type Options struct {
	// Workers is the size of the pool. Zero means DefaultWorkers. It can be
	// changed on a running engine with [Engine.SetWorkers].
	Workers int
	// BatchSize caps how many jobs one handler call receives. A batch that
	// reaches it fires at once, without waiting for the debounce. Zero means
	// DefaultBatchSize.
	BatchSize int
	// Rate is the shared outbound limit in jobs per second, across the whole
	// pool. Zero means DefaultRate; a negative value means no limit.
	Rate float64
	// Burst is how many jobs may start at once before the rate applies. Zero
	// means the rate, rounded up.
	Burst int
	// Debounce is the coalescing window. Zero means DefaultDebounce; a negative
	// value dispatches every job the moment it is enqueued, which is what most
	// tests want.
	Debounce time.Duration
	// MaxDebounce caps the postponement. Zero means DefaultMaxDebounce.
	MaxDebounce time.Duration
	// Retry is the retry policy. The zero value is the shipped ladder.
	Retry RetryPolicy
	// DeadLetterMax bounds the dead-letter list. Zero means
	// DefaultDeadLetterMax; the oldest entry is evicted, counted and logged
	// when the list is full.
	DeadLetterMax int
	// CacheDir is where jobs.json is written. Empty disables persistence
	// entirely, which is what a caller with no cache directory — and most unit
	// tests — want.
	CacheDir string
	// JournalFlush is the window journal writes are coalesced over. Zero means
	// DefaultJournalFlush.
	JournalFlush time.Duration
	// Retention is how long a done job is kept before it is pruned. Zero means
	// DefaultRetention.
	Retention time.Duration
	// PruneInterval is how often a running engine prunes. Zero means
	// DefaultPruneInterval.
	PruneInterval time.Duration
	// Clock is the source of time. Nil means [SystemClock].
	Clock Clock
	// Logger receives the few things worth saying out loud: a damaged journal,
	// a dead-letter eviction, a failed write. Nil discards them.
	Logger *slog.Logger
	// Rand supplies the retry jitter in [0, 1). Nil means math/rand.
	Rand func() float64
	// Classify overrides the error classifier. Nil means [Classify], which
	// reads the interfaces documented in errors.go. This is the seam for a
	// client whose errors say what they mean in some other way.
	Classify func(err error, now time.Time) Classification
	// OnJob is called after every state change, with a copy of the job, on a
	// goroutine that is not holding the engine lock. It is the seam the
	// WebSocket layer of a later story hangs off. Calls are made one at a time
	// and in the order the changes happened, even when they come from different
	// goroutines (GIT-US-0146), so it must not block for long and must never
	// call back into a method that changes a job — Enqueue, Cancel,
	// RetryDeadLetter — nor into the engine's blocking methods: the call it
	// would make waits for the one in progress to return.
	OnJob func(Job)
}

// withDefaults fills the zero fields.
func (o Options) withDefaults() Options {
	if o.Workers == 0 {
		o.Workers = DefaultWorkers
	}
	if o.BatchSize == 0 {
		o.BatchSize = DefaultBatchSize
	}
	if o.Rate == 0 {
		o.Rate = DefaultRate
	}
	if o.Debounce == 0 {
		o.Debounce = DefaultDebounce
	}
	if o.MaxDebounce == 0 {
		o.MaxDebounce = DefaultMaxDebounce
	}
	if o.DeadLetterMax == 0 {
		o.DeadLetterMax = DefaultDeadLetterMax
	}
	if o.JournalFlush == 0 {
		o.JournalFlush = DefaultJournalFlush
	}
	if o.Retention == 0 {
		o.Retention = DefaultRetention
	}
	if o.PruneInterval == 0 {
		o.PruneInterval = DefaultPruneInterval
	}
	if o.Clock == nil {
		o.Clock = SystemClock
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.Classify == nil {
		o.Classify = Classify
	}
	o.Retry = o.Retry.withDefaults()
	return o
}

// validate reports a setting that cannot be honored.
func (o Options) validate() error {
	switch {
	case o.Workers < 0:
		return fmt.Errorf("%w: Workers must not be negative", ErrInvalidOption)
	case o.BatchSize < 0:
		return fmt.Errorf("%w: BatchSize must not be negative", ErrInvalidOption)
	case o.DeadLetterMax < 0:
		return fmt.Errorf("%w: DeadLetterMax must not be negative", ErrInvalidOption)
	case o.Retention < 0:
		return fmt.Errorf("%w: Retention must not be negative", ErrInvalidOption)
	case o.PruneInterval < 0:
		return fmt.Errorf("%w: PruneInterval must not be negative", ErrInvalidOption)
	}
	return o.Retry.validate()
}

// batch is the accumulated state of one coalescing key, the same structure
// internal/gitops/committer.go keeps per repository and item.
type batch struct {
	kind Kind
	key  string
	ids  []string
	// ctx is the detached context of the job that opened the batch. It carries
	// the caller's values and none of its cancellation.
	ctx      context.Context
	timer    Timer
	deadline time.Time
}

// Engine is the background job engine. It is safe for concurrent use, and does
// nothing at all until [Engine.Start] is called.
type Engine struct {
	opts    Options
	clock   Clock
	log     *slog.Logger
	limiter *limiter
	journal *journal

	// generation identifies the current pool. Changing the pool size and
	// closing both bump it, and a worker whose generation is no longer current
	// retires as soon as it is between batches — the supervision idiom of
	// internal/tunnel/tunnel.go. Result fencing is finer than this and lives on
	// jobRecord.gen: a worker that is already inside a handler always finishes,
	// because dropping its batch would lose the jobs in it, but its result is
	// discarded for any job whose stamp changed under it.
	generation atomic.Uint64

	mu       sync.Mutex
	cond     *sync.Cond
	announce announcer
	handlers map[Kind]Handler
	pending  map[string]*batch
	queue    []*batch
	jobs     map[string]*jobRecord
	order    []string
	dead     []string
	evicted  int
	running  int
	started  bool
	closed   bool
	seq      uint64
	baseCtx  context.Context
	pruner   Timer
	wg       sync.WaitGroup

	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

// New builds an engine. It starts no goroutine and reads nothing from disk:
// register the handlers first, then call [Engine.Start], so a journal replayed
// at start-up always finds the handler its jobs need.
func New(opts Options) (*Engine, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	opts = opts.withDefaults()

	e := &Engine{
		opts:      opts,
		clock:     opts.Clock,
		log:       opts.Logger,
		handlers:  map[Kind]Handler{},
		pending:   map[string]*batch{},
		jobs:      map[string]*jobRecord{},
		baseCtx:   context.Background(),
		closeDone: make(chan struct{}),
	}
	e.cond = sync.NewCond(&e.mu)
	e.announce.init(opts.OnJob)
	e.limiter = newLimiter(opts.Clock, opts.Rate, opts.Burst)

	path := ""
	if opts.CacheDir != "" {
		path = filepath.Join(opts.CacheDir, JournalName)
	}
	e.journal = newJournal(path, opts.Clock, opts.Logger, opts.JournalFlush)
	e.journal.snapshot = e.journalDoc
	return e, nil
}

// Register attaches a handler to a kind. It must be called before the kind is
// enqueued, and each kind may be registered once: a second registration is a
// wiring mistake, not a replacement.
func (e *Engine) Register(kind Kind, h Handler) error {
	if kind == "" {
		return fmt.Errorf("%w: a handler needs a kind", ErrInvalidOption)
	}
	if h == nil {
		return fmt.Errorf("%w: a handler must not be nil", ErrInvalidOption)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	if _, exists := e.handlers[kind]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateKind, kind)
	}
	e.handlers[kind] = h
	return nil
}

// Start replays the journal, prunes what is too old to keep and brings the
// worker pool up. It is idempotent.
//
// ctx supplies the values — a logger, a request id — that every job context
// inherits; its cancellation does not reach the engine, which is stopped with
// [Engine.Close] instead.
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}
	if e.started {
		e.mu.Unlock()
		return nil
	}
	e.started = true
	e.baseCtx = context.WithoutCancel(ctx)
	e.mu.Unlock()

	e.replay()

	e.mu.Lock()
	//nolint:contextcheck // the pool runs on the engine's own detached base
	// context; the caller's context supplies values, never cancellation.
	e.startWorkersLocked(e.generation.Load(), e.opts.Workers)
	e.armPruneLocked()
	e.mu.Unlock()
	return nil
}

// Enqueue accepts a job and returns it as it was recorded. It never blocks on
// the work itself.
//
// The caller's context is detached with context.WithoutCancel: an HTTP response
// being written must not cancel the import it asked for. The values of ctx
// survive, so anything the caller attached still travels with the job.
func (e *Engine) Enqueue(ctx context.Context, req Request) (Job, error) {
	if req.Kind == "" {
		return Job{}, fmt.Errorf("%w: a job needs a kind", ErrInvalidOption)
	}
	jobCtx := context.WithoutCancel(ctx)

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return Job{}, ErrClosed
	}
	if _, ok := e.handlers[req.Kind]; !ok {
		e.mu.Unlock()
		return Job{}, fmt.Errorf("%w: %s", ErrNoHandler, req.Kind)
	}
	if req.ID != "" {
		if r, ok := e.jobs[req.ID]; ok {
			if r.job.State == StateQueued || r.job.State == StateRunning {
				job := r.job.clone()
				e.mu.Unlock()
				return job, nil
			}
		}
	}
	now := e.clock.Now()
	id := req.ID
	if id == "" {
		id = e.nextIDLocked()
	}
	r := &jobRecord{job: Job{
		ID:        id,
		Kind:      req.Kind,
		Key:       req.Key,
		Payload:   req.Payload,
		State:     StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}}
	e.jobs[id] = r
	e.order = append(e.order, id)
	e.addToBatchLocked(jobCtx, r, now)
	job := r.job.clone()
	turn := e.announce.reserveLocked()
	e.cond.Broadcast()
	e.mu.Unlock()

	e.journal.markDirty()
	e.announce.run(turn, job)
	return job, nil
}

// nextIDLocked allocates a job id. Ids are sequential so a journal, a log and a
// test all read in the order things happened.
func (e *Engine) nextIDLocked() string {
	for {
		e.seq++
		id := fmt.Sprintf("j-%06d", e.seq)
		if _, taken := e.jobs[id]; !taken {
			return id
		}
	}
}

// batchKey is what two jobs must share to be handed to the handler together.
func batchKey(kind Kind, key string) string { return string(kind) + "\x00" + key }

// addToBatchLocked puts a queued job into its coalescing batch, firing the
// batch at once when it is full or when the debounce is disabled.
func (e *Engine) addToBatchLocked(ctx context.Context, r *jobRecord, now time.Time) {
	key := batchKey(r.job.Kind, r.job.Key)
	b, ok := e.pending[key]
	if !ok {
		b = &batch{kind: r.job.Kind, key: r.job.Key, ctx: ctx, deadline: now.Add(e.opts.MaxDebounce)}
		e.pending[key] = b
	}
	b.ids = append(b.ids, r.job.ID)
	r.batchKey = key
	if e.opts.Debounce < 0 || len(b.ids) >= e.opts.BatchSize {
		e.fireLocked(key)
		return
	}
	e.armLocked(ctx, key, b, now)
}

// armLocked (re)starts the debounce timer of a batch without letting a steady
// stream of jobs postpone it past its deadline — the arm/fire pair of
// internal/gitops/committer.go, on the injectable clock.
func (e *Engine) armLocked(ctx context.Context, key string, b *batch, now time.Time) {
	wait := e.opts.Debounce
	if left := b.deadline.Sub(now); left < wait {
		wait = left
	}
	if wait < 0 {
		wait = 0
	}
	if b.timer != nil && b.timer.Stop() {
		// The pending callback will never run, so release the entry it held.
		e.wg.Done()
	}
	e.wg.Add(1)
	b.timer = e.clock.AfterFunc(wait, func() {
		defer e.wg.Done()
		e.fire(ctx, key)
	})
}

// fire moves the batch registered under key to the ready queue, if it is still
// there.
func (e *Engine) fire(_ context.Context, key string) {
	e.mu.Lock()
	e.fireLocked(key)
	e.mu.Unlock()
}

// fireLocked moves a pending batch to the ready queue and wakes a worker.
func (e *Engine) fireLocked(key string) {
	b, ok := e.pending[key]
	if !ok {
		return
	}
	delete(e.pending, key)
	if b.timer != nil && b.timer.Stop() {
		e.wg.Done()
	}
	b.timer = nil
	for _, id := range b.ids {
		if r := e.jobs[id]; r != nil && r.batchKey == key {
			r.batchKey = ""
		}
	}
	e.queue = append(e.queue, b)
	e.cond.Broadcast()
}

// startWorkersLocked brings up n workers stamped with a generation.
func (e *Engine) startWorkersLocked(gen uint64, n int) {
	for i := 0; i < n; i++ {
		e.wg.Add(1)
		go e.worker(gen)
	}
}

// worker pulls batches until its generation is retired or the engine closes.
func (e *Engine) worker(gen uint64) {
	defer e.wg.Done()
	for {
		b, ok := e.nextBatch(gen)
		if !ok {
			return
		}
		e.runBatch(b)
	}
}

// nextBatch blocks until a batch is ready, counting it as running in the very
// same critical section that takes it out of the queue: a batch that is in
// neither place is a batch a concurrent Flush would return without waiting for.
func (e *Engine) nextBatch(gen uint64) (*batch, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for {
		if gen != e.generation.Load() {
			return nil, false
		}
		if len(e.queue) > 0 {
			b := e.queue[0]
			e.queue = e.queue[1:]
			e.running++
			return b, true
		}
		e.cond.Wait()
	}
}

// finishBatch releases a batch's in-flight mark.
func (e *Engine) finishBatch() {
	e.mu.Lock()
	e.running--
	e.cond.Broadcast()
	e.mu.Unlock()
}

// runBatch is one trip through the limiter and the handler.
func (e *Engine) runBatch(b *batch) {
	defer e.finishBatch()

	jobs, stamps, turn, ctx, cancel := e.startBatch(b)
	defer cancel()
	// Announce the queued-to-running transition off the lock, as every other
	// state change is announced. Without this an observer sees a job queued and
	// then finished, and a progress display has nothing to open a row on. The
	// turn is taken even for an empty batch, so it must always be run.
	e.announce.run(turn, jobs...)
	if len(jobs) == 0 {
		// Every job in the batch was cancelled or superseded while it waited.
		return
	}

	err := e.limiter.Wait(ctx)
	if err == nil {
		e.mu.Lock()
		h, ok := e.handlers[b.kind]
		e.mu.Unlock()
		if !ok {
			err = fmt.Errorf("%w: %s", ErrNoHandler, b.kind)
		} else {
			err = safeHandle(ctx, h, jobs)
		}
	}
	e.complete(b, jobs, stamps, err)
}

// safeHandle calls a handler, turning a panic into a terminal error rather than
// letting one badly written handler take the companion down with it.
func safeHandle(ctx context.Context, h Handler, jobs []Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: handler panicked: %v", ErrTerminal, r)
		}
	}()
	return h.Handle(ctx, jobs) //nolint:wrapcheck // a handler error is classified, not wrapped: wrapping would hide the interfaces Classify reads
}

// startBatch marks the batch's still-queued jobs running and builds the context
// the handler runs under. The returned stamps are what complete fences its
// result with.
func (e *Engine) startBatch(b *batch) (jobs []Job, stamps map[string]uint64, turn uint64, ctx context.Context, cancel context.CancelFunc) {
	parent := b.ctx
	if parent == nil {
		parent = e.baseCtx
	}
	ctx, cancel = context.WithCancel(parent)

	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.clock.Now()
	jobs = make([]Job, 0, len(b.ids))
	stamps = make(map[string]uint64, len(b.ids))
	for _, id := range b.ids {
		r := e.jobs[id]
		if r == nil || r.job.State != StateQueued || r.retry != nil {
			continue
		}
		if err := transition(r, StateRunning, now); err != nil {
			continue
		}
		r.job.Attempts++
		r.cancel = cancel
		stamps[id] = r.gen
		jobs = append(jobs, r.job.clone())
	}
	return jobs, stamps, e.announce.reserveLocked(), ctx, cancel
}

// complete applies the outcome of one batch to each of its jobs.
func (e *Engine) complete(b *batch, jobs []Job, stamps map[string]uint64, err error) {
	var cls Classification
	now := e.clock.Now()
	if err != nil {
		cls = e.opts.Classify(err, now)
	}

	changed := make([]Job, 0, len(jobs))
	e.mu.Lock()
	for _, j := range jobs {
		r := e.jobs[j.ID]
		if r == nil || r.gen != stamps[j.ID] || r.job.State != StateRunning {
			// Superseded: the job was cancelled or re-queued while the handler
			// was still inside it, and that decision is the newer one.
			continue
		}
		r.cancel = nil
		switch {
		case err == nil:
			_ = transition(r, StateDone, now)
		case cls.Class == ClassCancelled:
			// The batch was abandoned rather than failed — the engine is
			// closing, or a sibling job in it was cancelled. Hand the job back
			// to the queue without spending an attempt on it: losing it would
			// be the one thing a queue must never do.
			r.job.Attempts--
			_ = transition(r, StateQueued, now)
			if !e.closed {
				e.addToBatchLocked(b.ctx, r, now)
			}
		default:
			r.job.LastError = &ErrorRecord{
				Attempt:    r.job.Attempts,
				Class:      cls.Class,
				Message:    redactString(err.Error()),
				At:         now,
				RetryAfter: cls.RetryAfter,
			}
			switch {
			case cls.Class == ClassTerminal || e.opts.Retry.exhausted(r.job.Attempts):
				_ = transition(r, StateFailed, now)
				e.deadLetterLocked(r)
			case e.closed:
				// Shutting down: the job keeps its place in the queue and the
				// journal carries it to the next run rather than a timer that
				// will never fire carrying it nowhere.
				_ = transition(r, StateQueued, now)
				r.job.NextAttempt = now.Add(e.opts.Retry.Delay(r.job.Attempts, cls.RetryAfter, e.opts.Rand))
			default:
				delay := e.opts.Retry.Delay(r.job.Attempts, cls.RetryAfter, e.opts.Rand)
				_ = transition(r, StateQueued, now)
				r.job.NextAttempt = now.Add(delay)
				e.scheduleRetryLocked(b.ctx, r.job.ID, delay)
			}
		}
		changed = append(changed, r.job.clone())
	}
	turn := e.announce.reserveLocked()
	e.cond.Broadcast()
	e.mu.Unlock()

	e.journal.markDirty()
	e.announce.run(turn, changed...)
}

// scheduleRetryLocked arms the timer that returns a job to its batch.
func (e *Engine) scheduleRetryLocked(ctx context.Context, id string, delay time.Duration) {
	r := e.jobs[id]
	if r == nil {
		return
	}
	e.wg.Add(1)
	r.retry = e.clock.AfterFunc(delay, func() {
		defer e.wg.Done()
		e.retryFire(ctx, id)
	})
}

// retryFire returns a job whose backoff has elapsed to its coalescing batch.
func (e *Engine) retryFire(ctx context.Context, id string) {
	e.mu.Lock()
	r := e.jobs[id]
	if r == nil || r.retry == nil || r.job.State != StateQueued {
		e.mu.Unlock()
		return
	}
	r.retry = nil
	now := e.clock.Now()
	r.job.NextAttempt = time.Time{}
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.addToBatchLocked(ctx, r, now)
	e.cond.Broadcast()
	e.mu.Unlock()
	e.journal.markDirty()
}

// deadLetterLocked records a failed job in the bounded dead-letter list.
func (e *Engine) deadLetterLocked(r *jobRecord) {
	if r.dead {
		return
	}
	r.dead = true
	e.dead = append(e.dead, r.job.ID)
	for len(e.dead) > e.opts.DeadLetterMax {
		oldest := e.dead[0]
		e.dead = e.dead[1:]
		e.evicted++
		if victim := e.jobs[oldest]; victim != nil {
			victim.dead = false
			e.removeLocked(oldest)
		}
		e.log.Warn("sync engine dead-letter list full, oldest entry evicted",
			"job", oldest, "limit", e.opts.DeadLetterMax, "evicted", e.evicted)
	}
}

// Cancel withdraws a job. A queued job leaves the queue; a running job has the
// context of the batch it is in cancelled, and its result is dropped when the
// handler returns. It reports whether anything was cancelled, so an unknown id
// — or a job that already finished — is a no-op and not an error.
//
// Canceling a running job cancels the context its whole batch shares. Its
// siblings are not cancelled: they return to the queue with their attempt count
// untouched and run again.
func (e *Engine) Cancel(id string) bool {
	e.mu.Lock()
	r := e.jobs[id]
	if r == nil {
		e.mu.Unlock()
		return false
	}
	now := e.clock.Now()
	var cancel func()
	switch r.job.State {
	case StateQueued:
		e.detachLocked(r)
		if err := transition(r, StateCancelled, now); err != nil {
			e.mu.Unlock()
			return false
		}
		r.gen++
	case StateRunning:
		cancel = r.cancel
		r.cancel = nil
		if err := transition(r, StateCancelled, now); err != nil {
			e.mu.Unlock()
			return false
		}
		// Bump the stamp so the worker still inside the handler finds its
		// result superseded and leaves the job cancelled.
		r.gen++
	default:
		e.mu.Unlock()
		return false
	}
	job := r.job.clone()
	turn := e.announce.reserveLocked()
	e.cond.Broadcast()
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	e.journal.markDirty()
	e.announce.run(turn, job)
	return true
}

// detachLocked removes a queued job from wherever it is waiting: its pending
// batch, or its retry timer.
func (e *Engine) detachLocked(r *jobRecord) {
	if r.retry != nil {
		if r.retry.Stop() {
			e.wg.Done()
		}
		r.retry = nil
	}
	if r.batchKey == "" {
		return
	}
	b, ok := e.pending[r.batchKey]
	r.batchKey = ""
	if !ok {
		return
	}
	ids := b.ids[:0]
	for _, id := range b.ids {
		if id != r.job.ID {
			ids = append(ids, id)
		}
	}
	b.ids = ids
	if len(b.ids) == 0 {
		delete(e.pending, b.kindKey())
		if b.timer != nil && b.timer.Stop() {
			e.wg.Done()
		}
		b.timer = nil
	}
}

// kindKey is the coalescing key a batch is registered under.
func (b *batch) kindKey() string { return batchKey(b.kind, b.key) }

// Flush fires everything pending and waits for the queue to drain, or for ctx
// to expire, whichever comes first. It is what a shutdown and a "sync now"
// button both call.
//
// It does not wait for a job whose retry is scheduled for a future moment: that
// would mean waiting out a backoff ladder, which is not what a flush is for.
func (e *Engine) Flush(ctx context.Context) error {
	e.mu.Lock()
	if !e.started {
		e.mu.Unlock()
		return ErrNotStarted
	}
	for key := range e.pending {
		e.fireLocked(key)
	}
	e.mu.Unlock()

	stop := context.AfterFunc(ctx, func() {
		e.mu.Lock()
		e.cond.Broadcast()
		e.mu.Unlock()
	})
	defer stop()

	e.mu.Lock()
	defer e.mu.Unlock()
	for len(e.queue) > 0 || e.running > 0 || len(e.pending) > 0 {
		if err := ctx.Err(); err != nil {
			return err //nolint:wrapcheck // a cancelled context is reported verbatim
		}
		e.cond.Wait()
	}
	return nil
}

// Close stops the engine. It refuses further enqueues, drains what it can
// within ctx, retires every worker and timer and writes the journal one last
// time. It is idempotent: a second call waits for the first and returns the
// same error.
func (e *Engine) Close(ctx context.Context) error {
	e.closeOnce.Do(func() {
		e.closeErr = e.shutdown(ctx)
		close(e.closeDone)
	})
	<-e.closeDone
	return e.closeErr
}

// shutdown is the body of the first Close.
func (e *Engine) shutdown(ctx context.Context) error {
	e.mu.Lock()
	e.closed = true
	started := e.started
	e.mu.Unlock()

	if started {
		// Best effort: whatever does not drain in time is cancelled below and
		// returns to the queue, where the journal preserves it for the next run.
		_ = e.Flush(ctx)
	}

	e.mu.Lock()
	// Retire the pool and wake everything that is waiting on the queue.
	e.generation.Add(1)
	cancels := make([]func(), 0, len(e.jobs))
	for _, r := range e.jobs {
		if r.retry != nil {
			if r.retry.Stop() {
				e.wg.Done()
			}
			r.retry = nil
		}
		if r.cancel != nil {
			cancels = append(cancels, r.cancel)
			r.cancel = nil
		}
	}
	for key, b := range e.pending {
		if b.timer != nil && b.timer.Stop() {
			e.wg.Done()
		}
		b.timer = nil
		delete(e.pending, key)
	}
	if e.pruner != nil {
		if e.pruner.Stop() {
			e.wg.Done()
		}
		e.pruner = nil
	}
	e.cond.Broadcast()
	e.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	e.wg.Wait()

	return e.journal.close()
}

// Pending reports how many jobs the engine is holding, by state.
func (e *Engine) Pending() Counts {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.countsLocked()
}

// countsLocked summarizes the job table.
func (e *Engine) countsLocked() Counts {
	var c Counts
	for _, r := range e.jobs {
		switch r.job.State {
		case StateQueued:
			c.Queued++
		case StateRunning:
			c.Running++
		case StateDone:
			c.Done++
		case StateFailed:
			c.Failed++
		case StateCancelled:
			c.Cancelled++
		}
	}
	return c
}

// Job returns one job by id.
func (e *Engine) Job(id string) (Job, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.jobs[id]
	if !ok {
		return Job{}, false
	}
	return r.job.clone(), true
}

// Snapshot is everything the REST layer and the queue table need, in one
// consistent read.
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := Snapshot{
		Jobs:              make([]Job, 0, len(e.order)),
		DeadLetter:        make([]Job, 0, len(e.dead)),
		Counts:            e.countsLocked(),
		Workers:           e.opts.Workers,
		BatchSize:         e.opts.BatchSize,
		Rate:              e.limiter.rateOf(),
		DeadLetterEvicted: e.evicted,
		Running:           e.running,
		At:                e.clock.Now(),
	}
	for _, id := range e.order {
		if r, ok := e.jobs[id]; ok {
			s.Jobs = append(s.Jobs, r.job.clone())
		}
	}
	for _, id := range e.dead {
		if r, ok := e.jobs[id]; ok {
			s.DeadLetter = append(s.DeadLetter, r.job.clone())
		}
	}
	return s
}

// DeadLetter lists the jobs that gave up, oldest first.
func (e *Engine) DeadLetter() []Job {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Job, 0, len(e.dead))
	for _, id := range e.dead {
		if r, ok := e.jobs[id]; ok {
			out = append(out, r.job.clone())
		}
	}
	return out
}

// RetryDeadLetter puts a dead-lettered job back in the queue with a fresh
// attempt budget, keeping its history: the error that killed it stays on the
// record so the UI can still show what happened.
func (e *Engine) RetryDeadLetter(id string) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}
	r, ok := e.jobs[id]
	if !ok {
		e.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrUnknownJob, id)
	}
	if !r.dead {
		e.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrNotRetryable, id)
	}
	now := e.clock.Now()
	if err := transition(r, StateQueued, now); err != nil {
		e.mu.Unlock()
		return err
	}
	r.dead = false
	r.job.Attempts = 0
	r.gen++
	e.dead = removeString(e.dead, id)
	e.addToBatchLocked(e.baseCtx, r, now)
	job := r.job.clone()
	turn := e.announce.reserveLocked()
	e.cond.Broadcast()
	e.mu.Unlock()

	e.journal.markDirty()
	e.announce.run(turn, job)
	return nil
}

// ClearDeadLetter forgets every dead-lettered job and reports how many it
// dropped. Nothing else removes a failed job: they are the user's to clear.
func (e *Engine) ClearDeadLetter() int {
	e.mu.Lock()
	n := len(e.dead)
	for _, id := range e.dead {
		if r, ok := e.jobs[id]; ok {
			r.dead = false
		}
		e.removeLocked(id)
	}
	e.dead = nil
	e.mu.Unlock()
	if n > 0 {
		e.journal.markDirty()
	}
	return n
}

// SetWorkers resizes the pool on a running engine. The old workers retire as
// soon as they are between batches; one that is inside a handler finishes the
// batch it has, because abandoning it would lose the jobs in it.
func (e *Engine) SetWorkers(n int) error {
	if n <= 0 {
		return fmt.Errorf("%w: Workers must be at least 1", ErrInvalidOption)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	e.opts.Workers = n
	if !e.started {
		return nil
	}
	gen := e.generation.Add(1)
	e.cond.Broadcast()
	e.startWorkersLocked(gen, n)
	return nil
}

// SetRate changes the shared outbound limit on a running engine. A rate of zero
// or less removes the limit.
func (e *Engine) SetRate(rate float64, burst int) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}
	e.opts.Rate, e.opts.Burst = rate, burst
	e.mu.Unlock()
	e.limiter.setRate(rate, burst)
	return nil
}

// SetBatchSize changes how many jobs one handler call may receive.
func (e *Engine) SetBatchSize(n int) error {
	if n <= 0 {
		return fmt.Errorf("%w: BatchSize must be at least 1", ErrInvalidOption)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	e.opts.BatchSize = n
	return nil
}

// armPruneLocked schedules the next sweep of jobs that are too old to keep.
func (e *Engine) armPruneLocked() {
	if e.closed || e.opts.PruneInterval <= 0 {
		return
	}
	e.wg.Add(1)
	e.pruner = e.clock.AfterFunc(e.opts.PruneInterval, func() {
		defer e.wg.Done()
		e.mu.Lock()
		e.pruner = nil
		n := e.pruneLocked(e.clock.Now())
		e.armPruneLocked()
		e.mu.Unlock()
		if n > 0 {
			e.journal.markDirty()
		}
	})
}

// pruneLocked forgets jobs that finished longer ago than the retention window.
// Failed and dead-lettered jobs are never pruned: they are the record of what
// went wrong and only the user clears them.
func (e *Engine) pruneLocked(now time.Time) int {
	cutoff := now.Add(-e.opts.Retention)
	removed := 0
	for _, id := range append([]string(nil), e.order...) {
		r, ok := e.jobs[id]
		if !ok {
			continue
		}
		if r.job.State != StateDone && r.job.State != StateCancelled {
			continue
		}
		if r.job.UpdatedAt.After(cutoff) {
			continue
		}
		e.removeLocked(id)
		removed++
	}
	return removed
}

// removeLocked forgets one job.
func (e *Engine) removeLocked(id string) {
	delete(e.jobs, id)
	e.order = removeString(e.order, id)
}

// removeString drops the first occurrence of s.
func removeString(list []string, s string) []string {
	for i, v := range list {
		if v == s {
			return append(list[:i:i], list[i+1:]...)
		}
	}
	return list
}

// journalDoc renders the engine's bookkeeping for the journal. It is called by
// the journal, which never holds the engine lock.
func (e *Engine) journalDoc() journalDoc {
	e.mu.Lock()
	defer e.mu.Unlock()
	doc := journalDoc{
		Version:   journalVersion,
		UpdatedAt: e.clock.Now(),
		Jobs:      make([]Job, 0, len(e.order)),
	}
	for _, id := range e.order {
		if r, ok := e.jobs[id]; ok {
			doc.Jobs = append(doc.Jobs, r.job.clone())
		}
	}
	doc.DeadLetter = append([]string(nil), e.dead...)
	return doc
}

// replay reads the journal back into the queue.
//
// A job that was running when the process died is re-queued with its attempt
// count intact: the engine cannot know whether the handler finished, so it
// leans on the idempotence contract handlers are held to rather than on
// guessing. Anything older than the retention window is dropped on the way in.
func (e *Engine) replay() {
	doc := e.journal.load()
	if len(doc.Jobs) == 0 {
		return
	}
	dead := map[string]bool{}
	for _, id := range doc.DeadLetter {
		dead[id] = true
	}

	e.mu.Lock()
	now := e.clock.Now()
	cutoff := now.Add(-e.opts.Retention)
	restored, requeued := 0, 0
	for _, job := range doc.Jobs {
		if job.ID == "" || job.Kind == "" {
			continue
		}
		if _, exists := e.jobs[job.ID]; exists {
			continue
		}
		switch job.State {
		case StateDone, StateCancelled:
			if !job.UpdatedAt.After(cutoff) {
				continue
			}
		case StateRunning:
			// The process died mid-batch. Nothing else can decide what happened
			// to it, so it goes round again.
			job.State = StateQueued
			job.UpdatedAt = now
			requeued++
		case StateQueued:
			job.NextAttempt = time.Time{}
		case StateFailed:
		default:
			continue
		}
		r := &jobRecord{job: job.clone()}
		e.jobs[job.ID] = r
		e.order = append(e.order, job.ID)
		if job.State == StateFailed && dead[job.ID] {
			r.dead = true
			e.dead = append(e.dead, job.ID)
		}
		if job.State == StateQueued {
			e.addToBatchLocked(e.baseCtx, r, now)
		}
		e.seq = maxSeq(e.seq, job.ID)
		restored++
	}
	sort.SliceStable(e.order, func(i, j int) bool {
		a, b := e.jobs[e.order[i]], e.jobs[e.order[j]]
		if a == nil || b == nil {
			return false
		}
		return a.job.CreatedAt.Before(b.job.CreatedAt)
	})
	e.cond.Broadcast()
	e.mu.Unlock()

	if restored > 0 {
		e.log.Info("sync engine journal replayed", "jobs", restored, "requeued", requeued)
		e.journal.markDirty()
	}
}

// maxSeq keeps the id sequence ahead of everything the journal restored, so a
// replayed run cannot allocate an id that is already taken.
func maxSeq(seq uint64, id string) uint64 {
	digits := strings.TrimPrefix(id, "j-")
	if digits == id {
		return seq
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil || n <= seq {
		return seq
	}
	return n
}
