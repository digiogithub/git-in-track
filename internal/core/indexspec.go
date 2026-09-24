package core

import (
	"fmt"
	"sort"
	"strings"
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
		if delta, ok := ix.deltas[id]; ok {
			ix.derivedDiags = append(ix.derivedDiags, specDeltaDiagnostics(it, delta, cfg)...)
		}
		if d, ok := SchemaFeatureDiagnostic(it, cfg); ok {
			ix.derivedDiags = append(ix.derivedDiags, d)
		}
	}
	ix.checkDeltaTargets()
}

// addSpecDelta parses the Spec Delta of a story or task and, while it is
// unapplied, records each MODIFIED and REMOVED target as a pending modifies
// relation of the item, so impact and coverage see changes under review
// (R-DELTA-4). A delta counts as applied once the item reaches a done-category
// status; a cancelled item's delta is never applied, so it proposes nothing.
func (ix *Index) addSpecDelta(g *Graph, it *Item) {
	if !carriesSpecDelta(it.Type) || !mayHaveSpecDelta(it.Body) {
		return
	}
	delta := ParseSpecDelta(it.Body)
	if len(delta.Operations) == 0 && len(delta.Findings) == 0 {
		return
	}
	ix.deltas[it.ID] = delta
	if cfg := ix.configOf(it); cfg != nil {
		switch cfg.CategoryOf(it.Status) {
		case CategoryDone, CategoryCancelled:
			return
		}
	}
	for _, op := range delta.Operations {
		if op.Ref != nil && (op.Op == DeltaModified || op.Op == DeltaRemoved) {
			g.addPendingLink(it.ID, Link{Kind: LinkModifies, Target: op.Ref.String()})
		}
	}
}

// checkDeltaTargets reports every Spec Delta target the index does not hold as
// W-DELTA-DANGLING: an unknown spec, a MODIFIED or REMOVED ref whose block its
// spec does not declare, and a Supersedes: ref likewise. A warning, because
// the spec or the block may arrive in a later merge (R-DELTA-5).
func (ix *Index) checkDeltaTargets() {
	blocks := map[ItemID]map[int]bool{}
	for _, id := range sortedIDs(ix.byID) {
		delta, ok := ix.deltas[id]
		if !ok {
			continue
		}
		it := ix.byID[id]
		report := func(field string, line int, format string, args ...any) {
			ix.derivedDiags = append(ix.derivedDiags, Diagnostic{
				Code: CodeWarnDeltaDangling, Severity: SeverityWarning, Path: it.Path, Field: field,
				Message: fmt.Sprintf("line %d of the body: ", line) + fmt.Sprintf(format, args...),
			})
		}
		check := func(field string, line int, what string, spec ItemID, ref *RequirementRef) {
			s, ok := ix.byID[spec]
			switch {
			case !ok || s.Type != TypeSpec:
				report(field, line, "%s names unknown spec %s", what, spec)
			case ref != nil && !ix.hasRequirementBlock(blocks, s, ref.Number):
				report(field, line, "%s names unknown requirement %s", what, ref)
			}
		}
		for _, op := range delta.Operations {
			field := "body." + op.Target()
			check(field, op.Line, string(op.Op), op.Spec, op.Ref)
			if op.Supersedes != nil {
				check(field, op.Line, "Supersedes: of "+string(op.Op)+" "+op.Target(), op.Supersedes.Spec, op.Supersedes)
			}
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
// the index holds in a link target, item-level or requirement-level, or in a
// Spec Delta (a MODIFIED or REMOVED target, an applied ADDED, a Supersedes:
// line), sorted by number. They keep a number reserved after its block was
// deleted by hand (R-REQ-5).
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
		if delta, ok := ix.deltas[id]; ok {
			for _, ref := range delta.Refs() {
				add([]Link{{Target: ref.String()}})
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

// addRequirementLinks records the links of a spec's requirements: entries, whose
// source is the requirement ref rather than the spec (R-REQ-13).
func addRequirementLinks(g *Graph, it *Item) {
	if it.Type != TypeSpec {
		return
	}
	for _, key := range it.Requirements.Keys() {
		e := it.Requirements[key]
		n, ok := ParseRequirementKey(key)
		if e == nil || !ok {
			continue
		}
		from := ItemID(RequirementRef{Spec: it.ID, Number: n}.String())
		for _, l := range e.Links {
			g.addLink(from, l)
		}
	}
}

// hasRequirementBlock reports whether a spec's body declares the block R<n>. A
// requirement whose block was deleted by hand is dangling even when its
// requirements: entry survives (R-REQ-6). cache holds the parsed block numbers
// of each spec for the duration of one integrity check.
func (ix *Index) hasRequirementBlock(cache map[ItemID]map[int]bool, spec *Item, n int) bool {
	nums, ok := cache[spec.ID]
	if !ok {
		nums = map[int]bool{}
		for _, b := range ParseSpecBody(spec.ID, spec.Body).Blocks {
			nums[b.Ref.Number] = true
		}
		cache[spec.ID] = nums
	}
	return nums[n]
}

// RequirementFilter narrows Index.Requirements. Every set field must match.
type RequirementFilter struct {
	Projects []ProjectKey
	Spec     ItemID
	Statuses []Status
	// Text keeps the requirements whose ref, title or text contain every
	// whitespace-separated term, case-insensitively.
	Text string
	// IncludeDeleted also lists the requirements of soft-deleted specs.
	IncludeDeleted bool
}

// Requirements lists the requirements of every indexed spec that f admits, as
// rows of their own (docs/03 section 21), sorted by spec id and then in body
// order. Each carries its project, requirement rev and block rev.
func (ix *Index) Requirements(f RequirementFilter) ([]RequirementView, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	projects := map[ProjectKey]bool{}
	for _, p := range f.Projects {
		projects[p] = true
	}
	statuses := map[Status]bool{}
	for _, s := range f.Statuses {
		statuses[s] = true
	}
	terms := strings.Fields(strings.ToLower(f.Text))
	out := []RequirementView{}
	for _, id := range sortedIDs(ix.byID) {
		it := ix.byID[id]
		if it.Type != TypeSpec || (it.Deleted && !f.IncludeDeleted) || (f.Spec != "" && it.ID != f.Spec) {
			continue
		}
		project := ix.projectOf(it)
		if len(projects) > 0 && !projects[project] {
			continue
		}
		views, err := SpecRequirements(it, ix.configOf(it))
		if err != nil {
			return nil, err
		}
		for _, v := range views {
			if len(statuses) > 0 && !statuses[v.Status] {
				continue
			}
			if len(terms) > 0 && requirementScore(v, terms) == 0 {
				continue
			}
			v.Project = project
			out = append(out, v)
		}
	}
	return out, nil
}

// Requirement returns one requirement of an indexed spec, with its text.
func (ix *Index) Requirement(ref RequirementRef) (RequirementView, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	it, ok := ix.byID[ref.Spec]
	if !ok {
		return RequirementView{}, fmt.Errorf("%s: spec %s: %w", ref, ref.Spec, ErrItemNotFound)
	}
	v, err := FindRequirement(it, ref.Number, ix.configOf(it))
	if err != nil {
		return RequirementView{}, err
	}
	v.Project = ix.projectOf(it)
	return v, nil
}

// requirementScore ranks a requirement against lower-cased search terms with
// the weights Search uses for items: an exact ref outranks a title match,
// which outranks a match in the text. Zero means some term did not match.
func requirementScore(v RequirementView, terms []string) float64 {
	ref := strings.ToLower(v.Ref.String())
	title := strings.ToLower(v.Title)
	text := strings.ToLower(v.Text)
	score := 0.0
	for _, t := range terms {
		switch {
		case ref == t:
			score += scoreID
		case strings.Contains(title, t):
			score += scoreTitle
		case strings.Contains(text, t) || strings.Contains(ref, t):
			score += scoreBody
		default:
			return 0
		}
	}
	return score
}

// SearchRequirements runs the substring search over requirement blocks: every
// requirement of a live spec is a hit of its own, of kind "requirement", whose
// id is the requirement ref and whose path is the spec's file (docs/03 section
// 21). Hits are ranked like Search ranks items.
func (ix *Index) SearchRequirements(q string, limit int) []SearchHit {
	terms := strings.Fields(strings.ToLower(q))
	if len(terms) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	views, err := ix.Requirements(RequirementFilter{})
	if err != nil {
		return nil
	}
	var hits []SearchHit
	for _, v := range views {
		score := requirementScore(v, terms)
		if score == 0 {
			continue
		}
		hits = append(hits, SearchHit{
			Kind: SearchKindRequirement, ID: ItemID(v.Ref.String()), Path: v.Path, Title: v.Title,
			Project: v.Project, Score: score, Snippet: snippet(v.Text, terms[0]), Source: SearchSourceCore,
			Spec: v.Spec, Status: v.Status,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].ID < hits[j].ID
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}
