package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the host seam for requirement coverage and the verification
// stamp (ADR-037 section 7, docs/03 section 21.6, GIT-US-0116). Coverage needs
// test results, the trace graph and git history, none of which this package
// or internal/core may reach (ADR-003), so a native host installs a provider —
// the companion's is internal/trace's Coverage — and a browser-only session
// installs none: "coverage.list" and "requirement.stamp" then answer
// `unavailable`.
//
// The stamp itself is written here, through the requirement write path and
// under the requirement rev (docs/03 section 21.5), and only at the
// moments the ADR allows: when a story or task that implements or modifies
// the requirement moves to a done-category status (donestamp.go, from inside
// that write, GIT-US-0141), and on an explicit request, which is
// what `gintrack spec verify --commit` sends (GIT-US-0125) and, for one ref
// under its requirement rev, the MCP verify_requirement (GIT-US-0124). Nothing else is
// ever written: the coverage state, suspect included, is computed.

// RequirementCoverage is the backend a host installs with
// [Vault.SetRequirementCoverage]. Both methods receive the vault's own index
// and the requirements as the caller read them, so the block rev a status is
// computed against is the one the caller holds.
//
// Coverage runs without the vault mutex held ("coverage.list" and
// "spec.context", GIT-US-0163), so it may call back into the vault.
// StampEvidence does not: a stamp is a write, decided and written in one
// transaction under the mutex, so an implementation must never call back
// into the vault from it — trace.Coverage reads only the trace engine, the
// test-result evidence and git.
type RequirementCoverage interface {
	// Coverage returns one row per requirement, in the order given.
	Coverage(ctx context.Context, ix *core.Index, reqs []core.RequirementView) ([]core.CoverageRow, error)
	// StampEvidence returns, per requirement and in the order given, the
	// stamp its evidence allows — every linked test passed at one commit — or
	// the reason there is none.
	StampEvidence(ctx context.Context, ix *core.Index, reqs []core.RequirementView) ([]core.StampEvidence, error)
}

// SetRequirementCoverage installs the coverage backend. Passing nil removes
// it, which is what a browser-only session leaves in place.
func (v *Vault) SetRequirementCoverage(c RequirementCoverage) {
	v.seams.Lock()
	defer v.seams.Unlock()
	v.coverage = c
}

// requirementCoverage returns the installed backend, nil when there is none.
func (v *Vault) requirementCoverage() RequirementCoverage {
	v.seams.Lock()
	defer v.seams.Unlock()
	return v.coverage
}

// CoverageAvailable reports whether a coverage backend is installed.
func (v *Vault) CoverageAvailable() bool { return v.requirementCoverage() != nil }

func coverageUnavailable() error {
	return failf("unavailable",
		"requirement coverage is not available: this session cannot read test results or git history (browser-only mode)")
}

// coverageListParams is the filter of "coverage.list".
type coverageListParams struct {
	Project string `json:"project,omitempty"`
	Spec    string `json:"spec,omitempty"`
	// Refs limits the rows to these requirements.
	Refs stringList `json:"refs,omitempty"`
	// Status keeps the rows in one of these coverage states.
	Status stringList `json:"status,omitempty"`
}

// coverageList answers "coverage.list": one compact row per requirement,
// computed by provider over ix without the vault mutex (see seamCall).
func coverageList(ctx context.Context, provider RequirementCoverage, ix *core.Index, raw []byte) (any, error) {
	p, err := decodeParams[coverageListParams](raw)
	if err != nil {
		return nil, err
	}
	want := map[core.CoverageStatus]bool{}
	for _, s := range p.Status {
		st := core.CoverageStatus(s)
		if !st.Valid() {
			return nil, failf("invalid_request", "unknown coverage status %q: use untested, passing, failing or suspect", s)
		}
		want[st] = true
	}
	if provider == nil {
		return nil, coverageUnavailable()
	}
	var reqs []core.RequirementView
	if len(p.Refs) > 0 {
		for _, s := range p.Refs {
			ref, err := parseRequirementRef(s)
			if err != nil {
				return nil, err
			}
			view, err := ix.Requirement(ref)
			if err != nil {
				return nil, fmt.Errorf("coverage of %s: %w", ref, err)
			}
			reqs = append(reqs, view)
		}
	} else {
		f := core.RequirementFilter{Spec: core.ItemID(p.Spec)}
		if p.Project != "" {
			f.Projects = []core.ProjectKey{core.ProjectKey(p.Project)}
		}
		if reqs, err = ix.Requirements(f); err != nil {
			return nil, fmt.Errorf("list requirements: %w", err)
		}
	}
	rows, err := provider.Coverage(ctx, ix, reqs)
	if err != nil {
		return nil, fmt.Errorf("coverage: %w", err)
	}
	out := make([]core.CoverageRow, 0, len(rows))
	for _, r := range rows {
		if len(want) == 0 || want[r.Status] {
			out = append(out, r)
		}
	}
	return map[string]any{"coverage": out, "total": len(out)}, nil
}

// StampedRequirement is a requirement whose stamp was written. The type lives
// in the core because the done transition reports it too (GIT-US-0141).
type StampedRequirement = core.StampedRequirement

// UnstampedRequirement is a requirement left without a new stamp, and why:
// a coverage reason code of the evidence (failed, partial, no-results,
// no-tests), mixed-commits, no-commit, text (the tested text is not the
// current one), unchanged (the stamp already says this), stamp-newer (the
// existing stamp records a later run), unavailable (no coverage host) or
// stale (the requirement changed while the stamp was being written).
type UnstampedRequirement = core.UnstampedRequirement

// StampReport is the result of a stamp run.
type StampReport struct {
	Stamped   []StampedRequirement   `json:"stamped"`
	Unstamped []UnstampedRequirement `json:"unstamped"`
}

// Reasons a requirement is left unstamped that the vault decides itself.
const (
	stampReasonUnavailable = "unavailable"
	stampReasonUnchanged   = "unchanged"
	stampReasonNewer       = "stamp-newer"
	stampReasonStale       = "stale"
)

// stampVerified writes the `verified` stamp of each requirement whose
// evidence allows one (ADR-037 section 7): every linked test passed at one
// commit, on the text the requirement holds now. It reads each requirement
// from disk, not from the index, so it can follow another write of the same
// transaction — GIT-US-0110 calls it after applying a Spec Delta, with the
// handle of whoever moved the item to done. It never refuses: a requirement
// it cannot stamp is listed in Unstamped, and no stamp is written for it. A
// failing run never overwrites a stamp, because a failing run offers none.
//
// The caller owns the transaction: it calls v.fs.begin before and v.commit
// after, and holds the vault mutex throughout.
func (v *Vault) stampVerified(ctx context.Context, refs []core.RequirementRef, by string) (StampReport, error) {
	return v.stampVerifiedAt(ctx, refs, by, nil)
}

// stampVerifiedAt is stampVerified under the requirement revs a caller read
// (GIT-US-0124, the MCP verify_requirement). A requirement named in expected
// is stamped only if its requirement rev is still that one: when it moved,
// the write fails with the core's StaleRevisionError — current rev and the
// verified field in conflict — instead of being listed as stale, because the
// caller asked for that one stamp and must re-read. A requirement whose
// evidence allows no stamp is still reported unstamped whatever its rev: no
// write was attempted, so there is nothing to lose.
func (v *Vault) stampVerifiedAt(
	ctx context.Context, refs []core.RequirementRef, by string, expected map[core.RequirementRef]core.Rev,
) (StampReport, error) {
	report := StampReport{Stamped: []StampedRequirement{}, Unstamped: []UnstampedRequirement{}}
	provider := v.requirementCoverage()
	if provider == nil {
		for _, ref := range refs {
			report.Unstamped = append(report.Unstamped, UnstampedRequirement{Ref: ref, Reason: stampReasonUnavailable})
		}
		return report, nil
	}
	views := make([]core.RequirementView, 0, len(refs))
	for _, ref := range refs {
		store, err := v.storeForItem(ref.Spec)
		if err != nil {
			return StampReport{}, err
		}
		view, err := store.GetRequirement(ctx, ref)
		if err != nil {
			return StampReport{}, fmt.Errorf("stamp %s: %w", ref, err)
		}
		views = append(views, view)
	}
	evidence, err := provider.StampEvidence(ctx, v.index, views)
	if err != nil {
		return StampReport{}, fmt.Errorf("stamp: %w", err)
	}
	if len(evidence) != len(views) {
		return StampReport{}, fmt.Errorf("stamp: the coverage host answered %d of %d requirements", len(evidence), len(views))
	}
	for i, view := range views {
		ev := evidence[i]
		reason := stampDecision(view, ev)
		if reason != "" {
			report.Unstamped = append(report.Unstamped, UnstampedRequirement{Ref: view.Ref, Reason: reason})
			continue
		}
		stamp := *ev.Verified
		stamp.Extra = nil
		if stamp.By == "" {
			stamp.By = by
		}
		store, err := v.storeForItem(view.Ref.Spec)
		if err != nil {
			return StampReport{}, err
		}
		lock, pinned := expected[view.Ref]
		if !pinned {
			lock = view.Rev
		}
		_, written, err := store.UpdateRequirement(ctx, view.Ref, core.RequirementPatch{Verified: &stamp}, lock)
		if err != nil {
			var stale *core.StaleRevisionError
			if errors.As(err, &stale) && !pinned {
				report.Unstamped = append(report.Unstamped, UnstampedRequirement{Ref: view.Ref, Reason: stampReasonStale})
				continue
			}
			return StampReport{}, fmt.Errorf("stamp %s: %w", view.Ref, err)
		}
		out := stamp
		if written.Verified != nil {
			out = *written.Verified
			out.Extra = nil
		}
		report.Stamped = append(report.Stamped, StampedRequirement{Ref: view.Ref, Verified: out})
	}
	return report, nil
}

// stampDecision returns why ev may not be stamped onto view, "" when it may.
func stampDecision(view core.RequirementView, ev core.StampEvidence) string {
	if ev.Verified == nil {
		if ev.Reason == "" {
			return core.CoverageReasonNoResults
		}
		return ev.Reason
	}
	next := ev.Verified
	if next.Rev != view.BlockRev {
		return core.CoverageReasonText
	}
	if !core.ValidCommitID(next.Commit) {
		return core.StampReasonNoCommit
	}
	if cur := view.Verified; cur != nil {
		if cur.Rev == next.Rev && cur.Commit == next.Commit {
			return stampReasonUnchanged
		}
		if !cur.At.IsZero() && cur.At.After(next.At.Time) {
			return stampReasonNewer
		}
	}
	return ""
}

// requirementStamp answers "requirement.stamp": the explicit stamp request of
// `gintrack spec verify --commit`. It names the requirements — refs, or every
// requirement of a spec — and the handle recorded when the evidence does not
// carry one. With rev it stamps exactly one ref under that requirement rev,
// which is what the MCP verify_requirement sends: a rev that is no longer
// current fails with stale_revision rather than stamping text the caller
// never read.
func (v *Vault) requirementStamp(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Refs stringList `json:"refs,omitempty"`
		Spec string     `json:"spec,omitempty"`
		By   string     `json:"by"`
		Rev  string     `json:"rev,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	by := strings.TrimSpace(p.By)
	if by == "" {
		return nil, failf("invalid_request", "requirement.stamp needs by: the handle that ran the verification")
	}
	if v.requirementCoverage() == nil {
		return nil, coverageUnavailable()
	}
	var refs []core.RequirementRef
	for _, s := range p.Refs {
		ref, err := parseRequirementRef(s)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	if p.Spec != "" {
		rows, err := v.index.Requirements(core.RequirementFilter{Spec: core.ItemID(p.Spec)})
		if err != nil {
			return nil, fmt.Errorf("list requirements: %w", err)
		}
		for _, r := range rows {
			refs = append(refs, r.Ref)
		}
	}
	if len(refs) == 0 {
		return nil, failf("invalid_request", "requirement.stamp needs refs or a spec")
	}
	var expected map[core.RequirementRef]core.Rev
	if rev := strings.TrimSpace(p.Rev); rev != "" {
		if len(refs) != 1 || p.Spec != "" {
			return nil, failf("invalid_request", "requirement.stamp with rev stamps exactly one ref, not %d", len(refs))
		}
		if rev == "*" {
			return nil, failf("invalid_request",
				"requirement.stamp takes the requirement rev of the text that was verified, never \"*\"")
		}
		expected = map[core.RequirementRef]core.Rev{refs[0]: core.Rev(rev)}
	}
	v.fs.begin()
	report, err := v.stampVerifiedAt(ctx, refs, by, expected)
	if err != nil {
		_, _ = v.commit(ctx)
		return nil, err
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"stamped": report.Stamped, "unstamped": report.Unstamped, "writes": writes}, nil
}
