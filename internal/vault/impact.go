package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the host seam for the requirement impact query (ADR-037,
// docs/03 section 21.11, GIT-US-0119). Impact needs git history, the marker
// scan and, for its transitive and semantic tiers, Pando — none of which this
// package or internal/core may reach (ADR-003). So a native host installs a
// resolver — the companion's is internal/impact's Resolver, one per
// repository — and a browser-only session installs none: "impact.query" then
// answers `unavailable`.

// RequirementImpact is the backend a host installs with
// [Vault.SetRequirementImpact]. It receives the vault's own index, so the
// requirements, the Spec Deltas and the links it reads are the ones the vault
// sees.
//
// Impact runs without the vault mutex held (GIT-US-0163), so an
// implementation may call back into the vault — the Pando semantic searcher
// resolves its hits through Requirement, Item and Page — and may wait on git
// or the network without blocking other readers. It must treat the index as
// shared: read it through its methods, never keep what they return past the
// call.
type RequirementImpact interface {
	// Impact resolves the requirements a diff affects, in three tiers.
	Impact(ctx context.Context, ix *core.Index, q core.ImpactQuery) (core.ImpactResult, error)
}

// SetRequirementImpact installs the impact backend. Passing nil removes it,
// which is what a browser-only session leaves in place.
func (v *Vault) SetRequirementImpact(r RequirementImpact) {
	v.seams.Lock()
	defer v.seams.Unlock()
	v.impact = r
}

// requirementImpact returns the installed backend, nil when there is none.
func (v *Vault) requirementImpact() RequirementImpact {
	v.seams.Lock()
	defer v.seams.Unlock()
	return v.impact
}

// ImpactAvailable reports whether an impact backend is installed.
func (v *Vault) ImpactAvailable() bool { return v.requirementImpact() != nil }

// impactMaxDepth and impactMaxLimit bound what a caller may ask of tier 2 and
// tier 3, so one query cannot fan out into thousands of Pando calls.
const (
	impactMaxDepth = 5
	impactMaxLimit = 50
)

// impactQuery answers "impact.query" with backend over ix. It runs without
// the vault mutex (see seamCall): the resolver's Pando seams may call back
// into the vault.
func impactQuery(ctx context.Context, backend RequirementImpact, ix *core.Index, raw []byte) (any, error) {
	q, err := decodeParams[core.ImpactQuery](raw)
	if err != nil {
		return nil, err
	}
	res, err := resolveImpact(ctx, backend, ix, q)
	if err != nil {
		return nil, err
	}
	return map[string]any{"impact": res}, nil
}

// impactReportParams are the params of "impact.report": the query, and the
// page of the token-budgeted report to render from its answer.
type impactReportParams struct {
	core.ImpactQuery
	Budget int                     `json:"budget,omitempty"`
	Cursor string                  `json:"cursor,omitempty"`
	Format core.ImpactReportFormat `json:"format,omitempty"`
}

// impactReport answers "impact.report": the compact, ranked, token-budgeted
// report of GIT-US-0120 (docs/03 section 21.11, R-IMP-8 to R-IMP-10). It is
// the one renderer the MCP tool, the CLI and the HTTP API share.
//
// Like impactQuery, it runs without the vault mutex.
func impactReport(ctx context.Context, backend RequirementImpact, ix *core.Index, raw []byte) (any, error) {
	p, err := decodeParams[impactReportParams](raw)
	if err != nil {
		return nil, err
	}
	if err := checkReportPage(p.Budget, p.Format); err != nil {
		return nil, err
	}
	res, err := resolveImpact(ctx, backend, ix, p.ImpactQuery)
	if err != nil {
		return nil, err
	}
	report, err := core.RenderImpactReport(res, core.ImpactReportOptions{Budget: p.Budget, Cursor: p.Cursor, Format: p.Format})
	if errors.Is(err, core.ErrInvalidCursor) {
		return nil, failf("invalid_request", "%v", err)
	}
	if err != nil {
		return nil, fmt.Errorf("impact report: %w", err)
	}
	return map[string]any{"report": report}, nil
}

// resolveImpact validates an impact query and runs it on backend, the one
// the host installed (nil when there is none), over ix.
func resolveImpact(ctx context.Context, backend RequirementImpact, ix *core.Index, q core.ImpactQuery) (core.ImpactResult, error) {
	q.Base = strings.TrimSpace(q.Base)
	q.Head = strings.TrimSpace(q.Head)
	if q.Base == "" {
		q.Base = "HEAD"
	}
	for _, t := range q.Tiers {
		if t < core.ImpactTierDirect || t > core.ImpactTierSemantic {
			return core.ImpactResult{}, failf("invalid_request", "unknown impact tier %d: use 1, 2 or 3", t)
		}
	}
	if q.Depth < 0 || q.Depth > impactMaxDepth {
		return core.ImpactResult{}, failf("invalid_request", "depth %d is out of range: use 1 to %d", q.Depth, impactMaxDepth)
	}
	if q.Limit < 0 || q.Limit > impactMaxLimit {
		return core.ImpactResult{}, failf("invalid_request", "limit %d is out of range: use 1 to %d", q.Limit, impactMaxLimit)
	}
	if backend == nil {
		return core.ImpactResult{}, failf("unavailable",
			"the impact query is not available: this session cannot read git history or the code (browser-only mode, or a repository without git)")
	}
	if q.Story != "" {
		if _, err := ix.Item(q.Story); err != nil {
			return core.ImpactResult{}, fmt.Errorf("impact of %s: %w", q.Story, err)
		}
	}
	res, err := backend.Impact(ctx, ix, q)
	if errors.Is(err, core.ErrUnknownRevision) {
		return core.ImpactResult{}, failf("invalid_request", "%v", err)
	}
	if err != nil {
		return core.ImpactResult{}, fmt.Errorf("impact: %w", err)
	}
	if res.Hits == nil {
		res.Hits = []core.ImpactHit{}
	}
	return res, nil
}
