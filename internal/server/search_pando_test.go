package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	indexed  []string
	indexErr error
	// codeHits is what SearchCode answers, per project id; codeErr fails it.
	codeHits map[string][]pando.CodeHit
	codeErr  error
	// projects is what ListProjects answers, and projectsErr fails it.
	projects    []pando.Project
	projectsErr error
	// lastCode records the options of the last code search.
	lastCode pando.CodeSearchOptions
	// codeSearched names every project the code leg was asked about.
	codeSearched []string
	reindex      pando.ReindexStats
	reindexErr   error
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

func (f *fakePando) SearchCode(
	ctx context.Context, projectID, _ string, o pando.CodeSearchOptions,
) ([]pando.CodeHit, error) {
	f.mu.Lock()
	f.lastCode = o
	f.codeSearched = append(f.codeSearched, projectID)
	block, hits, err := f.block, f.codeHits[projectID], f.codeErr
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

func (f *fakePando) ListProjects(context.Context) ([]pando.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.projects, f.projectsErr
}

func (f *fakePando) indexedProjects() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.indexed...)
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

	searcher := &pandoSearcher{
		client: f, repos: s.repos, log: s.log, budget: 200 * time.Millisecond,
		projects: s.search.codeProjects(),
	}
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
		Kind        string  `json:"kind"`
		ID          string  `json:"id"`
		Path        string  `json:"path"`
		Title       string  `json:"title"`
		Snippet     string  `json:"snippet"`
		Score       float64 `json:"score"`
		Source      string  `json:"source"`
		Project     string  `json:"project"`
		VaultID     string  `json:"vaultId"`
		Index       string  `json:"index"`
		Match       string  `json:"match"`
		MoreMatches int     `json:"moreMatches"`
	} `json:"hits"`
	Engine   string `json:"engine"`
	Degraded bool   `json:"degraded"`
	Dropped  int    `json:"dropped"`
}

func searchFor(t *testing.T, s *Server, target string) searchResponse {
	t.Helper()

	var out searchResponse
	decode(t, send(t, s, request{method: http.MethodGet, target: target}), http.StatusOK, &out)
	return out
}

// TestParsePandoPath pins the three shapes a hit's path can have now that
// Pando indexes the repository itself: a backlog file, a comment on one, and
// everything else, which only the index can tell apart into a page and a
// foreign file (GIT-US-0096).
func TestParsePandoPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want pandoRef
	}{
		{
			"an item",
			".pmngr/stories/DEMO-US-0001-guest-checkout.md",
			pandoRef{kind: "item", id: "DEMO-US-0001", path: ".pmngr/stories/DEMO-US-0001-guest-checkout.md"},
		},
		{
			"a project key with a digit in it",
			".pmngr/tasks/A1-T-0007-wire-the-gateway.md",
			pandoRef{kind: "item", id: "A1-T-0007", path: ".pmngr/tasks/A1-T-0007-wire-the-gateway.md"},
		},
		{
			"a slug with a dot in it",
			".pmngr/tasks/DEMO-T-0001-upgrade-to-v1.2.3.md",
			pandoRef{kind: "item", id: "DEMO-T-0001", path: ".pmngr/tasks/DEMO-T-0001-upgrade-to-v1.2.3.md"},
		},
		{
			"a comment, which is its item",
			".pmngr/comments/DEMO-US-0001/20260901T104512Z-marta.md",
			pandoRef{
				kind: "item", id: "DEMO-US-0001", comment: true,
				path: ".pmngr/comments/DEMO-US-0001/20260901T104512Z-marta.md",
			},
		},
		{
			"an absolute source_path",
			"/home/dana/src/demo/docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md",
			pandoRef{
				kind: "item", id: "DEMO-US-0001",
				path: "/home/dana/src/demo/docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md",
			},
		},
		{
			"a page",
			"architecture/overview.md",
			pandoRef{kind: "page", path: "architecture/overview.md"},
		},
		{
			"a page at the documentation root",
			"index.md",
			pandoRef{kind: "page", path: "index.md"},
		},
		{
			"a backlog file that names no item",
			".pmngr/project.yaml",
			pandoRef{kind: "file", path: ".pmngr/project.yaml"},
		},
		{
			"a comment directory that is not an item id",
			".pmngr/comments/not-an-id/20260901T104512Z-marta.md",
			pandoRef{kind: "file", path: ".pmngr/comments/not-an-id/20260901T104512Z-marta.md"},
		},
		{
			"an item nested deeper than the layout",
			".pmngr/stories/sub/DEMO-T-0001-nested.md",
			pandoRef{kind: "file", path: ".pmngr/stories/sub/DEMO-T-0001-nested.md"},
		},
		{"nothing at all", "", pandoRef{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parsePandoPath(tc.in); got != tc.want {
				t.Fatalf("parsePandoPath(%q) = %+v; want %+v", tc.in, got, tc.want)
			}
		})
	}
}

// TestSemanticHitsResolveAgainstTheRepository walks every shape end to end
// against the fixture, which is the only way to tell a page from a foreign
// file: the parser cannot, and the index must.
func TestSemanticHitsResolveAgainstTheRepository(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	installPando(t, s, &fakePando{hits: []pando.KBHit{
		{FilePath: ".pmngr/stories/DEMO-US-0002-save-payment-methods.md", Chunk: "saved cards", Score: 0.03, Rank: 1},
		{FilePath: "architecture/overview.md", Chunk: "the checkout flow", Score: 0.02, Rank: 2},
		// No file_path at all: only Pando's absolute source_path.
		{
			Metadata: map[string]any{
				"source_path": filepath.Join(root, "docs", ".pmngr", "tasks",
					"DEMO-T-0001-add-address-validation.md"),
			},
			Chunk: "validate the address", Score: 0.019, Rank: 3,
		},
		// Under the documentation root but neither backlog nor page.
		{FilePath: "notes/scratch.txt", Chunk: "a loose note", Score: 0.01, Rank: 4},
	}})
	writeFixtureFile(t, filepath.Join(root, "docs", "notes", "scratch.txt"), "a loose note\n")

	got := searchFor(t, s, "/api/v1/search?q=checkout&limit=20")
	byPath := map[string]int{}
	for i, hit := range got.Hits {
		byPath[hit.Path] = i
	}

	item, ok := byPath["docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md"]
	if !ok {
		t.Fatalf("the item hit did not resolve: %+v", got.Hits)
	}
	if h := got.Hits[item]; h.Kind != "item" || h.ID != "DEMO-US-0002" ||
		h.Title != "Save payment methods" || h.Project != "DEMO" || h.VaultID != testRepoID {
		t.Errorf("the item hit does not carry the index's own fields: %+v", got.Hits[item])
	}

	page, ok := byPath["docs/architecture/overview.md"]
	if !ok {
		t.Fatalf("the page hit did not resolve: %+v", got.Hits)
	}
	if h := got.Hits[page]; h.Kind != "page" || h.Title == "" {
		t.Errorf("the page hit does not carry the page: %+v", got.Hits[page])
	}

	task, ok := byPath["docs/.pmngr/tasks/DEMO-T-0001-add-address-validation.md"]
	if !ok {
		t.Fatalf("the absolute source_path did not resolve: %+v", got.Hits)
	}
	if h := got.Hits[task]; h.Kind != "item" || h.ID != "DEMO-T-0001" {
		t.Errorf("the source_path hit resolved to %+v", got.Hits[task])
	}

	loose, ok := byPath["notes/scratch.txt"]
	if !ok {
		t.Fatalf("a document that is neither item nor page vanished: %+v", got.Hits)
	}
	if h := got.Hits[loose]; h.Kind != "file" || h.ID != "" || h.Title != "" || h.Snippet == "" {
		t.Errorf("the foreign hit is not a plain file result: %+v", got.Hits[loose])
	}
	if got.Dropped != 0 {
		t.Errorf("dropped = %d, want nothing dropped", got.Dropped)
	}
}

// A project scope reaches the semantic half: candidates outside it — here a
// plain file, which names no project — are dropped (GIT-US-0102).
func TestSemanticHitsHonourTheProjectScope(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	installPando(t, s, &fakePando{hits: []pando.KBHit{
		{FilePath: ".pmngr/stories/DEMO-US-0002-save-payment-methods.md", Chunk: "saved cards", Score: 0.03, Rank: 1},
		{FilePath: "notes/scratch.txt", Chunk: "a loose note", Score: 0.01, Rank: 2},
	}})
	writeFixtureFile(t, filepath.Join(root, "docs", "notes", "scratch.txt"), "a loose note\n")

	scoped := searchFor(t, s, "/api/v1/search?q=checkout&limit=20&project=DEMO")
	sawItem := false
	for _, hit := range scoped.Hits {
		if hit.Project != "DEMO" {
			t.Errorf("a scoped search returned %+v", hit)
		}
		sawItem = sawItem || hit.ID == "DEMO-US-0002"
	}
	if !sawItem {
		t.Errorf("the in-scope semantic hit is missing: %+v", scoped.Hits)
	}
}

// TestSemanticCommentHitsCollapseOntoTheirItem pins the maintainer's decision:
// a comment hit is the item it belongs to, marked as a comment match, and a
// thread that answers the query several times is one row with a count
// (GIT-US-0096).
func TestSemanticCommentHitsCollapseOntoTheirItem(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	installPando(t, s, &fakePando{hits: []pando.KBHit{
		{
			FilePath: ".pmngr/comments/DEMO-US-0001/20260901T104512Z-marta.md",
			Chunk:    "we rejected the wallet because", Score: 0.04, Rank: 1,
		},
		{
			FilePath: ".pmngr/comments/DEMO-US-0001/20260902T090000Z-dana.md",
			Chunk:    "the second comment on the same item", Score: 0.03, Rank: 2,
		},
		// A comment directory whose item is not in the index: dropped and
		// counted, never a dangling row and never a failed search.
		{
			FilePath: ".pmngr/comments/DEMO-US-9999/20260902T090000Z-dana.md",
			Chunk:    "on an item that is gone", Score: 0.02, Rank: 3,
		},
	}})

	got := searchFor(t, s, "/api/v1/search?q=wallet&limit=20")
	var found int
	for _, hit := range got.Hits {
		if hit.Source != "pando" {
			continue
		}
		found++
		if hit.ID != "DEMO-US-0001" || hit.Kind != "item" {
			t.Errorf("a comment hit did not resolve to its item: %+v", hit)
		}
		if hit.Match != "comment" {
			t.Errorf("match = %q, want the hit marked as a comment match", hit.Match)
		}
		if hit.MoreMatches != 1 {
			t.Errorf("moreMatches = %d, want the second comment counted", hit.MoreMatches)
		}
		if hit.Snippet == "" || !strings.Contains(hit.Snippet, "rejected the wallet") {
			t.Errorf("the hit does not carry the best-scoring fragment: %q", hit.Snippet)
		}
	}
	if found != 1 {
		t.Fatalf("the thread produced %d semantic rows, want exactly one", found)
	}
	if got.Dropped != 1 {
		t.Errorf("dropped = %d, want the comment on a deleted item counted", got.Dropped)
	}
}

func TestSearchMergesSemanticHitsAfterExactOnes(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	installPando(t, s, &fakePando{hits: []pando.KBHit{
		// A story the substring engine cannot find for this query, which is the
		// whole point of the accelerator.
		{FilePath: ".pmngr/stories/DEMO-US-0002-save-payment-methods.md", Chunk: "saved cards are\n  reused at checkout", Score: 0.019, Rank: 1},
		// A page it can find, so the merge has something to deduplicate.
		{FilePath: "architecture/overview.md", Chunk: "the checkout flow", Score: 0.017, Rank: 2},
		// A document whose source is gone: dropped, never shown dangling.
		{FilePath: ".pmngr/stories/DEMO-US-9999-vanished.md", Chunk: "vanished", Score: 0.016, Rank: 3},
		// A path no longer in the repository at all.
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
	// Authoritative fields come from the local index, never from Pando: the
	// title is the one in the repository, not the chunk Pando returned.
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

// TestSearchOverFetchesWithoutAPathPrefix pins the other half of the scope
// decision: the repository layout gives a project no path prefix of its own,
// and a prefix built here would match nothing at all against the absolute
// source_path Pando reports for some documents. So the over-fetch is the whole
// filter, and the scope is applied on resolved hits.
func TestSearchOverFetchesWithoutAPathPrefix(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	fake := &fakePando{}
	installPando(t, s, fake)

	searchFor(t, s, "/api/v1/search?q=checkout&project=DEMO&limit=5")
	opts := fake.options()
	if opts.Limit != 10 {
		t.Errorf("limit = %d, want the caller's limit doubled", opts.Limit)
	}
	if opts.PathPrefix != "" {
		t.Errorf("a scoped query sent the path prefix %q", opts.PathPrefix)
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
	fake.hits = []pando.KBHit{{FilePath: ".pmngr/stories/DEMO-US-0002-save-payment-methods.md", Chunk: "saved cards", Score: 0.02}}
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
		{FilePath: ".pmngr/stories/DEMO-US-0002-save-payment-methods.md", Chunk: "saved cards", Score: 0.02},
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

// writeFixtureFile drops a file into the copied fixture tree.
func writeFixtureFile(t *testing.T, at, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(at), err)
	}
	if err := os.WriteFile(at, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", at, err)
	}
}
