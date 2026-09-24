package vault

import (
	"errors"
	"fmt"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file connects the two halves of the first stamp moment of ADR-037
// section 7 (R-REQ-11a (a), GIT-US-0141): the core's done transition
// (FileStore.DoneHook, GIT-US-0110), which runs inside the write that moves a
// story or a task to a done-category status after its Spec Delta was applied,
// and the stamp decision of GIT-US-0116 (stampDecision over the coverage
// host's StampEvidence). The stamps are staged into the same transaction as
// the move, so they are written together with it or not at all.
//
// The hook never refuses the move for want of evidence: a requirement it
// cannot stamp — no coverage host (browser-only mode), no passing run of its
// current text in the verification cache, a spec of another project — is
// listed in the transition result's `unstamped` with its reason.

// Reasons a requirement is left unstamped that only the done transition gives.
const (
	// stampReasonMissing: the spec or the requirement block does not exist.
	stampReasonMissing = "missing"
	// stampReasonRemoved: the item's own Spec Delta removed the requirement.
	stampReasonRemoved = "removed"
	// stampReasonOtherProject: the requirement belongs to another project,
	// whose specs this transaction does not write.
	stampReasonOtherProject = "other-project"
)

// doneStamp is the DoneHook the vault installs on every project store.
func (v *Vault) doneStamp(t *core.DoneTransition) error {
	if len(t.Refs) == 0 {
		return nil
	}
	report := t.Delta
	unstamped := func(ref core.RequirementRef, reason string) {
		report.Unstamped = append(report.Unstamped, core.UnstampedRequirement{Ref: ref, Reason: reason})
	}
	removed := map[core.RequirementRef]bool{}
	for _, ref := range report.Removed {
		removed[ref] = true
	}
	provider := v.requirementCoverage()

	var (
		views []core.RequirementView
		specs []*core.Item
	)
	for _, ref := range t.Refs {
		switch {
		case removed[ref]:
			unstamped(ref, stampReasonRemoved)
			continue
		case provider == nil:
			unstamped(ref, stampReasonUnavailable)
			continue
		}
		if key, _, _, err := core.ParseItemID(string(ref.Spec)); err == nil && t.Config != nil && key != t.Config.Key {
			unstamped(ref, stampReasonOtherProject)
			continue
		}
		spec, err := t.Spec(ref.Spec)
		if err != nil {
			if errors.Is(err, core.ErrSpecDeltaConflict) || errors.Is(err, core.ErrItemNotFound) {
				unstamped(ref, stampReasonMissing)
				continue
			}
			return fmt.Errorf("stamp %s: %w", ref, err)
		}
		view, err := core.FindRequirement(spec, ref.Number, t.Config)
		if err != nil {
			if errors.Is(err, core.ErrItemNotFound) {
				unstamped(ref, stampReasonMissing)
				continue
			}
			return fmt.Errorf("stamp %s: %w", ref, err)
		}
		views = append(views, view)
		specs = append(specs, spec)
	}
	if len(views) == 0 {
		return nil
	}

	evidence, err := provider.StampEvidence(t.Context, v.index, views)
	if err == nil && len(evidence) != len(views) {
		err = fmt.Errorf("the coverage host answered %d of %d requirements", len(evidence), len(views))
	}
	if err != nil {
		// Evidence that cannot be read is no evidence: the move goes ahead
		// unstamped rather than being refused.
		for _, view := range views {
			unstamped(view.Ref, stampReasonUnavailable)
		}
		return nil //nolint:nilerr // reported as unavailable, never a refusal
	}
	for i, view := range views {
		ev := evidence[i]
		if reason := stampDecision(view, ev); reason != "" {
			unstamped(view.Ref, reason)
			continue
		}
		stamp := *ev.Verified
		stamp.Extra = nil
		if stamp.By == "" {
			stamp.By = doneStampBy(t.Item)
		}
		if err := t.StageVerified(specs[i], view.Ref.Number, stamp); err != nil {
			return fmt.Errorf("stamp %s: %w", view.Ref, err)
		}
		report.Stamped = append(report.Stamped, core.StampedRequirement{Ref: view.Ref, Verified: stamp})
	}
	return nil
}

// doneStampBy is the handle recorded on a done stamp whose evidence names
// nobody: the item's first assignee, else its author.
func doneStampBy(it *core.Item) string {
	for _, a := range it.Assignees {
		if a != "" {
			return a
		}
	}
	if it.Author != "" {
		return it.Author
	}
	return "gintrack"
}
