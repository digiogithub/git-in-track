package core

import (
	"strings"
	"testing"
	"time"
)

// retroInput is the fixture workspace a retro renders against: DEMO cloned,
// WEB resolved from its committed snapshot.
func retroInput(t *testing.T) RetroInput {
	t.Helper()
	in := snapshotInput(t)
	in.Now = time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	return RetroInput{Board: in}
}

func TestBuildRetroView(t *testing.T) {
	retro := readFixtureRetro(t)
	view := BuildRetroView(retro, retroInput(t))

	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "themes are ranked by the votes they got",
			check: func(t *testing.T) {
				if len(view.Themes) != 3 {
					t.Fatalf("themes = %d", len(view.Themes))
				}
				if view.Themes[0].ID != "t2" || view.Themes[0].Votes != 2 {
					t.Fatalf("top theme = %+v", view.Themes[0])
				}
				if len(view.Themes[0].NoteTexts) != 2 {
					t.Fatalf("t2 grouped %d notes, want 2", len(view.Themes[0].NoteTexts))
				}
				if len(view.Themes[0].Actions) != 2 {
					t.Fatalf("t2 produced %v", view.Themes[0].Actions)
				}
			},
		},
		{
			name: "a promoted action shows the live card of its task",
			check: func(t *testing.T) {
				action := actionOf(t, view.Actions, "a1")
				if action.Card == nil || action.Card.Title != "Add address validation" {
					t.Fatalf("a1 card = %+v", action.Card)
				}
				if action.Card.Source != CardSourceLive {
					t.Fatalf("a1 card source = %q", action.Card.Source)
				}
				// `in_review` is not terminal, so the action is still open even
				// though the retro file calls it `promoted` (R-RETRO-1).
				if action.Done || !action.Open {
					t.Fatalf("a1 = done %v open %v", action.Done, action.Open)
				}
			},
		},
		{
			name: "an action promoted into a project nobody cloned reads from the snapshot",
			check: func(t *testing.T) {
				action := actionOf(t, view.Actions, "a3")
				if action.Card == nil || !action.Card.Remote || action.Card.Source != CardSourceSnapshot {
					t.Fatalf("a3 card = %+v", action.Card)
				}
				if !action.Open {
					t.Fatal("a3 is open: WEB-T-0007 is `doing`")
				}
			},
		},
		{
			name: "an action that was never promoted falls back to its own status",
			check: func(t *testing.T) {
				action := actionOf(t, view.Actions, "a2")
				if action.Card != nil {
					t.Fatalf("a2 has no task: %+v", action.Card)
				}
				if !action.Done || action.Open {
					t.Fatalf("a2 = done %v open %v", action.Done, action.Open)
				}
			},
		},
		{
			name: "the summary counts the follow-through",
			check: func(t *testing.T) {
				m := view.Retro.Metrics
				if m.Actions != 3 || m.Promoted != 2 || m.Done != 1 || m.Open != 2 || m.NoOwner != 0 {
					t.Fatalf("metrics = %+v", m)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.check)
	}
}

func TestCarriedActions(t *testing.T) {
	previous := readFixtureRetro(t)
	in := retroInput(t).Board

	tests := []struct {
		name  string
		next  *Retro
		want  []string
		count int
	}{
		{
			name:  "a new retro carries the open actions of the one before it",
			next:  &Retro{ID: "DEMO-TEAM-R-0002", CarriedFrom: "DEMO-TEAM-R-0001"},
			want:  []string{"a1", "a3"},
			count: 2,
		},
		{
			name:  "without carried_from every earlier retro is reviewed",
			next:  &Retro{ID: "DEMO-TEAM-R-0002"},
			want:  []string{"a1", "a3"},
			count: 2,
		},
		{
			name:  "a carried_from that names another retro carries nothing",
			next:  &Retro{ID: "DEMO-TEAM-R-0002", CarriedFrom: "DEMO-TEAM-R-0009"},
			count: 0,
		},
		{
			name:  "a retro never carries its own actions",
			next:  previous,
			count: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CarriedActions(tc.next, []*Retro{previous}, in)
			if len(got) != tc.count {
				t.Fatalf("carried %d actions, want %d", len(got), tc.count)
			}
			for i, id := range tc.want {
				if got[i].ID != id {
					t.Fatalf("carried[%d] = %q, want %q", i, got[i].ID, id)
				}
				if got[i].Retro != "DEMO-TEAM-R-0001" {
					t.Fatalf("carried[%d] came from %q", i, got[i].Retro)
				}
			}
		})
	}
}

func TestResolveCard(t *testing.T) {
	in := snapshotInput(t)
	tests := []struct {
		name   string
		ref    string
		want   func(t *testing.T, card BoardCard)
		reason bool
	}{
		{
			name: "a cloned project resolves live",
			ref:  "DEMO/DEMO-T-0001",
			want: func(t *testing.T, card BoardCard) {
				if card.Source != CardSourceLive || card.Remote || card.Title == "" {
					t.Fatalf("card = %+v", card)
				}
			},
		},
		{
			name:   "a project nobody cloned resolves from the snapshot",
			ref:    "WEB/WEB-T-0007",
			reason: true,
			want: func(t *testing.T, card BoardCard) {
				if card.Source != CardSourceSnapshot || !card.Remote {
					t.Fatalf("card = %+v", card)
				}
			},
		},
		{
			name:   "an item the clone does not hold carries the reason",
			ref:    "DEMO/DEMO-T-9999",
			reason: true,
			want: func(t *testing.T, card BoardCard) {
				if card.Status != "" {
					t.Fatalf("card = %+v", card)
				}
			},
		},
		{
			name:   "a malformed reference carries the reason",
			ref:    "nonsense",
			reason: true,
			want:   func(*testing.T, BoardCard) {},
		},
		{
			name:   "an undeclared project carries the reason",
			ref:    "OTHER/OTHER-T-0001",
			reason: true,
			want: func(t *testing.T, card BoardCard) {
				if card.Declared {
					t.Fatalf("card = %+v", card)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			card := ResolveCard(in, tc.ref)
			if tc.reason != (card.Reason != "") {
				t.Fatalf("reason = %q, want a reason: %v", card.Reason, tc.reason)
			}
			tc.want(t, card)
		})
	}
}

func TestPlanPromotion(t *testing.T) {
	retro := readFixtureRetro(t)
	action, _ := retro.Action("a2")
	plan := PlanPromotion(retro, *action, "DEMO", nil)

	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "the task carries the action's owner, due date and the retro label",
			check: func(t *testing.T) {
				if plan.Draft.Type != TypeTask || plan.Project != "DEMO" {
					t.Fatalf("plan = %+v", plan)
				}
				if len(plan.Draft.Assignees) != 1 || plan.Draft.Assignees[0] != "marta" {
					t.Fatalf("assignees = %v", plan.Draft.Assignees)
				}
				if plan.Draft.Due.String() != "2026-09-07" {
					t.Fatalf("due = %q", plan.Draft.Due)
				}
				if len(plan.Draft.Labels) != 1 || plan.Draft.Labels[0] != RetroTaskLabel {
					t.Fatalf("labels = %v", plan.Draft.Labels)
				}
				if plan.Draft.Author != "marta" {
					t.Fatalf("author = %q", plan.Draft.Author)
				}
			},
		},
		{
			name: "the body links back to the retro in plain text",
			check: func(t *testing.T) {
				want := "Promoted from retro DEMO-TEAM-R-0001 (action a2)."
				if !strings.Contains(plan.Draft.Body, want) {
					t.Fatalf("body = %q", plan.Draft.Body)
				}
				if !strings.Contains(plan.Draft.Body, action.Note) {
					t.Fatalf("body dropped the action note: %q", plan.Draft.Body)
				}
			},
		},
		{
			name: "the label set is configurable",
			check: func(t *testing.T) {
				custom := PlanPromotion(retro, *action, "DEMO", []string{"process"})
				if len(custom.Draft.Labels) != 1 || custom.Draft.Labels[0] != "process" {
					t.Fatalf("labels = %v", custom.Draft.Labels)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.check)
	}
}

// actionOf finds one rendered action by its id.
func actionOf(t *testing.T, actions []RetroActionView, id string) RetroActionView {
	t.Helper()
	for _, action := range actions {
		if action.ID == id {
			return action
		}
	}
	t.Fatalf("no action %q in %d actions", id, len(actions))
	return RetroActionView{}
}
