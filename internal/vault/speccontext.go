package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file answers "spec.context" (GIT-US-0123): the token-budgeted spec
// context of one story or task, step 1 of the agent loop (docs/03 section
// 21.11). The requirements, the Spec Delta and the related pages come from the
// index, so the method answers in every mode, browser-only included; the
// coverage of each requirement comes from the coverage seam of reqcoverage.go
// when the host installed one, and the context says `coverage: unavailable`
// when it did not — the rest of the context still answers.

// specContextParams are the params of "spec.context".
type specContextParams struct {
	ID     string                  `json:"id"`
	Budget int                     `json:"budget,omitempty"`
	Cursor string                  `json:"cursor,omitempty"`
	Format core.ImpactReportFormat `json:"format,omitempty"`
}

// specContext answers "spec.context" over ix, with the coverage of provider
// when the host installed one. It runs without the vault mutex (see
// seamCall): the coverage backend reads git and the working tree.
func specContext(ctx context.Context, provider RequirementCoverage, ix *core.Index, raw []byte) (any, error) {
	p, err := decodeParams[specContextParams](raw)
	if err != nil {
		return nil, err
	}
	id := core.ItemID(strings.TrimSpace(p.ID))
	if id == "" {
		return nil, failf("invalid_request", "spec.context needs id: the story or task")
	}
	if err := checkReportPage(p.Budget, p.Format); err != nil {
		return nil, err
	}
	sc, err := ix.SpecContext(id)
	if err != nil {
		return nil, fmt.Errorf("spec.context: %w", err)
	}
	sc.Coverage = core.SpecCoverageUnavailable
	if provider != nil {
		refs := sc.Refs()
		views := make([]core.RequirementView, 0, len(refs))
		for _, ref := range refs {
			view, err := ix.Requirement(ref)
			if err != nil {
				return nil, fmt.Errorf("coverage of %s: %w", ref, err)
			}
			views = append(views, view)
		}
		rows, err := provider.Coverage(ctx, ix, views)
		if err != nil {
			return nil, fmt.Errorf("coverage: %w", err)
		}
		sc.ApplyCoverage(rows)
	}
	report, err := core.RenderSpecContext(sc, core.ImpactReportOptions{Budget: p.Budget, Cursor: p.Cursor, Format: p.Format})
	if errors.Is(err, core.ErrInvalidCursor) {
		return nil, failf("invalid_request", "%v", err)
	}
	if err != nil {
		return nil, fmt.Errorf("spec context: %w", err)
	}
	return map[string]any{"report": report}, nil
}

// checkReportPage validates the budget and the format of a token-budgeted
// report page, the same way for every report.
func checkReportPage(budget int, format core.ImpactReportFormat) error {
	if budget < 0 || budget > core.MaxImpactBudget {
		return failf("invalid_request", "budget %d is out of range: use 1 to %d tokens (0 is %d)",
			budget, core.MaxImpactBudget, core.DefaultImpactBudget)
	}
	switch format {
	case "", core.ImpactReportJSON, core.ImpactReportText:
		return nil
	default:
		return failf("invalid_request", "unknown report format %q: use json or text", format)
	}
}
