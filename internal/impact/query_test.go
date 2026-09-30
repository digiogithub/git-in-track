package impact

import (
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// TestSemanticQueryText pins the tier-3 query of GIT-US-0165: the story's
// title and Spec Delta, the caller's title, and the changed declarations in
// words — a doc comment's first sentence, else the split name — with test
// declarations only when nothing else changed, capped in bytes.
func TestSemanticQueryText(t *testing.T) {
	t.Parallel()
	documented := strings.Replace(fxAlloc, "// Implements: ACME-SP-0001.R1\n",
		"// Implements: ACME-SP-0001.R1\n// NextID returns one more than the highest id. It never reuses one.\n", 1)
	tests := []struct {
		name  string
		files map[string]string // working-tree edits on top of the fixture's
		story core.ItemID
		title string
		want  string
	}{
		{
			name:  "story title and spec delta, undocumented symbol split into words",
			story: "ACME-US-0001",
			want: "Reserve the numbers a delta names\n" +
				"Reserve numbers: The allocator SHALL reserve every number a delta names, applied or not.\n" +
				"next id",
		},
		{
			name:  "a documented declaration contributes its first sentence",
			files: map[string]string{"src/alloc.go": strings.Replace(documented, "max + 1", "max + 2", 1)},
			title: "tighten allocation",
			want:  "tighten allocation\nNextID returns one more than the highest id.",
		},
		{
			name: "a changed test is left out next to changed code",
			files: map[string]string{
				"src/alloc_test.go": strings.Replace(fxTests, "func TestFormat(t *testing.T) {}", "func TestFormat(t *testing.T) { t.Log() }", 1),
			},
			want: "next id",
		},
		{
			name: "only tests changed: their names without the Test prefix",
			files: map[string]string{
				"src/alloc.go":      fxAlloc,
				"src/alloc_test.go": strings.Replace(fxTests, "func TestFormat(t *testing.T) {}", "func TestFormat(t *testing.T) { t.Log() }", 1),
			},
			want: "format",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			for p, text := range tc.files {
				f.write(p, text)
			}
			sem := &fakeSemantic{}
			res := f.impact(f.resolver(nil, sem), core.ImpactQuery{
				Base: f.base, Story: tc.story, Title: tc.title, Tiers: []int{core.ImpactTierSemantic},
			})
			if res.Tiers[2].Status != core.ImpactTierOK {
				t.Fatalf("tier 3 = %+v", res.Tiers[2])
			}
			if sem.query.Kind != core.SearchKindRequirement || sem.query.Q != tc.want {
				t.Errorf("query = %q (kind %q)\nwant %q", sem.query.Q, sem.query.Kind, tc.want)
			}
		})
	}

	t.Run("the query is capped at a word boundary", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		sem := &fakeSemantic{}
		long := strings.Repeat("allocation ", 300)
		f.impact(f.resolver(nil, sem), core.ImpactQuery{Base: f.base, Title: long, Tiers: []int{core.ImpactTierSemantic}})
		q := sem.query.Q
		if len(q) > maxQueryBytes || len(q) < maxQueryBytes-len("allocation ") {
			t.Errorf("query is %d bytes, want at most %d and close to it", len(q), maxQueryBytes)
		}
		if !strings.HasSuffix(q, "allocation") {
			t.Errorf("query ends mid-word: %q", q[len(q)-20:])
		}
	})
}

func TestSplitIdentifier(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"NextID":           "next id",
		"nextNumber":       "next number",
		"maxPageSize":      "max page size",
		"HTTPServer":       "http server",
		"parse_spec_delta": "parse spec delta",
		"TestNextID":       "test next id",
		"v2Store":          "v2 store",
		"":                 "",
	} {
		if got := splitIdentifier(in); got != want {
			t.Errorf("splitIdentifier(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCutCandidates pins the tier-3 cut (GIT-US-0180, docs/03 R-IMP-4): a
// candidate needs both legs of Pando's fusion (a score above 0.0164), stays
// within semanticGap of the best one, and at most limit are kept.
func TestCutCandidates(t *testing.T) {
	t.Parallel()
	rank := func(scores ...float64) []semanticHit {
		out := make([]semanticHit, len(scores))
		for i, s := range scores {
			out[i] = semanticHit{ref: core.RequirementRef{Spec: "ACME-SP-0001", Number: i + 1}, score: s}
		}
		return out
	}
	tests := []struct {
		name   string
		ranked []semanticHit
		limit  int
		want   int
	}{
		{"no candidates", nil, 0, 0},
		{"a flat single-leg ranking gives none", rank(0.016, 0.016, 0.015, 0.015, 0.014, 0.014, 0.014, 0.014), 0, 0},
		{"one leg at its best is still below the floor", rank(0.0164), 0, 0},
		{"a clear leader is kept alone", rank(0.033, 0.016, 0.015), 0, 1},
		{"a leader and a close second", rank(0.033, 0.030, 0.016), 0, 2},
		{"a second under the gap is dropped", rank(0.033, 0.0255, 0.025), 0, 1},
		{"the cap is the default limit", rank(0.033, 0.033, 0.032, 0.031, 0.031, 0.030, 0.030), 0, defaultSemanticLimit},
		{"an explicit limit caps too", rank(0.033, 0.033, 0.032), 2, 2},
		{"a limit does not lower the floor", rank(0.033, 0.016), 5, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cutCandidates(tc.ranked, tc.limit)
			if len(got) != tc.want {
				t.Fatalf("kept %d candidates (%+v), want %d", len(got), got, tc.want)
			}
			for i, h := range got {
				if h.ref != tc.ranked[i].ref {
					t.Errorf("candidate %d = %s, want the ranking's %s", i, h.ref, tc.ranked[i].ref)
				}
			}
		})
	}
}

func TestNameQueries(t *testing.T) {
	t.Parallel()
	sym := func(path, symbol string) changedSymbol { return changedSymbol{path: path, symbol: symbol} }
	tests := []struct {
		name    string
		symbols []changedSymbol
		want    []string
	}{
		{"names in words", []changedSymbol{sym("a.go", "nextNumber"), sym("a.go", "Allocator.ReserveRange")}, []string{"next number", "reserve range"}},
		{"a single word is dropped", []changedSymbol{sym("a.go", "Item"), sym("a.go", "nextNumber")}, []string{"next number"}},
		{"a duplicate is asked once", []changedSymbol{sym("a.go", "nextNumber"), sym("b.go", "nextNumber")}, []string{"next number"}},
		{"tests only when nothing else changed", []changedSymbol{sym("a_test.go", "TestNextNumber"), sym("a.go", "reserveRange")}, []string{"reserve range"}},
		{"only tests changed", []changedSymbol{sym("a_test.go", "TestNextNumber")}, []string{"next number"}},
		{"nothing changed", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := nameQueries(tc.symbols)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("nameQueries = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("at most maxNameQueries", func(t *testing.T) {
		t.Parallel()
		var syms []changedSymbol
		for _, n := range []string{"aaAa", "bbBb", "ccCc", "ddDd", "eeEe", "ffFf", "ggGg", "hhHh"} {
			syms = append(syms, sym("a.go", n))
		}
		if got := nameQueries(syms); len(got) != maxNameQueries {
			t.Errorf("got %d queries, want %d", len(got), maxNameQueries)
		}
	})
}
