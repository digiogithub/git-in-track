package watcher

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Op is the kind of change reported for one path. It mirrors the fsnotify
// operations, and the polling fallback synthesizes the same values.
type Op string

// The change kinds a watcher reports.
const (
	// Create is a path that appeared, including a rename into place.
	Create Op = "create"
	// Write is content written to an existing path.
	Write Op = "write"
	// Remove is a path that disappeared.
	Remove Op = "remove"
	// Rename is the source name of a rename; the destination arrives as Create.
	Rename Op = "rename"
	// Chmod is a metadata-only change (mode, owner, times).
	Chmod Op = "chmod"
)

// Event is one coalesced change to one path of one repository.
type Event struct {
	// Repo is the key the path's repository was registered under.
	Repo string `json:"repo"`
	// Path is repo-relative and always uses forward slashes.
	Path string `json:"path"`
	// Op is the change kind after coalescing the debounce window.
	Op Op `json:"op"`
	// Time is when the last raw event for this path was seen.
	Time time.Time `json:"time"`
}

// Defaults applied by New when an Options field is left zero.
const (
	// DefaultDebounce is the coalescing window (docs/06 section 9.2).
	DefaultDebounce = 250 * time.Millisecond
	// DefaultMaxWindow caps how long a busy tree can keep sliding the window,
	// so a git checkout of many files still produces batches promptly.
	DefaultMaxWindow = 500 * time.Millisecond
	// DefaultPollInterval is the scan period of the degraded mode.
	DefaultPollInterval = 5 * time.Second
	// DefaultMaxWatches guards against inotify exhaustion (docs/07 section 7.2).
	DefaultMaxWatches = 8192
)

// inotifyHint is printed with the degradation warning on Linux, where the
// watch limit is the usual cause.
const inotifyHint = "raise the limit with: sysctl fs.inotify.max_user_watches=524288"

// Channel capacities. Events blocks the watcher when the consumer stalls, which
// is deliberate: dropping a batch would silently desynchronise the index.
// Errors is advisory and drops instead of blocking.
const (
	eventBuffer = 64
	errorBuffer = 16
)

// pmngrDir is the one dot-directory the watcher descends into: it holds the
// backlog (docs/03 section 2).
const pmngrDir = ".pmngr"

// defaultIgnore are the editor droppings and temporary files that must never
// reach the index, including the ".tmp" files our own atomic writes create
// (docs/06 section 9.2).
var defaultIgnore = []string{
	"*.swp", "*.swx", "*~", ".#*", "#*#", "4913", "*.tmp", "*.orig", ".DS_Store",
}

// ErrClosed is returned by AddRepo and RemoveRepo after Close.
var ErrClosed = errors.New("watcher: closed")

// errMaxWatches stops a tree walk once the watch budget is exhausted.
var errMaxWatches = errors.New("watcher: watch limit reached")

// Options configures a Watcher. The zero value is usable: every field has a
// documented default.
type Options struct {
	// Debounce is the coalescing window, DefaultDebounce when zero.
	Debounce time.Duration
	// Ignore holds extra gitignore-style globs, on top of the built-in editor
	// droppings. A pattern without a slash matches any path segment; a pattern
	// with a slash is anchored at the repository root; a "dir/**" pattern
	// matches the directory and everything below it.
	Ignore []string
	// MaxWatches caps the number of registered directories across all
	// repositories, DefaultMaxWatches when zero. Reaching it logs a warning and
	// stops registering new directories.
	MaxWatches int
	// PollInterval is the scan period of the degraded mode,
	// DefaultPollInterval when zero.
	PollInterval time.Duration
	// Logger receives degradation and limit warnings. Defaults to slog.Default().
	Logger *slog.Logger
	// Clock stamps Event.Time. Defaults to time.Now. It does not drive the
	// debounce window, which always uses the real clock.
	Clock func() time.Time
	// ForcePolling skips fsnotify entirely and starts degraded. It exists for
	// tests and for users on network filesystems that deliver no events.
	ForcePolling bool
}

// fileMeta is the polling fallback's fingerprint of a file.
type fileMeta struct {
	size    int64
	modTime time.Time
}

// repoWatch is one registered repository.
type repoWatch struct {
	key  string
	root string
	// dirs are the absolute directories currently watched for this repository.
	dirs map[string]struct{}
	// snap is the last polling snapshot, nil while fsnotify is healthy.
	snap map[string]fileMeta
}

// pendingKey identifies a coalescing slot.
type pendingKey struct {
	repo string
	path string
}

// pendingEvent is the accumulated state of one path inside the window.
type pendingEvent struct {
	op  Op
	at  time.Time
	seq uint64
}

// Watcher turns file-system changes under the registered repositories into
// batched, debounced events. It is safe for concurrent use.
type Watcher struct {
	debounce     time.Duration
	maxWindow    time.Duration
	pollInterval time.Duration
	maxWatches   int
	ignore       ignoreSet
	log          *slog.Logger
	clock        func() time.Time

	events chan []Event
	errs   chan error
	done   chan struct{}
	wg     sync.WaitGroup

	mu        sync.Mutex
	closed    bool
	fsw       *fsnotify.Watcher
	degraded  bool
	warnedMax bool
	repos     map[string]*repoWatch
	dirOwner  map[string]string
	watches   int
	pending   map[pendingKey]*pendingEvent
	seq       uint64
}

// New builds a Watcher and starts its event loop. It only fails on invalid
// options: a file-system watcher that cannot be created is not an error, it is
// a degradation to polling (docs/07 section 6.3).
func New(opts Options) (*Watcher, error) {
	if opts.Debounce < 0 || opts.PollInterval < 0 {
		return nil, errors.New("watcher: debounce and poll interval must not be negative")
	}
	if opts.MaxWatches < 0 {
		return nil, errors.New("watcher: max watches must not be negative")
	}
	if opts.Debounce == 0 {
		opts.Debounce = DefaultDebounce
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = DefaultPollInterval
	}
	if opts.MaxWatches == 0 {
		opts.MaxWatches = DefaultMaxWatches
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}

	maxWindow := DefaultMaxWindow
	if opts.Debounce > maxWindow {
		maxWindow = 2 * opts.Debounce
	}

	w := &Watcher{
		debounce:     opts.Debounce,
		maxWindow:    maxWindow,
		pollInterval: opts.PollInterval,
		maxWatches:   opts.MaxWatches,
		ignore:       newIgnoreSet(opts.Ignore),
		log:          opts.Logger,
		clock:        opts.Clock,
		events:       make(chan []Event, eventBuffer),
		errs:         make(chan error, errorBuffer),
		done:         make(chan struct{}),
		repos:        make(map[string]*repoWatch),
		dirOwner:     make(map[string]string),
		pending:      make(map[pendingKey]*pendingEvent),
	}

	if opts.ForcePolling {
		w.mu.Lock()
		w.degradeLocked("polling forced by configuration", nil)
		w.mu.Unlock()
	} else {
		fsw, err := fsnotify.NewWatcher()
		if err != nil {
			w.mu.Lock()
			w.degradeLocked("cannot create a file-system watcher", err)
			w.mu.Unlock()
		} else {
			w.fsw = fsw
		}
	}

	w.wg.Add(1)
	go w.run()
	return w, nil
}

// Events returns the batch channel. One batch is one debounce window, with at
// most one event per path, in the order the paths were first touched. The
// channel is closed by Close.
func (w *Watcher) Events() <-chan []Event { return w.events }

// Errors returns non-fatal watcher errors. It is advisory: when nobody reads
// it, errors are dropped rather than blocking the event loop.
func (w *Watcher) Errors() <-chan error { return w.errs }

// Degraded reports whether the watcher fell back to periodic scanning.
func (w *Watcher) Degraded() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.degraded
}

// Watched reports how many directories are currently registered with fsnotify.
// It is zero in degraded mode.
func (w *Watcher) Watched() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.watches
}

// AddRepo registers the tree rooted at root under key. Every directory below
// root is watched, skipping .git, node_modules, dot-directories other than
// .pmngr, and the ignore list.
func (w *Watcher) AddRepo(key, root string) error {
	if key == "" {
		return errors.New("watcher: repository key must not be empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("watcher: resolve %s: %w", root, err)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("watcher: open %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("watcher: open %s: not a directory", root)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrClosed
	}
	if existing, ok := w.repos[key]; ok {
		if existing.root == abs {
			return nil
		}
		return fmt.Errorf("watcher: repository %q is already registered at %s", key, existing.root)
	}

	r := &repoWatch{key: key, root: abs, dirs: make(map[string]struct{})}
	w.repos[key] = r

	if w.fsw != nil {
		if err := w.watchTreeLocked(r, abs); err != nil {
			w.degradeLocked("cannot register a directory watch", err)
		}
	}
	if w.fsw == nil {
		snap, err := scanTree(abs, w.ignore)
		if err != nil {
			return fmt.Errorf("watcher: scan %s: %w", root, err)
		}
		r.snap = snap
	}
	return nil
}

// RemoveRepo drops a repository, its watches and its pending events. Removing
// an unknown key is a no-op.
func (w *Watcher) RemoveRepo(key string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrClosed
	}
	r, ok := w.repos[key]
	if !ok {
		return nil
	}
	for dir := range r.dirs {
		w.unwatchLocked(dir)
	}
	delete(w.repos, key)
	for k := range w.pending {
		if k.repo == key {
			delete(w.pending, k)
		}
	}
	return nil
}

// Close stops the watcher and closes the event and error channels. It is
// idempotent and safe to call concurrently; pending, undelivered changes are
// discarded.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	fsw := w.fsw
	w.fsw = nil
	close(w.done)
	w.mu.Unlock()

	var err error
	if fsw != nil {
		if cerr := fsw.Close(); cerr != nil {
			err = fmt.Errorf("watcher: close: %w", cerr)
		}
	}
	w.wg.Wait()
	close(w.events)
	close(w.errs)
	return err
}

// run is the single goroutine that owns the debounce timer. Every mutation of
// the pending buffer happens under w.mu, so AddRepo and RemoveRepo can run
// concurrently with it.
func (w *Watcher) run() {
	defer w.wg.Done()

	fsEvents, fsErrors := w.fsChannels()

	poll := time.NewTicker(w.pollInterval)
	defer poll.Stop()

	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()

	var (
		armed       bool
		windowStart time.Time
	)
	arm := func() {
		if !w.hasPending() {
			return
		}
		now := time.Now()
		wait := w.debounce
		if !armed {
			armed = true
			windowStart = now
		} else if rest := windowStart.Add(w.maxWindow).Sub(now); rest < wait {
			wait = max(rest, 0)
		}
		timer.Reset(wait)
	}

	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-fsEvents:
			if !ok {
				fsEvents = nil
				continue
			}
			w.handleFSEvent(ev)
			arm()
		case err, ok := <-fsErrors:
			if !ok {
				fsErrors = nil
				continue
			}
			if err != nil {
				w.log.Warn("file-system watcher reported an error", "error", err)
				w.sendErr(fmt.Errorf("watcher: fsnotify: %w", err))
			}
		case <-poll.C:
			if w.Degraded() {
				w.pollOnce()
				arm()
			}
		case <-timer.C:
			armed = false
			w.flush()
		}
	}
}

// fsChannels snapshots the fsnotify channels once, so that a later degradation
// (which closes the watcher) simply closes them.
func (w *Watcher) fsChannels() (events <-chan fsnotify.Event, errs <-chan error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fsw == nil {
		return nil, nil
	}
	return w.fsw.Events, w.fsw.Errors
}

// hasPending reports whether the coalescing buffer holds anything.
func (w *Watcher) hasPending() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.pending) > 0
}

// flush delivers the current window. The batch is built under the lock and
// sent outside it, so a slow consumer never blocks AddRepo or Close.
func (w *Watcher) flush() {
	w.mu.Lock()
	batch := w.drainLocked()
	w.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	select {
	case w.events <- batch:
	case <-w.done:
	}
}

// drainLocked empties the coalescing buffer into a batch ordered by first touch.
func (w *Watcher) drainLocked() []Event {
	if len(w.pending) == 0 {
		return nil
	}
	type ordered struct {
		ev  Event
		seq uint64
	}
	rows := make([]ordered, 0, len(w.pending))
	for k, p := range w.pending {
		rows = append(rows, ordered{
			ev:  Event{Repo: k.repo, Path: k.path, Op: p.op, Time: p.at},
			seq: p.seq,
		})
		delete(w.pending, k)
	}
	slices.SortFunc(rows, func(a, b ordered) int {
		switch {
		case a.seq < b.seq:
			return -1
		case a.seq > b.seq:
			return 1
		default:
			return 0
		}
	})
	batch := make([]Event, len(rows))
	for i, r := range rows {
		batch[i] = r.ev
	}
	return batch
}

// sendErr publishes a non-fatal error, dropping it when nobody is listening.
func (w *Watcher) sendErr(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sendErrLocked(err)
}

func (w *Watcher) sendErrLocked(err error) {
	if err == nil || w.closed {
		return
	}
	select {
	case w.errs <- err:
	default:
	}
}

// handleFSEvent converts one raw fsnotify event into buffer state.
func (w *Watcher) handleFSEvent(ev fsnotify.Event) {
	abs := filepath.Clean(ev.Name)
	op := toOp(ev.Op)
	if op == "" {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	key, rel, ok := w.locateLocked(abs)
	if !ok {
		return
	}
	// A directory we watch: never an item event, but the watch set must follow.
	if _, isDir := w.dirOwner[abs]; isDir {
		if op == Remove || op == Rename {
			w.dropTreeLocked(abs)
		}
		return
	}
	if w.skipRel(rel, false) {
		return
	}
	if op == Create || op == Write {
		if info, err := os.Lstat(abs); err == nil && info.IsDir() {
			if !w.skipRel(rel, true) {
				w.adoptDirLocked(w.repos[key], abs)
			}
			return
		}
	}
	w.recordLocked(key, rel, op)
}

// locateLocked maps an absolute path to its repository and repo-relative path.
func (w *Watcher) locateLocked(abs string) (key, rel string, ok bool) {
	key, ok = w.dirOwner[filepath.Dir(abs)]
	if !ok {
		if key, ok = w.dirOwner[abs]; !ok {
			return "", "", false
		}
	}
	r, ok := w.repos[key]
	if !ok {
		return "", "", false
	}
	rel, err := filepath.Rel(r.root, abs)
	if err != nil {
		return "", "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", "", false
	}
	return key, rel, true
}

// adoptDirLocked registers a directory that appeared after the initial walk and
// reports the files it already contains, which may have been created in the gap
// between the directory's creation and its watch.
func (w *Watcher) adoptDirLocked(r *repoWatch, dir string) {
	if r == nil || w.fsw == nil {
		return
	}
	if err := w.watchTreeLocked(r, dir); err != nil {
		w.degradeLocked("cannot register a directory watch", err)
		return
	}
	err := walkTree(dir, r.root, w.ignore, nil, func(_, rel string, _ fs.FileInfo) error {
		w.recordLocked(r.key, rel, Create)
		return nil
	})
	if err != nil {
		w.log.Warn("cannot list a new directory", "dir", dir, "error", err)
	}
}

// dropTreeLocked forgets a directory and everything watched below it.
func (w *Watcher) dropTreeLocked(dir string) {
	key, ok := w.dirOwner[dir]
	if !ok {
		return
	}
	r, ok := w.repos[key]
	if !ok {
		return
	}
	prefix := dir + string(filepath.Separator)
	for d := range r.dirs {
		if d == dir || strings.HasPrefix(d, prefix) {
			w.unwatchLocked(d)
		}
	}
}

// unwatchLocked removes one directory watch. fsnotify drops watches for deleted
// directories on its own, so a failure here is expected and ignored.
func (w *Watcher) unwatchLocked(dir string) {
	key, ok := w.dirOwner[dir]
	if !ok {
		return
	}
	if r, ok := w.repos[key]; ok {
		delete(r.dirs, dir)
	}
	delete(w.dirOwner, dir)
	if w.watches > 0 {
		w.watches--
	}
	if w.fsw != nil {
		_ = w.fsw.Remove(dir)
	}
}

// watchTreeLocked registers dir and every directory below it, stopping at the
// watch budget.
func (w *Watcher) watchTreeLocked(r *repoWatch, dir string) error {
	return walkTree(dir, r.root, w.ignore, func(abs, _ string) error {
		err := w.addWatchLocked(r, abs)
		if errors.Is(err, errMaxWatches) {
			return fs.SkipAll
		}
		return err
	}, nil)
}

// addWatchLocked registers one directory.
func (w *Watcher) addWatchLocked(r *repoWatch, dir string) error {
	if _, ok := r.dirs[dir]; ok {
		return nil
	}
	if w.watches >= w.maxWatches {
		if !w.warnedMax {
			w.warnedMax = true
			w.log.Warn("watch limit reached, new directories are not watched",
				"max_watches", w.maxWatches, "hint", inotifyHint)
		}
		return errMaxWatches
	}
	if err := w.fsw.Add(dir); err != nil {
		return fmt.Errorf("watch %s: %w", dir, err)
	}
	r.dirs[dir] = struct{}{}
	w.dirOwner[dir] = r.key
	w.watches++
	return nil
}

// degradeLocked switches to the polling scanner. It is one-way: the process
// keeps polling until it restarts, and gintrack doctor reports the state.
func (w *Watcher) degradeLocked(reason string, cause error) {
	if w.degraded {
		return
	}
	w.degraded = true
	w.log.Warn("file watching degraded to periodic scanning",
		"reason", reason, "error", cause, "poll_interval", w.pollInterval, "hint", inotifyHint)

	if w.fsw != nil {
		_ = w.fsw.Close()
		w.fsw = nil
	}
	w.dirOwner = make(map[string]string)
	w.watches = 0
	for _, r := range w.repos {
		r.dirs = make(map[string]struct{})
		if r.snap != nil {
			continue
		}
		snap, err := scanTree(r.root, w.ignore)
		if err != nil {
			w.log.Warn("cannot scan a repository for polling", "repo", r.key, "error", err)
			continue
		}
		r.snap = snap
	}
	w.sendErrLocked(cause)
}

// pollOnce rescans every repository and turns the difference in size and
// modification time into events.
func (w *Watcher) pollOnce() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, r := range w.repos {
		snap, err := scanTree(r.root, w.ignore)
		if err != nil {
			w.log.Warn("polling scan failed", "repo", r.key, "error", err)
			continue
		}
		for rel, now := range snap {
			before, ok := r.snap[rel]
			switch {
			case !ok:
				w.recordLocked(r.key, rel, Create)
			case before.size != now.size || !before.modTime.Equal(now.modTime):
				w.recordLocked(r.key, rel, Write)
			}
		}
		for rel := range r.snap {
			if _, ok := snap[rel]; !ok {
				w.recordLocked(r.key, rel, Remove)
			}
		}
		r.snap = snap
	}
}

// recordLocked folds one change into the coalescing buffer.
func (w *Watcher) recordLocked(repoKey, rel string, op Op) {
	k := pendingKey{repo: repoKey, path: rel}
	cur, ok := w.pending[k]
	if !ok {
		w.seq++
		w.pending[k] = &pendingEvent{op: op, at: w.clock(), seq: w.seq}
		return
	}
	merged, keep := mergeOps(cur.op, op)
	if !keep {
		delete(w.pending, k)
		return
	}
	cur.op = merged
	cur.at = w.clock()
}

// mergeOps folds a new operation into the one already buffered for a path. It
// returns false when the pair cancels out, as a create followed by a remove
// inside the same window does.
func mergeOps(old, incoming Op) (Op, bool) {
	switch {
	case incoming == Chmod:
		// Metadata changes never override a content change.
		return old, true
	case old == Chmod:
		return incoming, true
	case old == Create && (incoming == Remove || incoming == Rename):
		return "", false
	case old == Create:
		// Create + Write (or another Create) is still a creation.
		return Create, true
	case (old == Remove || old == Rename) && (incoming == Create || incoming == Write):
		// The path came back inside the window: that is a modification.
		return Write, true
	case old == Write && incoming == Create:
		// A rename into place over a file we already saw written.
		return Write, true
	default:
		return incoming, true
	}
}

// toOp translates an fsnotify operation set. Compound sets keep the most
// significant change.
func toOp(op fsnotify.Op) Op {
	switch {
	case op.Has(fsnotify.Create):
		return Create
	case op.Has(fsnotify.Remove):
		return Remove
	case op.Has(fsnotify.Rename):
		return Rename
	case op.Has(fsnotify.Write):
		return Write
	case op.Has(fsnotify.Chmod):
		return Chmod
	default:
		return ""
	}
}

// skipRel reports whether a repo-relative path is outside the watcher's
// interest, either because a directory on the way is skipped or because the
// path matches an ignore pattern.
func (w *Watcher) skipRel(rel string, isDir bool) bool {
	return skipRelPath(rel, isDir, w.ignore)
}

func skipRelPath(rel string, isDir bool, ig ignoreSet) bool {
	if rel == "" || rel == "." {
		return false
	}
	segs := strings.Split(rel, "/")
	for i, seg := range segs {
		if (i < len(segs)-1 || isDir) && skipDirName(seg) {
			return true
		}
	}
	return ig.match(rel)
}

// skipDirName reports the directories a watch walk never descends into. It
// mirrors core's vault walk: .git, node_modules and every dot-directory except
// the backlog folder.
//
// docs/06 section 9.2 also wants .git/HEAD and .git/refs to be observed so that
// branch switches are noticed; that is a separate, narrow watch registered by
// the git layer, not part of the documents subtree walked here.
func skipDirName(name string) bool {
	switch name {
	case pmngrDir:
		return false
	case ".git", "node_modules":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// scanTree fingerprints every interesting file below root, keyed by
// repo-relative path.
func scanTree(root string, ig ignoreSet) (map[string]fileMeta, error) {
	out := make(map[string]fileMeta)
	err := walkTree(root, root, ig, nil, func(_, rel string, info fs.FileInfo) error {
		out[rel] = fileMeta{size: info.Size(), modTime: info.ModTime()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// walkTree walks root, reporting directories and files whose paths, relative to
// base, survive the skip rules. Symlinks are never followed. Unreadable entries
// are skipped rather than failing the walk.
func walkTree(root, base string, ig ignoreSet,
	onDir func(abs, rel string) error,
	onFile func(abs, rel string, info fs.FileInfo) error,
) error {
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			// An entry that vanished or cannot be read is skipped: a walk of a
			// live tree races with the writers it is there to observe.
			return nil //nolint:nilerr // deliberate: unreadable entries are skipped
		}
		rel, relErr := filepath.Rel(base, p)
		if relErr != nil {
			// Outside base: nothing we can report a path for.
			return nil //nolint:nilerr // an unrelatable path is skipped, not fatal
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			if skipRelPath(rel, true, ig) {
				return fs.SkipDir
			}
			if onDir == nil {
				return nil
			}
			return onDir(p, rel)
		case d.Type()&fs.ModeSymlink != 0:
			return nil
		default:
			if onFile == nil || skipRelPath(rel, false, ig) {
				return nil
			}
			info, infoErr := d.Info()
			if infoErr != nil {
				// The file disappeared between the walk and the stat.
				return nil //nolint:nilerr // deliberate: a vanished file needs no report
			}
			return onFile(p, rel, info)
		}
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", root, err)
	}
	return nil
}

// ignoreSet matches repo-relative paths against gitignore-style globs.
type ignoreSet struct {
	patterns []string
}

// newIgnoreSet combines the built-in editor droppings with the user's globs.
func newIgnoreSet(extra []string) ignoreSet {
	patterns := make([]string, 0, len(defaultIgnore)+len(extra))
	patterns = append(patterns, defaultIgnore...)
	for _, p := range extra {
		if p = strings.TrimSpace(p); p != "" {
			patterns = append(patterns, p)
		}
	}
	return ignoreSet{patterns: patterns}
}

// match reports whether rel is ignored.
func (s ignoreSet) match(rel string) bool {
	if rel == "" || rel == "." {
		return false
	}
	segs := strings.Split(rel, "/")
	for _, raw := range s.patterns {
		p := strings.TrimSuffix(strings.TrimPrefix(raw, "/"), "/")
		anchored := strings.HasPrefix(raw, "/")
		if strings.HasPrefix(p, "**/") {
			p = strings.TrimPrefix(p, "**/")
			anchored = false
		}
		if p == "" {
			continue
		}
		if prefix, ok := strings.CutSuffix(p, "/**"); ok {
			if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
				return true
			}
			continue
		}
		if anchored || strings.Contains(p, "/") {
			if ok, err := path.Match(p, rel); err == nil && ok {
				return true
			}
			continue
		}
		for _, seg := range segs {
			if ok, err := path.Match(p, seg); err == nil && ok {
				return true
			}
		}
	}
	return false
}
