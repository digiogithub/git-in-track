package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The write half of the Jujutsu backend, story GIT-US-0041
// (docs/06-git-sync.md section 14, ADR-024).
//
// Every write here is a jj command, never a git one. The epic's audit found
// that a git write behind a jj workspace lands on `@-`, moves no bookmark and
// is abandoned as an orphan by the next jj command; this file is the answer to
// that, and two properties of jj shape all of it.
//
// The working copy is a commit. There is no index, so "record exactly these
// paths" is `jj commit -m … -- <paths>`: the named paths of `@` become a new
// commit and everything else stays in a fresh working-copy commit on top.
// Nothing is orphaned and nothing outside the request is captured.
//
// A bookmark does not move when a commit is made. `jj commit` on its own would
// therefore leave every commit on save unreachable from any bookmark, and
// `jj git push` would never publish it — the exact outcome this epic exists to
// prevent. Each commit is followed by a fast-forward `jj bookmark move`, which
// is what a jj user does by hand and which jj itself refuses when it would not
// be a fast-forward.
//
// Snapshotting is deliberate here, where the read half of GIT-US-0040 forbids
// it. A command that has to see what is on disk — commit, rebase, resolve,
// undo — runs through runWrite and lets jj snapshot the working copy first; one
// that only moves refs — fetch, push, bookmark move — keeps
// `--ignore-working-copy`, so a background sync never turns the user's
// un-snapshotted edits into a working-copy commit behind their back.

// jujutsuMergeSubject is the subject of the merge commit the merge strategy
// creates. jj has no `git merge`, so the merge is a commit with two parents.
const jujutsuMergeSubject = "Merge "

// Commit records exactly req.Paths as one commit and fast-forwards the bookmark
// of the current line of work onto it.
//
// `jj commit -- <paths>` is the whole of the scoping: jj splits the
// working-copy commit, keeping the named paths in the commit it creates and
// leaving every other change in the new working copy. That is the contract of
// CommitRequest.Paths without an index anywhere in sight.
func (b *jujutsuBackend) Commit(ctx context.Context, req CommitRequest) (CommitResult, error) {
	if req.Sign {
		return CommitResult{}, failf("commit", CodeUnsupported,
			"the jj backend does not sign commits: configure `signing.behavior` in jj itself, "+
				"or turn signing off for %s", b.path)
	}
	identity, err := b.Identity(ctx)
	if err != nil {
		return CommitResult{}, err
	}
	if req.Author.Valid() {
		identity = req.Author
	}
	paths := normalisePaths(req.Paths)
	out := CommitResult{Author: identity, Subject: req.Message.Subject, Paths: paths}
	if len(paths) == 0 && !req.AllowEmpty {
		out.Empty = true
		return out, nil
	}

	// The snapshot happens here, before anything is decided: what the commit
	// covers has to be what is on disk, not what jj last recorded.
	changed, err := b.changedPaths(ctx, paths)
	if err != nil {
		return CommitResult{}, err
	}
	if len(changed) == 0 && !req.AllowEmpty {
		out.Empty = true
		return out, nil
	}

	// The line of work is read before the commit, because `jj commit` makes the
	// working copy a child of the new commit and the bookmark stays behind.
	line, err := b.line(ctx)
	if err != nil {
		return CommitResult{}, err
	}

	args := []string{"commit", "-m", req.Message.String()}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	if _, err := b.runWriteAs(ctx, identity, args...); err != nil {
		return CommitResult{}, b.commitFailure(err)
	}

	// `jj commit` leaves the new commit as the parent of the working copy.
	sha := b.commitOf(ctx, "@-")
	out.SHA = sha
	if err := b.advanceBookmark(ctx, line.PushTarget, sha); err != nil {
		return out, err
	}
	return out, nil
}

// changedPaths reports which of the requested paths `@` actually changes
// against its parent. It is what keeps an unchanged save from recording an
// empty commit: `jj commit -- <paths>` would happily create one, with only a
// warning on standard error.
//
// An empty path list means the whole working copy, which is what AllowEmpty
// callers ask for.
func (b *jujutsuBackend) changedPaths(ctx context.Context, paths []string) ([]string, error) {
	args := append([]string{"diff", "--summary", "-r", "@"}, pathArgs(paths)...)
	raw, err := b.runWrite(ctx, args...)
	if err != nil {
		return nil, wrap("commit", CodeCommitFailed, err,
			"read what the working copy of %s changed", b.path)
	}
	out := []string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 || line[1] != ' ' {
			continue
		}
		if path := summaryPath(line[2:]); path != "" {
			out = append(out, path)
		}
	}
	return out, nil
}

// pathArgs renders a path list as the `-- a b c` tail of a jj command, or as
// nothing at all when the list is empty.
func pathArgs(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	return append([]string{"--"}, paths...)
}

// commitFailure classifies a refused `jj commit`. jj runs no repository hook,
// so there is no hook failure to report; what is left is an immutable
// destination or a plain failure, and in both cases nothing was recorded.
func (b *jujutsuBackend) commitFailure(err error) *Error {
	output := outputOf(err)
	if containsAny(strings.ToLower(output), "immutable") {
		return &Error{
			Code: CodeCommitFailed, Op: "commit", Err: err, Detail: redactSecrets(output),
			Message: "jj refuses to rewrite an immutable commit in " + b.path +
				": nothing was recorded, and your working copy is untouched",
		}
	}
	return &Error{
		Code: CodeCommitFailed, Op: "commit", Err: err, Detail: redactSecrets(output),
		Message: "could not record the commit in " + b.path +
			": nothing was recorded, and your working copy is untouched",
	}
}

// advanceBookmark fast-forwards a bookmark onto a commit.
//
// This is the step that makes commit on save publishable. `jj bookmark move`
// refuses to move a bookmark backwards or sideways unless it is told to, and it
// is never told to: the bookmark this moves was chosen as an ancestor of `@`,
// so the move is always a fast-forward, and a refusal means the repository is
// not what we read a moment ago.
func (b *jujutsuBackend) advanceBookmark(ctx context.Context, name, to string) error {
	if name == "" || name == JujutsuWorkingCopy || to == "" {
		// Nothing to move: the line of work is an unnamed working copy, which
		// is a legitimate jj state. The commit is still reachable from `@`;
		// it simply has no bookmark to publish it under yet.
		return nil
	}
	if _, err := b.run(ctx, "bookmark", "move", jujutsuSymbol(name), "--to", to); err != nil {
		return &Error{
			Code: CodeCommitFailed, Op: "commit", Err: err, Detail: redactSecrets(outputOf(err)),
			Message: "the commit " + short(to) + " was recorded in " + b.path +
				", but the bookmark " + name + " could not be moved onto it, so " +
				"`jj git push` would not publish it: run `jj bookmark move " + name +
				" --to " + short(to) + "` and check what moved the bookmark meanwhile",
		}
	}
	return nil
}

// short renders a commit id the way jj's own output does.
func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// Fetch brings the remote bookmarks in with `jj git fetch`.
//
// It moves refs only, so it runs with `--ignore-working-copy`: a background
// sync must not turn the user's un-snapshotted edits into a working-copy commit
// as a side effect of talking to the network.
func (b *jujutsuBackend) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	st, err := b.SyncStatus(ctx)
	if err != nil {
		return FetchResult{}, err
	}
	remote, target, upstream := b.target(st, req.Remote, req.Branch)
	if remote == "" {
		return FetchResult{}, failf("fetch", CodeNoRemote,
			"%s has no git remote to fetch from: add one with `jj git remote add origin <url>`",
			b.path)
	}
	before := b.trackingCommit(ctx, target, remote)

	args := []string{"git", "fetch", "--remote", remote}
	if req.Branch != "" {
		args = append(args, "-b", req.Branch)
	}
	if _, err := b.run(ctx, args...); err != nil {
		return FetchResult{}, b.transportFailure(ctx, "fetch", remote, err, CodeFetchFailed,
			"could not fetch "+remote+" in "+b.path+" (nothing was changed locally)")
	}
	after := b.trackingCommit(ctx, target, remote)
	return FetchResult{
		Remote: remote, Upstream: upstream,
		Before: before, After: after, Updated: before != after,
	}, nil
}

// target resolves the remote, the bookmark and the remote-tracking name one
// request acts on, honoring the caller's overrides. It is the jj twin of the
// system backend's helper of the same name.
func (b *jujutsuBackend) target(st SyncStatus, remote, name string) (resolvedRemote, resolvedName, upstreamRef string) {
	if remote == "" {
		remote = st.Remote
	}
	if name == "" {
		name = st.PushTarget
	}
	upstream := st.Upstream
	if upstream == "" && remote != "" && name != "" {
		upstream = remote + "/" + name
	}
	return remote, name, upstream
}

// trackingCommit resolves the commit a remote bookmark points at, or the empty
// string when the remote does not have it yet.
func (b *jujutsuBackend) trackingCommit(ctx context.Context, name, remote string) string {
	if name == "" || remote == "" {
		return ""
	}
	return b.commitOf(ctx, jujutsuRemoteSymbol(name, remote))
}

// Integrate brings the fetched work into the current line of work.
//
// jj has no `git merge` and no half-finished rebase. `jj rebase -b @ -d
// <upstream>` replays every local commit that the upstream does not already
// hold — the bookmark's own commit included, because bookmarks follow the
// commits they name — and the merge strategy builds the merge commit with
// `jj new` and moves the bookmark onto it. Either way the operation completes:
// a conflict is *recorded inside* the resulting commits, which is what
// Integration.Resume being ResumeNone has always said.
func (b *jujutsuBackend) Integrate(ctx context.Context, req IntegrateRequest) (IntegrateResult, error) {
	strategy := req.Strategy
	if strategy == "" {
		strategy = StrategyRebase
	}
	if !strategy.Valid() {
		return IntegrateResult{}, failf("integrate", CodeUnsupported,
			"unknown integration strategy %q: use rebase or merge", string(strategy))
	}
	st, err := b.SyncStatus(ctx)
	if err != nil {
		return IntegrateResult{}, err
	}
	upstream := b.upstreamRevset(ctx, req.Upstream, st)
	if upstream == "" {
		return IntegrateResult{}, failf("integrate", CodeNoUpstream,
			"nothing to integrate: %s tracks no remote bookmark", b.path)
	}
	res := IntegrateResult{
		Strategy: strategy,
		Before:   b.lineCommit(ctx, st.PushTarget),
		// A line of work with nothing of its own is carried straight onto the
		// upstream, which is jj's equivalent of a fast-forward.
		FastForward: st.Ahead == 0,
	}

	destination := upstream
	if strategy == StrategyMerge {
		merged, mergeErr := b.mergeCommit(ctx, st, upstream)
		if mergeErr != nil {
			return res, mergeErr
		}
		destination = merged
	}
	if _, err := b.runWrite(ctx, "rebase", "-b", "@", "-d", destination); err != nil {
		return res, &Error{
			Code: CodeIntegrateFailed, Op: "integrate", Err: err,
			Detail:  redactSecrets(outputOf(err)),
			Message: "could not " + string(strategy) + " onto " + req.Upstream + " in " + b.path + ": " + core.JujutsuUndoCommand + " takes the operation back and no commit was lost",
		}
	}
	if strategy == StrategyMerge {
		if err := b.advanceBookmark(ctx, st.PushTarget, destination); err != nil {
			return res, err
		}
	}

	after, err := b.SyncStatus(ctx)
	if err != nil {
		return res, err
	}
	res.After = b.lineCommit(ctx, after.PushTarget)
	if len(after.Conflicted) > 0 {
		res.Conflicts, res.Integration = after.Conflicted, after.Integration
		return res, jujutsuConflictError("integrate", strategy, after.Conflicted, b.path)
	}
	return res, nil
}

// mergeCommit builds the merge commit of the merge strategy: a commit whose
// parents are the head of the local line of work and the upstream. `jj new
// --no-edit` creates it without moving the working copy, which is then rebased
// onto it by the caller, so the user keeps the working-copy commit they had.
func (b *jujutsuBackend) mergeCommit(ctx context.Context, st SyncStatus, upstream string) (string, error) {
	local := b.lineCommit(ctx, st.PushTarget)
	if local == "" {
		return "", failf("integrate", CodeNoUpstream,
			"nothing to merge into in %s: the working copy has no commit of its own", b.path)
	}
	subject := jujutsuMergeSubject + st.Upstream
	if st.PushTarget != "" {
		subject += " into " + st.PushTarget
	}
	if _, err := b.runWrite(ctx, "new", "--no-edit", "-m", subject, local, upstream); err != nil {
		return "", &Error{
			Code: CodeIntegrateFailed, Op: "integrate", Err: err,
			Detail:  redactSecrets(outputOf(err)),
			Message: "could not create the merge commit in " + b.path + ": nothing was changed",
		}
	}
	// The merge is the only child of both parents that jj just created, which
	// is what `heads(children(local) & children(upstream))` names exactly.
	merged := b.commitOf(ctx, "heads(children("+local+") & children("+upstream+"))")
	if merged == "" {
		return "", failf("integrate", CodeIntegrateFailed,
			"the merge commit could not be located in %s after it was created: "+
				"`%s` takes the operation back", b.path, core.JujutsuUndoCommand)
	}
	return merged, nil
}

// upstreamRevset translates the git-shaped upstream the pipeline passes
// ("origin/main") into the jj revset that names it ("main"@"origin"), falling
// back to the status's own upstream.
func (b *jujutsuBackend) upstreamRevset(ctx context.Context, upstream string, st SyncStatus) string {
	if upstream == "" {
		upstream = st.Upstream
	}
	if upstream == "" {
		return ""
	}
	return b.revsetOf(ctx, upstream)
}

// jujutsuConflictError is the failure of an integration that recorded
// conflicts. It differs from git's in the only way that matters to the reader:
// there is no half-finished operation to continue or abort, the rebase
// completed, and the conflicts are inside the commits it produced.
func jujutsuConflictError(op string, strategy Strategy, conflicts []Conflict, path string) *Error {
	names := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		names = append(names, c.Path)
	}
	return &Error{
		Code: CodeConflict,
		Op:   op,
		Message: "the " + string(strategy) + " recorded conflicts in " + itoa(len(conflicts)) +
			" file(s) in " + path + ": " + strings.Join(names, ", ") +
			". The operation itself completed and jj keeps the conflicts inside the commits, " +
			"so nothing is half-finished and nothing was pushed: resolve each file, or run `" +
			core.JujutsuUndoCommand + "` to take the whole operation back",
	}
}

// Push publishes the bookmark of the current line of work.
//
// It moves refs only, so like Fetch it keeps `--ignore-working-copy`: what is
// published is what jj has recorded, and a push never snapshots the disk.
func (b *jujutsuBackend) Push(ctx context.Context, req PushRequest) (PushResult, error) {
	st, err := b.SyncStatus(ctx)
	if err != nil {
		return PushResult{}, err
	}
	remote, name, _ := b.target(st, req.Remote, req.Target)
	if remote == "" {
		return PushResult{}, failf("push", CodeNoRemote,
			"%s has no git remote to push to: add one with `jj git remote add origin <url>`",
			b.path)
	}
	if name == "" || name == JujutsuWorkingCopy {
		return PushResult{}, failf("push", CodeNoUpstream,
			"%s has no bookmark to publish: jj pushes bookmarks, not working copies, so "+
				"create one with `jj bookmark create <name> -r @-` and sync again",
			b.path)
	}
	if st.Ahead == 0 && !req.DryRun {
		return PushResult{Remote: remote, Target: name, UpToDate: true}, nil
	}

	args := []string{"git", "push", "--remote", remote, "-b", jujutsuSymbol(name)}
	if req.DryRun {
		args = append(args, "--dry-run")
	}
	if _, err := b.run(ctx, args...); err != nil {
		return PushResult{}, b.pushFailure(ctx, err, remote, name)
	}
	return PushResult{Remote: remote, Target: name, Pushed: st.Ahead, UpToDate: st.Ahead == 0}, nil
}

// pushFailure classifies a refused push. jj shells out to the user's own git
// for the network, so the transport failures read exactly as the system
// backend's; what is jj's own is the wording of a bookmark the remote moved
// under us, which is the case the sync retry ladder answers with a fetch.
func (b *jujutsuBackend) pushFailure(ctx context.Context, err error, remote, name string) *Error {
	output := outputOf(err)
	tc := transportContext{Op: "push", Path: b.path, Remote: remote, URL: b.remoteURL(ctx, remote)}
	if classified := classifyTransport(tc, err, output); classified != nil {
		return classified
	}
	text := strings.ToLower(output)
	switch {
	case containsAny(text, "protected branch", "pre-receive hook declined",
		"refusing to allow", "branch is read-only"):
		return &Error{
			Code: CodePushRejected, Op: "push", Err: err, Detail: redactSecrets(output),
			Message: "the remote refused the push of the bookmark " + name + " to " + remote +
				" by policy (a protected branch or a server hook): your commits are safe locally; " +
				"switch this repository to user-branch mode or ask for permission to push",
		}
	case containsAny(text, "unexpectedly moved on the remote", "stale info",
		"non-fast-forward", "fetch first", "failed to push some bookmarks", "rejected"):
		return &Error{
			Code: CodePushRejected, Op: "push", Err: err, Detail: redactSecrets(output),
			Message: "the remote already moved on, so the push of the bookmark " + name + " to " +
				remote + " was rejected: fetch and integrate again, then push; your commits are " +
				"safe locally",
		}
	}
	return &Error{
		Code: CodePushFailed, Op: "push", Err: err, Detail: redactSecrets(output),
		Message: "could not push the bookmark " + name + " to " + remote +
			": your commits are safe locally and nothing was lost",
	}
}

// transportFailure classifies a network failure of a jj command, falling back
// to the caller's message. jj runs the user's own git for the network, so
// classifyTransport recognizes what it prints without any translation.
func (b *jujutsuBackend) transportFailure(ctx context.Context, op, remote string, err error, code, message string) *Error {
	output := outputOf(err)
	tc := transportContext{Op: op, Path: b.path, Remote: remote, URL: b.remoteURL(ctx, remote)}
	if classified := classifyTransport(tc, err, output); classified != nil {
		return classified
	}
	return &Error{Code: code, Op: op, Err: err, Detail: redactSecrets(output), Message: message}
}

// Undo takes the last jj operation back, which is what Integration.Undo
// announces as UndoOperationLog.
//
// It is stronger than git's `--abort`: jj has an operation log rather than a
// half-finished state, so there does not have to be anything unfinished for
// undo to have something to take back. `jj op restore` is what the same log
// offers for going further back, and it is the command the message names.
func (b *jujutsuBackend) Undo(ctx context.Context) error {
	if _, err := b.runWrite(ctx, "undo"); err != nil {
		return &Error{
			Code: CodeIntegrateFailed, Op: "abort", Err: err, Detail: redactSecrets(outputOf(err)),
			Message: "could not undo the last operation in " + b.path +
				": inspect `jj op log` and restore the operation you want with " +
				"`jj op restore <id>`; nothing was lost",
		}
	}
	return nil
}

// Resume has nothing to carry forward, and says so.
//
// jj never leaves an unfinished operation: a rebase completes and records what
// it could not merge inside the resulting commits. That is exactly what
// Integration.Resume being ResumeNone announces, and faking a `--continue` here
// would be a lie the caller would build on.
func (b *jujutsuBackend) Resume(_ context.Context) (IntegrateResult, error) {
	return IntegrateResult{}, failf("continue", CodeUnsupported,
		"there is nothing to continue in %s: a jj operation always completes and the "+
			"conflicts it could not merge are recorded inside the commits, so resolving "+
			"the last file finishes the job", b.path)
}

// ResolvePath records one path's resolution where jj keeps the conflict.
//
// The resolver of GIT-US-0022 hands us a merged file, so `jj resolve` — which
// drives an interactive merge tool — is not what records it. The resolution is
// written into the working copy, snapshotted, and then squashed into the
// conflicted commit, which is the workflow jj itself prints as a hint when a
// rebase records a conflict. That is what makes the bookmark's own commit
// conflict-free again, and therefore pushable.
func (b *jujutsuBackend) ResolvePath(ctx context.Context, req ResolveRequest) (ResolveResult, error) {
	path := filepath.ToSlash(strings.TrimSpace(req.Path))
	if path == "" {
		return ResolveResult{}, failf("resolve", CodeNotFound, "no path was given")
	}
	before, err := b.SyncStatus(ctx)
	if err != nil {
		return ResolveResult{}, err
	}
	if _, ok := conflictOf(before.Conflicted, path); !ok {
		return ResolveResult{}, notConflicted("resolve", path, b.path)
	}

	// The conflicted ancestor is read before the working copy changes, because
	// squashing into it is what actually resolves the conflict jj recorded.
	target := b.conflictedAncestor(ctx)
	out := ResolveResult{Path: path}
	if err := b.writeResolution(ctx, path, req); err != nil {
		return out, err
	}
	out.Staged = true

	if target != "" {
		if _, err := b.runWrite(ctx, append(
			[]string{"squash", "--from", "@", "--into", target}, pathArgs([]string{path})...)...); err != nil {
			return out, &Error{
				Code: CodeCommitFailed, Op: "resolve", Err: err, Detail: redactSecrets(outputOf(err)),
				Message: "the resolution of " + path + " was written into the working copy of " +
					b.path + " but could not be squashed into the conflicted commit " +
					short(target) + ": run `jj squash --into " + short(target) + " -- " + path +
					"` yourself, or `" + core.JujutsuUndoCommand + "` to take it back",
			}
		}
	}

	after, err := b.SyncStatus(ctx)
	if err != nil {
		return out, err
	}
	out.Remaining, out.Status = after.Conflicted, after
	// Continue is a no-op in a VCS whose Integration.Resume is ResumeNone: the
	// integration completed when it ran, and resolving the last file is the
	// whole of finishing it. Saying so is what keeps the caller honest.
	if req.Continue && len(after.Conflicted) == 0 {
		out.Continued = true
		out.Result = &IntegrateResult{
			Strategy:    StrategyRebase,
			After:       b.lineCommit(ctx, after.PushTarget),
			Integration: Integration{Undo: UndoOperationLog, Resume: ResumeNone},
		}
	}
	return out, nil
}

// conflictedAncestor reports the nearest commit at or below `@-` that still
// records a conflict, which is where a resolution has to land. It is the empty
// string when the conflict lives in the working-copy commit alone, where
// writing the file is the whole of the resolution.
func (b *jujutsuBackend) conflictedAncestor(ctx context.Context) string {
	return b.commitOf(ctx, "heads(::@- & conflicts())")
}

// writeResolution puts the resolved content on disk and lets jj snapshot it.
// The file is written first and snapshotted second, so a failure at either step
// leaves work the user can still see.
func (b *jujutsuBackend) writeResolution(ctx context.Context, path string, req ResolveRequest) error {
	full := filepath.Join(b.path, filepath.FromSlash(path))
	if req.Delete {
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return wrap("resolve", CodeCommitFailed, err, "remove %s in %s", path, b.path)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil { //nolint:gosec,mnd // repository folders keep the modes the editor uses
			return wrap("resolve", CodeCommitFailed, err, "create the folder of %s in %s", path, b.path)
		}
		if err := os.WriteFile(full, []byte(req.Content), 0o644); err != nil { //nolint:gosec,mnd // a resolved file is a working-tree file like any other
			return wrap("resolve", CodeCommitFailed, err, "write %s in %s", path, b.path)
		}
	}
	// jj has no `add`: the snapshot *is* the staging, and it happens on the
	// next command that is allowed to take one.
	if _, err := b.runWrite(ctx, "status"); err != nil {
		return wrap("resolve", CodeCommitFailed, err,
			"record the resolution of %s in %s", path, b.path)
	}
	return nil
}
