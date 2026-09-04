package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The companion's answer to "where does the history come from" (ADR-017): the
// git history of the item files themselves, read through internal/gitops and
// handed to the workspace, which turns it into observations and charts in
// internal/core. Nothing about the metric is computed here.

// gitHistorySource reads the past of a repository's files out of its commits.
// It is the vault.HistorySource of the companion process; the browser installs
// none and gets the stated approximation instead.
type gitHistorySource struct {
	git *gitState
	mu  sync.Mutex
	// caches hold one memoized walk per repository, keyed inside by the commit
	// they were read at. A new commit moves HEAD and the entry is read again,
	// which is the whole of "invalidated when new commits arrive".
	caches map[string]*gitops.HistoryCache
}

// newGitHistorySource binds a history reader to the git layer.
func newGitHistorySource(git *gitState) *gitHistorySource {
	return &gitHistorySource{git: git, caches: map[string]*gitops.HistoryCache{}}
}

// cacheFor returns the memo of one repository, creating it on first use.
func (h *gitHistorySource) cacheFor(repo string) *gitops.HistoryCache {
	h.mu.Lock()
	defer h.mu.Unlock()
	cache, ok := h.caches[repo]
	if !ok {
		cache = gitops.NewHistoryCache()
		h.caches[repo] = cache
	}
	return cache
}

// FileHistory reads every revision of paths in the repository mounted as
// vaultID. A repository git does not drive answers with an empty history rather
// than an error: the metric then degrades to the stated approximation instead
// of failing.
func (h *gitHistorySource) FileHistory(
	ctx context.Context, vaultID string, paths []string,
) (vault.RepoHistory, error) {
	backend, ok := h.git.backendFor(vaultID)
	if !ok {
		return vault.RepoHistory{}, nil
	}
	req := gitops.HistoryRequest{Paths: paths}
	// The head the walk is memoized against. A repository whose HEAD cannot be
	// read is walked without caching rather than not walked at all.
	head := ""
	if commits, err := backend.Commits(ctx, gitops.LogRequest{To: "HEAD", Limit: 1}); err == nil &&
		len(commits) > 0 {
		head = commits[0].SHA
	}
	read, err := h.cacheFor(vaultID).Do(head, req, func() (gitops.FileHistory, error) {
		history, err := backend.History(ctx, req)
		if err != nil {
			return gitops.FileHistory{}, fmt.Errorf("read the history of %s: %w", vaultID, err)
		}
		return history, nil
	})
	if err != nil {
		return vault.RepoHistory{}, fmt.Errorf("history of %s: %w", vaultID, err)
	}
	out := vault.RepoHistory{
		Commits: read.Commits, Truncated: read.Truncated, Oldest: read.Oldest,
		Revisions: make([]vault.RepoRevision, 0, len(read.Revisions)),
	}
	for _, rev := range read.Revisions {
		out.Revisions = append(out.Revisions, vault.RepoRevision{
			Path: rev.Path, At: rev.When, Data: rev.Data, Deleted: rev.Deleted,
		})
	}
	return out, nil
}
