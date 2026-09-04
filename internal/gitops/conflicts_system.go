package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// The structured conflict surface of the system backend (GIT-US-0022).
//
// Everything here reads the index git already filled when the integration
// stopped, so nothing is recomputed and nothing can disagree with what
// `git status` says.

// ConflictFile reads the three versions of one conflicted path out of the
// index: stage 1 is the merge base, stage 2 ours, stage 3 theirs.
func (b *systemBackend) ConflictFile(ctx context.Context, path string) (ConflictVersions, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return ConflictVersions{}, failf("conflict", CodeNotFound, "no path was given")
	}
	st, err := b.SyncStatus(ctx)
	if err != nil {
		return ConflictVersions{}, err
	}
	conflict, ok := conflictOf(st.Conflicted, path)
	if !ok {
		return ConflictVersions{}, notConflicted("conflict", path, b.path)
	}

	out := ConflictVersions{Path: path, Kind: conflict.Kind}
	stages, err := b.conflictStages(ctx, path)
	if err != nil {
		return ConflictVersions{}, err
	}
	for stage, blob := range stages {
		content, readErr := b.blob(ctx, blob)
		if readErr != nil {
			return ConflictVersions{}, readErr
		}
		switch stage {
		case 1:
			out.Base, out.HasBase = content, true
		case 2:
			out.Ours, out.HasOurs = content, true
		case 3:
			out.Theirs, out.HasTheirs = content, true
		}
	}
	if st.Operation == OpRebase {
		out.swapSides()
	}
	if raw, readErr := os.ReadFile(filepath.Join(b.path, filepath.FromSlash(path))); readErr == nil {
		out.Working = string(raw)
	}
	out.Binary = isBinary(out.Base) || isBinary(out.Ours) || isBinary(out.Theirs)
	return out, nil
}

// conflictStages maps the stage number of a conflicted path to its blob id.
func (b *systemBackend) conflictStages(ctx context.Context, path string) (map[int]string, error) {
	raw, err := b.run(ctx, "ls-files", "--unmerged", "--", path)
	if err != nil {
		return nil, wrap("conflict", CodeCommitFailed, err,
			"read the conflicted stages of %s in %s", path, b.path)
	}
	out := map[int]string{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		meta, _, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) < 3 {
			continue
		}
		switch fields[2] {
		case "1":
			out[1] = fields[1]
		case "2":
			out[2] = fields[1]
		case "3":
			out[3] = fields[1]
		}
	}
	return out, nil
}

// blob reads one object's content.
func (b *systemBackend) blob(ctx context.Context, oid string) (string, error) {
	out, err := b.run(ctx, "cat-file", "blob", oid)
	if err != nil {
		return "", wrap("conflict", CodeCommitFailed, err, "read object %s of %s", oid, b.path)
	}
	return out, nil
}

// ResolvePath writes one resolution, stages it and, when nothing is left
// conflicted and the caller asked for it, continues the rebase or merge.
//
// Every step is recoverable: the file is written before it is staged, and a
// failed continue leaves the operation in progress, which Abort still undoes.
func (b *systemBackend) ResolvePath(ctx context.Context, req ResolveRequest) (ResolveResult, error) {
	path := filepath.ToSlash(strings.TrimSpace(req.Path))
	st, err := b.SyncStatus(ctx)
	if err != nil {
		return ResolveResult{}, err
	}
	if st.Operation == "" {
		return ResolveResult{}, failf("resolve", CodeInProgress,
			"no rebase or merge is in progress in %s: there is nothing to resolve", b.path)
	}
	if _, ok := conflictOf(st.Conflicted, path); !ok {
		return ResolveResult{}, notConflicted("resolve", path, b.path)
	}

	out := ResolveResult{Path: path}
	if err := b.writeResolution(ctx, path, req); err != nil {
		return out, err
	}
	out.Staged = true

	after, err := b.SyncStatus(ctx)
	if err != nil {
		return out, err
	}
	out.Remaining, out.Status = after.Conflicted, after
	if !req.Continue || len(after.Conflicted) > 0 {
		return out, nil
	}

	res, err := b.Continue(ctx)
	if err != nil {
		return out, err
	}
	out.Continued, out.Integration = true, &res
	if final, statusErr := b.SyncStatus(ctx); statusErr == nil {
		out.Status = final
	}
	return out, nil
}

// writeResolution puts the resolved content on disk and into the index.
func (b *systemBackend) writeResolution(ctx context.Context, path string, req ResolveRequest) error {
	full := filepath.Join(b.path, filepath.FromSlash(path))
	if req.Delete {
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return wrap("resolve", CodeCommitFailed, err, "remove %s in %s", path, b.path)
		}
		if _, err := b.run(ctx, "rm", "-f", "--", path); err != nil {
			return wrap("resolve", CodeCommitFailed, err, "stage the removal of %s in %s", path, b.path)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil { //nolint:gosec,mnd // repository files keep the modes git and the editor use
		return wrap("resolve", CodeCommitFailed, err, "create the folder of %s in %s", path, b.path)
	}
	if err := os.WriteFile(full, []byte(req.Content), 0o644); err != nil { //nolint:gosec,mnd // a resolved file is a working-tree file like any other
		return wrap("resolve", CodeCommitFailed, err, "write %s in %s", path, b.path)
	}
	if _, err := b.run(ctx, "add", "--", path); err != nil {
		return wrap("resolve", CodeCommitFailed, err, "stage %s in %s", path, b.path)
	}
	return nil
}
