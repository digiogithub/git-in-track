package server

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// fakePando stands in for a live Pando instance. Every test of the semantic
// half drives one of these: the searcher's contract is "candidates in, resolved
// hits out", and nothing about it needs a running MCP server.
type fakePando struct {
	mu sync.Mutex

	hits      []pando.KBHit
	searchErr error
	healthErr error
	// block, when set, holds SearchKB until it is closed or the context ends,
	// which is how the latency budget is exercised.
	block chan struct{}

	lastQuery string
	lastOpts  pando.KBSearchOptions
	searches  int

	indexed    []string
	indexErr   error
	reindex    pando.ReindexStats
	reindexErr error
	// indexGate holds IndexProject open so that a test can observe a reindex
	// while it is still running.
	indexGate chan struct{}
}

func (f *fakePando) Health(context.Context) error { return f.healthErr }

func (f *fakePando) SearchKB(ctx context.Context, q string, o pando.KBSearchOptions) ([]pando.KBHit, error) {
	f.mu.Lock()
	f.lastQuery, f.lastOpts, f.searches = q, o, f.searches+1
	block, hits, err := f.block, f.hits, f.searchErr
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, pando.ErrTimeout
		}
	}
	return hits, err
}

func (f *fakePando) IndexProject(ctx context.Context, path, _ string) (string, error) {
	f.mu.Lock()
	gate := f.indexGate
	f.indexed = append(f.indexed, path)
	err := f.indexErr
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
		}
	}
	if err != nil {
		return "", err
	}
	return "job-1", nil
}

func (f *fakePando) ReindexKB(context.Context) (pando.ReindexStats, error) {
	return f.reindex, f.reindexErr
}

func (f *fakePando) Close() error { return nil }

func (f *fakePando) options() pando.KBSearchOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastOpts
}

// installPando mounts a fake upstream on a server built by newAPIServer, the
// way newSearchState would mount a real client.
func installPando(t *testing.T, s *Server, f *fakePando) {
	t.Helper()

	searcher := &pandoSearcher{client: f, repos: s.repos, log: s.log, budget: 200 * time.Millisecond}
	s.search.mu.Lock()
	s.search.settings = config.SearchPando{MCPURL: "http://127.0.0.1:9777/mcp"}
	s.search.client, s.search.searcher = f, searcher
	s.search.mu.Unlock()
	s.search.degraded.Store(false)
	s.repos.workspace().SetSemanticSearcher(searcher)
}

// searchResponse is the envelope of GET /api/v1/search (GIT-US-0082).
type searchResponse struct {
	Query string `json:"query"`
	Hits  []struct {
		Kind    string  `json:"kind"`
		ID      string  `json:"id"`
		Path    string  `json:"path"`
		Title   string  `json:"title"`
		Snippet string  `json:"snippet"`
		Score   float64 `json:"score"`
		Source  string  `json:"source"`
		Project string  `json:"project"`
		VaultID string  `json:"vaultId"`
	} `json:"hits"`
	Engine   string `json:"engine"`
	Degraded bool   `json:"degraded"`
}

func searchFor(t *testing.T, s *Server, target string) searchResponse {
	t.Helper()

	var out searchResponse
	decode(t, send(t, s, request{method: http.MethodGet, target: target}), http.StatusOK, &out)
	return out
}

func TestParseCorpusPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		in     string
		want   corpusRef
		wantOK bool
	}{
		{"an item", "DEMO/items/DEMO-US-0001.md", corpusRef{"item", "DEMO", "DEMO-US-0001"}, true},
		{"a page", "DEMO/kb/architecture/overview.md", corpusRef{"page", "DEMO", "architecture/overview.md"}, true},
		{
			"a path Pando reported with a prefix",
			"/var/cache/pando-kb/demo/DEMO/items/DEMO-T-0001.md",
			corpusRef{"item", "DEMO", "DEMO-T-0001"}, true,
		},
		{"a nested item is not the layout", "DEMO/items/sub/DEMO-T-0001.md", corpusRef{}, false},
		{"a document outside the layout", "notes/scratch.md", corpusRef{}, false},
		{"something that is not Markdown", "DEMO/items/DEMO-T-0001.txt", corpusRef{}, false},
		{"nothing at all", "", corpusRef{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseCorpusPath(tc.in)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("parseCorpusPath(%q) = %+v, %v; want %+v, %v", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestSearchMergesSemanticHitsAfterExactOnes(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	installPando(t, s, &fakePando{hits: []pando.KBHit{
		// A story the substring engine cannot find for this query, which is the
		// whole point of the accelerator.
		{FilePath: "DEMO/items/DEMO-US-0002.md", Chunk: "saved cards are\n  reused at checkout", Score: 0.019, Rank: 1},
		// A page it can find, so the merge has something to deduplicate.
		{FilePath: "DEMO/kb/architecture/overview.md", Chunk: "the checkout flow", Score: 0.017, Rank: 2},
		// A document whose source is gone: dropped, never shown dangling.
		{FilePath: "DEMO/items/DEMO-US-9999.md", Chunk: "vanished", Score: 0.016, Rank: 3},
		// A path that is not the corpus layout at all.
		{FilePath: "scratch/notes.md", Chunk: "unrelated", Score: 0.015, Rank: 4},
	}})

	got := searchFor(t, s, "/api/v1/search?q=checkout&limit=10")
	if got.Degraded {
		t.Error("a healthy Pando must not report a degraded answer")
	}
	if got.Engine != "pando" {
		t.Errorf("engine = %q, want pando", got.Engine)
	}

	firstSemantic := -1
	seen := map[string]int{}
	for i, hit := range got.Hits {
		if hit.Source == "pando" && firstSemantic < 0 {
			firstSemantic = i
		}
		if hit.Source == "core" && firstSemantic >= 0 {
			t.Errorf("hit %d (%s) is exact but comes after a semantic one", i, hit.Path)
		}
		key := hit.Kind + hit.ID + hit.Path
		seen[key]++
		if seen[key] > 1 {
			t.Errorf("hit %s is in the result twice", key)
		}
	}
	if firstSemantic < 0 {
		t.Fatalf("no semantic hit survived the merge: %+v", got.Hits)
	}
	semantic := got.Hits[firstSemantic]
	if semantic.ID != "DEMO-US-0002" {
		t.Errorf("the semantic hit is %q, want DEMO-US-0002", semantic.ID)
	}
	// Authoritative fields come from the local index, never from the corpus:
	// the title is the one in the repository, not the chunk Pando returned.
	if semantic.Title != "Save payment methods" {
		t.Errorf("title = %q, want the title the local index holds", semantic.Title)
	}
	if semantic.Project != "DEMO" || semantic.VaultID != testRepoID {
		t.Errorf("hit does not name its project and repository: %+v", semantic)
	}
	if semantic.Snippet == "" {
		t.Error("the semantic hit carries no snippet")
	}
	if semantic.Score != 0.019 {
		t.Errorf("score = %v, want Pando's own", semantic.Score)
	}
	for _, hit := range got.Hits {
		if hit.ID == "DEMO-US-9999" || hit.Path == "scratch/notes.md" {
			t.Errorf("an unresolvable candidate reached the result: %+v", hit)
		}
	}
}

func TestSearchOverFetchesAndScopesByPathPrefix(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	fake := &fakePando{}
	installPando(t, s, fake)

	searchFor(t, s, "/api/v1/search?q=checkout&project=DEMO&limit=5")
	opts := fake.options()
	if opts.Limit != 10 {
		t.Errorf("limit = %d, want the caller's limit doubled", opts.Limit)
	}
	if opts.PathPrefix != "DEMO/" {
		t.Errorf("pathPrefix = %q, want the project's corpus prefix", opts.PathPrefix)
	}

	searchFor(t, s, "/api/v1/search?q=checkout&limit=50")
	opts = fake.options()
	if opts.Limit != pandoSearchCap {
		t.Errorf("limit = %d, want it clamped to Pando's cap of %d", opts.Limit, pandoSearchCap)
	}
	if opts.PathPrefix != "" {
		t.Errorf("an unscoped query sent the prefix %q", opts.PathPrefix)
	}
}

func TestSearchDegradesWhenPandoFails(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		fake         *fakePando
		wantDegraded bool
		wantEngine   string
	}{
		"unreachable":  {&fakePando{searchErr: pando.ErrUnreachable}, true, "core"},
		"unauthorized": {&fakePando{searchErr: pando.ErrUnauthorized}, true, "core"},
		"switched off": {&fakePando{searchErr: pando.ErrNotConfigured}, false, "pando"},
		"too slow":     {&fakePando{block: make(chan struct{})}, true, "core"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, _ := newAPIServer(t)
			installPando(t, s, tc.fake)

			got := searchFor(t, s, "/api/v1/search?q=checkout&limit=10")
			if got.Degraded != tc.wantDegraded {
				t.Errorf("degraded = %v, want %v", got.Degraded, tc.wantDegraded)
			}
			if len(got.Hits) == 0 {
				t.Fatal("a Pando failure must still answer from the core index")
			}
			for _, hit := range got.Hits {
				if hit.Source != "core" {
					t.Errorf("hit %s survived a failed semantic leg", hit.Path)
				}
			}
			// The capability follows the backend that will actually answer the
			// next query, so a card never promises an accelerator that is down.
			var caps struct {
				Features struct {
					Search string `json:"search"`
				} `json:"features"`
			}
			decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/capabilities"}),
				http.StatusOK, &caps)
			if caps.Features.Search != tc.wantEngine {
				t.Errorf("features.search = %q, want %q", caps.Features.Search, tc.wantEngine)
			}
		})
	}
}

func TestSearchRecoversAfterADegradation(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	fake := &fakePando{searchErr: pando.ErrUnreachable}
	installPando(t, s, fake)

	if got := searchFor(t, s, "/api/v1/search?q=checkout"); !got.Degraded {
		t.Fatal("the first query must report the degradation")
	}
	fake.mu.Lock()
	fake.searchErr = nil
	fake.hits = []pando.KBHit{{FilePath: "DEMO/items/DEMO-US-0002.md", Chunk: "saved cards", Score: 0.02}}
	fake.mu.Unlock()

	got := searchFor(t, s, "/api/v1/search?q=checkout")
	if got.Degraded {
		t.Error("a recovered Pando must not keep reporting a degraded answer")
	}
	if got.Engine != "pando" {
		t.Errorf("engine = %q, want pando once the accelerator answers again", got.Engine)
	}
}

func TestSearchWithoutPandoIsUnchanged(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	got := searchFor(t, s, "/api/v1/search?q=checkout&limit=10")
	if got.Engine != "core" || got.Degraded {
		t.Errorf("engine = %q degraded = %v, want core and not degraded", got.Engine, got.Degraded)
	}
	if len(got.Hits) == 0 {
		t.Fatal("the core index found nothing")
	}
	if s.search.semantic() != nil {
		t.Error("a companion with no Pando configured must install no semantic backend")
	}
}

func TestSemanticSearchIsOnTheCoreContract(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)

	// Without a backend the method says so, rather than answering an empty
	// result a caller would read as "nothing matched".
	if _, err := s.repos.workspace().Dispatch(t.Context(), "search.semantic",
		[]byte(`{"q":"stale write"}`)); err == nil {
		t.Fatal("search.semantic answered without a backend installed")
	}

	installPando(t, s, &fakePando{hits: []pando.KBHit{
		{FilePath: "DEMO/items/DEMO-US-0002.md", Chunk: "saved cards", Score: 0.02},
	}})
	result, err := s.repos.workspace().Dispatch(t.Context(), "search.semantic",
		[]byte(`{"q":"stale write","limit":5,"project":"DEMO"}`))
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
	if hits[0].VaultID != testRepoID {
		t.Errorf("the hit does not name its repository: %+v", hits[0])
	}
}
