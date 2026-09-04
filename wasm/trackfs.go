package main

import (
	"sort"
	"sync"

	"github.com/digiogithub/git-in-track/internal/core"
)

// trackingFS decorates a core.FS with a write log.
//
// The browser has no way to observe what the Go core touched: the core writes
// into an in-memory file system inside the worker, and the host has to persist
// exactly those files through the File System Access API afterwards. Every
// mutating call therefore runs between begin and take, and take reports the
// files that were written or removed in between (the WriteSet of
// web/src/core-bridge/api.ts).
//
// The atomic write of core.FileStore goes through a sibling ".tmp" file and a
// rename, so a rename moves the pending write to its final path instead of
// reporting a phantom creation followed by a deletion.
type trackingFS struct {
	core.FS

	mu      sync.Mutex
	written map[string]bool
	removed map[string]bool
}

// newTrackingFS wraps an FS. The log starts empty.
func newTrackingFS(inner core.FS) *trackingFS {
	return &trackingFS{FS: inner, written: map[string]bool{}, removed: map[string]bool{}}
}

// begin drops the log so that the next take reports one operation only.
func (t *trackingFS) begin() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.written = map[string]bool{}
	t.removed = map[string]bool{}
}

// take returns the paths written and removed since the last begin, sorted, and
// clears the log.
func (t *trackingFS) take() (written, removed []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	written = sortedKeys(t.written)
	removed = sortedKeys(t.removed)
	t.written = map[string]bool{}
	t.removed = map[string]bool{}
	return written, removed
}

// WriteFile records the path and delegates.
func (t *trackingFS) WriteFile(p string, data []byte) error {
	if err := t.FS.WriteFile(p, data); err != nil {
		return err //nolint:wrapcheck // decorator: the inner file system already says what failed
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.removed, p)
	t.written[p] = true
	return nil
}

// Remove records the deletion and delegates.
func (t *trackingFS) Remove(p string) error {
	if err := t.FS.Remove(p); err != nil {
		return err //nolint:wrapcheck // decorator: the inner file system already says what failed
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.written[p] {
		// The file was created and dropped within the same operation: the host
		// never saw it, so there is nothing to persist and nothing to delete.
		delete(t.written, p)
		return nil
	}
	t.removed[p] = true
	return nil
}

// Rename moves a pending write instead of logging a delete plus a create, which
// is what keeps the ".tmp" file of an atomic write out of the WriteSet.
func (t *trackingFS) Rename(oldPath, newPath string) error {
	if err := t.FS.Rename(oldPath, newPath); err != nil {
		return err //nolint:wrapcheck // decorator: the inner file system already says what failed
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.written[oldPath] {
		delete(t.written, oldPath)
	} else {
		t.removed[oldPath] = true
	}
	delete(t.removed, newPath)
	t.written[newPath] = true
	return nil
}

// sortedKeys returns the keys of a set in lexicographic order.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
