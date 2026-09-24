package trace

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// Engine keeps the trace graph of one repository working tree current and
// answers its two queries. It owns a marker Cache and memoizes the Graph
// built from it, rebuilding the graph only when the index or the scan
// changed. It is what a native host installs into a vault as its requirement
// tracer (vault.RequirementTracer); its method set matches that interface
// without this package importing internal/vault.
//
// The scan is refreshed three ways: lazily on the first query, incrementally
// from the paths a query or a caller names (Update, and every TraceTouching
// call rescans the paths of its changes first), and in full when the last
// full scan is older than Options.MaxAge — the working tree outside the docs
// folders is not watched, so an age bound is what picks up code edited by
// hand. An Engine is safe for concurrent use.
type Engine struct {
	root   string
	tree   fs.FS
	maxAge time.Duration
	now    func() time.Time

	mu       sync.Mutex
	cache    *Cache
	scanned  time.Time // last full scan; zero before the first
	gen      uint64    // bumped on every scan change
	graph    *Graph
	graphGen uint64
	graphFP  string // index fingerprint the graph was built from
}

// EngineOptions tune an Engine.
type EngineOptions struct {
	Options
	// MaxAge is how old the last full scan may be before a query rescans the
	// whole tree. Zero means never: only the first query and Update scan.
	MaxAge time.Duration
	// Now is the clock MaxAge is measured with; nil means time.Now.
	Now func() time.Time
}

// NewEngine returns an engine over the working tree at root. Nothing is
// scanned until the first query.
func NewEngine(root string, opts EngineOptions) *Engine {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Engine{
		root: root, tree: os.DirFS(root), maxAge: opts.MaxAge, now: now,
		cache: NewCache(root, opts.Options),
	}
}

// Rebuild rescans the whole working tree.
func (e *Engine) Rebuild(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rebuildLocked(ctx)
}

func (e *Engine) rebuildLocked(ctx context.Context) error {
	if err := e.cache.Rebuild(ctx); err != nil {
		return err
	}
	e.scanned = e.now()
	e.gen++
	return nil
}

// Update rescans the given repository-relative paths, typically both sides of
// every entry of a diff. An engine that never scanned scans in full instead.
func (e *Engine) Update(ctx context.Context, paths []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.scanned.IsZero() {
		return e.rebuildLocked(ctx)
	}
	if len(paths) == 0 {
		return nil
	}
	if err := e.cache.Update(ctx, paths); err != nil {
		return err
	}
	e.gen++
	return nil
}

// Graph returns the trace graph for the index as it is now, rescanning the
// tree first when it was never scanned or the last scan is older than MaxAge.
func (e *Engine) Graph(ctx context.Context, ix *core.Index) (*Graph, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.graphLocked(ctx, ix)
}

func (e *Engine) graphLocked(ctx context.Context, ix *core.Index) (*Graph, error) {
	if e.scanned.IsZero() || (e.maxAge > 0 && e.now().Sub(e.scanned) > e.maxAge) {
		if err := e.rebuildLocked(ctx); err != nil {
			return nil, err
		}
	}
	fp := ix.Fingerprint()
	if e.graph != nil && e.graphGen == e.gen && e.graphFP == fp {
		return e.graph, nil
	}
	g, err := BuildGraph(ix, e.cache.Markers(), e.tree)
	if err != nil {
		return nil, err
	}
	e.graph, e.graphGen, e.graphFP = g, e.gen, fp
	return g, nil
}

// TraceRequirement returns the trace of one requirement. A requirement the
// index does not hold is core.ErrItemNotFound.
func (e *Engine) TraceRequirement(ctx context.Context, ix *core.Index, ref core.RequirementRef) (core.TracedRequirement, error) {
	g, err := e.Graph(ctx, ix)
	if err != nil {
		return core.TracedRequirement{}, err
	}
	tr, ok := g.Requirement(ref)
	if !ok {
		return core.TracedRequirement{}, fmt.Errorf("trace %s: %w", ref, core.ErrItemNotFound)
	}
	return tr, nil
}

// TraceTouching returns the trace edges a set of changes touches (impact tier
// 1). The paths of the changes, old sides of renames included, are rescanned
// first, so a marker the change added is found, and a marker it removed is
// reported as a "removed" hit from the graph as it stood before.
func (e *Engine) TraceTouching(ctx context.Context, ix *core.Index, changes []core.TraceChange) ([]core.TraceHit, error) {
	var paths []string
	for _, c := range changes {
		if c.Path != "" {
			paths = append(paths, c.Path)
		}
		if c.OldPath != "" {
			paths = append(paths, c.OldPath)
		}
	}
	before, err := e.Graph(ctx, ix)
	if err != nil {
		return nil, err
	}
	if err := e.Update(ctx, paths); err != nil {
		return nil, err
	}
	g, err := e.Graph(ctx, ix)
	if err != nil {
		return nil, err
	}
	hits := g.TouchingSince(before, changes, e.tree)
	if hits == nil {
		hits = []core.TraceHit{}
	}
	return hits, nil
}

// Tree returns the working tree the engine scans, rooted at the repository
// root. The impact query (GIT-US-0119) reads changed files and callers from
// the same tree, so a line maps to the same symbol everywhere.
func (e *Engine) Tree() fs.FS { return e.tree }
