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

// impactQuery answers "impact.query".
func (v *Vault) impactQuery(ctx context.Context, raw []byte) (any, error) {
	q, err := decodeParams[core.ImpactQuery](raw)
	if err != nil {
		return nil, err
	}
	q.Base = strings.TrimSpace(q.Base)
	q.Head = strings.TrimSpace(q.Head)
	if q.Base == "" {
		q.Base = "HEAD"
	}
	for _, t := range q.Tiers {
		if t < core.ImpactTierDirect || t > core.ImpactTierSemantic {
			return nil, failf("invalid_request", "unknown impact tier %d: use 1, 2 or 3", t)
		}
	}
	if q.Depth < 0 || q.Depth > impactMaxDepth {
		return nil, failf("invalid_request", "depth %d is out of range: use 1 to %d", q.Depth, impactMaxDepth)
	}
	if q.Limit < 0 || q.Limit > impactMaxLimit {
		return nil, failf("invalid_request", "limit %d is out of range: use 1 to %d", q.Limit, impactMaxLimit)
	}
	backend := v.requirementImpact()
	if backend == nil {
		return nil, failf("unavailable",
			"the impact query is not available: this session cannot read git history or the code (browser-only mode, or a repository without git)")
	}
	if q.Story != "" {
		if _, err := v.index.Item(q.Story); err != nil {
			return nil, fmt.Errorf("impact of %s: %w", q.Story, err)
		}
	}
	res, err := backend.Impact(ctx, v.index, q)
	if errors.Is(err, core.ErrUnknownRevision) {
		return nil, failf("invalid_request", "%v", err)
	}
	if err != nil {
		return nil, fmt.Errorf("impact: %w", err)
	}
	if res.Hits == nil {
		res.Hits = []core.ImpactHit{}
	}
	return map[string]any{"impact": res}, nil
}
