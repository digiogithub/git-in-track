package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// contextOf builds a context of n requirements, each with two scenarios of
// two steps, and one related page.
func contextOf(n int) SpecContext {
	c := SpecContext{Item: "ACME-US-0001", Title: "Checkout", Coverage: SpecCoverageOK,
		Pages: []SpecContextPage{{Path: "docs/checkout.md", Title: "Checkout"}}}
	for i := 1; i <= n; i++ {
		c.Requirements = append(c.Requirements, SpecContextRequirement{
			Ref: fmt.Sprintf("ACME-SP-0001.R%d", i), Title: fmt.Sprintf("Requirement %d", i),
			Via: []string{SpecViaImplements}, Statement: "The system SHALL do thing number " + fmt.Sprint(i) + ".",
			Scenarios: []SpecContextScenario{
				{Name: "Happy path", Steps: []string{"WHEN the input is valid", "THEN it is accepted"}},
				{Name: "Sad path", Steps: []string{"WHEN the input is empty", "THEN it is refused"}},
			},
			Status: CoveragePassing,
		})
	}
	return c
}

func TestRenderSpecContext(t *testing.T) {
	c := contextOf(12)
	full, err := RenderSpecContext(c, ImpactReportOptions{Budget: MaxImpactBudget})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		budget     int
		format     ImpactReportFormat
		wantDetail SpecContextDetail
		wantAll    bool
	}{
		{"everything fits with steps", MaxImpactBudget, ImpactReportJSON, SpecDetailSteps, true},
		{"one token short drops the steps first", full.Tokens - 1, ImpactReportJSON, SpecDetailNames, true},
		{"a tight budget cuts the list", 300, ImpactReportJSON, SpecDetailNames, false},
		{"text form cuts too", 250, ImpactReportText, SpecDetailNames, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen []string
			cursor := ""
			for guard := 0; guard < 20; guard++ {
				r, err := RenderSpecContext(c, ImpactReportOptions{Budget: tc.budget, Cursor: cursor, Format: tc.format})
				if err != nil {
					t.Fatal(err)
				}
				if guard == 0 && r.Detail != tc.wantDetail {
					t.Errorf("detail = %q, want %q", r.Detail, tc.wantDetail)
				}
				n := len(r.Requirements)
				if tc.format == ImpactReportText {
					n = strings.Count(r.Text, "\nACME-SP-0001.R")
				}
				// A page of one requirement may exceed a very small budget:
				// a walk always advances.
				if tokens := EstimateTokens(mustMarshal(r)); tokens > r.Tokens || (r.Tokens > tc.budget && n > 1) {
					t.Errorf("page of %d costs %d, reports %d, budget %d", n, tokens, r.Tokens, tc.budget)
				}
				if guard == 0 && tc.wantAll != (r.NextCursor == "") {
					t.Errorf("first page has %d of %d requirements, cursor %q", n, r.Total, r.NextCursor)
				}
				if guard > 0 && (r.Pages != nil || strings.Contains(r.Text, "kb: ")) {
					t.Error("a later page repeats the related pages")
				}
				for i := 0; i < n; i++ {
					seen = append(seen, fmt.Sprint(r.Offset+i))
				}
				if r.NextCursor == "" {
					break
				}
				cursor = r.NextCursor
			}
			if len(seen) != len(c.Requirements) {
				t.Errorf("the walk saw %d of %d requirements", len(seen), len(c.Requirements))
			}
		})
	}

	t.Run("a cursor of another context is refused", func(t *testing.T) {
		r, err := RenderSpecContext(c, ImpactReportOptions{Budget: 200})
		if err != nil || r.NextCursor == "" {
			t.Fatalf("first page: %v, %+v", err, r)
		}
		_, err = RenderSpecContext(contextOf(13), ImpactReportOptions{Budget: 200, Cursor: r.NextCursor})
		if !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("err = %v, want ErrInvalidCursor", err)
		}
	})
}

func TestSpecContextApplyCoverage(t *testing.T) {
	c := SpecContext{Requirements: []SpecContextRequirement{{Ref: "ACME-SP-0001.R2"}, {Ref: "ACME-SP-0001"}}}
	if refs := c.Refs(); len(refs) != 1 || refs[0].Number != 2 {
		t.Fatalf("refs = %v, want the numbered one alone", refs)
	}
	c.ApplyCoverage([]CoverageRow{{Ref: RequirementRef{Spec: "ACME-SP-0001", Number: 2}, Status: CoverageFailing,
		Reasons: []string{CoverageReasonFailed}}})
	if c.Coverage != SpecCoverageOK || c.Requirements[0].Status != CoverageFailing || c.Requirements[1].Status != "" {
		t.Errorf("context = %+v", c)
	}
}
