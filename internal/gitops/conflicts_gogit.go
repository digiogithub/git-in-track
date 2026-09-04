package gitops

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/index"
)

// The structured conflict surface of the go-git backend (GIT-US-0022).
//
// Reading works in process: the three stages of a conflicted path are entries
// of the index, and their blobs are objects like any other. Resolving does not,
// for the same reason Abort and Continue do not — go-git has no rebase and its
// merge is fast-forward only (docs/06 section 7.1) — so it refuses explicitly
// instead of writing a file into an integration it cannot finish.

// ConflictFile reads the base, ours and theirs versions of a conflicted path
// from the index stages.
func (b *goGitBackend) ConflictFile(_ context.Context, path string) (ConflictVersions, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	idx, err := b.repo.Storer.Index()
	if err != nil {
		return ConflictVersions{}, wrap("conflict", CodeCommitFailed, err,
			"read the index of %s", b.path)
	}
	out := ConflictVersions{Path: path, Kind: ConflictContent}
	found := false
	for _, entry := range idx.Entries {
		if filepath.ToSlash(entry.Name) != path || entry.Stage == index.Merged {
			continue
		}
		content, readErr := b.blobContent(entry.Hash)
		if readErr != nil {
			return ConflictVersions{}, readErr
		}
		found = true
		switch entry.Stage {
		case index.AncestorMode:
			out.Base, out.HasBase = content, true
		case index.OurMode:
			out.Ours, out.HasOurs = content, true
		case index.TheirMode:
			out.Theirs, out.HasTheirs = content, true
		}
	}
	if !found {
		return ConflictVersions{}, notConflicted("conflict", path, b.path)
	}
	if b.operationInProgress() == OpRebase {
		out.swapSides()
	}
	if raw, readErr := os.ReadFile(filepath.Join(b.path, filepath.FromSlash(path))); readErr == nil {
		out.Working = string(raw)
	}
	switch {
	case !out.HasOurs || !out.HasTheirs:
		out.Kind = ConflictDeleteModify
	case !out.HasBase:
		out.Kind = ConflictAddAdd
	}
	out.Binary = isBinary(out.Base) || isBinary(out.Ours) || isBinary(out.Theirs)
	return out, nil
}

// blobContent reads one object out of the store.
func (b *goGitBackend) blobContent(hash plumbing.Hash) (string, error) {
	blob, err := b.repo.BlobObject(hash)
	if err != nil {
		return "", wrap("conflict", CodeCommitFailed, err, "read object %s of %s", hash, b.path)
	}
	reader, err := blob.Reader()
	if err != nil {
		return "", wrap("conflict", CodeCommitFailed, err, "open object %s of %s", hash, b.path)
	}
	defer func() { _ = reader.Close() }()
	raw, err := io.ReadAll(reader)
	if err != nil {
		return "", wrap("conflict", CodeCommitFailed, err, "read object %s of %s", hash, b.path)
	}
	return string(raw), nil
}

// ResolvePath is not available in process: finishing the integration a
// resolution belongs to needs the rebase and merge machinery only the system
// backend has, and writing the file without finishing would leave the
// repository half-resolved.
func (b *goGitBackend) ResolvePath(_ context.Context, _ ResolveRequest) (ResolveResult, error) {
	return ResolveResult{}, failf("resolve", CodeUnsupported,
		"the go-git backend cannot complete a rebase or a merge, so it cannot apply a "+
			"resolution in %s: install git so the system backend is used, or resolve the "+
			"files and run `git rebase --continue` yourself", b.path)
}
