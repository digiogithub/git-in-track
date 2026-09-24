package gitops

import (
	"context"
	"sync"

	"github.com/digiogithub/git-in-track/internal/core"
)

// A go-git Repository and its filesystem object storage are not safe for
// concurrent use: a commit resets the object cache and the pack index that a
// status walk is reading at the same moment (GIT-US-0145, GIT-US-0146). The
// companion shares one Backend per repository across every HTTP handler, the
// sync engine and the Committer's timers, so the go-git backend is handed out
// wrapped in serialBackend, which takes one lock per repository around every
// method call.
//
// Lock order, outermost first:
//
//  1. Committer.mu — only ever held briefly, never while calling a Backend;
//  2. the Committer's per-repository commit lock (Committer.repoLocks), which
//     keeps one batch's commit from overlapping another batch's;
//  3. the repository lock below, which is a leaf: while it is held nothing but
//     the wrapped go-git backend runs, and that never calls back into a
//     serialBackend, a Committer or a caller.
//
// Because the repository lock is a leaf and is taken exactly once per call —
// by the wrapper, never by the goGitBackend methods, which call each other
// freely (Commit calls Identity, Fetch calls SyncStatus) — it needs no
// reentrancy and cannot deadlock against the Committer's lock.
//
// The system and jj backends are not wrapped: each call there is a child
// process, git and jj guard their own on-disk state with lock files, and
// serializing a slow network fetch in front of every status read would only
// make the UI wait.

// repoLocks maps the canonical working-tree root of a repository to the lock
// that serializes go-git access to it. Keying by path rather than by Backend
// value means two backends opened on the same repository — a second Open of the
// same mount, or one opened from a subfolder — still share one lock. An entry is
// never removed: there is one per repository the process ever opened.
var repoLocks = struct {
	sync.Mutex
	byRoot map[string]*sync.Mutex
}{byRoot: map[string]*sync.Mutex{}}

// repoLockFor returns the lock of the repository rooted at root.
func repoLockFor(root string) *sync.Mutex {
	key := canonicalPath(root)
	repoLocks.Lock()
	defer repoLocks.Unlock()
	lock, ok := repoLocks.byRoot[key]
	if !ok {
		lock = &sync.Mutex{}
		repoLocks.byRoot[key] = lock
	}
	return lock
}

// serialBackend runs every call of the Backend it wraps under the lock of that
// backend's repository.
type serialBackend struct {
	inner Backend
	mu    *sync.Mutex
}

// serialize wraps a backend so that no two of its calls — nor the calls of any
// other backend serialized on the same root — ever run at the same time.
func serialize(b Backend, root string) Backend {
	return &serialBackend{inner: b, mu: repoLockFor(root)}
}

// Name reports the wrapped backend's name; it reads a constant.
func (s *serialBackend) Name() string { return s.inner.Name() }

// Path reports the wrapped backend's working tree; it reads a constant.
func (s *serialBackend) Path() string { return s.inner.Path() }

// VCSInfo forwards what the wrapped backend knows about its working tree, so
// VCSOf answers the same for a wrapped backend as for a bare one.
func (s *serialBackend) VCSInfo() core.VCSInfo { return VCSOf(s.inner) }

// Capabilities implements Backend. The go-git backend answers it from
// constants, without touching the repository, so it takes no lock.
func (s *serialBackend) Capabilities() Capabilities { return s.inner.Capabilities() }

// Identity implements Backend.
func (s *serialBackend) Identity(ctx context.Context) (Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Identity(ctx) //nolint:wrapcheck // a transparent wrapper
}

// Status implements Backend.
func (s *serialBackend) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Status(ctx) //nolint:wrapcheck // a transparent wrapper
}

// Commit implements Backend.
func (s *serialBackend) Commit(ctx context.Context, req CommitRequest) (CommitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Commit(ctx, req) //nolint:wrapcheck // a transparent wrapper
}

// SyncStatus implements Backend.
func (s *serialBackend) SyncStatus(ctx context.Context) (SyncStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.SyncStatus(ctx) //nolint:wrapcheck // a transparent wrapper
}

// Fetch implements Backend.
func (s *serialBackend) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Fetch(ctx, req) //nolint:wrapcheck // a transparent wrapper
}

// Integrate implements Backend.
func (s *serialBackend) Integrate(ctx context.Context, req IntegrateRequest) (IntegrateResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Integrate(ctx, req) //nolint:wrapcheck // a transparent wrapper
}

// Push implements Backend.
func (s *serialBackend) Push(ctx context.Context, req PushRequest) (PushResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Push(ctx, req) //nolint:wrapcheck // a transparent wrapper
}

// Undo implements Backend.
func (s *serialBackend) Undo(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Undo(ctx) //nolint:wrapcheck // a transparent wrapper
}

// Resume implements Backend.
func (s *serialBackend) Resume(ctx context.Context) (IntegrateResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Resume(ctx) //nolint:wrapcheck // a transparent wrapper
}

// Commits implements Backend.
func (s *serialBackend) Commits(ctx context.Context, req LogRequest) ([]Commit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Commits(ctx, req) //nolint:wrapcheck // a transparent wrapper
}

// History implements Backend.
func (s *serialBackend) History(ctx context.Context, req HistoryRequest) (FileHistory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.History(ctx, req) //nolint:wrapcheck // a transparent wrapper
}

// ConflictFile implements Backend.
func (s *serialBackend) ConflictFile(ctx context.Context, path string) (ConflictVersions, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.ConflictFile(ctx, path) //nolint:wrapcheck // a transparent wrapper
}

// ResolvePath implements Backend.
func (s *serialBackend) ResolvePath(ctx context.Context, req ResolveRequest) (ResolveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.ResolvePath(ctx, req) //nolint:wrapcheck // a transparent wrapper
}
