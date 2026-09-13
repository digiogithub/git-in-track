package core

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// The guarantee of GIT-US-0071: untriaged work is invisible to every planning
// surface, and stays perfectly visible to everything that is not planning.
//
// The exclusion itself lives in one place — walkCards in boardview.go, plus the
// default of Filter.Inbox in query.go — on the argument that every board and
// sprint surface consumes the cards walkCards produces. That argument is only
// as good as the list of surfaces it was made against, so this file checks the
// list rather than the argument: each surface is rendered twice, over a project
// that holds triage items and over the same project without them, and the two
// answers must be identical. A future surface is one row in the table.

// triageWorld is the shared fixture every case below renders. It is built twice
// per run — with and without the triage items — and nothing else differs
// between the two.
type triageWorld struct {
	Index   *Index
	Board   *Board
	Input   BoardInput
	Sprint  *Sprint
	Metrics MetricsInput
}

// triageNow is the instant every surface is evaluated at. The core reads no
// clock (it compiles to WebAssembly), so the instant is an input and the
// answers are reproducible.
var triageNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// triageConfig is demoConfig plus the reserved triage status. A project that
// does not declare one has no inbox at all, which is the case every test
// written before the inbox already covers.
func triageConfig() *ProjectConfig {
	cfg := demoConfig()
	cfg.Workflow.Statuses = append(cfg.Workflow.Statuses,
		StatusDef{ID: "triage", Name: "Triage", Category: CategoryTriage},
		// The fixture items include one `archived` task, which demoConfig
		// leaves undeclared on purpose for the board tests. Here the items are
		// written through a real store, which validates the status, so the
		// workflow has to know it.
		StatusDef{ID: "archived", Name: "Archived", Category: CategoryDone, Terminal: true})
	cfg.Workflow.Transitions["triage"] = []Status{"backlog", "cancelled"}
	cfg.Workflow.Transitions["archived"] = []Status{"backlog"}
	return cfg
}

// triageItems returns n inbox items, cycling through the inbox states so that
// no case is accidentally proven for `pending` only. Every one of them carries
// an estimate, a priority and a recent `updated` stamp: an item that could not
// distort a number proves nothing.
func triageItems(n int) []Item {
	states := []ItemInbox{
		{Status: InboxPending, Source: "web"},
		{Status: InboxRejected, Source: "mcp"},
		{Status: InboxSnoozed, SnoozedUntil: inboxDate(2026, time.October, 1), Source: "web"},
		{Status: InboxDuplicate, DuplicateOf: "DEMO-US-0001", Source: "web"},
		{},
	}
	ts, err := ParseTimestamp("2026-09-03T09:00:00Z")
	if err != nil {
		panic(err)
	}
	out := make([]Item, 0, n)
	for i := range n {
		est := 13.0
		block := states[i%len(states)]
		item := Item{
			ID: ItemID(fmt.Sprintf("DEMO-T-%04d", 9000+i)), Type: TypeTask,
			Title: fmt.Sprintf("Raw submission %d", i), Status: "triage",
			Priority: PriorityCritical, Estimate: &est, Updated: ts,
			Rev:   Rev(fmt.Sprintf("sha256:%016x", i)),
			Inbox: &block,
		}
		out = append(out, item)
	}
	return out
}

// newTriageWorld builds the fixture. With triage, `count` inbox items are added
// to a project that otherwise holds exactly the ordinary fixture items.
func newTriageWorld(t *testing.T, count int) triageWorld {
	t.Helper()

	cfg := triageConfig()
	items := demoItems()
	extra := triageItems(count)
	items = append(items, extra...)

	world := triageWorld{
		Board: readFixtureBoard(t),
		Input: BoardInput{
			Declared:    []ProjectKey{"DEMO", "WEB"},
			TeamVaultID: "team",
			Sources: []BoardSource{{
				Project: "DEMO", VaultID: "demo", Config: cfg, Items: items,
			}},
		},
	}

	// A hand-edited sprint file: it names the ordinary story and every triage
	// item by reference. Nothing stops a person from writing this, which is
	// precisely why the model has to refuse it rather than the editor.
	sprint := &Sprint{
		ID: "TEAM-S-0001", Type: "sprint", Board: "delivery", State: SprintActive,
		Start: NewDate(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
		End:   NewDate(time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)),
		Items: []string{"DEMO/DEMO-US-0001"},
	}
	for _, it := range extra {
		sprint.Items = append(sprint.Items, "DEMO/"+string(it.ID))
	}
	sprint.Committed = append([]string(nil), sprint.Items...)
	world.Sprint = sprint

	history := []ItemHistory{{
		Ref: "DEMO/DEMO-US-0001", Complete: true,
		Observations: []ItemObservation{{
			At: triageStamp("2026-09-01T00:00:00Z"), Status: "in_progress",
			Category: CategoryInProgress, Estimate: ptrFloat(8),
		}},
	}}
	for _, it := range extra {
		history = append(history, ItemHistory{
			Ref: "DEMO/" + string(it.ID), Complete: true,
			Observations: []ItemObservation{{
				At: triageStamp("2026-09-01T00:00:00Z"), Status: "triage",
				Category: CategoryTriage, Estimate: ptrFloat(13),
			}},
		})
	}
	world.Metrics = MetricsInput{History: history, Now: triageNow}

	world.Index = triageIndex(t, cfg, items)
	return world
}

// triageIndex builds a real on-disk-shaped index holding exactly items, so that
// the query surfaces are exercised through the same path the vault uses.
func triageIndex(t *testing.T, cfg *ProjectConfig, items []Item) *Index {
	t.Helper()

	ctx := context.Background()
	fsys := NewMemFS()
	ref, err := CreateProject(fsys, "docs", NewProject{Key: "DEMO"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	ref.Config = cfg
	store := NewStore(fsys, ref.BacklogPath, cfg)
	for _, it := range items {
		draft := ItemDraft{
			ID: it.ID, Type: it.Type, Title: it.Title, Status: it.Status,
			Priority: it.Priority, Labels: it.Labels, Assignees: it.Assignees,
			Estimate: it.Estimate, Inbox: it.Inbox,
		}
		if _, err := store.Create(ctx, draft); err != nil {
			t.Fatalf("create %s: %v", it.ID, err)
		}
	}
	ix := NewIndex(fsys, []ProjectRef{ref})
	if _, err := ix.Build(ctx, true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ix
}

// triageStamp parses a timestamp a fixture pins.
func triageStamp(s string) Timestamp {
	ts, err := ParseTimestamp(s)
	if err != nil {
		panic(err)
	}
	return ts
}

// TestTriageItemsChangeNoPlanningSurface is the guarantee itself. Every surface
// is asked the same question twice and must give the same answer, byte for
// byte, whether or not the project's inbox is full.
func TestTriageItemsChangeNoPlanningSurface(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	without := newTriageWorld(t, 0)
	with := newTriageWorld(t, 7)

	surfaces := []struct {
		name string
		// answer renders one surface down to a comparable value. Anything a
		// triage item could reach must be inside it.
		answer func(t *testing.T, w triageWorld) any
	}{
		{
			name: "Index.Items with a default filter",
			answer: func(t *testing.T, w triageWorld) any {
				t.Helper()
				page, err := w.Index.Items(ctx, Filter{Sort: "id", Limit: MaxLimit})
				if err != nil {
					t.Fatalf("Items: %v", err)
				}
				ids := make([]string, 0, len(page.Items))
				for _, it := range page.Items {
					ids = append(ids, string(it.ID))
				}
				return []any{ids, page.Total}
			},
		},
		{
			name: "BuildBoardView",
			answer: func(t *testing.T, w triageWorld) any {
				t.Helper()
				view := BuildBoardView(w.Board, w.Input)
				return []any{refsOfEveryColumn(view), refsOfCards(view.Unmapped)}
			},
		},
		{
			// The sprint file names every triage item, so its cards *do* differ
			// between the two worlds — by one inert placeholder per reference
			// it cannot resolve. What must not differ is the work: a card that
			// carries a status is a card a planner can count.
			name: "BuildSprintView resolved cards",
			answer: func(t *testing.T, w triageWorld) any {
				t.Helper()
				view := BuildSprintView(w.Sprint, w.Board, w.Input)
				return refsOfCards(resolvedCards(view.Cards))
			},
		},
		{
			name: "the sprint candidate drawer",
			answer: func(t *testing.T, w triageWorld) any {
				t.Helper()
				view := BuildSprintView(w.Sprint, w.Board, w.Input)
				return refsOfCards(view.Backlog)
			},
		},
		{
			name: "SummarizeSprint",
			answer: func(t *testing.T, w triageWorld) any {
				t.Helper()
				view := BuildSprintView(w.Sprint, w.Board, w.Input)
				m := SummarizeSprint(w.Sprint, view.Cards, triageNow).Metrics
				// Items counts the references the sprint file names, which the
				// hand-edited file inflates on purpose; everything that means
				// *work* must be blind to them.
				return []any{m.Points, m.Done, m.DonePoints, m.Resolved}
			},
		},
		{
			name: "BuildSprintMetrics",
			answer: func(t *testing.T, w triageWorld) any {
				t.Helper()
				view := BuildSprintView(w.Sprint, w.Board, w.Input)
				in := w.Metrics
				in.Cards = view.Cards
				metrics := BuildSprintMetrics(w.Sprint, in)
				scope := make([]float64, 0, len(metrics.Burndown.Points))
				completed := make([]int, 0, len(metrics.Burndown.Points))
				for _, p := range metrics.Burndown.Points {
					scope = append(scope, p.Scope)
					completed = append(completed, p.Completed)
				}
				bands := make([]map[FlowBand]int, 0, len(metrics.Flow.Days))
				for _, d := range metrics.Flow.Days {
					bands = append(bands, d.Counts)
				}
				return []any{scope, completed, bands}
			},
		},
	}

	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			got := s.answer(t, with)
			want := s.answer(t, without)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("a triage item reached %s\nwith triage    = %#v\nwithout triage = %#v",
					s.name, got, want)
			}
		})
	}
}

// resolvedCards keeps the cards a planner could act on: a card the board could
// not resolve to an item carries no status, only a Reason.
func resolvedCards(cards []BoardCard) []BoardCard {
	out := make([]BoardCard, 0, len(cards))
	for _, c := range cards {
		if c.Status != "" {
			out = append(out, c)
		}
	}
	return out
}

// refsOfCards lists the refs of a card slice.
func refsOfCards(cards []BoardCard) []string {
	out := make([]string, 0, len(cards))
	for _, c := range cards {
		out = append(out, c.Ref)
	}
	return out
}

// TestTriageItemIsExcludedEvenWhenAColumnAsksForIt is the case the single-point
// implementation makes easy to get wrong, and which a reader would not guess:
// a board column may name the `triage` status outright, or declare the `triage`
// *category*, and the column still renders empty. The exclusion happens in
// walkCards, before any column is consulted, so a board cannot opt back in — by
// configuration or by accident.
func TestTriageItemIsExcludedEvenWhenAColumnAsksForIt(t *testing.T) {
	t.Parallel()

	world := newTriageWorld(t, 3)
	board := &Board{
		ID: "inbox-board", Type: "board", Kind: "kanban", Title: "Inbox board",
		Projects: []ProjectKey{"DEMO"},
		Columns: []BoardColumn{
			{ID: "by_status", Name: "By status", Statuses: map[string][]Status{
				BoardWildcard: {"triage"},
			}},
			{ID: "by_category", Name: "By category", Categories: []StatusCategory{CategoryTriage}},
			{ID: "todo", Name: "To Do", Categories: []StatusCategory{CategoryTodo}},
		},
		Path: ".pmngr/boards/inbox-board.md",
	}

	view := BuildBoardView(board, world.Input)
	for _, column := range view.Columns {
		if column.ID == "todo" {
			continue
		}
		if len(column.Cards) != 0 {
			t.Errorf("column %q rendered %v: a board must not be able to opt back into the inbox",
				column.ID, refsOfCards(column.Cards))
		}
	}
	// Nor may the item reappear as unmapped work, which would put it in the
	// board's diagnostics and back in front of a planner.
	for _, card := range view.Unmapped {
		if card.Category == CategoryTriage {
			t.Errorf("a triage item was reported as unmapped: %+v", card)
		}
	}
}

// TestBurndownOverAFullInboxIsUnmoved is the scale case. A hundred triage
// items, each carrying thirteen points and each named by the sprint file, must
// leave the burndown of a two-item sprint exactly where it was: scope must not
// rise, and none of them may be counted as completed or as an unknown band that
// would hatch the whole diagram.
func TestBurndownOverAFullInboxIsUnmoved(t *testing.T) {
	t.Parallel()

	quiet := newTriageWorld(t, 0)
	busy := newTriageWorld(t, 100)

	render := func(w triageWorld) SprintMetricsView {
		view := BuildSprintView(w.Sprint, w.Board, w.Input)
		in := w.Metrics
		in.Cards = view.Cards
		return BuildSprintMetrics(w.Sprint, in)
	}
	base := render(quiet)
	loaded := render(busy)

	if len(base.Burndown.Points) != len(loaded.Burndown.Points) {
		t.Fatalf("the burndown changed length: %d vs %d",
			len(base.Burndown.Points), len(loaded.Burndown.Points))
	}
	for i, p := range loaded.Burndown.Points {
		want := base.Burndown.Points[i]
		if p.Scope != want.Scope {
			t.Errorf("day %d: scope = %v, want %v — a busy inbox inflated the sprint",
				p.Day, p.Scope, want.Scope)
		}
		if p.Completed != want.Completed {
			t.Errorf("day %d: completed = %v, want %v", p.Day, p.Completed, want.Completed)
		}
		if p.Ideal != want.Ideal {
			t.Errorf("day %d: ideal = %v, want %v", p.Day, p.Ideal, want.Ideal)
		}
	}
	for i, d := range loaded.Flow.Days {
		if !reflect.DeepEqual(d.Counts, base.Flow.Days[i].Counts) {
			t.Errorf("day %d: flow bands = %v, want %v", d.Day, d.Counts, base.Flow.Days[i].Counts)
		}
	}
}

// TestTriageItemKeepsItsIdentity is the other half of the guarantee, and the
// half an over-eager exclusion would break: an inbox item is a real item from
// the moment it is submitted (ADR-033). It is readable by id, it carries its
// comments, and it is listed by an inbox query.
func TestTriageItemKeepsItsIdentity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ix, store, _ := inboxVault(t)
	const id ItemID = "ACME-T-0001"

	it, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get %s: %v", id, err)
	}
	if it.Status != "triage" {
		t.Fatalf("status = %q", it.Status)
	}
	if _, err := ix.Item(id); err != nil {
		t.Errorf("a triage item must stay readable by id through the index: %v", err)
	}

	comment, err := store.AddComment(ctx, id, CommentDraft{
		Author: "jose", Body: "Which release is this about?",
	})
	if err != nil {
		t.Fatalf("AddComment on a triage item: %v", err)
	}
	thread, err := store.ListComments(ctx, id)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(thread) != 1 || thread[0].Path != comment.Path {
		t.Errorf("thread = %+v, want the comment just written", thread)
	}

	page, err := ix.Items(ctx, Filter{Inbox: InboxOnly, Sort: "id"})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	found := false
	for _, listed := range page.Items {
		if listed.ID == id {
			found = true
		}
	}
	if !found {
		t.Errorf("the inbox listing does not hold %s: %+v", id, page.Items)
	}
}

// TestAcceptingATriageItemPutsItOnTheBoard closes the loop: the exclusion is a
// filter over the index, not a second corpus, so the instant an item's status
// leaves the triage category it is ordinary work — in the default query and in
// the board column its new status maps to, in the same index generation, with
// no rebuild in between.
func TestAcceptingATriageItemPutsItOnTheBoard(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := triageConfig()
	world := newTriageWorld(t, 1)
	const accepted ItemID = "DEMO-T-9000"

	before, err := world.Index.Items(ctx, Filter{Sort: "id", Limit: MaxLimit})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	for _, it := range before.Items {
		if it.ID == accepted {
			t.Fatal("the item is in the backlog before it was accepted")
		}
	}

	items := make([]Item, 0, len(world.Input.Sources[0].Items))
	for _, it := range world.Input.Sources[0].Items {
		if it.ID == accepted {
			// Accepting is one status change and nothing else: the `inbox:`
			// block stays behind as the record of how the item arrived.
			it.Status = "todo"
		}
		items = append(items, it)
	}
	ix := triageIndex(t, cfg, items)

	after, err := ix.Items(ctx, Filter{Sort: "id", Limit: MaxLimit})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	seen := false
	for _, it := range after.Items {
		if it.ID == accepted {
			seen = true
			if it.Inbox == nil {
				t.Error("the inbox block was dropped: it is the provenance of the item")
			}
		}
	}
	if !seen {
		t.Fatalf("the accepted item is not in the backlog: %+v", after.Items)
	}

	in := world.Input
	in.Sources[0].Items = items
	view := BuildBoardView(world.Board, in)
	if got := refsOf(columnOf(view, "todo")); !containsString(got, "DEMO/"+string(accepted)) {
		t.Errorf("the To Do column holds %v, want the accepted item in it", got)
	}
}

// TestSearchStillFindsATriageItem records, deliberately, the one surface that
// does *not* hide the inbox, because a reader coming from the story would
// expect it to.
//
// Index.Search is discovery, not planning: it is what the quick switcher and
// the MCP `search_items` tool answer from, and an agent about to file a
// duplicate has to be able to find the submission that is already sitting in
// the inbox. ADR-033 scopes the unconditional exclusion to board views, sprint
// views, sprint candidates and sprint metrics, and scopes queries to a *default*
// that a caller can lift with Filter.Inbox; Search takes no filter, so hiding
// triage there would make an inbox item unfindable by text from every surface
// at once — the opposite of "an item is real from the moment of submission".
//
// If this is ever revisited, the change belongs in ADR-033 first: a search hit
// carries no estimate, no status category and no column, so nothing here can
// reach a number a team plans with.
func TestSearchStillFindsATriageItem(t *testing.T) {
	t.Parallel()

	ix, _, _ := inboxVault(t)
	hits := ix.Search("submission", MaxLimit)
	found := false
	for _, hit := range hits {
		if hit.ID == "ACME-T-0001" {
			found = true
		}
	}
	if !found {
		t.Errorf("search no longer finds an inbox item: %+v.\n"+
			"If that exclusion was intended, ADR-033 has to say so and this test has to change with it",
			hits)
	}
}

// TestSprintFileNamingAnInboxItemResolvesToNothing is the hand-edited-file case
// ADR-033 promises: "a hand-edited sprint file naming a triage item cannot
// smuggle it into a sprint". Nothing stops a person from typing the reference —
// the sprint file is Markdown in git — so the model has to refuse it at read
// time. The reference is reported as unresolved, which is honest (the file does
// name it) and inert (it carries no status, no estimate and no points).
func TestSprintFileNamingAnInboxItemResolvesToNothing(t *testing.T) {
	t.Parallel()

	world := newTriageWorld(t, 3)
	view := BuildSprintView(world.Sprint, world.Board, world.Input)

	named := map[string]bool{}
	for _, ref := range world.Sprint.Items {
		named[ref] = true
	}
	for _, card := range view.Cards {
		if card.Ref == "DEMO/DEMO-US-0001" {
			continue
		}
		if !named[card.Ref] {
			t.Errorf("the sprint rendered a card it never named: %+v", card)
			continue
		}
		if card.Status != "" || card.Category != "" || card.Estimate != nil {
			t.Errorf("a triage reference rendered as work: %+v", card)
		}
		if card.Reason == "" {
			t.Errorf("an unresolved reference must say why it is unresolved: %+v", card)
		}
	}

	summary := SummarizeSprint(world.Sprint, view.Cards, triageNow)
	if summary.Metrics.Resolved != 1 {
		t.Errorf("resolved = %d, want only the one real story", summary.Metrics.Resolved)
	}
	if summary.Metrics.Unresolved != 3 {
		t.Errorf("unresolved = %d, want the three triage references", summary.Metrics.Unresolved)
	}
	if summary.Metrics.Points != 8 {
		t.Errorf("points = %v, want 8: 3 x 13 points of inbox work must not count",
			summary.Metrics.Points)
	}
	if summary.Metrics.Done != 0 || summary.Metrics.DonePoints != 0 {
		t.Errorf("a triage reference counted as done: %+v", summary.Metrics)
	}
}
