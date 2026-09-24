package gitops

import (
	"context"
	"errors"
	"io"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// The in-process half of ChangedFiles (GIT-US-0112): go-git resolves the refs
// and reads the trees. gitCommitSide is shared with the Jujutsu backend, which
// reads the same kind of object store (ADR-023).

// ChangedFiles lists the files that differ between two revisions, or between a
// revision and the working tree.
func (b *goGitBackend) ChangedFiles(ctx context.Context, from, to string) ([]FileChange, error) {
	fromHash, err := resolveGoGitRevision(b.repo, from, b.path)
	if err != nil {
		return nil, err
	}
	old, err := gitCommitSide(b.repo, fromHash, b.path)
	if err != nil {
		return nil, err
	}
	var side treeSide
	if strings.TrimSpace(to) == WorkingTree {
		side, err = b.workingTreeSide(ctx)
	} else {
		var toHash plumbing.Hash
		if toHash, err = resolveGoGitRevision(b.repo, to, b.path); err == nil {
			side, err = gitCommitSide(b.repo, toHash, b.path)
		}
	}
	if err != nil {
		return nil, err
	}
	return compareSides(ctx, old, side)
}

// workingTreeSide reads the working tree, treating every path in the index as
// tracked.
func (b *goGitBackend) workingTreeSide(ctx context.Context) (treeSide, error) {
	wt, err := b.repo.Worktree()
	if err != nil {
		return treeSide{}, wrap("changed-files", CodeCommitFailed, err, "open the working tree of %s", b.path)
	}
	tracked := map[string]bool{}
	if idx, err := b.repo.Storer.Index(); err == nil {
		for _, entry := range idx.Entries {
			tracked[entry.Name] = true
		}
	}
	return workingTreeSide(ctx, wt.Filesystem.Root(), tracked)
}

// resolveGoGitRevision turns a branch, a remote-tracking ref, a tag or a (short)
// SHA into a commit, failing with CodeUnknownRevision.
func resolveGoGitRevision(repo *git.Repository, ref, label string) (plumbing.Hash, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return plumbing.ZeroHash, unknownRevision(ref, label, nil)
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return plumbing.ZeroHash, unknownRevision(ref, label, err)
	}
	if _, err := repo.CommitObject(*hash); err != nil {
		return plumbing.ZeroHash, unknownRevision(ref, label, err)
	}
	return *hash, nil
}

// gitCommitSide reads the tree of one commit as a comparison side. Submodules
// are skipped: their content is another repository's history.
func gitCommitSide(repo *git.Repository, hash plumbing.Hash, label string) (treeSide, error) {
	commit, err := repo.CommitObject(hash)
	if err != nil {
		return treeSide{}, unknownRevision(hash.String(), label, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return treeSide{}, wrap("changed-files", CodeCommitFailed, err,
			"read the tree of %s in %s", hash, label)
	}
	ids := map[string]string{}
	err = tree.Files().ForEach(func(file *object.File) error {
		if file.Mode == filemode.Submodule || file.Mode == filemode.Dir {
			return nil
		}
		ids[file.Name] = file.Hash.String()
		return nil
	})
	if err != nil {
		return treeSide{}, wrap("changed-files", CodeCommitFailed, err,
			"list the tree of %s in %s", hash, label)
	}
	read := func(ctx context.Context, paths []string) (map[string][]byte, error) {
		out := make(map[string][]byte, len(paths))
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return nil, wrap("changed-files", CodeCancelled, err, "read %s", path)
			}
			data, err := readTreeFile(tree, path)
			if err != nil {
				return nil, wrap("changed-files", CodeCommitFailed, err,
					"read %s at %s in %s", path, hash, label)
			}
			out[path] = data
		}
		return out, nil
	}
	return treeSide{ids: ids, read: read}, nil
}

// readTreeFile reads the bytes of one blob of a tree.
func readTreeFile(tree *object.Tree, path string) ([]byte, error) {
	file, err := tree.File(path)
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by the caller
	}
	reader, err := file.Reader()
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by the caller
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	return data, errors.Join(readErr, closeErr)
}
