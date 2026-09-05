package vault

import (
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// Creating a board writes one file into the team repository and nothing else,
// and the board it returns is already rendered over the open repositories.
func TestWorkspaceBoardCreate(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		check  func(t *testing.T, w *Workspace, result BoardCreateResult)
	}{
		{
			name:   "a kanban board with the default columns",
			params: map[string]any{"title": "Squad Delivery", "author": "jose"},
			check: func(t *testing.T, w *Workspace, result BoardCreateResult) {
				if result.Board.ID != "squad-delivery" {
					t.Fatalf("id = %q", result.Board.ID)
				}
				if result.Board.Kind != core.BoardKanban {
					t.Fatalf("kind = %q", result.Board.Kind)
				}
				if len(result.Board.Columns) != 3 {
					t.Fatalf("columns = %d, want 3", len(result.Board.Columns))
				}
				if len(result.Writes) != 1 || len(result.Writes[0].Written) != 1 {
					t.Fatalf("writes = %+v", result.Writes)
				}
				if got := result.Writes[0].Written[0].Path; got != ".pmngr/boards/squad-delivery.md" {
					t.Fatalf("written path = %q", got)
				}
				listed := decode[BoardListResult](t, wsCall(t, w, "board.list", nil))
				if len(listed.Boards) != 3 {
					t.Fatalf("boards after the create = %d, want 3", len(listed.Boards))
				}
			},
		},
		{
			name: "a scrum board gets a backlog column and no sprint",
			params: map[string]any{
				"title": "Squad Sprint Board", "kind": "scrum",
				"projects": []string{"DEMO"},
			},
			check: func(t *testing.T, _ *Workspace, result BoardCreateResult) {
				if result.Board.Kind != core.BoardScrum {
					t.Fatalf("kind = %q", result.Board.Kind)
				}
				if result.Board.SprintInfo != nil {
					t.Fatalf("a new scrum board runs no sprint: %+v", result.Board.SprintInfo)
				}
				if result.Board.Columns[0].ID != "sprint_backlog" {
					t.Fatalf("first column = %q", result.Board.Columns[0].ID)
				}
			},
		},
		{
			name: "an explicit column set is kept",
			params: map[string]any{
				"title": "Triage",
				"columns": []map[string]any{
					{"id": "inbox", "name": "Inbox", "categories": []string{"todo"}, "wip": 3},
					{"id": "shipped", "name": "Shipped", "categories": []string{"done"}},
				},
			},
			check: func(t *testing.T, _ *Workspace, result BoardCreateResult) {
				if len(result.Board.Columns) != 2 || result.Board.Columns[0].WIP != 3 {
					t.Fatalf("columns = %+v", result.Board.Columns)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writableModes(t, func(t *testing.T, w *Workspace) {
				result := decode[BoardCreateResult](t, wsCall(t, w, "board.create", tc.params))
				tc.check(t, w, result)
			})
		})
	}
}

func TestWorkspaceBoardCreateRefusals(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		code   string
	}{
		{"a taken slug", map[string]any{"title": "Delivery"}, BoardExistsCode},
		{"no title", map[string]any{}, "validation_failed"},
		{"an unknown kind", map[string]any{"title": "Delivery Two", "kind": "waterfall"}, "validation_failed"},
		{
			"a column that maps nothing",
			map[string]any{
				"title":   "Delivery Two",
				"columns": []map[string]any{{"id": "todo", "name": "To Do"}},
			},
			"validation_failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writableModes(t, func(t *testing.T, w *Workspace) {
				code, message := wsFail(t, w, "board.create", tc.params)
				if code != tc.code {
					t.Fatalf("code = %q (%s), want %q", code, message, tc.code)
				}
			})
		})
	}
}

func TestWorkspaceBoardDelete(t *testing.T) {
	t.Run("a board no sprint points at", func(t *testing.T) {
		writableModes(t, func(t *testing.T, w *Workspace) {
			result := decode[BoardDeleteResult](t, wsCall(t, w, "board.delete",
				map[string]any{"board": "delivery"}))
			if result.Board != "delivery" {
				t.Fatalf("board = %q", result.Board)
			}
			if len(result.Writes) != 1 || len(result.Writes[0].Removed) != 1 {
				t.Fatalf("writes = %+v", result.Writes)
			}
			if got := result.Writes[0].Removed[0]; got != ".pmngr/boards/delivery.md" {
				t.Fatalf("removed = %q", got)
			}
			listed := decode[BoardListResult](t, wsCall(t, w, "board.list", nil))
			for _, b := range listed.Boards {
				if b.ID == "delivery" {
					t.Fatal("the board is still listed after the delete")
				}
			}
		})
	})

	t.Run("a board a running sprint points at is refused", func(t *testing.T) {
		writableModes(t, func(t *testing.T, w *Workspace) {
			code, message := wsFail(t, w, "board.delete", map[string]any{"board": "demo-scrum"})
			if code != SprintActiveCode {
				t.Fatalf("code = %q (%s), want %q", code, message, SprintActiveCode)
			}
			if _, err := w.BoardView(t.Context(), "demo-scrum"); err != nil {
				t.Fatalf("the board must still be there: %v", err)
			}
		})
	})

	t.Run("a stale revision is refused", func(t *testing.T) {
		writableModes(t, func(t *testing.T, w *Workspace) {
			code, _ := wsFail(t, w, "board.delete",
				map[string]any{"board": "delivery", "rev": "sha256:deadbeef"})
			if code != "stale_revision" {
				t.Fatalf("code = %q, want stale_revision", code)
			}
		})
	})

	t.Run("an unknown board is not found", func(t *testing.T) {
		writableModes(t, func(t *testing.T, w *Workspace) {
			code, _ := wsFail(t, w, "board.delete", map[string]any{"board": "nope"})
			if code != "not_found" {
				t.Fatalf("code = %q, want not_found", code)
			}
		})
	})
}
