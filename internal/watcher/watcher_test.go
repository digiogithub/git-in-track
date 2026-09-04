package watcher

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// waitTimeout is how long a test waits for an expected event. It is generous on
// purpose: a loaded CI machine can take a while to deliver inotify events.
const waitTimeout = 5 * time.Second

// testDebounce keeps the tests quick while staying far above the time two
// consecutive syscalls take, so coalescing is deterministic.
const testDebounce = 60 * time.Millisecond

func newTestWatcher(t *testing.T, opts Options) *Watcher {
	t.Helper()
	if opts.Debounce == 0 {
		opts.Debounce = testDebounce
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	w, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return w
}

// waitForBatch drains batches until one contains an event matching pred. It
// returns that batch and every event seen up to and including it.
func waitForBatch(t *testing.T, w *Watcher, what string, pred func(Event) bool) (batch, seen []Event) {
	t.Helper()
	deadline := time.After(waitTimeout)
	for {
		select {
		case b, ok := <-w.Events():
			if !ok {
				t.Fatalf("waiting for %s: event channel closed; saw %v", what, seen)
			}
			seen = append(seen, b...)
			for _, ev := range b {
				if pred(ev) {
					return b, seen
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s; saw %v", what, seen)
		}
	}
}

// assertQuiet fails when an event matching pred arrives within d.
func assertQuiet(t *testing.T, w *Watcher, d time.Duration, pred func(Event) bool) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case b, ok := <-w.Events():
			if !ok {
				return
			}
			for _, ev := range b {
				if pred(ev) {
					t.Fatalf("unexpected extra event %+v", ev)
				}
			}
		case <-deadline:
			return
		}
	}
}

func pathIs(p string) func(Event) bool {
	return func(ev Event) bool { return ev.Path == p }
}

func pathAndOp(p string, op Op) func(Event) bool {
	return func(ev Event) bool { return ev.Path == p && ev.Op == op }
}

func countPath(events []Event, p string) int {
	n := 0
	for _, ev := range events {
		if ev.Path == p {
			n++
		}
	}
	return n
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func TestWatcherReportsCreateWriteAndRemove(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, Options{})
	if err := w.AddRepo("proj", root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if w.Degraded() {
		t.Fatal("watcher degraded to polling on a local temp directory")
	}

	file := filepath.Join(root, "story.md")

	writeFile(t, file, "one")
	batch, _ := waitForBatch(t, w, "the create of story.md", pathAndOp("story.md", Create))
	if got := batch[0].Repo; got != "proj" {
		t.Errorf("Repo = %q, want %q", got, "proj")
	}
	if batch[0].Time.IsZero() {
		t.Error("Time is zero")
	}

	writeFile(t, file, "one two")
	waitForBatch(t, w, "the write of story.md", pathAndOp("story.md", Write))

	if err := os.Remove(file); err != nil {
		t.Fatalf("remove: %v", err)
	}
	waitForBatch(t, w, "the removal of story.md", pathAndOp("story.md", Remove))
}

func TestWatcherSkipsGitAndNodeModules(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{".git", "node_modules", ".pmngr"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	w := newTestWatcher(t, Options{})
	if err := w.AddRepo("proj", root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(root, "node_modules", "index.js"), "module.exports = {}\n")
	writeFile(t, filepath.Join(root, ".pmngr", "project.yaml"), "key: GIT\n")
	writeFile(t, filepath.Join(root, "marker.md"), "marker")

	_, seen := waitForBatch(t, w, "the create of marker.md", pathIs("marker.md"))
	for _, ev := range seen {
		if strings.HasPrefix(ev.Path, ".git/") || strings.HasPrefix(ev.Path, "node_modules/") {
			t.Errorf("event for an ignored tree leaked: %+v", ev)
		}
	}
	// The backlog folder is the one dot-directory that must be watched.
	if countPath(seen, ".pmngr/project.yaml") == 0 {
		waitForBatch(t, w, "the create of .pmngr/project.yaml", pathIs(".pmngr/project.yaml"))
	}
}

func TestWatcherCoalescesAtomicSave(t *testing.T) {
	if testing.Short() {
		t.Skip("timing sensitive")
	}
	root := t.TempDir()
	w := newTestWatcher(t, Options{})
	if err := w.AddRepo("proj", root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	tmp := filepath.Join(root, "x.md.tmp")
	final := filepath.Join(root, "x.md")
	writeFile(t, tmp, "# x\n")
	if err := os.Rename(tmp, final); err != nil {
		t.Fatalf("rename: %v", err)
	}

	batch, seen := waitForBatch(t, w, "the atomic save of x.md", pathIs("x.md"))
	if n := countPath(batch, "x.md"); n != 1 {
		t.Errorf("got %d events for x.md in one batch, want 1: %+v", n, batch)
	}
	for _, ev := range seen {
		if strings.HasSuffix(ev.Path, ".tmp") {
			t.Errorf("temporary file leaked into the batch: %+v", ev)
		}
	}
	if op := batch[0].Op; op != Create && op != Write {
		t.Errorf("Op = %q, want create or write", op)
	}
	assertQuiet(t, w, 3*testDebounce, pathIs("x.md"))
}

func TestWatcherCoalescesRapidWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("timing sensitive")
	}
	root := t.TempDir()
	w := newTestWatcher(t, Options{Debounce: 200 * time.Millisecond})
	if err := w.AddRepo("proj", root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	file := filepath.Join(root, "story.md")
	writeFile(t, file, "one")
	waitForBatch(t, w, "the create of story.md", pathAndOp("story.md", Create))

	writeFile(t, file, "two")
	writeFile(t, file, "three")

	batch, _ := waitForBatch(t, w, "the coalesced write", pathAndOp("story.md", Write))
	if n := countPath(batch, "story.md"); n != 1 {
		t.Errorf("got %d events for story.md, want 1: %+v", n, batch)
	}
	assertQuiet(t, w, 600*time.Millisecond, pathIs("story.md"))
}

func TestWatcherWatchesNewDirectories(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, Options{})
	if err := w.AddRepo("proj", root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	before := w.Watched()

	sub := filepath.Join(root, "stories", "nested")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(sub, "deep.md"), "# deep\n")

	waitForBatch(t, w, "the create inside a new directory", pathIs("stories/nested/deep.md"))
	if after := w.Watched(); after <= before {
		t.Errorf("watched directories = %d, want more than %d", after, before)
	}

	if err := os.RemoveAll(filepath.Join(root, "stories")); err != nil {
		t.Fatalf("remove tree: %v", err)
	}
	waitForBatch(t, w, "the removal of the nested file", pathAndOp("stories/nested/deep.md", Remove))
}

func TestWatcherPollingFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("timing sensitive")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "existing.md"), "already here")

	w := newTestWatcher(t, Options{
		ForcePolling: true,
		PollInterval: 20 * time.Millisecond,
		Debounce:     20 * time.Millisecond,
	})
	if err := w.AddRepo("proj", root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if !w.Degraded() {
		t.Fatal("Degraded() = false, want true with ForcePolling")
	}
	if got := w.Watched(); got != 0 {
		t.Errorf("Watched() = %d, want 0 in degraded mode", got)
	}

	writeFile(t, filepath.Join(root, "new.md"), "# new\n")
	_, seen := waitForBatch(t, w, "the polled create of new.md", pathAndOp("new.md", Create))
	for _, ev := range seen {
		if ev.Path == "existing.md" {
			t.Errorf("the initial snapshot leaked an event: %+v", ev)
		}
	}

	if err := os.Remove(filepath.Join(root, "existing.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	waitForBatch(t, w, "the polled removal of existing.md", pathAndOp("existing.md", Remove))
}

func TestWatcherIgnoreGlobs(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, Options{Ignore: []string{"*.log", "build/**"}})
	if err := w.AddRepo("proj", root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "build"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	writeFile(t, filepath.Join(root, "noise.log"), "noise")
	writeFile(t, filepath.Join(root, "build", "out.md"), "generated")
	writeFile(t, filepath.Join(root, "kept.md"), "kept")

	_, seen := waitForBatch(t, w, "the create of kept.md", pathIs("kept.md"))
	for _, ev := range seen {
		if ev.Path == "noise.log" || strings.HasPrefix(ev.Path, "build/") {
			t.Errorf("ignored path leaked: %+v", ev)
		}
	}
}

func TestRemoveRepoStopsEvents(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, Options{})
	if err := w.AddRepo("proj", root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if w.Watched() == 0 {
		t.Fatal("Watched() = 0 after AddRepo")
	}
	if err := w.RemoveRepo("proj"); err != nil {
		t.Fatalf("RemoveRepo: %v", err)
	}
	if got := w.Watched(); got != 0 {
		t.Errorf("Watched() = %d after RemoveRepo, want 0", got)
	}
	if err := w.RemoveRepo("proj"); err != nil {
		t.Errorf("RemoveRepo of an unknown key: %v", err)
	}

	writeFile(t, filepath.Join(root, "story.md"), "one")
	assertQuiet(t, w, 4*testDebounce, func(Event) bool { return true })
}

func TestAddRepoValidatesArguments(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, Options{})

	if err := w.AddRepo("", root); err == nil {
		t.Error("AddRepo with an empty key: want an error")
	}
	if err := w.AddRepo("proj", filepath.Join(root, "missing")); err == nil {
		t.Error("AddRepo with a missing root: want an error")
	}
	file := filepath.Join(root, "file.md")
	writeFile(t, file, "x")
	if err := w.AddRepo("proj", file); err == nil {
		t.Error("AddRepo with a file root: want an error")
	}
	if err := w.AddRepo("proj", root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := w.AddRepo("proj", root); err != nil {
		t.Errorf("re-adding the same root: %v", err)
	}
	if err := w.AddRepo("proj", t.TempDir()); err == nil {
		t.Error("re-adding a different root under the same key: want an error")
	}
}

func TestCloseIsIdempotentAndClosesTheChannel(t *testing.T) {
	root := t.TempDir()
	w, err := New(Options{
		Debounce: testDebounce,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.AddRepo("proj", root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	for range w.Events() { //nolint:revive // drain whatever was buffered
	}
	if _, ok := <-w.Events(); ok {
		t.Error("the event channel is still open after Close")
	}
	if _, ok := <-w.Errors(); ok {
		t.Error("the error channel is still open after Close")
	}
	if err := w.AddRepo("other", root); !errors.Is(err, ErrClosed) {
		t.Errorf("AddRepo after Close = %v, want %v", err, ErrClosed)
	}
	if err := w.RemoveRepo("proj"); !errors.Is(err, ErrClosed) {
		t.Errorf("RemoveRepo after Close = %v, want %v", err, ErrClosed)
	}
}

func TestNewAppliesDefaultsAndRejectsBadOptions(t *testing.T) {
	for name, opts := range map[string]Options{
		"negative debounce":      {Debounce: -time.Second},
		"negative poll interval": {PollInterval: -time.Second},
		"negative max watches":   {MaxWatches: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(opts); err == nil {
				t.Fatal("New: want an error")
			}
		})
	}

	got, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := got.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	if got.debounce != DefaultDebounce {
		t.Errorf("debounce = %v, want %v", got.debounce, DefaultDebounce)
	}
	if got.pollInterval != DefaultPollInterval {
		t.Errorf("poll interval = %v, want %v", got.pollInterval, DefaultPollInterval)
	}
	if got.maxWatches != DefaultMaxWatches {
		t.Errorf("max watches = %d, want %d", got.maxWatches, DefaultMaxWatches)
	}
}

func TestMaxWatchesIsEnforced(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"a", "b", "c", "d"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	w := newTestWatcher(t, Options{MaxWatches: 2})
	if err := w.AddRepo("proj", root); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if got := w.Watched(); got != 2 {
		t.Errorf("Watched() = %d, want 2 (the configured cap)", got)
	}
}

func TestMergeOps(t *testing.T) {
	tests := []struct {
		name     string
		old, in  Op
		want     Op
		wantKeep bool
	}{
		{"write then write", Write, Write, Write, true},
		{"create then write", Create, Write, Create, true},
		{"create then create", Create, Create, Create, true},
		{"create then remove", Create, Remove, "", false},
		{"create then rename", Create, Rename, "", false},
		{"write then remove", Write, Remove, Remove, true},
		{"write then create", Write, Create, Write, true},
		{"remove then create", Remove, Create, Write, true},
		{"rename then create", Rename, Create, Write, true},
		{"remove then write", Remove, Write, Write, true},
		{"write then chmod", Write, Chmod, Write, true},
		{"chmod then write", Chmod, Write, Write, true},
		{"chmod then chmod", Chmod, Chmod, Chmod, true},
		{"remove then remove", Remove, Remove, Remove, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, keep := mergeOps(tc.old, tc.in)
			if keep != tc.wantKeep {
				t.Fatalf("keep = %v, want %v", keep, tc.wantKeep)
			}
			if keep && got != tc.want {
				t.Errorf("op = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIgnoreSetMatch(t *testing.T) {
	ig := newIgnoreSet([]string{"*.log", "build/**", "/root-only.md", "**/generated.md"})
	tests := []struct {
		path string
		want bool
	}{
		{"docs/story.md", false},
		{".pmngr/stories/GIT-US-0001-a.md", false},
		{"docs/.#story.md", true},
		{"docs/story.md.swp", true},
		{"docs/story.md~", true},
		{"docs/4913", true},
		{"docs/.gintrack-123.tmp", true},
		{".DS_Store", true},
		{"docs/.DS_Store", true},
		{"out.log", true},
		{"deep/nested/out.log", true},
		{"build", true},
		{"build/x/y.md", true},
		{"root-only.md", true},
		{"docs/root-only.md", false},
		{"a/b/generated.md", true},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if got := ig.match(tc.path); got != tc.want {
				t.Errorf("match(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestSkipRelPath(t *testing.T) {
	ig := newIgnoreSet(nil)
	tests := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{".git", true, true},
		{".git/HEAD", false, true},
		{"node_modules", true, true},
		{"node_modules/pkg/index.js", false, true},
		{".pmngr", true, false},
		{".pmngr/project.yaml", false, false},
		{".cache", true, true},
		{"docs/.pmngr/stories/a.md", false, false},
		{"docs/story.md", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if got := skipRelPath(tc.path, tc.isDir, ig); got != tc.want {
				t.Errorf("skipRelPath(%q, %v) = %v, want %v", tc.path, tc.isDir, got, tc.want)
			}
		})
	}
}
