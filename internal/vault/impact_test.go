package vault

import (
	"context"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// stubImpact echoes the query it was handed.
type stubImpact struct {
	got core.ImpactQuery
}

func (s *stubImpact) Impact(_ context.Context, ix *core.Index, q core.ImpactQuery) (core.ImpactResult, error) {
	if ix == nil {
		panic("no index")
	}
	s.got = q
	return core.ImpactResult{Base: q.Base, Head: q.Head}, nil
}

func TestImpactQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		backend  bool
		params   map[string]any
		wantCode string // empty: success
		wantBase string
	}{
		{"unavailable without a host", false, map[string]any{"base": "main"}, "unavailable", ""},
		{"base defaults to HEAD", true, map[string]any{}, "", "HEAD"},
		{"explicit range", true, map[string]any{"base": " main ", "head": "HEAD", "tiers": []int{1, 2}, "depth": 3, "limit": 10}, "", "main"},
		{"tier out of range", true, map[string]any{"tiers": []int{0}}, "invalid_request", ""},
		{"negative limit", true, map[string]any{"limit": -1}, "invalid_request", ""},
		{"limit too high", true, map[string]any{"limit": 51}, "invalid_request", ""},
		{"unknown story", true, map[string]any{"story": "DEMO-US-0999"}, "not_found", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := specVault(t)
			stub := &stubImpact{}
			if tc.backend {
				v.SetRequirementImpact(stub)
			}
			if v.ImpactAvailable() != tc.backend {
				t.Fatalf("ImpactAvailable() = %v", v.ImpactAvailable())
			}
			env := rawCall(t, v, "impact.query", tc.params)
			if tc.wantCode != "" {
				if env.OK || env.Error.Code != tc.wantCode {
					t.Fatalf("impact.query = %+v, want %s", env, tc.wantCode)
				}
				return
			}
			if !env.OK {
				t.Fatalf("impact.query failed: %s %s", env.Error.Code, env.Error.Message)
			}
			got := decode[struct {
				Impact core.ImpactResult `json:"impact"`
			}](t, env.Result)
			if got.Impact.Base != tc.wantBase || stub.got.Base != tc.wantBase || got.Impact.Hits == nil {
				t.Errorf("impact = %+v (asked %+v), want base %s and an empty hit list", got.Impact, stub.got, tc.wantBase)
			}
		})
	}
}

// hitsImpact answers every query with n hits of spec DEMO-SP-0001.
type hitsImpact struct{ n int }

func (s hitsImpact) Impact(_ context.Context, _ *core.Index, q core.ImpactQuery) (core.ImpactResult, error) {
	res := core.ImpactResult{Base: q.Base, Tiers: []core.ImpactTier{{Tier: 1, Status: core.ImpactTierOK, Hits: s.n}}}
	for i := 1; i <= s.n; i++ {
		res.Hits = append(res.Hits, core.ImpactHit{
			Ref: core.RequirementRef{Spec: "DEMO-SP-0001", Number: i}, Title: "Requirement", Tier: 1,
			Status: core.CoverageUntested, Reasons: []string{"symbol:src/a.go#F"},
		})
	}
	return res, nil
}

func TestImpactReport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		backend  bool
		params   map[string]any
		wantCode string
		wantHits int
		wantText bool
	}{
		{"unavailable without a host", false, map[string]any{}, "unavailable", 0, false},
		{"default budget fits", true, map[string]any{}, "", 30, false},
		{"text form", true, map[string]any{"format": "text"}, "", 0, true},
		{"small budget truncates", true, map[string]any{"budget": 200}, "", -1, false},
		{"negative budget", true, map[string]any{"budget": -1}, "invalid_request", 0, false},
		{"budget too high", true, map[string]any{"budget": core.MaxImpactBudget + 1}, "invalid_request", 0, false},
		{"unknown format", true, map[string]any{"format": "yaml"}, "invalid_request", 0, false},
		{"foreign cursor", true, map[string]any{"cursor": "eyJvIjoxLCJmIjoiMDAwMDAwMDAifQ"}, "invalid_request", 0, false},
		{"query validated", true, map[string]any{"tiers": []int{4}}, "invalid_request", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := specVault(t)
			if tc.backend {
				v.SetRequirementImpact(hitsImpact{n: 30})
			}
			env := rawCall(t, v, "impact.report", tc.params)
			if tc.wantCode != "" {
				if env.OK || env.Error.Code != tc.wantCode {
					t.Fatalf("impact.report = %+v, want %s", env, tc.wantCode)
				}
				return
			}
			if !env.OK {
				t.Fatalf("impact.report failed: %s %s", env.Error.Code, env.Error.Message)
			}
			got := decode[struct {
				Report core.ImpactReport `json:"report"`
			}](t, env.Result).Report
			if got.Total != 30 || got.Tokens > got.Budget {
				t.Errorf("report = %+v", got)
			}
			if tc.wantText != (got.Text != "") {
				t.Errorf("text = %q, want text form %v", got.Text, tc.wantText)
			}
			if tc.wantHits >= 0 && !tc.wantText && len(got.Hits) != tc.wantHits {
				t.Errorf("hits = %d, want %d", len(got.Hits), tc.wantHits)
			}
			if tc.wantHits < 0 {
				if got.Truncated == 0 || got.NextCursor == "" || len(got.Hits)+got.Truncated != 30 {
					t.Fatalf("small budget: %d hits, truncated %d, cursor %q", len(got.Hits), got.Truncated, got.NextCursor)
				}
				next := rawCall(t, v, "impact.report", map[string]any{"budget": 200, "cursor": got.NextCursor})
				if !next.OK {
					t.Fatalf("resuming failed: %+v", next.Error)
				}
				page := decode[struct {
					Report core.ImpactReport `json:"report"`
				}](t, next.Result).Report
				if page.Offset != len(got.Hits) || page.Hits[0].Ref.Number != len(got.Hits)+1 {
					t.Errorf("resumed at %d (%s), want %d", page.Offset, page.Hits[0].Ref, len(got.Hits))
				}
			}
		})
	}
}
