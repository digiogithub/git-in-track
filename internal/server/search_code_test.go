package server

import (
	"path/filepath"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/pando"
)

// TestCodeHitsAreLabelledWithTheirIndex covers the fourth acceptance criterion:
// a code hit reaches the search labeled with the index it came from, so it is
// never read as a backlog item (GIT-US-0098).
func TestCodeHitsAreLabelledWithTheirIndex(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	writeFixtureFile(t, filepath.Join(root, "internal", "exporter.go"),
		"package internal\n\nfunc Export() {}\n")
	project := pando.SanitizeProjectID(root)
	installPando(t, s, &fakePando{
		hits: []pando.KBHit{
			{FilePath: ".pmngr/stories/DEMO-US-0002-save-payment-methods.md", Chunk: "saved cards", Score: 0.03},
		},
		codeHits: map[string][]pando.CodeHit{
			project: {
				{Name: "Export", SymbolType: "function", FilePath: "internal/exporter.go",
					Signature: "func Export()", Score: 1, Rank: 1},
				// A symbol whose file is gone: dropped, never a dangling row.
				{Name: "Retired", FilePath: "internal/retired.go", Score: 0.5, Rank: 2},
			},
		},
	})

	got := searchFor(t, s, "/api/v1/search?q=exporter&limit=20")
	var code, kb int
	for _, hit := range got.Hits {
		switch hit.Index {
		case core.SearchIndexCode:
			code++
			if hit.Path != "internal/exporter.go" || hit.Kind != "file" || hit.Title != "Export" {
				t.Errorf("the code hit does not carry its symbol and path: %+v", hit)
			}
			if hit.Source != core.SearchSourcePando {
				t.Errorf("source = %q, want the semantic backend", hit.Source)
			}
		case core.SearchIndexKB:
			kb++
		}
	}
	if code != 1 {
		t.Errorf("code hits = %d, want the one whose file is still there: %+v", code, got.Hits)
	}
	if kb != 1 {
		t.Errorf("knowledge-base hits = %d, want the item: %+v", kb, got.Hits)
	}
	if got.Dropped != 1 {
		t.Errorf("dropped = %d, want the vanished symbol counted", got.Dropped)
	}
}

// TestSemanticMergeKeepsTheBetterScoreForADocsFile is the sixth acceptance
// criterion: the same `docs/` file comes back from both indexes with different
// scores, and the answer holds one row carrying the better one.
//
// The two scales are not comparable raw — a knowledge-base fusion score sits
// around 0.016 while a code score is already normalized — so "better" means
// better relative to the best hit of its own leg, which is what the merge
// compares.
func TestSemanticMergeKeepsTheBetterScoreForADocsFile(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	project := pando.SanitizeProjectID(root)
	installPando(t, s, &fakePando{
		hits: []pando.KBHit{
			// The best knowledge-base hit is another document, so the page is
			// a quarter as good as its own leg's best.
			{FilePath: ".pmngr/stories/DEMO-US-0002-save-payment-methods.md", Chunk: "saved cards", Score: 0.04},
			// Reported relative to Pando's KBPath, which is `docs/`.
			{FilePath: "architecture/overview.md", Chunk: "the checkout flow", Score: 0.01},
		},
		codeHits: map[string][]pando.CodeHit{
			// Reported relative to the repository root, and the best hit of
			// its own leg.
			project: {{
				Name: "The checkout flow", SymbolType: "namespace",
				FilePath: "docs/architecture/overview.md", Snippet: "## The checkout flow", Score: 1,
			}},
		},
	})

	// A query the substring index cannot match, so the merge under test is the
	// semantic one: a document the exact half already found is suppressed
	// earlier, by mergeSemantic.
	got := searchFor(t, s, "/api/v1/search?q=quokka&limit=20")
	var rows []int
	for i, hit := range got.Hits {
		if hit.Path == "docs/architecture/overview.md" && hit.Source == core.SearchSourcePando {
			rows = append(rows, i)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("the overlapping document appears %d times, want once: %+v", len(rows), got.Hits)
	}
	hit := got.Hits[rows[0]]
	if hit.Index != core.SearchIndexCode || hit.Score != 1 {
		t.Errorf("the merged row = %+v, want the better-scoring code side", hit)
	}
	// The merge is keyed on the resolved path, not the reported one: the
	// knowledge-base side reported `architecture/overview.md`.
	if hit.Kind != "page" || hit.Title == "" {
		t.Errorf("the merged row lost the page it resolves to: %+v", hit)
	}
}

// TestSemanticMergeSurvivesAHitWithoutFrontMatter pins the other half of the
// merge: a code hit carries a symbol name and a path and no front matter at
// all, so whichever side wins the score, the row a user sees carries the id,
// the title and the project the knowledge-base side knew.
func TestSemanticMergeSurvivesAHitWithoutFrontMatter(t *testing.T) {
	t.Parallel()

	kb := scoredHit{
		repo: "demo",
		hit: core.SearchHit{
			Kind: "page", Path: "docs/architecture/overview.md", Title: "Overview",
			Project: "DEMO", Snippet: "the checkout flow", Score: 0.01,
			Source: core.SearchSourcePando, Index: core.SearchIndexKB,
		},
		rel: 0.25,
	}
	code := scoredHit{
		repo: "demo",
		hit: core.SearchHit{
			Kind: "file", Path: "docs/architecture/overview.md", Title: "",
			Snippet: "## The checkout flow", Score: 1,
			Source: core.SearchSourcePando, Index: core.SearchIndexCode,
		},
		rel: 1,
	}

	for _, order := range [][]scoredHit{{kb, code}, {code, kb}} {
		merge := newSemanticMerge(2)
		for _, h := range order {
			merge.add(h)
		}
		out := merge.hits(0)
		if len(out) != 1 {
			t.Fatalf("merged %d rows, want one: %+v", len(out), out)
		}
		got := out[0]
		if got.Score != 1 || got.Index != core.SearchIndexCode {
			t.Errorf("the merge kept %+v, want the better-scoring code side", got)
		}
		if got.Title != "Overview" || got.Project != "DEMO" || got.Kind != "page" {
			t.Errorf("the merged row lost what the front matter knew: %+v", got)
		}
	}
}

// TestSemanticMergeKeepsTwoRepositoriesApart pins the other half of the merge
// key: two mounted clones can hold the same relative path and they are two
// documents, not one.
func TestSemanticMergeKeepsTwoRepositoriesApart(t *testing.T) {
	t.Parallel()

	merge := newSemanticMerge(2)
	for _, repo := range []string{"one", "two"} {
		merge.add(scoredHit{
			repo: repo,
			hit:  core.SearchHit{Kind: "page", Path: "docs/index.md", Title: repo, Score: 1},
			rel:  1,
		})
	}
	if out := merge.hits(0); len(out) != 2 {
		t.Fatalf("merged %d rows, want one per repository: %+v", len(out), out)
	}
}

// TestCodeSearchAsksForDocumentation pins the one option the code leg must
// always send: Pando excludes Markdown from a code search by default, and
// `docs/*.md` is exactly what a question about this repository is answered
// from.
func TestCodeSearchAsksForDocumentation(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	fake := &fakePando{codeHits: map[string][]pando.CodeHit{}}
	installPando(t, s, fake)

	searchFor(t, s, "/api/v1/search?q=checkout&limit=10")

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !fake.lastCode.IncludeDocs {
		t.Error("the code leg did not ask for documentation files")
	}
	if want := pando.SanitizeProjectID(root); len(fake.codeSearched) == 0 || fake.codeSearched[0] != want {
		t.Errorf("the code leg searched %v, want the registered project %q", fake.codeSearched, want)
	}
}
