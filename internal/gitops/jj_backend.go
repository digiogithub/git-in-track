package gitops

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The Jujutsu backend, story GIT-US-0040 (docs/06-git-sync.md section 14).
//
// This file is the read half: everything the status panel, the sync indicator,
// the conflict resolver and the metrics need, answered by jj itself rather than
// by the git repository jj commits into. The write half — Commit, Fetch,
// Integrate, Push, Undo, Resume, ResolvePath — is GIT-US-0041 and lives in
// jj_writes.go.
//
// Two rules govern every line here.
//
// First, jj is the source of truth. Git's HEAD sits at `@-`, its index is
// synchronized with `@`, and a non-colocated repository has no git working tree
// at all; none of the three can be corrected after the fact, so none of them is
// consulted. The one exception is the file history, which is read from the git
// object store jj commits into for the reasons ADR-023 records.
//
// Second, reading must not write. Almost every jj command snapshots the working
// copy before it runs, which creates a working-copy commit and an entry in the
// operation log. An indexer or a status poll that did that would rewrite the
// user's repository as a side effect of looking at it, so every invocation goes
// through jujutsuBackend.run, which passes `--ignore-working-copy`. The write
// half chooses per command: runWrite is what deliberately snapshots, and only
// the commands that have to see the disk use it.

// jujutsuFieldSeparator separates the fields of a jj template line. It is the
// ASCII unit separator, which cannot appear in a commit id, a bookmark name or
// a repository path, and it is the separator the git backends already use.
const jujutsuFieldSeparator = "\x1f"

// jujutsuGitRemote is the pseudo-remote a colocated repository always has: it
// is the local git repository jj exports to, not a host anything is published
// to, so it never counts as the remote of a line of work.
const jujutsuGitRemote = "git"

// jujutsuBackend drives a jj working copy by running the jj binary.
type jujutsuBackend struct {
	path    string
	bin     string
	version string
	info    core.VCSInfo
	// store is the git object store jj commits into, resolved from
	// `.jj/repo/store/git_target`. It is what History walks (ADR-023) and is
	// empty when it cannot be resolved, which makes History empty rather than
	// failing.
	store string
	opts  Options
}

// openJujutsu binds the jj backend to a jj working copy.
//
// It refuses a jj older than the documented floor instead of running commands
// whose output this build has never been verified against: a silently
// misparsed status is worse than a clear refusal.
func openJujutsu(path string, info core.VCSInfo, opts Options) (Backend, error) {
	bin, version, err := ResolveJujutsu(opts.JujutsuBinary)
	if err != nil {
		return nil, wrap("open", CodeJujutsuUnsupported, err,
			"%s is %s, and no usable jj binary is installed", path, info.Summary())
	}
	if JujutsuTooOld(version) {
		return nil, failf("open", CodeJujutsuTooOld,
			"jj %s drives %s, but this build is verified against jj %s or newer: upgrade jj",
			version, path, core.MinJujutsuVersion)
	}
	return &jujutsuBackend{
		path:    path,
		bin:     bin,
		version: version,
		info:    info,
		store:   jujutsuStore(path),
		opts:    opts,
	}, nil
}

// Name reports the backend name.
func (b *jujutsuBackend) Name() string { return string(KindJujutsu) }

// Path reports the working copy.
func (b *jujutsuBackend) Path() string { return b.path }

// VCSInfo reports what this working tree is managed with, which is what VCSOf
// reads.
func (b *jujutsuBackend) VCSInfo() core.VCSInfo { return b.info }

// Capabilities describes what this backend can do.
//
// Writes are on since GIT-US-0041: commit, fetch, integrate, push, undo and
// resolve all go through jj (jj_writes.go, ADR-024). What stays off is what jj
// genuinely does not do here — it runs no git hook, and this backend asks it
// for no signature; the read-only guard of GIT-US-0038, which is what drives a
// jj repository when no jj binary is installed, still reports Writes false.
func (b *jujutsuBackend) Capabilities() Capabilities {
	return Capabilities{
		Backend:   string(KindJujutsu),
		Version:   b.version,
		VCS:       string(core.VCSJujutsu),
		VCSLayout: string(b.info.Layout),
		// jj commits the working copy itself and takes a path list on its own
		// commit command, so a commit can be scoped exactly — the contract
		// ADR-022 states without naming git's index.
		ScopedCommit: true,
		Writes:       true,
		// jj shells out to the user's own git for every network operation
		// (`git.executable-path`), so the configured credential helpers, the
		// `insteadOf` rewrites and the ssh-agent all apply exactly as they do
		// for the system backend (docs/06 section 14.6).
		CredentialHelpers: true,
		// jj runs no git hook of its own, and this backend requests no
		// signature: signing is jj's `signing.behavior`, not ours to turn on.
		Hooks:   false,
		Signing: false,
	}
}

// Identity resolves the author from the overrides, then from jj's own
// configuration chain, which is where a jj user keeps it.
func (b *jujutsuBackend) Identity(ctx context.Context) (Identity, error) {
	if id := (Identity{Name: b.opts.AuthorName, Email: b.opts.AuthorEmail}); id.Valid() {
		return id, nil
	}
	name, _ := b.run(ctx, "config", "get", "user.name")
	email, _ := b.run(ctx, "config", "get", "user.email")
	id := Identity{Name: strings.TrimSpace(name), Email: strings.TrimSpace(email)}
	if !id.Valid() {
		return Identity{}, noIdentity("identity", b.path)
	}
	return id, nil
}

// Status reports the line of work and the dirty set of the working-copy commit.
//
// The dirty set is what `@` changed against its parent, which is jj's own
// answer to "what has not been recorded yet". Git's index is never consulted:
// jj keeps it synchronized with `@`, so it reports the whole working copy as
// staged work the user forgot about, which is exactly the misreading the epic
// exists to end.
//
// The three buckets of docs/06 section 14.3 map onto jj like this:
//
//   - Staged is always empty. jj has no staging area.
//   - Untracked holds the paths `@` adds, which no parent commit contains. It
//     is the closest true statement to git's "content no commit holds yet".
//   - Modified holds the paths `@` changes or removes.
func (b *jujutsuBackend) Status(ctx context.Context) (Status, error) {
	out := Status{Staged: []string{}, Modified: []string{}, Untracked: []string{}}
	line, err := b.line(ctx)
	if err != nil {
		return Status{}, err
	}
	out.Line = line

	changes, err := b.workingCopyChanges(ctx)
	if err != nil {
		return Status{}, err
	}
	for _, change := range changes {
		if change.added {
			out.Untracked = append(out.Untracked, change.path)
			continue
		}
		out.Modified = append(out.Modified, change.path)
	}
	out.Modified, out.Untracked = normalisePaths(out.Modified), normalisePaths(out.Untracked)
	out.Clean = len(out.Modified)+len(out.Untracked) == 0
	return out, nil
}

// SyncStatus reports the line of work, the dirty set, the tracked remote
// bookmark and the counters against it.
//
// Its State is the truthful one. The `jujutsu` placeholder of GIT-US-0038 said
// only "this is a jj repository", because a git backend could compute nothing
// else here; with jj answering, up_to_date, ahead, behind, diverged, dirty and
// conflicted all mean what they say. The `jujutsu` flag is still set, so the
// UI keeps rendering the repository as jj-managed, but it is set *after*
// resolveState so that it no longer overrides the headline.
func (b *jujutsuBackend) SyncStatus(ctx context.Context) (SyncStatus, error) {
	out := SyncStatus{}
	line, err := b.line(ctx)
	if err != nil {
		return SyncStatus{}, err
	}
	out.Line = line

	changes, err := b.workingCopyChanges(ctx)
	if err != nil {
		return SyncStatus{}, err
	}
	dirty := make([]string, 0, len(changes))
	for _, change := range changes {
		dirty = append(dirty, change.path)
		if !change.added {
			out.Tracked = true
		}
	}
	out.Dirty = normalisePaths(dirty)

	conflicted, err := b.conflicts(ctx)
	if err != nil {
		return SyncStatus{}, err
	}
	out.Conflicted = conflicted

	if err := b.fillRemote(ctx, &out); err != nil {
		return SyncStatus{}, err
	}

	// A jj operation is never half-finished: `jj rebase` always completes and
	// what is unsettled is recorded inside the resulting commits. There is
	// therefore nothing to carry forward, and `jj undo` is what takes an
	// operation back.
	out.Integration = Integration{Undo: UndoOperationLog, Resume: ResumeNone}
	out.resolveState()
	out.Jujutsu = true
	return out, nil
}

// fillRemote resolves the remote of the current bookmark, its URL, the
// remote-tracking bookmark and the ahead/behind counters against it.
func (b *jujutsuBackend) fillRemote(ctx context.Context, out *SyncStatus) error {
	bookmarks, err := b.bookmarks(ctx)
	if err != nil {
		return err
	}
	remote := remoteOfBookmark(bookmarks, out.PushTarget)
	if remote == "" {
		// No tracked remote bookmark. A repository that has a remote at all is
		// reported as tracking nothing rather than as having no remote, which
		// is the same distinction git draws.
		remote = b.singleRemote(ctx)
		if remote != "" {
			out.Remote = remote
			out.RemoteURL = redactURL(b.remoteURL(ctx, remote))
		}
		return nil
	}
	out.Remote = remote
	out.RemoteURL = redactURL(b.remoteURL(ctx, remote))
	// The published API spells a remote-tracking ref git's way, `origin/main`;
	// jj spells the same thing `main@origin`, which is what the revsets below
	// use. The wire format is frozen (ADR-022), so the translation happens here.
	out.Upstream = remote + "/" + out.PushTarget

	// The local side is resolved to a commit id rather than named. A fetch that
	// moves the remote bookmark while the local one has moved too makes jj
	// report the *name* as conflicted, and every revset that mentions it then
	// fails with "Name `main` is conflicted" — which is exactly the state the
	// sync preflight meets, and the one in which the counters matter most.
	local, tracking := b.lineCommit(ctx, out.PushTarget), jujutsuRemoteSymbol(out.PushTarget, remote)
	if local == "" {
		return nil
	}
	ahead, err := b.countRevisions(ctx, tracking+".."+local)
	if err != nil {
		return err
	}
	behind, err := b.countRevisions(ctx, local+".."+tracking)
	if err != nil {
		return err
	}
	out.Ahead, out.Behind = ahead, behind
	return nil
}

// line resolves the current line of work: the bookmark that publishing `@`
// would move, or `@` itself when no bookmark is reachable from it.
//
// A bookmark does not move when the working-copy commit does, so the line of
// work is the nearest bookmark among the ancestors of `@`, which is exactly
// what `heads(::@ & bookmarks())` selects. When several are equally near, a
// bookmark that is tracked on a remote wins — it is the one a publish would
// update — and the tie after that is broken by name, so two calls in a row
// never disagree.
func (b *jujutsuBackend) line(ctx context.Context) (Line, error) {
	raw, err := b.run(ctx, "log", "--no-graph",
		"-T", `local_bookmarks.map(|bm| bm.name()).join("`+jujutsuFieldSeparator+`") ++ "\n"`,
		"-r", "heads(::@ & bookmarks())")
	if err != nil {
		return Line{}, wrap("status", CodeCommitFailed, err,
			"read the bookmarks of %s", b.path)
	}
	var names []string
	for _, line := range strings.Split(raw, "\n") {
		for _, name := range strings.Split(strings.TrimSpace(line), jujutsuFieldSeparator) {
			if name != "" {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return jujutsuLine(), nil
	}
	sort.Strings(names)
	chosen := names[0]
	if bookmarks, listErr := b.bookmarks(ctx); listErr == nil {
		for _, name := range names {
			if remoteOfBookmark(bookmarks, name) != "" {
				chosen = name
				break
			}
		}
	}
	return Line{Name: chosen, Kind: LineBookmark, PushTarget: chosen}, nil
}

// lineCommit resolves the commit the line of work would publish: the commit the
// named bookmark points at among the ancestors of `@`, or `@` itself when there
// is no bookmark.
//
// It is deliberately a revset over `::@` rather than a lookup of the name.
// A bookmark jj considers conflicted has two positions, and only one of them is
// an ancestor of the working copy — the local one, which is what everything
// this backend reports is about.
func (b *jujutsuBackend) lineCommit(ctx context.Context, name string) string {
	if name != "" && name != JujutsuWorkingCopy {
		if sha := b.commitOf(ctx, "heads(::@ & bookmarks(exact:"+jujutsuSymbol(name)+"))"); sha != "" {
			return sha
		}
	}
	return b.commitOf(ctx, "@")
}

// commitOf resolves a revset to a single commit id, or the empty string when it
// selects nothing or cannot be parsed.
func (b *jujutsuBackend) commitOf(ctx context.Context, revset string) string {
	raw, err := b.run(ctx, "log", "--no-graph", "-T", `commit_id ++ "\n"`, "-r", revset)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(raw, "\n") {
		if id := strings.TrimSpace(line); id != "" {
			return id
		}
	}
	return ""
}

// jujutsuChange is one entry of the working-copy diff summary.
type jujutsuChange struct {
	path string
	// added reports a path the parent commit does not hold.
	added bool
}

// workingCopyChanges reads what `@` changed against its parent.
func (b *jujutsuBackend) workingCopyChanges(ctx context.Context) ([]jujutsuChange, error) {
	raw, err := b.run(ctx, "diff", "--summary", "-r", "@")
	if err != nil {
		return nil, wrap("status", CodeCommitFailed, err,
			"read the working-copy changes of %s", b.path)
	}
	out := []jujutsuChange{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 || line[1] != ' ' {
			continue
		}
		path := summaryPath(line[2:])
		if path == "" {
			continue
		}
		out = append(out, jujutsuChange{path: path, added: line[0] == 'A'})
	}
	return out, nil
}

// summaryPath unwraps the path of a diff summary line. A rename is written
// `dir/{old.md => new.md}`, and the path that matters is the new one.
func summaryPath(raw string) string {
	path := strings.TrimSpace(raw)
	open := strings.Index(path, "{")
	arrow := strings.Index(path, " => ")
	closing := strings.LastIndex(path, "}")
	if open >= 0 && arrow > open && closing > arrow {
		path = path[:open] + path[arrow+4:closing] + path[closing+1:]
	}
	return strings.TrimSpace(path)
}

// jujutsuBookmark is one row of `jj bookmark list`: a local bookmark, or the
// same bookmark as one remote holds it.
type jujutsuBookmark struct {
	name    string
	remote  string
	tracked bool
}

// bookmarks lists every bookmark, local and remote, with its tracking state.
func (b *jujutsuBackend) bookmarks(ctx context.Context) ([]jujutsuBookmark, error) {
	raw, err := b.run(ctx, "bookmark", "list", "--all-remotes",
		"-T", `name ++ "`+jujutsuFieldSeparator+`" ++ if(remote, remote, "") ++`+
			` "`+jujutsuFieldSeparator+`" ++ if(tracked, "1", "0") ++ "\n"`)
	if err != nil {
		return nil, wrap("status", CodeCommitFailed, err, "list the bookmarks of %s", b.path)
	}
	out := []jujutsuBookmark{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Split(strings.TrimSpace(line), jujutsuFieldSeparator)
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		out = append(out, jujutsuBookmark{name: fields[0], remote: fields[1], tracked: fields[2] == "1"})
	}
	return out, nil
}

// remoteOfBookmark reports the remote a bookmark is tracked on, preferring
// origin when it is tracked on several. The `git` pseudo-remote of a colocated
// repository is never one of them: it is the local git repository jj exports
// to, not a host.
func remoteOfBookmark(bookmarks []jujutsuBookmark, name string) string {
	if name == "" {
		return ""
	}
	candidates := []string{}
	for _, bm := range bookmarks {
		if bm.name != name || !bm.tracked || bm.remote == "" || bm.remote == jujutsuGitRemote {
			continue
		}
		if bm.remote == "origin" {
			return bm.remote
		}
		candidates = append(candidates, bm.remote)
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Strings(candidates)
	return candidates[0]
}

// singleRemote reports the remote of a repository that has exactly one, or
// origin when it has several. It is what fills the remote of a line of work
// that tracks nothing yet.
func (b *jujutsuBackend) singleRemote(ctx context.Context) string {
	raw, err := b.run(ctx, "git", "remote", "list")
	if err != nil {
		return ""
	}
	names := []string{}
	for _, line := range strings.Split(raw, "\n") {
		name, _, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || name == "" || name == jujutsuGitRemote {
			continue
		}
		if name == "origin" {
			return name
		}
		names = append(names, name)
	}
	if len(names) == 1 {
		return names[0]
	}
	return ""
}

// remoteURL reads one remote's URL. It is the raw configured value, credential
// and all, so every caller redacts it before it reaches a message or the UI.
func (b *jujutsuBackend) remoteURL(ctx context.Context, remote string) string {
	if remote == "" {
		return ""
	}
	raw, err := b.run(ctx, "git", "remote", "list")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(raw, "\n") {
		name, url, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && name == remote {
			return strings.TrimSpace(url)
		}
	}
	return ""
}

// countRevisions counts the commits a revset selects.
func (b *jujutsuBackend) countRevisions(ctx context.Context, revset string) (int, error) {
	raw, err := b.run(ctx, "log", "--no-graph", "-T", `"\n"`, "-r", revset)
	if err != nil {
		return 0, wrap("status", CodeCommitFailed, err,
			"count the revisions of %s in %s", revset, b.path)
	}
	// The template emits exactly one newline per selected commit.
	return strings.Count(raw, "\n"), nil
}

// conflicts lists the paths the working-copy commit records a conflict for.
//
// `jj resolve --list` fails when there is none, so the commit is asked first;
// treating "no conflicts" as an error would turn every clean status into one.
func (b *jujutsuBackend) conflicts(ctx context.Context) ([]Conflict, error) {
	flag, err := b.run(ctx, "log", "--no-graph",
		"-T", `if(conflict, "1", "0")`, "-r", "@")
	if err != nil {
		return nil, wrap("status", CodeCommitFailed, err,
			"read the conflict state of %s", b.path)
	}
	if strings.TrimSpace(flag) != "1" {
		return nil, nil
	}
	raw, err := b.run(ctx, "resolve", "--list")
	if err != nil {
		return nil, wrap("status", CodeCommitFailed, err,
			"list the conflicted paths of %s", b.path)
	}
	out := []Conflict{}
	for _, line := range strings.Split(raw, "\n") {
		path, description := splitConflictLine(line)
		if path == "" {
			continue
		}
		out = append(out, Conflict{Path: path, Kind: jujutsuConflictKind(description)})
	}
	return out, nil
}

// splitConflictLine splits `docs/a.md    2-sided conflict` into its path and
// its description. jj pads the column with at least two spaces, and a
// description never contains a double space, so the last run of them is the
// separator even when the path itself holds single spaces.
func splitConflictLine(line string) (path, description string) {
	line = strings.TrimRight(strings.TrimRight(line, "\r"), " ")
	if strings.TrimSpace(line) == "" {
		return "", ""
	}
	cut := -1
	for i := 0; i+1 < len(line); i++ {
		if line[i] == ' ' && line[i+1] == ' ' {
			cut = i
		}
	}
	if cut < 0 {
		return strings.TrimSpace(line), ""
	}
	return strings.TrimSpace(line[:cut]), strings.TrimSpace(line[cut:])
}

// jujutsuConflictKind classifies a conflict from the sentence jj describes it
// with, in the vocabulary the resolver of GIT-US-0022 already speaks.
func jujutsuConflictKind(description string) string {
	text := strings.ToLower(description)
	switch {
	case strings.Contains(text, "deletion"):
		return ConflictDeleteModify
	case strings.Contains(text, "conflict"):
		return ConflictContent
	default:
		return ConflictUnknown
	}
}

// Commits lists what To has and From does not, newest first.
//
// The callers speak git: they pass `HEAD` and `origin/main`, because that is
// what SyncStatus.Upstream carries. Those are translated into jj's `@` and
// `main@origin` here rather than leaking git's spelling into a revset.
func (b *jujutsuBackend) Commits(ctx context.Context, req LogRequest) ([]Commit, error) {
	to := b.revsetOf(ctx, req.To)
	if to == "" {
		to = "@"
	}
	revset := to
	if from := b.revsetOf(ctx, req.From); from != "" {
		revset = from + ".." + to
	}
	args := []string{"log", "--no-graph", "-r", revset + " ~ root()",
		"-T", `commit_id ++ "` + jujutsuFieldSeparator + `" ++ description.first_line() ++ "` +
			jujutsuFieldSeparator + `" ++ author.name() ++ " <" ++ author.email() ++ ">" ++ "` +
			jujutsuFieldSeparator + `" ++ author.timestamp().utc().format("%Y-%m-%dT%H:%M:%S%:z") ++ "\n"`}
	if req.Limit > 0 {
		args = append(args, "--limit", itoa(req.Limit))
	}
	raw, err := b.run(ctx, args...)
	if err != nil {
		return nil, wrap("log", CodeCommitFailed, err, "read the log of %s", b.path)
	}
	out := []Commit{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), jujutsuFieldSeparator)
		if len(fields) < 4 || fields[0] == "" {
			continue
		}
		out = append(out, Commit{SHA: fields[0], Subject: fields[1], Author: fields[2], Date: fields[3]})
	}
	return out, nil
}

// revsetOf translates one git-shaped ref into a jj revset symbol. `HEAD` is the
// working copy, `origin/main` is a remote bookmark, and anything else is passed
// through as a quoted symbol so that a bookmark named `feat/x` cannot be
// mistaken for a remote-tracking ref.
func (b *jujutsuBackend) revsetOf(ctx context.Context, ref string) string {
	ref = strings.TrimSpace(ref)
	switch ref {
	case "":
		return ""
	case "HEAD", "@":
		return "@"
	}
	remote, name, ok := strings.Cut(ref, "/")
	if ok && name != "" && b.hasRemote(ctx, remote) {
		return jujutsuRemoteSymbol(name, remote)
	}
	return jujutsuSymbol(ref)
}

// hasRemote reports whether the repository has a git remote of that name.
func (b *jujutsuBackend) hasRemote(ctx context.Context, remote string) bool {
	raw, err := b.run(ctx, "git", "remote", "list")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(raw, "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		if name == remote {
			return true
		}
	}
	return false
}

// jujutsuSymbol quotes a name so that a revset cannot reinterpret the slashes
// and dots a bookmark name is allowed to hold.
func jujutsuSymbol(name string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
}

// jujutsuRemoteSymbol quotes a remote-tracking bookmark, jj's `main@origin`.
func jujutsuRemoteSymbol(name, remote string) string {
	return jujutsuSymbol(name) + "@" + jujutsuSymbol(remote)
}

// run executes jj in the working copy and returns its standard output.
//
// Every invocation of this backend goes through here, and every one of them
// carries `--ignore-working-copy`. That is not an optimization: without it jj
// snapshots the working copy first, which writes a new working-copy commit and
// an entry in the operation log. A status poll or an indexer that did that
// would rewrite the user's repository as a side effect of reading it.
func (b *jujutsuBackend) run(ctx context.Context, args ...string) (string, error) {
	return b.exec(ctx, nil, append([]string{"--ignore-working-copy"}, args...))
}

// runWrite executes jj and lets it snapshot the working copy first.
//
// It is the deliberate opposite of run, and only the write half of GIT-US-0041
// uses it: a command that has to see what is on disk — commit, rebase, squash,
// undo — must snapshot, and one that only moves refs must not. Nothing that
// serves a read may call it.
func (b *jujutsuBackend) runWrite(ctx context.Context, args ...string) (string, error) {
	return b.exec(ctx, nil, args)
}

// runWriteAs is runWrite with the commit author pinned. jj resolves the author
// from its own configuration chain, and JJ_USER/JJ_EMAIL are how that chain is
// overridden for one invocation, which is what CommitRequest.Author and the
// `git.authorName`/`git.authorEmail` settings ask for.
func (b *jujutsuBackend) runWriteAs(ctx context.Context, id Identity, args ...string) (string, error) {
	if !id.Valid() {
		return b.exec(ctx, nil, args)
	}
	return b.exec(ctx, []string{"JJ_USER=" + id.Name, "JJ_EMAIL=" + id.Email}, args)
}

// exec runs one jj command in the working copy and returns its standard output.
func (b *jujutsuBackend) exec(ctx context.Context, env, args []string) (string, error) {
	full := append([]string{"--color=never", "--no-pager"}, args...)
	cmd := exec.CommandContext(ctx, b.bin, full...) //nolint:gosec // b.bin comes from LookPath, args are built here
	cmd.Dir = b.path
	// The non-interactive environment of GIT-US-0023 applies unchanged: jj runs
	// the user's own git for every network operation, so GIT_TERMINAL_PROMPT,
	// the blanked askpass helpers and ssh's batch mode reach the process that
	// would otherwise open a prompt nobody is watching.
	cmd.Env = append(nonInteractiveEnv(os.Environ()), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), &commandError{
			bin:    "jj",
			args:   full,
			output: redactSecrets(strings.TrimSpace(stderr.String() + "\n" + stdout.String())),
			err:    err,
		}
	}
	return stdout.String(), nil
}
