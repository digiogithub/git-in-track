package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// This file is the creation and the removal half of a board's life cycle
// (docs/04 section 5). Parsing, validation and emission live in board.go; what
// is here is what a brand-new board needs before it can be written: a slug, a
// default column set and the refusal to overwrite a board that already exists.
//
// It is domain logic on purpose. The vault, the HTTP server and the browser all
// create boards, and none of them should own an opinion about what a default
// board looks like.

// ErrBoardExists is returned by BoardStore.Create when the slug is taken. A
// board is a file named after its id, so creating one twice would silently
// replace somebody else's board.
var ErrBoardExists = errors.New("board already exists")

// DefaultBoardBody is the body a board is created with. A board file is mostly
// front matter, and an empty body would leave the reader of the file with no
// place to say why the board exists.
const DefaultBoardBody = "## Notes\n"

// BoardDraft is everything the caller may decide about a new board. Every field
// is optional except Title: an empty Kind is kanban, an empty ID is derived
// from the title, and empty Columns are the default set for the kind.
type BoardDraft struct {
	ID            string
	Kind          BoardKind
	Title         string
	Description   string
	Projects      []ProjectKey
	Columns       []BoardColumn
	Filters       BoardFilters
	Swimlanes     BoardSwimlanes
	Card          BoardCardDisplay
	BacklogColumn string
	Author        string
	Body          string
}

// DefaultBoardColumns returns the column set a board is created with when the
// caller proposes none.
//
// The columns map status *categories* rather than status ids (docs/04 R-COL-2).
// A categories column works for a project whose workflow this team has never
// seen, which is exactly the state of a board created before the projects it
// will show are cloned; mapping explicit statuses is an edit away.
func DefaultBoardColumns(kind BoardKind) []BoardColumn {
	first := BoardColumn{ID: "todo", Name: "To Do", Categories: []StatusCategory{CategoryTodo}}
	if kind == BoardScrum {
		first = BoardColumn{
			ID: "sprint_backlog", Name: "Sprint Backlog",
			Categories: []StatusCategory{CategoryTodo},
		}
	}
	return []BoardColumn{
		first,
		{ID: "in_progress", Name: "In Progress", Categories: []StatusCategory{CategoryInProgress}},
		{ID: "done", Name: "Done", Categories: []StatusCategory{CategoryDone, CategoryCancelled}},
	}
}

// SlugifyBoardID turns a free title into a board id: lowercase, ASCII letters
// and digits, single hyphens between words, at most 48 characters
// (docs/04 section 5.1). It returns an empty string when nothing survives, and
// the caller then asks for an explicit id rather than inventing one.
func SlugifyBoardID(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = b.Len() > 0
		}
		if b.Len() >= 48 {
			break
		}
	}
	return b.String()
}

// NewBoard turns a draft into a board ready to be written, filling in the id,
// the kind, the columns, the scrum backlog column and the timestamps. It
// refuses a draft that would produce an invalid file, so that no caller has to
// repair a board it has just created.
func NewBoard(draft BoardDraft, now time.Time) (*Board, error) {
	kind := draft.Kind
	if kind == "" {
		kind = BoardKanban
	}
	if !kind.Valid() {
		return nil, fmt.Errorf("board kind %q is neither kanban nor scrum", draft.Kind)
	}
	title := strings.TrimSpace(draft.Title)
	if title == "" {
		return nil, errors.New("a board needs a title")
	}
	id := strings.TrimSpace(draft.ID)
	if id == "" {
		id = SlugifyBoardID(title)
	}
	if id == "" {
		return nil, fmt.Errorf("no board id can be derived from the title %q; give one", title)
	}
	if !boardSlugRE.MatchString(id) {
		return nil, fmt.Errorf("board id %q does not match [a-z0-9][a-z0-9-]{0,47}", id)
	}

	columns := append([]BoardColumn(nil), draft.Columns...)
	if len(columns) == 0 {
		columns = DefaultBoardColumns(kind)
	}
	board := &Board{
		ID: id, Type: "board", Kind: kind, Title: title,
		Description: strings.TrimSpace(draft.Description),
		Projects:    append([]ProjectKey(nil), draft.Projects...),
		Columns:     columns,
		Filters:     draft.Filters,
		Swimlanes:   draft.Swimlanes,
		Card:        draft.Card,
		Created:     NewTimestamp(now),
		Updated:     NewTimestamp(now),
		Author:      draft.Author,
		Body:        draft.Body,
	}
	if board.Body == "" {
		board.Body = DefaultBoardBody
	}
	if kind == BoardScrum {
		board.BacklogColumn = draft.BacklogColumn
		if board.BacklogColumn == "" {
			if _, ok := board.Column(columns[0].ID); ok {
				board.BacklogColumn = columns[0].ID
			}
		}
	}
	for _, d := range board.Validate(nil, nil) {
		if d.Severity == SeverityError {
			return nil, fmt.Errorf("%s: %s", d.Code, d.Message)
		}
	}
	return board, nil
}

// Create writes a board that does not exist yet. A slug already on disk is
// ErrBoardExists: a board file is named after its id, so overwriting is the
// only other outcome and it is never the one the caller wanted.
func (s *BoardStore) Create(ctx context.Context, b *Board) (*Board, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("board create", err)
	}
	if b == nil {
		return nil, errors.New("board create: nil board")
	}
	full := s.PathOf(b.ID)
	if _, err := s.fs.Stat(full); err == nil {
		return nil, fmt.Errorf("board %s: %w", b.ID, ErrBoardExists)
	} else if !errors.Is(err, ErrNotExist) {
		return nil, fmt.Errorf("stat %s: %w", full, err)
	}
	if err := s.fs.MkdirAll(s.dir); err != nil {
		return nil, fmt.Errorf("create %s: %w", s.dir, err)
	}
	b.Path = full
	return s.Write(ctx, b, "")
}

// Delete removes a board file, enforcing the optimistic lock when expected is
// not empty. Deleting a board deletes a view and nothing else: the items its
// cards referenced live in their own repositories and are untouched.
func (s *BoardStore) Delete(ctx context.Context, id string, expected Rev) error {
	if err := ctx.Err(); err != nil {
		return wrapContext("board delete", err)
	}
	full := s.PathOf(id)
	data, err := s.fs.ReadFile(full)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return fmt.Errorf("board %s: %w", id, ErrItemNotFound)
		}
		return fmt.Errorf("read %s: %w", full, err)
	}
	if expected != "" {
		if got := ComputeRev(data); got != expected {
			return &StaleRevisionError{Path: full, Expected: expected, Current: got}
		}
	}
	if err := s.fs.Remove(full); err != nil {
		return fmt.Errorf("remove %s: %w", full, err)
	}
	return nil
}
