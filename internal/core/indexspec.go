package core

import (
	"fmt"
	"sort"
)

// The spec half of the index (ADR-037, docs/03 section 21): the per-spec
// findings doctor reports, and the inbound refs requirement allocation needs.

// checkSpecs reports the requirement findings of every spec and the
// E-SCHEMA-FEATURE finding of every spec construct in a project below
// SpecSchema. It runs on every rebuild, not on file load, because both depend
// on project.yaml — the workflow and the schema — which can change while the
// spec file does not.
func (ix *Index) checkSpecs() {
	for _, id := range sortedIDs(ix.byID) {
		it := ix.byID[id]
		cfg := ix.configOf(it)
		if it.Type == TypeSpec {
			ix.derivedDiags = append(ix.derivedDiags, RequirementDiagnostics(it, cfg)...)
		}
		if d, ok := SchemaFeatureDiagnostic(it, cfg); ok {
			ix.derivedDiags = append(ix.derivedDiags, d)
		}
	}
}

// configOf returns the configuration of the project whose folder holds an item,
// or nil when the project.yaml could not be decoded.
func (ix *Index) configOf(it *Item) *ProjectConfig {
	key := ix.fileProject[it.Path]
	if key == "" {
		key = ix.projectOf(it)
	}
	for _, p := range ix.projects {
		if p.Key == key && !p.Team {
			return p.Config
		}
	}
	return nil
}

// RequirementRefsTo returns every requirement ref to a spec that some item of
// the index holds in a link target, item-level or requirement-level, sorted by
// number. They keep a number reserved after its block was deleted by hand
// (R-REQ-5). Spec Delta headings are added by the Spec Delta parser.
func (ix *Index) RequirementRefsTo(spec ItemID) []RequirementRef {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.requirementRefsTo(spec)
}

func (ix *Index) requirementRefsTo(spec ItemID) []RequirementRef {
	seen := map[int]bool{}
	var out []RequirementRef
	add := func(links []Link) {
		for _, l := range links {
			ref, err := ParseRequirementRef(bareTarget(l.Target))
			if err != nil || ref.Spec != spec || seen[ref.Number] {
				continue
			}
			seen[ref.Number] = true
			out = append(out, ref)
		}
	}
	for _, id := range sortedIDs(ix.byID) {
		it := ix.byID[id]
		add(it.Links)
		for _, key := range it.Requirements.Keys() {
			if e := it.Requirements[key]; e != nil {
				add(e.Links)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// NextRequirementNumber returns the number the next requirement of a spec gets,
// max + 1 over its blocks, its requirements: keys and every inbound ref the
// index holds (R-REQ-5).
func (ix *Index) NextRequirementNumber(spec ItemID) (int, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	it, ok := ix.byID[spec]
	if !ok {
		return 0, fmt.Errorf("next requirement of %s: %w", spec, ErrItemNotFound)
	}
	if it.Type != TypeSpec {
		return 0, fmt.Errorf("next requirement of %s: a %s has no requirements", spec, it.Type)
	}
	return NextRequirementNumber(it, ix.requirementRefsTo(spec)), nil
}
