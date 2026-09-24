package vault

import (
	"context"
	"fmt"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the host seam for semantic search. The ranking itself can never
// live here: it needs a network call to a Pando instance, and this package
// compiles to WebAssembly next to internal/core (ADR-003). So the companion
// installs a backend and the contract gains one method; a browser-only session
// installs none and the method answers `unavailable`, which is a different
// thing from an error.
//
// The backend returns candidates only. Every field a caller shows is re-read
// from this workspace's own index by the backend before it answers, so nothing
// Pando's copy of a document holds can reach a user as if it were backlog
// state (GIT-US-0082).

// SemanticQuery is the input of the "search.semantic" method.
type SemanticQuery struct {
	// Q is the natural-language query. An empty one returns no hits.
	Q string `json:"q"`
	// Limit caps the hits. Zero means the backend's own default; a backend may
	// clamp it to what its upstream can return.
	Limit int `json:"limit,omitempty"`
	// Project scopes the query to one project key. Empty searches every
	// project of every mounted repository.
	Project string `json:"project,omitempty"`
	// Projects scopes the query to any of several project keys, next to
	// Project (GIT-US-0102). A hit naming no project — a plain file — is
	// outside any non-empty scope.
	Projects []string `json:"projects,omitempty"`
	// Kind scopes the query to "item", "page" or "requirement". Empty
	// searches every kind. A hit inside a spec resolves to the requirement
	// block it landed in unless Kind is "item", which keeps the spec itself
	// (GIT-US-0118).
	Kind string `json:"kind,omitempty"`
}

// Admits reports whether a hit of project key is inside the query's scope.
func (q SemanticQuery) Admits(key core.ProjectKey) bool {
	return InScope(ScopeKeys(q.Project, q.Projects), string(key))
}

// SemanticSearcher is the backend a host installs with
// [Workspace.SetSemanticSearcher]. The companion's implementation wraps
// internal/pando; a test installs a stub.
//
// An implementation must resolve every candidate back to a live document of
// this workspace — an item, a page, or a plain file it owns neither way — and
// drop the ones that no longer resolve, and it must set
// [core.SearchHit.Source] to [core.SearchSourcePando].
type SemanticSearcher interface {
	SearchSemantic(ctx context.Context, q SemanticQuery) ([]core.SearchHit, error)
}

// SetSemanticSearcher installs the semantic backend. Passing nil removes it,
// which is what the browser leaves in place.
func (w *Workspace) SetSemanticSearcher(s SemanticSearcher) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.semantic = s
}

// semanticSearcher returns the installed backend, nil when there is none.
func (w *Workspace) semanticSearcher() SemanticSearcher {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.semantic
}

// SemanticAvailable reports whether a backend is installed. It is what a caller
// asks before offering semantic search at all, rather than probing with a
// query that would fail.
func (w *Workspace) SemanticAvailable() bool { return w.semanticSearcher() != nil }

// SearchSemantic answers the "search.semantic" method: ranked-by-meaning
// candidates, resolved back into this workspace's index, each one carrying the
// repository it was found in.
//
// Scores come from the backend's own scale and are comparable only with each
// other — never with the field weights of the substring index (docs/02 §8).
func (w *Workspace) SearchSemantic(ctx context.Context, q SemanticQuery) ([]searchHit, error) {
	backend := w.semanticSearcher()
	if backend == nil {
		return nil, failf("unavailable",
			"semantic search is not configured: this session has no Pando backend")
	}
	hits, err := backend.SearchSemantic(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}
	out := make([]searchHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, w.semanticHit(h))
	}
	return out, nil
}

// semanticHit renders one backend hit on the wire, naming the repository that
// holds it so a workspace-wide answer is never ambiguous (GIT-US-0016).
func (w *Workspace) semanticHit(h core.SearchHit) searchHit {
	source := h.Source
	if source == "" {
		source = core.SearchSourcePando
	}
	out := searchHit{
		Kind: h.Kind, ID: string(h.ID), Path: h.Path, Title: h.Title,
		Snippet: h.Snippet, Score: h.Score, Project: string(h.Project), Source: source,
		Index: h.Index, Match: h.Match, MoreMatches: h.MoreMatches,
		Spec: string(h.Spec), Status: string(h.Status), Anchor: h.Anchor,
	}
	if m, ok := w.MountForProject(h.Project); ok {
		out.VaultID = m.ID
	}
	return out
}
