package vault

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// writableWorkspace is nativeWorkspace over writable copies of both fixtures.
func writableWorkspace(t *testing.T) *Workspace {
	t.Helper()
	w := NewWorkspace()
	if _, err := w.Attach("demo-team", RoleTeam, openFixture(t, copyFixture(t, teamFixtureRoot))); err != nil {
		t.Fatalf("attach the team repository: %v", err)
	}
	if _, err := w.Attach("demo", RoleProject, openFixture(t, copyFixture(t, fixtureRoot))); err != nil {
		t.Fatalf("attach the project repository: %v", err)
	}
	return w
}

// writableModes runs a subtest against both operating modes with writable
// repositories: a temporary copy on disk, and the in-memory browser vault.
func writableModes(t *testing.T, run func(t *testing.T, w *Workspace)) {
	t.Helper()
	t.Run("companion", func(t *testing.T) { run(t, writableWorkspace(t)) })
	t.Run("browser", func(t *testing.T) { run(t, browserWorkspace(t)) })
}

// wsFail runs one method and requires it to fail, returning the error code.
func wsFail(t *testing.T, w *Workspace, method string, params any) (code, message string) {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encode params: %v", err)
	}
	var env envelope
	if err := json.Unmarshal([]byte(w.Call(method, string(data))), &env); err != nil {
		t.Fatalf("%s returned invalid JSON: %v", method, err)
	}
	if env.OK {
		t.Fatalf("%s unexpectedly succeeded", method)
	}
	return env.Error.Code, env.Error.Message
}

func TestWorkspaceBoardList(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		result := decode[BoardListResult](t, wsCall(t, w, "board.list", nil))
		if len(result.Boards) != 1 {
			t.Fatalf("boards = %d, want 1", len(result.Boards))
		}
		b := result.Boards[0]
		if b.ID != "delivery" || b.Kind != core.BoardKanban {
			t.Fatalf("board = %+v", b)
		}
		if b.VaultID != "demo-team" {
			t.Errorf("vaultId = %q, want demo-team", b.VaultID)
		}
		if b.Columns != 4 {
			t.Errorf("columns = %d, want 4", b.Columns)
		}
		if !core.Rev(b.Rev).Valid() {
			t.Errorf("rev = %q", b.Rev)
		}
		for _, d := range b.Diagnostics {
			if d.Severity == core.SeverityError {
				t.Errorf("unexpected error diagnostic: %s", d)
			}
		}
	})
}

func TestWorkspaceBoardGet(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		view := decode[core.BoardView](t, wsCall(t, w, "board.get", map[string]any{"board": "delivery"}))

		byID := map[string]core.BoardColumnView{}
		for _, c := range view.Columns {
			byID[c.ID] = c
		}
		todo := byID["todo"]
		if len(todo.Cards) != 2 {
			t.Fatalf("todo = %d cards", len(todo.Cards))
		}
		if todo.Cards[0].Item != "DEMO-US-0002" || todo.Cards[0].Remote {
			t.Errorf("first card = %+v", todo.Cards[0])
		}
		if !todo.Cards[1].Remote || todo.Cards[1].Project != "WEB" {
			t.Errorf("second card must be the remote WEB one: %+v", todo.Cards[1])
		}
		if todo.Cards[1].Reason == "" {
			t.Error("a remote card must explain why it cannot be edited")
		}
		if got := byID["in_progress"]; len(got.Cards) != 2 || got.Cards[0].Title != "Guest checkout" {
			t.Errorf("in_progress = %+v", got.Cards)
		}
		if len(view.Unmapped) != 0 {
			t.Errorf("the fixture project maps every status: %+v", view.Unmapped)
		}
	})
}

func TestWorkspaceBoardMove(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, w *Workspace)
	}{
		{
			name: "a move between columns writes the item and the board and nothing else",
			run: func(t *testing.T, w *Workspace) {
				before := decode[core.BoardView](t, wsCall(t, w, "board.get", map[string]any{"board": "delivery"}))
				result := decode[BoardMoveResult](t, wsCall(t, w, "board.move", map[string]any{
					"board": "delivery", "ref": "DEMO/DEMO-US-0002",
					"toColumn": "in_review", "position": 0, "rev": string(before.Rev), "force": true,
				}))
				if result.Item == nil || result.Item.Status != "in_review" {
					t.Fatalf("item = %+v", result.Item)
				}
				if !result.Move.StatusChanged || result.Move.FromColumn != "todo" {
					t.Fatalf("move = %+v", result.Move)
				}
				if len(result.Writes) != 2 {
					t.Fatalf("writes = %+v, want one per repository", result.Writes)
				}
				byVault := map[string][]string{}
				for _, ws := range result.Writes {
					for _, f := range ws.Written {
						byVault[ws.VaultID] = append(byVault[ws.VaultID], f.Path)
					}
				}
				if got := byVault["demo"]; len(got) != 1 || !strings.Contains(got[0], "DEMO-US-0002") {
					t.Fatalf("project writes = %v", got)
				}
				if got := byVault["demo-team"]; len(got) != 1 || !strings.HasSuffix(got[0], "boards/delivery.md") {
					t.Fatalf("team writes = %v", got)
				}
				column := columnByID(t, result.Board, "in_review")
				if column.Cards[0].Item != "DEMO-US-0002" {
					t.Fatalf("in_review = %+v", column.Cards)
				}
			},
		},
		{
			name: "a re-order inside a column touches the board only",
			run: func(t *testing.T, w *Workspace) {
				result := decode[BoardMoveResult](t, wsCall(t, w, "board.move", map[string]any{
					"board": "delivery", "ref": "DEMO/DEMO-US-0001",
					"toColumn": "in_progress", "position": 1,
				}))
				if result.Move.StatusChanged || result.Item != nil {
					t.Fatalf("a re-order must not touch an item: %+v", result.Move)
				}
				if len(result.Writes) != 1 || result.Writes[0].VaultID != "demo-team" {
					t.Fatalf("writes = %+v", result.Writes)
				}
				column := columnByID(t, result.Board, "in_progress")
				if column.Cards[0].Ref != "WEB/WEB-T-0007" || column.Cards[1].Item != "DEMO-US-0001" {
					t.Fatalf("in_progress = %+v", column.Cards)
				}
			},
		},
		{
			name: "a move over a WIP limit is refused with an explanation",
			run: func(t *testing.T, w *Workspace) {
				code, message := wsFail(t, w, "board.move", map[string]any{
					"board": "delivery", "ref": "DEMO/DEMO-US-0002",
					"toColumn": "in_review", "position": 0,
				})
				if code != WipCode {
					t.Fatalf("code = %q, want %q (%s)", code, WipCode, message)
				}
				if !strings.Contains(message, "WIP limit") {
					t.Fatalf("message = %q", message)
				}
				// Nothing was written: the item is where it was.
				view := decode[core.BoardView](t, wsCall(t, w, "board.get", map[string]any{"board": "delivery"}))
				if got := columnByID(t, view, "todo"); got.Cards[0].Item != "DEMO-US-0002" {
					t.Fatalf("the refused move wrote something: %+v", got.Cards)
				}
			},
		},
		{
			name: "the same move goes through when it is confirmed",
			run: func(t *testing.T, w *Workspace) {
				result := decode[BoardMoveResult](t, wsCall(t, w, "board.move", map[string]any{
					"board": "delivery", "ref": "DEMO/DEMO-US-0002",
					"toColumn": "in_review", "position": 0, "force": true,
				}))
				if !result.Move.WIP.Exceeded || result.Move.WIP.Limit != 1 {
					t.Fatalf("wip = %+v", result.Move.WIP)
				}
				if !columnByID(t, result.Board, "in_review").Exceeded {
					t.Fatal("the column must be marked over its limit")
				}
			},
		},
		{
			name: "a remote card refuses to move",
			run: func(t *testing.T, w *Workspace) {
				code, message := wsFail(t, w, "board.move", map[string]any{
					"board": "delivery", "ref": "WEB/WEB-US-0031",
					"toColumn": "in_progress", "position": 0,
				})
				if code != RemoteCardCode {
					t.Fatalf("code = %q, want %q (%s)", code, RemoteCardCode, message)
				}
			},
		},
		{
			name: "a stale board revision is refused",
			run: func(t *testing.T, w *Workspace) {
				code, _ := wsFail(t, w, "board.move", map[string]any{
					"board": "delivery", "ref": "DEMO/DEMO-US-0002",
					"toColumn": "in_progress", "position": 0, "rev": "sha256:0000000000000000",
				})
				if code != "stale_revision" {
					t.Fatalf("code = %q", code)
				}
			},
		},
		{
			name: "a stale item revision is refused and the board is left alone",
			run: func(t *testing.T, w *Workspace) {
				code, _ := wsFail(t, w, "board.move", map[string]any{
					"board": "delivery", "ref": "DEMO/DEMO-US-0002",
					"toColumn": "in_progress", "position": 0,
					"itemRev": "sha256:0000000000000000", "force": true,
				})
				if code != "stale_revision" {
					t.Fatalf("code = %q", code)
				}
				view := decode[core.BoardView](t, wsCall(t, w, "board.get", map[string]any{"board": "delivery"}))
				if got := columnByID(t, view, "todo"); len(got.Cards) != 2 {
					t.Fatalf("the board was written despite the failed item write: %+v", got.Cards)
				}
			},
		},
		{
			name: "an unknown column is refused",
			run: func(t *testing.T, w *Workspace) {
				code, _ := wsFail(t, w, "board.move", map[string]any{
					"board": "delivery", "ref": "DEMO/DEMO-US-0002", "toColumn": "nope",
				})
				if code != "invalid_request" {
					t.Fatalf("code = %q", code)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writableModes(t, tc.run)
		})
	}
}

// columnByID returns a rendered column, failing when it is absent.
func columnByID(t *testing.T, view core.BoardView, id string) core.BoardColumnView {
	t.Helper()
	for _, c := range view.Columns {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no column %q in %+v", id, view.Columns)
	return core.BoardColumnView{}
}

func TestVaultBoardMoveNeedsTheWorkspace(t *testing.T) {
	v := openFixture(t, teamFixtureRoot)
	if _, err := v.Dispatch(context.Background(), "board.move", []byte(`{"board":"delivery"}`)); err == nil {
		t.Fatal("a lone vault cannot move a card across repositories")
	}
}

func TestVaultBoardGetWithoutTheProject(t *testing.T) {
	// The team repository alone: every card is remote, and the board still
	// renders (docs/04 section 7).
	v := openFixture(t, teamFixtureRoot)
	result, err := v.Dispatch(context.Background(), "board.get", []byte(`{"board":"delivery"}`))
	if err != nil {
		t.Fatalf("board.get: %v", err)
	}
	view, ok := result.(core.BoardView)
	if !ok {
		t.Fatalf("result = %T", result)
	}
	for _, c := range view.Columns {
		for _, card := range c.Cards {
			if !card.Remote {
				t.Fatalf("card %s should be remote without a clone", card.Ref)
			}
		}
	}
}
