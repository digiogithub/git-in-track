package core

import (
	"fmt"
	"sort"
	"strings"
	"time"
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

	Title  string   `json:"title,omitempty"`
	Type   ItemType `json:"type,omitempty"`
	Status Status   `json:"status,omitempty"`
	// Category is the coarse bucket of Status in the card's own project
	// workflow. It is what tells a finished card from an open one without
	// knowing that project's statuses, and it is empty when no workflow — live
	// or published by a snapshot — could be read.
	Category  StatusCategory `json:"category,omitempty"`
	Priority  Priority       `json:"priority,omitempty"`
	Assignees []string       `json:"assignees,omitempty"`
	Labels    []string       `json:"labels,omitempty"`
	Estimate  *float64       `json:"estimate,omitempty"`
	Milestone ItemID         `json:"milestone,omitempty"`
	Parent    ItemID         `json:"parent,omitempty"`
	Due       Date           `json:"due,omitempty"`
	Updated   Timestamp      `json:"updated,omitempty"`
	Path      string         `json:"path,omitempty"`
	Rev       Rev            `json:"rev,omitempty"`

	// Source is where the card was read from: "live" for a local clone,
	// "snapshot" for a committed `.pmngr/index/<KEY>.json` (docs/04 section 6).
	// It is empty for a remote card no snapshot could resolve.
	Source string `json:"source,omitempty"`
	// SnapshotAt is when the snapshot the card came from was generated.
	SnapshotAt Timestamp `json:"snapshotAt,omitempty"`
	// Stale reports a snapshot older than the team's `snapshots.max_age_days`
	// (R-SNAP-9). The card still renders; it is badged.
	Stale bool `json:"stale,omitempty"`
	// RemoteURL is the item's file on the git host, built from the project's
	// web_url and default branch (docs/04 section 7.3). Empty when no link can
	// be built.
	RemoteURL string `json:"remoteUrl,omitempty"`

	// InSprint reports a card the scrum board's sprint lists (docs/04 8.2).
	InSprint bool `json:"inSprint,omitempty"`
	// Committed reports a card the sprint committed to when it started, as
	// opposed to one added mid-sprint (R-SPR-1).
	Committed bool `json:"committed,omitempty"`
	// Backlog reports a sprint candidate: an item the board's filters match
	// that the sprint does not list, shown in the board's `backlog_column`
	// (docs/04 section 5.5).
	Backlog bool `json:"backlog,omitempty"`

	// Reason explains, in one sentence, why a card cannot be edited here.
	Reason string `json:"reason,omitempty"`
}

// Done reports whether the card's status is terminal in its own project.
func (c BoardCard) Done() bool {
	return c.Category == CategoryDone || c.Category == CategoryCancelled
}

// Points is the card's estimate, 0 when it carries none.
func (c BoardCard) Points() float64 {
	if c.Estimate == nil {
		return 0
	}
	return *c.Estimate
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
		Source:    CardSourceLive,
	}
}

// snapshotCardOf projects one entry of a committed snapshot onto a card: the
// same fields a live card carries, plus where they came from and how old they
// are. The card is never editable, and it says so (R-REM-1, R-REM-3).
func snapshotCardOf(key ProjectKey, entry ProjectSnapshotItem, info SnapshotInfo, project TeamProject) BoardCard {
	it := entry.Item()
	card := cardOf(key, "", &it)
	card.VaultID = ""
	card.Remote = true
	card.Source = CardSourceSnapshot
	card.SnapshotAt = info.Generated
	card.Stale = info.Stale
	card.RemoteURL = project.FileURL(entry.Path)
	card.Reason = fmt.Sprintf(
		"%s is not cloned on this machine: this card is read from the index snapshot of %s and cannot be edited here",
		key, formatSnapshotAge(info))
	return card
}

// formatSnapshotAge renders the age of a snapshot for a card's explanation.
func formatSnapshotAge(info SnapshotInfo) string {
	if info.Generated.IsZero() || info.AgeSeconds <= 0 {
		return "the team repository"
	}
	return humanAge(info.Age()) + " ago"
}

// BoardColumnView is one rendered column.
type BoardColumnView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	WIP       int    `json:"wip,omitempty"`
	Color     string `json:"color,omitempty"`
	Collapsed bool   `json:"collapsed,omitempty"`
	// Statuses and Categories echo the column's mapping, exactly one of which
	// a valid column declares (R-COL-2). They are what the board editor needs
	// to show — and patch back — what a column claims, without reading the
	// board file a second time.
	Statuses   map[string][]Status `json:"statuses,omitempty"`
	Categories []StatusCategory    `json:"categories,omitempty"`
	Cards      []BoardCard         `json:"cards"`
	// Exceeded reports the live WIP condition of R-COL-5, recomputed on every
	// render rather than stored anywhere.
	Exceeded bool `json:"exceeded"`
}

// BoardView is a board plus the cards it currently shows.
type BoardView struct {
	ID            string           `json:"id"`
	Kind          BoardKind        `json:"kind"`
	Title         string           `json:"title"`
	Description   string           `json:"description,omitempty"`
	Path          string           `json:"path"`
	Rev           Rev              `json:"rev"`
	TeamVaultID   string           `json:"teamVaultId,omitempty"`
	Projects      []ProjectKey     `json:"projects"`
	Filters       BoardFilters     `json:"filters"`
	Swimlanes     BoardSwimlanes   `json:"swimlanes"`
	Card          BoardCardDisplay `json:"card"`
	Sprint        string           `json:"sprint,omitempty"`
	BacklogColumn string           `json:"backlogColumn,omitempty"`
	// SprintInfo is the goal, the dates and the metrics of the sprint a scrum
	// board is scoped to; nil on a kanban board, and on a scrum board whose
	// sprint file could not be read (docs/04 section 5.5).
	SprintInfo *SprintSummary    `json:"sprintInfo,omitempty"`
	Columns    []BoardColumnView `json:"columns"`
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
	// Projects are the team.yaml declarations, used to build the link to a
	// remote item's file on its git host (docs/04 section 7.3).
	Projects []TeamProject
	// Snapshots resolves the cards of the projects nobody cloned. A nil set
	// renders those cards as the bare reference, which is what a team with
	// `snapshots.enabled: false` sees (R-SNAP-10).
	Snapshots *SnapshotSet
	// Sprint is the sprint a scrum board is scoped to, read from the team
	// repository. A nil sprint — or a kanban board — shows everything the
	// filters match, exactly as before (docs/04 section 5.5).
	Sprint *Sprint
	// Now is the instant the sprint's remaining days are counted from. The zero
	// time leaves them uncounted.
	Now time.Time
}

// scoped reports whether this render is limited to a sprint's scope.
func (in BoardInput) scoped(b *Board) bool {
	return b.Kind == BoardScrum && in.Sprint != nil
}

// project returns the team.yaml declaration of a key.
func (in BoardInput) project(key ProjectKey) TeamProject {
	for _, p := range in.Projects {
		if p.Key == key {
			return p
		}
	}
	return TeamProject{}
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
			Collapsed: c.Collapsed, Statuses: c.Statuses, Categories: c.Categories,
			Cards: []BoardCard{},
		})
	}

	// Every candidate the open repositories and the committed snapshots supply,
	// live cards first. `known` marks the references a snapshot resolved, even
	// the ones the filters — or the sprint scope — dropped, so that they never
	// come back as bare placeholders in the order pass below.
	placed := map[string]bool{}
	known := map[string]bool{}
	scoped := in.scoped(b)
	var members map[string]bool
	if scoped {
		members = in.Sprint.Members()
	}
	walkCards(b, in, func(c cardCandidate) {
		if c.Remote {
			known[c.Card.Ref] = true
		}
		if !c.Matched {
			return
		}
		card := c.Card
		index := -1
		for ci, column := range b.Columns {
			if column.Shows(card.Project, c.Config, card.Status) {
				index = ci
				break
			}
		}
		if scoped {
			// A scrum board shows the sprint's scope, plus — in the backlog
			// column — the candidates the sprint does not list yet
			// (docs/04 section 5.5).
			if !members[card.Ref] {
				backlog := -1
				for ci, column := range b.Columns {
					if column.ID == b.BacklogColumn {
						backlog = ci
					}
				}
				if backlog < 0 || !isSprintCandidate(b, c) {
					return
				}
				card.Backlog = true
				buckets[backlog] = append(buckets[backlog], card)
				placed[card.Ref] = true
				return
			}
			card.InSprint = true
			card.Committed = containsString(in.Sprint.Committed, card.Ref)
		}
		if index < 0 {
			card.Reason = fmt.Sprintf("status %s maps to no column of this board", card.Status)
			view.Unmapped = append(view.Unmapped, card)
			return
		}
		buckets[index] = append(buckets[index], card)
		placed[card.Ref] = true
	})

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
			if scoped && !members[ref.String()] {
				// The board is scoped to a sprint and the order list still
				// names a reference the sprint dropped: it is not on the board.
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
			if known[ref.String()] {
				// The snapshot knows the item; the board filters it out or its
				// status maps to no column. R-ORD-2 ignores such a ref on read,
				// exactly as it does for a cloned project.
				continue
			}
			card := BoardCard{
				Ref: ref.String(), Project: ref.Project, Item: ref.Item,
				Declared: declared, Remote: true,
			}
			if declared {
				info := in.Snapshots.Info(ref.Project)
				card.SnapshotAt = info.Generated
				card.Stale = info.Stale
				switch {
				case info.Error != "":
					card.Reason = fmt.Sprintf("the index snapshot of %s cannot be read (%s); clone the project to see this card",
						ref.Project, info.Error)
				case info.Present:
					card.Source = CardSourceSnapshot
					card.Reason = fmt.Sprintf("%s is not in the index snapshot of %s; it may be closed, or the snapshot may be out of date",
						ref.Item, ref.Project)
				case !info.Enabled:
					card.Reason = fmt.Sprintf("project %s is not cloned and this team publishes no index snapshots; clone it to see this card",
						ref.Project)
				default:
					card.Reason = fmt.Sprintf("project %s is not cloned on this machine and has no index snapshot yet; clone it to move this card",
						ref.Project)
				}
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
	if scoped {
		summary := SummarizeSprint(in.Sprint, boardCards(view), in.Now)
		view.SprintInfo = &summary
	}
	return view
}

// boardCards returns every card of a rendered view, columns and unmapped items
// alike, which is what the sprint metrics are counted over.
func boardCards(view BoardView) []BoardCard {
	var out []BoardCard
	for _, column := range view.Columns {
		out = append(out, column.Cards...)
	}
	return append(out, view.Unmapped...)
}

// cardCandidate is one card a board could show, before the board decided where
// it goes: the card itself, the workflow of its project, whether the board's
// filters keep it and whether it came from a committed snapshot.
type cardCandidate struct {
	Card    BoardCard
	Config  *ProjectConfig
	Matched bool
	Remote  bool
}

// walkCards yields every card the open repositories and the committed snapshots
// can supply for a board's project scope, live cards first. It is the one place
// that turns items into cards, so that a board render and a sprint render never
// disagree about what a card carries.
func walkCards(b *Board, in BoardInput, fn func(cardCandidate)) {
	scope := b.Scope(in.Declared)
	sources := map[ProjectKey]BoardSource{}
	for _, s := range in.Sources {
		sources[s.Project] = s
	}
	for _, key := range scope {
		source, cloned := sources[key]
		if !cloned {
			continue
		}
		for i := range source.Items {
			it := &source.Items[i]
			card := cardOf(key, source.VaultID, it)
			if source.Config != nil {
				card.Category = source.Config.CategoryOf(it.Status)
			}
			fn(cardCandidate{Card: card, Config: source.Config, Matched: b.matches(it, source.Config)})
		}
	}
	// Projects nobody cloned: their cards come from the committed snapshot,
	// read-only and stale-dated (docs/04 sections 6 and 7).
	for _, key := range scope {
		if _, cloned := sources[key]; cloned {
			continue
		}
		snap, ok := in.Snapshots.Snapshot(key)
		if !ok {
			continue
		}
		cfg := in.Snapshots.Config(key)
		info := in.Snapshots.Info(key)
		project := in.project(key)
		for _, entry := range snap.Items {
			card := snapshotCardOf(key, entry, info, project)
			card.Declared = containsKey(in.Declared, key)
			card.Category = entry.Category
			if card.Category == "" && cfg != nil {
				card.Category = cfg.CategoryOf(entry.Status)
			}
			it := entry.Item()
			fn(cardCandidate{Card: card, Config: cfg, Matched: b.matches(&it, cfg), Remote: true})
		}
	}
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

// matchBoardAssignees applies the `assignees` filter, honoring the
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
	// SprintAdd reports that the move pulls the card out of the backlog column
	// of a scrum board and into the sprint, which appends its reference to the
	// sprint file in the team repository (docs/04 section 5.5).
	SprintAdd bool
	// Sprint is the sprint the board is scoped to, when there is one.
	Sprint string
}

// PlanMove computes what moving a card implies, without writing anything. view
// is the board as it is rendered right now, which is what makes the WIP count
// and the current column knowable.
//
// cfg is the workflow of the card's project, nil for a project nobody cloned.
func PlanMove(b *Board, view BoardView, move BoardMove, cfg *ProjectConfig) (BoardMovePlan, error) {
	return PlanMoveInSprint(b, view, move, cfg, nil)
}

// PlanMoveInSprint is PlanMove on a scrum board: dragging a card out of the
// backlog column commits it to the sprint, which appends its reference to the
// sprint file (docs/04 section 5.5). Sprint membership is team-repository
// state, so it is allowed even for a card whose project nobody cloned — what a
// remote card still cannot do is change its own status (R-REM-1).
func PlanMoveInSprint(
	b *Board, view BoardView, move BoardMove, cfg *ProjectConfig, sprint *Sprint,
) (BoardMovePlan, error) {
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
	if sprint != nil && b.Kind == BoardScrum {
		plan.Sprint = sprint.ID
		plan.SprintAdd = !sprint.Has(ref) && column.ID != b.BacklogColumn
	}
	if current.Remote && plan.SprintAdd && plan.FromColumn == b.BacklogColumn {
		// A remote candidate joins the sprint — team-repo state — but its
		// status stays where it is, because the item lives elsewhere.
		plan.Choices = column.StatusesFor(move.Ref.Project, cfg)
		plan.Status = current.Status
		plan.WIPUsed++
		plan.WIPExceeded = column.WIP > 0 && plan.WIPUsed > column.WIP
		return plan, nil
	}
	if current.Remote && plan.FromColumn != column.ID {
		// Everything whose state lives in the project repository is read-only
		// for a remote card; the order list lives in the team repository, so a
		// re-order inside one column stays allowed (R-REM-1).
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
