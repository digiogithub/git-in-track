package gitops

import (
	"context"
	"sync"
	"time"
)

// DefaultDebounce is the window rapid saves of the same item are coalesced over
// (`git.commitDebounce`, docs/06 section 13). Two seconds is long enough that a
// burst of keystrokes in the editor becomes one commit and short enough that a
// commit still feels like part of the save.
const DefaultDebounce = 2 * time.Second

// maxDebounce caps how long a pending change may keep being postponed by
// further edits. Without it a user typing steadily for a minute would have
// nothing committed until they paused.
const maxDebounce = 15 * time.Second

// Change is one logical edit waiting to be committed: the repository it landed
// in, the files it touched and what to say about it.
type Change struct {
	// Repo is the caller's key for the repository, normally the mount id. It is
	// what Backends resolves and what an Outcome reports back.
	Repo string
	// Paths are the repository-relative files the write touched, created,
	// modified or removed.
	Paths []string
	// Fields render the commit message.
	Fields Fields
}

// Outcome is the result of committing one batch.
type Outcome struct {
	Repo   string       `json:"repo"`
	Result CommitResult `json:"result"`
	// Err is nil on success. A failure never touches the working tree.
	Err error `json:"-"`
	// Code is the machine code of Err, empty on success.
	Code string `json:"code,omitempty"`
	// Message is the human-readable failure, empty on success.
	Message string `json:"message,omitempty"`
}

// CommitterOptions configures a Committer.
type CommitterOptions struct {
	// Debounce is the coalescing window. Zero means DefaultDebounce; a negative
	// value commits synchronously on Enqueue, which is what tests want.
	Debounce time.Duration
	// Template renders the subject. Nil means the shipped default.
	Template *Template
	// Backend resolves a repository key to its backend. A key with no backend
	// (a mount that is not a git working tree) is dropped silently: not being a
	// repository is a normal state, not a failure.
	Backend func(repo string) (Backend, bool)
	// Sign asks for signed commits; honored by the system backend only.
	Sign bool
	// OnResult is called for every batch that was committed or failed. It runs
	// on the committer's own goroutine, so it must not block for long and must
	// never call Flush or Close: both wait for the commit it is reporting.
	OnResult func(Outcome)
	// Now is the clock. Nil means time.Now.
	Now func() time.Time
}

// Committer batches writes into commits.
//
// The batching key is the repository plus the item the write is about, which is
// exactly the coalescing docs/06 section 3.3 describes: rapid saves of the same
// file become one commit, while two different items still get one commit each.
// A single call that writes many files (a bulk update, a card move) arrives as
// one Change and is therefore one commit per repository — never one per file.
//
// A Committer is safe for concurrent use and does nothing at all until it is
// enabled, so commit-on-save being off costs one boolean check per write.
type Committer struct {
	opts CommitterOptions

	mu      sync.Mutex
	pending map[string]*batch
	closed  bool
	wg      sync.WaitGroup
	// inflight counts the batches that have already left pending and are being
	// written to their repository. A batch is counted in the very same critical
	// section that removes it, so it is never invisible: a batch is either
	// pending, or in flight, or committed.
	inflight int
	// idle is broadcast when inflight falls back to zero.
	idle sync.Cond
}

// batch is the accumulated state of one coalescing key.
type batch struct {
	repo     string
	paths    map[string]bool
	fields   Fields
	items    map[string]bool
	timer    *time.Timer
	deadline time.Time
}

// NewCommitter builds a committer. It starts no goroutine: a timer is armed per
// pending batch and disarmed when the batch is committed.
func NewCommitter(opts CommitterOptions) *Committer {
	if opts.Debounce == 0 {
		opts.Debounce = DefaultDebounce
	}
	if opts.Template == nil {
		opts.Template = MustParseTemplate(DefaultTemplate)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	c := &Committer{opts: opts, pending: map[string]*batch{}}
	c.idle.L = &c.mu
	return c
}

// startCommit records that a batch is about to be written. It must be called
// with mu held, in the same critical section that took the batch out of
// pending: a batch that is in neither place is a batch a concurrent Flush would
// return without waiting for.
func (c *Committer) startCommit() { c.inflight++ }

// finishCommit records that a batch has finished being written.
func (c *Committer) finishCommit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inflight--
	if c.inflight == 0 {
		c.idle.Broadcast()
	}
}

// waitIdle blocks until nothing is being written to a repository any more.
func (c *Committer) waitIdle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for c.inflight > 0 {
		c.idle.Wait()
	}
}

// commitTracked commits one batch and clears its in-flight mark, which the
// caller must have set.
func (c *Committer) commitTracked(ctx context.Context, b *batch) Outcome {
	defer c.finishCommit()
	return c.commit(ctx, b)
}

// Enqueue records a write. It returns immediately; the commit happens once the
// debounce window has elapsed without another write for the same key, or when
// Flush is called.
//
// A negative Debounce commits inline, which keeps tests free of sleeps.
//
// ctx is the caller's context. The deferred commit outlives the call that
// enqueued it, so the timer path carries a cancellation-free copy of it: an
// HTTP response being written must not cancel the commit of what it wrote.
func (c *Committer) Enqueue(ctx context.Context, change Change) {
	if change.Repo == "" || len(change.Paths) == 0 {
		return
	}
	if c.opts.Debounce < 0 {
		c.mu.Lock()
		c.startCommit()
		c.mu.Unlock()
		c.commitTracked(ctx, newBatch(change))
		return
	}
	ctx = context.WithoutCancel(ctx)

	key := change.Repo + "\x00" + coalesceKey(change.Fields)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	b, ok := c.pending[key]
	if !ok {
		b = newBatch(change)
		b.deadline = c.opts.Now().Add(maxDebounce)
		c.pending[key] = b
	} else {
		b.merge(change)
	}
	c.arm(ctx, key, b)
}

// arm (re)starts the timer of a batch, without letting a steady stream of edits
// postpone it past its deadline.
func (c *Committer) arm(ctx context.Context, key string, b *batch) {
	wait := c.opts.Debounce
	if left := b.deadline.Sub(c.opts.Now()); left < wait {
		wait = left
	}
	if wait < 0 {
		wait = 0
	}
	if b.timer != nil && b.timer.Stop() {
		// The pending callback will never run, so release the entry it held.
		c.wg.Done()
	}
	c.wg.Add(1)
	b.timer = time.AfterFunc(wait, func() {
		defer c.wg.Done()
		c.fire(ctx, key)
	})
}

// fire commits the batch registered under key, if it is still there.
func (c *Committer) fire(ctx context.Context, key string) {
	c.mu.Lock()
	b, ok := c.pending[key]
	if ok {
		delete(c.pending, key)
		c.startCommit()
	}
	c.mu.Unlock()
	if !ok {
		return
	}
	c.commitTracked(ctx, b)
}

// Flush commits everything pending right now and waits for it. It is what a
// sync run calls in PREFLIGHT (docs/06 section 4.1) and what a shutdown calls
// so that no edit is left uncommitted.
//
// When it returns, nothing is being written to a repository any more, and that
// includes a batch whose debounce timer fired just before this call: that
// commit is already out of pending and running on the timer's goroutine, so
// Flush cannot commit it, but it still waits for it. Anything else would let a
// caller that flushes and then exits — the "Commit N changes" button followed
// by a shutdown, a sync run's PREFLIGHT — race a commit that is still writing
// into .git.
//
// The outcomes returned are the batches this call committed itself; a batch
// committed by a timer that won the race is reported through OnResult only.
func (c *Committer) Flush(ctx context.Context) []Outcome {
	c.mu.Lock()
	batches := make([]*batch, 0, len(c.pending))
	for key, b := range c.pending {
		if b.timer != nil && b.timer.Stop() {
			// The timer will not run, so release the wait group entry it held.
			c.wg.Done()
		}
		batches = append(batches, b)
		delete(c.pending, key)
		c.startCommit()
	}
	c.mu.Unlock()

	out := make([]Outcome, 0, len(batches))
	for _, b := range batches {
		out = append(out, c.commitTracked(ctx, b))
	}
	c.waitIdle()
	return out
}

// Pending reports how many batches are waiting, which the sync panel shows as
// "N changes not committed yet".
func (c *Committer) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

// Close flushes and refuses further writes. It is idempotent. When it returns,
// no commit is in flight and no timer is left armed, so the process may exit or
// the repository may be removed.
func (c *Committer) Close(ctx context.Context) []Outcome {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		// A second Close must not report "closed" while the first one is still
		// committing what it took.
		c.waitIdle()
		c.wg.Wait()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	out := c.Flush(ctx)
	c.wg.Wait()
	return out
}

// commit renders the message and commits one batch.
func (c *Committer) commit(ctx context.Context, b *batch) Outcome {
	out := Outcome{Repo: b.repo}
	backend, ok := c.backendFor(b.repo)
	if !ok {
		return out
	}
	fields := b.render()
	msg, err := c.opts.Template.Render(fields)
	if err != nil {
		return c.report(fail(out, err))
	}
	res, err := backend.Commit(ctx, CommitRequest{
		Paths:   sortedSet(b.paths),
		Message: msg,
		Sign:    c.opts.Sign,
	})
	if err != nil {
		return c.report(fail(out, err))
	}
	out.Result = res
	return c.report(out)
}

// backendFor resolves a repository key.
func (c *Committer) backendFor(repo string) (Backend, bool) {
	if c.opts.Backend == nil {
		return nil, false
	}
	return c.opts.Backend(repo)
}

// report hands an outcome to the callback and returns it.
func (c *Committer) report(out Outcome) Outcome {
	if c.opts.OnResult != nil {
		c.opts.OnResult(out)
	}
	return out
}

// fail fills the error fields of an outcome.
func fail(out Outcome, err error) Outcome {
	out.Err = err
	out.Code = CodeOf(err)
	if out.Code == "" {
		out.Code = CodeCommitFailed
	}
	out.Message = err.Error()
	return out
}

// newBatch starts a batch from one change.
func newBatch(change Change) *batch {
	b := &batch{
		repo:   change.Repo,
		paths:  map[string]bool{},
		items:  map[string]bool{},
		fields: change.Fields,
	}
	b.absorb(change)
	return b
}

// merge folds a further change of the same key into the batch.
func (b *batch) merge(change Change) {
	prev := b.fields
	b.fields = change.Fields
	// A create followed by edits stays a create: the commit that introduces the
	// file is the interesting one, and the file is new either way.
	if prev.Action == ActionCreate {
		b.fields.Action = ActionCreate
	}
	// A delete always wins: the file is gone whatever came before.
	if change.Fields.Action == ActionDelete {
		b.fields.Action = ActionDelete
	}
	if b.fields.PrevStatus == "" {
		b.fields.PrevStatus = prev.PrevStatus
	}
	if b.fields.Title == "" {
		b.fields.Title = prev.Title
	}
	b.absorb(change)
}

// absorb records the paths and the items a change covers.
func (b *batch) absorb(change Change) {
	for _, p := range normalisePaths(change.Paths) {
		b.paths[p] = true
	}
	if change.Fields.ItemID != "" {
		b.items[change.Fields.ItemID] = true
	}
	if n := change.Fields.Count; n > 1 {
		// A call that already knew it wrote several items keeps its own count.
		b.fields.Count = n
	}
}

// render produces the fields the message is rendered from, folding the item
// count the batch actually covers into them.
func (b *batch) render() Fields {
	f := b.fields
	if n := len(b.items); n > f.Count {
		f.Count = n
	}
	if f.Count > 1 {
		// Several items in one commit: no single id or title can name it, so
		// the built-in bulk subject takes over (docs/06 section 3.3).
		f.ItemID, f.Title = "", ""
	}
	return f
}

// coalesceKey is what two writes must share to end up in the same commit: the
// item they are about, or the exact file set when there is no item.
func coalesceKey(f Fields) string {
	if f.ItemID != "" {
		return f.ItemID
	}
	if f.Board != "" {
		return "board/" + f.Board
	}
	return "repo"
}

// sortedSet renders a path set as a sorted slice.
func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sortStrings(out)
	return out
}
