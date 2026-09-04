package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// The sync operations of the go-git backend (GIT-US-0021).
//
// go-git covers status, fetch and push in process, with no external dependency.
// What it does not have is a rebase, and its merge is fast-forward only
// (docs/06 section 7.1). Rather than pretend, this backend refuses anything it
// cannot do correctly with CodeUnsupported and an actionable message, which is
// the "degrade explicitly" rule of that section. With the default `auto`
// backend a machine that has git never lands here.

// maxWalk bounds every history walk, so a repository with a huge history cannot
// turn a status call into a minute of CPU.
const maxWalk = 5000

// SyncStatus reads the branch, the upstream, the counters and the conflicted
// set in process.
func (b *goGitBackend) SyncStatus(ctx context.Context) (SyncStatus, error) {
	base, err := b.Status(ctx)
	if err != nil {
		return SyncStatus{}, err
	}
	out := SyncStatus{Branch: base.Branch, Detached: base.Detached}
	out.Dirty = normalisePaths(append(append(append([]string{},
		base.Staged...), base.Modified...), base.Untracked...))
	out.Tracked = len(base.Staged)+len(base.Modified) > 0

	if err := b.fillConflicts(&out); err != nil {
		return SyncStatus{}, err
	}
	b.fillRemote(&out)
	b.fillCounters(&out)
	out.Operation = b.operationInProgress()
	out.resolveState()
	return out, nil
}

// fillConflicts records the unmerged paths of a stopped integration.
func (b *goGitBackend) fillConflicts(out *SyncStatus) error {
	wt, err := b.repo.Worktree()
	if err != nil {
		return wrap("status", CodeCommitFailed, err, "open the worktree of %s", b.path)
	}
	st, err := wt.Status()
	if err != nil {
		return wrap("status", CodeCommitFailed, err, "read the status of %s", b.path)
	}
	for path, entry := range st {
		if entry.Staging == git.UpdatedButUnmerged || entry.Worktree == git.UpdatedButUnmerged {
			out.Conflicted = append(out.Conflicted, Conflict{
				Path: filepath.ToSlash(path),
				Kind: conflictKind(byte(entry.Staging), byte(entry.Worktree)),
			})
		}
	}
	return nil
}

// fillRemote resolves the remote, its redacted URL and the tracking ref.
func (b *goGitBackend) fillRemote(out *SyncStatus) {
	cfg, err := b.repo.Config()
	if err != nil {
		return
	}
	remote := ""
	if branch, ok := cfg.Branches[out.Branch]; ok && branch.Remote != "" {
		remote = branch.Remote
	}
	if remote == "" {
		if _, ok := cfg.Remotes[git.DefaultRemoteName]; ok {
			remote = git.DefaultRemoteName
		} else if len(cfg.Remotes) == 1 {
			for name := range cfg.Remotes {
				remote = name
			}
		}
	}
	if remote == "" {
		return
	}
	out.Remote = remote
	if r, ok := cfg.Remotes[remote]; ok && len(r.URLs) > 0 {
		out.RemoteURL = redactURL(r.URLs[0])
	}
	if out.Branch == "" || out.Detached {
		return
	}
	ref := plumbing.NewRemoteReferenceName(remote, out.Branch)
	if _, err := b.repo.Reference(ref, true); err == nil {
		out.Upstream = remote + "/" + out.Branch
	}
}

// remoteURL reads a remote's configured URL. It is the raw value, credential
// and all, so every caller redacts it before it reaches a message or the UI.
func (b *goGitBackend) remoteURL(remote string) string {
	if remote == "" {
		return ""
	}
	cfg, err := b.repo.Config()
	if err != nil {
		return ""
	}
	if r, ok := cfg.Remotes[remote]; ok && len(r.URLs) > 0 {
		return r.URLs[0]
	}
	return ""
}

// fillCounters computes ahead and behind against the tracking ref.
func (b *goGitBackend) fillCounters(out *SyncStatus) {
	if out.Upstream == "" {
		return
	}
	head, err := b.repo.Head()
	if err != nil {
		return
	}
	remoteRef, err := b.repo.Reference(plumbing.NewRemoteReferenceName(out.Remote, out.Branch), true)
	if err != nil {
		return
	}
	ahead, err := b.countRange(remoteRef.Hash(), head.Hash())
	if err != nil {
		return
	}
	behind, err := b.countRange(head.Hash(), remoteRef.Hash())
	if err != nil {
		return
	}
	out.Ahead, out.Behind = ahead, behind
}

// reachable is the set of commits reachable from a hash, bounded by maxWalk.
func (b *goGitBackend) reachable(from plumbing.Hash) (map[plumbing.Hash]bool, error) {
	seen := map[plumbing.Hash]bool{}
	if from.IsZero() {
		return seen, nil
	}
	commit, err := b.repo.CommitObject(from)
	if err != nil {
		return nil, wrap("log", CodeCommitFailed, err, "read commit %s", from.String())
	}
	iter := object.NewCommitPreorderIter(commit, seen, nil)
	defer iter.Close()
	count := 0
	err = iter.ForEach(func(c *object.Commit) error {
		seen[c.Hash] = true
		count++
		if count >= maxWalk {
			return errWalkBound
		}
		return nil
	})
	if err != nil && !errors.Is(err, errWalkBound) {
		return nil, wrap("log", CodeCommitFailed, err, "walk the history of %s", b.path)
	}
	return seen, nil
}

// errWalkBound ends a bounded walk without being a failure.
var errWalkBound = errors.New("walk bound reached")

// countRange counts the commits reachable from to but not from from.
func (b *goGitBackend) countRange(from, to plumbing.Hash) (int, error) {
	commits, err := b.commitRange(from, to, 0)
	if err != nil {
		return 0, err
	}
	return len(commits), nil
}

// commitRange lists the commits reachable from to but not from from, newest
// first, stopping at limit when it is positive.
func (b *goGitBackend) commitRange(from, to plumbing.Hash, limit int) ([]*object.Commit, error) {
	if to.IsZero() {
		return nil, nil
	}
	exclude, err := b.reachable(from)
	if err != nil {
		return nil, err
	}
	head, err := b.repo.CommitObject(to)
	if err != nil {
		return nil, wrap("log", CodeCommitFailed, err, "read commit %s", to.String())
	}
	out := []*object.Commit{}
	// Passing the excluded set as the iterator's seen map prunes those branches
	// of the walk instead of visiting and discarding them.
	seen := make(map[plumbing.Hash]bool, len(exclude))
	for h := range exclude {
		seen[h] = true
	}
	if seen[to] {
		return out, nil
	}
	iter := object.NewCommitPreorderIter(head, seen, nil)
	defer iter.Close()
	err = iter.ForEach(func(c *object.Commit) error {
		out = append(out, c)
		if (limit > 0 && len(out) >= limit) || len(out) >= maxWalk {
			return errWalkBound
		}
		return nil
	})
	if err != nil && !errors.Is(err, errWalkBound) {
		return nil, wrap("log", CodeCommitFailed, err, "walk the history of %s", b.path)
	}
	return out, nil
}

// gitDir locates the repository's git directory, following the `gitdir:` file a
// worktree or a submodule has instead of a directory.
func (b *goGitBackend) gitDir() string {
	dir := b.path
	for {
		candidate := filepath.Join(dir, ".git")
		info, err := os.Stat(candidate)
		switch {
		case err == nil && info.IsDir():
			return candidate
		case err == nil:
			raw, readErr := os.ReadFile(candidate) //nolint:gosec // the path is inside the repository we were opened on
			if readErr != nil {
				return ""
			}
			target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), "gitdir:"))
			if !filepath.IsAbs(target) {
				target = filepath.Join(dir, target)
			}
			return target
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// operationInProgress reports a half-finished rebase or merge.
func (b *goGitBackend) operationInProgress() string {
	dir := b.gitDir()
	if dir == "" {
		return ""
	}
	for _, probe := range []struct{ name, op string }{
		{"rebase-merge", OpRebase},
		{"rebase-apply", OpRebase},
		{"MERGE_HEAD", OpMerge},
	} {
		if _, err := os.Stat(filepath.Join(dir, probe.name)); err == nil {
			return probe.op
		}
	}
	return ""
}

// Fetch downloads the remote branch in process.
func (b *goGitBackend) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	st, err := b.SyncStatus(ctx)
	if err != nil {
		return FetchResult{}, err
	}
	remote := req.Remote
	if remote == "" {
		remote = st.Remote
	}
	if remote == "" {
		return FetchResult{}, failf("fetch", CodeNoRemote, "%s has no git remote to fetch from", b.path)
	}
	branch := req.Branch
	if branch == "" {
		branch = st.Branch
	}
	upstream := remote + "/" + branch
	before := b.revision(upstream)

	tc := transportContext{Op: "fetch", Path: b.path, Remote: remote, URL: b.remoteURL(remote)}
	opts := &git.FetchOptions{
		RemoteName: remote,
		Prune:      req.Prune,
		Tags:       git.NoTags,
	}
	// The credentials come from the user's own helper or ssh-agent and live
	// only for this call (GIT-US-0023, docs/06 section 8.1).
	opts.Auth = authFor(ctx, tc.URL, b.opts.GitBinary)
	err = b.repo.FetchContext(ctx, opts)
	switch {
	case err == nil, errors.Is(err, git.NoErrAlreadyUpToDate):
	default:
		if classified := classifyTransport(tc, err, redactSecrets(err.Error())); classified != nil {
			return FetchResult{}, classified
		}
		if errors.Is(err, transport.ErrAuthenticationRequired) || errors.Is(err, transport.ErrAuthorizationFailed) {
			return FetchResult{}, tc.authError(err, redactSecrets(err.Error()), "authentication required")
		}
		return FetchResult{}, wrap("fetch", CodeFetchFailed, err,
			"could not fetch %s in %s (nothing was changed locally)", remote, b.path)
	}
	after := b.revision(upstream)
	return FetchResult{
		Remote: remote, Upstream: upstream, Before: before, After: after, Updated: before != after,
	}, nil
}

// revision resolves a ref to its hex hash, empty when it does not exist.
func (b *goGitBackend) revision(ref string) string {
	h, err := b.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil || h == nil {
		return ""
	}
	return h.String()
}

// Integrate fast-forwards the branch onto the upstream ref.
//
// go-git has no rebase and no real merge, so anything but a fast-forward is
// refused with CodeUnsupported and a message that names the fix, rather than
// half-applied.
func (b *goGitBackend) Integrate(ctx context.Context, req IntegrateRequest) (IntegrateResult, error) {
	strategy := req.Strategy
	if strategy == "" {
		strategy = StrategyRebase
	}
	st, err := b.SyncStatus(ctx)
	if err != nil {
		return IntegrateResult{}, err
	}
	if req.Upstream == "" || st.Upstream == "" {
		return IntegrateResult{}, failf("integrate", CodeNoUpstream,
			"nothing to integrate: %s tracks no remote branch", b.path)
	}
	res := IntegrateResult{Strategy: strategy, Before: b.revision("HEAD")}
	switch {
	case st.Ahead > 0:
		return res, failf("integrate", CodeUnsupported,
			"the go-git backend can only fast-forward, and %s has %d local commit(s) to %s onto %s: "+
				"install git and set git.backend to auto or system; nothing was changed",
			b.path, st.Ahead, strategy, req.Upstream)
	case st.Tracked:
		return res, failf("integrate", CodeDirtyTree,
			"%s has uncommitted changes to tracked files: commit or stash them before integrating",
			b.path)
	case st.Behind == 0:
		res.After = res.Before
		return res, nil
	}

	target, err := b.repo.ResolveRevision(plumbing.Revision(req.Upstream))
	if err != nil || target == nil {
		return res, wrap("integrate", CodeIntegrateFailed, err, "resolve %s in %s", req.Upstream, b.path)
	}
	wt, err := b.repo.Worktree()
	if err != nil {
		return res, wrap("integrate", CodeIntegrateFailed, err, "open the worktree of %s", b.path)
	}
	// The tree is clean and the branch is strictly behind, so moving branch,
	// index and files onto the upstream commit is exactly a fast-forward.
	if err := wt.Reset(&git.ResetOptions{Commit: *target, Mode: git.HardReset}); err != nil {
		return res, wrap("integrate", CodeIntegrateFailed, err,
			"fast-forward %s onto %s", b.path, req.Upstream)
	}
	res.After, res.FastForward = target.String(), true
	return res, nil
}

// Push publishes the current branch in process.
func (b *goGitBackend) Push(ctx context.Context, req PushRequest) (PushResult, error) {
	st, err := b.SyncStatus(ctx)
	if err != nil {
		return PushResult{}, err
	}
	remote := req.Remote
	if remote == "" {
		remote = st.Remote
	}
	if remote == "" {
		return PushResult{}, failf("push", CodeNoRemote, "%s has no git remote to push to", b.path)
	}
	branch := req.Branch
	if branch == "" {
		branch = st.Branch
	}
	spec := config.RefSpec("refs/heads/" + st.Branch + ":refs/heads/" + branch)
	tc := transportContext{Op: "push", Path: b.path, Remote: remote, URL: b.remoteURL(remote)}
	err = b.repo.PushContext(ctx, &git.PushOptions{
		RemoteName:        remote,
		RefSpecs:          []config.RefSpec{spec},
		RequireRemoteRefs: []config.RefSpec{}, // no lease: we never force-push
		Auth:              authFor(ctx, tc.URL, b.opts.GitBinary),
	})
	switch {
	case errors.Is(err, git.NoErrAlreadyUpToDate):
		return PushResult{Remote: remote, Branch: branch, UpToDate: true}, nil
	case err == nil:
		return PushResult{Remote: remote, Branch: branch, Pushed: st.Ahead}, nil
	}
	if classified := classifyTransport(tc, err, redactSecrets(err.Error())); classified != nil {
		return PushResult{}, classified
	}
	if errors.Is(err, transport.ErrAuthenticationRequired) || errors.Is(err, transport.ErrAuthorizationFailed) {
		return PushResult{}, tc.authError(err, redactSecrets(err.Error()), "authentication required")
	}
	if errors.Is(err, git.ErrNonFastForwardUpdate) ||
		containsAny(strings.ToLower(err.Error()), "non-fast-forward", "fetch first", "rejected") {
		return PushResult{}, &Error{
			Code: CodePushRejected, Op: "push", Err: err,
			Message: "the remote already moved on, so the push to " + remote + "/" + branch +
				" was rejected: fetch and integrate again, then push; your commits are safe locally",
		}
	}
	return PushResult{}, wrap("push", CodePushFailed, err,
		"could not push to %s/%s: your commits are safe locally and nothing was lost", remote, branch)
}

// Abort is not available in process: undoing a half-finished rebase needs the
// reflog handling only the system backend has.
func (b *goGitBackend) Abort(_ context.Context) error {
	return failf("abort", CodeUnsupported,
		"the go-git backend cannot abort a rebase or a merge: run `git rebase --abort` "+
			"or `git merge --abort` in %s, or install git so the system backend is used", b.path)
}

// Continue is not available in process, for the same reason as Abort.
func (b *goGitBackend) Continue(_ context.Context) (IntegrateResult, error) {
	return IntegrateResult{}, failf("continue", CodeUnsupported,
		"the go-git backend cannot continue a rebase or a merge: run `git rebase --continue` "+
			"or `git merge --continue` in %s, or install git so the system backend is used", b.path)
}

// Commits lists what To has and From does not, newest first.
func (b *goGitBackend) Commits(_ context.Context, req LogRequest) ([]Commit, error) {
	to := req.To
	if to == "" {
		to = "HEAD"
	}
	toHash, err := b.repo.ResolveRevision(plumbing.Revision(to))
	if err != nil || toHash == nil {
		return nil, wrap("log", CodeCommitFailed, err, "resolve %s in %s", to, b.path)
	}
	var fromHash plumbing.Hash
	if req.From != "" {
		if h, err := b.repo.ResolveRevision(plumbing.Revision(req.From)); err == nil && h != nil {
			fromHash = *h
		}
	}
	commits, err := b.commitRange(fromHash, *toHash, req.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]Commit, 0, len(commits))
	for _, c := range commits {
		out = append(out, Commit{
			SHA:     c.Hash.String(),
			Subject: subjectOf(c.Message),
			Author:  c.Author.Name + " <" + c.Author.Email + ">",
			Date:    c.Author.When.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	return out, nil
}

// subjectOf takes the first line of a commit message.
func subjectOf(message string) string {
	subject, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	return subject
}
