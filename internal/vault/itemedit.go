package vault

import (
	"context"
	"errors"
	"fmt"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file carries the two item operations the detail view needs and the
// editor does not: flipping one acceptance-criteria checkbox, and finding out
// what still points at an item before it is deleted.

// itemTaskParams is the input of "item.task.set".
type itemTaskParams struct {
	ID string `json:"id"`
	// Line is the 1-based line of the checkbox inside the body, as the renderer
	// stamped it on the rendered input.
	Line    int    `json:"line"`
	Checked bool   `json:"checked"`
	Rev     string `json:"rev"`
}

// itemTaskSet flips one task-list checkbox in the body of an item.
//
// The rewrite happens in the core, on the body the store just read under the
// caller's `rev`, and the result is written back as an ordinary body patch. So
// a toggle is a normal rev-guarded write: it stamps `updated`, goes through the
// canonical serialiser, and loses a race with a concurrent edit exactly the way
// the editor does (docs/05-web-app.md section 8.2).
func (v *Vault) itemTaskSet(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[itemTaskParams](raw)
	if err != nil {
		return nil, err
	}
	if p.Rev == "" {
		return nil, failf("invalid_request", "a task toggle needs the rev the body was read at")
	}
	store, err := v.storeForItem(core.ItemID(p.ID))
	if err != nil {
		return nil, err
	}
	current, err := store.Get(ctx, core.ItemID(p.ID))
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", p.ID, err)
	}
	if core.Rev(p.Rev) != current.Rev {
		// Report the stale revision the way every other write does, so the web
		// app's conflict path does not need a second shape to understand.
		return nil, &core.StaleRevisionError{
			ID: current.ID, Path: current.Path, Expected: core.Rev(p.Rev), Current: current.Rev,
		}
	}

	body, err := core.SetTaskListItem(current.Body, p.Line, p.Checked)
	if err != nil {
		if errors.Is(err, core.ErrNotTaskListItem) {
			return nil, failf(core.TaskListItemMismatchCode,
				"line %d of %s is not a task-list item; the body has changed since it was rendered",
				p.Line, p.ID)
		}
		return nil, fmt.Errorf("toggle %s: %w", p.ID, err)
	}

	v.fs.begin()
	it, err := store.Update(ctx, core.ItemID(p.ID), core.ItemPatch{Body: &body}, core.Rev(p.Rev))
	if err != nil {
		return nil, fmt.Errorf("update %s: %w", p.ID, err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"item": it, "writes": writes}, nil
}

// Items returns every item the repository indexes, deleted ones included. It
// takes the vault lock.
func (v *Vault) Items() []core.Item {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.index.AllItems()
}

// ItemReferencesParams is the input of "item.references".
type ItemReferencesParams struct {
	ID string `json:"id"`
	// Team scopes the artifact half of the search to one team repository. Left
	// empty, every open team is searched, which is what a workspace holding two
	// teams needs before it deletes an item either of them may have carded.
	Team string `json:"team,omitempty"`
}

// ItemReferencesResult is what "item.references" answers.
type ItemReferencesResult struct {
	ID string `json:"id"`
	// References is everything that points at the item, sorted.
	References []core.ItemReference `json:"references"`
	// Children repeats the direct children among them, because a child is the
	// one reference a delete would leave with a dangling parent.
	Children []core.ItemReference `json:"children"`
}

// ItemReferences collects everything that points at an item: the items of the
// repository that owns it, plus the boards, sprints and retros of every open
// team repository.
//
// It is a workspace-level call because the two halves live in different
// repositories: the parent of a task sits next to it in the project, while the
// card that carries it sits in the team repo (ADR-007).
func (w *Workspace) ItemReferences(ctx context.Context, p ItemReferencesParams) (ItemReferencesResult, error) {
	id := core.ItemID(p.ID)
	if id == "" {
		return ItemReferencesResult{}, failf("invalid_request", "name the item with \"id\"")
	}
	key, _, _, err := core.ParseItemID(p.ID)
	if err != nil {
		return ItemReferencesResult{}, failf("invalid_request", "%v", err)
	}
	mount, ok := w.MountForProject(key)
	if !ok {
		return ItemReferencesResult{}, failf("not_found", "no open repository serves project %s", key)
	}

	artifacts, err := w.teamArtifacts(ctx, p.Team)
	if err != nil {
		return ItemReferencesResult{}, err
	}
	refs := core.ItemReferences(id, core.ItemReferenceSources{
		Items:     mount.Vault.Items(),
		Artifacts: artifacts,
	})

	out := ItemReferencesResult{ID: p.ID, References: refs, Children: []core.ItemReference{}}
	for _, ref := range refs {
		if ref.Kind == "item" && ref.Field == "parent" {
			out.Children = append(out.Children, ref)
		}
	}
	return out, nil
}

// teamArtifacts loads the boards, sprints and retros to search. A team whose
// artifacts cannot be listed is reported rather than skipped: a warning built
// on half the repositories would be worse than no warning at all.
func (w *Workspace) teamArtifacts(ctx context.Context, team string) (core.TeamArtifacts, error) {
	var mounts []*Mount
	if team != "" {
		m, err := w.TeamMount(team)
		if err != nil {
			return core.TeamArtifacts{}, err
		}
		mounts = []*Mount{m}
	} else {
		mounts = w.TeamMounts()
	}

	var out core.TeamArtifacts
	for _, m := range mounts {
		boards, _, err := m.Vault.Boards(ctx)
		if err != nil {
			return core.TeamArtifacts{}, err
		}
		sprints, _, err := m.Vault.Sprints(ctx)
		if err != nil {
			return core.TeamArtifacts{}, err
		}
		retros, _, err := m.Vault.Retros(ctx)
		if err != nil {
			return core.TeamArtifacts{}, err
		}
		out.Boards = append(out.Boards, boards...)
		out.Sprints = append(out.Sprints, sprints...)
		out.Retros = append(out.Retros, retros...)
	}
	return out, nil
}
