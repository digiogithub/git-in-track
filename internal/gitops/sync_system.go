package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The sync operations of the system backend (GIT-US-0021).
//
// Everything here shells out to the git the user already has, which is what
// brings credential helpers, `insteadOf` rewrites, LFS and a battle-tested
// rebase along with it (docs/06 section 7.1). Every invocation inherits the
// non-interactive environment of systemBackend.env, so a missing credential
// fails with a message instead of hanging on a prompt.

// SyncStatus reads the branch, the upstream, the counters and the conflicted
// set. `status --porcelain -b` answers all of it in one process.
func (b *systemBackend) SyncStatus(ctx context.Context) (SyncStatus, error) {
	raw, err := b.run(ctx, "status", "--porcelain", "--branch", "--untracked-files=normal")
	if err != nil {
		return SyncStatus{}, wrap("status", CodeCommitFailed, err, "read the status of %s", b.path)
	}
	out := SyncStatus{}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "## ") {
			b.parseBranchLine(&out, line[3:])
			continue
		}
		if len(line) < 4 {
			continue
		}
		index, worktree, path := line[0], line[1], statusPath(line[3:])
		if index == 'U' || worktree == 'U' || (index == 'A' && worktree == 'A') ||
			(index == 'D' && worktree == 'D') {
			out.Conflicted = append(out.Conflicted, Conflict{Path: path, Kind: conflictKind(index, worktree)})
			continue
		}
		out.Dirty = append(out.Dirty, path)
		if index != '?' {
			out.Tracked = true
		}
	}
	out.Dirty = normalisePaths(out.Dirty)

	out.Remote = b.remoteOf(ctx, out.Branch)
	if out.Remote != "" {
		out.RemoteURL = redactURL(strings.TrimSpace(mustRun(b.run(ctx, "remote", "get-url", out.Remote))))
	}
	out.Operation = b.operationInProgress(ctx)
	out.resolveState()
	return out, nil
}

// parseBranchLine reads `main...origin/main [ahead 1, behind 2]`.
func (b *systemBackend) parseBranchLine(out *SyncStatus, line string) {
	if strings.HasPrefix(line, "HEAD (no branch)") {
		out.Branch, out.Detached = "HEAD", true
		return
	}
	head, counters, _ := strings.Cut(line, " [")
	local, upstream, hasUpstream := strings.Cut(head, "...")
	out.Branch = strings.TrimSpace(local)
	if hasUpstream {
		out.Upstream = strings.TrimSpace(upstream)
	}
	for _, part := range strings.Split(strings.TrimSuffix(counters, "]"), ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), " ")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		switch name {
		case "ahead":
			out.Ahead = n
		case "behind":
			out.Behind = n
		}
	}
}

// remoteOf resolves the remote a branch pushes to, falling back to the single
// configured remote when the branch tracks nothing yet.
func (b *systemBackend) remoteOf(ctx context.Context, branch string) string {
	if branch != "" {
		if name := strings.TrimSpace(mustRun(b.run(ctx, "config", "--get", "branch."+branch+".remote"))); name != "" {
			return name
		}
	}
	names := strings.Fields(mustRun(b.run(ctx, "remote")))
	for _, name := range names {
		if name == "origin" {
			return name
		}
	}
	if len(names) == 1 {
		return names[0]
	}
	return ""
}

// operationInProgress reports a half-finished rebase or merge.
func (b *systemBackend) operationInProgress(ctx context.Context) string {
	for _, probe := range []struct{ path, op string }{
		{"rebase-merge", OpRebase},
		{"rebase-apply", OpRebase},
		{"MERGE_HEAD", OpMerge},
	} {
		if b.gitPathExists(ctx, probe.path) {
			return probe.op
		}
	}
	return ""
}

// gitPathExists resolves a path inside the git directory and stats it.
func (b *systemBackend) gitPathExists(ctx context.Context, name string) bool {
	p := strings.TrimSpace(mustRun(b.run(ctx, "rev-parse", "--git-path", name)))
	if p == "" {
		return false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(b.path, p)
	}
	_, err := os.Stat(p)
	return err == nil
}

// Fetch downloads the remote branch. It writes no working file, so a failure
// leaves the repository exactly as it was.
func (b *systemBackend) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	st, err := b.SyncStatus(ctx)
	if err != nil {
		return FetchResult{}, err
	}
	remote, branch, upstream := b.target(st, req.Remote, req.Branch)
	if remote == "" {
		return FetchResult{}, failf("fetch", CodeNoRemote,
			"%s has no git remote to fetch from", b.path)
	}
	before := b.revision(ctx, upstream)

	args := []string{"fetch", remote}
	if req.Prune {
		args = append(args, "--prune")
	}
	if branch != "" {
		args = append(args, branch)
	}
	if _, err := b.run(ctx, args...); err != nil {
		if classified := classifyTransport("fetch", err, outputOf(err)); classified != nil {
			return FetchResult{}, classified
		}
		return FetchResult{}, &Error{
			Code: CodeFetchFailed, Op: "fetch", Err: err, Detail: outputOf(err),
			Message: "could not fetch " + remote + " in " + b.path +
				" (nothing was changed locally)",
		}
	}
	after := b.revision(ctx, upstream)
	return FetchResult{
		Remote: remote, Upstream: upstream,
		Before: before, After: after, Updated: before != after,
	}, nil
}

// target resolves the remote, the remote branch and the tracking ref a request
// acts on, honoring the overrides the caller passed.
func (b *systemBackend) target(st SyncStatus, remote, branch string) (resolvedRemote, resolvedBranch, upstreamRef string) {
	if remote == "" {
		remote = st.Remote
	}
	if branch == "" && st.Upstream != "" {
		if r, br, ok := strings.Cut(st.Upstream, "/"); ok && r == remote {
			branch = br
		}
	}
	if branch == "" {
		branch = st.Branch
	}
	upstream := st.Upstream
	if upstream == "" && remote != "" && branch != "" {
		upstream = remote + "/" + branch
	}
	return remote, branch, upstream
}

// revision resolves a ref, returning the empty string when it does not exist.
func (b *systemBackend) revision(ctx context.Context, ref string) string {
	if ref == "" {
		return ""
	}
	out, err := b.run(ctx, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Integrate rebases or merges the upstream ref into the current branch.
func (b *systemBackend) Integrate(ctx context.Context, req IntegrateRequest) (IntegrateResult, error) {
	strategy := req.Strategy
	if strategy == "" {
		strategy = StrategyRebase
	}
	if !strategy.Valid() {
		return IntegrateResult{}, failf("integrate", CodeUnsupported,
			"unknown integration strategy %q: use rebase or merge", string(strategy))
	}
	if req.Upstream == "" {
		return IntegrateResult{}, failf("integrate", CodeNoUpstream,
			"nothing to integrate: %s tracks no remote branch", b.path)
	}
	before := b.revision(ctx, "HEAD")

	var err error
	if strategy == StrategyRebase {
		_, err = b.run(ctx, "rebase", req.Upstream)
	} else {
		_, err = b.run(ctx, "merge", "--no-edit", req.Upstream)
	}
	if err != nil {
		return b.integrateFailure(ctx, strategy, before, err)
	}
	after := b.revision(ctx, "HEAD")
	// A fast-forward is exactly the case where HEAD landed on the upstream:
	// there was no local commit to replay or to merge.
	return IntegrateResult{
		Strategy: strategy, Before: before, After: after,
		FastForward: after != "" && after == b.revision(ctx, req.Upstream),
	}, nil
}

// integrateFailure classifies a stopped rebase or merge. Conflicts are the
// expected case, and the operation is deliberately left in progress so that the
// user can resolve it or abort it (docs/06 section 12, failure 6).
func (b *systemBackend) integrateFailure(ctx context.Context, strategy Strategy, before string, err error) (IntegrateResult, error) {
	st, statusErr := b.SyncStatus(ctx)
	res := IntegrateResult{Strategy: strategy, Before: before}
	if statusErr == nil && len(st.Conflicted) > 0 {
		res.Conflicts, res.Operation = st.Conflicted, st.Operation
		return res, conflictError("integrate", strategy, st.Conflicted, b.path)
	}
	return res, &Error{
		Code: CodeIntegrateFailed, Op: "integrate", Err: err, Detail: outputOf(err),
		Message: "could not " + string(strategy) + " in " + b.path +
			": the repository was left as git found it, and no commit was lost",
	}
}

// Push publishes the current branch.
func (b *systemBackend) Push(ctx context.Context, req PushRequest) (PushResult, error) {
	st, err := b.SyncStatus(ctx)
	if err != nil {
		return PushResult{}, err
	}
	remote, branch, _ := b.target(st, req.Remote, req.Branch)
	if remote == "" {
		return PushResult{}, failf("push", CodeNoRemote, "%s has no git remote to push to", b.path)
	}
	args := []string{"push"}
	if req.DryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, remote, "HEAD:refs/heads/"+branch)
	if _, err := b.run(ctx, args...); err != nil {
		return PushResult{}, b.pushError(err, remote, branch)
	}
	return PushResult{Remote: remote, Branch: branch, Pushed: st.Ahead, UpToDate: st.Ahead == 0}, nil
}

// pushError classifies a refused push. A rejection is not a broken repository:
// every local commit is still there, which is what the message says.
func (b *systemBackend) pushError(err error, remote, branch string) *Error {
	output := outputOf(err)
	if classified := classifyTransport("push", err, output); classified != nil {
		return classified
	}
	text := strings.ToLower(output)
	switch {
	case containsAny(text, "protected branch", "pre-receive hook declined",
		"refusing to allow", "policy", "branch is read-only"):
		return &Error{
			Code: CodePushRejected, Op: "push", Err: err, Detail: output,
			Message: "the remote refused the push to " + remote + "/" + branch +
				" by policy (a protected branch or a server hook): your commits are safe locally; " +
				"switch this repository to user-branch mode or ask for permission to push",
		}
	case containsAny(text, "non-fast-forward", "fetch first", "rejected", "stale info"):
		return &Error{
			Code: CodePushRejected, Op: "push", Err: err, Detail: output,
			Message: "the remote already moved on, so the push to " + remote + "/" + branch +
				" was rejected: fetch and integrate again, then push; your commits are safe locally",
		}
	}
	return &Error{
		Code: CodePushFailed, Op: "push", Err: err, Detail: output,
		Message: "could not push to " + remote + "/" + branch +
			": your commits are safe locally and nothing was lost",
	}
}

// Abort undoes a half-finished rebase or merge.
func (b *systemBackend) Abort(ctx context.Context) error {
	op := b.operationInProgress(ctx)
	switch op {
	case OpRebase:
		if _, err := b.run(ctx, "rebase", "--abort"); err != nil {
			return wrap("abort", CodeIntegrateFailed, err, "abort the rebase in %s", b.path)
		}
	case OpMerge:
		if _, err := b.run(ctx, "merge", "--abort"); err != nil {
			return wrap("abort", CodeIntegrateFailed, err, "abort the merge in %s", b.path)
		}
	default:
		return failf("abort", CodeInProgress, "no rebase or merge is in progress in %s", b.path)
	}
	return nil
}

// Continue resumes a stopped rebase or merge. The editor is disabled so that a
// merge message never opens one in a background process.
func (b *systemBackend) Continue(ctx context.Context) (IntegrateResult, error) {
	st, err := b.SyncStatus(ctx)
	if err != nil {
		return IntegrateResult{}, err
	}
	if st.Operation == "" {
		return IntegrateResult{}, failf("continue", CodeInProgress,
			"no rebase or merge is in progress in %s", b.path)
	}
	if len(st.Conflicted) > 0 {
		return IntegrateResult{Conflicts: st.Conflicted, Operation: st.Operation},
			conflictError("continue", Strategy(st.Operation), st.Conflicted, b.path)
	}
	strategy := StrategyRebase
	if st.Operation == OpMerge {
		strategy = StrategyMerge
	}
	res := IntegrateResult{Strategy: strategy, Before: b.revision(ctx, "HEAD")}
	env := append(b.env(), "GIT_EDITOR=true")
	if _, err := b.runWith(ctx, env, "", st.Operation, "--continue"); err != nil {
		return b.integrateFailure(ctx, strategy, res.Before, err)
	}
	res.After = b.revision(ctx, "HEAD")
	return res, nil
}

// commitFieldSeparator is the unit separator; it cannot appear in a subject.
const commitFieldSeparator = "\x1f"

// Commits lists what To has and From does not, newest first.
func (b *systemBackend) Commits(ctx context.Context, req LogRequest) ([]Commit, error) {
	to := req.To
	if to == "" {
		to = "HEAD"
	}
	spec := to
	if req.From != "" {
		spec = req.From + ".." + to
	}
	args := []string{"log", "--format=%H" + commitFieldSeparator + "%s" +
		commitFieldSeparator + "%an <%ae>" + commitFieldSeparator + "%aI"}
	if req.Limit > 0 {
		args = append(args, "--max-count="+strconv.Itoa(req.Limit))
	}
	args = append(args, spec)
	raw, err := b.run(ctx, args...)
	if err != nil {
		return nil, wrap("log", CodeCommitFailed, err, "read the log of %s", b.path)
	}
	out := []Commit{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		fields := strings.Split(line, commitFieldSeparator)
		if len(fields) < 4 {
			continue
		}
		out = append(out, Commit{SHA: fields[0], Subject: fields[1], Author: fields[2], Date: fields[3]})
	}
	return out, nil
}

// statusPath unquotes and de-renames the path half of a porcelain line.
func statusPath(raw string) string {
	path := strings.TrimSpace(raw)
	if idx := strings.Index(path, " -> "); idx >= 0 {
		path = path[idx+4:]
	}
	return strings.Trim(path, `"`)
}
