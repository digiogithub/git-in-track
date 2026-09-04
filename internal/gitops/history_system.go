package gitops

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// The system-git implementation of a file history walk. It asks git for the
// commits that touched each path — which is the one question git is fastest at
// — and then reads every blob it needs in a single `cat-file --batch`, so the
// cost of a walk is two processes plus one per path rather than one per
// revision.

// historyFieldSeparator separates the fields of a history log line. It is the
// ASCII unit separator, which cannot appear in a commit hash or an ISO date.
const historyFieldSeparator = "\x1f"

// History reads every revision of the requested paths, newest commit last.
func (b *systemBackend) History(ctx context.Context, req HistoryRequest) (FileHistory, error) {
	req = req.normalized()
	out := FileHistory{Revisions: []FileRevision{}}
	if len(req.Paths) == 0 {
		return out, nil
	}
	head, err := b.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		// A repository with no commit yet has no history, and that is not a
		// failure: it is an empty one.
		return out, nil //nolint:nilerr // an unborn branch is an empty history
	}
	out.Head = strings.TrimSpace(head)

	type candidate struct {
		path string
		sha  string
		when time.Time
	}
	var candidates []candidate
	for _, path := range req.Paths {
		args := []string{"log", "--full-history",
			"--format=%H" + historyFieldSeparator + "%aI",
			"--max-count=" + strconv.Itoa(req.Limit)}
		if !req.Since.IsZero() {
			args = append(args, "--since="+req.Since.UTC().Format(time.RFC3339))
		}
		args = append(args, "--", path)
		raw, err := b.run(ctx, args...)
		if err != nil {
			return FileHistory{}, wrap("history", CodeCommitFailed, err,
				"read the history of %s in %s", path, b.path)
		}
		lines := strings.Split(strings.TrimSpace(raw), "\n")
		count := 0
		for _, line := range lines {
			sha, stamp, ok := strings.Cut(strings.TrimSpace(line), historyFieldSeparator)
			if !ok || sha == "" {
				continue
			}
			when, err := time.Parse(time.RFC3339, stamp)
			if err != nil {
				continue
			}
			candidates = append(candidates, candidate{path: path, sha: sha, when: when.UTC()})
			count++
		}
		if count >= req.Limit {
			out.Truncated = true
		}
	}
	if len(candidates) == 0 {
		return out, nil
	}

	specs := make([]string, 0, len(candidates))
	for _, c := range candidates {
		specs = append(specs, c.sha+":"+c.path)
	}
	blobs, err := b.catFileBatch(ctx, specs)
	if err != nil {
		return FileHistory{}, err
	}
	seen := map[string]bool{}
	for i, c := range candidates {
		blob := blobs[i]
		rev := FileRevision{Path: c.path, SHA: c.sha, When: c.when}
		if blob.missing {
			rev.Deleted = true
		} else {
			rev.Data = blob.data
		}
		out.Revisions = append(out.Revisions, rev)
		if !seen[c.sha] {
			seen[c.sha] = true
			out.Commits++
		}
	}
	out.Revisions = dedupeRevisions(out.Revisions)
	out.finish()
	return out, nil
}

// blobResult is one answer of `cat-file --batch`.
type blobResult struct {
	data    []byte
	missing bool
}

// catFileBatch reads every requested `<commit>:<path>` in one process. A spec
// that names a path the commit does not hold comes back as missing, which is
// how a deletion is recognized.
func (b *systemBackend) catFileBatch(ctx context.Context, specs []string) ([]blobResult, error) {
	raw, err := b.runWith(ctx, b.env(), strings.Join(specs, "\n")+"\n", "cat-file", "--batch")
	if err != nil {
		return nil, wrap("history", CodeCommitFailed, err, "read %d blobs in %s", len(specs), b.path)
	}
	out := make([]blobResult, 0, len(specs))
	rest := raw
	for range specs {
		header, remainder, ok := strings.Cut(rest, "\n")
		if !ok {
			out = append(out, blobResult{missing: true})
			continue
		}
		fields := strings.Fields(header)
		if len(fields) < 3 {
			// "<spec> missing" and anything else unexpected: no content.
			out = append(out, blobResult{missing: true})
			rest = remainder
			continue
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size > len(remainder) {
			out = append(out, blobResult{missing: true})
			rest = remainder
			continue
		}
		out = append(out, blobResult{data: []byte(remainder[:size])})
		// The payload is followed by a newline git adds itself.
		rest = strings.TrimPrefix(remainder[size:], "\n")
	}
	return out, nil
}

// dedupeRevisions drops the revisions that changed nothing about a path. A
// merge commit lists a file that both sides already agreed on, and history
// simplification can report the same content twice; neither is a transition, so
// neither belongs in a metric.
func dedupeRevisions(revs []FileRevision) []FileRevision {
	byPath := map[string][]FileRevision{}
	for _, rev := range revs {
		byPath[rev.Path] = append(byPath[rev.Path], rev)
	}
	out := make([]FileRevision, 0, len(revs))
	for path := range byPath {
		list := byPath[path]
		sortRevisionsByTime(list)
		var previous string
		first := true
		for _, rev := range list {
			fingerprint := "deleted"
			if !rev.Deleted {
				fingerprint = string(rev.Data)
			}
			if !first && fingerprint == previous {
				continue
			}
			first = false
			previous = fingerprint
			out = append(out, rev)
		}
	}
	return out
}

// sortRevisionsByTime orders one path's revisions oldest first.
func sortRevisionsByTime(revs []FileRevision) {
	for i := 1; i < len(revs); i++ {
		for j := i; j > 0 && revs[j].When.Before(revs[j-1].When); j-- {
			revs[j], revs[j-1] = revs[j-1], revs[j]
		}
	}
}
