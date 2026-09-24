package vault

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The single-requirement half of the CoreApi contract (ADR-037, docs/03
// section 21.5, GIT-US-0107): list, get, create and update one requirement of a
// spec. Every read returns two hashes, `rev` — the requirement rev, the write
// token — and `blockRev` — the block rev, what a verification stamp records —
// and every update quotes `rev`. A write to one requirement never invalidates
// the rev of another requirement of the same spec.

// requirementListParams is the filter of "requirement.list".
type requirementListParams struct {
	Project string   `json:"project,omitempty"`
	Spec    string   `json:"spec,omitempty"`
	Status  []string `json:"status,omitempty"`
	// Q keeps the requirements whose ref, title or text hold every term.
	Q string `json:"q,omitempty"`
	// Text includes each requirement's text, statement and scenarios; a list
	// row carries neither by default.
	Text           bool `json:"text,omitempty"`
	IncludeDeleted bool `json:"includeDeleted,omitempty"`
}

// parseRequirementRef reads the ref of a requirement call. A "<KEY>/" project
// qualifier is accepted when it names the spec's own project.
func parseRequirementRef(s string) (core.RequirementRef, error) {
	s = strings.TrimSpace(s)
	if key, bare, ok := strings.Cut(s, "/"); ok {
		ref, err := core.ParseRequirementRef(bare)
		if err != nil {
			return core.RequirementRef{}, failf("invalid_request", "%v", err)
		}
		if specKey, _, _, err := core.ParseItemID(string(ref.Spec)); err != nil || string(specKey) != key {
			return core.RequirementRef{}, failf("invalid_request", "ref %q: %s does not belong to project %s", s, ref.Spec, key)
		}
		return ref, nil
	}
	ref, err := core.ParseRequirementRef(s)
	if err != nil {
		return core.RequirementRef{}, failf("invalid_request", "%v", err)
	}
	return ref, nil
}

// requirementList answers "requirement.list": one row per requirement.
func (v *Vault) requirementList(raw []byte) (any, error) {
	p, err := decodeParams[requirementListParams](raw)
	if err != nil {
		return nil, err
	}
	f := core.RequirementFilter{Spec: core.ItemID(p.Spec), Text: p.Q, IncludeDeleted: p.IncludeDeleted}
	if p.Project != "" {
		f.Projects = []core.ProjectKey{core.ProjectKey(p.Project)}
	}
	for _, s := range p.Status {
		f.Statuses = append(f.Statuses, core.Status(s))
	}
	rows, err := v.index.Requirements(f)
	if err != nil {
		return nil, fmt.Errorf("list requirements: %w", err)
	}
	if !p.Text {
		for i := range rows {
			rows[i] = rows[i].Row()
		}
	}
	return map[string]any{"requirements": rows, "total": len(rows)}, nil
}

// requirementGet answers "requirement.get": one requirement with its text and
// both of its hashes, plus the file rev of its spec.
func (v *Vault) requirementGet(raw []byte) (any, error) {
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
	view, err := v.index.Requirement(ref)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", ref, err)
	}
	spec, err := v.index.Item(ref.Spec)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", ref, err)
	}
	return map[string]any{"requirement": view, "specRev": spec.Rev}, nil
}

// requirementCreate answers "requirement.create": it allocates R<n> as max + 1
// over the spec's blocks, its requirements: keys and every inbound ref in the
// index (R-REQ-5), and appends the block.
func (v *Vault) requirementCreate(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Spec string `json:"spec"`
		core.RequirementDraft
	}](raw)
	if err != nil {
		return nil, err
	}
	spec := core.ItemID(p.Spec)
	if spec == "" {
		return nil, failf("invalid_request", "requirement.create needs a spec")
	}
	store, err := v.storeForItem(spec)
	if err != nil {
		return nil, err
	}
	v.fs.begin()
	schemaBefore := store.Schema()
	it, view, err := store.CreateRequirement(ctx, spec, p.RequirementDraft, v.index.RequirementRefsTo(spec))
	if err != nil {
		return nil, fmt.Errorf("create requirement in %s: %w", spec, err)
	}
	return v.requirementResult(ctx, it, view, schemaBefore, store)
}

// requirementUpdate answers "requirement.update": a sparse patch of one
// requirement's block and requirements: entry, conditional on its requirement
// rev. The rev is required; "*" is the explicit, unsafe waiver (R-REV-3b).
func (v *Vault) requirementUpdate(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Ref   string                `json:"ref"`
		Patch core.RequirementPatch `json:"patch"`
		Rev   string                `json:"rev"`
	}](raw)
	if err != nil {
		return nil, err
	}
	ref, err := parseRequirementRef(p.Ref)
	if err != nil {
		return nil, err
	}
	expected := core.Rev(p.Rev)
	switch p.Rev {
	case "":
		return nil, failf("precondition_required",
			"requirement.update needs the requirement rev of %s (rev, from requirement.get), not its blockRev", ref)
	case "*":
		expected = ""
	}
	store, err := v.storeForItem(ref.Spec)
	if err != nil {
		return nil, err
	}
	v.fs.begin()
	schemaBefore := store.Schema()
	it, view, err := store.UpdateRequirement(ctx, ref, p.Patch, expected)
	if err != nil {
		return nil, fmt.Errorf("update %s: %w", ref, err)
	}
	return v.requirementResult(ctx, it, view, schemaBefore, store)
}

// requirementResult reports a requirement write: the requirement as written,
// the spec's new file rev, the WriteSet and, when the write raised the schema,
// schemaUpgraded (ADR-037 section 11).
func (v *Vault) requirementResult(
	ctx context.Context, it *core.Item, view core.RequirementView, schemaBefore int, store *core.FileStore,
) (any, error) {
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	if indexed, err := v.index.Requirement(view.Ref); err == nil {
		view.Project = indexed.Project
	}
	out := map[string]any{"requirement": view, "specRev": it.Rev, "writes": writes}
	return withSchemaUpgrade(out, schemaBefore, store), nil
}

// mergeHits folds requirement hits into the item and page hits of one search,
// in the order core.Index.Search ranks: score, then kind, id and path.
func mergeHits(hits, more []core.SearchHit, limit int) []core.SearchHit {
	out := append(append([]core.SearchHit(nil), hits...), more...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Path < b.Path
	})
	if limit <= 0 {
		limit = core.DefaultLimit
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
