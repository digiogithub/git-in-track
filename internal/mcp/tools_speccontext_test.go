package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// contextFixture builds a story with everything spec_context reads: the spec
// of specFixture plus a fourth requirement with scenarios, declared
// implements and modifies links, a Spec Delta with an unnumbered ADDED block
// and a MODIFIED operation, and a wikilink to a knowledge-base page. It
// returns the spec id.
func contextFixture(t *testing.T, h *harness) string {
	t.Helper()
	spec := specFixture(t, h)
	call[RequirementWriteResult](t, h, "create_requirement", map[string]any{
		"spec": spec, "title": "Validate the street",
		"text": "The checkout SHALL refuse a street shorter than three characters.\n\n" +
			"#### Scenario: Short street\n\n- WHEN the street is \"ab\"\n- THEN the form shows an error\n\n" +
			"#### Scenario: Long street\n\n- WHEN the street has 200 characters\n- THEN the form accepts it\n",
	})
	body := "## Description\n\nSee [[architecture/overview]] for the components.\n\n" +
		"## Spec Delta\n\n" +
		"### ADDED " + spec + " — Uppercase the postcode\n\n" +
		"The checkout SHALL store the postcode in upper case.\n\n" +
		"#### Scenario: Lower-case input\n\n- WHEN the postcode is \"sw1a 1aa\"\n- THEN it is stored as \"SW1A 1AA\"\n\n" +
		"### MODIFIED " + spec + ".R3 — Normalize the country code\n\n" +
		"The checkout SHALL store the country as an ISO 3166 alpha-2 code.\n"
	links := []core.Link{
		{Kind: core.LinkBlockedBy, Target: "DEMO-T-0001"},
		{Kind: core.LinkImplements, Target: spec + ".R1"},
		{Kind: core.LinkImplements, Target: spec + ".R4"},
		{Kind: core.LinkModifies, Target: spec + ".R2"},
	}
	params, _ := json.Marshal(map[string]any{
		"id": "DEMO-US-0001", "rev": "", // the vault spells the wildcard as ""
		"patch": map[string]any{"set": map[string]any{"links": links}, "body": body},
	})
	if _, err := h.space.Dispatch(context.Background(), "item.update", params); err != nil {
		t.Fatalf("link the story: %v", err)
	}
	return spec
}

func TestSpecContext(t *testing.T) {
	t.Run("links, the Spec Delta, coverage and pages", func(t *testing.T) {
		h := newHarness(t, true)
		spec := contextFixture(t, h)
		withTraceSeams(t, h)

		res := rawCall(t, h, "spec_context", map[string]any{"story": "DEMO-US-0001"})
		if res.IsError {
			t.Fatalf("spec_context failed: %s", textOf(res))
		}
		raw, _ := json.Marshal(res.StructuredContent)
		var got SpecContext
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		refs := make([]string, 0, len(got.Requirements))
		for _, r := range got.Requirements {
			refs = append(refs, r.Ref+":"+strings.Join(r.Via, ","))
		}
		want := []string{
			spec + ".R1:implements", spec + ".R2:modifies", spec + ".R3:delta-modified",
			spec + ".R4:implements", spec + ":delta-added",
		}
		if strings.Join(refs, " ") != strings.Join(want, " ") {
			t.Fatalf("requirements = %v, want %v", refs, want)
		}
		if got.Coverage != "ok" || got.Requirements[1].Status != "failing" || got.Requirements[0].Status != "passing" {
			t.Errorf("coverage = %q, rows %+v", got.Coverage, got.Requirements[:2])
		}
		if got.Requirements[4].Status != "" {
			t.Errorf("an unnumbered ADDED block has a coverage status: %+v", got.Requirements[4])
		}
		r3 := got.Requirements[2]
		if !r3.Proposed || r3.Title != "Normalize the country code" || !strings.Contains(r3.Statement, "ISO 3166") {
			t.Errorf("MODIFIED R3 = %+v, want the proposal", r3)
		}
		r4 := got.Requirements[3]
		if len(r4.Scenarios) != 2 || r4.Scenarios[0].Name != "Short street" || len(r4.Scenarios[0].Steps) != 2 {
			t.Errorf("R4 scenarios = %+v, want both with their steps", r4.Scenarios)
		}
		if got.Detail != "steps" || got.Total != 5 || got.NextCursor != "" {
			t.Errorf("detail %q total %d cursor %q", got.Detail, got.Total, got.NextCursor)
		}
		if len(got.Pages) != 1 || got.Pages[0].Path != "docs/architecture/overview.md" || got.Pages[0].Title == "" {
			t.Errorf("pages = %+v, want the linked overview", got.Pages)
		}
		if got.Tokens == 0 || got.Tokens > got.Budget || got.Budget != core.DefaultImpactBudget {
			t.Errorf("budget %d tokens %d", got.Budget, got.Tokens)
		}
		if res.Meta[untrustedMeta] != untrustedValue {
			t.Error("the context is not marked as repository content")
		}
		t.Logf("json context: %d bytes, %d tokens", len(raw), got.Tokens)
	})

	t.Run("text form", func(t *testing.T) {
		h := newHarness(t, true)
		spec := contextFixture(t, h)
		withTraceSeams(t, h)
		got := call[SpecContext](t, h, "spec_context", map[string]any{"story": "DEMO-US-0001", "format": "text"})
		if !strings.HasPrefix(got.Text, "context DEMO-US-0001 \"Guest checkout\": 5 requirements, coverage ok\n") {
			t.Errorf("text = %q", got.Text)
		}
		for _, line := range []string{
			spec + ".R2 modifies failing \"Reject empty postcode\"",
			spec + " delta-added - proposed \"Uppercase the postcode\"",
			"  scenario Short street: WHEN the street is \"ab\" / THEN the form shows an error",
			"kb: docs/architecture/overview.md",
		} {
			if !strings.Contains(got.Text, line) {
				t.Errorf("text lacks %q:\n%s", line, got.Text)
			}
		}
		if got.Requirements != nil || got.Pages != nil {
			t.Error("the text form also carries the json fields")
		}
		t.Logf("text context: %d tokens\n%s", got.Tokens, got.Text)
	})

	t.Run("a tight budget drops steps, then pages with a cursor", func(t *testing.T) {
		h := newHarness(t, true)
		contextFixture(t, h)
		withTraceSeams(t, h)

		full := call[SpecContext](t, h, "spec_context", map[string]any{"story": "DEMO-US-0001"})
		names := call[SpecContext](t, h, "spec_context", map[string]any{"story": "DEMO-US-0001", "budget": full.Tokens - 1})
		if names.Detail != "names" {
			t.Fatalf("detail = %q at budget %d, want names", names.Detail, full.Tokens-1)
		}
		for _, r := range names.Requirements {
			for _, sc := range r.Scenarios {
				if len(sc.Steps) > 0 {
					t.Errorf("%s keeps steps at detail names", r.Ref)
				}
			}
		}

		var seen []string
		cursor, pages := "", 0
		for page := 0; page < 10; page++ {
			pages++
			args := map[string]any{"story": "DEMO-US-0001", "budget": 150}
			if cursor != "" {
				args["cursor"] = cursor
			}
			got := call[SpecContext](t, h, "spec_context", args)
			if len(got.Requirements) == 0 {
				t.Fatalf("page %d is empty: %+v", page, got)
			}
			if got.Offset != len(seen) {
				t.Errorf("page %d offset %d, want %d", page, got.Offset, len(seen))
			}
			if page > 0 && got.Pages != nil {
				t.Error("the related pages ride on a later page")
			}
			if len(got.Requirements) > 1 && got.Tokens > got.Budget {
				t.Errorf("page %d costs %d tokens over a %d budget", page, got.Tokens, got.Budget)
			}
			for _, r := range got.Requirements {
				seen = append(seen, r.Ref)
			}
			if got.NextCursor == "" {
				break
			}
			if got.Truncated != got.Total-len(seen) {
				t.Errorf("truncated %d, want %d", got.Truncated, got.Total-len(seen))
			}
			cursor = got.NextCursor
		}
		if len(seen) != 5 || pages < 2 {
			t.Fatalf("walk saw %v in %d pages, want the five requirements over several pages", seen, pages)
		}
		unique := map[string]bool{}
		for _, ref := range seen {
			unique[ref] = true
		}
		if len(unique) != len(seen) {
			t.Errorf("the walk repeats a requirement: %v", seen)
		}
	})

	t.Run("no coverage backend still answers", func(t *testing.T) {
		h := newHarness(t, true)
		contextFixture(t, h)
		got := call[SpecContext](t, h, "spec_context", map[string]any{"story": "DEMO-US-0001", "format": "text"})
		if !strings.Contains(got.Text, "coverage unavailable") || got.Total != 5 {
			t.Errorf("text = %s", got.Text)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		h := newHarness(t, false)
		for name, tc := range map[string]struct {
			args map[string]any
			code string
		}{
			"blank story":    {map[string]any{"story": " "}, codeInvalidRequest},
			"unknown story":  {map[string]any{"story": "DEMO-US-0999"}, codeNotFound},
			"unknown format": {map[string]any{"story": "DEMO-US-0001", "format": "yaml"}, codeInvalidRequest},
			"budget too big": {map[string]any{"story": "DEMO-US-0001", "budget": core.MaxImpactBudget + 1}, codeInvalidRequest},
			"foreign cursor": {map[string]any{"story": "DEMO-US-0001", "cursor": "eyJvIjoxLCJmIjoiMDAwMDAwMDAifQ"}, codeInvalidRequest},
		} {
			if got := callFails(t, h, "spec_context", tc.args); got.Code != tc.code {
				t.Errorf("%s: code %q, want %q (%s)", name, got.Code, tc.code, got.Message)
			}
		}
	})
}

func TestSpecCoverage(t *testing.T) {
	t.Run("one row per requirement, paged and counted", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		withTraceSeams(t, h)

		first := call[CoveragePage](t, h, "spec_coverage", map[string]any{"spec": spec, "limit": 2})
		if first.Total != 3 || len(first.Coverage) != 2 || first.NextCursor == "" {
			t.Fatalf("first page = %+v", first)
		}
		if first.Counts != (CoverageCounts{Passing: 1, Failing: 1, Untested: 1}) {
			t.Errorf("counts = %+v", first.Counts)
		}
		if r := first.Coverage[1]; r.Ref != spec+".R2" || r.Status != "failing" || len(r.Tests) != 3 {
			t.Errorf("R2 row = %+v", r)
		}
		next := call[CoveragePage](t, h, "spec_coverage", map[string]any{"spec": spec, "limit": 2, "cursor": first.NextCursor})
		if len(next.Coverage) != 1 || next.Coverage[0].Ref != spec+".R3" || next.NextCursor != "" {
			t.Errorf("second page = %+v", next)
		}
		raw, _ := json.Marshal(first)
		t.Logf("two rows: %d bytes (~%d tokens)", len(raw), core.EstimateTokens(string(raw)))
	})

	t.Run("status filter and projection", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		withTraceSeams(t, h)
		got := call[CoveragePage](t, h, "spec_coverage", map[string]any{
			"spec": spec, "status": []string{"failing"}, "fields": []string{"reasons"},
		})
		if got.Total != 1 || got.Coverage[0].Ref != spec+".R2" || got.Coverage[0].Tests != nil {
			t.Errorf("page = %+v, want R2 alone, without tests", got)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		withTraceSeams(t, h)
		bad := callFails(t, h, "spec_coverage", map[string]any{"status": []string{"green"}})
		if bad.Code != codeInvalidRequest || bad.Field != "status" {
			t.Errorf("unknown status = %+v", bad)
		}
		first := call[CoveragePage](t, h, "spec_coverage", map[string]any{"spec": spec, "limit": 1})
		moved := callFails(t, h, "spec_coverage", map[string]any{"spec": spec, "status": []string{"passing"}, "cursor": first.NextCursor})
		if moved.Code != codeInvalidCursor {
			t.Errorf("a cursor under another filter = %+v", moved)
		}
	})

	t.Run("no coverage backend answers unavailable with a fallback", func(t *testing.T) {
		h := newHarness(t, false)
		got := callFails(t, h, "spec_coverage", map[string]any{"project": "DEMO"})
		if got.Code != codeUnavailable || !strings.Contains(got.Retry, "list_requirements") {
			t.Errorf("error = %+v", got)
		}
	})
}

func TestSpecContextToolsAreReadOnly(t *testing.T) {
	h := newHarness(t, false)
	listed, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	want := map[string]bool{"spec_context": true, "spec_coverage": true}
	for _, tool := range listed.Tools {
		if !want[tool.Name] {
			continue
		}
		delete(want, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not annotated read-only", tool.Name)
		}
		if !strings.Contains(tool.Description, untrustedNote) {
			t.Errorf("%s does not carry the data boundary", tool.Name)
		}
	}
	for name := range want {
		t.Errorf("%s is not advertised without --allow-write", name)
	}
}
