package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The spec-driven loop of an agent that has changed code (ADR-037, docs/03
// sections 21.6, 21.7 and 21.11, GIT-US-0124): spec_impact asks which
// requirements the diff affects, trace_requirement shows the trace of one of
// them, and verify_requirement stamps the ones the ingested test results prove.
// All three are shims over vault methods whose backends a native host installs
// — the companion and `gintrack mcp` alike — and that a session without them
// answers `unavailable`, never an empty result.

// -------------------------------------------------------------- wire shape --

// ImpactTier is how one tier of an impact query was answered.
type ImpactTier struct {
	Tier      int    `json:"tier" jsonschema:"1 direct trace, 2 transitive callers (Pando), 3 semantic candidates (Pando)"`
	Status    string `json:"status" jsonschema:"ok, unavailable, error or skipped"`
	Hits      int    `json:"hits"`
	Truncated bool   `json:"truncated,omitempty" jsonschema:"The tier was cut at a bound and may reach more"`
	Message   string `json:"message,omitempty" jsonschema:"Why the tier is unavailable or failed"`
}

// ImpactHit is one requirement a diff affects.
type ImpactHit struct {
	Ref       string   `json:"ref"`
	Title     string   `json:"title"`
	Tier      int      `json:"tier"`
	Candidate bool     `json:"candidate,omitempty" jsonschema:"A tier-3 semantic neighbor, not a trace"`
	Score     float64  `json:"score,omitempty"`
	Status    string   `json:"status,omitempty" jsonschema:"Coverage state: untested, passing, failing or suspect"`
	Suspect   bool     `json:"suspect,omitempty"`
	Reasons   []string `json:"reasons" jsonschema:"Short codes <kind>:<detail>"`
	Pending   []string `json:"pending,omitempty" jsonschema:"Open stories and tasks whose Spec Delta modifies the requirement"`
}

// ImpactReport is the compact, token-budgeted impact report: the bytes
// `gintrack spec impact` and the HTTP API return for the same query.
type ImpactReport struct {
	Base       string       `json:"base"`
	Head       string       `json:"head,omitempty" jsonschema:"Empty means the working tree"`
	Files      int          `json:"files"`
	Symbols    int          `json:"symbols"`
	Tiers      []ImpactTier `json:"tiers,omitempty" jsonschema:"Per-tier status (json form); the text form carries it on its second line"`
	Hits       []ImpactHit  `json:"hits,omitempty" jsonschema:"Ranked hits of this page (json form)"`
	Text       string       `json:"text,omitempty" jsonschema:"One line per requirement (text form)"`
	Total      int          `json:"total"`
	Offset     int          `json:"offset,omitempty"`
	Truncated  int          `json:"truncated,omitempty" jsonschema:"Hits after this page"`
	NextCursor string       `json:"nextCursor,omitempty" jsonschema:"Pass back as cursor with the query unchanged"`
	Budget     int          `json:"budget"`
	Tokens     int          `json:"tokens" jsonschema:"Estimated tokens of this page"`
}

// TraceLocation is one place of code or tests traced to a requirement.
type TraceLocation struct {
	At string `json:"at" jsonschema:"Repository-relative path[#symbol]"`
	// Origin is where the edge was read: an in-code marker, a trace: entry of
	// the spec, or both — they are unioned, neither overrides the other.
	Origin []string `json:"origin" jsonschema:"marker (an Implements:/Verifies: comment in the code), trace (a trace: entry of the spec), or both"`
	Lines  []int    `json:"lines,omitempty" jsonschema:"1-based lines of the markers behind the edge"`
}

// TraceWork is a story or task linked to a requirement.
type TraceWork struct {
	ID        string `json:"id"`
	Kind      string `json:"kind" jsonschema:"implements or modifies, as written on the work item"`
	WholeSpec bool   `json:"wholeSpec,omitempty" jsonschema:"The item links the whole spec, not this requirement"`
}

// TraceBroken is a trace: entry that no longer resolves.
type TraceBroken struct {
	Field string `json:"field" jsonschema:"trace.code or trace.tests"`
	Entry string `json:"entry"`
	Code  string `json:"code"`
}

// RequirementTrace is the whole trace of one requirement.
type RequirementTrace struct {
	Ref     string          `json:"ref"`
	Project string          `json:"project,omitempty"`
	Code    []TraceLocation `json:"code,omitempty" jsonschema:"Code that realizes the requirement"`
	Tests   []TraceLocation `json:"tests,omitempty" jsonschema:"Tests that verify it"`
	Work    []TraceWork     `json:"work,omitempty" jsonschema:"Stories and tasks that implement or modify it"`
	Broken  []TraceBroken   `json:"broken,omitempty" jsonschema:"trace: entries whose path or symbol no longer exists"`
}

// VerifyResult is the answer of a verification stamp that was written, or
// that was already there.
type VerifyResult struct {
	Ref string `json:"ref"`
	// Rev is the requirement rev after the stamp: the token the next write
	// quotes.
	Rev      string       `json:"rev"`
	Verified Verification `json:"verified"`
	// Unchanged reports that the stamp already recorded this evidence, so
	// nothing was written.
	Unchanged bool     `json:"unchanged,omitempty" jsonschema:"The stamp already recorded this evidence; nothing was written"`
	Changed   []string `json:"changed,omitempty" jsonschema:"Vault-relative paths written by this call"`
}

// VerifyTest is one linked test that kept a requirement from being stamped.
type VerifyTest struct {
	Test   string `json:"test"`
	Result string `json:"result" jsonschema:"fail, skip, or missing when no ingested result matched it"`
}

// ------------------------------------------------------------------ input ---

// SpecImpactInput is the diff to resolve, and the page of its report.
type SpecImpactInput struct {
	Base    string `json:"base,omitempty" jsonschema:"Revision the diff starts from: a branch, a SHA or HEAD; default HEAD"`
	Head    string `json:"head,omitempty" jsonschema:"Revision the diff ends at; empty or \"worktree\" means the working tree"`
	Story   string `json:"story,omitempty" jsonschema:"Story or task the diff is for: its Spec Delta and implements/modifies links are direct hits"`
	Title   string `json:"title,omitempty" jsonschema:"Extra text for the semantic tier"`
	Project string `json:"project,omitempty" jsonschema:"Project key whose repository holds the diff; needed when the workspace holds more than one and no story is named"`
	Tiers   []int  `json:"tiers,omitempty" jsonschema:"Tiers to run, from 1, 2 and 3; default all"`
	Budget  int    `json:"budget,omitempty" jsonschema:"Token budget of the page, 1 to 20000; default 1500"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"nextCursor from the previous page, with the query unchanged"`
	Format  string `json:"format,omitempty" jsonschema:"json (default: tiers and ranked hits) or text (one line per requirement, cheaper)"`
}

// TraceRequirementInput names one requirement.
type TraceRequirementInput struct {
	Ref string `json:"ref" jsonschema:"Requirement ref, for example ACME-SP-0003.R2"`
}

// VerifyRequirementInput names one requirement and the rev it was read at.
type VerifyRequirementInput struct {
	Ref string `json:"ref" jsonschema:"Requirement ref, for example ACME-SP-0003.R2"`
	Rev string `json:"rev" jsonschema:"Required. The requirement rev (rev, not blockRev) of the read the verification is based on; \"*\" is refused"`
}

// ---------------------------------------------------------------- registry --

// registerTraceTools declares the spec-driven half of the surface.
func registerTraceTools(s *Server) {
	register(s, toolDef{
		Name:  "spec_impact",
		Title: "Requirements a diff affects",
		Description: "Report the requirements a diff affects, ranked failing, then suspect, then by tier, " +
			"and cut at a token budget (default 1500): tier 1 is the direct trace (markers, trace: entries, " +
			"the story's Spec Delta and links), tier 2 the transitive callers and tier 3 semantic " +
			"candidates, both read from Pando. base defaults to HEAD; an empty head, or \"worktree\", is the " +
			"working tree. A tier that cannot run is reported unavailable in tiers (json) or on the tiers " +
			"line (text) while the other tiers still answer, so without Pando you get the tier-1 hits. A " +
			"session that cannot read git history answers unavailable. Walk nextCursor with the query " +
			"unchanged; format text is cheaper than json.",
		Untrusted: true,
	}, specImpact)

	register(s, toolDef{
		Name:  "trace_requirement",
		Title: "Trace one requirement",
		Description: "Return the trace of one requirement: the code that realizes it and the tests that " +
			"verify it as path#symbol, each with its origin — marker (an Implements:/Verifies: comment in " +
			"the code) or trace (a trace: entry of the spec), or both — and the marker lines; the stories " +
			"and tasks that implement or modify it; and trace: entries that no longer resolve. A session " +
			"without the marker scanner answers unavailable.",
		Untrusted: true,
	}, traceRequirement)

	register(s, toolDef{
		Name:  "verify_requirement",
		Title: "Stamp a verified requirement",
		Description: "Write the verified stamp of one requirement from the latest ingested test results " +
			"(gintrack spec ingest): only when every linked test passed at one commit on the text the " +
			"requirement holds now. Otherwise it writes nothing and refuses with not_verified, the reason " +
			"and the failing or missing tests. It runs no tests. rev is required and is the requirement rev " +
			"(rev, never blockRev) the read returned; a rev that is no longer current is refused with " +
			"stale_revision carrying currentRev and conflicts, like update_requirement.",
		Write:     true,
		Untrusted: true,
	}, verifyRequirement)
}

// ---------------------------------------------------------------- handlers --

// specImpact answers the impact report of a diff.
func specImpact(ctx context.Context, s *Server, in SpecImpactInput) (ImpactReport, error) {
	head := strings.TrimSpace(in.Head)
	if strings.EqualFold(head, "worktree") {
		head = ""
	}
	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format != "" && format != string(core.ImpactReportJSON) && format != string(core.ImpactReportText) {
		return ImpactReport{}, invalidField("format", "unknown report format "+in.Format, []string{"json", "text"})
	}
	params := map[string]any{
		"base":    strings.TrimSpace(in.Base),
		"head":    head,
		"story":   strings.TrimSpace(in.Story),
		"title":   in.Title,
		"tiers":   in.Tiers,
		"budget":  in.Budget,
		"cursor":  in.Cursor,
		"format":  format,
		"project": in.Project,
		// The story routes the call to the repository that holds it.
		"id": strings.TrimSpace(in.Story),
	}
	got, err := dispatch[struct {
		Report ImpactReport `json:"report"`
	}](ctx, s, "impact.report", params)
	if err != nil {
		return ImpactReport{}, withFallback(err,
			"read one requirement's trace with trace_requirement, or run `gintrack spec impact` in a git checkout")
	}
	return got.Report, nil
}

// traceRequirement answers the trace of one requirement, compacted: one
// location string per edge and its origin, no repeated ref.
func traceRequirement(ctx context.Context, s *Server, in TraceRequirementInput) (RequirementTrace, error) {
	ref := strings.TrimSpace(in.Ref)
	if ref == "" {
		return RequirementTrace{}, invalidField("ref", "trace_requirement needs a requirement ref", "ACME-SP-0003.R2")
	}
	got, err := dispatch[struct {
		Trace core.TracedRequirement `json:"trace"`
	}](ctx, s, "trace.requirement", map[string]any{"ref": ref})
	if err != nil {
		return RequirementTrace{}, err
	}
	return traceOf(got.Trace), nil
}

// traceOf projects the core trace onto the wire shape.
func traceOf(tr core.TracedRequirement) RequirementTrace {
	out := RequirementTrace{Ref: tr.Ref.String(), Project: string(tr.Project)}
	out.Code = locationsOf(tr.Code)
	out.Tests = locationsOf(tr.Tests)
	for _, w := range tr.Work {
		out.Work = append(out.Work, TraceWork{ID: string(w.ID), Kind: string(w.Kind), WholeSpec: w.WholeSpec})
	}
	for _, b := range tr.Broken {
		out.Broken = append(out.Broken, TraceBroken{Field: b.Field, Entry: b.Entry, Code: string(b.Code)})
	}
	return out
}

func locationsOf(edges []core.TraceEdge) []TraceLocation {
	if len(edges) == 0 {
		return nil
	}
	out := make([]TraceLocation, 0, len(edges))
	for _, e := range edges {
		loc := TraceLocation{At: e.TraceRef(), Origin: make([]string, 0, len(e.Sources)), Lines: e.Lines}
		for _, src := range e.Sources {
			loc.Origin = append(loc.Origin, string(src))
		}
		out = append(out, loc)
	}
	return out
}

// verifyRequirement stamps one requirement under the requirement rev the
// agent read, or refuses with the tests that keep it from being verified.
func verifyRequirement(ctx context.Context, s *Server, in VerifyRequirementInput) (VerifyResult, error) {
	ref := strings.TrimSpace(in.Ref)
	if ref == "" {
		return VerifyResult{}, invalidField("ref", "verify_requirement needs a requirement ref", "ACME-SP-0003.R2")
	}
	rev, err := requiredRev("rev", in.Rev)
	if err != nil {
		return VerifyResult{}, err
	}
	if rev == "" {
		// requiredRev spells the wildcard as "". A stamp records the text that
		// was verified, so it is never written blind.
		return VerifyResult{}, invalidField("rev",
			"verify_requirement stamps the text you read, so it needs that requirement rev, never \"*\"",
			"the rev a get_item on the ref returned")
	}
	result, err := s.dispatchRaw(ctx, "requirement.stamp", map[string]any{
		"refs": []string{ref},
		"rev":  rev,
		"by":   s.agent,
		// The ref routes the call to the repository that holds the spec.
		"ref": ref,
	})
	if err != nil {
		return VerifyResult{}, err
	}
	report, err := decodeResult[struct {
		Stamped []struct {
			Ref      string            `json:"ref"`
			Verified core.Verification `json:"verified"`
		} `json:"stamped"`
		Unstamped []struct {
			Ref    string `json:"ref"`
			Reason string `json:"reason"`
		} `json:"unstamped"`
		Writes writeSet `json:"writes"`
	}](result)
	if err != nil {
		return VerifyResult{}, err
	}
	if len(report.Stamped) == 1 {
		st := report.Stamped[0]
		spec, _, _ := strings.Cut(st.Ref, ".")
		s.announce(ctx, WriteEvent{
			Tool: "verify_requirement", Method: "requirement.stamp", ItemID: spec, Op: "updated", Result: result,
		})
		newRev, _ := s.requirementRev(ctx, st.Ref)
		return VerifyResult{
			Ref: st.Ref, Rev: newRev, Verified: verificationOf(&st.Verified), Changed: report.Writes.paths(),
		}, nil
	}
	reason := ""
	if len(report.Unstamped) > 0 {
		reason = report.Unstamped[0].Reason
	}
	if reason == unstampedUnchanged {
		cur, err := dispatch[struct {
			Requirement core.RequirementView `json:"requirement"`
		}](ctx, s, "requirement.get", map[string]any{"ref": ref})
		if err != nil {
			return VerifyResult{}, err
		}
		return VerifyResult{
			Ref: cur.Requirement.Ref.String(), Rev: string(cur.Requirement.Rev),
			Verified: verificationOf(cur.Requirement.Verified), Unchanged: true,
		}, nil
	}
	return VerifyResult{}, s.notVerified(ctx, ref, reason)
}

// unstampedUnchanged is the reason the vault gives when the stamp already
// records the evidence: nothing to write, and nothing to refuse.
const unstampedUnchanged = "unchanged"

// verificationOf projects a stamp onto the wire shape.
func verificationOf(v *core.Verification) Verification {
	if v == nil {
		return Verification{}
	}
	return Verification{Rev: string(v.Rev), Commit: v.Commit, At: v.At.String(), By: v.By}
}

// notVerified builds the refusal of a stamp the evidence does not allow,
// listing the linked tests that did not pass so the agent knows what to fix.
func (s *Server) notVerified(ctx context.Context, ref, reason string) error {
	out := &toolError{
		Code:    codeNotVerified,
		Message: fmt.Sprintf("%s was not stamped: %s", ref, unstampedMessage(reason)),
		Field:   "ref",
		Reason:  reason,
		Retry:   "Fix or run the tests listed, ingest their results with `gintrack spec ingest`, then call verify_requirement again.",
	}
	rows, err := dispatch[struct {
		Coverage []core.CoverageRow `json:"coverage"`
	}](ctx, s, "coverage.list", map[string]any{"refs": []string{ref}, "ref": ref})
	if err == nil && len(rows.Coverage) == 1 {
		for _, t := range rows.Coverage[0].Tests {
			if t.Result != "pass" {
				out.Tests = append(out.Tests, VerifyTest{Test: t.Test, Result: t.Result})
			}
		}
	}
	return out
}

// unstampedMessage explains a vault reason code in one clause.
func unstampedMessage(reason string) string {
	switch reason {
	case core.CoverageReasonFailed:
		return "a linked test failed in the latest ingested results"
	case core.CoverageReasonPartial:
		return "some linked tests have no ingested result"
	case core.CoverageReasonNoResults:
		return "no linked test has an ingested result"
	case core.CoverageReasonNoTests:
		return "no test is linked to it (add a Verifies: marker or a trace.tests entry)"
	case core.CoverageReasonText:
		return "the tests ran against another version of its text"
	case core.StampReasonMixedCommits:
		return "its linked tests passed at different commits"
	case core.StampReasonNoCommit:
		return "the passing results carry no full commit id"
	case "stamp-newer":
		return "its stamp records a later run"
	case "":
		return "the evidence does not allow a stamp"
	default:
		return "the evidence does not allow a stamp (" + reason + ")"
	}
}

// withFallback adds the capability to use instead to an `unavailable` refusal.
func withFallback(err error, fallback string) error {
	var te *toolError
	if errors.As(err, &te) && te.Code == codeUnavailable && te.Retry == "" {
		te.Retry = "Instead, " + fallback + "."
	}
	return err
}
