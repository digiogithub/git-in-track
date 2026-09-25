package vault

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// stubCoverage answers every requirement passing and offers a stamp for R1
// only; R2's evidence failed.
type stubCoverage struct {
	commit string
	rev    core.Rev // when set, the tested rev instead of the current one
}

func (s *stubCoverage) Coverage(_ context.Context, _ *core.Index, reqs []core.RequirementView) ([]core.CoverageRow, error) {
	out := make([]core.CoverageRow, 0, len(reqs))
	for _, r := range reqs {
		st := core.CoveragePassing
		if r.Ref.Number == 2 {
			st = core.CoverageFailing
		}
		out = append(out, core.CoverageRow{Ref: r.Ref, Status: st})
	}
	return out, nil
}

func (s *stubCoverage) StampEvidence(_ context.Context, _ *core.Index, reqs []core.RequirementView) ([]core.StampEvidence, error) {
	out := make([]core.StampEvidence, 0, len(reqs))
	for _, r := range reqs {
		if r.Ref.Number != 1 {
			out = append(out, core.StampEvidence{Ref: r.Ref, Reason: core.CoverageReasonFailed})
			continue
		}
		rev := r.BlockRev
		if s.rev != "" {
			rev = s.rev
		}
		out = append(out, core.StampEvidence{Ref: r.Ref, Verified: &core.Verification{
			Rev: rev, Commit: s.commit, At: core.NewTimestamp(time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)),
		}})
	}
	return out, nil
}

func TestCoverageMethods(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider bool
		method   string
		params   map[string]any
		wantCode string // empty: success
	}{
		{"list unavailable without a host", false, "coverage.list", map[string]any{}, "unavailable"},
		{"stamp unavailable without a host", false, "requirement.stamp", map[string]any{"spec": "DEMO-SP-0001", "by": "ana"}, "unavailable"},
		{"unknown status", true, "coverage.list", map[string]any{"status": "tested"}, "invalid_request"},
		{"unknown requirement", true, "coverage.list", map[string]any{"refs": []string{"DEMO-SP-0001.R9"}}, "not_found"},
		{"stamp without by", true, "requirement.stamp", map[string]any{"spec": "DEMO-SP-0001"}, "invalid_request"},
		{"stamp without refs", true, "requirement.stamp", map[string]any{"by": "ana"}, "invalid_request"},
		{"list", true, "coverage.list", map[string]any{"spec": "DEMO-SP-0001", "status": []string{"failing"}}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := specVault(t)
			if tc.provider {
				v.SetRequirementCoverage(&stubCoverage{commit: stubCommit})
			}
			if v.CoverageAvailable() != tc.provider {
				t.Fatalf("CoverageAvailable() = %v", v.CoverageAvailable())
			}
			env := rawCall(t, v, tc.method, tc.params)
			if tc.wantCode != "" {
				if env.OK || env.Error.Code != tc.wantCode {
					t.Fatalf("%s = %+v, want %s", tc.method, env, tc.wantCode)
				}
				return
			}
			if !env.OK {
				t.Fatalf("%s failed: %s %s", tc.method, env.Error.Code, env.Error.Message)
			}
			got := decode[struct {
				Coverage []core.CoverageRow `json:"coverage"`
				Total    int                `json:"total"`
			}](t, env.Result)
			if got.Total != 1 || got.Coverage[0].Ref.Number != 2 || got.Coverage[0].Status != core.CoverageFailing {
				t.Errorf("coverage = %+v, want R2 failing only", got)
			}
		})
	}
}

const stubCommit = "9c1f0a2e5b7d4c3e8f1a6b2d9e0c7f4a3b5d8e21"

type stampWire struct {
	Stamped []struct {
		Ref      string            `json:"ref"`
		Verified core.Verification `json:"verified"`
	} `json:"stamped"`
	Unstamped []UnstampedRequirement `json:"unstamped"`
	Writes    WriteSet               `json:"writes"`
}

func TestRequirementStamp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		stub       stubCoverage
		wantR1     string // reason R1 is unstamped; empty: stamped
		wantWrites int
	}{
		{"stamps what passed", stubCoverage{commit: stubCommit}, "", 1},
		{"another text was tested", stubCoverage{commit: stubCommit, rev: "sha256:0000000000000000"}, "text", 0},
		{"no commit", stubCoverage{}, "no-commit", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := specVault(t)
			v.SetRequirementCoverage(&tc.stub)
			before := getRequirement(t, v, "DEMO-SP-0001.R2")
			got := decode[stampWire](t, call(t, v, "requirement.stamp", map[string]any{"spec": "DEMO-SP-0001", "by": "ana"}))
			if len(got.Writes.Written) != tc.wantWrites {
				t.Errorf("writes = %d, want %d", len(got.Writes.Written), tc.wantWrites)
			}
			reasons := map[string]string{}
			for _, u := range got.Unstamped {
				reasons[u.Ref.String()] = u.Reason
			}
			if reasons["DEMO-SP-0001.R2"] != "failed" {
				t.Errorf("R2 = %q, want unstamped: failed", reasons["DEMO-SP-0001.R2"])
			}
			if reasons["DEMO-SP-0001.R1"] != tc.wantR1 {
				t.Fatalf("R1 unstamped reason = %q, want %q (%+v)", reasons["DEMO-SP-0001.R1"], tc.wantR1, got)
			}
			if after := getRequirement(t, v, "DEMO-SP-0001.R2"); after.Requirement.Rev != before.Requirement.Rev {
				t.Error("an unstamped requirement was written")
			}
			if tc.wantR1 != "" {
				return
			}
			r1 := getRequirement(t, v, "DEMO-SP-0001.R1")
			text := got.Writes.Written[0].Text
			if !strings.Contains(text, "verified: {rev: \""+r1.Requirement.BlockRev+"\", commit: "+stubCommit+", at: 2026-09-02T08:00:00Z, by: ana}") {
				t.Errorf("spec after stamping:\n%s", text)
			}
			if strings.Contains(text, "suspect") || strings.Contains(text, "coverage") {
				t.Error("the stamp wrote a computed state")
			}
			again := decode[stampWire](t, call(t, v, "requirement.stamp", map[string]any{"refs": "DEMO-SP-0001.R1", "by": "ana"}))
			if len(again.Unstamped) != 1 || again.Unstamped[0].Reason != "unchanged" || len(again.Writes.Written) != 0 {
				t.Errorf("restamp = %+v, want unchanged and no write", again)
			}
		})
	}
}

// TestStampVerifiedWithoutHost is the done-transition path of GIT-US-0110 in
// a browser-only session: nothing is stamped and nothing fails.
func TestStampVerifiedWithoutHost(t *testing.T) {
	t.Parallel()
	v := specVault(t)
	ref := core.RequirementRef{Spec: "DEMO-SP-0001", Number: 1}
	got, err := v.stampVerified(context.Background(), []core.RequirementRef{ref}, "ana")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Stamped) != 0 || len(got.Unstamped) != 1 || got.Unstamped[0].Reason != "unavailable" {
		t.Errorf("stampVerified = %+v", got)
	}
}

// TestRequirementStampUnderRev covers the rev-pinned stamp the MCP
// verify_requirement sends (GIT-US-0124): exactly one ref, never "*", and a
// rev that moved is stale_revision with the verified field in conflict
// rather than a quiet "stale" entry.
func TestRequirementStampUnderRev(t *testing.T) {
	t.Parallel()
	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		v := specVault(t)
		v.SetRequirementCoverage(&stubCoverage{commit: stubCommit})
		for name, params := range map[string]map[string]any{
			"two refs":  {"refs": []string{"DEMO-SP-0001.R1", "DEMO-SP-0001.R2"}, "rev": "sha256:1", "by": "ana"},
			"a spec":    {"spec": "DEMO-SP-0001", "rev": "sha256:1", "by": "ana"},
			"wildcard":  {"refs": []string{"DEMO-SP-0001.R1"}, "rev": "*", "by": "ana"},
			"no refs":   {"rev": "sha256:1", "by": "ana"},
			"no author": {"refs": []string{"DEMO-SP-0001.R1"}, "rev": "sha256:1"},
		} {
			if env := rawRequirementCall(t, v, "requirement.stamp", params); env.OK || env.Error.Code != "invalid_request" {
				t.Errorf("%s: %+v, want invalid_request", name, env)
			}
		}
	})
	t.Run("a current rev stamps", func(t *testing.T) {
		t.Parallel()
		v := specVault(t)
		v.SetRequirementCoverage(&stubCoverage{commit: stubCommit})
		r1 := getRequirement(t, v, "DEMO-SP-0001.R1")
		got := decode[stampWire](t, call(t, v, "requirement.stamp", map[string]any{
			"refs": []string{"DEMO-SP-0001.R1"}, "rev": r1.Requirement.Rev, "by": "ana",
		}))
		if len(got.Stamped) != 1 || len(got.Writes.Written) != 1 {
			t.Fatalf("stamp = %+v, want R1 stamped", got)
		}
	})
	t.Run("a moved rev is stale_revision", func(t *testing.T) {
		t.Parallel()
		v := specVault(t)
		v.SetRequirementCoverage(&stubCoverage{commit: stubCommit})
		before := getRequirement(t, v, "DEMO-SP-0001.R1")
		moved := decode[requirementResultWire](t, call(t, v, "requirement.update", map[string]any{
			"ref": "DEMO-SP-0001.R1", "rev": before.Requirement.Rev, "patch": map[string]any{"title": "Renamed"},
		}))
		env := rawRequirementCall(t, v, "requirement.stamp", map[string]any{
			"refs": []string{"DEMO-SP-0001.R1"}, "rev": before.Requirement.Rev, "by": "ana",
		})
		if env.OK || env.Error.Code != core.StaleRevisionCode {
			t.Fatalf("stamp under a moved rev = %+v, want stale_revision", env)
		}
		if env.Error.CurrentRev != moved.Requirement.Rev {
			t.Errorf("currentRev = %q, want %q", env.Error.CurrentRev, moved.Requirement.Rev)
		}
		if len(env.Error.Conflicts) != 1 || env.Error.Conflicts[0].Field != "verified" {
			t.Errorf("conflicts = %+v, want the verified field", env.Error.Conflicts)
		}
		if after := getRequirement(t, v, "DEMO-SP-0001.R1"); after.Requirement.Rev != moved.Requirement.Rev {
			t.Error("a stale stamp was written")
		}
	})
}
