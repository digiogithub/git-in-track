package gitops

import (
	"context"

	"github.com/go-git/go-git/v5/plumbing"
)

// Branches lists the local and remote-tracking branches, local first, each
// sorted by name (GIT-US-0149). Symbolic refs such as `origin/HEAD` are not
// branches of their own and are left out.
func (b *goGitBackend) Branches(_ context.Context) ([]Branch, error) {
	current := ""
	if head, err := b.repo.Storer.Reference(plumbing.HEAD); err == nil &&
		head.Type() == plumbing.SymbolicReference && head.Target().IsBranch() {
		current = head.Target().Short()
	}
	refs, err := b.repo.References()
	if err != nil {
		return nil, wrap("branches", CodeCommitFailed, err, "list the refs of %s", b.path)
	}
	out := []Branch{}
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference {
			return nil
		}
		switch name := ref.Name(); {
		case name.IsBranch():
			out = append(out, Branch{Name: name.Short(), SHA: ref.Hash().String(),
				Current: name.Short() == current})
		case name.IsRemote():
			if remote, _, ok := remoteBranch(name.Short()); ok {
				out = append(out, Branch{Name: name.Short(), Remote: remote, SHA: ref.Hash().String()})
			}
		}
		return nil
	})
	if err != nil {
		return nil, wrap("branches", CodeCommitFailed, err, "list the refs of %s", b.path)
	}
	return sortBranches(out), nil
}
