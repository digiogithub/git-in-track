package server

import (
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// Requirement hits (GIT-US-0118).
//
// A spec lives under `docs/.pmngr/specs/`, inside the documentation folder
// Pando's knowledge-base indexation already walks, so Pando indexes every spec
// file as it is — nothing is exported, fed or written for it (ADR-036). What
// Pando answers with is a chunk of the spec file, which on its own would
// resolve to the whole spec. So the resolver goes one step further: it locates
// the chunk in the spec's current body and maps it onto the requirement block
// whose byte range it overlaps most, and the hit becomes that requirement —
// its ref, its spec, its anchor and its live status — as a row of its own.
//
// Staleness needs no bookkeeping. The block ranges are those of the body as it
// is now, not as Pando last read it: a chunk from a block that was removed is
// found nowhere and the hit stays the spec's, and a chunk from a block whose
// text changed is still found by its unchanged head or tail.

// refineRequirement turns a knowledge-base hit on a spec into a hit on the
// requirement block the chunk landed in. It reports false, and leaves the hit
// alone, when the chunk lies outside every block — the spec's introduction —
// or is no longer in the body at all: the row then stays the spec's.
func refineRequirement(v *vault.Vault, spec core.Item, chunk string, hit *core.SearchHit) bool {
	match, ok := core.LocateRequirement(spec.ID, spec.Body, chunk)
	if !ok {
		return false
	}
	view, ok := v.Requirement(match.Block.Ref)
	if !ok {
		return false
	}
	hit.Kind = core.SearchKindRequirement
	hit.ID = core.ItemID(view.Ref.String())
	hit.Spec = view.Spec
	hit.Title = view.Title
	hit.Status = view.Status
	hit.Anchor = view.Anchor
	if view.Path != "" {
		hit.Path = view.Path
	}
	if view.Project != "" {
		hit.Project = view.Project
	}
	// The snippet is the part of the chunk inside this block, so a chunk that
	// straddles two requirements does not show the neighbour's text as the
	// reason this one matched.
	hit.Snippet = chunkSnippet(spec.Body[match.Start:match.End])
	return true
}

// wantsRequirements reports whether a query admits requirement rows. A query
// scoped to items keeps a spec hit as the spec.
func wantsRequirements(q vault.SemanticQuery) bool {
	return q.Kind == "" || q.Kind == core.SearchKindRequirement
}
