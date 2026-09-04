package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// goGitBackend drives a working tree in process, with no external dependency.
//
// What it deliberately does not do (docs/06 section 7.1): it runs no hooks, it
// signs nothing with the user's gpg or ssh agent, and it reads no credential
// helper. Those are exactly the reasons `auto` prefers the system binary.
type goGitBackend struct {
	path string
	opts Options
	repo *git.Repository
}

// openGoGit binds a go-git backend to a working tree.
func openGoGit(path string, opts Options) (Backend, error) {
	repo, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return nil, wrap("open", CodeNotARepository, err,
				"%s is not inside a git working tree", path)
		}
		return nil, wrap("open", CodeNotARepository, err, "open %s", path)
	}
	return &goGitBackend{path: path, opts: opts, repo: repo}, nil
}

// Name reports the backend name.
func (b *goGitBackend) Name() string { return string(KindGoGit) }

// Path reports the working tree.
func (b *goGitBackend) Path() string { return b.path }

// Capabilities describes what go-git can do here.
func (b *goGitBackend) Capabilities() Capabilities {
	return Capabilities{
		Backend:           string(KindGoGit),
		Hooks:             false,
		Signing:           false,
		CredentialHelpers: false,
		PathspecCommit:    false,
	}
}

// Identity resolves the author from the overrides, then from the repository and
// global git configuration.
func (b *goGitBackend) Identity(_ context.Context) (Identity, error) {
	if id := (Identity{Name: b.opts.AuthorName, Email: b.opts.AuthorEmail}); id.Valid() {
		return id, nil
	}
	cfg, err := b.repo.ConfigScoped(config.SystemScope)
	if err != nil {
		return Identity{}, wrap("identity", CodeNoIdentity, err,
			"read the git configuration of %s", b.path)
	}
	id := Identity{Name: cfg.User.Name, Email: cfg.User.Email}
	if !id.Valid() {
		return Identity{}, noIdentity("identity", b.path)
	}
	return id, nil
}

// Status reports the branch and the dirty set.
func (b *goGitBackend) Status(_ context.Context) (Status, error) {
	head, err := b.repo.Head()
	out := Status{Staged: []string{}, Modified: []string{}, Untracked: []string{}}
	switch {
	case err == nil:
		out.Branch = head.Name().Short()
		out.Detached = !head.Name().IsBranch()
	case errors.Is(err, plumbing.ErrReferenceNotFound):
		// A repository with no commit yet: HEAD points at an unborn branch.
		if ref, refErr := b.repo.Reference(plumbing.HEAD, false); refErr == nil {
			out.Branch = ref.Target().Short()
		}
	default:
		return Status{}, wrap("status", CodeCommitFailed, err, "read HEAD of %s", b.path)
	}

	wt, err := b.repo.Worktree()
	if err != nil {
		return Status{}, wrap("status", CodeCommitFailed, err, "open the worktree of %s", b.path)
	}
	st, err := wt.Status()
	if err != nil {
		return Status{}, wrap("status", CodeCommitFailed, err, "read the status of %s", b.path)
	}
	for path, entry := range st {
		switch {
		case entry.Worktree == git.Untracked:
			out.Untracked = append(out.Untracked, path)
		case entry.Staging != git.Unmodified && entry.Staging != git.Untracked:
			out.Staged = append(out.Staged, path)
			if entry.Worktree != git.Unmodified {
				out.Modified = append(out.Modified, path)
			}
		case entry.Worktree != git.Unmodified:
			out.Modified = append(out.Modified, path)
		}
	}
	out.Staged, out.Modified, out.Untracked =
		normalisePaths(out.Staged), normalisePaths(out.Modified), normalisePaths(out.Untracked)
	out.Clean = len(out.Staged)+len(out.Modified)+len(out.Untracked) == 0
	return out, nil
}

// Commit stages exactly the requested paths and commits them.
func (b *goGitBackend) Commit(ctx context.Context, req CommitRequest) (CommitResult, error) {
	if req.Sign {
		return CommitResult{}, failf("commit", CodeUnsupported,
			"the go-git backend cannot sign commits: set git.backend to system, or turn git.signCommits off")
	}
	paths := normalisePaths(req.Paths)
	if len(paths) == 0 && !req.AllowEmpty {
		return CommitResult{Empty: true}, nil
	}
	author := req.Author
	if !author.Valid() {
		resolved, err := b.Identity(ctx)
		if err != nil {
			return CommitResult{}, err
		}
		author = resolved
	}

	wt, err := b.repo.Worktree()
	if err != nil {
		return CommitResult{}, wrap("commit", CodeCommitFailed, err, "open the worktree of %s", b.path)
	}
	staged, err := b.stage(wt, paths)
	if err != nil {
		return CommitResult{}, err
	}
	if !staged && !req.AllowEmpty {
		return CommitResult{Empty: true, Paths: paths, Author: author}, nil
	}

	when := b.opts.clock()()
	sig := &object.Signature{Name: author.Name, Email: author.Email, When: when}
	hash, err := wt.Commit(req.Message.String(), &git.CommitOptions{
		Author:            sig,
		Committer:         sig,
		AllowEmptyCommits: req.AllowEmpty,
	})
	if err != nil {
		if errors.Is(err, git.ErrEmptyCommit) {
			return CommitResult{Empty: true, Paths: paths, Author: author}, nil
		}
		return CommitResult{}, wrap("commit", CodeCommitFailed, err,
			"commit %d file(s) in %s (the files on disk were not touched)", len(paths), b.path)
	}
	return CommitResult{
		SHA:     hash.String(),
		Subject: req.Message.Subject,
		Author:  author,
		Paths:   paths,
	}, nil
}

// stage adds the paths to the index, staging a deletion for the ones that no
// longer exist. It reports whether anything ended up staged.
func (b *goGitBackend) stage(wt *git.Worktree, paths []string) (bool, error) {
	staged := false
	for _, p := range paths {
		abs := filepath.Join(b.path, filepath.FromSlash(p))
		_, statErr := os.Lstat(abs)
		switch {
		case statErr == nil:
			if _, err := wt.Add(p); err != nil {
				return false, wrap("commit", CodeCommitFailed, err, "stage %s", p)
			}
		case errors.Is(statErr, os.ErrNotExist):
			// The write removed the file: stage the deletion. go-git reports an
			// error for a path it never tracked, which is a no-op for us.
			if _, err := wt.Remove(p); err != nil && !isMissingEntry(err) {
				return false, wrap("commit", CodeCommitFailed, err, "stage the deletion of %s", p)
			}
		default:
			return false, wrap("commit", CodeCommitFailed, statErr, "read %s", p)
		}
		staged = true
	}
	if !staged {
		return false, nil
	}
	return b.hasStagedChanges(wt)
}

// hasStagedChanges reports whether the index differs from HEAD, so that a write
// that changed nothing produces no empty commit.
func (b *goGitBackend) hasStagedChanges(wt *git.Worktree) (bool, error) {
	st, err := wt.Status()
	if err != nil {
		return false, wrap("commit", CodeCommitFailed, err, "read the status of %s", b.path)
	}
	for _, entry := range st {
		if entry.Staging != git.Unmodified && entry.Staging != git.Untracked {
			return true, nil
		}
	}
	return false, nil
}

// isMissingEntry reports whether go-git refused a removal because the path was
// never tracked, which is not a failure for us.
func isMissingEntry(err error) bool {
	return err != nil && strings.Contains(err.Error(), "entry not found")
}

// ensure the clock default is honoured even when Options came in zero-valued.
func (o Options) clock() func() time.Time {
	if o.Now == nil {
		return time.Now
	}
	return o.Now
}
