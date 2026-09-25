package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The fake seams below stand in for what a native host installs — the trace
// engine, the coverage backend and the impact resolver of internal/trace and
// internal/impact — reduced to fixed answers, so these tests pin the framing
// of the three tools and nothing of the engines behind them.

// stubTracer answers one trace for every ref.
type stubTracer struct{}

func (stubTracer) TraceRequirement(_ context.Context, _ *core.Index, ref core.RequirementRef) (core.TracedRequirement, error) {
	return core.TracedRequirement{
		Ref: ref, Project: "DEMO",
		Code: []core.TraceEdge{
			{Ref: ref, Role: core.TraceRoleCode, Path: "internal/address/trim.go", Symbol: "Trim",
				Sources: []core.TraceSource{core.TraceSourceMarker}, Lines: []int{12}},
			{Ref: ref, Role: core.TraceRoleCode, Path: "internal/address/form.go",
				Sources: []core.TraceSource{core.TraceSourceEntry}},
		},
		Tests: []core.TraceEdge{
			{Ref: ref, Role: core.TraceRoleTest, Path: "internal/address/trim_test.go", Symbol: "TestTrim",
				Sources: []core.TraceSource{core.TraceSourceMarker, core.TraceSourceEntry}, Lines: []int{8}},
		},
		Work: []core.TraceWork{{ID: "DEMO-US-0001", Kind: core.LinkImplements}},
		Broken: []core.TraceBroken{{
			Ref: ref, Field: "trace.code", Entry: "internal/address/gone.go", Code: core.CodeWarnTraceBroken,
			Severity: core.SeverityWarning, Message: "the path does not exist",
		}},
	}, nil
}

func (stubTracer) TraceTouching(context.Context, *core.Index, []core.TraceChange) ([]core.TraceHit, error) {
	return nil, nil
}

// stubEvidence offers a stamp for R1 at stubCommit; R2's linked tests failed
// or have no result, and R3 has no linked test.
type stubEvidence struct{}

const stubCommit = "9c1f0a2e5b7d4c3e8f1a6b2d9e0c7f4a3b5d8e21"

func (stubEvidence) Coverage(_ context.Context, _ *core.Index, reqs []core.RequirementView) ([]core.CoverageRow, error) {
	out := make([]core.CoverageRow, 0, len(reqs))
	for _, r := range reqs {
		row := core.CoverageRow{Ref: r.Ref, Status: core.CoveragePassing, Tests: []core.CoverageTest{
			{Test: "internal/address/trim_test.go#TestTrim", Result: "pass"},
		}}
		switch r.Ref.Number {
		case 2:
			row.Status = core.CoverageFailing
			row.Tests = []core.CoverageTest{
				{Test: "internal/address/postcode_test.go#TestEmpty", Result: "fail"},
				{Test: "internal/address/postcode_test.go#TestBlank", Result: "missing"},
				{Test: "internal/address/postcode_test.go#TestValid", Result: "pass"},
			}
		case 3:
			row.Status, row.Tests = core.CoverageUntested, nil
		}
		out = append(out, row)
	}
	return out, nil
}

func (stubEvidence) StampEvidence(_ context.Context, _ *core.Index, reqs []core.RequirementView) ([]core.StampEvidence, error) {
	out := make([]core.StampEvidence, 0, len(reqs))
	for _, r := range reqs {
		switch r.Ref.Number {
		case 1:
			out = append(out, core.StampEvidence{Ref: r.Ref, Verified: &core.Verification{
				Rev: r.BlockRev, Commit: stubCommit,
				At: core.NewTimestamp(time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)),
			}})
		case 2:
			out = append(out, core.StampEvidence{Ref: r.Ref, Reason: core.CoverageReasonFailed})
		default:
			out = append(out, core.StampEvidence{Ref: r.Ref, Reason: core.CoverageReasonNoTests})
		}
	}
	return out, nil
}

// stubImpact answers a fixed result: tier 1 ran, tiers 2 and 3 had no Pando.
type stubImpact struct{ asked core.ImpactQuery }

func (s *stubImpact) Impact(_ context.Context, _ *core.Index, q core.ImpactQuery) (core.ImpactResult, error) {
	s.asked = q
	return core.ImpactResult{
		Base: q.Base, Head: q.Head, Files: 2, Symbols: 3,
		Tiers: []core.ImpactTier{
			{Tier: 1, Status: core.ImpactTierOK, Hits: 2},
			{Tier: 2, Status: core.ImpactTierUnavailable, Message: "Pando is not configured"},
			{Tier: 3, Status: core.ImpactTierUnavailable, Message: "Pando is not configured"},
		},
		Hits: []core.ImpactHit{
			{Ref: core.RequirementRef{Spec: "DEMO-SP-0001", Number: 1}, Title: "Trim input", Tier: 1,
				Status: core.CoveragePassing, Suspect: true, Reasons: []string{"symbol:internal/address/trim.go#Trim"}},
			{Ref: core.RequirementRef{Spec: "DEMO-SP-0001", Number: 2}, Title: "Reject empty postcode", Tier: 1,
				Status: core.CoverageFailing, Reasons: []string{"file:internal/address/postcode.go"}},
		},
	}, nil
}

// withTraceSeams installs the fake seams on the project repository, the way
// the companion and `gintrack mcp` do at startup.
func withTraceSeams(t *testing.T, h *harness) *stubImpact {
	t.Helper()
	m, ok := h.space.Lookup("demo")
	if !ok {
		t.Fatal("the harness has no demo repository")
	}
	impact := &stubImpact{}
	m.Vault.SetRequirementTracer(stubTracer{})
	m.Vault.SetRequirementCoverage(stubEvidence{})
	m.Vault.SetRequirementImpact(impact)
	return impact
}

var (
	_ vault.RequirementTracer   = stubTracer{}
	_ vault.RequirementCoverage = stubEvidence{}
	_ vault.RequirementImpact   = (*stubImpact)(nil)
)

func TestSpecImpact(t *testing.T) {
	t.Run("json form carries the tiers and the ranked hits", func(t *testing.T) {
		h := newHarness(t, false)
		backend := withTraceSeams(t, h)

		res := rawCall(t, h, "spec_impact", map[string]any{
			"base": "main", "head": "worktree", "project": "DEMO", "tiers": []int{1, 2, 3},
		})
		if res.IsError {
			t.Fatalf("spec_impact failed: %s", textOf(res))
		}
		raw, _ := json.Marshal(res.StructuredContent)
		var got ImpactReport
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if backend.asked.Base != "main" || backend.asked.Head != "" {
			t.Errorf("asked base %q head %q, want main and the working tree", backend.asked.Base, backend.asked.Head)
		}
		if got.Total != 2 || len(got.Hits) != 2 || got.Hits[0].Ref != "DEMO-SP-0001.R2" {
			t.Fatalf("report = %+v, want both hits, the failing one first", got)
		}
		if len(got.Tiers) != 3 || got.Tiers[1].Status != "unavailable" || got.Tiers[0].Status != "ok" {
			t.Errorf("tiers = %+v, want tier 1 ok and tiers 2-3 unavailable", got.Tiers)
		}
		if got.Budget != core.DefaultImpactBudget || got.Tokens == 0 || got.Tokens > got.Budget {
			t.Errorf("budget %d tokens %d", got.Budget, got.Tokens)
		}
		if res.Meta[untrustedMeta] != untrustedValue {
			t.Error("the report is not marked as repository content")
		}
		t.Logf("json report: %d bytes", len(raw))
	})

	t.Run("text form is one line per requirement", func(t *testing.T) {
		h := newHarness(t, false)
		withTraceSeams(t, h)
		got := call[ImpactReport](t, h, "spec_impact", map[string]any{"project": "DEMO", "format": "text", "budget": 300})
		if !strings.HasPrefix(got.Text, "impact HEAD..worktree: 2 files, 3 symbols, 2 hits\n") {
			t.Errorf("text = %q", got.Text)
		}
		if !strings.Contains(got.Text, "2 unavailable (Pando is not configured)") {
			t.Errorf("text does not report the unavailable tiers:\n%s", got.Text)
		}
		if got.Hits != nil || got.Tiers != nil {
			t.Error("the text form also carries the json fields")
		}
		t.Logf("text report: %d tokens", got.Tokens)
	})

	t.Run("a small budget pages with a cursor", func(t *testing.T) {
		h := newHarness(t, false)
		withTraceSeams(t, h)
		first := call[ImpactReport](t, h, "spec_impact", map[string]any{"project": "DEMO", "budget": 1})
		if len(first.Hits) != 1 || first.Truncated != 1 || first.NextCursor == "" {
			t.Fatalf("first page = %+v", first)
		}
		next := call[ImpactReport](t, h, "spec_impact", map[string]any{"project": "DEMO", "budget": 1, "cursor": first.NextCursor})
		if len(next.Hits) != 1 || next.Hits[0].Ref == first.Hits[0].Ref || next.NextCursor != "" {
			t.Errorf("second page = %+v", next)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		h := newHarness(t, false)
		withTraceSeams(t, h)
		for name, tc := range map[string]struct {
			args map[string]any
			code string
		}{
			"unknown format": {map[string]any{"format": "yaml"}, codeInvalidRequest},
			"unknown tier":   {map[string]any{"tiers": []int{4}}, codeInvalidRequest},
			"budget too big": {map[string]any{"budget": core.MaxImpactBudget + 1}, codeInvalidRequest},
			"unknown story":  {map[string]any{"story": "DEMO-US-0999"}, codeNotFound},
		} {
			if got := callFails(t, h, "spec_impact", tc.args); got.Code != tc.code {
				t.Errorf("%s: code %q, want %q (%s)", name, got.Code, tc.code, got.Message)
			}
		}
	})

	t.Run("a session without git history answers unavailable with a fallback", func(t *testing.T) {
		h := newHarness(t, false)
		got := callFails(t, h, "spec_impact", map[string]any{"project": "DEMO"})
		if got.Code != codeUnavailable {
			t.Fatalf("code = %q, want %q", got.Code, codeUnavailable)
		}
		if !strings.Contains(got.Retry, "trace_requirement") {
			t.Errorf("retry = %q, want the fallback named", got.Retry)
		}
	})
}

func TestTraceRequirement(t *testing.T) {
	t.Run("every edge carries its origin", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		withTraceSeams(t, h)

		res := rawCall(t, h, "trace_requirement", map[string]any{"ref": spec + ".R1"})
		if res.IsError {
			t.Fatalf("trace_requirement failed: %s", textOf(res))
		}
		raw, _ := json.Marshal(res.StructuredContent)
		var got RequirementTrace
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Ref != spec+".R1" || len(got.Code) != 2 || len(got.Tests) != 1 {
			t.Fatalf("trace = %+v", got)
		}
		want := map[string]string{
			"internal/address/trim.go#Trim":          "marker",
			"internal/address/form.go":               "trace",
			"internal/address/trim_test.go#TestTrim": "marker,trace",
		}
		for _, loc := range append(append([]TraceLocation{}, got.Code...), got.Tests...) {
			if origin := strings.Join(loc.Origin, ","); origin != want[loc.At] {
				t.Errorf("%s origin = %q, want %q", loc.At, origin, want[loc.At])
			}
		}
		if got.Code[0].Lines[0] != 12 {
			t.Errorf("marker lines = %v", got.Code[0].Lines)
		}
		if len(got.Work) != 1 || got.Work[0].ID != "DEMO-US-0001" || got.Work[0].Kind != "implements" {
			t.Errorf("work = %+v", got.Work)
		}
		if len(got.Broken) != 1 || got.Broken[0].Code != string(core.CodeWarnTraceBroken) {
			t.Errorf("broken = %+v", got.Broken)
		}
		if strings.Count(string(raw), spec+".R1") != 1 {
			t.Errorf("the ref is repeated per edge: %s", raw)
		}
		t.Logf("trace: %d bytes", len(raw))
	})

	t.Run("refusals", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		if got := callFails(t, h, "trace_requirement", map[string]any{"ref": spec + ".R1"}); got.Code != codeUnavailable {
			t.Errorf("without a tracer: code %q, want unavailable", got.Code)
		}
		withTraceSeams(t, h)
		if got := callFails(t, h, "trace_requirement", map[string]any{"ref": spec + ".R9"}); got.Code != codeNotFound {
			t.Errorf("a missing requirement: code %q, want not_found", got.Code)
		}
		if got := callFails(t, h, "trace_requirement", map[string]any{"ref": ""}); got.Code != codeInvalidRequest {
			t.Errorf("no ref: code %q, want invalid_request", got.Code)
		}
	})
}

func TestVerifyRequirement(t *testing.T) {
	read := func(t *testing.T, h *harness, ref string) Requirement {
		t.Helper()
		return *call[ItemResult](t, h, "get_item", map[string]any{"id": ref, "fields": []string{"blockRev", "verified"}}).Requirement
	}

	t.Run("stamps when every linked test passed", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		withTraceSeams(t, h)
		before := read(t, h, spec+".R1")

		res := rawCall(t, h, "verify_requirement", map[string]any{"ref": spec + ".R1", "rev": before.Rev})
		if res.IsError {
			t.Fatalf("verify_requirement failed: %s", textOf(res))
		}
		raw, _ := json.Marshal(res.StructuredContent)
		var got VerifyResult
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		t.Logf("verify: %d bytes", len(raw))
		if got.Verified.Rev != before.BlockRev || got.Verified.Commit != stubCommit || got.Verified.By != "test-agent" {
			t.Errorf("verified = %+v, want the block rev, the commit and the agent", got.Verified)
		}
		after := read(t, h, spec+".R1")
		if got.Rev == before.Rev || got.Rev != after.Rev {
			t.Errorf("rev = %q, want the new requirement rev %q", got.Rev, after.Rev)
		}
		if after.Verified == nil || after.Verified.Commit != stubCommit {
			t.Errorf("the stamp is not on disk: %+v", after.Verified)
		}
		if len(got.Changed) != 1 || len(h.writes) == 0 || h.writes[len(h.writes)-1].Tool != "verify_requirement" {
			t.Errorf("changed %v, writes %+v", got.Changed, h.writes)
		}

		again := call[VerifyResult](t, h, "verify_requirement", map[string]any{"ref": spec + ".R1", "rev": got.Rev})
		if !again.Unchanged || again.Rev != got.Rev || len(again.Changed) != 0 {
			t.Errorf("second verify = %+v, want unchanged and no write", again)
		}
	})

	t.Run("refuses with the failing and missing tests", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		withTraceSeams(t, h)
		before := read(t, h, spec+".R2")

		got := callFails(t, h, "verify_requirement", map[string]any{"ref": spec + ".R2", "rev": before.Rev})
		if got.Code != codeNotVerified || got.Reason != core.CoverageReasonFailed {
			t.Fatalf("error = %+v, want not_verified: failed", got)
		}
		tests := map[string]string{}
		for _, tc := range got.Tests {
			tests[tc.Test] = tc.Result
		}
		if len(tests) != 2 || tests["internal/address/postcode_test.go#TestEmpty"] != "fail" ||
			tests["internal/address/postcode_test.go#TestBlank"] != "missing" {
			t.Errorf("tests = %+v, want the failing and the missing one only", got.Tests)
		}
		if after := read(t, h, spec+".R2"); after.Rev != before.Rev || after.Verified != nil {
			t.Error("a refused verification wrote the spec")
		}

		r3 := read(t, h, spec+".R3")
		none := callFails(t, h, "verify_requirement", map[string]any{"ref": spec + ".R3", "rev": r3.Rev})
		if none.Code != codeNotVerified || none.Reason != core.CoverageReasonNoTests || !strings.Contains(none.Message, "Verifies:") {
			t.Errorf("no linked test: %+v", none)
		}
	})

	t.Run("a stale rev is stale_revision", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		withTraceSeams(t, h)
		before := read(t, h, spec+".R1")
		moved := call[RequirementWriteResult](t, h, "update_requirement", map[string]any{
			"ref": spec + ".R1", "rev": before.Rev, "status": "todo",
		})

		got := callFails(t, h, "verify_requirement", map[string]any{"ref": spec + ".R1", "rev": before.Rev})
		if got.Code != core.StaleRevisionCode || got.CurrentRev != moved.Requirement.Rev || got.Retry == "" {
			t.Fatalf("error = %+v, want stale_revision with the current rev", got)
		}
		if len(got.Conflicts) != 1 || got.Conflicts[0].Field != "verified" {
			t.Errorf("conflicts = %+v, want verified", got.Conflicts)
		}
		// Retrying once with currentRev succeeds.
		call[VerifyResult](t, h, "verify_requirement", map[string]any{"ref": spec + ".R1", "rev": got.CurrentRev})
	})

	t.Run("argument refusals", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		withTraceSeams(t, h)
		for name, tc := range map[string]struct {
			args map[string]any
			code string
		}{
			"no rev":   {map[string]any{"ref": spec + ".R1", "rev": ""}, codePreconditionRequired},
			"wildcard": {map[string]any{"ref": spec + ".R1", "rev": "*"}, codeInvalidRequest},
			"no ref":   {map[string]any{"ref": " ", "rev": "sha256:1"}, codeInvalidRequest},
			"missing":  {map[string]any{"ref": spec + ".R9", "rev": "sha256:1"}, codeNotFound},
		} {
			if got := callFails(t, h, "verify_requirement", tc.args); got.Code != tc.code {
				t.Errorf("%s: code %q, want %q (%s)", name, got.Code, tc.code, got.Message)
			}
		}
	})

	t.Run("without a coverage host it is unavailable", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		r1 := read(t, h, spec+".R1")
		if got := callFails(t, h, "verify_requirement", map[string]any{"ref": spec + ".R1", "rev": r1.Rev}); got.Code != codeUnavailable {
			t.Errorf("code = %q, want unavailable", got.Code)
		}
	})
}
