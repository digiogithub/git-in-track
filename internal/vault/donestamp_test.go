package vault

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// fakeCoverage is a coverage host whose stamp evidence is fixed per ref.
type fakeCoverage struct {
	commit string
	offer  map[string]bool // refs whose evidence allows a stamp
	err    error
}

func (f fakeCoverage) Coverage(context.Context, *core.Index, []core.RequirementView) ([]core.CoverageRow, error) {
	return nil, errors.New("not used")
}

func (f fakeCoverage) StampEvidence(_ context.Context, _ *core.Index, reqs []core.RequirementView) ([]core.StampEvidence, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]core.StampEvidence, 0, len(reqs))
	for _, v := range reqs {
		se := core.StampEvidence{Ref: v.Ref, Reason: core.CoverageReasonNoResults}
		if f.offer[v.Ref.String()] {
			se = core.StampEvidence{Ref: v.Ref, Verified: &core.Verification{
				Rev: v.BlockRev, Commit: f.commit, At: core.NewTimestamp(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)),
			}}
		}
		out = append(out, se)
	}
	return out, nil
}

// doneStampResult is the stamp part of a done transition's specDelta.
type doneStampResult struct {
	Writes    WriteSet `json:"writes"`
	SpecDelta *struct {
		Stamped   []StampedRequirement   `json:"stamped"`
		Unstamped []UnstampedRequirement `json:"unstamped"`
	} `json:"specDelta"`
}

func (r doneStampResult) reasons() map[string]string {
	out := map[string]string{}
	if r.SpecDelta != nil {
		for _, u := range r.SpecDelta.Unstamped {
			out[u.Ref.String()] = u.Reason
		}
	}
	return out
}

// implementsStory creates a story in review implementing refs, with no delta.
func implementsStory(t *testing.T, v *Vault, refs ...string) string {
	t.Helper()
	var links []map[string]string
	for _, ref := range refs {
		links = append(links, map[string]string{"kind": "implements", "target": ref})
	}
	raw := call(t, v, "item.create", map[string]any{
		"project": "DEMO", "type": "story", "title": "Implement", "status": "in_review", "links": links,
	})
	return decode[struct {
		Item deltaItem `json:"item"`
	}](t, raw).Item.ID
}

func moveDone(t *testing.T, v *Vault, id string) doneStampResult {
	t.Helper()
	env := moveItem(t, v, id, "done")
	if !env.OK {
		t.Fatalf("move to done: %s: %s", env.Error.Code, env.Error.Message)
	}
	return decode[doneStampResult](t, env.Result)
}

// TestDoneStamp covers the vault's half of the done-transition stamp
// (R-REQ-11a (a), GIT-US-0141): the move is never refused for want of
// evidence, and a stamp the evidence allows is written in the same write.
func TestDoneStamp(t *testing.T) {
	t.Run("no coverage host: unstamped, unavailable", func(t *testing.T) {
		v := specVault(t)
		got := moveDone(t, v, implementsStory(t, v, "DEMO-SP-0001.R1", "DEMO-SP-0009.R1", "OTHER/OTHER-SP-0001.R1"))
		want := map[string]string{
			"DEMO-SP-0001.R1": stampReasonUnavailable, "DEMO-SP-0009.R1": stampReasonUnavailable,
			"OTHER-SP-0001.R1": stampReasonUnavailable,
		}
		if r := got.reasons(); len(r) != len(want) || r["DEMO-SP-0001.R1"] != want["DEMO-SP-0001.R1"] {
			t.Errorf("unstamped = %v, want %v", r, want)
		}
	})

	t.Run("a missing requirement or another project's is never refused", func(t *testing.T) {
		v := specVault(t)
		v.SetRequirementCoverage(fakeCoverage{commit: strings.Repeat("b", 40)})
		got := moveDone(t, v, implementsStory(t, v, "DEMO-SP-0001.R1", "DEMO-SP-0001.R40", "OTHER/OTHER-SP-0001.R1"))
		r := got.reasons()
		if r["DEMO-SP-0001.R1"] != core.CoverageReasonNoResults || r["DEMO-SP-0001.R40"] != stampReasonMissing ||
			r["OTHER-SP-0001.R1"] != stampReasonOtherProject {
			t.Errorf("unstamped = %v", r)
		}
	})

	t.Run("unreadable evidence: unstamped, unavailable", func(t *testing.T) {
		v := specVault(t)
		v.SetRequirementCoverage(fakeCoverage{err: errors.New("disk on fire")})
		got := moveDone(t, v, implementsStory(t, v, "DEMO-SP-0001.R1"))
		if r := got.reasons(); r["DEMO-SP-0001.R1"] != stampReasonUnavailable {
			t.Errorf("unstamped = %v", r)
		}
	})

	t.Run("a stamp the evidence allows is written with the move", func(t *testing.T) {
		v := specVault(t)
		commit := strings.Repeat("c", 40)
		v.SetRequirementCoverage(fakeCoverage{commit: commit, offer: map[string]bool{"DEMO-SP-0001.R1": true}})
		got := moveDone(t, v, implementsStory(t, v, "DEMO-SP-0001.R1", "DEMO-SP-0001.R2"))
		if got.SpecDelta == nil || len(got.SpecDelta.Stamped) != 1 || got.SpecDelta.Stamped[0].Ref.String() != "DEMO-SP-0001.R1" {
			t.Fatalf("specDelta = %+v, want R1 stamped", got.SpecDelta)
		}
		if s := got.SpecDelta.Stamped[0].Verified; s.Commit != commit || s.By == "" {
			t.Errorf("stamp = %+v, want commit %s and a handle", s, commit)
		}
		if r := got.reasons(); r["DEMO-SP-0001.R2"] != core.CoverageReasonNoResults {
			t.Errorf("unstamped = %v", r)
		}
		wroteSpec := false
		for _, w := range got.Writes.Written {
			wroteSpec = wroteSpec || strings.Contains(w.Path, "specs/DEMO-SP-0001")
		}
		if !wroteSpec {
			t.Errorf("writes = %+v, want the spec written with the move", got.Writes)
		}
		r1 := decode[struct {
			Requirement core.RequirementView `json:"requirement"`
		}](t, call(t, v, "requirement.get", map[string]any{"ref": "DEMO-SP-0001.R1"})).Requirement
		if r1.Verified == nil || r1.Verified.Commit != commit || r1.Verified.Rev != r1.BlockRev {
			t.Errorf("R1 = %+v, want verified at %s", r1.Verified, commit)
		}
		if again := moveDone(t, v, implementsStory(t, v, "DEMO-SP-0001.R1")).reasons(); again["DEMO-SP-0001.R1"] != stampReasonUnchanged {
			t.Errorf("second story = %v, want unchanged", again)
		}
	})
}
