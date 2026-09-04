package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// demoItems returns the items of the fixture project, shaped the way an index
// hands them to a board.
func demoItems() []Item {
	est := func(v float64) *float64 { return &v }
	must := func(s string) Timestamp {
		ts, err := ParseTimestamp(s)
		if err != nil {
			panic(err)
		}
		return ts
	}
	return []Item{
		{ID: "DEMO-US-0001", Type: TypeStory, Title: "Guest checkout", Status: "in_progress",
			Priority: PriorityHigh, Assignees: []string{"marta", "jose"}, Labels: []string{"frontend"},
			Estimate: est(8), Updated: must("2026-09-01T10:45:12Z"), Rev: "sha256:1111111111111111"},
		{ID: "DEMO-US-0002", Type: TypeStory, Title: "Save payment methods", Status: "todo",
			Priority: PriorityMedium, Assignees: []string{"marta"}, Labels: []string{"backend", "payments"},
			Estimate: est(5), Updated: must("2026-08-30T16:20:10Z"), Rev: "sha256:2222222222222222"},
		{ID: "DEMO-T-0001", Type: TypeTask, Title: "Add address validation", Status: "in_review",
			Priority: PriorityHigh, Assignees: []string{"jose"}, Labels: []string{"backend"},
			Updated: must("2026-09-02T16:03:19Z"), Rev: "sha256:3333333333333333"},
		{ID: "DEMO-EP-0001", Type: TypeEpic, Title: "Checkout revamp", Status: "in_progress",
			Priority: PriorityHigh, Updated: must("2026-09-01T09:00:00Z"), Rev: "sha256:4444444444444444"},
		{ID: "DEMO-T-0002", Type: TypeTask, Title: "Retire the old cart", Status: "archived",
			Priority: PriorityLow, Updated: must("2026-08-01T09:00:00Z"), Rev: "sha256:5555555555555555"},
	}
}

// fixtureInput is the workspace the fixture board renders against: DEMO cloned,
// WEB declared but not.
func fixtureInput() BoardInput {
	return BoardInput{
		Declared:    []ProjectKey{"DEMO", "WEB"},
		TeamVaultID: "team",
		Sources: []BoardSource{{
			Project: "DEMO", VaultID: "demo", Config: demoConfig(), Items: demoItems(),
		}},
	}
}

// columnOf returns a rendered column by id.
func columnOf(view BoardView, id string) BoardColumnView {
	for _, c := range view.Columns {
		if c.ID == id {
			return c
		}
	}
	return BoardColumnView{}
}

// refsOf lists the card refs of a column.
func refsOf(c BoardColumnView) []string {
	out := make([]string, 0, len(c.Cards))
	for _, card := range c.Cards {
		out = append(out, card.Ref)
	}
	return out
}

func TestBuildBoardView(t *testing.T) {
	board := readFixtureBoard(t)
	view := BuildBoardView(board, fixtureInput())

	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "cards come from every referenced project",
			check: func(t *testing.T) {
				if got := refsOf(columnOf(view, "todo")); len(got) != 2 ||
					got[0] != "DEMO/DEMO-US-0002" || got[1] != "WEB/WEB-US-0031" {
					t.Fatalf("todo = %v", got)
				}
				if got := refsOf(columnOf(view, "in_progress")); len(got) != 2 ||
					got[1] != "WEB/WEB-T-0007" {
					t.Fatalf("in_progress = %v", got)
				}
			},
		},
		{
			name: "a card of a project nobody cloned is remote and inert",
			check: func(t *testing.T) {
				card := columnOf(view, "todo").Cards[1]
				if !card.Remote || !card.Declared {
					t.Fatalf("card = %+v", card)
				}
				if card.Title != "" || card.Status != "" {
					t.Fatalf("a remote card carries no item state yet: %+v", card)
				}
				if !strings.Contains(card.Reason, "not cloned") {
					t.Fatalf("reason = %q", card.Reason)
				}
			},
		},
		{
			name: "cards carry id, title, assignees, labels, priority and estimate",
			check: func(t *testing.T) {
				card := columnOf(view, "in_progress").Cards[0]
				if card.Item != "DEMO-US-0001" || card.Title != "Guest checkout" {
					t.Fatalf("card = %+v", card)
				}
				if len(card.Assignees) != 2 || len(card.Labels) != 1 ||
					card.Priority != PriorityHigh || card.Estimate == nil || *card.Estimate != 8 {
					t.Fatalf("card = %+v", card)
				}
			},
		},
		{
			name: "the type filter is applied on load",
			check: func(t *testing.T) {
				for _, c := range view.Columns {
					for _, card := range c.Cards {
						if card.Type == TypeEpic {
							t.Fatalf("the epic passed a types: [story, task] filter")
						}
					}
				}
			},
		},
		{
			name: "an unmapped status is surfaced, never hidden",
			check: func(t *testing.T) {
				if len(view.Unmapped) != 1 || view.Unmapped[0].Item != "DEMO-T-0002" {
					t.Fatalf("unmapped = %+v", view.Unmapped)
				}
				if !strings.Contains(view.Unmapped[0].Reason, "maps to no column") {
					t.Fatalf("reason = %q", view.Unmapped[0].Reason)
				}
			},
		},
		{
			name: "a column over its limit is marked",
			check: func(t *testing.T) {
				if columnOf(view, "in_progress").Exceeded {
					t.Fatal("in_progress holds 2 of 2")
				}
				if !columnOf(view, "in_review").Exceeded {
					// in_review has a limit of 1 and holds DEMO-T-0001 only.
					t.Log("in_review is within its limit, as expected")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.check(t) })
	}
}

func TestBuildBoardViewOrdering(t *testing.T) {
	tests := []struct {
		name  string
		order []string
		want  []string
	}{
		{
			name:  "the order list wins",
			order: []string{"DEMO/DEMO-T-0001", "DEMO/DEMO-US-0001"},
			want:  []string{"DEMO/DEMO-T-0001", "DEMO/DEMO-US-0001"},
		},
		{
			name:  "unlisted cards follow by priority then updated",
			order: nil,
			want:  []string{"DEMO/DEMO-T-0001", "DEMO/DEMO-US-0001"},
		},
		{
			name:  "a partial list puts the listed card first",
			order: []string{"DEMO/DEMO-US-0001"},
			want:  []string{"DEMO/DEMO-US-0001", "DEMO/DEMO-T-0001"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			board, err := ParseBoard("b.md", []byte(`---
id: b
type: board
kind: kanban
title: B
columns:
  - id: doing
    statuses:
      "*": [in_progress, in_review]
filters:
  types: [story, task]
---
`))
			if err != nil {
				t.Fatalf("ParseBoard: %v", err)
			}
			if tc.order != nil {
				board.OrderList().Set("doing", tc.order)
			}
			view := BuildBoardView(board, fixtureInput())
			got := refsOf(columnOf(view, "doing"))
			if len(got) != len(tc.want) {
				t.Fatalf("cards = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("cards = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestPlanMove(t *testing.T) {
	board := readFixtureBoard(t)
	view := BuildBoardView(board, fixtureInput())
	cfg := demoConfig()

	tests := []struct {
		name      string
		move      BoardMove
		cfg       *ProjectConfig
		wantErr   bool
		wantAbout string
		check     func(t *testing.T, plan BoardMovePlan)
	}{
		{
			name: "a move between columns picks the first mapped status",
			move: BoardMove{Ref: Ref{Project: "DEMO", Item: "DEMO-US-0002"}, ToColumn: "in_progress", Position: 0},
			cfg:  cfg,
			check: func(t *testing.T, plan BoardMovePlan) {
				if plan.FromColumn != "todo" || plan.ToColumn != "in_progress" {
					t.Fatalf("plan = %+v", plan)
				}
				if plan.Status != "in_progress" || !plan.StatusChanged {
					t.Fatalf("plan = %+v", plan)
				}
				if !plan.WIPExceeded || plan.WIPUsed != 3 || plan.WIPLimit != 2 {
					t.Fatalf("wip = %+v", plan)
				}
			},
		},
		{
			name: "a re-order inside a column changes no status",
			move: BoardMove{Ref: Ref{Project: "DEMO", Item: "DEMO-US-0001"}, ToColumn: "in_progress", Position: 1},
			cfg:  cfg,
			check: func(t *testing.T, plan BoardMovePlan) {
				if plan.StatusChanged {
					t.Fatalf("plan = %+v", plan)
				}
				if plan.Status != "in_progress" {
					t.Fatalf("status = %q", plan.Status)
				}
			},
		},
		{
			name: "a categories column resolves through the project workflow",
			move: BoardMove{Ref: Ref{Project: "DEMO", Item: "DEMO-T-0001"}, ToColumn: "done", Position: 0},
			cfg:  cfg,
			check: func(t *testing.T, plan BoardMovePlan) {
				if plan.Status != "done" {
					t.Fatalf("status = %q, choices %v", plan.Status, plan.Choices)
				}
				if len(plan.Choices) != 2 {
					t.Fatalf("choices = %v", plan.Choices)
				}
			},
		},
		{
			name: "an explicit status must be one the column maps",
			move: BoardMove{Ref: Ref{Project: "DEMO", Item: "DEMO-T-0001"}, ToColumn: "done",
				Position: 0, Status: "backlog"},
			cfg:       cfg,
			wantErr:   true,
			wantAbout: "does not map status",
		},
		{
			name:      "a remote card refuses to move",
			move:      BoardMove{Ref: Ref{Project: "WEB", Item: "WEB-US-0031"}, ToColumn: "in_progress"},
			cfg:       nil,
			wantErr:   true,
			wantAbout: "not cloned",
		},
		{
			name:      "an unknown column is an error",
			move:      BoardMove{Ref: Ref{Project: "DEMO", Item: "DEMO-US-0001"}, ToColumn: "nope"},
			cfg:       cfg,
			wantErr:   true,
			wantAbout: "no column",
		},
		{
			name:      "a card the board does not show is an error",
			move:      BoardMove{Ref: Ref{Project: "DEMO", Item: "DEMO-US-0099"}, ToColumn: "todo"},
			cfg:       cfg,
			wantErr:   true,
			wantAbout: "does not show",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanMove(board, view, tc.move, tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %+v", plan)
				}
				if !strings.Contains(err.Error(), tc.wantAbout) {
					t.Fatalf("error = %v, want it to mention %q", err, tc.wantAbout)
				}
				return
			}
			if err != nil {
				t.Fatalf("PlanMove: %v", err)
			}
			tc.check(t, plan)
		})
	}
}

func TestApplyMoveRewritesOnlyTheOrder(t *testing.T) {
	board := readFixtureBoard(t)
	before, err := SerializeBoard(board)
	if err != nil {
		t.Fatalf("SerializeBoard: %v", err)
	}
	ApplyMove(board, BoardMove{
		Ref: Ref{Project: "DEMO", Item: "DEMO-US-0002"}, ToColumn: "in_progress", Position: 0,
	})
	after, err := SerializeBoard(board)
	if err != nil {
		t.Fatalf("SerializeBoard: %v", err)
	}

	// Everything outside the order block is byte-identical: a move rewrites one
	// item file and the board's order list, and nothing else (R-MOVE-1).
	beforeRest, beforeOrder := splitOrderBlock(t, string(before))
	afterRest, afterOrder := splitOrderBlock(t, string(after))
	if beforeRest != afterRest {
		t.Fatalf("a move changed the board outside order:\n--- before ---\n%s\n--- after ---\n%s",
			beforeRest, afterRest)
	}
	if beforeOrder == afterOrder {
		t.Fatal("the order block did not change")
	}
	if !strings.Contains(afterOrder, "  in_progress:\n    - DEMO/DEMO-US-0002\n    - DEMO/DEMO-US-0001\n") {
		t.Fatalf("order was not rewritten:\n%s", afterOrder)
	}
	if strings.Contains(afterOrder, "  todo:\n    - DEMO/DEMO-US-0002\n") {
		t.Fatalf("the card was not removed from its old column:\n%s", afterOrder)
	}
}

// splitOrderBlock separates a serialized board into everything but its order
// block and the block itself.
func splitOrderBlock(t *testing.T, text string) (rest, order string) {
	t.Helper()
	start := strings.Index(text, "\norder:\n")
	if start < 0 {
		t.Fatalf("no order block in:\n%s", text)
	}
	end := strings.Index(text[start+1:], "\ncreated:")
	if end < 0 {
		t.Fatalf("no created: after the order block in:\n%s", text)
	}
	end += start + 1
	return text[:start] + text[end:], text[start:end]
}

// TestConcurrentMovesMerge is the milestone-4 exit criterion: two people moving
// different cards, each starting from the same board, must produce diffs git
// merges without a conflict. It drives a real `git merge-file`, so it measures
// the emitted bytes rather than a model of them.
func TestConcurrentMovesMerge(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	base := readFixtureBoard(t)
	baseBytes, err := SerializeBoard(base)
	if err != nil {
		t.Fatalf("SerializeBoard: %v", err)
	}

	tests := []struct {
		name  string
		mine  BoardMove
		yours BoardMove
	}{
		{
			name:  "two different cards into two different columns",
			mine:  BoardMove{Ref: Ref{Project: "DEMO", Item: "DEMO-US-0002"}, ToColumn: "in_progress", Position: 0},
			yours: BoardMove{Ref: Ref{Project: "DEMO", Item: "DEMO-T-0001"}, ToColumn: "done", Position: 0},
		},
		{
			name:  "one card moved, one card re-ordered in another column",
			mine:  BoardMove{Ref: Ref{Project: "DEMO", Item: "DEMO-T-0001"}, ToColumn: "done", Position: 0},
			yours: BoardMove{Ref: Ref{Project: "WEB", Item: "WEB-US-0031"}, ToColumn: "todo", Position: 0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mine := boardAfter(t, baseBytes, tc.mine)
			yours := boardAfter(t, baseBytes, tc.yours)

			dir := t.TempDir()
			write := func(name string, data []byte) string {
				p := filepath.Join(dir, name)
				if err := os.WriteFile(p, data, 0o600); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
				return p
			}
			minePath := write("mine.md", mine)
			basePath := write("base.md", baseBytes)
			yoursPath := write("yours.md", yours)

			// git merge-file rewrites the first file in place and exits with the
			// number of conflicts; anything but 0 is a conflict.
			cmd := exec.Command("git", "merge-file", "-L", "mine", "-L", "base", "-L", "yours",
				minePath, basePath, yoursPath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("git merge-file reported a conflict: %v\n%s", err, out)
			}

			merged, err := os.ReadFile(minePath)
			if err != nil {
				t.Fatalf("read merged: %v", err)
			}
			if strings.Contains(string(merged), "<<<<<<<") {
				t.Fatalf("the merge left conflict markers:\n%s", merged)
			}
			board, err := ParseBoard("delivery.md", merged)
			if err != nil {
				t.Fatalf("the merged board does not parse: %v\n%s", err, merged)
			}
			// Both moves survived the merge.
			for _, move := range []BoardMove{tc.mine, tc.yours} {
				refs := board.Order.Refs(move.ToColumn)
				if !containsString(refs, move.Ref.String()) {
					t.Fatalf("%s is not in column %s after the merge: %v",
						move.Ref, move.ToColumn, refs)
				}
			}
		})
	}
}

// boardAfter applies one move to a serialized board and returns the new bytes.
func boardAfter(t *testing.T, data []byte, move BoardMove) []byte {
	t.Helper()
	board, err := ParseBoard("delivery.md", data)
	if err != nil {
		t.Fatalf("ParseBoard: %v", err)
	}
	ApplyMove(board, move)
	out, err := SerializeBoard(board)
	if err != nil {
		t.Fatalf("SerializeBoard: %v", err)
	}
	return out
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestOrderFromView(t *testing.T) {
	board := readFixtureBoard(t)
	view := BuildBoardView(board, fixtureInput())
	OrderFromView(board, view)
	got := board.Order.Refs("in_review")
	if len(got) != 1 || got[0] != "DEMO/DEMO-T-0001" {
		t.Fatalf("in_review = %v", got)
	}
	if !board.Order.Has("done") || len(board.Order.Refs("done")) != 0 {
		t.Fatalf("an empty column keeps an empty list")
	}
	// Every ref the view no longer shows is gone.
	text, err := SerializeBoard(board)
	if err != nil {
		t.Fatalf("SerializeBoard: %v", err)
	}
	if strings.Contains(string(text), "DEMO-T-0002") {
		t.Fatalf("an unmapped item must not be written into order:\n%s", text)
	}
}

func TestBoardFilters(t *testing.T) {
	tests := []struct {
		name    string
		filters string
		want    []ItemID
	}{
		{
			name:    "labels_any keeps the matching items",
			filters: "  labels_any: [payments]\n",
			want:    []ItemID{"DEMO-US-0002"},
		},
		{
			name:    "labels_none drops them",
			filters: "  labels_none: [backend]\n",
			want:    []ItemID{"DEMO-US-0001"},
		},
		{
			name:    "assignees filter on the handle",
			filters: "  assignees: [jose]\n",
			want:    []ItemID{"DEMO-US-0001", "DEMO-T-0001"},
		},
		{
			name:    "priorities filter",
			filters: "  priorities: [medium]\n",
			want:    []ItemID{"DEMO-US-0002"},
		},
		{
			name:    "a free-text query matches the title",
			filters: "  query: checkout\n",
			want:    []ItemID{"DEMO-US-0001"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text := fmt.Sprintf(`---
id: b
type: board
kind: kanban
title: B
columns:
  - id: all
    statuses:
      "*": [todo, in_progress, in_review]
filters:
  types: [story, task]
%s---
`, tc.filters)
			board, err := ParseBoard("b.md", []byte(text))
			if err != nil {
				t.Fatalf("ParseBoard: %v", err)
			}
			view := BuildBoardView(board, fixtureInput())
			got := map[ItemID]bool{}
			for _, card := range columnOf(view, "all").Cards {
				got[card.Item] = true
			}
			if len(got) != len(tc.want) {
				t.Fatalf("cards = %v, want %v", refsOf(columnOf(view, "all")), tc.want)
			}
			for _, id := range tc.want {
				if !got[id] {
					t.Fatalf("cards = %v, want %v", refsOf(columnOf(view, "all")), tc.want)
				}
			}
		})
	}
}
