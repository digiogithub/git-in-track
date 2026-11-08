package rag

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/digiogithub/pando/rag/kb"
	"github.com/digiogithub/pando/rag/migrate"
	_ "modernc.org/sqlite" // the pure-Go SQLite driver pando/rag is built on (FTS5 included)
)

// PandoStore is the Store backed by the pando/rag library (ADR-043 §5): one
// SQLite database per repository, full-text search with FTS5, and vector
// search once an embedder is configured. It is derived data: deleting the
// file only costs a full reindex from the repository's HEAD.
type PandoStore struct {
	db *sql.DB
	kb *kb.KBStore
}

// OpenPando opens, or creates and migrates, the index at path.
func OpenPando(ctx context.Context, path string) (*PandoStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One writer per store: SQLite serializes writes anyway, and the indexer
	// is the only writer.
	db.SetMaxOpenConns(1)
	if err := migrate.Up(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	store, err := kb.Open(db, kb.Options{})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open the index in %s: %w", path, err)
	}
	return &PandoStore{db: db, kb: store}, nil
}

// ApplyChanges implements Store.
func (p *PandoStore) ApplyChanges(ctx context.Context, fsys fs.FS, changes []Change, watermark string) (Stats, error) {
	converted := make([]kb.Change, 0, len(changes))
	for _, c := range changes {
		converted = append(converted, kb.Change{
			Op: kb.ChangeOp(c.Op), Path: c.Path, OldPath: c.OldPath, Blob: c.Blob, Content: c.Content,
		})
	}
	st, err := p.kb.ApplyChanges(ctx, fsys, converted, watermark)
	if err != nil {
		return Stats{}, fmt.Errorf("apply changes: %w", err)
	}
	return Stats{Added: st.Added, Updated: st.Updated, Deleted: st.Deleted, Renamed: st.Renamed, Skipped: st.Unchanged}, nil
}

// Watermark implements Store.
func (p *PandoStore) Watermark(ctx context.Context) (string, error) {
	mark, err := p.kb.Watermark(ctx)
	if err != nil {
		return "", fmt.Errorf("read the watermark: %w", err)
	}
	return mark, nil
}

// Search implements Store.
func (p *PandoStore) Search(ctx context.Context, q string, query Query) ([]Hit, error) {
	statuses := make([]string, 0, len(query.Statuses))
	for _, s := range query.Statuses {
		if s == AnyStatus {
			s = kb.AnyStatus
		}
		statuses = append(statuses, s)
	}
	hits, _, err := p.kb.Search(ctx, q, kb.Query{
		Limit: query.Limit, PathPrefixes: query.PathPrefixes, ExcludePrefixes: query.ExcludePrefixes,
		Kinds: query.Kinds, Statuses: statuses, Tags: query.Tags,
	})
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		out = append(out, Hit{
			Path: h.Path, Chunk: h.Chunk, StartLine: h.StartLine, EndLine: h.EndLine, Score: h.Score,
			Kind: h.Kind, Status: h.Status, Rev: h.Rev, GitBlob: h.GitBlob,
		})
	}
	return out, nil
}

// Close implements Store.
func (p *PandoStore) Close() error {
	_ = p.kb.Close()
	if err := p.db.Close(); err != nil {
		return fmt.Errorf("close the index: %w", err)
	}
	return nil
}
