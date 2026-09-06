package gitops

import (
	"context"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// The file history of a Jujutsu repository (GIT-US-0040, ADR-023).
//
// jj's default backend stores every commit as a real git commit in the object
// store `.jj/repo/store/git_target` points at — the workspace's own `.git` in a
// colocated repository, a bare repository inside `.jj` otherwise. The history
// is therefore walked in process with go-git over that store, exactly the way
// the go-git backend walks a git working tree.
//
// The alternative, `jj log -r 'files(<path>)'` plus one `jj file show` per
// revision, is one operating-system process per blob. The metrics of
// GIT-US-0028 read thousands of revisions on every rebuild, so that route would
// have made the burndown and the CFD unusable. ADR-023 records the decision and
// why it is not a git-shaped reading of a jj repository: the starting point is
// the commit *jj* reports for `@`, never git's HEAD, which in a jj repository
// points at `@-` and would silently drop the newest commit.

// History reads every revision of the requested paths out of the git object
// store jj commits into, starting from the working-copy commit.
func (b *jujutsuBackend) History(ctx context.Context, req HistoryRequest) (FileHistory, error) {
	req = req.normalized()
	out := FileHistory{Revisions: []FileRevision{}}
	if len(req.Paths) == 0 || b.store == "" {
		return out, nil
	}
	head, err := b.workingCopyCommit(ctx)
	if err != nil || head == "" {
		// A repository whose working-copy commit cannot be read has no history
		// to offer, and that is an empty answer rather than a failure: the
		// metrics report the days they cannot cover as unknown.
		return out, nil //nolint:nilerr // an unreadable working copy is an empty history
	}
	repo, err := git.PlainOpen(b.store)
	if err != nil {
		return FileHistory{}, wrap("history", CodeCommitFailed, err,
			"open the jj object store of %s", b.path)
	}
	return walkGitHistory(ctx, repo, plumbing.NewHash(head), b.path, req)
}

// workingCopyCommit reports the git commit id of `@`.
//
// It is what makes this walk a jj reading rather than a git one. Git's HEAD in
// a colocated repository sits at `@-`, so a walk started there misses the
// working-copy commit; in a non-colocated repository there is no HEAD to start
// from at all.
func (b *jujutsuBackend) workingCopyCommit(ctx context.Context) (string, error) {
	raw, err := b.run(ctx, "log", "--no-graph", "-T", `commit_id`, "-r", "@")
	if err != nil {
		return "", wrap("history", CodeCommitFailed, err,
			"read the working-copy commit of %s", b.path)
	}
	return strings.TrimSpace(raw), nil
}
