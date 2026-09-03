package core

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func itemIDs(page Page[Item]) []ItemID {
	out := make([]ItemID, 0, len(page.Items))
	for _, it := range page.Items {
		out = append(out, it.ID)
	}
	return out
}

func TestItemsFilters(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		filter Filter
		want   []ItemID
	}{
		{
			name:   "everything sorted by updated desc",
			filter: Filter{Limit: 50},
			want:   []ItemID{"DEMO-T-0001", "DEMO-US-0001", "DEMO-EP-0001", "DEMO-M-0001", "DEMO-US-0002"},
		},
		{
			name:   "by type",
			filter: Filter{Types: []ItemType{TypeStory}, Sort: "id"},
			want:   []ItemID{"DEMO-US-0001", "DEMO-US-0002"},
		},
		{
			name:   "by several statuses is an OR",
			filter: Filter{Statuses: []Status{"todo", "in_review"}, Sort: "id"},
			want:   []ItemID{"DEMO-T-0001", "DEMO-US-0002"},
		},
		{
			name:   "by assignee",
			filter: Filter{Assignees: []string{"marta"}, Sort: "id"},
			want:   []ItemID{"DEMO-US-0001", "DEMO-US-0002"},
		},
		{
			name:   "by @me",
			filter: Filter{Assignees: []string{MeToken}, Me: "jose", Sort: "id"},
			want:   []ItemID{"DEMO-EP-0001", "DEMO-T-0001", "DEMO-US-0001"},
		},
		{
			name:   "unassigned",
			filter: Filter{Assignees: []string{Unassigned}, Sort: "id"},
			want:   []ItemID{"DEMO-M-0001"},
		},
		{
			name:   "labels are ANDed",
			filter: Filter{Labels: []string{"frontend", "payments"}, Sort: "id"},
			want:   []ItemID{"DEMO-EP-0001", "DEMO-M-0001"},
		},
		{
			name:   "labels are case insensitive",
			filter: Filter{Labels: []string{"FrontEnd"}, Sort: "id"},
			want:   []ItemID{"DEMO-EP-0001", "DEMO-M-0001", "DEMO-US-0001"},
		},
		{
			name:   "by parent",
			filter: Filter{Parent: "DEMO-EP-0001", Sort: "id"},
			want:   []ItemID{"DEMO-US-0001", "DEMO-US-0002"},
		},
		{
			name:   "by milestone",
			filter: Filter{Milestone: "DEMO-M-0001", Sort: "id"},
			want:   []ItemID{"DEMO-EP-0001", "DEMO-US-0001", "DEMO-US-0002"},
		},
		{
			name:   "by priority",
			filter: Filter{Priorities: []Priority{PriorityHigh}, Sort: "id"},
			want:   []ItemID{"DEMO-EP-0001", "DEMO-T-0001", "DEMO-US-0001"},
		},
		{
			name:   "by project",
			filter: Filter{Projects: []ProjectKey{"OTHER"}, Sort: "id"},
			want:   []ItemID{},
		},
		{
			name:   "text matches the title case insensitively",
			filter: Filter{Text: "GUEST checkout", Sort: "id"},
			want:   []ItemID{"DEMO-M-0001", "DEMO-US-0001"},
		},
		{
			name:   "text matches the body",
			filter: Filter{Text: "postal addresses", Sort: "id"},
			want:   []ItemID{"DEMO-T-0001"},
		},
		{
			name:   "text matches a label",
			filter: Filter{Text: "payments", Sort: "id"},
			want:   []ItemID{"DEMO-EP-0001", "DEMO-M-0001", "DEMO-US-0002"},
		},
		{
			name:   "updated since",
			filter: Filter{UpdatedSince: NewTimestamp(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)), Sort: "id"},
			want:   []ItemID{"DEMO-T-0001"},
		},
		{
			name: "combined filters are ANDed",
			filter: Filter{
				Types:    []ItemType{TypeStory},
				Statuses: []Status{"in_progress"},
				Labels:   []string{"frontend"},
				Sort:     "id",
			},
			want: []ItemID{"DEMO-US-0001"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, err := ix.Items(ctx, tc.filter)
			if err != nil {
				t.Fatalf("Items: %v", err)
			}
			if got := itemIDs(page); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ids = %v, want %v", got, tc.want)
			}
			if page.Total != len(tc.want) {
				t.Errorf("total = %d, want %d", page.Total, len(tc.want))
			}
		})
	}
}

func TestItemsSorting(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	ctx := context.Background()

	tests := []struct {
		sort string
		want []ItemID
	}{
		{"id", []ItemID{"DEMO-EP-0001", "DEMO-M-0001", "DEMO-T-0001", "DEMO-US-0001", "DEMO-US-0002"}},
		{"-id", []ItemID{"DEMO-US-0002", "DEMO-US-0001", "DEMO-T-0001", "DEMO-M-0001", "DEMO-EP-0001"}},
		{"created", []ItemID{"DEMO-M-0001", "DEMO-EP-0001", "DEMO-US-0001", "DEMO-US-0002", "DEMO-T-0001"}},
		{"-updated", []ItemID{"DEMO-T-0001", "DEMO-US-0001", "DEMO-EP-0001", "DEMO-M-0001", "DEMO-US-0002"}},
		// Descending priority puts the most important item first; the milestone
		// declares none and sorts last.
		{"-priority,id", []ItemID{"DEMO-EP-0001", "DEMO-T-0001", "DEMO-US-0001", "DEMO-US-0002", "DEMO-M-0001"}},
		{"title", []ItemID{"DEMO-T-0001", "DEMO-EP-0001", "DEMO-US-0001", "DEMO-M-0001", "DEMO-US-0002"}},
	}
	for _, tc := range tests {
		t.Run(tc.sort, func(t *testing.T) {
			page, err := ix.Items(ctx, Filter{Sort: tc.sort, Limit: 50})
			if err != nil {
				t.Fatalf("Items: %v", err)
			}
			if got := itemIDs(page); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ids = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("unknown key", func(t *testing.T) {
		if _, err := ix.Items(ctx, Filter{Sort: "color"}); err == nil {
			t.Error("want an error for an unknown sort key")
		}
	})

	t.Run("order is deterministic", func(t *testing.T) {
		first, err := ix.Items(ctx, Filter{Sort: "status", Limit: 50})
		if err != nil {
			t.Fatalf("Items: %v", err)
		}
		for i := 0; i < 5; i++ {
			again, err := ix.Items(ctx, Filter{Sort: "status", Limit: 50})
			if err != nil {
				t.Fatalf("Items: %v", err)
			}
			if !reflect.DeepEqual(itemIDs(first), itemIDs(again)) {
				t.Fatalf("run %d differs: %v vs %v", i, itemIDs(first), itemIDs(again))
			}
		}
	})
}

func TestItemsPagination(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	ctx := context.Background()

	var seen []ItemID
	filter := Filter{Sort: "id", Limit: 2}
	for page := 0; ; page++ {
		got, err := ix.Items(ctx, filter)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if got.Total != 5 {
			t.Errorf("total = %d, want 5", got.Total)
		}
		seen = append(seen, itemIDs(got)...)
		if got.NextCursor == "" {
			if got.Truncated {
				t.Error("truncated without a cursor")
			}
			break
		}
		filter.Cursor = got.NextCursor
		if page > 5 {
			t.Fatal("pagination does not terminate")
		}
	}
	want := []ItemID{"DEMO-EP-0001", "DEMO-M-0001", "DEMO-T-0001", "DEMO-US-0001", "DEMO-US-0002"}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("walked %v, want %v", seen, want)
	}

	t.Run("cursor is bound to its sort", func(t *testing.T) {
		first, err := ix.Items(ctx, Filter{Sort: "id", Limit: 2})
		if err != nil {
			t.Fatalf("Items: %v", err)
		}
		if _, err := ix.Items(ctx, Filter{Sort: "-updated", Limit: 2, Cursor: first.NextCursor}); err == nil {
			t.Error("want an error when the sort changes mid-pagination")
		}
	})

	t.Run("cursor survives an insertion before it", func(t *testing.T) {
		first, err := ix.Items(ctx, Filter{Sort: "id", Limit: 2})
		if err != nil {
			t.Fatalf("Items: %v", err)
		}
		next, err := ix.Items(ctx, Filter{Sort: "id", Limit: 2, Cursor: first.NextCursor})
		if err != nil {
			t.Fatalf("Items: %v", err)
		}
		if got := itemIDs(next); !reflect.DeepEqual(got, []ItemID{"DEMO-T-0001", "DEMO-US-0001"}) {
			t.Errorf("second page = %v", got)
		}
	})

	t.Run("bad cursor", func(t *testing.T) {
		if _, err := ix.Items(ctx, Filter{Cursor: "not base64 !!"}); err == nil {
			t.Error("want an error for a malformed cursor")
		}
	})

	t.Run("limit is clamped", func(t *testing.T) {
		page, err := ix.Items(ctx, Filter{Limit: 10_000})
		if err != nil {
			t.Fatalf("Items: %v", err)
		}
		if page.Limit != MaxLimit {
			t.Errorf("limit = %d, want %d", page.Limit, MaxLimit)
		}
	})
}

func TestItemChildrenAndComments(t *testing.T) {
	ix, _ := buildFixtureIndex(t)

	kids := ix.Children("DEMO-EP-0001")
	if len(kids) != 2 || kids[0].ID != "DEMO-US-0001" || kids[1].ID != "DEMO-US-0002" {
		t.Errorf("children = %v", kids)
	}
	if got := ix.Children("DEMO-US-0001"); len(got) != 1 || got[0].ID != "DEMO-T-0001" {
		t.Errorf("children of the story = %v", got)
	}
	comments := ix.Comments("DEMO-US-0001")
	if len(comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(comments))
	}
	if comments[0].Author != "marta" {
		t.Errorf("author = %q", comments[0].Author)
	}
	if got := comments[0].Ref(); got != "DEMO-US-0001#20260901T104512Z-marta" {
		t.Errorf("ref = %q", got)
	}

	if _, err := ix.Item("DEMO-US-9999"); !errors.Is(err, ErrItemNotFound) {
		t.Errorf("Item error = %v, want ErrItemNotFound", err)
	}
}

func TestItemReturnsACopy(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	it, err := ix.Item("DEMO-US-0001")
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	it.Title = "mutated"
	it.Labels[0] = "mutated"
	again, err := ix.Item("DEMO-US-0001")
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if again.Title == "mutated" || again.Labels[0] == "mutated" {
		t.Error("the index handed out a reference into its own state")
	}
}

func TestMilestonesAndEpics(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	ms := ix.Milestones()
	if len(ms) != 1 || ms[0].ID != "DEMO-M-0001" {
		t.Errorf("milestones = %v", ms)
	}
	eps := ix.Epics()
	if len(eps) != 1 || eps[0].ID != "DEMO-EP-0001" {
		t.Errorf("epics = %v", eps)
	}
}

func TestSearchRanking(t *testing.T) {
	ix, _ := buildFixtureIndex(t)

	hits := ix.Search("checkout", 10)
	if len(hits) == 0 {
		t.Fatal("no hits for checkout")
	}
	// The epic has the term in its title and outranks a body-only match.
	if hits[0].ID != "DEMO-EP-0001" {
		t.Errorf("first hit = %+v, want the epic whose title matches", hits[0])
	}
	kinds := map[string]bool{}
	for _, h := range hits {
		kinds[h.Kind] = true
	}
	if !kinds["item"] {
		t.Error("no item hit")
	}

	t.Run("labels outrank bodies", func(t *testing.T) {
		hits := ix.Search("payments", 10)
		if len(hits) < 2 {
			t.Fatalf("hits = %v", hits)
		}
		if hits[0].Score <= hits[len(hits)-1].Score {
			t.Errorf("scores are not ordered: %v", hits)
		}
	})

	t.Run("exact id wins", func(t *testing.T) {
		hits := ix.Search("demo-t-0001", 5)
		if len(hits) == 0 || hits[0].ID != "DEMO-T-0001" {
			t.Errorf("hits = %v", hits)
		}
	})

	t.Run("pages are searched too", func(t *testing.T) {
		hits := ix.Search("storefront", 10)
		if len(hits) != 1 || hits[0].Kind != "page" {
			t.Fatalf("hits = %v", hits)
		}
		if hits[0].Path != "docs/architecture/overview.md" {
			t.Errorf("path = %q", hits[0].Path)
		}
		if hits[0].Snippet == "" {
			t.Error("no snippet")
		}
	})

	t.Run("every term must match", func(t *testing.T) {
		if hits := ix.Search("checkout unrelatedterm", 10); len(hits) != 0 {
			t.Errorf("hits = %v, want none", hits)
		}
	})

	t.Run("empty query", func(t *testing.T) {
		if hits := ix.Search("   ", 10); hits != nil {
			t.Errorf("hits = %v, want nil", hits)
		}
	})
}

func TestProjectFields(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	it, err := ix.Item("DEMO-US-0001")
	if err != nil {
		t.Fatalf("Item: %v", err)
	}

	t.Run("defaults", func(t *testing.T) {
		got := ProjectFields(it, nil)
		for _, f := range DefaultFields {
			if _, ok := got[f]; !ok {
				t.Errorf("missing default field %q", f)
			}
		}
		if _, ok := got["body"]; ok {
			t.Error("the default projection must not carry the body")
		}
	})

	t.Run("selected", func(t *testing.T) {
		got := ProjectFields(it, []string{"id", "title", "estimate", "parent", "project", "unknown"})
		if len(got) != 5 {
			t.Errorf("fields = %v", got)
		}
		if got["id"] != ItemID("DEMO-US-0001") {
			t.Errorf("id = %v", got["id"])
		}
		if got["estimate"] != 8.0 {
			t.Errorf("estimate = %v", got["estimate"])
		}
		if got["parent"] != "DEMO-EP-0001" {
			t.Errorf("parent = %v", got["parent"])
		}
		if got["project"] != ProjectKey("DEMO") {
			t.Errorf("project = %v", got["project"])
		}
	})

	t.Run("absent values are null", func(t *testing.T) {
		task, err := ix.Item("DEMO-T-0001")
		if err != nil {
			t.Fatalf("Item: %v", err)
		}
		got := ProjectFields(task, []string{"estimate", "milestone", "due"})
		for k, v := range got {
			if v != nil {
				t.Errorf("%s = %v, want nil", k, v)
			}
		}
	})
}

func TestParseUpdatedSince(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: ""},
		{in: "2026-09-01T00:00:00Z", want: "2026-09-01T00:00:00Z"},
		{in: "2026-09-01", want: "2026-09-01T00:00:00Z"},
		{in: "7d", want: "2026-08-27T12:00:00Z"},
		{in: "12h", want: "2026-09-03T00:00:00Z"},
		{in: "2w", want: "2026-08-20T12:00:00Z"},
		{in: "30m", want: "2026-09-03T11:30:00Z"},
		{in: "7y", wantErr: true},
		{in: "soon", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseUpdatedSince(tc.in, now)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseUpdatedSince: %v", err)
			}
			if got.String() != tc.want {
				t.Errorf("got %q, want %q", got.String(), tc.want)
			}
		})
	}
}

func TestKbTreeAndPage(t *testing.T) {
	ix, _ := buildFixtureIndex(t)

	page, ok := ix.Page("docs/architecture/overview.md")
	if !ok {
		t.Fatal("page not found")
	}
	if page.Title != "Architecture overview" {
		t.Errorf("title = %q", page.Title)
	}
	if page.RelPath != "architecture/overview.md" {
		t.Errorf("rel path = %q", page.RelPath)
	}
	if page.Project != "DEMO" {
		t.Errorf("project = %q", page.Project)
	}
	if len(page.Headings) < 2 {
		t.Errorf("headings = %v", page.Headings)
	}
	if _, ok := ix.Page("docs/nope.md"); ok {
		t.Error("a missing page must not be found")
	}

	tree := ix.KbTree()
	if tree.Path != "docs" || !tree.IsDir {
		t.Fatalf("root = %+v", tree)
	}
	var names []string
	for _, c := range tree.Children {
		names = append(names, c.Name)
	}
	if !reflect.DeepEqual(names, []string{"architecture", "index.md"}) {
		t.Errorf("children = %v, want the folder first", names)
	}
	if len(tree.Children[0].Children) != 1 {
		t.Errorf("architecture children = %v", tree.Children[0].Children)
	}
	if got := tree.Children[0].Children[0].Title; got != "Architecture overview" {
		t.Errorf("leaf title = %q", got)
	}
}

func TestItemsOverGeneratedCorpus(t *testing.T) {
	m := generatedVault(200)
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	ix := NewIndex(m, projects)
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	page, err := ix.Items(context.Background(), Filter{
		Labels: []string{"generated"}, Sort: "-priority,id", Limit: MaxLimit,
	})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if page.Total != 200 {
		t.Fatalf("total = %d, want 200", page.Total)
	}
	last := 5
	for _, it := range page.Items {
		rank := priorityRank(it.Priority)
		if rank > last {
			t.Fatalf("%s (%s) is out of order", it.ID, it.Priority)
		}
		last = rank
	}
	if !strings.HasPrefix(string(page.Items[0].ID), "BENCH-") {
		t.Errorf("unexpected id %q", page.Items[0].ID)
	}
}
