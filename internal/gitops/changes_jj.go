package gitops

import (
	"context"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// The Jujutsu half of ChangedFiles (GIT-US-0112).
//
// jj resolves the refs — a bookmark, a commit id, a git-shaped `origin/main`
// translated to `main@origin` by revsetOf — and the trees are read in process
// from the git object store jj commits into, exactly as History reads them
// (ADR-023). The working tree is read from disk rather than from `@`: `@` is
// only as fresh as jj's last snapshot, and taking a new snapshot would write an
// operation, which no read of this backend is allowed to do.

// ChangedFiles lists the files that differ between two revisions, or between a
// revision and the working tree.
func (b *jujutsuBackend) ChangedFiles(ctx context.Context, from, to string) ([]FileChange, error) {
	if b.store == "" {
		return nil, failf("changed-files", CodeUnsupported,
			"the jj repository at %s has no git object store to read", b.path)
	}
	repo, err := git.PlainOpen(b.store)
	if err != nil {
		return nil, wrap("changed-files", CodeCommitFailed, err,
			"open the jj object store of %s", b.path)
	}
	fromID, err := b.resolveRevision(ctx, from)
	if err != nil {
		return nil, err
	}
	old, err := gitCommitSide(repo, plumbing.NewHash(fromID), b.path)
	if err != nil {
		return nil, err
	}
	var side treeSide
	if strings.TrimSpace(to) == WorkingTree {
		side, err = b.workingTreeSide(ctx, repo)
	} else {
		var toID string
		if toID, err = b.resolveRevision(ctx, to); err == nil {
			side, err = gitCommitSide(repo, plumbing.NewHash(toID), b.path)
		}
	}
	if err != nil {
		return nil, err
	}
	return compareSides(ctx, old, side)
}

// resolveRevision turns a git-shaped ref into exactly one commit id, failing
// with CodeUnknownRevision when it names none — or several, which a revset can.
func (b *jujutsuBackend) resolveRevision(ctx context.Context, ref string) (string, error) {
	revset := b.revsetOf(ctx, ref)
	if revset == "" {
		return "", unknownRevision(ref, b.path, nil)
	}
	raw, err := b.run(ctx, "log", "--no-graph", "-r", revset, "-T", `commit_id ++ "\n"`)
	if err != nil {
		return "", unknownRevision(ref, b.path, err)
	}
	ids := strings.Fields(raw)
	if len(ids) != 1 {
		return "", unknownRevision(ref, b.path, nil)
	}
	return ids[0], nil
}

// workingTreeSide reads the working copy from disk, treating every path of `@`
// — the last snapshot — as tracked.
func (b *jujutsuBackend) workingTreeSide(ctx context.Context, repo *git.Repository) (treeSide, error) {
	head, err := b.workingCopyCommit(ctx)
	if err != nil {
		return treeSide{}, err
	}
	tracked := map[string]bool{}
	if head != "" {
		snapshot, err := gitCommitSide(repo, plumbing.NewHash(head), b.path)
		if err != nil {
			return treeSide{}, err
		}
		for path := range snapshot.ids {
			tracked[path] = true
		}
	}
	return workingTreeSide(ctx, b.path, tracked)
}
