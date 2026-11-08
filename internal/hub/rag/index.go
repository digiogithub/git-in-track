package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// Repo is one repository the indexer follows: its mount id and working tree.
type Repo struct {
	ID  string
	Dir string
}

// Indexer keeps every repository's Store, and the vault's own index, in step
// with git by commit range. It replaces the file watcher in the hub: what a
// pull brings in, and what the hub commits, is applied from the diff between
// the commit last applied and HEAD.
type Indexer struct {
	// Git is the git executable; empty means "git".
	Git string
	// Open returns the store of a repository; it is called once per repo.
	Open func(repo Repo) (Store, error)
	// Apply folds file changes into the vault index and announces them
	// (server.ApplyFileEvents). Nil skips the vault half.
	Apply func(ctx context.Context, repo string, events []core.FileEvent) error
	Log   *slog.Logger

	mu     sync.Mutex
	stores map[string]Store
	heads  map[string]string
	dirs   map[string]string
}

// dir is the working tree of a repository, "" before its first Sync.
func (ix *Indexer) dir(repo string) string {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.dirs[repo]
}

// Store returns the store of a repository, nil before its first Sync.
func (ix *Indexer) Store(repo string) Store {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.stores[repo]
}

// Sync brings every repository up to its HEAD. The vault half starts from the
// HEAD seen at the first Sync, because the vault indexed the working tree when
// it was mounted; the store half starts from its own watermark, so a restart
// resumes where the store left off instead of reindexing everything.
func (ix *Indexer) Sync(ctx context.Context, repos []Repo) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.stores == nil {
		ix.stores, ix.heads, ix.dirs = map[string]Store{}, map[string]string{}, map[string]string{}
	}
	for _, r := range repos {
		ix.dirs[r.ID] = r.Dir
	}
	var firstErr error
	for _, r := range repos {
		if err := ix.syncOne(ctx, r); err != nil {
			ix.log().Warn("semantic index update failed", "repo", r.ID, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (ix *Indexer) syncOne(ctx context.Context, r Repo) error {
	head, err := ix.git(ctx, r.Dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if prev, ok := ix.heads[r.ID]; ok && prev != head && ix.Apply != nil {
		all, err := ix.diff(ctx, r.Dir, prev, head)
		if err != nil {
			return err
		}
		if err := ix.Apply(ctx, r.ID, fileEvents(all)); err != nil {
			return fmt.Errorf("apply to the vault: %w", err)
		}
	}
	ix.heads[r.ID] = head

	store, ok := ix.stores[r.ID]
	if !ok {
		if store, err = ix.Open(r); err != nil {
			return fmt.Errorf("open the store: %w", err)
		}
		ix.stores[r.ID] = store
	}
	mark, err := store.Watermark(ctx)
	if err != nil || mark == head {
		return err //nolint:wrapcheck // the store names its own failure
	}
	var changes []Change
	if mark == "" {
		changes, err = ix.tree(ctx, r.Dir, head)
	} else {
		changes, err = ix.diff(ctx, r.Dir, mark, head)
	}
	if err != nil {
		return err
	}
	blobs, err := ix.blobs(ctx, r.Dir, head)
	if err != nil {
		return err
	}
	var markdown []Change
	for _, c := range changes {
		if strings.HasSuffix(c.Path, ".md") || strings.HasSuffix(c.OldPath, ".md") {
			c.Blob = blobs[c.Path]
			markdown = append(markdown, c)
		}
	}
	_, err = store.ApplyChanges(ctx, os.DirFS(r.Dir), markdown, head)
	return err //nolint:wrapcheck // the store names its own failure
}

// tree lists every file of a commit as an add.
func (ix *Indexer) tree(ctx context.Context, dir, commit string) ([]Change, error) {
	out, err := ix.git(ctx, dir, "ls-tree", "-r", "--name-only", commit)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for _, p := range strings.Split(out, "\n") {
		if p != "" {
			changes = append(changes, Change{Op: OpAdd, Path: p})
		}
	}
	return changes, nil
}

// diff lists what changed between two commits, renames detected.
func (ix *Indexer) diff(ctx context.Context, dir, from, to string) ([]Change, error) {
	out, err := ix.git(ctx, dir, "diff", "--name-status", "-M", from, to)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		switch status := fields[0]; {
		case status == "A":
			changes = append(changes, Change{Op: OpAdd, Path: fields[1]})
		case status == "D":
			changes = append(changes, Change{Op: OpDelete, Path: fields[1]})
		case strings.HasPrefix(status, "R") && len(fields) == 3:
			changes = append(changes, Change{Op: OpRename, OldPath: fields[1], Path: fields[2]})
		default:
			changes = append(changes, Change{Op: OpModify, Path: fields[len(fields)-1]})
		}
	}
	return changes, nil
}

// blobs maps every path of a commit to its blob id.
func (ix *Indexer) blobs(ctx context.Context, dir, commit string) (map[string]string, error) {
	out, err := ix.git(ctx, dir, "ls-tree", "-r", commit)
	if err != nil {
		return nil, err
	}
	blobs := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		meta, p, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		if fields := strings.Fields(meta); len(fields) == 3 {
			blobs[p] = fields[2]
		}
	}
	return blobs, nil
}

func (ix *Indexer) git(ctx context.Context, dir string, args ...string) (string, error) {
	bin := ix.Git
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (ix *Indexer) log() *slog.Logger {
	if ix.Log == nil {
		return slog.Default()
	}
	return ix.Log
}

// fileEvents turns a diff into the vault's file events.
func fileEvents(changes []Change) []core.FileEvent {
	out := make([]core.FileEvent, 0, len(changes))
	for _, c := range changes {
		switch c.Op {
		case OpDelete:
			out = append(out, core.FileEvent{Kind: core.FileRemoved, Path: c.Path})
		case OpRename:
			out = append(out, core.FileEvent{Kind: core.FileRemoved, Path: c.OldPath},
				core.FileEvent{Kind: core.FileCreated, Path: c.Path})
		case OpAdd:
			out = append(out, core.FileEvent{Kind: core.FileCreated, Path: c.Path})
		default:
			out = append(out, core.FileEvent{Kind: core.FileModified, Path: c.Path})
		}
	}
	return out
}

// Searcher answers search.semantic over the indexer's stores. Every hit is
// resolved back to the live item or page by its rev, and dropped when the
// document changed since it was indexed, so nothing a store holds reaches a
// caller as if it were current (vault/semantic.go).
type Searcher struct {
	Indexer *Indexer
	// Space returns the workspace, read lazily.
	Space func() *vault.Workspace
}

// SearchSemantic implements vault.SemanticSearcher.
func (s *Searcher) SearchSemantic(ctx context.Context, q vault.SemanticQuery) ([]core.SearchHit, error) {
	if strings.TrimSpace(q.Q) == "" {
		return nil, nil
	}
	var out []core.SearchHit
	for _, m := range s.Space().Mounts() {
		store := s.Indexer.Store(m.ID)
		if store == nil {
			continue
		}
		var prefixes []string
		for _, ref := range m.Vault.Projects() {
			if q.Admits(ref.Key) {
				prefixes = append(prefixes, ref.DocsPath)
			}
		}
		if len(prefixes) == 0 {
			continue
		}
		query := Query{Limit: q.Limit, PathPrefixes: prefixes, Statuses: []string{AnyStatus}}
		switch q.Kind {
		case "page":
			query.Kinds = []string{"page"}
		case "item":
			query.Kinds = []string{"epic", "story", "task", "milestone", "spec"}
		}
		hits, err := store.Search(ctx, q.Q, query)
		if err != nil {
			return nil, fmt.Errorf("search %s: %w", m.ID, err)
		}
		dir := s.Indexer.dir(m.ID)
		for _, h := range hits {
			if hit, ok := resolve(m, dir, h); ok {
				out = append(out, hit)
			}
		}
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// resolve maps a hit to the live document, or reports false when the file on
// disk is no longer what the store indexed. The store's rev is compared with
// the live content in both spellings a store may use: the core's content
// hash, and pando/rag's sha256 in hex.
func resolve(m *vault.Mount, dir string, h Hit) (core.SearchHit, bool) {
	live, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(h.Path)))
	if err != nil || (string(core.ComputeRev(live)) != h.Rev && sha256Hex(live) != h.Rev) {
		return core.SearchHit{}, false
	}
	if id := core.IDFromFileName(h.Path); id != "" {
		it, ok := m.Vault.Item(id)
		if !ok || it.Path != h.Path {
			return core.SearchHit{}, false
		}
		key, _, _, _ := core.ParseItemID(string(it.ID))
		return core.SearchHit{Kind: "item", ID: it.ID, Path: it.Path, Title: it.Title, Project: key,
			Score: h.Score, Snippet: h.Chunk, Source: core.SearchSourcePando}, true
	}
	page, ok := m.Vault.Page(h.Path)
	if !ok {
		return core.SearchHit{}, false
	}
	return core.SearchHit{Kind: "page", Path: page.Path, Title: page.Title, Project: page.Project,
		Score: h.Score, Snippet: h.Chunk, Source: core.SearchSourcePando}, true
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
