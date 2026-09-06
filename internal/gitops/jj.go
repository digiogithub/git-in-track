package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// Jujutsu support, story GIT-US-0038 (docs/06-git-sync.md section 14).
//
// A Jujutsu repository stores its commits in a git repository, so every git
// *read* this package does — log, history, diff — is correct. Every git
// *write* is not: git's HEAD sits at `@-`, the parent of the working-copy
// commit, so a commit lands there, moves no bookmark, and is re-parented and
// abandoned as an orphan by the next jj command. This file detects such a
// repository and wraps its backend in a guard that refuses every write and
// stops the status from describing jj's steady state as a failure.
//
// The jj backend itself is GIT-US-0040/GIT-US-0041. Until it lands, a jj
// repository is read-only to this product, and it says so.

// JujutsuWorkingCopy is what the guard reports as the branch of a jj
// repository. Git answers the literal "HEAD" there, because the working copy is
// a commit and not a branch; `@` is how jj itself names it, and it is honest
// where "HEAD"/detached is not.
const JujutsuWorkingCopy = "@"

// MinJujutsu is the oldest jj this product is verified against. It is parsed
// from core.MinJujutsuVersion so the documented minimum and the enforced one
// cannot drift.
var MinJujutsu = [2]int{0, 41}

// jjDirName is the marker folder of a jj workspace.
const jjDirName = ".jj"

// DetectVCS reports which version-control system manages a folder.
//
// A jj workspace is recognized by its `.jj` marker, and its layout by where the
// git store it commits into lives:
//
//   - colocated: the store is the workspace's own `.git`, so the folder is also
//     a git working tree and git reads work against it;
//   - internal: the store lives inside `.jj` (the `.jj/repo/store/git` of older
//     jj releases), so the folder is not a git working tree at all.
//
// The `.jj` marker wins over `.git`: a colocated repository has both, and
// calling it git is exactly the misreading this story exists to end.
func DetectVCS(repoPath string) core.VCSInfo {
	gitDir := exists(filepath.Join(repoPath, ".git"))
	if !exists(filepath.Join(repoPath, jjDirName)) {
		if gitDir {
			return core.VCSInfo{Kind: core.VCSGit, GitDir: true}
		}
		return core.VCSInfo{Kind: core.VCSNone}
	}
	info := core.VCSInfo{Kind: core.VCSJujutsu, Layout: core.LayoutInternal, GitDir: gitDir}
	if jujutsuStore(repoPath) == canonicalPath(filepath.Join(repoPath, ".git")) && gitDir {
		info.Layout = core.LayoutColocated
	}
	return info
}

// jujutsuStore resolves the git repository a jj workspace commits into, or the
// empty string when it cannot be read. The `git_target` file is relative to the
// store folder that holds it; `.jj/repo` is itself a file in a secondary
// workspace, holding the path of the repository the workspace shares.
func jujutsuStore(repoPath string) string {
	repoDir := filepath.Join(repoPath, jjDirName, "repo")
	if data, err := os.ReadFile(repoDir); err == nil {
		// A secondary workspace: `.jj/repo` is a file naming the shared repo.
		shared := strings.TrimSpace(string(data))
		if shared == "" {
			return ""
		}
		if !filepath.IsAbs(shared) {
			shared = filepath.Join(repoPath, jjDirName, shared)
		}
		repoDir = shared
	}
	storeDir := filepath.Join(repoDir, "store")
	target, err := os.ReadFile(filepath.Join(storeDir, "git_target"))
	if err != nil {
		// No `git_target`: the legacy layout keeps the store inline.
		if exists(filepath.Join(storeDir, "git")) {
			return canonicalPath(filepath.Join(storeDir, "git"))
		}
		return ""
	}
	path := strings.TrimSpace(string(target))
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(storeDir, path)
	}
	return canonicalPath(path)
}

// canonicalPath cleans a path and resolves the symlinks it can, so that two
// spellings of the same folder compare equal.
func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

// exists reports whether a path is there at all, file or folder.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// ResolveJujutsu locates the jj binary and reports its version, the way
// resolveGit does for git. `binary` overrides the executable; empty means the
// `jj` on PATH.
//
// It runs `jj --version` and nothing else. Every other jj invocation would
// snapshot the working copy, which is a write, and this story writes nothing.
func ResolveJujutsu(binary string) (path, version string, err error) {
	if binary == "" {
		binary = "jj"
	}
	path, err = exec.LookPath(binary)
	if err != nil {
		return "", "", fmt.Errorf("no jj executable on PATH: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output() //nolint:gosec // path comes from LookPath or explicit configuration
	if err != nil {
		return "", "", fmt.Errorf("run %s --version: %w", path, err)
	}
	version = parseGitVersion(string(out))
	if version == "" {
		return "", "", errors.New("could not parse the jj version")
	}
	return path, version, nil
}

// JujutsuTooOld reports whether a resolved jj version is older than MinJujutsu.
// It is a warning, never a refusal: the product runs no jj command yet, so an
// older binary changes nothing but what we can promise.
func JujutsuTooOld(version string) bool {
	return version != "" && !atLeast(version, MinJujutsu)
}

// vcsAware is the optional interface the guard implements. It is deliberately
// not part of Backend: generalizing that interface is GIT-US-0039, and this
// story adds no method to it.
type vcsAware interface {
	VCSInfo() core.VCSInfo
}

// VCSOf reports what an open backend's working tree is managed with. A backend
// that answers nothing is a plain git working tree, which is the only thing
// this package could open before jj existed.
func VCSOf(b Backend) core.VCSInfo {
	if b == nil {
		return core.VCSInfo{Kind: core.VCSNone}
	}
	if aware, ok := b.(vcsAware); ok {
		return aware.VCSInfo()
	}
	return core.VCSInfo{Kind: core.VCSGit, GitDir: true}
}

// refuseJujutsu builds the refusal of one git write.
func refuseJujutsu(op, path, command string) *Error {
	return &Error{
		Code:    CodeJujutsuWriteRefused,
		Op:      op,
		Message: core.JujutsuRefusal(op, path, command),
	}
}

// jujutsuGuard wraps the backend of a colocated jj repository. Reads pass
// straight through to the embedded backend; writes are refused before a single
// git process is started, and the two statuses are corrected.
type jujutsuGuard struct {
	Backend
	info core.VCSInfo
}

// guardJujutsu wraps a backend when its working tree is a jj repository, and
// returns it untouched otherwise.
func guardJujutsu(b Backend, info core.VCSInfo) Backend {
	if !info.IsJujutsu() {
		return b
	}
	return &jujutsuGuard{Backend: b, info: info}
}

// VCSInfo reports what the guarded working tree is managed with.
func (g *jujutsuGuard) VCSInfo() core.VCSInfo { return g.info }

// Capabilities reports the underlying backend's capabilities with the write
// half turned off, so a UI hides what it cannot offer instead of failing at the
// last step (ADR-006).
func (g *jujutsuGuard) Capabilities() Capabilities {
	caps := g.Backend.Capabilities()
	caps.VCS = string(core.VCSJujutsu)
	caps.VCSLayout = string(g.info.Layout)
	caps.Writes = false
	caps.Signing = false
	return caps
}

// Status corrects what git reports about a jj working copy.
//
// Git answers "HEAD" for the branch and calls itself detached, because jj's
// working copy is a commit rather than a branch; that is the steady state, not
// a problem. jj also keeps git's index synchronized with `@` while HEAD is at
// `@-`, so the contents of the working-copy commit read as staged. Neither is
// work the user forgot to commit, so the index column is dropped and the branch
// is reported as `@`.
func (g *jujutsuGuard) Status(ctx context.Context) (Status, error) {
	st, err := g.Backend.Status(ctx)
	if err != nil {
		return Status{}, err //nolint:wrapcheck // the backend error already carries a code and a message
	}
	st.Branch, st.Detached = JujutsuWorkingCopy, false
	st.Staged = []string{}
	st.Clean = len(st.Modified)+len(st.Untracked) == 0
	return st, nil
}

// SyncStatus corrects the sync view the same way and marks the repository as
// jj, which is what makes the panel render it honestly instead of painting a
// destructive "Detached HEAD" badge over it.
func (g *jujutsuGuard) SyncStatus(ctx context.Context) (SyncStatus, error) {
	st, err := g.Backend.SyncStatus(ctx)
	if err != nil {
		return SyncStatus{}, err //nolint:wrapcheck // the backend error already carries a code and a message
	}
	st.Jujutsu = true
	st.Branch, st.Detached = JujutsuWorkingCopy, false
	// The dirty set is rebuilt from the working-copy column only, for the same
	// reason Status drops the index: the index is jj's, not the user's.
	plain, statusErr := g.Status(ctx)
	if statusErr == nil {
		dirty := append(append([]string{}, plain.Modified...), plain.Untracked...)
		sort.Strings(dirty)
		st.Dirty, st.Tracked = dirty, len(plain.Modified) > 0
	}
	st.resolveState()
	return st, nil
}

// Commit refuses commit on save. This is the write the epic's audit found to be
// destructive: it lands on `@-`, moves no bookmark, and the next jj command
// abandons the previous working-copy commit as an orphan.
func (g *jujutsuGuard) Commit(_ context.Context, _ CommitRequest) (CommitResult, error) {
	return CommitResult{}, refuseJujutsu("commit", g.Path(), core.JujutsuCommitCommand)
}

// Fetch refuses to write remote-tracking refs into a store jj owns.
func (g *jujutsuGuard) Fetch(_ context.Context, _ FetchRequest) (FetchResult, error) {
	return FetchResult{}, refuseJujutsu("fetch", g.Path(), core.JujutsuFetchCommand)
}

// Integrate refuses the rebase or merge.
func (g *jujutsuGuard) Integrate(_ context.Context, _ IntegrateRequest) (IntegrateResult, error) {
	return IntegrateResult{}, refuseJujutsu("integrate", g.Path(), core.JujutsuRebaseCommand)
}

// Push refuses the push: git would publish a commit no bookmark points at.
func (g *jujutsuGuard) Push(_ context.Context, _ PushRequest) (PushResult, error) {
	return PushResult{}, refuseJujutsu("push", g.Path(), core.JujutsuPushCommand)
}

// Abort refuses; `jj undo` is what undoes an operation in jj.
func (g *jujutsuGuard) Abort(_ context.Context) error {
	return refuseJujutsu("abort", g.Path(), core.JujutsuUndoCommand)
}

// Continue refuses; jj records conflicts inside commits and has no
// half-finished operation to resume.
func (g *jujutsuGuard) Continue(_ context.Context) (IntegrateResult, error) {
	return IntegrateResult{}, refuseJujutsu("continue", g.Path(), core.JujutsuResolveCommand)
}

// ResolvePath refuses: writing a resolution stages it and continues an
// integration, both of them git writes.
func (g *jujutsuGuard) ResolvePath(_ context.Context, _ ResolveRequest) (ResolveResult, error) {
	return ResolveResult{}, refuseJujutsu("resolve", g.Path(), core.JujutsuResolveCommand)
}
