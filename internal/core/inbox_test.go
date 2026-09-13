package core

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// inboxDate is the shorthand the snooze tests read better with.
func inboxDate(y int, m time.Month, d int) Date {
	return NewDate(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
}

func inboxTime(y int, m time.Month, d, h int) Timestamp {
	return NewTimestamp(time.Date(y, m, d, h, 0, 0, 0, time.UTC))
}

func TestStatusCategoryTriage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		category StatusCategory
		valid    bool
	}{
		{CategoryTodo, true},
		{CategoryInProgress, true},
		{CategoryDone, true},
		{CategoryCancelled, true},
		{CategoryTriage, true},
		{StatusCategory("inbox"), false},
		{StatusCategory(""), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			t.Parallel()
			if got := tt.category.Valid(); got != tt.valid {
				t.Errorf("%q.Valid() = %t, want %t", tt.category, got, tt.valid)
			}
		})
	}
	if got := StatusCategories(); len(got) != 5 || got[4] != CategoryTriage {
		t.Errorf("StatusCategories() = %v", got)
	}
}

func TestWorkflowTriageStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		wf   Workflow
		want Status
	}{
		{
			name: "the default workflow declares one",
			wf:   DefaultWorkflow(),
			want: "triage",
		},
		{
			name: "a project without a triage status simply has no inbox",
			wf: Workflow{Statuses: []StatusDef{
				{ID: "todo", Category: CategoryTodo},
				{ID: "done", Category: CategoryDone},
			}},
			want: "",
		},
		{
			name: "the first declared triage status wins",
			wf: Workflow{Statuses: []StatusDef{
				{ID: "todo", Category: CategoryTodo},
				{ID: "inbox", Category: CategoryTriage},
				{ID: "spam", Category: CategoryTriage},
			}},
			want: "inbox",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.wf.TriageStatus(); got != tt.want {
				t.Errorf("TriageStatus() = %q, want %q", got, tt.want)
			}
		})
	}

	multi := Workflow{Statuses: []StatusDef{
		{ID: "inbox", Category: CategoryTriage},
		{ID: "todo", Category: CategoryTodo},
		{ID: "spam", Category: CategoryTriage},
	}}
	if got := multi.TriageStatuses(); !reflect.DeepEqual(got, []Status{"inbox", "spam"}) {
		t.Errorf("TriageStatuses() = %v", got)
	}
}

func TestProjectValidateAcceptsTriageCategory(t *testing.T) {
	t.Parallel()

	cfg, err := LoadProjectConfig([]byte(`schema: 1
key: ACME
name: ACME
workflow:
  initial: backlog
  statuses:
    - { id: triage,  name: Triage,  category: triage }
    - { id: backlog, name: Backlog, category: todo }
    - { id: done,    name: Done,    category: done }
`))
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	for _, d := range ValidateProject(cfg) {
		if d.Severity == SeverityError {
			t.Errorf("triage must be a known category, got %v", d)
		}
	}
	if cfg.CategoryOf("triage") != CategoryTriage {
		t.Errorf("CategoryOf(triage) = %q", cfg.CategoryOf("triage"))
	}
	if !cfg.IsTriageStatus("triage") || cfg.IsTriageStatus("backlog") {
		t.Error("IsTriageStatus must follow the declared category")
	}

	// A project declaring no triage status still validates clean and has no inbox.
	plain, err := LoadProjectConfig([]byte(`schema: 1
key: ACME
name: ACME
workflow:
  initial: backlog
  statuses:
    - { id: backlog, name: Backlog, category: todo }
    - { id: done,    name: Done,    category: done }
`))
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	for _, d := range ValidateProject(plain) {
		if d.Severity == SeverityError {
			t.Errorf("a project without an inbox must validate clean, got %v", d)
		}
	}
	if plain.Workflow.TriageStatus() != "" {
		t.Error("a project without a triage status must report no triage status")
	}
}

func TestScaffoldedProjectDeclaresTriage(t *testing.T) {
	t.Parallel()

	fsys := NewMemFS()
	ref, err := CreateProject(fsys, "docs", NewProject{Key: "ACME"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if got := ref.Config.Workflow.TriageStatus(); got != "triage" {
		t.Fatalf("scaffolded triage status = %q", got)
	}
	data, err := fsys.ReadFile(ref.ConfigPath)
	if err != nil {
		t.Fatalf("read project.yaml: %v", err)
	}
	if !strings.Contains(string(data), "category: triage") {
		t.Errorf("project.yaml does not declare a triage status:\n%s", data)
	}
	// Triage is not the initial status and is not a transition target, so an
	// ordinary item never lands there by accident.
	if ref.Config.InitialStatus() == "triage" {
		t.Error("triage must not be the initial status")
	}
	for from, targets := range ref.Config.Workflow.Transitions {
		for _, to := range targets {
			if to == "triage" {
				t.Errorf("triage is reachable from %q by an ordinary transition", from)
			}
		}
	}
}

func TestParseAndSerializeInboxBlock(t *testing.T) {
	t.Parallel()

	src := []byte(`---
id: ACME-T-0301
type: task
title: Broken export
status: triage
created: 2026-09-10T08:00:00Z
updated: 2026-09-10T08:00:00Z
inbox:
  status: snoozed
  snoozed_until: 2026-10-01
  source: web
  received: 2026-09-10T07:59:12Z
  reporter_email: reporter@example.com
---

Body.
`)
	it, err := ParseItem("tasks/ACME-T-0301-broken-export.md", src)
	if err != nil {
		t.Fatalf("ParseItem(): %v", err)
	}
	if it.Inbox == nil {
		t.Fatal("inbox block was not parsed")
	}
	if it.Inbox.Status != InboxSnoozed || it.Inbox.Source != "web" {
		t.Errorf("inbox = %#v", it.Inbox)
	}
	if it.Inbox.SnoozedUntil.String() != "2026-10-01" {
		t.Errorf("snoozed_until = %q", it.Inbox.SnoozedUntil.String())
	}
	if it.Inbox.Received.String() != "2026-09-10T07:59:12Z" {
		t.Errorf("received = %q", it.Inbox.Received.String())
	}
	if got := it.Inbox.Extra["reporter_email"]; got != "reporter@example.com" {
		t.Errorf("an unknown key inside the block was lost: %#v", it.Inbox.Extra)
	}
	if _, ok := it.Extra["inbox"]; ok {
		t.Error("inbox must be a first-class field, not an unknown top-level key")
	}

	out, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}
	if string(out) != string(src) {
		t.Errorf("round trip is not byte-identical\n--- got ---\n%s\n--- want ---\n%s", out, src)
	}
}

func TestSerializeItemOmitsAnEmptyInboxBlock(t *testing.T) {
	t.Parallel()

	it := &Item{ID: "ACME-T-0301", Type: TypeTask, Title: "X", Status: "triage", Inbox: &ItemInbox{}}
	out, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}
	if strings.Contains(string(out), "inbox") {
		t.Errorf("an empty block must be omitted, not written as `inbox:`:\n%s", out)
	}
}

func TestValidateInbox(t *testing.T) {
	t.Parallel()

	cfg, err := LoadProjectConfig([]byte(`schema: 1
key: ACME
name: ACME
workflow:
  initial: backlog
  statuses:
    - { id: triage,  name: Triage,  category: triage }
    - { id: backlog, name: Backlog, category: todo }
    - { id: done,    name: Done,    category: done }
`))
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	tests := []struct {
		name     string
		status   Status
		inbox    *ItemInbox
		code     Code
		severity Severity
	}{
		{
			name:   "a pending item in triage is clean",
			status: "triage",
			inbox:  &ItemInbox{Status: InboxPending, Source: "web"},
		},
		{
			name:   "snoozed with a date is clean",
			status: "triage",
			inbox:  &ItemInbox{Status: InboxSnoozed, SnoozedUntil: inboxDate(2026, time.October, 1)},
		},
		{
			name:     "snoozed without a date is an error",
			status:   "triage",
			inbox:    &ItemInbox{Status: InboxSnoozed},
			code:     CodeInboxSnooze,
			severity: SeverityError,
		},
		{
			name:     "a snooze date on a pending item is an error",
			status:   "triage",
			inbox:    &ItemInbox{Status: InboxPending, SnoozedUntil: inboxDate(2026, time.October, 1)},
			code:     CodeInboxSnooze,
			severity: SeverityError,
		},
		{
			name:     "an unknown triage state is an error",
			status:   "triage",
			inbox:    &ItemInbox{Status: InboxStatus("parked")},
			code:     CodeInboxStatus,
			severity: SeverityError,
		},
		{
			name:     "duplicate_of that is not an item id is an error",
			status:   "triage",
			inbox:    &ItemInbox{Status: InboxDuplicate, DuplicateOf: "not-an-id"},
			code:     CodeInboxDuplicate,
			severity: SeverityError,
		},
		{
			name:     "status duplicate without a target is an error",
			status:   "triage",
			inbox:    &ItemInbox{Status: InboxDuplicate},
			code:     CodeInboxDuplicate,
			severity: SeverityError,
		},
		{
			name:     "an item cannot be a duplicate of itself",
			status:   "triage",
			inbox:    &ItemInbox{Status: InboxDuplicate, DuplicateOf: "ACME-T-0301"},
			code:     CodeInboxDuplicate,
			severity: SeverityError,
		},
		{
			name:     "an inbox block outside the triage category is a warning",
			status:   "backlog",
			inbox:    &ItemInbox{Status: InboxAccepted, Source: "web"},
			code:     CodeWarnInboxCategory,
			severity: SeverityWarning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := &Item{
				ID: "ACME-T-0301", Type: TypeTask, Title: "X", Status: tt.status,
				Path: "tasks/ACME-T-0301-x.md", Inbox: tt.inbox,
			}
			d := &diagSet{path: item.Path}
			validateInbox(d, item, cfg)
			if tt.code == "" {
				if len(d.out) != 0 {
					t.Fatalf("want no diagnostic, got %v", d.out)
				}
				return
			}
			var found *Diagnostic
			for i := range d.out {
				if d.out[i].Code == tt.code {
					found = &d.out[i]
				}
			}
			if found == nil {
				t.Fatalf("want %s, got %v", tt.code, d.out)
			}
			if found.Severity != tt.severity {
				t.Errorf("severity = %q, want %q", found.Severity, tt.severity)
			}
		})
	}
}

func TestInboxEffectiveStatusSnoozeExpiry(t *testing.T) {
	t.Parallel()

	snoozed := &ItemInbox{Status: InboxSnoozed, SnoozedUntil: inboxDate(2026, time.October, 1)}

	tests := []struct {
		name  string
		inbox *ItemInbox
		asOf  Timestamp
		want  InboxStatus
	}{
		{"a nil block is pending", nil, Timestamp{}, InboxPending},
		{"an empty status is pending", &ItemInbox{}, Timestamp{}, InboxPending},
		{"accepted stays accepted", &ItemInbox{Status: InboxAccepted}, inboxTime(2026, time.November, 1, 0), InboxAccepted},
		{"before the date it is still snoozed", snoozed, inboxTime(2026, time.September, 30, 23), InboxSnoozed},
		{"exactly at the date it reads as pending", snoozed, inboxTime(2026, time.October, 1, 0), InboxPending},
		{"after the date it reads as pending", snoozed, inboxTime(2026, time.October, 1, 1), InboxPending},
		{"without a clock nothing expires", snoozed, Timestamp{}, InboxSnoozed},
		{"snoozed without a date never expires", &ItemInbox{Status: InboxSnoozed}, inboxTime(2030, time.January, 1, 0), InboxSnoozed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.inbox.EffectiveStatus(tt.asOf); got != tt.want {
				t.Errorf("EffectiveStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseInboxScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want InboxScope
		ok   bool
	}{
		{"", InboxExclude, true},
		{"exclude", InboxExclude, true},
		{"only", InboxOnly, true},
		{"INCLUDE", InboxInclude, true},
		{"maybe", InboxExclude, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseInboxScope(tt.in)
			if got != tt.want || ok != tt.ok {
				t.Errorf("ParseInboxScope(%q) = %q, %t; want %q, %t", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// inboxVault builds a project whose backlog holds one ordinary story and four
// triage tasks, one per interesting triage state.
func inboxVault(t *testing.T) (*Index, *FileStore, ProjectRef) {
	t.Helper()

	ctx := context.Background()
	fsys := NewMemFS()
	ref, err := CreateProject(fsys, "docs", NewProject{Key: "ACME"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	store := NewStore(fsys, ref.BacklogPath, ref.Config)

	if _, err := store.Create(ctx, ItemDraft{
		ID: "ACME-US-0001", Type: TypeStory, Title: "Real work", Status: "backlog", Estimate: ptrFloat(5),
	}); err != nil {
		t.Fatalf("create story: %v", err)
	}
	drafts := []ItemDraft{
		{ID: "ACME-T-0001", Title: "Pending submission", Inbox: &ItemInbox{Status: InboxPending, Source: "web"}},
		{ID: "ACME-T-0002", Title: "Rejected submission", Inbox: &ItemInbox{Status: InboxRejected, Source: "mcp"}},
		{ID: "ACME-T-0003", Title: "Snoozed submission", Inbox: &ItemInbox{
			Status: InboxSnoozed, SnoozedUntil: inboxDate(2026, time.October, 1), Source: "web",
		}},
		{ID: "ACME-T-0004", Title: "Untriaged submission"},
	}
	for _, d := range drafts {
		d.Type = TypeTask
		d.Status = "triage"
		if _, err := store.Create(ctx, d); err != nil {
			t.Fatalf("create %s: %v", d.ID, err)
		}
	}

	ix := NewIndex(fsys, []ProjectRef{ref})
	if _, err := ix.Build(ctx, true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ix, store, ref
}

func ptrFloat(f float64) *float64 { return &f }

func TestQueryInboxScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ix, _, _ := inboxVault(t)

	tests := []struct {
		name   string
		filter Filter
		want   []ItemID
	}{
		{
			name:   "the default filter excludes triage with no configuration",
			filter: Filter{},
			want:   []ItemID{"ACME-US-0001"},
		},
		{
			name:   "only returns just the inbox",
			filter: Filter{Inbox: InboxOnly, Sort: "id"},
			want:   []ItemID{"ACME-T-0001", "ACME-T-0002", "ACME-T-0003", "ACME-T-0004"},
		},
		{
			name:   "include returns both sides",
			filter: Filter{Inbox: InboxInclude, Sort: "id"},
			want: []ItemID{
				"ACME-T-0001", "ACME-T-0002", "ACME-T-0003", "ACME-T-0004", "ACME-US-0001",
			},
		},
		{
			name:   "an item with no block counts as pending",
			filter: Filter{Inbox: InboxOnly, InboxStatuses: []InboxStatus{InboxPending}, Sort: "id"},
			want:   []ItemID{"ACME-T-0001", "ACME-T-0004"},
		},
		{
			name:   "rejected is selectable on its own",
			filter: Filter{Inbox: InboxOnly, InboxStatuses: []InboxStatus{InboxRejected}},
			want:   []ItemID{"ACME-T-0002"},
		},
		{
			name:   "before the snooze expires the item is snoozed, not pending",
			filter: Filter{Inbox: InboxOnly, InboxStatuses: []InboxStatus{InboxPending}, SnoozeAsOf: inboxTime(2026, time.September, 30, 23), Sort: "id"},
			want:   []ItemID{"ACME-T-0001", "ACME-T-0004"},
		},
		{
			name:   "exactly at the snooze date the item reads as pending",
			filter: Filter{Inbox: InboxOnly, InboxStatuses: []InboxStatus{InboxPending}, SnoozeAsOf: inboxTime(2026, time.October, 1, 0), Sort: "id"},
			want:   []ItemID{"ACME-T-0001", "ACME-T-0003", "ACME-T-0004"},
		},
		{
			name:   "after the snooze date it is no longer snoozed",
			filter: Filter{Inbox: InboxOnly, InboxStatuses: []InboxStatus{InboxSnoozed}, SnoozeAsOf: inboxTime(2026, time.October, 2, 0)},
			want:   nil,
		},
		{
			name:   "an inbox-status filter never leaks non-triage items",
			filter: Filter{Inbox: InboxInclude, InboxStatuses: []InboxStatus{InboxPending}, Sort: "id"},
			want:   []ItemID{"ACME-T-0001", "ACME-T-0004"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			page, err := ix.Items(ctx, tt.filter)
			if err != nil {
				t.Fatalf("Items: %v", err)
			}
			var got []ItemID
			for _, it := range page.Items {
				got = append(got, it.ID)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ids = %v, want %v", got, tt.want)
			}
			if page.Total != len(tt.want) {
				t.Errorf("total = %d, want %d", page.Total, len(tt.want))
			}
		})
	}
}

func TestQueryInboxDoesNotDisturbSortingOrPagination(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ix, _, _ := inboxVault(t)

	first, err := ix.Items(ctx, Filter{Inbox: InboxOnly, Sort: "id", Limit: 2})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(first.Items) != 2 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	second, err := ix.Items(ctx, Filter{Inbox: InboxOnly, Sort: "id", Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("Items(page 2): %v", err)
	}
	if len(second.Items) != 2 || second.Items[0].ID != "ACME-T-0003" {
		t.Fatalf("second page = %+v", second.Items)
	}
	if second.Truncated {
		t.Error("the inbox of four items must end after two pages of two")
	}
}

func TestIndexInboxHelpers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ix, _, _ := inboxVault(t)

	if !ix.IsTriage("ACME-T-0001") {
		t.Error("a triage item must report as triage")
	}
	if ix.IsTriage("ACME-US-0001") {
		t.Error("an ordinary item must not report as triage")
	}
	if got := ix.ItemCategory("ACME-US-0001"); got != CategoryTodo {
		t.Errorf("ItemCategory(story) = %q", got)
	}
	page, err := ix.Inbox(ctx, Filter{})
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if page.Total != 4 {
		t.Errorf("Inbox total = %d, want 4", page.Total)
	}
}

func TestIndexReportsADeadDuplicateOf(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fsys := NewMemFS()
	ref, err := CreateProject(fsys, "docs", NewProject{Key: "ACME"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	store := NewStore(fsys, ref.BacklogPath, ref.Config)
	if _, err := store.Create(ctx, ItemDraft{
		ID: "ACME-T-0001", Type: TypeTask, Title: "Duplicate submission", Status: "triage",
		Inbox: &ItemInbox{Status: InboxDuplicate, DuplicateOf: "ACME-T-0099"},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	ix := NewIndex(fsys, []ProjectRef{ref})
	if _, err := ix.Build(ctx, true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	found := false
	for _, d := range ix.Warnings() {
		if d.Code == CodeWarnInboxDupDead {
			found = true
			if d.Severity != SeverityWarning {
				t.Errorf("severity = %q", d.Severity)
			}
		}
	}
	if !found {
		t.Errorf("want %s, got %v", CodeWarnInboxDupDead, ix.Warnings())
	}
}

func TestStoreInboxPatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, store, _ := inboxVault(t)

	item, err := store.Get(ctx, "ACME-T-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	accepted, err := store.Update(ctx, item.ID, ItemPatch{
		Inbox: &ItemInbox{Status: InboxAccepted, Source: item.Inbox.Source},
	}, item.Rev)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if accepted.Inbox == nil || accepted.Inbox.Status != InboxAccepted {
		t.Fatalf("inbox = %#v", accepted.Inbox)
	}
	cleared, err := store.Update(ctx, accepted.ID, ItemPatch{Unset: []string{"inbox"}}, accepted.Rev)
	if err != nil {
		t.Fatalf("Update(unset): %v", err)
	}
	if cleared.Inbox != nil {
		t.Errorf("unset inbox = %#v", cleared.Inbox)
	}
}

// triageBoardInput is the fixture workspace with one extra item sitting in the
// inbox. withTriage decides whether that item is in the source at all, which is
// how the tests assert "identical results whether or not triage items exist".
func triageBoardInput(withTriage bool) BoardInput {
	cfg := demoConfig()
	cfg.Workflow.Statuses = append([]StatusDef{
		{ID: "triage", Name: "Triage", Category: CategoryTriage},
	}, cfg.Workflow.Statuses...)

	items := demoItems()
	if withTriage {
		ts, err := ParseTimestamp("2026-09-03T09:00:00Z")
		if err != nil {
			panic(err)
		}
		est := 13.0
		items = append(items, Item{
			ID: "DEMO-T-0009", Type: TypeTask, Title: "Raw submission", Status: "triage",
			Priority: PriorityHigh, Estimate: &est, Updated: ts, Rev: "sha256:9999999999999999",
			Inbox: &ItemInbox{Status: InboxPending, Source: "web"},
		})
	}
	return BoardInput{
		Declared:    []ProjectKey{"DEMO", "WEB"},
		TeamVaultID: "team",
		Sources:     []BoardSource{{Project: "DEMO", VaultID: "demo", Config: cfg, Items: items}},
	}
}

func TestBoardViewExcludesTriageItems(t *testing.T) {
	t.Parallel()

	board := readFixtureBoard(t)
	without := BuildBoardView(board, triageBoardInput(false))
	with := BuildBoardView(board, triageBoardInput(true))

	if !reflect.DeepEqual(refsOfEveryColumn(without), refsOfEveryColumn(with)) {
		t.Errorf("a triage item changed the board\nwithout = %v\nwith    = %v",
			refsOfEveryColumn(without), refsOfEveryColumn(with))
	}
	for _, card := range boardCards(with) {
		if card.Category == CategoryTriage || card.Ref == "DEMO/DEMO-T-0009" {
			t.Errorf("a triage card reached the board: %+v", card)
		}
	}
}

// refsOfEveryColumn flattens a board view into column id → card refs.
func refsOfEveryColumn(view BoardView) map[string][]string {
	out := make(map[string][]string, len(view.Columns))
	for _, c := range view.Columns {
		out[c.ID] = refsOf(c)
	}
	return out
}

func TestSprintSurfacesReportATriageReferenceAsUnresolved(t *testing.T) {
	t.Parallel()

	board := readFixtureBoard(t)
	in := triageBoardInput(true)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	sprint := &Sprint{
		ID: "TEAM-S-0001", Type: "sprint", Board: "delivery", State: SprintActive,
		Start: NewDate(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
		End:   NewDate(time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)),
		Items: []string{"DEMO/DEMO-US-0001", "DEMO/DEMO-T-0009"},
	}
	sprint.Committed = append([]string(nil), sprint.Items...)

	view := BuildSprintView(sprint, board, in)
	for _, card := range view.Backlog {
		if card.Ref == "DEMO/DEMO-T-0009" {
			t.Error("a triage item must never be offered as a sprint candidate")
		}
	}

	summary := SummarizeSprint(sprint, view.Cards, now)
	if summary.Metrics.Items != 2 {
		t.Fatalf("items = %d, want 2", summary.Metrics.Items)
	}
	if summary.Metrics.Resolved != 1 || summary.Metrics.Unresolved != 1 {
		t.Errorf("resolved = %d, unresolved = %d; the triage reference must be unresolved",
			summary.Metrics.Resolved, summary.Metrics.Unresolved)
	}
	if summary.Metrics.Points != 8 {
		t.Errorf("points = %v, want 8: the triage item's 13 points must not count", summary.Metrics.Points)
	}
	if summary.Metrics.Done != 0 || summary.Metrics.DonePoints != 0 {
		t.Errorf("a triage reference must never count as done: %+v", summary.Metrics)
	}
}

func TestSprintMetricsIgnoreATriageObservation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	sprint := &Sprint{
		ID: "TEAM-S-0001", Type: "sprint", Board: "delivery", State: SprintActive,
		Start: NewDate(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
		End:   NewDate(time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)),
		Items: []string{"DEMO/DEMO-T-0009"},
	}
	est := 13.0
	history := ItemHistory{Ref: "DEMO/DEMO-T-0009", Observations: []ItemObservation{{
		At: inboxTime(2026, time.September, 1, 0),
		// The item existed from day one, but in the inbox.
		Status: "triage", Category: CategoryTriage, Estimate: &est,
	}}}

	view := BuildSprintMetrics(sprint, MetricsInput{
		Cards: []BoardCard{}, History: []ItemHistory{history}, Now: now,
	})
	for _, p := range view.Burndown.Points {
		if p.Scope != 0 {
			t.Errorf("day %d counted %v points of inbox work", p.Day, p.Scope)
		}
		if p.Completed != 0 {
			t.Errorf("day %d counted inbox work as completed", p.Day)
		}
	}
	for _, d := range view.Flow.Days {
		for band, n := range d.Counts {
			if n != 0 {
				t.Errorf("day %d put an inbox item in band %q", d.Day, band)
			}
		}
	}
	if bandOf(CategoryTriage) != FlowUnknown {
		t.Errorf("bandOf(triage) = %q, want the unknown band", bandOf(CategoryTriage))
	}
}

// TestInboxLandingStatus covers both settings of the YouTrack land_in_inbox
// option: off lands work where it has always landed, on lands it in triage, and
// on without a triage status is a refusal rather than a silent fallback.
func TestInboxLandingStatus(t *testing.T) {
	t.Parallel()

	withTriage := &ProjectConfig{Workflow: Workflow{
		Initial: "backlog",
		Statuses: []StatusDef{
			{ID: "triage", Category: CategoryTriage},
			{ID: "backlog", Category: CategoryTodo},
			{ID: "done", Category: CategoryDone},
		},
	}}
	withoutTriage := &ProjectConfig{Workflow: Workflow{
		Initial: "backlog",
		Statuses: []StatusDef{
			{ID: "backlog", Category: CategoryTodo},
			{ID: "done", Category: CategoryDone},
		},
	}}

	tests := []struct {
		name        string
		cfg         *ProjectConfig
		landInInbox bool
		want        Status
		wantErr     bool
	}{
		{
			name: "off lands in the workflow initial status",
			cfg:  withTriage,
			want: "backlog",
		},
		{
			name: "off does not need a triage status at all",
			cfg:  withoutTriage,
			want: "backlog",
		},
		{
			name:        "on lands in the project's triage status",
			cfg:         withTriage,
			landInInbox: true,
			want:        "triage",
		},
		{
			name:        "on without an inbox is refused, never quietly re-routed",
			cfg:         withoutTriage,
			landInInbox: true,
			wantErr:     true,
		},
		{
			name:        "no configuration at all is refused",
			cfg:         nil,
			landInInbox: true,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := InboxLandingStatus(tt.cfg, tt.landInInbox)
			switch {
			case tt.wantErr && !errors.Is(err, ErrNoTriageStatus):
				t.Fatalf("error = %v, want ErrNoTriageStatus", err)
			case !tt.wantErr && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case got != tt.want:
				t.Errorf("status = %q, want %q", got, tt.want)
			}
		})
	}
}
