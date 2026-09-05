package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSlugifyBoardID(t *testing.T) {
	cases := []struct {
		name  string
		title string
		want  string
	}{
		{"plain", "Delivery", "delivery"},
		{"two words", "Sprint Board", "sprint-board"},
		{"punctuation collapses", "Demo Shop — Sprint Board!", "demo-shop-sprint-board"},
		{"leading and trailing noise", "  ***Delivery***  ", "delivery"},
		{"digits are kept", "Squad 7", "squad-7"},
		{"nothing survives", "***", ""},
		{"truncated to 48", strings.Repeat("a", 60), strings.Repeat("a", 48)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SlugifyBoardID(tc.title); got != tc.want {
				t.Fatalf("SlugifyBoardID(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestNewBoard(t *testing.T) {
	now := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		draft BoardDraft
		check func(t *testing.T, b *Board, err error)
	}{
		{
			name:  "a kanban board gets the default columns",
			draft: BoardDraft{Title: "Delivery"},
			check: func(t *testing.T, b *Board, err error) {
				if err != nil {
					t.Fatalf("NewBoard: %v", err)
				}
				if b.ID != "delivery" || b.Kind != BoardKanban {
					t.Fatalf("got id %q kind %q", b.ID, b.Kind)
				}
				if len(b.Columns) != 3 || b.Columns[0].ID != "todo" {
					t.Fatalf("columns = %+v", b.Columns)
				}
				if b.BacklogColumn != "" {
					t.Fatalf("a kanban board must have no backlog column, got %q", b.BacklogColumn)
				}
				if b.Created.String() != NewTimestamp(now).String() {
					t.Fatalf("created = %s", b.Created)
				}
			},
		},
		{
			name:  "a scrum board points its backlog column at the first column",
			draft: BoardDraft{Title: "Squad Sprint", Kind: BoardScrum},
			check: func(t *testing.T, b *Board, err error) {
				if err != nil {
					t.Fatalf("NewBoard: %v", err)
				}
				if b.BacklogColumn != "sprint_backlog" {
					t.Fatalf("backlog column = %q", b.BacklogColumn)
				}
				if b.Sprint != "" {
					t.Fatalf("a new scrum board points at no sprint, got %q", b.Sprint)
				}
			},
		},
		{
			name:  "an explicit id wins over the title",
			draft: BoardDraft{Title: "Delivery", ID: "team-delivery"},
			check: func(t *testing.T, b *Board, err error) {
				if err != nil {
					t.Fatalf("NewBoard: %v", err)
				}
				if b.ID != "team-delivery" {
					t.Fatalf("id = %q", b.ID)
				}
			},
		},
		{
			name:  "a title is required",
			draft: BoardDraft{},
			check: func(t *testing.T, _ *Board, err error) {
				if err == nil {
					t.Fatal("want an error for a board with no title")
				}
			},
		},
		{
			name:  "an unusable title asks for an id",
			draft: BoardDraft{Title: "***"},
			check: func(t *testing.T, _ *Board, err error) {
				if err == nil || !strings.Contains(err.Error(), "give one") {
					t.Fatalf("err = %v", err)
				}
			},
		},
		{
			name:  "an invalid id is refused",
			draft: BoardDraft{Title: "Delivery", ID: "Not A Slug"},
			check: func(t *testing.T, _ *Board, err error) {
				if err == nil {
					t.Fatal("want an error for an invalid slug")
				}
			},
		},
		{
			name:  "an unknown kind is refused",
			draft: BoardDraft{Title: "Delivery", Kind: BoardKind("waterfall")},
			check: func(t *testing.T, _ *Board, err error) {
				if err == nil {
					t.Fatal("want an error for an unknown kind")
				}
			},
		},
		{
			name: "a column that maps nothing is refused",
			draft: BoardDraft{
				Title:   "Delivery",
				Columns: []BoardColumn{{ID: "todo", Name: "To Do"}},
			},
			check: func(t *testing.T, _ *Board, err error) {
				if err == nil {
					t.Fatal("want a validation error for a column with no mapping")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := NewBoard(tc.draft, now)
			tc.check(t, b, err)
		})
	}
}

// A board written by a creation flow must come back byte-identical through the
// emitter, which is what keeps two people creating different boards mergeable.
func TestNewBoardRoundTrips(t *testing.T) {
	now := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)
	board, err := NewBoard(BoardDraft{
		Title:       "Delivery",
		Description: "Everything the squad is working on.",
		Projects:    []ProjectKey{"DEMO", "WEB"},
		Filters:     BoardFilters{Types: []ItemType{TypeStory, TypeTask}},
		Card:        BoardCardDisplay{Show: []string{"key", "project"}},
		Author:      "jose",
	}, now)
	if err != nil {
		t.Fatalf("NewBoard: %v", err)
	}
	board.Path = ".pmngr/boards/delivery.md"

	first, err := SerializeBoard(board)
	if err != nil {
		t.Fatalf("SerializeBoard: %v", err)
	}
	parsed, err := ParseBoard(board.Path, first)
	if err != nil {
		t.Fatalf("ParseBoard: %v", err)
	}
	second, err := SerializeBoard(parsed)
	if err != nil {
		t.Fatalf("SerializeBoard again: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("the board does not round-trip:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if len(parsed.Validate(nil, nil)) != 0 {
		t.Fatalf("a created board must validate clean: %+v", parsed.Validate(nil, nil))
	}
}

func TestBoardStoreCreateAndDelete(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)

	newStore := func() *BoardStore {
		fs := NewMemFS()
		store := NewBoardStore(fs, ".pmngr")
		store.Clock = ClockFunc(func() time.Time { return now })
		return store
	}

	t.Run("create writes the file the slug names", func(t *testing.T) {
		store := newStore()
		board, err := NewBoard(BoardDraft{Title: "Delivery"}, now)
		if err != nil {
			t.Fatalf("NewBoard: %v", err)
		}
		written, err := store.Create(ctx, board)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if written.Path != ".pmngr/boards/delivery.md" {
			t.Fatalf("path = %q", written.Path)
		}
		got, err := store.Get(ctx, "delivery")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Title != "Delivery" {
			t.Fatalf("title = %q", got.Title)
		}
	})

	t.Run("create refuses to overwrite", func(t *testing.T) {
		store := newStore()
		board, err := NewBoard(BoardDraft{Title: "Delivery"}, now)
		if err != nil {
			t.Fatalf("NewBoard: %v", err)
		}
		if _, err := store.Create(ctx, board); err != nil {
			t.Fatalf("Create: %v", err)
		}
		again, err := NewBoard(BoardDraft{Title: "Delivery", Description: "another one"}, now)
		if err != nil {
			t.Fatalf("NewBoard: %v", err)
		}
		if _, err := store.Create(ctx, again); !errors.Is(err, ErrBoardExists) {
			t.Fatalf("err = %v, want ErrBoardExists", err)
		}
		kept, err := store.Get(ctx, "delivery")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if kept.Description != "" {
			t.Fatalf("the existing board was overwritten: %q", kept.Description)
		}
	})

	t.Run("delete removes the file", func(t *testing.T) {
		store := newStore()
		board, err := NewBoard(BoardDraft{Title: "Delivery"}, now)
		if err != nil {
			t.Fatalf("NewBoard: %v", err)
		}
		written, err := store.Create(ctx, board)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.Delete(ctx, "delivery", written.Rev); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := store.Get(ctx, "delivery"); err == nil {
			t.Fatal("the board is still readable after a delete")
		}
	})

	t.Run("delete honors the optimistic lock", func(t *testing.T) {
		store := newStore()
		board, err := NewBoard(BoardDraft{Title: "Delivery"}, now)
		if err != nil {
			t.Fatalf("NewBoard: %v", err)
		}
		if _, err := store.Create(ctx, board); err != nil {
			t.Fatalf("Create: %v", err)
		}
		var stale *StaleRevisionError
		if err := store.Delete(ctx, "delivery", "sha256:deadbeef"); !errors.As(err, &stale) {
			t.Fatalf("err = %v, want a StaleRevisionError", err)
		}
	})

	t.Run("delete reports a board that is not there", func(t *testing.T) {
		store := newStore()
		if err := store.Delete(ctx, "nope", ""); !errors.Is(err, ErrItemNotFound) {
			t.Fatalf("err = %v, want ErrItemNotFound", err)
		}
	})
}
