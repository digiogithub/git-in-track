package core

import (
	"testing"
)

func TestItemReferences(t *testing.T) {
	t.Parallel()

	items := []Item{
		{ID: "ACME-US-0042", Type: TypeStory, Title: "The target", Path: "stories/ACME-US-0042.md"},
		{ID: "ACME-T-0001", Type: TypeTask, Title: "A child", Path: "tasks/ACME-T-0001.md", Parent: "ACME-US-0042"},
		{ID: "ACME-US-0043", Type: TypeStory, Title: "A blocked story", Path: "stories/ACME-US-0043.md", Links: []Link{
			{Kind: LinkBlockedBy, Target: "ACME-US-0042"},
			{Kind: LinkRelatesTo, Target: "ACME-US-0099"},
		}},
		{ID: "WEB-US-0007", Type: TypeStory, Title: "Cross project", Path: "stories/WEB-US-0007.md", Links: []Link{
			{Kind: LinkRelatesTo, Target: "ACME/ACME-US-0042"},
		}},
		{ID: "ACME-US-0044", Type: TypeStory, Title: "Unrelated", Path: "stories/ACME-US-0044.md"},
	}
	milestoneItems := []Item{
		{ID: "ACME-M-0002", Type: TypeMilestone, Title: "1.0", Path: "milestones/ACME-M-0002.md"},
		{ID: "ACME-US-0050", Type: TypeStory, Title: "In the milestone", Path: "stories/ACME-US-0050.md", Milestone: "ACME-M-0002"},
	}

	board := &Board{ID: "delivery", Title: "Delivery", Path: "boards/delivery.md", Order: NewBoardOrder()}
	board.Order.Set("doing", []string{"ACME/ACME-US-0042", "ACME/ACME-US-0044"})
	board.Order.Set("done", []string{"WEB/WEB-US-0007"})
	milestoneBoard := &Board{
		ID: "release", Title: "Release", Path: "boards/release.md",
		Filters: BoardFilters{Milestone: "ACME/ACME-M-0002"},
	}
	sprint := &Sprint{
		ID: "ACME-S-0003", Title: "Sprint 3", Path: "sprints/ACME-S-0003.md",
		Items:     []string{"ACME/ACME-US-0042"},
		Committed: []string{"ACME/ACME-US-0042", "ACME/ACME-US-0044"},
	}
	retro := &Retro{
		ID: "ACME-R-0002", Title: "Retro 2", Path: "retros/ACME-R-0002.md",
		Actions: []RetroAction{
			{ID: "a1", Title: "Promoted", Task: "ACME/ACME-US-0042"},
			{ID: "a2", Title: "Not promoted"},
		},
	}

	type want struct {
		kind  string
		id    string
		field string
	}

	tests := []struct {
		name  string
		id    ItemID
		src   ItemReferenceSources
		want  []want
		count int
	}{
		{
			name: "an item nothing points at",
			id:   "ACME-US-0044",
			src:  ItemReferenceSources{Items: items},
		},
		{
			name: "an empty id is never a reference",
			id:   "   ",
			src:  ItemReferenceSources{Items: items, Artifacts: TeamArtifacts{Boards: []*Board{board}}},
		},
		{
			name: "children, links and cross-project links",
			id:   "ACME-US-0042",
			src:  ItemReferenceSources{Items: items},
			want: []want{
				{"item", "ACME-T-0001", "parent"},
				{"item", "ACME-US-0043", "links.blocked_by"},
				{"item", "WEB-US-0007", "links.relates_to"},
			},
		},
		{
			name: "a milestone is referenced by its items and by a board scope",
			id:   "ACME-M-0002",
			src: ItemReferenceSources{
				Items:     milestoneItems,
				Artifacts: TeamArtifacts{Boards: []*Board{milestoneBoard}},
			},
			want: []want{
				{"board", "release", "filters.milestone"},
				{"item", "ACME-US-0050", "milestone"},
			},
		},
		{
			name: "board order, sprint scope and a promoted retro action",
			id:   "ACME-US-0042",
			src: ItemReferenceSources{
				Artifacts: TeamArtifacts{
					Boards:  []*Board{board},
					Sprints: []*Sprint{sprint},
					Retros:  []*Retro{retro},
				},
			},
			want: []want{
				{"board", "delivery", "order.doing"},
				{"retro", "ACME-R-0002", "actions.a1"},
				{"sprint", "ACME-S-0003", "committed"},
				{"sprint", "ACME-S-0003", "items"},
			},
		},
		{
			name: "nil artifacts contribute nothing",
			id:   "ACME-US-0042",
			src: ItemReferenceSources{Artifacts: TeamArtifacts{
				Boards: []*Board{nil}, Sprints: []*Sprint{nil}, Retros: []*Retro{nil},
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ItemReferences(tc.id, tc.src)
			if len(got) != len(tc.want) {
				t.Fatalf("ItemReferences() returned %d references, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, w := range tc.want {
				if got[i].Kind != w.kind || got[i].ID != w.id || got[i].Field != w.field {
					t.Errorf("reference %d = {%s %s %s}, want {%s %s %s}",
						i, got[i].Kind, got[i].ID, got[i].Field, w.kind, w.id, w.field)
				}
				if got[i].Path == "" {
					t.Errorf("reference %d carries no path: %+v", i, got[i])
				}
			}
		})
	}
}

func TestItemReferencesIgnoresTheTargetItself(t *testing.T) {
	t.Parallel()

	// A self-referential link is a data error the validator reports; it must
	// never show up as a reason not to delete the item.
	items := []Item{{
		ID: "ACME-US-0042", Type: TypeStory, Path: "stories/ACME-US-0042.md",
		Links: []Link{{Kind: LinkRelatesTo, Target: "ACME-US-0042"}},
	}}
	if got := ItemReferences("ACME-US-0042", ItemReferenceSources{Items: items}); len(got) != 0 {
		t.Errorf("ItemReferences() = %+v, want none", got)
	}
}
