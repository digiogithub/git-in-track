package core

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// readFixtureBoard loads the board of testdata/fixtures/team-basic.
func readFixtureBoard(t *testing.T) *Board {
	t.Helper()
	const p = "../../testdata/fixtures/team-basic/.pmngr/boards/delivery.md"
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	board, err := ParseBoard(".pmngr/boards/delivery.md", data)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return board
}

// demoConfig is the workflow of the fixture project, hand-built so that the
// board tests do not depend on the project.yaml loader.
func demoConfig() *ProjectConfig {
	return &ProjectConfig{
		Schema: 1,
		Key:    "DEMO",
		Name:   "Demo Shop",
		Workflow: Workflow{
			Initial: "backlog",
			Statuses: []StatusDef{
				{ID: "backlog", Name: "Backlog", Category: CategoryTodo},
				{ID: "todo", Name: "To Do", Category: CategoryTodo},
				{ID: "in_progress", Name: "In Progress", Category: CategoryInProgress},
				{ID: "in_review", Name: "In Review", Category: CategoryInProgress},
				{ID: "done", Name: "Done", Category: CategoryDone, Terminal: true},
				{ID: "cancelled", Name: "Cancelled", Category: CategoryCancelled, Terminal: true},
			},
			Transitions: map[Status][]Status{
				"backlog":     {"todo", "cancelled"},
				"todo":        {"in_progress", "backlog", "cancelled"},
				"in_progress": {"in_review", "todo", "cancelled"},
				"in_review":   {"done", "in_progress", "cancelled"},
				"done":        {"in_progress"},
				"cancelled":   {"backlog"},
			},
		},
	}
}

func TestParseBoard(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		check func(t *testing.T, b *Board)
	}{
		{
			name: "the fixture board round-trips",
			text: "",
			check: func(t *testing.T, b *Board) {
				if b.ID != "delivery" || b.Kind != BoardKanban {
					t.Fatalf("id/kind = %q/%q", b.ID, b.Kind)
				}
				if len(b.Columns) != 4 {
					t.Fatalf("columns = %d, want 4", len(b.Columns))
				}
				if got := b.Order.Refs("todo"); len(got) != 2 || got[0] != "DEMO/DEMO-US-0002" {
					t.Fatalf("order[todo] = %v", got)
				}
				if !b.Order.Has("done") || len(b.Order.Refs("done")) != 0 {
					t.Fatalf("order[done] should be present and empty")
				}
			},
		},
		{
			name: "a project override replaces the wildcard entirely",
			text: `---
id: b
type: board
kind: kanban
title: B
columns:
  - id: doing
    statuses:
      "*": [in_progress]
      WEB: [doing, wip]
---
`,
			check: func(t *testing.T, b *Board) {
				c := b.Columns[0]
				if got := c.StatusesFor("WEB", nil); len(got) != 2 || got[0] != "doing" {
					t.Fatalf("WEB statuses = %v", got)
				}
				if got := c.StatusesFor("DEMO", nil); len(got) != 1 || got[0] != "in_progress" {
					t.Fatalf("DEMO statuses = %v", got)
				}
			},
		},
		{
			name: "a categories column expands through the project workflow",
			text: `---
id: b
type: board
kind: kanban
title: B
columns:
  - id: open
    categories: [todo]
---
`,
			check: func(t *testing.T, b *Board) {
				got := b.Columns[0].StatusesFor("DEMO", demoConfig())
				if len(got) != 2 || got[0] != "backlog" || got[1] != "todo" {
					t.Fatalf("statuses = %v", got)
				}
				if b.Columns[0].StatusesFor("WEB", nil) != nil {
					t.Fatalf("an uncloned project cannot expand categories")
				}
			},
		},
		{
			name: "unknown keys survive a parse",
			text: `---
id: b
type: board
kind: kanban
title: B
columns:
  - id: open
    categories: [todo]
x-vendor: keep-me
---
`,
			check: func(t *testing.T, b *Board) {
				if b.Extra["x-vendor"] != "keep-me" {
					t.Fatalf("extra = %v", b.Extra)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var board *Board
			if tc.text == "" {
				board = readFixtureBoard(t)
			} else {
				var err error
				board, err = ParseBoard("b.md", []byte(tc.text))
				if err != nil {
					t.Fatalf("ParseBoard: %v", err)
				}
			}
			tc.check(t, board)
		})
	}
}

func TestParseBoardErrors(t *testing.T) {
	tests := []struct {
		name string
		text string
		code Code
	}{
		{name: "no front matter", text: "# just markdown\n", code: CodeFMMissing},
		{name: "broken YAML", text: "---\nid: [\n---\n", code: CodeFMYAML},
		{name: "front matter is not a mapping", text: "---\n- a\n- b\n---\n", code: CodeFMYAML},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseBoard("b.md", []byte(tc.text))
			if err == nil {
				t.Fatal("want an error")
			}
			pe, ok := err.(*ParseError)
			if !ok {
				t.Fatalf("want *ParseError, got %T", err)
			}
			if pe.Code != tc.code {
				t.Fatalf("code = %s, want %s", pe.Code, tc.code)
			}
		})
	}
}

func TestSerializeBoardIsIdempotent(t *testing.T) {
	board := readFixtureBoard(t)
	first, err := SerializeBoard(board)
	if err != nil {
		t.Fatalf("SerializeBoard: %v", err)
	}
	again, err := ParseBoard(board.Path, first)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	second, err := SerializeBoard(again)
	if err != nil {
		t.Fatalf("re-serialize: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("serialization is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if !strings.Contains(string(first), "  todo:\n    - DEMO/DEMO-US-0002\n") {
		t.Fatalf("order must be one ref per line:\n%s", first)
	}
	if !strings.Contains(string(first), "  done: []\n") {
		t.Fatalf("an empty order list stays present:\n%s", first)
	}
}

func TestSerializeBoardIsStableAcrossRuns(t *testing.T) {
	// The statuses mapping is a Go map; emission must not depend on its
	// iteration order, or every write would produce a spurious diff.
	board := readFixtureBoard(t)
	first, err := SerializeBoard(board)
	if err != nil {
		t.Fatalf("SerializeBoard: %v", err)
	}
	for i := 0; i < 20; i++ {
		next, err := SerializeBoard(board)
		if err != nil {
			t.Fatalf("SerializeBoard: %v", err)
		}
		if string(next) != string(first) {
			t.Fatalf("run %d differs from the first", i)
		}
	}
}

func TestBoardValidate(t *testing.T) {
	declared := []ProjectKey{"DEMO", "WEB"}
	configs := map[ProjectKey]*ProjectConfig{"DEMO": demoConfig()}

	tests := []struct {
		name string
		text string
		want []Code
	}{
		{
			name: "the fixture board is clean",
			text: "",
			want: nil,
		},
		{
			name: "a column with both mappings",
			text: `---
id: b
type: board
kind: kanban
title: B
columns:
  - id: open
    categories: [todo]
    statuses: { "*": [todo] }
---
`,
			want: []Code{CodeBoardColMapping},
		},
		{
			name: "a column with neither mapping",
			text: `---
id: b
type: board
kind: kanban
title: B
columns:
  - id: open
---
`,
			want: []Code{CodeBoardColMapping},
		},
		{
			name: "two columns claiming one status",
			text: `---
id: b
type: board
kind: kanban
title: B
columns:
  - id: one
    statuses: { "*": [in_progress] }
  - id: two
    statuses: { "*": [in_progress] }
---
`,
			want: []Code{CodeBoardStatusAmbiguous},
		},
		{
			name: "a duplicate column id",
			text: `---
id: b
type: board
kind: kanban
title: B
columns:
  - id: one
    statuses: { "*": [todo] }
  - id: one
    statuses: { "*": [done] }
---
`,
			want: []Code{CodeBoardColumns},
		},
		{
			name: "an id that disagrees with the file name",
			text: `---
id: other
type: board
kind: kanban
title: B
columns:
  - id: one
    statuses: { "*": [todo] }
---
`,
			want: []Code{CodeBoardID},
		},
		{
			name: "an unknown kind",
			text: `---
id: b
type: board
kind: freeform
title: B
columns:
  - id: one
    statuses: { "*": [todo] }
---
`,
			want: []Code{CodeBoardKind},
		},
		{
			name: "a sprint on a kanban board",
			text: `---
id: b
type: board
kind: kanban
title: B
sprint: DEMO-TEAM-S-0001
columns:
  - id: one
    statuses: { "*": [todo] }
---
`,
			want: []Code{CodeBoardSprintKind},
		},
		{
			name: "a ref that is not <KEY>/<ITEM-ID>",
			text: `---
id: b
type: board
kind: kanban
title: B
columns:
  - id: one
    statuses: { "*": [todo] }
order:
  one:
    - DEMO-US-0001
---
`,
			want: []Code{CodeBoardRefFormat},
		},
		{
			name: "a ref into an undeclared project",
			text: `---
id: b
type: board
kind: kanban
title: B
columns:
  - id: one
    statuses: { "*": [todo] }
order:
  one:
    - MOB/MOB-T-0001
---
`,
			want: []Code{CodeBoardUnknownProject},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var board *Board
			if tc.text == "" {
				board = readFixtureBoard(t)
			} else {
				var err error
				board, err = ParseBoard("b.md", []byte(tc.text))
				if err != nil {
					t.Fatalf("ParseBoard: %v", err)
				}
			}
			got := board.Validate(declared, configs)
			for _, want := range tc.want {
				if !hasCode(got, want) {
					t.Fatalf("missing %s in %v", want, codesOf(got))
				}
			}
			if tc.want == nil {
				for _, d := range got {
					if d.Severity == SeverityError {
						t.Fatalf("unexpected error: %s", d)
					}
				}
			}
		})
	}
}

func TestBoardOrder(t *testing.T) {
	t.Run("insert clamps the position", func(t *testing.T) {
		o := NewBoardOrder()
		o.Set("todo", []string{"A/A-T-0001", "A/A-T-0002"})
		o.Insert("todo", "A/A-T-0003", 99)
		if got := o.Refs("todo"); got[2] != "A/A-T-0003" {
			t.Fatalf("refs = %v", got)
		}
	})
	t.Run("remove drops a ref from every column", func(t *testing.T) {
		o := NewBoardOrder()
		o.Set("a", []string{"A/A-T-0001"})
		o.Set("b", []string{"A/A-T-0001"})
		o.Remove("A/A-T-0001")
		if len(o.Refs("a")) != 0 || len(o.Refs("b")) != 0 {
			t.Fatalf("refs survived: %v %v", o.Refs("a"), o.Refs("b"))
		}
	})
	t.Run("clone is deep", func(t *testing.T) {
		o := NewBoardOrder()
		o.Set("a", []string{"A/A-T-0001"})
		c := o.Clone()
		c.Set("a", []string{"A/A-T-0002"})
		if o.Refs("a")[0] != "A/A-T-0001" {
			t.Fatalf("the original was mutated: %v", o.Refs("a"))
		}
	})
}

func TestBoardStore(t *testing.T) {
	board := readFixtureBoard(t)
	data, err := SerializeBoard(board)
	if err != nil {
		t.Fatalf("SerializeBoard: %v", err)
	}
	fs := NewMemFSFromMap(map[string]string{
		".pmngr/boards/delivery.md": string(data),
		".pmngr/boards/notes.txt":   "not a board",
	})
	store := NewBoardStore(fs, ".pmngr")
	ctx := context.Background()

	t.Run("list skips everything that is not markdown", func(t *testing.T) {
		boards, diags, err := store.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(boards) != 1 || boards[0].ID != "delivery" {
			t.Fatalf("boards = %v", boards)
		}
		if len(diags) != 0 {
			t.Fatalf("diagnostics = %v", diags)
		}
	})

	t.Run("an absent folder is not an error", func(t *testing.T) {
		empty := NewBoardStore(NewMemFS(), ".pmngr")
		boards, _, err := empty.List(ctx)
		if err != nil || len(boards) != 0 {
			t.Fatalf("List = %v, %v", boards, err)
		}
	})

	t.Run("get reports a missing board as not found", func(t *testing.T) {
		if _, err := store.Get(ctx, "nope"); err == nil {
			t.Fatal("want an error")
		}
	})

	t.Run("write enforces the optimistic lock", func(t *testing.T) {
		loaded, err := store.Get(ctx, "delivery")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if _, err := store.Write(ctx, loaded, "sha256:0000000000000000"); err == nil {
			t.Fatal("want a stale-revision error")
		}
		store.Clock = ClockFunc(func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) })
		before := loaded.Rev
		written, err := store.Write(ctx, loaded, before)
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		if written.Updated.String() != "2026-09-04T10:00:00Z" {
			t.Fatalf("updated = %s", written.Updated)
		}
		if written.Rev == before {
			t.Fatal("the rev must change with the file")
		}
	})
}
