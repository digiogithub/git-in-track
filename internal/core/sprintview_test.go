package core

import (
	"testing"
	"time"
)

// scrumInput is the fixture workspace a scrum board renders against: DEMO
// cloned, WEB resolved from its committed snapshot, and the fixture sprint.
func scrumInput(t *testing.T) BoardInput {
	t.Helper()
	in := snapshotInput(t)
	in.Sprint = readFixtureSprint(t)
	in.Now = time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	return in
}

func TestBuildBoardViewScopedToSprint(t *testing.T) {
	board := readScrumBoard(t)
	view := BuildBoardView(board, scrumInput(t))

	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "the working columns hold the sprint scope only",
			check: func(t *testing.T) {
				card, ok := cardOfRef(view, "DEMO/DEMO-US-0001")
				if !ok || !card.InSprint || card.Backlog {
					t.Fatalf("the cloned sprint card = %+v (found %v)", card, ok)
				}
				if got := refsOf(columnOf(view, "in_progress")); len(got) != 1 || got[0] != "DEMO/DEMO-US-0001" {
					t.Fatalf("in_progress = %v", got)
				}
				if got := refsOf(columnOf(view, "in_review")); len(got) != 1 || got[0] != "DEMO/DEMO-T-0001" {
					t.Fatalf("in_review = %v", got)
				}
			},
		},
		{
			name: "a remote sprint item comes from the committed snapshot",
			check: func(t *testing.T) {
				card, ok := cardOfRef(view, "WEB/WEB-US-0031")
				if !ok {
					t.Fatal("the remote sprint card is missing")
				}
				if !card.Remote || card.Source != CardSourceSnapshot || card.Title == "" {
					t.Fatalf("remote card = %+v", card)
				}
				if !card.InSprint || !card.Committed {
					t.Fatalf("the remote card is part of the commitment: %+v", card)
				}
			},
		},
		{
			name: "the backlog column offers the candidates the sprint does not list",
			check: func(t *testing.T) {
				// The fixture board's backlog column doubles as its "todo"
				// column, so it holds both a candidate and the sprint item
				// whose status maps there (docs/04 section 5.9).
				cards := columnOf(view, "sprint_backlog").Cards
				byRef := map[string]BoardCard{}
				for _, card := range cards {
					byRef[card.Ref] = card
				}
				if len(cards) != 2 {
					t.Fatalf("sprint_backlog = %v", refsOf(columnOf(view, "sprint_backlog")))
				}
				candidate := byRef["DEMO/DEMO-US-0002"]
				if !candidate.Backlog || candidate.InSprint {
					t.Fatalf("candidate = %+v", candidate)
				}
				member := byRef["WEB/WEB-US-0031"]
				if member.Backlog || !member.InSprint {
					t.Fatalf("sprint item in the backlog column = %+v", member)
				}
				// A finished item and one whose status the column does not
				// claim are never offered as candidates.
				if _, offered := byRef["DEMO/DEMO-T-0002"]; offered {
					t.Fatal("an item whose status the backlog column does not claim is not a candidate")
				}
			},
		},
		{
			name: "the header carries the goal, the days and the points",
			check: func(t *testing.T) {
				info := view.SprintInfo
				if info == nil {
					t.Fatal("a scrum board carries its sprint header")
				}
				if info.ID != "DEMO-TEAM-S-0001" || info.Goal == "" || info.State != SprintActive {
					t.Fatalf("header = %+v", info)
				}
				if info.TotalDays != 14 || info.RemainingDays != 5 {
					t.Fatalf("days = %d total, %d remaining", info.TotalDays, info.RemainingDays)
				}
				// DEMO-US-0001 is 8 points and committed, WEB-US-0031 is 5 and
				// committed, DEMO-T-0001 was added mid-sprint with no estimate.
				if info.Metrics.Items != 3 || info.Metrics.Resolved != 3 || info.Metrics.Added != 1 {
					t.Fatalf("metrics = %+v", info.Metrics)
				}
				if info.Metrics.Points != 13 || info.Metrics.CommittedPoints != 13 {
					t.Fatalf("points = %+v", info.Metrics)
				}
				if info.Metrics.Done != 0 || info.Metrics.DonePoints != 0 {
					t.Fatalf("nothing is done yet: %+v", info.Metrics)
				}
			},
		},
		{
			name: "a kanban board is never scoped",
			check: func(t *testing.T) {
				kanban := BuildBoardView(readFixtureBoard(t), scrumInput(t))
				if kanban.SprintInfo != nil {
					t.Fatalf("a kanban board carries no sprint header: %+v", kanban.SprintInfo)
				}
				if _, ok := cardOfRef(kanban, "DEMO/DEMO-US-0002"); !ok {
					t.Fatal("a kanban board still shows everything the filters match")
				}
			},
		},
		{
			name: "a ref the sprint dropped never comes back as a placeholder",
			check: func(t *testing.T) {
				scoped := readScrumBoard(t)
				scoped.OrderList().Set("in_progress", []string{"WEB/WEB-T-0007", "DEMO/DEMO-US-0001"})
				out := BuildBoardView(scoped, scrumInput(t))
				if _, ok := cardOfRef(out, "WEB/WEB-T-0007"); ok {
					t.Fatal("the order list must not resurrect a card the sprint does not list")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.check)
	}
}

func TestBuildSprintView(t *testing.T) {
	sprint := readFixtureSprint(t)
	board := readScrumBoard(t)
	view := BuildSprintView(sprint, board, scrumInput(t))

	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "the scope keeps the order of the file",
			check: func(t *testing.T) {
				if len(view.Cards) != 3 {
					t.Fatalf("cards = %d", len(view.Cards))
				}
				want := []string{"DEMO/DEMO-US-0001", "DEMO/DEMO-T-0001", "WEB/WEB-US-0031"}
				for i, ref := range want {
					if view.Cards[i].Ref != ref {
						t.Fatalf("card %d = %s, want %s", i, view.Cards[i].Ref, ref)
					}
					if !view.Cards[i].InSprint {
						t.Fatalf("card %d is in the sprint: %+v", i, view.Cards[i])
					}
				}
			},
		},
		{
			name: "candidates are the items the board shows that the sprint does not",
			check: func(t *testing.T) {
				if len(view.Backlog) != 1 || view.Backlog[0].Ref != "DEMO/DEMO-US-0002" {
					t.Fatalf("backlog = %+v", view.Backlog)
				}
				if !view.Backlog[0].Backlog {
					t.Fatalf("a candidate is flagged: %+v", view.Backlog[0])
				}
			},
		},
		{
			name: "a reference nothing resolves still gets a card and a reason",
			check: func(t *testing.T) {
				broken := readFixtureSprint(t)
				broken.Items = append(broken.Items, "DEMO/DEMO-US-9999")
				out := BuildSprintView(broken, board, scrumInput(t))
				last := out.Cards[len(out.Cards)-1]
				if last.Ref != "DEMO/DEMO-US-9999" || last.Reason == "" {
					t.Fatalf("card = %+v", last)
				}
				found := false
				for _, d := range out.Diagnostics {
					if d.Code == CodeSprintRefDead {
						found = true
					}
				}
				if !found {
					t.Fatalf("diagnostics = %v", out.Diagnostics)
				}
				if out.Sprint.Metrics.Unresolved != 1 {
					t.Fatalf("metrics = %+v", out.Sprint.Metrics)
				}
			},
		},
		{
			name: "a sprint with no board still lists its scope",
			check: func(t *testing.T) {
				out := BuildSprintView(sprint, nil, scrumInput(t))
				if len(out.Cards) != 3 || len(out.Backlog) != 0 {
					t.Fatalf("cards = %d, backlog = %d", len(out.Cards), len(out.Backlog))
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.check)
	}
}

func TestSummarizeClose(t *testing.T) {
	sprint := readFixtureSprint(t)
	board := readScrumBoard(t)
	in := scrumInput(t)

	tests := []struct {
		name  string
		build func(*BoardInput)
		done  int
		open  int
	}{
		{
			name:  "nothing finished yet",
			build: func(*BoardInput) {},
			done:  0,
			open:  3,
		},
		{
			name: "a finished item is reported as completed",
			build: func(in *BoardInput) {
				items := demoItems()
				for i := range items {
					if items[i].ID == "DEMO-US-0001" {
						items[i].Status = "done"
					}
				}
				in.Sources = []BoardSource{{Project: "DEMO", VaultID: "demo", Config: demoConfig(), Items: items}}
			},
			done: 1,
			open: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := in
			tc.build(&input)
			report := SummarizeClose(sprint, BuildSprintView(sprint, board, input))
			if len(report.Completed) != tc.done || len(report.Incomplete) != tc.open {
				t.Fatalf("report = %d done, %d open (want %d/%d)",
					len(report.Completed), len(report.Incomplete), tc.done, tc.open)
			}
			if report.Sprint != sprint.ID || report.Board != sprint.Board {
				t.Fatalf("report = %+v", report)
			}
		})
	}
}

func TestPlanMoveInSprint(t *testing.T) {
	board := readScrumBoard(t)
	in := scrumInput(t)
	view := BuildBoardView(board, in)

	tests := []struct {
		name      string
		ref       string
		toColumn  string
		wantAdd   bool
		wantErr   bool
		wantWrite bool
	}{
		{
			name: "dragging a candidate out of the backlog commits it to the sprint",
			ref:  "DEMO/DEMO-US-0002", toColumn: "in_progress",
			wantAdd: true, wantWrite: true,
		},
		{
			name: "moving a sprint card between columns changes no membership",
			ref:  "DEMO/DEMO-US-0001", toColumn: "in_review",
			wantAdd: false, wantWrite: true,
		},
		{
			name: "dropping a sprint card into the backlog column keeps it in the sprint",
			ref:  "DEMO/DEMO-US-0001", toColumn: "sprint_backlog",
			wantAdd: false, wantWrite: true,
		},
		{
			name: "a remote sprint card still refuses to change status",
			ref:  "WEB/WEB-US-0031", toColumn: "in_progress",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := ParseRef(tc.ref)
			if err != nil {
				t.Fatalf("ParseRef: %v", err)
			}
			plan, err := PlanMoveInSprint(board, view, BoardMove{Ref: ref, ToColumn: tc.toColumn, Position: 0},
				demoConfig(), in.Sprint)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("plan = %+v, want an error", plan)
				}
				return
			}
			if err != nil {
				t.Fatalf("PlanMoveInSprint: %v", err)
			}
			if plan.SprintAdd != tc.wantAdd {
				t.Fatalf("SprintAdd = %v, want %v", plan.SprintAdd, tc.wantAdd)
			}
			if plan.Sprint != "DEMO-TEAM-S-0001" {
				t.Fatalf("Sprint = %q", plan.Sprint)
			}
			if plan.StatusChanged != tc.wantWrite {
				t.Fatalf("StatusChanged = %v, want %v", plan.StatusChanged, tc.wantWrite)
			}
		})
	}
}
