package vault

import (
	"context"
	"fmt"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the host seam for the requirement trace graph (ADR-037
// sections 4, 5 and 8; docs/03 section 21.7; GIT-US-0114). The graph needs the
// marker scanner, which walks the working tree and therefore cannot live in
// this package or in internal/core (ADR-003). So a native host installs a
// tracer — the companion's is internal/trace's Engine — and a browser-only
// session installs none, where both methods answer `unavailable`, never an
// empty trace.

// RequirementTracer is the backend a host installs with
// [Vault.SetRequirementTracer]. Both methods receive the vault's own index, so
// the answer always reflects the backlog as the vault sees it.
//
// Both run without the vault mutex held (GIT-US-0163): an implementation may
// call back into the vault, and may scan the working tree without blocking
// other readers.
type RequirementTracer interface {
	// TraceRequirement returns the code, tests, work and broken trace:
	// entries of one requirement.
	TraceRequirement(ctx context.Context, ix *core.Index, ref core.RequirementRef) (core.TracedRequirement, error)
	// TraceTouching returns the trace edges a set of changed paths and line
	// spans touches: the requirements a diff reaches directly.
	TraceTouching(ctx context.Context, ix *core.Index, changes []core.TraceChange) ([]core.TraceHit, error)
}

// SetRequirementTracer installs the trace backend. Passing nil removes it,
// which is what a browser-only session leaves in place.
func (v *Vault) SetRequirementTracer(t RequirementTracer) {
	v.seams.Lock()
	defer v.seams.Unlock()
	v.tracer = t
}

// requirementTracer returns the installed backend, nil when there is none.
func (v *Vault) requirementTracer() RequirementTracer {
	v.seams.Lock()
	defer v.seams.Unlock()
	return v.tracer
}

// TraceAvailable reports whether a trace backend is installed.
func (v *Vault) TraceAvailable() bool { return v.requirementTracer() != nil }

func traceUnavailable() error {
	return failf("unavailable",
		"the requirement trace is not available: this session has no marker scanner (browser-only mode)")
}

// traceRequirement answers "trace.requirement" with tracer over ix, without
// the vault mutex (see seamCall).
func traceRequirement(ctx context.Context, tracer RequirementTracer, ix *core.Index, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Ref string `json:"ref"`
	}](raw)
	if err != nil {
		return nil, err
	}
	ref, err := parseRequirementRef(p.Ref)
	if err != nil {
		return nil, err
	}
	if tracer == nil {
		return nil, traceUnavailable()
	}
	if _, err := ix.Requirement(ref); err != nil {
		return nil, fmt.Errorf("trace %s: %w", ref, err)
	}
	tr, err := tracer.TraceRequirement(ctx, ix, ref)
	if err != nil {
		return nil, fmt.Errorf("trace %s: %w", ref, err)
	}
	return map[string]any{"trace": tr}, nil
}

// traceTouching answers "trace.touching" with tracer over ix, without the
// vault mutex (see seamCall).
func traceTouching(ctx context.Context, tracer RequirementTracer, ix *core.Index, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Changes []core.TraceChange `json:"changes"`
	}](raw)
	if err != nil {
		return nil, err
	}
	if tracer == nil {
		return nil, traceUnavailable()
	}
	hits, err := tracer.TraceTouching(ctx, ix, p.Changes)
	if err != nil {
		return nil, fmt.Errorf("trace touching: %w", err)
	}
	if hits == nil {
		hits = []core.TraceHit{}
	}
	return map[string]any{"hits": hits}, nil
}
