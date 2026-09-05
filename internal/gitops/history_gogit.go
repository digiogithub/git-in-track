package gitops

import (
	"context"
	"errors"
	"fmt"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// The pure-Go implementation of a file history walk. go-git can filter the log
// by path, so the walk visits only the commits that touched an item file; each
// of those is then read once per requested path and the unchanged readings are
// dropped, which leaves exactly the transitions.

// History reads every revision of the requested paths.
func (b *goGitBackend) History(ctx context.Context, req HistoryRequest) (FileHistory, error) {
	req = req.normalized()
	out := FileHistory{Revisions: []FileRevision{}}
	if len(req.Paths) == 0 {
		return out, nil
	}
	head, err := b.repo.Head()
	if err != nil {
		// A repository with no commit yet has an empty history, not a failure.
		return out, nil
	}
	out.Head = head.Hash().String()

	wanted := make(map[string]bool, len(req.Paths))
	for _, path := range req.Paths {
		wanted[path] = true
	}
	opts := &git.LogOptions{
		From:       head.Hash(),
		Order:      git.LogOrderCommitterTime,
		PathFilter: func(p string) bool { return wanted[p] },
	}
	if !req.Since.IsZero() {
		since := req.Since
		opts.Since = &since
	}
	iter, err := b.repo.Log(opts)
	if err != nil {
		return FileHistory{}, wrap("history", CodeCommitFailed, err,
			"read the history of %s", b.path)
	}
	defer iter.Close()

	err = iter.ForEach(func(commit *object.Commit) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("walk the history of %s: %w", b.path, err)
		}
		if out.Commits >= req.Limit {
			out.Truncated = true
			return errStopWalk
		}
		out.Commits++
		tree, err := commit.Tree()
		if err != nil {
			// A commit whose tree cannot be read contributes no revision. It is
			// a gap in the walk, not a failure of it, and the truncation flag
			// is what tells the metrics the history is incomplete.
			out.Truncated = true
			return nil //nolint:nilerr // a damaged commit is skipped, never fatal
		}
		when := commit.Author.When.UTC()
		for _, path := range req.Paths {
			rev := FileRevision{Path: path, SHA: commit.Hash.String(), When: when}
			file, err := tree.File(path)
			if err != nil {
				rev.Deleted = true
			} else {
				contents, err := file.Contents()
				if err != nil {
					continue
				}
				rev.Data = []byte(contents)
			}
			out.Revisions = append(out.Revisions, rev)
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return FileHistory{}, wrap("history", CodeCommitFailed, err,
			"walk the history of %s", b.path)
	}
	out.Revisions = dedupeRevisions(out.Revisions)
	// A path that was already absent at the oldest commit read is not a
	// deletion: it is the file not existing yet. Drop a leading deletion.
	out.Revisions = dropLeadingDeletions(out.Revisions)
	out.finish()
	return out, nil
}

// errStopWalk ends a ForEach walk early without turning it into a failure.
var errStopWalk = errors.New("stop")

// dropLeadingDeletions removes the "absent" reading a path starts with when the
// walk reached commits older than the file itself. Absent-before-creation is
// the normal state of every item and must not be reported as a deletion.
func dropLeadingDeletions(revs []FileRevision) []FileRevision {
	oldest := map[string]int{}
	for i, rev := range revs {
		at, ok := oldest[rev.Path]
		if !ok || rev.When.Before(revs[at].When) {
			oldest[rev.Path] = i
		}
	}
	drop := map[int]bool{}
	for _, index := range oldest {
		if revs[index].Deleted {
			drop[index] = true
		}
	}
	out := make([]FileRevision, 0, len(revs))
	for i, rev := range revs {
		if drop[i] {
			continue
		}
		out = append(out, rev)
	}
	return out
}
