package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The typical-PR fixture: twelve hits across the three tiers, one failing,
// several suspect, three semantic candidates and one tier-1 hit whose reasons
// were already cut at four. Its report must fit the milestone's budget of
// 1.5k tokens in both forms without truncating.
func loadImpactFixture(t *testing.T, name string) ImpactResult {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "impactreport", name))
	if err != nil {
		t.Fatal(err)
	}
	var res ImpactResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	return res
}

// largeImpactResult is a big diff: n hits spread over the tiers, statuses and
// candidates, in an arbitrary input order.
func largeImpactResult(n int) ImpactResult {
	res := ImpactResult{Base: "main", Head: "HEAD", Files: 140, Symbols: 380, Tiers: []ImpactTier{
		{Tier: 1, Status: ImpactTierOK},
		{Tier: 2, Status: ImpactTierOK, Truncated: true},
		{Tier: 3, Status: ImpactTierUnavailable, Message: "Pando is not configured for this session; set pando.url to enable tiers 2 and 3"},
	}}
	statuses := []CoverageStatus{CoveragePassing, CoverageUntested, "", CoverageFailing, CoverageSuspect, CoveragePassing, CoverageUntested}
	for i := 0; i < n; i++ {
		// Walk the specs in a scrambled order so the input is not pre-sorted.
		k := (i * 37) % n
		h := ImpactHit{
			Ref:     RequirementRef{Spec: ItemID(fmt.Sprintf("ACME-SP-%04d", 1+k%23)), Number: 1 + k/23},
			Title:   fmt.Sprintf("Requirement %d of a large change, with a title long enough to be clipped", k),
			Tier:    1 + k%3,
			Status:  statuses[k%len(statuses)],
			Reasons: []string{fmt.Sprintf("symbol:internal/pkg%d/file%d.go#Func%d", k%9, k%5, k)},
		}
		if h.Tier == ImpactTierSemantic {
			h.Candidate, h.Score, h.Status, h.Reasons = true, float64(900-k)/1000, "", []string{"semantic"}
		}
		if h.Status == CoveragePassing && k%2 == 0 {
			h.Suspect = true
		}
		if h.Status == CoverageSuspect {
			h.Suspect = true
		}
		res.Hits = append(res.Hits, h)
	}
	return res
}

func checkImpactGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "impactreport", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file:\n%s", name, got)
	}
}

func TestEstimateTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"abc", 1},
		{"abcd", 2},
		{strings.Repeat("x", 3000), 1000},
		{"ñ", 1}, // two bytes of UTF-8
	}
	for _, tc := range tests {
		if got := EstimateTokens(tc.in); got != tc.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestRankImpactHits(t *testing.T) {
	t.Parallel()
	ref := func(spec string, n int) RequirementRef { return RequirementRef{Spec: ItemID(spec), Number: n} }
	in := []ImpactHit{
		{Ref: ref("A-SP-0002", 1), Tier: 3, Candidate: true, Score: 0.5},
		{Ref: ref("A-SP-0002", 2), Tier: 3, Candidate: true, Score: 0.9},
		{Ref: ref("A-SP-0001", 10), Tier: 1, Status: CoverageUntested},
		{Ref: ref("A-SP-0001", 9), Tier: 1, Status: CoverageUntested},
		{Ref: ref("A-SP-0003", 1), Tier: 2, Status: CoveragePassing, Suspect: true},
		{Ref: ref("A-SP-0004", 1), Tier: 2, Status: CoverageFailing},
		{Ref: ref("A-SP-0001", 1), Tier: 1, Status: CoverageSuspect},
		{Ref: ref("A-SP-0005", 1), Tier: 2},
		{Ref: ref("A-SP-0006", 1), Tier: 1, Kind: ImpactKindTestOnly, Status: CoveragePassing, Suspect: true},
		{Ref: ref("A-SP-0006", 2), Tier: 1, Kind: ImpactKindTestOnly, Status: CoverageFailing},
		{Ref: ref("A-SP-0006", 3), Tier: 1, Kind: ImpactKindTestOnly},
		{Ref: ref("A-SP-0007", 1), Tier: 2, Kind: ImpactKindBehaviour},
	}
	want := []string{
		"A-SP-0004.R1", // failing
		"A-SP-0006.R2", // failing, test-only: failing still comes first
		"A-SP-0001.R1", // suspect, tier 1
		"A-SP-0003.R1", // suspect, tier 2
		"A-SP-0006.R1", // suspect, test-only after every behaviour suspect
		"A-SP-0001.R9", // tier 1, numeric ref order
		"A-SP-0001.R10",
		"A-SP-0005.R1", // tier 2
		"A-SP-0007.R1", // tier 2, behaviour
		"A-SP-0006.R3", // test-only, after the behaviour hits of any tier
		"A-SP-0002.R2", // candidate, best score
		"A-SP-0002.R1", // candidate
	}
	got := RankImpactHits(in)
	for i, h := range got {
		if h.Ref.String() != want[i] {
			t.Fatalf("rank %d = %s, want %s (got %v)", i, h.Ref, want[i], got)
		}
	}
	if in[0].Ref.String() != "A-SP-0002.R1" {
		t.Error("RankImpactHits modified its input")
	}
}

// TestImpactReportTypicalPR is the milestone's success criterion: the typical
// PR's report stays within 1.5k estimated tokens in both forms, whole.
func TestImpactReportTypicalPR(t *testing.T) {
	t.Parallel()
	res := loadImpactFixture(t, "typical-pr.json")
	for _, format := range []ImpactReportFormat{ImpactReportJSON, ImpactReportText} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			r, err := RenderImpactReport(res, ImpactReportOptions{Format: format})
			if err != nil {
				t.Fatal(err)
			}
			wire := mustMarshal(r)
			if tokens := EstimateTokens(wire); tokens > DefaultImpactBudget || tokens > r.Tokens || r.Tokens > DefaultImpactBudget {
				t.Errorf("typical PR report = %d estimated tokens (reported %d), want ≤ %d", tokens, r.Tokens, DefaultImpactBudget)
			}
			if r.Truncated != 0 || r.NextCursor != "" || r.Total != len(res.Hits) {
				t.Errorf("typical PR report truncated: %+v", r)
			}
			if format == ImpactReportText {
				if strings.Count(r.Text, "\n") != len(res.Hits)+2 {
					t.Errorf("text form has %d lines, want a header, a tier line and one per hit", strings.Count(r.Text, "\n"))
				}
				checkImpactGolden(t, "typical-pr.golden.txt", r.Text)
				return
			}
			indented, err := json.MarshalIndent(r, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			checkImpactGolden(t, "typical-pr.golden.json", string(indented)+"\n")
		})
	}
}

// TestImpactReportWalk cuts a large result at small budgets and walks the
// cursor: every page fits, pages are contiguous, and the walk returns every
// hit exactly once in rank order.
func TestImpactReportWalk(t *testing.T) {
	t.Parallel()
	res := largeImpactResult(150)
	ranked := RankImpactHits(res.Hits)
	tests := []struct {
		format ImpactReportFormat
		budget int
	}{
		{ImpactReportJSON, 0},
		{ImpactReportText, 0},
		{ImpactReportJSON, 400},
		{ImpactReportText, 300},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s/%d", tc.format, tc.budget), func(t *testing.T) {
			t.Parallel()
			budget := tc.budget
			if budget == 0 {
				budget = DefaultImpactBudget
			}
			var walked []string
			cursor := ""
			for pages := 0; ; pages++ {
				if pages > len(ranked) {
					t.Fatal("the walk does not terminate")
				}
				r, err := RenderImpactReport(res, ImpactReportOptions{Budget: tc.budget, Cursor: cursor, Format: tc.format})
				if err != nil {
					t.Fatalf("page %d: %v", pages, err)
				}
				if tokens := EstimateTokens(mustMarshal(r)); tokens > budget || tokens > r.Tokens || r.Tokens > budget {
					t.Errorf("page %d = %d tokens (reported %d), budget %d", pages, tokens, r.Tokens, budget)
				}
				if r.Offset != len(walked) {
					t.Fatalf("page %d starts at %d, want %d", pages, r.Offset, len(walked))
				}
				shown := pageRefs(t, r)
				if len(shown) == 0 {
					t.Fatalf("page %d is empty", pages)
				}
				walked = append(walked, shown...)
				if r.Truncated != len(ranked)-len(walked) {
					t.Fatalf("page %d truncated = %d, want %d", pages, r.Truncated, len(ranked)-len(walked))
				}
				if r.NextCursor == "" {
					break
				}
				cursor = r.NextCursor
			}
			if len(walked) != len(ranked) {
				t.Fatalf("walked %d hits, want %d", len(walked), len(ranked))
			}
			for i, h := range ranked {
				if walked[i] != h.Ref.String() {
					t.Fatalf("hit %d = %s, want %s", i, walked[i], h.Ref)
				}
			}
		})
	}
}

// pageRefs returns the refs a page shows, in either form.
func pageRefs(t *testing.T, r ImpactReport) []string {
	t.Helper()
	if r.Text == "" {
		refs := make([]string, 0, len(r.Hits))
		for _, h := range r.Hits {
			refs = append(refs, h.Ref.String())
		}
		return refs
	}
	var refs []string
	for _, line := range strings.Split(strings.TrimSuffix(r.Text, "\n"), "\n") {
		if strings.HasPrefix(line, "ACME-SP-") {
			refs = append(refs, strings.Fields(line)[0])
		}
	}
	return refs
}

func TestImpactReportLargeFirstPage(t *testing.T) {
	t.Parallel()
	r, err := RenderImpactReport(largeImpactResult(150), ImpactReportOptions{Format: ImpactReportText})
	if err != nil {
		t.Fatal(err)
	}
	if r.Truncated == 0 || r.NextCursor == "" {
		t.Fatalf("large report was not truncated: %+v", r)
	}
	if !strings.Contains(r.Text, "truncated: ") || !strings.Contains(r.Text, "3 unavailable (") {
		t.Errorf("text lacks the truncation or the tier line:\n%s", r.Text)
	}
	checkImpactGolden(t, "large-pr-page1.golden.txt", r.Text)
}

func TestImpactReportCursorErrors(t *testing.T) {
	t.Parallel()
	res := largeImpactResult(60)
	first, err := RenderImpactReport(res, ImpactReportOptions{Budget: 300})
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	changed := res
	changed.Hits = res.Hits[1:]
	tests := []struct {
		name   string
		res    ImpactResult
		cursor string
	}{
		{"not base64", res, "!!!"},
		{"not json", res, "bm90IGpzb24"},
		{"another result", changed, first.NextCursor},
		{"another base", ImpactResult{Base: "other", Hits: res.Hits}, first.NextCursor},
		{"offset past the end", res, encodeImpactCursor(61, impactFingerprint(res, RankImpactHits(res.Hits)))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := RenderImpactReport(tc.res, ImpactReportOptions{Cursor: tc.cursor}); !errors.Is(err, ErrInvalidCursor) {
				t.Errorf("err = %v, want ErrInvalidCursor", err)
			}
		})
	}
	// The cursor binds the result, not the budget or the form.
	if _, err := RenderImpactReport(res, ImpactReportOptions{Cursor: first.NextCursor, Budget: 5000, Format: ImpactReportText}); err != nil {
		t.Errorf("changing budget and form mid-walk: %v", err)
	}
	if _, err := RenderImpactReport(res, ImpactReportOptions{Format: "yaml"}); err == nil {
		t.Error("an unknown format was accepted")
	}
}

func TestImpactReportEdges(t *testing.T) {
	t.Parallel()
	t.Run("no hits", func(t *testing.T) {
		t.Parallel()
		r, err := RenderImpactReport(ImpactResult{Base: "HEAD", Tiers: []ImpactTier{{Tier: 1, Status: ImpactTierOK}}}, ImpactReportOptions{Format: ImpactReportText})
		if err != nil {
			t.Fatal(err)
		}
		want := "impact HEAD..worktree: 0 files, 0 symbols, 0 hits\ntiers: 1 ok 0\n"
		if r.Text != want || r.Truncated != 0 {
			t.Errorf("text = %q, want %q", r.Text, want)
		}
	})
	t.Run("a hit over a tiny budget still advances", func(t *testing.T) {
		t.Parallel()
		r, err := RenderImpactReport(largeImpactResult(5), ImpactReportOptions{Budget: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Hits) != 1 || r.Truncated != 4 {
			t.Errorf("page = %d hits, truncated %d; want 1 and 4", len(r.Hits), r.Truncated)
		}
	})
	t.Run("line format", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			hit  ImpactHit
			want string
		}{
			{ImpactHit{Ref: RequirementRef{Spec: "A-SP-0001", Number: 2}, Title: "Short", Tier: 1, Status: CoveragePassing, Suspect: true, Reasons: []string{"symbol:a.go#F", "file:b.go"}},
				`A-SP-0001.R2 t1 passing suspect "Short" symbol:a.go#F +1`},
			{ImpactHit{Ref: RequirementRef{Spec: "A-SP-0001", Number: 3}, Title: "S", Tier: 1, Status: CoverageSuspect, Suspect: true, Reasons: []string{"a", "b", "c", "d", "+3"}},
				`A-SP-0001.R3 t1 suspect "S" a +6`},
			{ImpactHit{Ref: RequirementRef{Spec: "A-SP-0002", Number: 1}, Title: strings.Repeat("long ", 20), Tier: 3, Candidate: true, Score: 0.812, Reasons: []string{"semantic"}},
				`A-SP-0002.R1 t3~0.812 - "long long long long long long long lo..." semantic`},
		}
		for _, tc := range tests {
			if got := impactLine(tc.hit); got != tc.want {
				t.Errorf("impactLine = %q\nwant         %q", got, tc.want)
			}
		}
	})
}
