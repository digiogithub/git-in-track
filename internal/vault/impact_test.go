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
