package vault

import (
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// retroOf reads one retro of the workspace, failing the test if it is gone.
func retroOf(t *testing.T, w *Workspace, id string) core.RetroView {
	t.Helper()
	return decode[core.RetroView](t, wsCall(t, w, "retro.get", map[string]any{"id": id}))
}

// actionIn finds one rendered improvement action by its id.
func actionIn(t *testing.T, actions []core.RetroActionView, id string) core.RetroActionView {
	t.Helper()
	for _, action := range actions {
		if action.ID == id {
			return action
		}
	}
	t.Fatalf("no action %q among %d", id, len(actions))
	return core.RetroActionView{}
}

func TestWorkspaceRetroRead(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		t.Run("list carries the follow-through of every retro", func(t *testing.T) {
			result := decode[RetroListResult](t, wsCall(t, w, "retro.list", nil))
			if len(result.Retros) != 1 {
				t.Fatalf("retros = %+v", result.Retros)
			}
			r := result.Retros[0]
			if r.ID != "DEMO-TEAM-R-0001" || r.Sprint != "DEMO-TEAM-S-0001" || r.State != core.RetroClosed {
				t.Fatalf("retro = %+v", r)
			}
			if r.Metrics.Actions != 3 || r.Metrics.Promoted != 2 || r.Metrics.Open != 2 {
				t.Fatalf("metrics = %+v", r.Metrics)
			}
			if len(result.Carried) != 2 {
				t.Fatalf("carried = %+v", result.Carried)
			}
		})

		t.Run("list filters by sprint, board and state", func(t *testing.T) {
			byBoard := decode[RetroListResult](t, wsCall(t, w, "retro.list",
				map[string]any{"board": "delivery"}))
			if len(byBoard.Retros) != 0 {
				t.Fatalf("the kanban board has no retro: %+v", byBoard.Retros)
			}
			open := decode[RetroListResult](t, wsCall(t, w, "retro.list",
				map[string]any{"state": "collecting"}))
			if len(open.Retros) != 0 {
				t.Fatalf("no retro is collecting: %+v", open.Retros)
			}
			code, _ := wsFail(t, w, "retro.list", map[string]any{"state": "chatting"})
			if code != "invalid_request" {
				t.Fatalf("code = %q", code)
			}
		})

		t.Run("get ranks the themes and resolves the promoted tasks", func(t *testing.T) {
			view := retroOf(t, w, "DEMO-TEAM-R-0001")
			if len(view.Notes) != 4 || len(view.Themes) != 3 || len(view.Actions) != 3 {
				t.Fatalf("view = %d notes, %d themes, %d actions",
					len(view.Notes), len(view.Themes), len(view.Actions))
			}
			if view.Themes[0].ID != "t2" || view.Themes[0].Votes != 2 {
				t.Fatalf("top theme = %+v", view.Themes[0])
			}
			live := actionIn(t, view.Actions, "a1")
			if live.Card == nil || live.Card.Source != core.CardSourceLive || !live.Open {
				t.Fatalf("a1 = %+v", live)
			}
			remote := actionIn(t, view.Actions, "a3")
			if remote.Card == nil || remote.Card.Source != core.CardSourceSnapshot {
				t.Fatalf("a3 = %+v", remote)
			}
			if view.Sprint == nil || view.Sprint.ID != "DEMO-TEAM-S-0001" {
				t.Fatalf("sprint header = %+v", view.Sprint)
			}
		})

		t.Run("an unknown retro is not found", func(t *testing.T) {
			code, _ := wsFail(t, w, "retro.get", map[string]any{"id": "DEMO-TEAM-R-0099"})
			if code != "not_found" {
				t.Fatalf("code = %q", code)
			}
		})
	})
}

func TestWorkspaceRetroCreate(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		t.Run("a second retro for the same sprint is refused", func(t *testing.T) {
			code, _ := wsFail(t, w, "retro.create", map[string]any{"sprint": "DEMO-TEAM-S-0001"})
			if code != "conflict" {
				t.Fatalf("code = %q", code)
			}
		})

		t.Run("an unknown sprint is not found", func(t *testing.T) {
			code, _ := wsFail(t, w, "retro.create", map[string]any{"sprint": "DEMO-TEAM-S-0099"})
			if code != "not_found" {
				t.Fatalf("code = %q", code)
			}
		})

		t.Run("a standalone retro carries the previous one forward", func(t *testing.T) {
			result := decode[RetroResult](t, wsCall(t, w, "retro.create", map[string]any{
				"board": "demo-scrum", "title": "Incident review",
				"date": "2026-09-20", "facilitator": "marta",
				"participants": []string{"jose", "marta"},
			}))
			r := result.Retro.Retro
			if r.ID != "DEMO-TEAM-R-0002" || r.Title != "Incident review" {
				t.Fatalf("retro = %+v", r)
			}
			if r.CarriedFrom != "DEMO-TEAM-R-0001" {
				t.Fatalf("carried_from = %q", r.CarriedFrom)
			}
			if len(result.Retro.Carried) != 2 {
				t.Fatalf("carried = %+v", result.Retro.Carried)
			}
			if !strings.Contains(result.Retro.Body, "## Went well") {
				t.Fatalf("body = %q", result.Retro.Body)
			}
			if len(result.Writes) != 1 || len(result.Writes[0].Written) != 1 {
				t.Fatalf("writes = %+v", result.Writes)
			}
			path := result.Writes[0].Written[0].Path
			if path != ".pmngr/retros/DEMO-TEAM-R-0002-incident-review.md" {
				t.Fatalf("path = %q", path)
			}
		})
	})
}

func TestWorkspaceRetroUpdate(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		rev := func() string { return string(retroOf(t, w, "DEMO-TEAM-R-0001").Retro.Rev) }

		t.Run("a stale revision is refused", func(t *testing.T) {
			code, _ := wsFail(t, w, "retro.update", map[string]any{
				"id": "DEMO-TEAM-R-0001", "rev": "sha256:0000000000000000",
				"patch": map[string]any{"title": "Nope"},
			})
			if code != "stale_revision" {
				t.Fatalf("code = %q", code)
			}
		})

		t.Run("notes are added, moved and removed one bullet at a time", func(t *testing.T) {
			result := decode[RetroResult](t, wsCall(t, w, "retro.update", map[string]any{
				"id": "DEMO-TEAM-R-0001", "rev": rev(),
				"patch": map[string]any{
					"addNotes": []map[string]any{
						{"category": "to_improve", "text": "Snapshot ageing went unnoticed", "author": "jose"},
					},
				},
			}))
			if len(result.Retro.Notes) != 5 {
				t.Fatalf("notes = %d", len(result.Retro.Notes))
			}
			added := noteIn(t, result.Retro.Notes, "n5")
			if added.Category != core.CategoryToImprove || added.Author != "jose" {
				t.Fatalf("added = %+v", added)
			}

			moved := decode[RetroResult](t, wsCall(t, w, "retro.update", map[string]any{
				"id": "DEMO-TEAM-R-0001", "rev": rev(),
				"patch": map[string]any{
					"updateNotes": []map[string]any{{"id": "n5", "category": "puzzle"}},
				},
			}))
			if got := countNotes(moved.Retro.Notes, core.CategoryPuzzle); got != 2 {
				t.Fatalf("puzzles = %d", got)
			}

			gone := decode[RetroResult](t, wsCall(t, w, "retro.update", map[string]any{
				"id": "DEMO-TEAM-R-0001", "rev": rev(),
				"patch": map[string]any{"removeNotes": []string{"n5"}},
			}))
			if len(gone.Retro.Notes) != 4 {
				t.Fatalf("notes = %d", len(gone.Retro.Notes))
			}
		})

		t.Run("themes and votes are replaced as one decision", func(t *testing.T) {
			result := decode[RetroResult](t, wsCall(t, w, "retro.update", map[string]any{
				"id": "DEMO-TEAM-R-0001", "rev": rev(),
				"patch": map[string]any{
					"themes": []map[string]any{
						{"id": "t1", "title": "Merged theme", "category": "to_improve", "notes": []string{"n1", "n2"}},
					},
					"votes": map[string][]string{"t1": {"jose", "marta"}},
				},
			}))
			if len(result.Retro.Themes) != 1 || result.Retro.Themes[0].Votes != 2 {
				t.Fatalf("themes = %+v", result.Retro.Themes)
			}
			if len(result.Retro.Themes[0].NoteTexts) != 2 {
				t.Fatalf("grouped notes = %+v", result.Retro.Themes[0].NoteTexts)
			}
		})

		t.Run("an improvement action is selected, edited and mirrored into the body", func(t *testing.T) {
			result := decode[RetroResult](t, wsCall(t, w, "retro.update", map[string]any{
				"id": "DEMO-TEAM-R-0001", "rev": rev(),
				"patch": map[string]any{
					"addActions": []map[string]any{
						{"title": "Alert on snapshot age", "owner": "jose", "due": "2026-09-30"},
					},
				},
			}))
			added := actionIn(t, result.Retro.Actions, "a4")
			if added.Owner != "jose" || added.Due.String() != "2026-09-30" || !added.Open {
				t.Fatalf("a4 = %+v", added)
			}
			if !strings.Contains(result.Retro.Body, "- [ ] a4 — Alert on snapshot age (jose, 2026-09-30)") {
				t.Fatalf("body = %q", result.Retro.Body)
			}

			closed := decode[RetroResult](t, wsCall(t, w, "retro.update", map[string]any{
				"id": "DEMO-TEAM-R-0001", "rev": rev(),
				"patch": map[string]any{
					"updateActions": []map[string]any{{"id": "a4", "status": "done"}},
				},
			}))
			if done := actionIn(t, closed.Retro.Actions, "a4"); !done.Done || done.Open {
				t.Fatalf("a4 = %+v", done)
			}
			if !strings.Contains(closed.Retro.Body, "- [x] a4 —") {
				t.Fatalf("body = %q", closed.Retro.Body)
			}

			gone := decode[RetroResult](t, wsCall(t, w, "retro.update", map[string]any{
				"id": "DEMO-TEAM-R-0001", "rev": rev(),
				"patch": map[string]any{"removeActions": []string{"a4"}},
			}))
			if len(gone.Retro.Actions) != 3 {
				t.Fatalf("actions = %d", len(gone.Retro.Actions))
			}
		})

		t.Run("an unknown category, note or action is refused", func(t *testing.T) {
			for _, patch := range []map[string]any{
				{"addNotes": []map[string]any{{"category": "vibes", "text": "x"}}},
				{"updateNotes": []map[string]any{{"id": "n99", "text": "x"}}},
				{"removeActions": []string{"a99"}},
				{"state": []string{}},
			} {
				code, _ := wsFail(t, w, "retro.update", map[string]any{
					"id": "DEMO-TEAM-R-0001", "rev": rev(), "patch": patch,
				})
				if code == "" {
					t.Fatalf("patch %v was accepted", patch)
				}
			}
		})
	})
}

func TestWorkspaceRetroPromote(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		rev := func() string { return string(retroOf(t, w, "DEMO-TEAM-R-0001").Retro.Rev) }

		t.Run("promoting an already promoted action is refused", func(t *testing.T) {
			code, _ := wsFail(t, w, "retro.promote", map[string]any{
				"id": "DEMO-TEAM-R-0001", "rev": rev(), "action": "a1", "project": "DEMO",
			})
			if code != RetroActionPromotedCode {
				t.Fatalf("code = %q", code)
			}
		})

		t.Run("a project nobody cloned is refused, not half written", func(t *testing.T) {
			code, message := wsFail(t, w, "retro.promote", map[string]any{
				"id": "DEMO-TEAM-R-0001", "rev": rev(), "action": "a2", "project": "WEB",
			})
			if code != RetroNotClonedCode || !strings.Contains(message, "copy the action as Markdown") {
				t.Fatalf("code = %q, message = %q", code, message)
			}
		})

		t.Run("an undeclared project is not found", func(t *testing.T) {
			code, _ := wsFail(t, w, "retro.promote", map[string]any{
				"id": "DEMO-TEAM-R-0001", "rev": rev(), "action": "a2", "project": "NOPE",
			})
			if code != "not_found" {
				t.Fatalf("code = %q", code)
			}
		})

		t.Run("an action becomes a task that links back to the retro", func(t *testing.T) {
			result := decode[RetroResult](t, wsCall(t, w, "retro.promote", map[string]any{
				"id": "DEMO-TEAM-R-0001", "rev": rev(), "action": "a2", "project": "DEMO",
			}))
			if result.Task == nil {
				t.Fatal("no task was created")
			}
			task := result.Task
			if task.Type != core.TypeTask || task.Title != "Agree a 30-minute cap on the daily sync" {
				t.Fatalf("task = %+v", task)
			}
			if len(task.Assignees) != 1 || task.Assignees[0] != "marta" {
				t.Fatalf("assignees = %v", task.Assignees)
			}
			if len(task.Labels) != 1 || task.Labels[0] != core.RetroTaskLabel {
				t.Fatalf("labels = %v", task.Labels)
			}
			if !strings.Contains(task.Body, "Promoted from retro DEMO-TEAM-R-0001 (action a2).") {
				t.Fatalf("body = %q", task.Body)
			}

			promoted := actionIn(t, result.Retro.Actions, "a2")
			want := "DEMO/" + string(task.ID)
			if promoted.Task != want || promoted.Status != core.ActionPromoted {
				t.Fatalf("a2 = %+v, want task %s", promoted.RetroAction, want)
			}
			if promoted.Card == nil || promoted.Card.Title != task.Title {
				t.Fatalf("a2 card = %+v", promoted.Card)
			}
			if !strings.Contains(result.Retro.Body, "→ `"+want+"`") {
				t.Fatalf("body = %q", result.Retro.Body)
			}
			// Two repositories were written: the project clone and the team repo.
			if len(result.Writes) != 2 {
				t.Fatalf("writes = %+v", result.Writes)
			}
		})
	})
}

// noteIn finds one note by its id.
func noteIn(t *testing.T, notes []core.RetroNote, id string) core.RetroNote {
	t.Helper()
	for _, note := range notes {
		if note.ID == id {
			return note
		}
	}
	t.Fatalf("no note %q among %d", id, len(notes))
	return core.RetroNote{}
}

// countNotes counts the notes of one category.
func countNotes(notes []core.RetroNote, category core.RetroCategory) int {
	total := 0
	for _, note := range notes {
		if note.Category == category {
			total++
		}
	}
	return total
}
