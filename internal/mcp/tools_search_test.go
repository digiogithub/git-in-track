package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// stubSemantic is the backend the companion installs, reduced to what a tool
// test needs: it records the query it was asked and answers a fixed list. The
// ranking itself is not this package's business (internal/vault/semantic.go).
type stubSemantic struct {
	asked vault.SemanticQuery
	hits  []core.SearchHit
	err   error
}

func (s *stubSemantic) SearchSemantic(_ context.Context, q vault.SemanticQuery) ([]core.SearchHit, error) {
	s.asked = q
	return s.hits, s.err
}

// withSemantic installs a backend on the harness workspace, the way the
// companion does at startup.
func withSemantic(t *testing.T, h *harness, hits []core.SearchHit) *stubSemantic {
	t.Helper()
	backend := &stubSemantic{hits: hits}
	h.space.SetSemanticSearcher(backend)
	t.Cleanup(func() { h.space.SetSemanticSearcher(nil) })
	return backend
}

func TestSearchSemantic(t *testing.T) {
	t.Run("returns ranked candidates with the tokens to act on them", func(t *testing.T) {
		h := newHarness(t, false)
		backend := withSemantic(t, h, []core.SearchHit{
			{
				Kind: "item", ID: "DEMO-US-0001", Title: "Checkout",
				Project: "DEMO", Score: 0.82, Snippet: "…the delivery address is remembered…",
				Source: core.SearchSourcePando,
			},
			{
				Kind: "page", Path: "docs/architecture/overview.md", Title: "Overview",
				Score: 0.41, Snippet: "…how the pieces fit together…", Source: core.SearchSourcePando,
			},
		})

		got := call[SemanticHits](t, h, "search_semantic", map[string]any{
			"query": "how do we remember where to deliver", "project": "DEMO", "limit": 5,
		})

		if got.Engine != semanticEngine {
			t.Errorf("engine = %q, want %q", got.Engine, semanticEngine)
		}
		if got.Degraded {
			t.Error("the answer came back marked degraded")
		}
		if len(got.Hits) != 2 {
			t.Fatalf("hits = %+v, want two", got.Hits)
		}
		item, page := got.Hits[0], got.Hits[1]
		if item.ID != "DEMO-US-0001" || item.Score == 0 || item.Snippet == "" {
			t.Errorf("item hit = %+v, want an id, a score and a snippet", item)
		}
		if item.Rev == "" || item.Status == "" {
			t.Errorf("item hit = %+v, want the rev and status a write has to quote", item)
		}
		if page.Path == "" || page.Rev == "" {
			t.Errorf("page hit = %+v, want a path and a rev", page)
		}

		// The arguments reach the backend as the core contract spells them.
		if backend.asked.Q != "how do we remember where to deliver" {
			t.Errorf("query = %q", backend.asked.Q)
		}
		if backend.asked.Project != "DEMO" || backend.asked.Limit != 5 {
			t.Errorf("query = %+v, want the project and the limit forwarded", backend.asked)
		}
	})

	t.Run("an empty result is an empty list, not a failure", func(t *testing.T) {
		h := newHarness(t, false)
		withSemantic(t, h, nil)
		got := call[SemanticHits](t, h, "search_semantic", map[string]any{"query": "nothing like this"})
		if len(got.Hits) != 0 {
			t.Errorf("hits = %+v, want none", got.Hits)
		}
		if got.Engine != semanticEngine {
			t.Errorf("engine = %q, want %q", got.Engine, semanticEngine)
		}
	})

	t.Run("bounds and defaults the limit", func(t *testing.T) {
		h := newHarness(t, false)
		backend := withSemantic(t, h, nil)

		call[SemanticHits](t, h, "search_semantic", map[string]any{"query": "budgets"})
		if backend.asked.Limit != defaultSemanticHits {
			t.Errorf("limit = %d, want the default %d", backend.asked.Limit, defaultSemanticHits)
		}
		call[SemanticHits](t, h, "search_semantic", map[string]any{"query": "budgets", "limit": 500})
		if backend.asked.Limit != maxSemanticHits {
			t.Errorf("limit = %d, want the cap %d", backend.asked.Limit, maxSemanticHits)
		}
	})

	t.Run("a requirement hit carries its ref, spec, anchor and requirement rev", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		ref := spec + ".R2"
		withSemantic(t, h, []core.SearchHit{{
			Kind: core.SearchKindRequirement, ID: core.ItemID(ref), Title: "Reject empty postcode",
			Project: "DEMO", Spec: core.ItemID(spec), Anchor: "demo-sp-0001-r2",
			Path: "docs/.pmngr/specs/x.md", Score: 0.5, Snippet: "SHALL reject empty postcode",
			Source: core.SearchSourcePando, Index: core.SearchIndexKB,
		}})

		got := call[SemanticHits](t, h, "search_semantic", map[string]any{"query": "postcodes", "kind": "requirement"})
		if len(got.Hits) != 1 {
			t.Fatalf("hits = %+v, want one", got.Hits)
		}
		hit := got.Hits[0]
		if hit.Kind != "requirement" || hit.ID != ref || hit.Spec != spec || hit.Anchor != "demo-sp-0001-r2" {
			t.Errorf("requirement hit = %+v, want its ref, spec and anchor", hit)
		}
		read := call[ItemResult](t, h, "get_item", map[string]any{"id": ref})
		if read.Requirement == nil || hit.Rev != read.Requirement.Rev || hit.Status == "" {
			t.Errorf("requirement hit rev = %q status = %q, want the requirement rev get_item returns", hit.Rev, hit.Status)
		}
	})

	t.Run("scopes to one kind", func(t *testing.T) {
		h := newHarness(t, false)
		backend := withSemantic(t, h, nil)
		call[SemanticHits](t, h, "search_semantic", map[string]any{"query": "budgets", "kind": "page"})
		if backend.asked.Kind != "page" {
			t.Errorf("kind = %q, want page", backend.asked.Kind)
		}
	})

	t.Run("a session with no backend says so and names the fallback", func(t *testing.T) {
		h := newHarness(t, false)
		got := callFails(t, h, "search_semantic", map[string]any{"query": "how do we rotate tokens"})
		if got.Code != codeUnavailable {
			t.Fatalf("code = %q, want %q (%s)", got.Code, codeUnavailable, got.Message)
		}
		if !strings.Contains(got.Retry, "search_items") {
			t.Errorf("retry = %q, want it to name search_items", got.Retry)
		}
		if got.Message == "" {
			t.Error("the refusal carries no reason")
		}
	})

	t.Run("rejects an empty query and an unknown kind", func(t *testing.T) {
		h := newHarness(t, false)
		withSemantic(t, h, nil)
		for field, args := range map[string]map[string]any{
			"query": {"query": "   "},
			"kind":  {"query": "budgets", "kind": "epic"},
		} {
			got := callFails(t, h, "search_semantic", args)
			if got.Code != codeInvalidRequest || got.Field != field {
				t.Errorf("error = %+v, want an invalid_request on %q", got, field)
			}
		}
	})
}
