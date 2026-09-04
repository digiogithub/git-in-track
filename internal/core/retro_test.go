package core

import (
	"context"
	"os"
	"strings"
	"testing"
)

const retroFixturePath = "../../testdata/fixtures/team-basic/.pmngr/retros/DEMO-TEAM-R-0001-sprint-1.md"

// readFixtureRetro loads the retro of testdata/fixtures/team-basic.
func readFixtureRetro(t *testing.T) *Retro {
	t.Helper()
	data, err := os.ReadFile(retroFixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	retro, err := ParseRetro(".pmngr/retros/DEMO-TEAM-R-0001-sprint-1.md", data)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return retro
}

func TestParseRetroID(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		key     TeamKey
		number  int
		wantErr bool
	}{
		{name: "simple key", in: "ACME-R-0007", key: "ACME", number: 7},
		{name: "hyphenated key splits from the right", in: "ACME-TEAM-R-0007", key: "ACME-TEAM", number: 7},
		{name: "five digits", in: "DEMO-TEAM-R-10231", key: "DEMO-TEAM", number: 10231},
		{name: "three digits are refused", in: "ACME-R-007", wantErr: true},
		{name: "sprint code is refused", in: "ACME-S-0007", wantErr: true},
		{name: "lower case is refused", in: "acme-R-0007", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, number, err := ParseRetroID(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseRetroID(%q) = %s/%d, want an error", tc.in, key, number)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRetroID(%q): %v", tc.in, err)
			}
			if key != tc.key || number != tc.number {
				t.Fatalf("ParseRetroID(%q) = %s/%d, want %s/%d", tc.in, key, number, tc.key, tc.number)
			}
			if got := FormatRetroID(tc.key, tc.number); got != tc.in {
				t.Fatalf("FormatRetroID = %q, want %q", got, tc.in)
			}
		})
	}
}

func TestParseRetro(t *testing.T) {
	retro := readFixtureRetro(t)
	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "front matter",
			check: func(t *testing.T) {
				if retro.ID != "DEMO-TEAM-R-0001" || retro.Type != "retro" {
					t.Fatalf("id/type = %q/%q", retro.ID, retro.Type)
				}
				if retro.Sprint != "DEMO-TEAM-S-0001" || retro.Board != "demo-scrum" {
					t.Fatalf("sprint/board = %q/%q", retro.Sprint, retro.Board)
				}
				if retro.State != RetroClosed || retro.Date.String() != "2026-09-07" {
					t.Fatalf("state/date = %q/%q", retro.State, retro.Date)
				}
				if retro.VoteBudget() != 3 {
					t.Fatalf("vote budget = %d", retro.VoteBudget())
				}
			},
		},
		{
			name: "notes come out of the body, one bullet per note",
			check: func(t *testing.T) {
				if len(retro.Notes) != 4 {
					t.Fatalf("notes = %d, want 4", len(retro.Notes))
				}
				first := retro.Notes[0]
				if first.ID != "n1" || first.Category != CategoryWentWell || first.Author != "jose" {
					t.Fatalf("first note = %+v", first)
				}
				if !strings.HasPrefix(first.Text, "Pairing on the sandbox") {
					t.Fatalf("first note text = %q", first.Text)
				}
				if got := len(retro.NotesOf(CategoryToImprove)); got != 2 {
					t.Fatalf("to_improve notes = %d, want 2", got)
				}
			},
		},
		{
			name: "themes, votes and actions",
			check: func(t *testing.T) {
				if len(retro.Themes) != 3 || len(retro.Actions) != 3 {
					t.Fatalf("themes/actions = %d/%d", len(retro.Themes), len(retro.Actions))
				}
				if retro.VoteCount("t2") != 2 || retro.VotesCast("jose") != 2 {
					t.Fatalf("votes t2=%d jose=%d", retro.VoteCount("t2"), retro.VotesCast("jose"))
				}
				a1, ok := retro.Action("a1")
				if !ok || a1.Task != "DEMO/DEMO-T-0001" || a1.State() != ActionPromoted {
					t.Fatalf("a1 = %+v (%t)", a1, ok)
				}
				if a2, _ := retro.Action("a2"); !a2.Settled() {
					t.Fatalf("a2 should be settled: %+v", a2)
				}
			},
		},
		{
			name: "an unknown front-matter key is preserved",
			check: func(t *testing.T) {
				parsed, err := ParseRetro("r.md", []byte(
					"---\nid: ACME-R-0001\ntype: retro\ntitle: T\ndate: 2026-01-01\nstate: closed\nmood: 4\n---\n\nbody\n"))
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				if parsed.Extra["mood"] != 4 {
					t.Fatalf("extra = %v", parsed.Extra)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.check)
	}
}

func TestSerializeRetroRoundTrip(t *testing.T) {
	data, err := os.ReadFile(retroFixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	retro, err := ParseRetro(".pmngr/retros/DEMO-TEAM-R-0001-sprint-1.md", data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SerializeRetro(retro)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if string(out) != string(data) {
		t.Fatalf("round trip changed the file:\n--- got ---\n%s\n--- want ---\n%s", out, data)
	}
	again, err := ParseRetro(retro.Path, out)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	twice, err := SerializeRetro(again)
	if err != nil {
		t.Fatalf("reserialize: %v", err)
	}
	if string(twice) != string(out) {
		t.Fatal("the emitter is not idempotent")
	}
}

func TestRetroBodyEdits(t *testing.T) {
	tests := []struct {
		name  string
		check func(t *testing.T, r *Retro)
	}{
		{
			name: "adding a note appends one bullet and nothing else",
			check: func(t *testing.T, r *Retro) {
				before := len(strings.Split(r.Body, "\n"))
				note := r.AddNote(CategoryToImprove, "Staging credentials expired", "marta")
				if note.ID != "n5" {
					t.Fatalf("note id = %q, want n5", note.ID)
				}
				after := strings.Split(r.Body, "\n")
				if len(after) != before+1 {
					t.Fatalf("body grew by %d lines, want 1", len(after)-before)
				}
				if !strings.Contains(r.Body, "- (n5) Staging credentials expired — marta") {
					t.Fatalf("body = %s", r.Body)
				}
			},
		},
		{
			name: "an anonymous retro records no author",
			check: func(t *testing.T, r *Retro) {
				r.Anonymous = true
				r.AddNote(CategoryWentWell, "The deploy was boring", "jose")
				if strings.Contains(r.Body, "The deploy was boring — jose") {
					t.Fatalf("an anonymous retro attributed a note: %s", r.Body)
				}
			},
		},
		{
			name: "a note moves between categories",
			check: func(t *testing.T, r *Retro) {
				if !r.UpdateNote("n4", nil, nil, CategoryToImprove) {
					t.Fatal("UpdateNote reported no such note")
				}
				if got := len(r.NotesOf(CategoryPuzzle)); got != 0 {
					t.Fatalf("puzzles = %d, want 0", got)
				}
				if got := len(r.NotesOf(CategoryToImprove)); got != 3 {
					t.Fatalf("to_improve = %d, want 3", got)
				}
			},
		},
		{
			name: "removing a note unlinks it from its theme",
			check: func(t *testing.T, r *Retro) {
				if !r.RemoveNote("n2") {
					t.Fatal("RemoveNote reported no such note")
				}
				theme, _ := r.Theme("t2")
				if len(theme.Notes) != 1 || theme.Notes[0] != "n3" {
					t.Fatalf("theme notes = %v", theme.Notes)
				}
			},
		},
		{
			name: "an action is mirrored into the checklist",
			check: func(t *testing.T, r *Retro) {
				added := r.AddAction(RetroAction{Title: "Write the runbook", Owner: "jose"})
				if added.ID != "a4" || added.Status != ActionProposed {
					t.Fatalf("action = %+v", added)
				}
				if !strings.Contains(r.Body, "- [ ] a4 — Write the runbook (jose)") {
					t.Fatalf("body = %s", r.Body)
				}
				if !r.RemoveAction("a4") || strings.Contains(r.Body, "a4") {
					t.Fatalf("a4 was not removed: %s", r.Body)
				}
			},
		},
		{
			name: "a section the file does not have is created in canonical order",
			check: func(t *testing.T, r *Retro) {
				fresh := &Retro{ID: "ACME-R-0001", Type: "retro", Title: "T"}
				fresh.AddNote(CategoryPuzzle, "Why is CI slow?", "jose")
				fresh.AddNote(CategoryWentWell, "Green build", "marta")
				want := "## Went well\n\n- (n2) Green build — marta\n\n## Puzzles\n\n- (n1) Why is CI slow? — jose"
				if fresh.Body != want {
					t.Fatalf("body =\n%s\nwant\n%s", fresh.Body, want)
				}
				_ = r
			},
		},
		{
			name: "an unowned section is preserved verbatim",
			check: func(t *testing.T, r *Retro) {
				r.AddNote(CategoryWentWell, "One more", "jose")
				if !strings.Contains(r.Body, "## Discussion\n\nt2 (2 votes) dominated") {
					t.Fatalf("the discussion was rewritten: %s", r.Body)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, readFixtureRetro(t))
		})
	}
}

func TestRetroValidate(t *testing.T) {
	base := func() *Retro {
		return &Retro{
			ID: "DEMO-TEAM-R-0002", Type: "retro", Title: "T",
			Path:  ".pmngr/retros/DEMO-TEAM-R-0002.md",
			Date:  mustDate(t, "2026-09-20"),
			State: RetroClosed, Participants: []string{"jose"},
		}
	}
	in := RetroValidateInput{TeamKey: "DEMO-TEAM", Sprints: []string{"DEMO-TEAM-S-0001"}}

	tests := []struct {
		name  string
		build func(r *Retro)
		want  Code
	}{
		{name: "clean", build: func(*Retro) {}},
		{name: "id must match the file name prefix", build: func(r *Retro) { r.ID = "DEMO-TEAM-R-0009" }, want: CodeRetroID},
		{name: "id must carry the team key", build: func(r *Retro) {
			r.ID, r.Path = "OTHER-R-0002", ".pmngr/retros/OTHER-R-0002.md"
		}, want: CodeRetroID},
		{name: "date is required", build: func(r *Retro) { r.Date = Date{} }, want: CodeRetroDate},
		{name: "state is an enum", build: func(r *Retro) { r.State = "gathering" }, want: CodeRetroState},
		{name: "a vote names a known theme", build: func(r *Retro) {
			r.Votes = map[string][]string{"t9": {"jose"}}
		}, want: CodeRetroVoteTheme},
		{name: "a voter is a participant", build: func(r *Retro) {
			r.Themes = []RetroTheme{{ID: "t1", Title: "T"}}
			r.Votes = map[string][]string{"t1": {"nobody"}}
		}, want: CodeRetroVoteNonParticipant},
		{name: "the vote budget is enforced", build: func(r *Retro) {
			r.Themes = []RetroTheme{{ID: "t1", Title: "A"}, {ID: "t2", Title: "B"}}
			budget := 1
			r.VotesPerPerson = &budget
			r.Votes = map[string][]string{"t1": {"jose"}, "t2": {"jose"}}
		}, want: CodeRetroVoteBudget},
		{name: "duplicate action ids", build: func(r *Retro) {
			r.Actions = []RetroAction{{ID: "a1", Title: "A", Owner: "jose"}, {ID: "a1", Title: "B", Owner: "jose"}}
		}, want: CodeRetroActionIDDup},
		{name: "an action needs an owner", build: func(r *Retro) {
			r.Actions = []RetroAction{{ID: "a1", Title: "A"}}
		}, want: CodeRetroActionNoOwner},
		{name: "a promoted task must resolve", build: func(r *Retro) {
			r.Actions = []RetroAction{{ID: "a1", Title: "A", Owner: "jose", Task: "DEMO/DEMO-T-9999"}}
		}, want: CodeRetroActionTaskDead},
		{name: "a dead sprint reference", build: func(r *Retro) { r.Sprint = "DEMO-TEAM-S-0099" }, want: CodeRetroSprintDead},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			retro := base()
			tc.build(retro)
			scoped := in
			scoped.Declared = []ProjectKey{"DEMO"}
			scoped.Cloned = map[ProjectKey]bool{"DEMO": true}
			scoped.Known = map[string]bool{"DEMO/DEMO-T-0001": true}
			diags := retro.Validate(scoped)
			if tc.want == "" {
				if len(diags) != 0 {
					t.Fatalf("diagnostics = %v, want none", diags)
				}
				return
			}
			for _, d := range diags {
				if d.Code == tc.want {
					return
				}
			}
			t.Fatalf("diagnostics = %v, want %s", diags, tc.want)
		})
	}
}

func TestRetroStore(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T) *RetroStore {
		t.Helper()
		data, err := os.ReadFile(retroFixturePath)
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		fsys := NewMemFSFromMap(map[string]string{
			".pmngr/retros/DEMO-TEAM-R-0001-sprint-1.md": string(data),
		})
		return NewRetroStore(fsys, ".pmngr")
	}

	tests := []struct {
		name  string
		check func(t *testing.T, store *RetroStore)
	}{
		{
			name: "get finds the file by its id prefix, past the slug",
			check: func(t *testing.T, store *RetroStore) {
				retro, err := store.Get(ctx, "DEMO-TEAM-R-0001")
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				if retro.Path != ".pmngr/retros/DEMO-TEAM-R-0001-sprint-1.md" {
					t.Fatalf("path = %q", retro.Path)
				}
			},
		},
		{
			name: "list reports one retro and no diagnostics",
			check: func(t *testing.T, store *RetroStore) {
				retros, diags, err := store.List(ctx)
				if err != nil || len(retros) != 1 || len(diags) != 0 {
					t.Fatalf("List = %d, %v, %v", len(retros), diags, err)
				}
			},
		},
		{
			name: "next id follows the highest number on disk",
			check: func(t *testing.T, store *RetroStore) {
				id, err := store.NextID(ctx, "DEMO-TEAM")
				if err != nil || id != "DEMO-TEAM-R-0002" {
					t.Fatalf("NextID = %q, %v", id, err)
				}
			},
		},
		{
			name: "a write under a stale revision is refused",
			check: func(t *testing.T, store *RetroStore) {
				retro, err := store.Get(ctx, "DEMO-TEAM-R-0001")
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				if _, err := store.Write(ctx, retro, "sha256:0000000000000000"); err == nil {
					t.Fatal("a stale write was accepted")
				}
				retro.AddNote(CategoryWentWell, "Late note", "jose")
				written, err := store.Write(ctx, retro, retro.Rev)
				if err != nil {
					t.Fatalf("Write: %v", err)
				}
				if !strings.Contains(written.Body, "Late note") {
					t.Fatalf("body = %s", written.Body)
				}
				back, err := store.Get(ctx, "DEMO-TEAM-R-0001")
				if err != nil || len(back.Notes) != 5 {
					t.Fatalf("read back %d notes: %v", len(back.Notes), err)
				}
			},
		},
		{
			name: "a new retro is stored under its id and slug",
			check: func(t *testing.T, store *RetroStore) {
				if got := store.PathOf("DEMO-TEAM-R-0002", "Sprint 2 Retrospective"); got !=
					".pmngr/retros/DEMO-TEAM-R-0002-sprint-2-retrospective.md" {
					t.Fatalf("PathOf = %q", got)
				}
				if got := store.PathOf("DEMO-TEAM-R-0002", ""); got != ".pmngr/retros/DEMO-TEAM-R-0002.md" {
					t.Fatalf("PathOf = %q", got)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.check(t, seed(t)) })
	}
}
