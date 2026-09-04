package gitops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Kind selects the git implementation a repository is driven with
// (docs/06-git-sync.md section 7.1, docs/07-cli-and-api.md section 3.4).
type Kind string

// The three settings `git.backend` accepts.
const (
	// KindAuto uses the system git binary when a usable one is on PATH and
	// go-git otherwise. It is the default.
	KindAuto Kind = "auto"
	// KindGoGit is the pure-Go implementation: no external dependency, no
	// hooks, no signing.
	KindGoGit Kind = "go-git"
	// KindSystem shells out to the git executable, which is what inherits the
	// user's credential helpers, hooks and signing setup.
	KindSystem Kind = "system"
)

// Valid reports whether the kind is one this build knows.
func (k Kind) Valid() bool { return k == KindAuto || k == KindGoGit || k == KindSystem }

// MinSystemGit is the oldest system git this build drives. Older binaries lack
// `git commit --pathspec-from-file` and predate the plumbing the sync pipeline
// of GIT-US-0021 needs, so `auto` falls back to go-git instead of using them.
var MinSystemGit = [2]int{2, 20}

// Identity is the author and committer of a commit. It is read from the git
// configuration chain and never invented (docs/06 section 3.3).
type Identity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Valid reports whether both halves are present.
func (i Identity) Valid() bool { return i.Name != "" && i.Email != "" }

// String renders the identity the way git logs it.
func (i Identity) String() string { return i.Name + " <" + i.Email + ">" }

// Capabilities is what the current backend can actually do. The UI hides what a
// mode cannot do instead of failing at the last step (ADR-006).
type Capabilities struct {
	// Backend is the resolved backend name, "go-git" or "system".
	Backend string `json:"backend"`
	// Version is the system git version, empty for go-git.
	Version string `json:"version,omitempty"`
	// Hooks reports whether committing runs the repository's hooks.
	Hooks bool `json:"hooks"`
	// Signing reports whether commits can be signed with the user's gpg or ssh
	// setup.
	Signing bool `json:"signing"`
	// CredentialHelpers reports whether the user's configured credential
	// helpers are used. It matters for GIT-US-0021 and GIT-US-0023.
	CredentialHelpers bool `json:"credentialHelpers"`
	// PathspecCommit reports whether a commit can be limited to a pathspec
	// regardless of what else is staged in the index.
	PathspecCommit bool `json:"pathspecCommit"`
}

// Status is the part of `git status` the UI needs. Ahead and Behind are filled
// by the sync pipeline of GIT-US-0021; commit-on-save only needs the branch and
// the dirty set.
type Status struct {
	Branch    string   `json:"branch"`
	Detached  bool     `json:"detached"`
	Clean     bool     `json:"clean"`
	Staged    []string `json:"staged"`
	Modified  []string `json:"modified"`
	Untracked []string `json:"untracked"`
}

// Message is a commit message split into the subject line and the body that
// carries the machine-readable trailers (docs/06 section 3.3).
type Message struct {
	Subject string `json:"subject"`
	Body    string `json:"body,omitempty"`
}

// String renders the message the way git stores it.
func (m Message) String() string {
	if m.Body == "" {
		return m.Subject
	}
	return m.Subject + "\n\n" + m.Body
}

// CommitRequest is one commit: the paths to stage and the message to write.
type CommitRequest struct {
	// Paths are repository-relative, forward-slashed. A path that no longer
	// exists stages its deletion. Only these paths are staged (AC 5).
	Paths []string
	// Message is the rendered message.
	Message Message
	// Author overrides the identity resolved from the git configuration. Both
	// halves must be set for the override to apply.
	Author Identity
	// Sign asks for a signed commit. It is honored by the system backend only
	// and reported as unsupported by go-git.
	Sign bool
	// AllowEmpty commits even when the staged paths changed nothing.
	AllowEmpty bool
}

// CommitResult is what a commit produced.
type CommitResult struct {
	// SHA is the new commit, empty when Empty is true.
	SHA string `json:"sha,omitempty"`
	// Empty reports that nothing had changed, so no commit was made. It is a
	// success: a no-op write must not raise an error.
	Empty bool `json:"empty"`
	// Subject is the subject line that was written.
	Subject string `json:"subject,omitempty"`
	// Author is the identity the commit was made with.
	Author Identity `json:"author"`
	// Paths are the paths that were staged.
	Paths []string `json:"paths,omitempty"`
}

// Backend is the one git abstraction of the companion process. Each instance is
// bound to one working tree, which is what the caller has: a mounted repository
// (docs/07 section 6.4).
//
// The sync half of the interface (SyncStatus, Fetch, Integrate, Push, Abort,
// Commits) landed with GIT-US-0021, and the structured conflict surface —
// reading the base/ours/theirs blobs of a conflicted path and continuing the
// integration from a resolution — with GIT-US-0022.
type Backend interface {
	// Name is "go-git" or "system".
	Name() string
	// Path is the absolute path of the working tree.
	Path() string
	// Capabilities describes what this backend can do.
	Capabilities() Capabilities
	// Identity resolves the author from the configuration chain, or fails with
	// CodeNoIdentity.
	Identity(ctx context.Context) (Identity, error)
	// Status reports the branch and the dirty set.
	Status(ctx context.Context) (Status, error)
	// Commit stages exactly req.Paths and commits them. It never touches the
	// working tree, so a failed commit loses nothing (AC 7).
	Commit(ctx context.Context, req CommitRequest) (CommitResult, error)

	// SyncStatus reports everything the status indicator needs: the branch, the
	// dirty set, the remote, the ahead/behind counters, any conflicted path and
	// any half-finished rebase or merge.
	SyncStatus(ctx context.Context) (SyncStatus, error)
	// Fetch downloads the remote branch. It updates no working file, so a
	// failure here is always non-destructive.
	Fetch(ctx context.Context, req FetchRequest) (FetchResult, error)
	// Integrate rebases or merges req.Upstream into the current branch. An
	// integration that conflicts fails with CodeConflict and leaves the
	// operation in progress, which is the recoverable state Abort undoes.
	Integrate(ctx context.Context, req IntegrateRequest) (IntegrateResult, error)
	// Push publishes the current branch. A non-fast-forward rejection fails
	// with CodePushRejected and leaves every local commit intact.
	Push(ctx context.Context, req PushRequest) (PushResult, error)
	// Abort undoes a half-finished rebase or merge, restoring the tree to what
	// it was before the integration started.
	Abort(ctx context.Context) error
	// Continue resumes a half-finished rebase or merge once its conflicted
	// paths have been resolved and staged. It fails with CodeConflict while
	// any path is still unmerged.
	Continue(ctx context.Context) (IntegrateResult, error)
	// Commits lists the commits reachable from req.To but not from req.From,
	// newest first. It is what a dry-run preview is made of.
	Commits(ctx context.Context, req LogRequest) ([]Commit, error)

	// ConflictFile reads the three versions of a conflicted path — the merge
	// base, ours and theirs — out of the index stages a stopped integration
	// left behind. It is what the resolver of GIT-US-0022 is built on.
	ConflictFile(ctx context.Context, path string) (ConflictVersions, error)
	// ResolvePath writes one path's resolution, stages it and, once nothing is
	// left conflicted, continues the rebase or merge. Abort stays available
	// throughout: a resolution that goes wrong is still recoverable.
	ResolvePath(ctx context.Context, req ResolveRequest) (ResolveResult, error)
}

// Options configures Open.
type Options struct {
	// Backend selects the implementation. Empty means KindAuto.
	Backend Kind
	// GitBinary overrides the executable the system backend runs. Empty means
	// the `git` on PATH.
	GitBinary string
	// AuthorName and AuthorEmail override the git configuration when both are
	// set (docs/06 section 3.3).
	AuthorName  string
	AuthorEmail string
	// Now is the clock stamping commits. Nil means time.Now.
	Now func() time.Time
}

// Open binds a backend to the working tree at path.
//
// A path that is not inside a git working tree fails with CodeNotARepository,
// which the caller reports as "commit-on-save is off for this repository"
// rather than as a broken mount.
func Open(path string, opts Options) (Backend, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, wrap("open", CodeNotARepository, err, "%s is not a usable path", path)
	}
	kind := opts.Backend
	if kind == "" {
		kind = KindAuto
	}
	if !kind.Valid() {
		return nil, failf("open", CodeUnsupported,
			"unknown git backend %q: use auto, go-git or system", string(kind))
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	switch kind {
	case KindSystem:
		return openSystem(abs, opts)
	case KindGoGit:
		return openGoGit(abs, opts)
	case KindAuto:
		if _, _, err := resolveGit(opts.GitBinary); err == nil {
			if b, sysErr := openSystem(abs, opts); sysErr == nil {
				return b, nil
			}
		}
		return openGoGit(abs, opts)
	}
	return nil, failf("open", CodeUnsupported, "unknown git backend %q", string(kind))
}

// Resolve reports which backend `auto` would pick and why, without opening a
// repository. `gintrack doctor` and GET /api/v1/capabilities use it.
func Resolve(kind Kind, binary string) (name, version string) {
	if kind == "" {
		kind = KindAuto
	}
	if kind == KindGoGit {
		return string(KindGoGit), ""
	}
	_, v, err := resolveGit(binary)
	if err != nil {
		if kind == KindSystem {
			return string(KindSystem), ""
		}
		return string(KindGoGit), ""
	}
	return string(KindSystem), v
}

// resolveGit locates a usable git binary and reports its version.
func resolveGit(binary string) (path, version string, err error) {
	if binary == "" {
		binary = "git"
	}
	path, err = exec.LookPath(binary)
	if err != nil {
		return "", "", fmt.Errorf("no git executable on PATH: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output() //nolint:gosec // path comes from LookPath or explicit configuration
	if err != nil {
		return "", "", fmt.Errorf("run %s --version: %w", path, err)
	}
	version = parseGitVersion(string(out))
	if version == "" {
		return "", "", errors.New("could not parse the git version")
	}
	if !atLeast(version, MinSystemGit) {
		return "", "", fmt.Errorf("git %s is older than the required %d.%d",
			version, MinSystemGit[0], MinSystemGit[1])
	}
	return path, version, nil
}

// parseGitVersion extracts "2.45.2" from "git version 2.45.2\n".
func parseGitVersion(out string) string {
	fields := strings.Fields(strings.TrimSpace(out))
	for _, f := range fields {
		if f != "" && f[0] >= '0' && f[0] <= '9' {
			return f
		}
	}
	return ""
}

// atLeast compares a dotted version against a major/minor floor.
func atLeast(version string, floor [2]int) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, minor := atoi(parts[0]), atoi(parts[1])
	if major != floor[0] {
		return major > floor[0]
	}
	return minor >= floor[1]
}

// atoi parses the leading digits of a version component, ignoring suffixes such
// as the "windows" in "2.45.2.windows.1".
func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// normalisePaths sorts, de-duplicates and cleans a path list so that two calls
// with the same files stage the same pathspec.
func normalisePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		clean := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(p)), "./")
		if clean == "" || clean == "." || seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	sortStrings(out)
	return out
}

// sortStrings sorts in place; it exists so the file has no sort import spread
// through it.
func sortStrings(in []string) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j] < in[j-1]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}
