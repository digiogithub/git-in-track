package vault

import (
	"context"
	"errors"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

func TestPagesAreClonedNotLeaked(t *testing.T) {
	t.Parallel()

	v := openFixture(t, fixtureRoot)
	pages := v.Pages()
	if len(pages) == 0 {
		t.Fatal("the fixture has no knowledge-base pages")
	}

	// The exporter walks these while a watcher pass can be rewriting the index;
	// a leaked pointer would be a data race, so the copy has to be real.
	first := pages[0]
	title := first.Title
	first.Title = "rewritten by the caller"
	if len(first.Tags) > 0 {
		first.Tags[0] = "rewritten"
	}

	again := v.Pages()
	if again[0].Title != title {
		t.Errorf("the index kept the caller's edit: %q", again[0].Title)
	}
	if again[0] == first {
		t.Error("Pages handed out the index's own pointer")
	}
}

func TestItemAndPageLookups(t *testing.T) {
	t.Parallel()

	v := openFixture(t, fixtureRoot)

	it, ok := v.Item("DEMO-US-0001")
	if !ok {
		t.Fatal("DEMO-US-0001 is not indexed")
	}
	if it.Title == "" || it.Body == "" {
		t.Errorf("the item came back without its content: %+v", it)
	}
	if _, ok := v.Item("DEMO-US-9999"); ok {
		t.Error("an unknown id resolved")
	}

	page, ok := v.Page("docs/architecture/overview.md")
	if !ok {
		t.Fatal("the page is not indexed by its vault-relative path")
	}
	if page.RelPath != "architecture/overview.md" {
		t.Errorf("relPath = %q", page.RelPath)
	}
	if _, ok := v.Page("docs/nope.md"); ok {
		t.Error("an unknown page resolved")
	}
}

func TestPageByProjectPath(t *testing.T) {
	t.Parallel()

	v := openFixture(t, fixtureRoot)

	// This is the reverse of the corpus layout: a hit names the project and the
	// documentation-folder relative path, and the vault finds the live page.
	page, ok := v.PageByProjectPath("DEMO", "architecture/overview.md")
	if !ok {
		t.Fatal("the page did not resolve from its project and relative path")
	}
	if page.Path != "docs/architecture/overview.md" {
		t.Errorf("path = %q, want the vault-relative one", page.Path)
	}
	if _, ok := v.PageByProjectPath("OTHER", "architecture/overview.md"); ok {
		t.Error("a page of another project resolved")
	}
	if _, ok := v.PageByProjectPath("DEMO", ""); ok {
		t.Error("an empty path resolved")
	}
}

// stubSemantic is a semantic backend that answers a fixed list.
type stubSemantic struct {
	hits []core.SearchHit
	err  error
	last SemanticQuery
}

func (s *stubSemantic) SearchSemantic(_ context.Context, q SemanticQuery) ([]core.SearchHit, error) {
	s.last = q
	return s.hits, s.err
}

func TestSemanticSearchNeedsABackend(t *testing.T) {
	t.Parallel()

	w := NewWorkspace()
	if _, err := w.Attach("demo", RoleProject, openFixture(t, fixtureRoot)); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if w.SemanticAvailable() {
		t.Error("a fresh workspace must report no semantic backend")
	}

	_, err := w.SearchSemantic(t.Context(), SemanticQuery{Q: "stale write"})
	var verr *Error
	if !errors.As(err, &verr) || verr.Code != "unavailable" {
		t.Fatalf("err = %v, want an `unavailable` contract error", err)
	}
}

func TestSemanticSearchNamesTheRepository(t *testing.T) {
	t.Parallel()

	w := NewWorkspace()
	if _, err := w.Attach("demo", RoleProject, openFixture(t, fixtureRoot)); err != nil {
		t.Fatalf("attach: %v", err)
	}
	backend := &stubSemantic{hits: []core.SearchHit{
		{Kind: "item", ID: "DEMO-US-0001", Path: "docs/.pmngr/stories/x.md", Title: "Guest checkout",
			Project: "DEMO", Score: 0.02, Snippet: "…", Source: core.SearchSourcePando},
		// A backend that forgot to say where the hit came from still ends up
		// labeled: the contract never answers a hit with no source.
		{Kind: "page", Path: "docs/index.md", Title: "Index", Project: "DEMO", Score: 0.01},
	}}
	w.SetSemanticSearcher(backend)
	if !w.SemanticAvailable() {
		t.Fatal("the backend was not installed")
	}

	hits, err := w.SearchSemantic(t.Context(), SemanticQuery{Q: "stale write", Limit: 5, Project: "DEMO"})
	if err != nil {
		t.Fatalf("SearchSemantic: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %+v", hits)
	}
	for _, hit := range hits {
		if hit.Source != core.SearchSourcePando {
			t.Errorf("hit %s came back as %q", hit.Path, hit.Source)
		}
		if hit.VaultID != "demo" {
			t.Errorf("hit %s does not name its repository", hit.Path)
		}
	}
	if backend.last.Q != "stale write" || backend.last.Limit != 5 || backend.last.Project != "DEMO" {
		t.Errorf("the query reached the backend as %+v", backend.last)
	}

	// The method is on the contract, so every host reaches it the same way.
	result, err := w.Dispatch(t.Context(), "search.semantic", []byte(`{"q":"stale write"}`))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got, ok := result.([]SearchHit); !ok || len(got) != 2 {
		t.Fatalf("search.semantic answered %T %+v", result, result)
	}

	w.SetSemanticSearcher(nil)
	if w.SemanticAvailable() {
		t.Error("the backend was not removed")
	}
}

func TestCoreSearchHitsNameTheirBackend(t *testing.T) {
	t.Parallel()

	v := openFixture(t, fixtureRoot)
	raw := call(t, v, "search", map[string]any{"q": "checkout", "limit": 5})
	hits := decode[[]SearchHit](t, raw)
	if len(hits) == 0 {
		t.Fatal("the fixture search found nothing")
	}
	for _, hit := range hits {
		if hit.Source != core.SearchSourceCore {
			t.Errorf("hit %s came from %q, want core", hit.Path, hit.Source)
		}
	}
}
