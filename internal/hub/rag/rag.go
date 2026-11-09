// Package rag is gintrack-hub's semantic index (ADR-043 §5, GIT-US-0214):
// one Store per repository, kept in step with git by commit rather than by a
// file watcher, and a vault.SemanticSearcher that answers over the stores and
// resolves every hit back to the live document.
//
// Store is shaped after the pando/rag library (its kb.KBStore), so the
// adapter that wires the real library in is a matter of type conversion. Until
// that module is tagged, the hub ships with NewMemoryStore, a dependency-free
// full-text store with the same contract.
package rag

import (
	"context"
	"io/fs"
)

// The change operations of ApplyChanges.
const (
	OpAdd    = "add"
	OpModify = "modify"
	OpDelete = "delete"
	OpRename = "rename"
)

// Change is one file a commit range changed.
type Change struct {
	Op      string
	Path    string
	OldPath string
	// Blob is the git blob id of the new content; a rename whose blob is
	// unchanged is moved without being re-read or re-embedded.
	Blob string
	// Content, when nil, is read from the fs.FS given to ApplyChanges.
	Content []byte
}

// Stats counts what an ApplyChanges did.
type Stats struct {
	Added, Updated, Deleted, Renamed, Skipped int
}

// Query narrows a search. Every filter is applied by the store, before
// ranking, so a narrower scope never returns fewer hits than it could.
type Query struct {
	Limit           int
	PathPrefixes    []string
	ExcludePrefixes []string
	Kinds           []string
	// Statuses defaults to active when empty, the way pando/rag does; pass
	// AnyStatus for every status.
	Statuses []string
	Tags     []string
}

// AnyStatus asks for every status.
const AnyStatus = "*"

// Hit is one ranked chunk.
type Hit struct {
	Path      string
	Chunk     string
	StartLine int
	EndLine   int
	Score     float64
	Kind      string
	Status    string
	// Rev is the content hash the store indexed; the searcher drops a hit
	// whose document has changed since.
	Rev     string
	GitBlob string
	Title   string
}

// Store is one repository's index.
type Store interface {
	// ApplyChanges applies a commit range and records watermark (the commit
	// it reaches) in the same step. A watermark already recorded is a no-op.
	ApplyChanges(ctx context.Context, fsys fs.FS, changes []Change, watermark string) (Stats, error)
	// Watermark is the last commit applied, "" before the first.
	Watermark(ctx context.Context) (string, error)
	// Search ranks the chunks that match q.
	Search(ctx context.Context, q string, query Query) ([]Hit, error)
	Close() error
}
