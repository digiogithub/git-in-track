package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/pando/pandotest"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// cachingPando is a real MCP server with Pando's response cache in front of
// its knowledge-base search, so these tests drive the production
// *pando.Client end to end rather than the pandoAPI fake (GIT-US-0164).
type cachingPando struct {
	srv   *httptest.Server
	cache *pandotest.Cache
}

// newCachingPando answers kb_search_documents with kb, which the cache turns
// into a stub when it is large, and code_hybrid_search with no hits.
func newCachingPando(t *testing.T, kb string) *cachingPando {
	t.Helper()
	cache := pandotest.NewCache()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "pando-fake", Version: "test"}, nil)
	cache.Register(srv)
	answer := map[string]string{
		"kb_search_documents": kb,
		"code_hybrid_search":  "No results found.",
	}
	for name, text := range answer {
		srv.AddTool(&mcpsdk.Tool{Name: name, InputSchema: map[string]any{"type": "object"}},
			func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
				res := &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}}}
				return cache.Intercept(name, res), nil
			})
	}
	h := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return srv }, nil)
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return &cachingPando{srv: ts, cache: cache}
}

// installCachingPando mounts a real Pando client on s, pointed at p.
func installCachingPando(t *testing.T, s *Server, p *cachingPando) {
	t.Helper()
	client, err := pando.New(pando.Options{MCPURL: p.srv.URL + "/mcp", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	searcher := &pandoSearcher{
		client: client, repos: s.repos, log: s.log, budget: 5 * time.Second,
		projects: s.search.codeProjects(),
	}
	s.search.mu.Lock()
	s.search.settings = config.SearchPando{MCPURL: p.srv.URL + "/mcp"}
	s.search.client, s.search.searcher = client, searcher
	s.search.mu.Unlock()
	s.search.degraded.Store(false)
	s.repos.workspace().SetSemanticSearcher(searcher)
}

// cachedKB is a knowledge-base answer well past Pando's 15,000-byte
// threshold: one backlog item first, then pages no index holds.
func cachedKB() string {
	hits := []pandotest.KBHit{{
		FilePath: ".pmngr/stories/DEMO-US-0002-save-payment-methods.md",
		Chunk:    "saved cards " + strings.Repeat("s", 1200), Score: 0.03,
	}}
	for i := range 15 {
		hits = append(hits, pandotest.KBHit{
			FilePath: fmt.Sprintf("nowhere/page-%02d.md", i),
			Chunk:    strings.Repeat("n", 1200), Score: 0.01,
		})
	}
	return pandotest.KBSearchTOON(hits...)
}

// TestSearchSemanticFollowsPandoCache covers the method search_semantic
// calls: a cached Pando answer is paged back and resolved, not reported as
// unavailable.
func TestSearchSemanticFollowsPandoCache(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	p := newCachingPando(t, cachedKB())
	installCachingPando(t, s, p)

	result, err := s.repos.workspace().Dispatch(t.Context(), "search.semantic",
		[]byte(`{"q":"saved cards","limit":8}`))
	if err != nil {
		t.Fatalf("search.semantic: %v", err)
	}
	hits, ok := result.([]vault.SearchHit)
	if !ok {
		t.Fatalf("search.semantic answered %T", result)
	}
	if len(hits) != 1 || hits[0].ID != "DEMO-US-0002" || hits[0].Source != "pando" {
		t.Fatalf("search.semantic answered %+v", hits)
	}
	if p.cache.ReadCount() == 0 {
		t.Error("the answer was never cached: the test does not exercise the stub")
	}
}

// TestWorkspaceSearchFollowsPandoCache covers GET /api/v1/search: the
// semantic leg pages a cached answer back instead of degrading to the core
// index.
func TestWorkspaceSearchFollowsPandoCache(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	p := newCachingPando(t, cachedKB())
	installCachingPando(t, s, p)

	got := searchFor(t, s, "/api/v1/search?q=wallet&limit=20")
	if got.Degraded || got.Engine != "pando" {
		t.Fatalf("engine = %q degraded = %v, want pando and not degraded", got.Engine, got.Degraded)
	}
	found := false
	for _, h := range got.Hits {
		if h.Source == "pando" && h.ID == "DEMO-US-0002" {
			found = true
		}
	}
	if !found {
		t.Errorf("the cached Pando hit did not reach the answer: %+v", got.Hits)
	}
	if p.cache.ReadCount() == 0 {
		t.Error("the answer was never cached: the test does not exercise the stub")
	}
}
