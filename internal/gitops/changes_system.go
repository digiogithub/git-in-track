package gitops

import (
	"context"
	"strings"
)

// The system-git half of ChangedFiles (GIT-US-0112): git resolves the refs,
// lists the trees with `ls-tree` and reads the blobs that differ in one
// `cat-file --batch`. The comparison itself is shared (changes.go), so the
// result is the one the go-git and jj backends return.

// ChangedFiles lists the files that differ between two revisions, or between a
// revision and the working tree.
func (b *systemBackend) ChangedFiles(ctx context.Context, from, to string) ([]FileChange, error) {
	fromSHA, err := b.resolveRevision(ctx, from)
	if err != nil {
		return nil, err
	}
	old, err := b.commitSide(ctx, fromSHA)
	if err != nil {
		return nil, err
	}
	var side treeSide
	if strings.TrimSpace(to) == WorkingTree {
		side, err = b.workingTreeSide(ctx)
	} else {
		var toSHA string
		if toSHA, err = b.resolveRevision(ctx, to); err == nil {
			side, err = b.commitSide(ctx, toSHA)
		}
	}
	if err != nil {
		return nil, err
	}
	return compareSides(ctx, old, side)
}

// resolveRevision turns a ref into a commit SHA, failing with
// CodeUnknownRevision. A ref that starts with a dash is refused rather than
// handed to git, where it would read as an option.
func (b *systemBackend) resolveRevision(ctx context.Context, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "-") {
		return "", unknownRevision(ref, b.path, nil)
	}
	raw, err := b.run(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	sha := strings.TrimSpace(raw)
	if err != nil || sha == "" {
		return "", unknownRevision(ref, b.path, err)
	}
	return sha, nil
}

// commitSide lists the blobs of one commit's tree as a comparison side.
func (b *systemBackend) commitSide(ctx context.Context, sha string) (treeSide, error) {
	raw, err := b.run(ctx, "ls-tree", "-r", "-z", "--full-tree", sha)
	if err != nil {
		return treeSide{}, wrap("changed-files", CodeCommitFailed, err,
			"list the tree of %s in %s", sha, b.path)
	}
	ids := map[string]string{}
	for _, record := range strings.Split(raw, "\x00") {
		// "<mode> SP <type> SP <object> TAB <path>"
		meta, path, ok := strings.Cut(record, "\t")
		if !ok || path == "" {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 || fields[1] != "blob" {
			continue // a submodule is a commit entry, not a blob
		}
		ids[path] = fields[2]
	}
	read := func(ctx context.Context, paths []string) (map[string][]byte, error) {
		out := make(map[string][]byte, len(paths))
		if len(paths) == 0 {
			return out, nil
		}
		specs := make([]string, 0, len(paths))
		for _, path := range paths {
			specs = append(specs, ids[path])
		}
		blobs, err := b.catFileBatch(ctx, specs)
		if err != nil {
			return nil, err
		}
		for i, path := range paths {
			if blobs[i].missing {
				return nil, failf("changed-files", CodeCommitFailed,
					"read %s at %s in %s: the blob is missing", path, sha, b.path)
			}
			out[path] = blobs[i].data
		}
		return out, nil
	}
	return treeSide{ids: ids, read: read}, nil
}

// workingTreeSide reads the working tree, treating every path in the index as
// tracked.
func (b *systemBackend) workingTreeSide(ctx context.Context) (treeSide, error) {
	top, err := b.run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return treeSide{}, wrap("changed-files", CodeCommitFailed, err,
			"find the top of the working tree of %s", b.path)
	}
	listed, err := b.run(ctx, "ls-files", "-z")
	if err != nil {
		return treeSide{}, wrap("changed-files", CodeCommitFailed, err,
			"list the tracked files of %s", b.path)
	}
	tracked := map[string]bool{}
	for _, path := range strings.Split(listed, "\x00") {
		if path != "" {
			tracked[path] = true
		}
	}
	return workingTreeSide(ctx, strings.TrimSpace(top), tracked)
}
