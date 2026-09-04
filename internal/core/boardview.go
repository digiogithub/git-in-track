package core

import (
	"fmt"
	"sort"
	"strings"
)

// This file turns a parsed Board and the items of the repositories that happen
// to be open into the columns the UI renders. It is pure: no file system, no
// clock, no host. The adapters gather the inputs, this decides what the board
// looks like, and the same decision therefore holds in the browser and in the
// companion process.

// BoardCard is one card of a rendered board. A card whose project is not cloned
// carries Remote and a Reason and nothing else: reading its title and status
// from a committed snapshot is GIT-US-0019 (docs/04 sections 6 and 7).
type BoardCard struct {
	Ref     string     `json:"ref"`
	Project ProjectKey `json:"project"`
	Item    ItemID     `json:"item"`
	// Declared reports whether team.yaml lists the project at all. An
	// undeclared ref renders as inert text (R-ORD-1).
	Declared bool `json:"declared"`
	// Remote reports a project no open repository serves.
	Remote bool `json:"remote"`
	// VaultID is the repository the item was read from.
	VaultID string `json:"vaultId,omitempty"`

	Title     string    `json:"title,omitempty"`
	Type      ItemType  `json:"type,omitempty"`
	Status    Status    `json:"status,omitempty"`
	Priority  Priority  `json:"priority,omitempty"`
	Assignees []string  `json:"assignees,omitempty"`
	Labels    []string  `json:"labels,omitempty"`
	Estimate  *float64  `json:"estimate,omitempty"`
	Milestone ItemID    `json:"milestone,omitempty"`
	Parent    ItemID    `json:"parent,omitempty"`
	Due       Date      `json:"due,omitempty"`
	Updated   Timestamp `json:"updated,omitempty"`
	Path      string    `json:"path,omitempty"`
	Rev       Rev       `json:"rev,omitempty"`

	// Reason explains, in one sentence, why a card cannot be edited here.
	Reason string `json:"reason,omitempty"`
}

// cardOf projects an item onto a card.
func cardOf(key ProjectKey, vaultID string, it *Item) BoardCard {
	return BoardCard{
		Ref:       Ref{Project: key, Item: it.ID}.String(),
		Project:   key,
		Item:      it.ID,
		Declared:  true,
		VaultID:   vaultID,
		Title:     it.Title,
		Type:      it.Type,
		Status:    it.Status,
		Priority:  it.Priority,
		Assignees: append([]string(nil), it.Assignees...),
		Labels:    append([]string(nil), it.Labels...),
		Estimate:  clonePtr(it.Estimate),
		Milestone: it.Milestone,
		Parent:    it.Parent,
		Due:       it.Due,
		Updated:   it.Updated,
		Path:      it.Path,
		Rev:       it.Rev,
	}
}

// BoardColumnView is one rendered column.
type BoardColumnView struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	WIP       int         `json:"wip,omitempty"`
	Color     string      `json:"color,omitempty"`
	Collapsed bool        `json:"collapsed,omitempty"`
	Cards     []BoardCard `json:"cards"`
	// Exceeded reports the live WIP condition of R-COL-5, recomputed on every
	// render rather than stored anywhere.
	Exceeded bool `json:"exceeded"`
}

// BoardView is a board plus the cards it currently shows.
type BoardView struct {
	ID            string            `json:"id"`
	Kind          BoardKind         `json:"kind"`
	Title         string            `json:"title"`
	Description   string            `json:"description,omitempty"`
	Path          string            `json:"path"`
	Rev           Rev               `json:"rev"`
	TeamVaultID   string            `json:"teamVaultId,omitempty"`
	Projects      []ProjectKey      `json:"projects"`
	Filters       BoardFilters      `json:"filters"`
	Swimlanes     BoardSwimlanes    `json:"swimlanes"`
	Card          BoardCardDisplay  `json:"card"`
	Sprint        string            `json:"sprint,omitempty"`
	BacklogColumn string            `json:"backlogColumn,omitempty"`
	Columns       []BoardColumnView `json:"columns"`
	// Unmapped holds the items that match the filters but whose status maps to
	// no column. They are surfaced, never hidden (R-COL-4).
	Unmapped    []BoardCard  `json:"unmapped"`
	Body        string       `json:"body,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// BoardSource is one cloned project the board can draw cards from.
type BoardSource struct {
	Project ProjectKey
	VaultID string
	// Config is the project workflow; nil disables the category mapping for
	// this project, exactly as a missing project.yaml would.
	Config *ProjectConfig
	// Items are the candidate items of the project, bodies excluded.
	Items []Item
}

// BoardInput is everything BuildBoardView needs beyond the board itself.
type BoardInput struct {
	// Declared is every project key of team.yaml, in declaration order.
	Declared []ProjectKey
	// Sources are the projects an open repository serves.
	Sources []BoardSource
	// TeamVaultID names the repository the board file lives in.
	TeamVaultID string
}

// BuildBoardView renders a board: it applies the filters, assigns every item to
// the column its status maps to, orders each column by the board's `order:`
// list and marks the columns that are over their WIP limit.
//
// It never fails. Everything that would be an error elsewhere — an undeclared
// project, a dead ref, an unmapped status — is a diagnostic on the view, so
// that a board with one broken line still renders.
func BuildBoardView(b *Board, in BoardInput) BoardView {
	view := BoardView{
		ID: b.ID, Kind: b.Kind, Title: b.Title, Description: b.Description,
		Path: b.Path, Rev: b.Rev, TeamVaultID: in.TeamVaultID,
		Filters: b.Filters, Swimlanes: b.Swimlanes, Card: b.Card,
		Sprint: b.Sprint, BacklogColumn: b.BacklogColumn,
		Body:        b.Body,
		Columns:     []BoardColumnView{},
		Unmapped:    []BoardCard{},
		Diagnostics: []Diagnostic{},
	}
	scope := b.Scope(in.Declared)
	view.Projects = scope

	sources := map[ProjectKey]BoardSource{}
	for _, s := range in.Sources {
		sources[s.Project] = s
	}

	// Column assignment. A card lands in the first column that claims its
	// status; a status claimed twice is a validation error, reported once.
	columns := make([]BoardColumnView, 0, len(b.Columns))
	buckets := make([][]BoardCard, len(b.Columns))
	for _, c := range b.Columns {
		columns = append(columns, BoardColumnView{
			ID: c.ID, Name: c.Name, WIP: c.WIP, Color: c.Color,
			Collapsed: c.Collapsed, Cards: []BoardCard{},
		})
	}

	placed := map[string]bool{}
	for _, key := range scope {
		source, cloned := sources[key]
		if !cloned {
			continue
		}
		for i := range source.Items {
			it := &source.Items[i]
			if !b.matches(it, source.Config) {
				continue
			}
			card := cardOf(key, source.VaultID, it)
			index := -1
			for ci, c := range b.Columns {
				if c.Shows(key, source.Config, it.Status) {
					index = ci
					break
				}
			}
			if index < 0 {
				card.Reason = fmt.Sprintf("status %s maps to no column of this board", it.Status)
				view.Unmapped = append(view.Unmapped, card)
				continue
			}
			buckets[index] = append(buckets[index], card)
			placed[card.Ref] = true
		}
	}

	// Refs the order list carries for projects nobody cloned. They keep the
	// position the board gives them, because nothing else can tell where they
	// belong (docs/04 section 7).
	for ci, c := range b.Columns {
		for _, raw := range b.Order.Refs(c.ID) {
			ref, err := ParseRef(raw)
			if err != nil {
				view.Diagnostics = append(view.Diagnostics, Diagnostic{
					Code: CodeBoardRefFormat, Severity: SeverityWarning, Path: b.Path,
					Field: "order", Message: err.Error(),
				})
				continue
			}
			if placed[ref.String()] {
				continue
			}
			declared := containsKey(in.Declared, ref.Project)
			if _, cloned := sources[ref.Project]; cloned {
				// The project is open but the ref points at nothing, or at an
				// item this board filters out: R-ORD-2 ignores it on read.
				if declared {
					view.Diagnostics = append(view.Diagnostics, Diagnostic{
						Code: CodeBoardRefDead, Severity: SeverityWarning, Path: b.Path,
						Field:   "order",
						Message: fmt.Sprintf("ref %s is not on this board any more; it is pruned on the next write", raw),
					})
				}
				continue
			}
			card := BoardCard{
				Ref: ref.String(), Project: ref.Project, Item: ref.Item,
				Declared: declared, Remote: true,
			}
			if declared {
				card.Reason = fmt.Sprintf("project %s is not cloned on this machine; clone it to move this card", ref.Project)
			} else {
				card.Reason = fmt.Sprintf("project %s is not declared in %s", ref.Project, TeamFileName)
				view.Diagnostics = append(view.Diagnostics, Diagnostic{
					Code: CodeBoardUnknownProject, Severity: SeverityWarning, Path: b.Path,
					Field: "order", Message: card.Reason,
				})
			}
			buckets[ci] = append(buckets[ci], card)
			placed[card.Ref] = true
		}
	}

	for i, c := range b.Columns {
		cards := sortBoardCards(buckets[i], b.Order.Refs(c.ID))
		columns[i].Cards = cards
		columns[i].Exceeded = c.WIP > 0 && len(cards) > c.WIP
	}
	view.Columns = columns
	sortBoardCardsByRank(view.Unmapped)
	return view
}

// sortBoardCards puts the cards the order list names first, in that order, and
// appends the rest by priority then updated descending (R-ORD-2).
func sortBoardCards(cards []BoardCard, order []string) []BoardCard {
	rank := make(map[string]int, len(order))
	for i, ref := range order {
		if _, seen := rank[ref]; !seen {
			rank[ref] = i
		}
	}
	listed := make([]BoardCard, 0, len(cards))
	rest := make([]BoardCard, 0, len(cards))
	for _, c := range cards {
		if _, ok := rank[c.Ref]; ok {
			listed = append(listed, c)
			continue
		}
		rest = append(rest, c)
	}
	sort.SliceStable(listed, func(i, j int) bool { return rank[listed[i].Ref] < rank[listed[j].Ref] })
	sortBoardCardsByRank(rest)
	return append(listed, rest...)
}

// sortBoardCardsByRank is the implicit order of a card the board does not list:
// priority first, then the most recently updated, then the ref so that the
// result is total and therefore reproducible.
func sortBoardCardsByRank(cards []BoardCard) {
	sort.SliceStable(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		if ra, rb := priorityRank(a.Priority), priorityRank(b.Priority); ra != rb {
			return ra < rb
		}
		if !a.Updated.Equal(b.Updated.Time) {
			return a.Updated.After(b.Updated.Time)
		}
		return a.Ref < b.Ref
	})
}

// matches applies the board filters of docs/04 section 5.3 to one item.
func (b *Board) matches(it *Item, cfg *ProjectConfig) bool {
	if it.Deleted {
		return false
	}
	f := b.Filters
	if len(f.Types) > 0 && !containsType(f.Types, it.Type) {
		return false
	}
	if len(f.Priorities) > 0 && !containsPriority(f.Priorities, it.Priority) {
		return false
	}
	if len(f.LabelsAny) > 0 && !anyOf(it.Labels, f.LabelsAny) {
		return false
	}
	for _, want := range f.LabelsAll {
		if !containsFold(it.Labels, want) {
			return false
		}
	}
	if anyOf(it.Labels, f.LabelsNone) {
		return false
	}
	if len(f.Assignees) > 0 && !matchBoardAssignees(it.Assignees, f.Assignees) {
		return false
	}
	if f.Milestone != "" && !refEqualsID(f.Milestone, it.Milestone) {
		return false
	}
	if f.Sprint != "" && f.Sprint != it.Sprint {
		return false
	}
	if !f.DueBefore.IsZero() && (it.Due.IsZero() || !it.Due.Before(f.DueBefore.Time)) {
		return false
	}
	if !f.UpdatedSince.IsZero() && it.Updated.Before(f.UpdatedSince.Time) {
		return false
	}
	if f.Query != "" && !matchesText(it, f.Query) {
		return false
	}
	if !f.IncludeClosed && cfg != nil {
		// A terminal item still belongs on the board when a column claims it:
		// the "Done" column is the point of the rule. It is only the terminal
		// items outside such a column that `include_closed` hides.
		switch cfg.CategoryOf(it.Status) {
		case CategoryDone, CategoryCancelled:
			return b.columnClaims(it.Status, cfg)
		}
	}
	return true
}

// columnClaims reports whether any column of the board maps a status, for the
// project the configuration belongs to.
func (b *Board) columnClaims(status Status, cfg *ProjectConfig) bool {
	for _, c := range b.Columns {
		if c.Shows(cfg.Key, cfg, status) {
			return true
		}
	}
	return false
}

// matchBoardAssignees applies the `assignees` filter, honouring the
// `unassigned` pseudo-handle of docs/04 section 5.3.
func matchBoardAssignees(assignees, want []string) bool {
	for _, w := range want {
		if w == "unassigned" || w == Unassigned {
			if len(assignees) == 0 {
				return true
			}
			continue
		}
		if containsFold(assignees, w) {
			return true
		}
	}
	return false
}

// anyOf reports whether list holds any of want, case-insensitively.
func anyOf(list, want []string) bool {
	for _, w := range want {
		if containsFold(list, w) {
			return true
		}
	}
	return false
}

// refEqualsID compares a filter value that may be qualified (`ACME/ACME-M-0003`)
// with a bare item id.
func refEqualsID(filter string, id ItemID) bool {
	if filter == string(id) {
		return true
	}
	if _, bare, ok := strings.Cut(filter, "/"); ok {
		return bare == string(id)
	}
	return false
}

// BoardMove is one requested card move: where the card goes and, when the move
// crosses columns, which status it acquires.
type BoardMove struct {
	// Ref is the card, `<projectKey>/<itemId>`.
	Ref Ref
	// ToColumn is the target column id.
	ToColumn string
	// Position is the 0-based index in the target column; a negative value
	// appends.
	Position int
	// Status overrides the status the column mapping would pick. It is what the
	// UI sends when a column maps several statuses and the user chose one.
	Status Status
}

// BoardMovePlan is what a move implies, computed before anything is written.
type BoardMovePlan struct {
	// FromColumn is the column the card sits in today, empty when the board
	// does not list it.
	FromColumn string
	ToColumn   string
	// Status is the status the item must acquire, empty when the move is a
	// re-order inside one column and no status change is needed.
	Status Status
	// StatusChanged reports whether the item file has to be written at all.
	StatusChanged bool
	// Choices are every status the target column maps for this project. More
	// than one means the caller may pick (docs/05 section 9).
	Choices []Status
	// WIPLimit and WIPUsed describe the target column after the move.
	WIPLimit int
	WIPUsed  int
	// WIPExceeded reports that the move puts the column over its limit. The
	// limit is advisory: the caller decides whether to confirm (R-COL-5).
	WIPExceeded bool
}

// PlanMove computes what moving a card implies, without writing anything. view
// is the board as it is rendered right now, which is what makes the WIP count
// and the current column knowable.
//
// cfg is the workflow of the card's project, nil for a project nobody cloned.
func PlanMove(b *Board, view BoardView, move BoardMove, cfg *ProjectConfig) (BoardMovePlan, error) {
	column, ok := b.Column(move.ToColumn)
	if !ok {
		return BoardMovePlan{}, fmt.Errorf("board %s has no column %q", b.ID, move.ToColumn)
	}
	plan := BoardMovePlan{ToColumn: column.ID, WIPLimit: column.WIP}
	ref := move.Ref.String()

	var current BoardCard
	found := false
	for _, c := range view.Columns {
		for _, card := range c.Cards {
			if card.Ref != ref {
				continue
			}
			current, found = card, true
			plan.FromColumn = c.ID
		}
		if c.ID == column.ID {
			plan.WIPUsed = len(c.Cards)
		}
	}
	if !found {
		for _, card := range view.Unmapped {
			if card.Ref == ref {
				current, found = card, true
			}
		}
	}
	if !found {
		return BoardMovePlan{}, fmt.Errorf("board %s does not show %s", b.ID, ref)
	}
	if current.Remote {
		return BoardMovePlan{}, fmt.Errorf("%s: %s", ref, current.Reason)
	}

	plan.Choices = column.StatusesFor(move.Ref.Project, cfg)
	switch {
	case move.Status != "":
		if len(plan.Choices) > 0 && !containsStatus(plan.Choices, move.Status) {
			return BoardMovePlan{}, fmt.Errorf("column %q of board %s does not map status %q for project %s",
				column.ID, b.ID, move.Status, move.Ref.Project)
		}
		plan.Status = move.Status
	case plan.FromColumn == column.ID:
		// A re-order inside one column never touches the item file.
		plan.Status = current.Status
	case len(plan.Choices) == 0:
		return BoardMovePlan{}, fmt.Errorf("column %q of board %s maps no status for project %s",
			column.ID, b.ID, move.Ref.Project)
	default:
		plan.Status = plan.Choices[0]
	}
	plan.StatusChanged = plan.Status != current.Status
	if plan.FromColumn != column.ID {
		plan.WIPUsed++
	}
	plan.WIPExceeded = column.WIP > 0 && plan.WIPUsed > column.WIP
	return plan, nil
}

// ApplyMove rewrites the board's `order:` list for a move: the ref leaves every
// column it was listed in and is inserted at its new position. Nothing else in
// the file changes, which is what keeps a move a one-hunk diff (R-ORD-3).
func ApplyMove(b *Board, move BoardMove) {
	order := b.OrderList()
	ref := move.Ref.String()
	order.Remove(ref)
	order.Insert(move.ToColumn, ref, move.Position)
	PruneOrder(b, nil)
}

// PruneOrder drops the columns the board no longer declares and, when keep is
// not nil, the refs that are no longer on the board. keep is the set of refs
// the column currently shows; a nil map prunes nothing but the columns
// (R-ORD-2).
func PruneOrder(b *Board, keep map[string]map[string]bool) {
	order := b.Order
	if order == nil {
		return
	}
	pruned := NewBoardOrder()
	for _, c := range b.Columns {
		if !order.Has(c.ID) {
			continue
		}
		refs := order.Refs(c.ID)
		if keep != nil {
			live := refs[:0]
			for _, ref := range refs {
				if keep[c.ID][ref] {
					live = append(live, ref)
				}
			}
			refs = live
		}
		pruned.Set(c.ID, refs)
	}
	b.Order = pruned
}

// OrderFromView rebuilds the order list from a rendered board, so that a write
// persists the positions the user actually sees. Remote cards keep their place;
// refs the board no longer shows disappear.
func OrderFromView(b *Board, view BoardView) {
	order := NewBoardOrder()
	for _, c := range view.Columns {
		refs := make([]string, 0, len(c.Cards))
		for _, card := range c.Cards {
			refs = append(refs, card.Ref)
		}
		order.Set(c.ID, refs)
	}
	b.Order = order
}
