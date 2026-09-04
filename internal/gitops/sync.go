package gitops

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// The sync pipeline of docs/06-git-sync.md section 4, story GIT-US-0021.
//
// `sync(repo)` is PREFLIGHT -> FETCH -> INTEGRATE -> PUSH, cancellable at every
// step and non-destructive at every failure. The preflight commit of what
// commit-on-save batched belongs to the caller (it owns the Committer); what
// lives here is everything that talks to git.

// Strategy is how remote work is integrated into the local branch.
type Strategy string

// The two integration strategies of `git.pullStrategy` (docs/06 section 4.3).
const (
	// StrategyRebase replays local commits on top of the remote branch. It is
	// the default because it keeps the history of a backlog linear.
	StrategyRebase Strategy = "rebase"
	// StrategyMerge merges the remote branch into the local one. It is what
	// teams with protected branches and the browser runtime use.
	StrategyMerge Strategy = "merge"
)

// Valid reports whether the strategy is one this build knows.
func (s Strategy) Valid() bool { return s == StrategyRebase || s == StrategyMerge }

// State is the headline status of a repository: the one word the sync panel
// shows next to a repository row.
type State string

// The states, in the precedence order resolveState applies.
const (
	// StateConflicted means an integration stopped with conflicted paths.
	StateConflicted State = "conflicted"
	// StateInProgress means a rebase or a merge is half-done; the user has to
	// continue it or abort it before anything else happens.
	StateInProgress State = "in_progress"
	// StateDetached means HEAD is not on a branch, so there is nothing to sync.
	StateDetached State = "detached"
	// StateNoRemote means the repository has no remote at all.
	StateNoRemote State = "no_remote"
	// StateNoUpstream means the branch tracks nothing yet.
	StateNoUpstream State = "no_upstream"
	// StateDiverged means both sides have commits the other does not.
	StateDiverged State = "diverged"
	// StateBehind means the remote has commits we do not.
	StateBehind State = "behind"
	// StateAhead means we have commits the remote does not.
	StateAhead State = "ahead"
	// StateDirty means the working tree has uncommitted changes and nothing
	// else to report.
	StateDirty State = "dirty"
	// StateUpToDate means there is nothing to do.
	StateUpToDate State = "up_to_date"
)

// Operation names a half-finished git operation.
const (
	// OpRebase is a rebase stopped in the middle, `.git/rebase-merge` present.
	OpRebase = "rebase"
	// OpMerge is a merge stopped in the middle, `.git/MERGE_HEAD` present.
	OpMerge = "merge"
)

// Conflict is one path an integration could not merge on its own. The
// structured resolution of these files is GIT-US-0022; what this story owes is
// naming them precisely and leaving the tree recoverable.
type Conflict struct {
	// Path is repository-relative and forward-slashed.
	Path string `json:"path"`
	// Kind is "content", "delete-modify", "add-add" or "unknown".
	Kind string `json:"kind"`
}

// The conflict kinds, from the porcelain status codes of a stopped merge.
const (
	ConflictContent      = "content"
	ConflictDeleteModify = "delete-modify"
	ConflictAddAdd       = "add-add"
	ConflictUnknown      = "unknown"
)

// Commit is one commit in a preview or a report. It carries no diff: the sync
// panel lists subjects, and the file-level detail is the git log surface.
type Commit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	Author  string `json:"author,omitempty"`
	Date    string `json:"date,omitempty"`
}

// SyncStatus is everything the status indicator needs for one repository.
type SyncStatus struct {
	// Branch is the checked-out branch, or "HEAD" when detached.
	Branch string `json:"branch"`
	// Detached reports a HEAD that is not on a branch.
	Detached bool `json:"detached"`
	// Clean reports an unmodified working tree.
	Clean bool `json:"clean"`
	// Dirty lists the uncommitted paths: staged, modified and untracked.
	Dirty []string `json:"dirty,omitempty"`
	// Tracked reports whether any of the dirty paths is tracked, which is what
	// blocks a rebase; untracked files never do.
	Tracked bool `json:"trackedChanges"`
	// Remote is the remote the branch syncs against, normally "origin".
	Remote string `json:"remote,omitempty"`
	// RemoteURL is that remote's URL with any credential removed.
	RemoteURL string `json:"remoteUrl,omitempty"`
	// Upstream is the remote-tracking branch, for example "origin/main".
	Upstream string `json:"upstream,omitempty"`
	// Ahead and Behind count the commits each side has that the other lacks.
	Ahead  int `json:"ahead"`
	Behind int `json:"behind"`
	// Conflicted lists the paths of a stopped integration.
	Conflicted []Conflict `json:"conflicted,omitempty"`
	// Operation is OpRebase or OpMerge when one is half-finished, else empty.
	Operation string `json:"operation,omitempty"`
	// State is the headline word for this repository.
	State State `json:"state"`
}

// resolveState fills State from the rest of the status, in a fixed precedence:
// a blocked repository is reported as blocked before it is reported as behind.
func (s *SyncStatus) resolveState() {
	s.Clean = len(s.Dirty) == 0
	switch {
	case len(s.Conflicted) > 0:
		s.State = StateConflicted
	case s.Operation != "":
		s.State = StateInProgress
	case s.Detached:
		s.State = StateDetached
	case s.Remote == "":
		s.State = StateNoRemote
	case s.Upstream == "":
		s.State = StateNoUpstream
	case s.Ahead > 0 && s.Behind > 0:
		s.State = StateDiverged
	case s.Behind > 0:
		s.State = StateBehind
	case s.Ahead > 0:
		s.State = StateAhead
	case !s.Clean:
		s.State = StateDirty
	default:
		s.State = StateUpToDate
	}
}

// FetchRequest asks for one fetch.
type FetchRequest struct {
	// Remote is the remote to fetch; empty means the branch's own remote.
	Remote string
	// Branch is the remote branch; empty means the branch's upstream.
	Branch string
	// Prune deletes remote-tracking refs the remote no longer has. It is off by
	// default (docs/06 section 4.1).
	Prune bool
}

// FetchResult is what a fetch brought in.
type FetchResult struct {
	Remote string `json:"remote"`
	// Upstream is the remote-tracking ref that was updated.
	Upstream string `json:"upstream"`
	// Before and After are that ref before and after the fetch.
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	// Updated reports whether the remote-tracking ref moved.
	Updated bool `json:"updated"`
}

// IntegrateRequest asks for one rebase or merge onto the upstream ref.
type IntegrateRequest struct {
	Strategy Strategy
	// Upstream is the ref to integrate, for example "origin/main".
	Upstream string
}

// IntegrateResult is what an integration did.
type IntegrateResult struct {
	Strategy Strategy `json:"strategy"`
	// FastForward reports that no local commit had to be replayed.
	FastForward bool `json:"fastForward"`
	// Before and After are HEAD around the integration.
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	// Conflicts is non-empty when the integration stopped. The repository is
	// then left mid-rebase or mid-merge on purpose: both are resumable, and
	// throwing the work away silently would be the destructive choice.
	Conflicts []Conflict `json:"conflicts,omitempty"`
	// Operation is what was left in progress, when Conflicts is non-empty.
	Operation string `json:"operation,omitempty"`
}

// PushRequest asks for one push of the current branch.
type PushRequest struct {
	Remote string
	// Branch is the remote branch to update; empty means the local name.
	Branch string
	// DryRun asks git what it would do without sending anything.
	DryRun bool
}

// PushResult is what a push did.
type PushResult struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
	// UpToDate reports that the remote already had everything.
	UpToDate bool `json:"upToDate"`
	// Pushed is how many commits were sent.
	Pushed int `json:"pushed"`
}

// LogRequest selects commits reachable from To but not from From. An empty From
// means "everything reachable from To", bounded by Limit.
type LogRequest struct {
	From  string
	To    string
	Limit int
}

// defaultPreviewLimit bounds a dry-run preview: a repository that is 4 000
// commits behind is summarized by its counters, not by 4 000 subjects.
const defaultPreviewLimit = 50

// Phase is where a sync run got to.
type Phase string

// The phases of the pipeline, as they appear in a `sync.progress` event
// (docs/07 section 5.6).
const (
	PhasePreflight Phase = "preflight"
	PhaseFetch     Phase = "fetch"
	PhaseIntegrate Phase = "integrate"
	PhasePush      Phase = "push"
	PhaseDone      Phase = "done"
	PhaseConflicts Phase = "conflicts"
	PhaseFailed    Phase = "failed"
)

// Progress is one step of a running sync, handed to SyncOptions.Progress.
type Progress struct {
	Repo    string `json:"repo"`
	Phase   Phase  `json:"phase"`
	Percent int    `json:"percent"`
	Message string `json:"message"`
	Ahead   int    `json:"ahead"`
	Behind  int    `json:"behind"`
}

// SyncOptions configures one Sync call.
type SyncOptions struct {
	// Repo is the caller's key for the repository; it is echoed in the result
	// and in every progress event.
	Repo string
	// Strategy is rebase or merge. Empty means StrategyRebase.
	Strategy Strategy
	// Push reports whether the run ends with a push (`git.pushOnSync`).
	Push bool
	// DryRun previews the run: it fetches, which is read-only, and then reports
	// what would happen. It never integrates, never pushes and never writes to
	// the working tree.
	DryRun bool
	// Remote and Branch override the branch's own upstream.
	Remote string
	Branch string
	// MaxPushRetries is how many times a non-fast-forward rejection is answered
	// with fetch + integrate + push. Zero means DefaultMaxPushRetries.
	MaxPushRetries int
	// Backoff is the wait before retry n (1-based). Nil means the documented
	// 0.5 s / 1.5 s / 4 s ladder; tests pass a zero function.
	Backoff func(attempt int) time.Duration
	// PreviewLimit bounds the incoming and outgoing commit lists.
	PreviewLimit int
	// Progress receives every phase change. It is called on the calling
	// goroutine, so it must not block.
	Progress func(Progress)
}

// DefaultMaxPushRetries is `git.maxPushRetries` (docs/06 section 4.2).
const DefaultMaxPushRetries = 3

// SyncResult is the report of one repository's sync.
type SyncResult struct {
	Repo     string   `json:"repo"`
	DryRun   bool     `json:"dryRun"`
	Strategy Strategy `json:"strategy"`
	// Phase is where the run ended: done, conflicts or failed.
	Phase Phase `json:"phase"`
	// Before and After are the status around the run. On a dry run After is
	// the status after the read-only fetch, so the counters are current.
	Before SyncStatus `json:"before"`
	After  SyncStatus `json:"after"`
	// Pulled and Pushed are the commit counts that moved.
	Pulled int `json:"pulled"`
	Pushed int `json:"pushed"`
	// Incoming and Outgoing are the previews the dry run is for; a real run
	// fills them too, from the state before it acted.
	Incoming []Commit `json:"incoming,omitempty"`
	Outgoing []Commit `json:"outgoing,omitempty"`
	// Conflicts names the paths that stopped the integration.
	Conflicts []Conflict `json:"conflicts,omitempty"`
	// Retries counts the push retries that were needed.
	Retries int `json:"retries"`
	// Warnings are the things the user should know that did not stop the run.
	Warnings []string `json:"warnings,omitempty"`
	// DurationMs is how long the run took.
	DurationMs int64 `json:"durationMs"`
	// Code and Message describe a failure; both are empty on success. The
	// message is written to say what to do next, because every failure of this
	// pipeline leaves a recoverable tree.
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// OK reports whether the run finished with nothing left to do.
func (r SyncResult) OK() bool { return r.Phase == PhaseDone }

// Sync runs the pipeline against one working tree.
//
// It returns a filled SyncResult in every case, including failure, so a caller
// can report what happened without inspecting the error; the error is the same
// failure for callers that prefer errors.Is/CodeOf.
//
// Nothing here is destructive. A fetch that fails leaves the tree untouched; an
// integration that conflicts leaves a resumable rebase or merge and says so; a
// rejected push leaves the local commits intact.
func Sync(ctx context.Context, b Backend, opts SyncOptions) (SyncResult, error) {
	started := time.Now()
	if opts.Strategy == "" {
		opts.Strategy = StrategyRebase
	}
	if !opts.Strategy.Valid() {
		return failedResult(opts, started, failf("sync", CodeUnsupported,
			"unknown integration strategy %q: use rebase or merge", string(opts.Strategy)))
	}
	if opts.MaxPushRetries <= 0 {
		opts.MaxPushRetries = DefaultMaxPushRetries
	}
	if opts.PreviewLimit <= 0 {
		opts.PreviewLimit = defaultPreviewLimit
	}
	if opts.Backoff == nil {
		opts.Backoff = defaultBackoff
	}

	run := &syncRun{backend: b, opts: opts, started: started}
	res, err := run.execute(ctx)
	res.DurationMs = time.Since(started).Milliseconds()
	return res, err
}

// syncRun is the state of one Sync call.
type syncRun struct {
	backend Backend
	opts    SyncOptions
	started time.Time
	result  SyncResult
}

// execute walks the state machine.
func (r *syncRun) execute(ctx context.Context) (SyncResult, error) {
	r.result = SyncResult{
		Repo:     r.opts.Repo,
		DryRun:   r.opts.DryRun,
		Strategy: r.opts.Strategy,
		Phase:    PhasePreflight,
	}

	before, err := r.preflight(ctx)
	if err != nil {
		return r.fail(err)
	}
	r.result.Before, r.result.After = before, before

	r.report(PhaseFetch, 20, "fetching "+before.Remote)
	if _, err := r.backend.Fetch(ctx, FetchRequest{Remote: r.opts.Remote, Branch: r.opts.Branch}); err != nil {
		return r.fail(err)
	}

	after, err := r.backend.SyncStatus(ctx)
	if err != nil {
		return r.fail(err)
	}
	r.result.After = after
	r.preview(ctx, after)

	if r.opts.DryRun {
		r.result.Phase = PhaseDone
		r.report(PhaseDone, 100, r.previewMessage(after))
		return r.result, nil
	}

	if after.Behind > 0 {
		if err := r.integrate(ctx, after); err != nil {
			return r.fail(err)
		}
	}
	if !r.opts.Push {
		r.result.Warnings = append(r.result.Warnings,
			"nothing was pushed: push on sync is off for this run")
		return r.finish(ctx)
	}
	if err := r.push(ctx); err != nil {
		return r.fail(err)
	}
	return r.finish(ctx)
}

// preflight reads the status and refuses the run when the repository is in a
// state where syncing would make things worse rather than better.
func (r *syncRun) preflight(ctx context.Context) (SyncStatus, error) {
	r.report(PhasePreflight, 5, "checking the working tree")
	st, err := r.backend.SyncStatus(ctx)
	if err != nil {
		return SyncStatus{}, err //nolint:wrapcheck // backend errors already carry a code and an actionable message
	}
	switch {
	case st.Operation != "":
		return st, failf("sync", CodeInProgress,
			"a %s is already in progress in %s: finish it or abort it before syncing "+
				`(POST /api/v1/sync/abort, or "git %s --abort")`,
			st.Operation, r.backend.Path(), st.Operation)
	case st.Detached:
		return st, failf("sync", CodeUnexpectedBranch,
			"%s has a detached HEAD: check out the branch you want to sync first; "+
				"we never switch branches for you",
			r.backend.Path())
	case st.Remote == "":
		return st, failf("sync", CodeNoRemote,
			"%s has no git remote: add one with `git remote add origin <url>`", r.backend.Path())
	case st.Upstream == "":
		return st, failf("sync", CodeNoUpstream,
			"branch %s of %s tracks no remote branch: push it once with "+
				"`git push -u %s %s`, then sync",
			st.Branch, r.backend.Path(), st.Remote, st.Branch)
	case !r.opts.DryRun && st.Tracked:
		return st, failf("sync", CodeDirtyTree,
			"%s has %d uncommitted change(s) to tracked files: commit them "+
				"(the sync panel's \"Commit changes\", or `gintrack sync --commit-all`) "+
				"or stash them, then sync; nothing was fetched",
			r.backend.Path(), len(st.Dirty))
	}
	return st, nil
}

// preview fills the incoming and outgoing commit lists, which is the whole
// point of a dry run and useful context in a real one.
func (r *syncRun) preview(ctx context.Context, st SyncStatus) {
	if st.Upstream == "" {
		return
	}
	if st.Behind > 0 {
		if in, err := r.backend.Commits(ctx, LogRequest{
			From: "HEAD", To: st.Upstream, Limit: r.opts.PreviewLimit,
		}); err == nil {
			r.result.Incoming = in
		}
	}
	if st.Ahead > 0 {
		if out, err := r.backend.Commits(ctx, LogRequest{
			From: st.Upstream, To: "HEAD", Limit: r.opts.PreviewLimit,
		}); err == nil {
			r.result.Outgoing = out
		}
	}
}

// previewMessage is the one-line summary of a dry run.
func (r *syncRun) previewMessage(st SyncStatus) string {
	if st.Ahead == 0 && st.Behind == 0 {
		return "up to date; nothing was changed (dry run)"
	}
	return fmt.Sprintf("would %s %d commit(s) and push %d; nothing was changed (dry run)",
		r.opts.Strategy, st.Behind, st.Ahead)
}

// integrate rebases or merges the fetched work into the local branch.
func (r *syncRun) integrate(ctx context.Context, st SyncStatus) error {
	r.report(PhaseIntegrate, 50, fmt.Sprintf("%s %d commit(s) onto %s",
		r.opts.Strategy, st.Behind, st.Upstream))
	res, err := r.backend.Integrate(ctx, IntegrateRequest{
		Strategy: r.opts.Strategy, Upstream: st.Upstream,
	})
	r.result.Conflicts = res.Conflicts
	if err != nil {
		return err //nolint:wrapcheck // backend errors already carry a code and an actionable message
	}
	r.result.Pulled += st.Behind
	return nil
}

// push publishes local commits, answering a non-fast-forward rejection with the
// documented fetch + integrate + push retry ladder (docs/06 section 4.2).
func (r *syncRun) push(ctx context.Context) error {
	for attempt := 0; ; attempt++ {
		st, err := r.backend.SyncStatus(ctx)
		if err != nil {
			return err //nolint:wrapcheck // backend errors already carry a code and an actionable message
		}
		r.result.After = st
		if st.Ahead == 0 {
			return nil
		}
		r.report(PhasePush, 80, fmt.Sprintf("pushing %d commit(s) to %s", st.Ahead, st.Remote))
		res, pushErr := r.backend.Push(ctx, PushRequest{Remote: r.opts.Remote, Branch: r.opts.Branch})
		if pushErr == nil {
			r.result.Pushed += res.Pushed
			return nil
		}
		if CodeOf(pushErr) != CodePushRejected || attempt >= r.opts.MaxPushRetries {
			return pushErr //nolint:wrapcheck // backend errors already carry a code and an actionable message
		}
		r.result.Retries = attempt + 1
		r.result.Warnings = append(r.result.Warnings, fmt.Sprintf(
			"push rejected (attempt %d of %d): someone else pushed first, retrying after a fetch",
			attempt+1, r.opts.MaxPushRetries))
		if err := sleep(ctx, r.opts.Backoff(attempt+1)); err != nil {
			return err
		}
		if _, err := r.backend.Fetch(ctx, FetchRequest{Remote: r.opts.Remote, Branch: r.opts.Branch}); err != nil {
			return err //nolint:wrapcheck // backend errors already carry a code and an actionable message
		}
		fresh, err := r.backend.SyncStatus(ctx)
		if err != nil {
			return err //nolint:wrapcheck // backend errors already carry a code and an actionable message
		}
		if fresh.Behind > 0 {
			if err := r.integrate(ctx, fresh); err != nil {
				return err
			}
		}
	}
}

// finish reads the final status and closes the run.
func (r *syncRun) finish(ctx context.Context) (SyncResult, error) {
	if st, err := r.backend.SyncStatus(ctx); err == nil {
		r.result.After = st
	}
	r.result.Phase = PhaseDone
	r.report(PhaseDone, 100, fmt.Sprintf("pulled %d, pushed %d", r.result.Pulled, r.result.Pushed))
	return r.result, nil
}

// fail closes the run on an error, classifying a conflict as its own phase: a
// stopped integration is not a broken repository, it is work waiting for a
// decision (GIT-US-0022).
func (r *syncRun) fail(err error) (SyncResult, error) {
	code := CodeOf(err)
	if code == "" {
		code = CodeSyncFailed
	}
	r.result.Code, r.result.Message = code, err.Error()
	r.result.Phase = PhaseFailed
	if code == CodeConflict {
		r.result.Phase = PhaseConflicts
	}
	r.report(r.result.Phase, 100, err.Error())
	return r.result, err
}

// failedResult builds a result for a failure that happened before the run
// started, so a caller still gets a report rather than only an error.
func failedResult(opts SyncOptions, started time.Time, err error) (SyncResult, error) {
	code := CodeOf(err)
	if code == "" {
		code = CodeSyncFailed
	}
	return SyncResult{
		Repo: opts.Repo, DryRun: opts.DryRun, Strategy: opts.Strategy,
		Phase: PhaseFailed, Code: code, Message: err.Error(),
		DurationMs: time.Since(started).Milliseconds(),
	}, err
}

// report emits a progress event.
func (r *syncRun) report(phase Phase, percent int, message string) {
	if r.opts.Progress == nil {
		return
	}
	r.opts.Progress(Progress{
		Repo: r.opts.Repo, Phase: phase, Percent: percent, Message: message,
		Ahead: r.result.After.Ahead, Behind: r.result.After.Behind,
	})
}

// defaultBackoff is the documented jitter-free ladder; the jitter is added by
// the caller that wants it, so tests stay deterministic.
func defaultBackoff(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 500 * time.Millisecond
	case 2:
		return 1500 * time.Millisecond
	default:
		return 4 * time.Second
	}
}

// sleep waits, or returns the context's error when the run was cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err() //nolint:wrapcheck // a cancelled context is reported verbatim
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return wrap("sync", CodeCancelled, ctx.Err(), "the sync was cancelled")
	case <-t.C:
		return nil
	}
}

// redactURL removes any credential a remote URL carries, so that a token pasted
// into `git remote add` never reaches a log, an event or the UI.
func redactURL(raw string) string {
	if raw == "" || !strings.Contains(raw, "://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.User == nil {
		return u.String()
	}
	// url.User escapes what it is given, so the placeholder is spliced in
	// rather than assigned: the point is a readable URL with no credential.
	u.User = nil
	rest, found := strings.CutPrefix(u.String(), u.Scheme+"://")
	if !found {
		return u.String()
	}
	return u.Scheme + "://***@" + rest
}

// classifyTransport maps what a transport failure printed onto a code, so the
// UI can offer the right next step: credentials, connectivity or a real bug.
// The text is matched case-insensitively because every backend words it
// differently.
func classifyTransport(op string, err error, output string) *Error {
	text := strings.ToLower(output + " " + err.Error())
	switch {
	case containsAny(text, "authentication required", "authentication failed",
		"could not read username", "invalid username or password", "403 forbidden",
		"401 unauthorized", "permission denied (publickey", "access denied",
		"terminal prompts disabled"):
		return &Error{
			Code: CodeAuthRequired, Op: op, Err: err, Detail: output,
			Message: "the git host refused the credentials for this remote: " +
				"store a token or key for it and try again (nothing was changed locally)",
		}
	case containsAny(text, "could not resolve host", "connection refused", "network is unreachable",
		"connection timed out", "temporary failure in name resolution", "no such host",
		"failed to connect", "operation timed out"):
		return &Error{
			Code: CodeNetwork, Op: op, Err: err, Detail: output,
			Message: "the git host is unreachable: your local work is safe and committed, " +
				"sync again when you are online",
		}
	case containsAny(text, "host key verification failed", "known_hosts"):
		return &Error{
			Code: CodeHostKey, Op: op, Err: err, Detail: output,
			Message: "the SSH host key of this remote is not trusted yet: accept it " +
				"once on the terminal (`ssh <host>`), then sync again",
		}
	}
	return nil
}

// containsAny reports whether text holds any of the needles.
func containsAny(text string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(text, n) {
			return true
		}
	}
	return false
}

// conflictKind maps a porcelain status pair onto a conflict kind.
func conflictKind(index, worktree byte) string {
	switch {
	case index == 'D' && worktree == 'U', index == 'U' && worktree == 'D':
		return ConflictDeleteModify
	case index == 'A' && worktree == 'A':
		return ConflictAddAdd
	case index == 'U' || worktree == 'U':
		return ConflictContent
	case index == 'D' && worktree == 'D':
		return ConflictDeleteModify
	}
	return ConflictUnknown
}

// conflictError builds the failure of a stopped integration. It names every
// path, because "there were conflicts" is not something a user can act on.
func conflictError(op string, strategy Strategy, conflicts []Conflict, path string) *Error {
	names := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		names = append(names, c.Path)
	}
	return &Error{
		Code: CodeConflict,
		Op:   op,
		Message: fmt.Sprintf(
			"the %s stopped on %d conflicted file(s) in %s: %s. "+
				"The %s is still in progress and nothing was pushed; resolve the files and "+
				"continue it, or abort it to get the tree back exactly as it was",
			strategy, len(conflicts), path, strings.Join(names, ", "), strategy),
	}
}
