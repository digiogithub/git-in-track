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
