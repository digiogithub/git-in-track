package core

import (
	"sort"
	"strings"
)

// This file answers the question a delete has to ask first: what still points
// at this item? It is the item-level sibling of TeamProjectReferences, and it
// is written the same way — a pure function over artifacts the caller already
// loaded, so the browser and the companion warn about exactly the same things.

// ItemReference is one place something points at an item: the parent of a
// child, the milestone of a story, a typed link, a card in a board's column
// order, the scope of a sprint, or the task a retro action was promoted into.
type ItemReference struct {
	// Kind is "item", "board", "sprint" or "retro".
	Kind string `json:"kind"`
	// ID is the referring item or artifact, and Path its file.
	ID   string `json:"id"`
	Path string `json:"path"`
	// Title is the referring item or artifact title, so a warning can name it
	// in words rather than in ids alone.
	Title string `json:"title,omitempty"`
	// Type is the item type of a referring item ("story", "task", …). It is
	// empty for a team artifact, whose kind already says what it is.
	Type string `json:"type,omitempty"`
	// Field is the front-matter field the reference sits in: "parent",
	// "milestone", "links.blocks", "order.<column>", "items", "committed",
	// "filters.milestone" or "actions.<id>".
	Field string `json:"field"`
	// Ref is the reference as it is written in the file: a bare item id inside
	// a project, `<projectKey>/<itemId>` in a team artifact.
	Ref string `json:"ref"`
}

// ItemReferenceSources are the places an item reference can hide. A nil slice
// contributes nothing, so a caller with no team repository open still gets an
// answer about the items of its own project.
type ItemReferenceSources struct {
	// Items are the items of the repositories that could refer to the target.
	Items []Item
	// Artifacts are the boards, sprints and retros of the team repository.
	Artifacts TeamArtifacts
}

// ItemReferences collects everything that points at an item.
//
// The target itself is never reported, and neither is a reference an item makes
// to itself. The result is sorted, so a message built from it is stable.
func ItemReferences(id ItemID, src ItemReferenceSources) []ItemReference {
	id = ItemID(strings.TrimSpace(string(id)))
	if id == "" {
		return nil
	}
	var out []ItemReference
	add := func(ref ItemReference) { out = append(out, ref) }

	for i := range src.Items {
		it := src.Items[i]
		if it.ID == id {
			continue
		}
		base := ItemReference{Kind: "item", ID: string(it.ID), Path: it.Path, Title: it.Title, Type: string(it.Type)}
		if sameItem(string(it.Parent), id) {
			ref := base
			ref.Field, ref.Ref = "parent", string(it.Parent)
			add(ref)
		}
		if sameItem(string(it.Milestone), id) {
			ref := base
			ref.Field, ref.Ref = "milestone", string(it.Milestone)
			add(ref)
		}
		for _, l := range it.Links {
			if sameItem(l.Target, id) {
				ref := base
				ref.Field, ref.Ref = "links."+string(l.Kind), l.Target
				add(ref)
			}
		}
	}

	out = append(out, teamArtifactReferences(id, src.Artifacts)...)

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Field < out[j].Field
	})
	return out
}

// teamArtifactReferences is the team half of ItemReferences: the board, sprint
// and retro fields that name an item rather than a project.
func teamArtifactReferences(id ItemID, artifacts TeamArtifacts) []ItemReference {
	var out []ItemReference
	add := func(kind, artifactID, filePath, title, field, ref string) {
		out = append(out, ItemReference{
			Kind: kind, ID: artifactID, Path: filePath, Title: title, Field: field, Ref: ref,
		})
	}

	for _, b := range artifacts.Boards {
		if b == nil {
			continue
		}
		// The scope of a board can name a milestone, which is an item like any
		// other and would be left dangling by a delete.
		if sameItem(b.Filters.Milestone, id) {
			add("board", b.ID, b.Path, b.Title, "filters.milestone", b.Filters.Milestone)
		}
		// Read the order as it stands; a board with none is simply a board no
		// card was ever dragged on, not a board to be given an empty one.
		order := b.Order
		for _, column := range order.Columns() {
			for _, ref := range order.Refs(column) {
				if sameItem(ref, id) {
					add("board", b.ID, b.Path, b.Title, "order."+column, ref)
				}
			}
		}
	}
	for _, s := range artifacts.Sprints {
		if s == nil {
			continue
		}
		for _, field := range []struct {
			name string
			refs []string
		}{{"items", s.Items}, {"committed", s.Committed}} {
			for _, ref := range field.refs {
				if sameItem(ref, id) {
					add("sprint", s.ID, s.Path, s.DisplayTitle(), field.name, ref)
				}
			}
		}
	}
	for _, r := range artifacts.Retros {
		if r == nil {
			continue
		}
		for _, a := range r.Actions {
			if a.Task != "" && sameItem(a.Task, id) {
				add("retro", r.ID, r.Path, r.Title, "actions."+a.ID, a.Task)
			}
		}
	}
	return out
}

// sameItem reports whether a reference names an item, whether it is written
// bare (`ACME-US-0042`, inside a project) or qualified (`ACME/ACME-US-0042`, in
// a team artifact or a cross-project link, R-LINK-2).
func sameItem(ref string, id ItemID) bool {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return false
	}
	return ItemID(bareTarget(trimmed)) == id
}
