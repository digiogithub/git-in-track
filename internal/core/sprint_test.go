package core

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// readFixtureSprint loads the sprint of testdata/fixtures/team-basic.
func readFixtureSprint(t *testing.T) *Sprint {
	t.Helper()
	const p = "../../testdata/fixtures/team-basic/.pmngr/sprints/DEMO-TEAM-S-0001.md"
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	sprint, err := ParseSprint(".pmngr/sprints/DEMO-TEAM-S-0001.md", data)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return sprint
}

// readScrumBoard loads the scrum board of testdata/fixtures/team-basic.
func readScrumBoard(t *testing.T) *Board {
	t.Helper()
	const p = "../../testdata/fixtures/team-basic/.pmngr/boards/demo-scrum.md"
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	board, err := ParseBoard(".pmngr/boards/demo-scrum.md", data)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return board
}

func TestParseSprintID(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		key     TeamKey
		number  int
		wantErr bool
	}{
		{name: "simple key", in: "ACME-S-0007", key: "ACME", number: 7},
		{name: "hyphenated key splits from the right", in: "ACME-TEAM-S-0007", key: "ACME-TEAM", number: 7},
		{name: "five digits", in: "DEMO-TEAM-S-10231", key: "DEMO-TEAM", number: 10231},
		{name: "three digits are refused", in: "ACME-S-007", wantErr: true},
		{name: "retro code is refused", in: "ACME-R-0007", wantErr: true},
		{name: "lower case is refused", in: "acme-S-0007", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, number, err := ParseSprintID(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseSprintID(%q) = %s/%d, want an error", tc.in, key, number)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSprintID(%q): %v", tc.in, err)
			}
			if key != tc.key || number != tc.number {
				t.Fatalf("ParseSprintID(%q) = %s/%d, want %s/%d", tc.in, key, number, tc.key, tc.number)
			}
			if got := FormatSprintID(tc.key, tc.number); got != tc.in && tc.number >= 1000 {
				t.Fatalf("FormatSprintID round trip = %q, want %q", got, tc.in)
			}
		})
	}
}

func TestParseSprint(t *testing.T) {
	sprint := readFixtureSprint(t)

	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "front matter is decoded",
			check: func(t *testing.T) {
				if sprint.ID != "DEMO-TEAM-S-0001" || sprint.Board != "demo-scrum" {
					t.Fatalf("sprint = %+v", sprint)
				}
				if sprint.State != SprintActive {
					t.Fatalf("state = %q, want active", sprint.State)
				}
				if sprint.Start.String() != "2026-08-24" || sprint.End.String() != "2026-09-06" {
					t.Fatalf("dates = %s to %s", sprint.Start, sprint.End)
				}
				if sprint.Goal == "" || sprint.CapacityHours == nil || *sprint.CapacityHours != 180 {
					t.Fatalf("goal/capacity = %q/%v", sprint.Goal, sprint.CapacityHours)
				}
			},
		},
		{
			name: "the scope keeps its order and tells commitment from additions",
			check: func(t *testing.T) {
				want := []string{"DEMO/DEMO-US-0001", "DEMO/DEMO-T-0001", "WEB/WEB-US-0031"}
				if strings.Join(sprint.Items, ",") != strings.Join(want, ",") {
					t.Fatalf("items = %v, want %v", sprint.Items, want)
				}
				if len(sprint.Committed) != 2 || containsString(sprint.Committed, "DEMO/DEMO-T-0001") {
					t.Fatalf("committed = %v", sprint.Committed)
				}
			},
		},
		{
			name: "the body is kept",
			check: func(t *testing.T) {
				if !strings.Contains(sprint.Body, "## Goal") {
					t.Fatalf("body = %q", sprint.Body)
				}
			},
		},
		{
			name: "days are counted with both ends inclusive",
			check: func(t *testing.T) {
				if got := sprint.TotalDays(); got != 14 {
					t.Fatalf("TotalDays = %d, want 14", got)
				}
				day := func(d int) time.Time { return time.Date(2026, 9, d, 9, 0, 0, 0, time.UTC) }
				if got := sprint.RemainingDays(day(2)); got != 5 {
					t.Fatalf("RemainingDays(Sep 2) = %d, want 5", got)
				}
				if got := sprint.RemainingDays(day(6)); got != 1 {
					t.Fatalf("RemainingDays(the last day) = %d, want 1", got)
				}
				if got := sprint.RemainingDays(day(9)); got != 0 {
					t.Fatalf("RemainingDays(after the end) = %d, want 0", got)
				}
			},
		},
		{
			name: "the default title is the sprint number",
			check: func(t *testing.T) {
				bare := &Sprint{ID: "DEMO-TEAM-S-0009"}
				if got := bare.DisplayTitle(); got != "Sprint 9" {
					t.Fatalf("DisplayTitle = %q, want Sprint 9", got)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.check)
	}
}

func TestSerializeSprint(t *testing.T) {
	sprint := readFixtureSprint(t)

	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "serializing a parsed sprint is idempotent",
			check: func(t *testing.T) {
				data, err := SerializeSprint(sprint)
				if err != nil {
					t.Fatalf("SerializeSprint: %v", err)
				}
				again, err := ParseSprint(sprint.Path, data)
				if err != nil {
					t.Fatalf("reparse: %v", err)
				}
				second, err := SerializeSprint(again)
				if err != nil {
					t.Fatalf("SerializeSprint again: %v", err)
				}
				if string(data) != string(second) {
					t.Fatalf("emission is not idempotent:\n%s\n---\n%s", data, second)
				}
			},
		},
		{
			name: "references are emitted one per line",
			check: func(t *testing.T) {
				data, err := SerializeSprint(sprint)
				if err != nil {
					t.Fatalf("SerializeSprint: %v", err)
				}
				if !strings.Contains(string(data), "items:\n  - DEMO/DEMO-US-0001\n") {
					t.Fatalf("items are not one per line:\n%s", data)
				}
			},
		},
		{
			name: "an empty scope is emitted explicitly",
			check: func(t *testing.T) {
				data, err := SerializeSprint(&Sprint{ID: "DEMO-TEAM-S-0002", Type: "sprint",
					Board: "demo-scrum", State: SprintPlanned})
				if err != nil {
					t.Fatalf("SerializeSprint: %v", err)
				}
				if !strings.Contains(string(data), "items: []") {
					t.Fatalf("an empty scope must be explicit:\n%s", data)
				}
			},
		},
		{
			name: "an unknown key survives a round trip",
			check: func(t *testing.T) {
				withExtra := readFixtureSprint(t)
				withExtra.Extra = map[string]any{"cadence_note": "two weeks"}
				data, err := SerializeSprint(withExtra)
				if err != nil {
					t.Fatalf("SerializeSprint: %v", err)
				}
				if !strings.Contains(string(data), "cadence_note: two weeks") {
					t.Fatalf("the unknown key was dropped:\n%s", data)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.check)
	}
}

func TestSprintValidate(t *testing.T) {
	base := func() *Sprint {
		s := readFixtureSprint(t)
		return s
	}
	in := SprintValidateInput{
		TeamKey: "DEMO-TEAM", Boards: []string{"delivery", "demo-scrum"},
		Declared: []ProjectKey{"DEMO", "WEB"},
		Cloned:   map[ProjectKey]bool{"DEMO": true},
		Known:    map[string]bool{"DEMO/DEMO-US-0001": true, "DEMO/DEMO-T-0001": true},
	}

	tests := []struct {
		name  string
		build func(*Sprint)
		want  Code
	}{
		{name: "the fixture is clean", build: func(*Sprint) {}},
		{name: "id must match the file name", build: func(s *Sprint) { s.ID = "DEMO-TEAM-S-0002" }, want: CodeSprintID},
		{name: "id must carry the team key", build: func(s *Sprint) {
			s.ID = "OTHER-S-0001"
			s.Path = ".pmngr/sprints/OTHER-S-0001.md"
		}, want: CodeSprintID},
		{name: "the end date cannot precede the start", build: func(s *Sprint) {
			s.End = s.Start
			s.Start, _ = ParseDate("2026-09-30")
		}, want: CodeSprintDates},
		{name: "a missing start is an error", build: func(s *Sprint) { s.Start = Date{} }, want: CodeSprintDates},
		{name: "the board must exist", build: func(s *Sprint) { s.Board = "nowhere" }, want: CodeSprintBoard},
		{name: "the state is an enum", build: func(s *Sprint) { s.State = "running" }, want: CodeSprintState},
		{name: "an undeclared project is a warning", build: func(s *Sprint) {
			s.Items = append(s.Items, "MOB/MOB-T-0001")
		}, want: CodeSprintRefUnknownProject},
		{name: "a dead ref into a clone is a warning", build: func(s *Sprint) {
			s.Items = append(s.Items, "DEMO/DEMO-US-9999")
		}, want: CodeSprintRefDead},
		{name: "a malformed ref is a warning", build: func(s *Sprint) {
			s.Items = append(s.Items, "DEMO-US-0003")
		}, want: CodeBoardRefFormat},
		{name: "a ref into a project nobody cloned is never dead", build: func(s *Sprint) {
			s.Items = append(s.Items, "WEB/WEB-US-0099")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			tc.build(s)
			diags := s.Validate(in)
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

func TestValidateSprintSet(t *testing.T) {
	date := func(s string) Date {
		d, err := ParseDate(s)
		if err != nil {
			t.Fatalf("ParseDate: %v", err)
		}
		return d
	}
	first := &Sprint{ID: "DEMO-TEAM-S-0001", Board: "demo-scrum", State: SprintActive,
		Start: date("2026-08-24"), End: date("2026-09-06")}

	tests := []struct {
		name  string
		other *Sprint
		want  Code
	}{
		{
			name: "a following sprint is clean",
			other: &Sprint{ID: "DEMO-TEAM-S-0002", Board: "demo-scrum", State: SprintPlanned,
				Start: date("2026-09-07"), End: date("2026-09-20")},
		},
		{
			name: "overlapping dates are reported",
			other: &Sprint{ID: "DEMO-TEAM-S-0002", Board: "demo-scrum", State: SprintPlanned,
				Start: date("2026-09-01"), End: date("2026-09-14")},
			want: CodeSprintOverlap,
		},
		{
			name: "two active sprints on one board are reported",
			other: &Sprint{ID: "DEMO-TEAM-S-0002", Board: "demo-scrum", State: SprintActive,
				Start: date("2026-09-07"), End: date("2026-09-20")},
			want: CodeSprintTwoActive,
		},
		{
			name: "another board never overlaps",
			other: &Sprint{ID: "DEMO-TEAM-S-0002", Board: "delivery", State: SprintActive,
				Start: date("2026-09-01"), End: date("2026-09-14")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags := ValidateSprintSet([]*Sprint{first, tc.other})
			if tc.want == "" {
				if len(diags) != 0 {
					t.Fatalf("diagnostics = %v, want none", diags)
				}
				return
			}
			found := false
			for _, d := range diags {
				if d.Code == tc.want {
					found = true
					if !strings.Contains(d.Message, "demo-scrum") {
						t.Errorf("the message must name the board: %q", d.Message)
					}
				}
			}
			if !found {
				t.Fatalf("diagnostics = %v, want %s", diags, tc.want)
			}
		})
	}
}

func TestSprintStore(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T) *SprintStore {
		t.Helper()
		data, err := os.ReadFile("../../testdata/fixtures/team-basic/.pmngr/sprints/DEMO-TEAM-S-0001.md")
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		fsys := NewMemFSFromMap(map[string]string{".pmngr/sprints/DEMO-TEAM-S-0001.md": string(data)})
		return NewSprintStore(fsys, ".pmngr")
	}

	tests := []struct {
		name  string
		check func(t *testing.T, store *SprintStore)
	}{
		{
			name: "list and get read the folder",
			check: func(t *testing.T, store *SprintStore) {
				sprints, diags, err := store.List(ctx)
				if err != nil || len(sprints) != 1 || len(diags) != 0 {
					t.Fatalf("List = %d sprints, %v, %v", len(sprints), diags, err)
				}
				if _, err := store.Get(ctx, "DEMO-TEAM-S-0001"); err != nil {
					t.Fatalf("Get: %v", err)
				}
			},
		},
		{
			name: "an unknown sprint is not found",
			check: func(t *testing.T, store *SprintStore) {
				if _, err := store.Get(ctx, "DEMO-TEAM-S-0999"); err == nil {
					t.Fatal("Get of an unknown sprint must fail")
				}
			},
		},
		{
			name: "the next id continues the numbering",
			check: func(t *testing.T, store *SprintStore) {
				got, err := store.NextID(ctx, "DEMO-TEAM")
				if err != nil || got != "DEMO-TEAM-S-0002" {
					t.Fatalf("NextID = %q, %v", got, err)
				}
			},
		},
		{
			name: "a stale revision is refused",
			check: func(t *testing.T, store *SprintStore) {
				sprint, err := store.Get(ctx, "DEMO-TEAM-S-0001")
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				sprint.Goal = "Something else"
				if _, err := store.Write(ctx, sprint, "sha256:0000000000000000"); err == nil {
					t.Fatal("a stale revision must be refused")
				}
				if _, err := store.Write(ctx, sprint, sprint.Rev); err != nil {
					t.Fatalf("Write: %v", err)
				}
				again, err := store.Get(ctx, "DEMO-TEAM-S-0001")
				if err != nil || again.Goal != "Something else" {
					t.Fatalf("reread = %+v, %v", again, err)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.check(t, seed(t)) })
	}
}
