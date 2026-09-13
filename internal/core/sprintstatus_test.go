package core

import (
	"strings"
	"testing"
	"time"
)

// newDatedSprint builds a sprint with a range, which is all the derivation rules need.
func newDatedSprint(id string, state SprintState, start, end string) *Sprint {
	s := &Sprint{ID: id, Type: "sprint", Board: "demo-scrum", State: state, Items: []string{}}
	if start != "" {
		s.Start = sprintDate(start)
	}
	if end != "" {
		s.End = sprintDate(end)
	}
	return s
}

func sprintDate(s string) Date {
	d, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func sprintDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 9, 30, 0, 0, time.UTC)
}

func TestSprintDerivedStatus(t *testing.T) {
	tests := []struct {
		name   string
		sprint *Sprint
		now    time.Time
		want   SprintStatus
	}{
		{
			name:   "no dates at all is a draft",
			sprint: newDatedSprint("DEMO-TEAM-S-0001", SprintPlanned, "", ""),
			now:    sprintDay(2026, time.September, 2),
			want:   SprintStatusDraft,
		},
		{
			name:   "the day before the start is upcoming",
			sprint: newDatedSprint("DEMO-TEAM-S-0001", SprintPlanned, "2026-09-01", "2026-09-14"),
			now:    sprintDay(2026, time.August, 31),
			want:   SprintStatusUpcoming,
		},
		{
			name:   "the first day is current",
			sprint: newDatedSprint("DEMO-TEAM-S-0001", SprintPlanned, "2026-09-01", "2026-09-14"),
			now:    sprintDay(2026, time.September, 1),
			want:   SprintStatusCurrent,
		},
		{
			name:   "a day inside the range is current",
			sprint: newDatedSprint("DEMO-TEAM-S-0001", SprintActive, "2026-09-01", "2026-09-14"),
			now:    sprintDay(2026, time.September, 7),
			want:   SprintStatusCurrent,
		},
		{
			name:   "the last day is still current",
			sprint: newDatedSprint("DEMO-TEAM-S-0001", SprintActive, "2026-09-01", "2026-09-14"),
			now:    sprintDay(2026, time.September, 14),
			want:   SprintStatusCurrent,
		},
		{
			name:   "the day after the end is completed",
			sprint: newDatedSprint("DEMO-TEAM-S-0001", SprintActive, "2026-09-01", "2026-09-14"),
			now:    sprintDay(2026, time.September, 15),
			want:   SprintStatusCompleted,
		},
		{
			name:   "closed beats the calendar mid-range",
			sprint: newDatedSprint("DEMO-TEAM-S-0001", SprintClosed, "2026-09-01", "2026-09-14"),
			now:    sprintDay(2026, time.September, 7),
			want:   SprintStatusCompleted,
		},
		{
			name:   "closed beats the calendar before the start",
			sprint: newDatedSprint("DEMO-TEAM-S-0001", SprintClosed, "2026-09-01", "2026-09-14"),
			now:    sprintDay(2026, time.August, 1),
			want:   SprintStatusCompleted,
		},
		{
			name:   "a closed sprint with no dates is completed, not a draft",
			sprint: newDatedSprint("DEMO-TEAM-S-0001", SprintClosed, "", ""),
			now:    sprintDay(2026, time.September, 7),
			want:   SprintStatusCompleted,
		},
		{
			name:   "a half-dated sprint is a draft until its file is repaired",
			sprint: newDatedSprint("DEMO-TEAM-S-0001", SprintPlanned, "2026-09-01", ""),
			now:    sprintDay(2026, time.September, 7),
			want:   SprintStatusDraft,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.sprint.DerivedStatus(tc.now); got != tc.want {
				t.Fatalf("DerivedStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSprintStatusIsNeverWritten(t *testing.T) {
	sprint := newDatedSprint("DEMO-TEAM-S-0001", SprintActive, "2026-09-01", "2026-09-14")
	data, err := SerializeSprint(sprint)
	if err != nil {
		t.Fatalf("SerializeSprint: %v", err)
	}
	if strings.Contains(string(data), "status:") {
		t.Fatalf("the derived status was written to the file:\n%s", data)
	}

	summary := SummarizeSprint(sprint, nil, sprintDay(2026, time.September, 7))
	if summary.Status != SprintStatusCurrent {
		t.Fatalf("summary status = %q, want current", summary.Status)
	}
	if summary.State != SprintActive {
		t.Fatalf("the stored state must survive untouched: %q", summary.State)
	}
}

func TestParseSprintStatus(t *testing.T) {
	tests := []struct {
		in      string
		want    SprintStatus
		wantErr bool
	}{
		{in: "draft", want: SprintStatusDraft},
		{in: " Current ", want: SprintStatusCurrent},
		{in: "upcoming", want: SprintStatusUpcoming},
		{in: "completed", want: SprintStatusCompleted},
		{in: "active", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseSprintStatus(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseSprintStatus(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ParseSprintStatus(%q) = %q, %v", tc.in, got, err)
			}
		})
	}
}

func TestSprintDatesAreBothOrNeither(t *testing.T) {
	in := SprintValidateInput{TeamKey: "DEMO-TEAM", Boards: []string{"demo-scrum"}}
	codesOf := func(diags []Diagnostic) []Code {
		var out []Code
		for _, d := range diags {
			out = append(out, d.Code)
		}
		return out
	}
	has := func(codes []Code, want Code) bool {
		for _, c := range codes {
			if c == want {
				return true
			}
		}
		return false
	}

	tests := []struct {
		name    string
		sprint  *Sprint
		wantErr bool
	}{
		{
			name:   "neither date is a valid draft",
			sprint: newDatedSprint("DEMO-TEAM-S-0001", SprintPlanned, "", ""),
		},
		{
			name:   "both dates are valid",
			sprint: newDatedSprint("DEMO-TEAM-S-0001", SprintPlanned, "2026-09-01", "2026-09-14"),
		},
		{
			name:    "a start alone is an error",
			sprint:  newDatedSprint("DEMO-TEAM-S-0001", SprintPlanned, "2026-09-01", ""),
			wantErr: true,
		},
		{
			name:    "an end alone is an error",
			sprint:  newDatedSprint("DEMO-TEAM-S-0001", SprintPlanned, "", "2026-09-14"),
			wantErr: true,
		},
		{
			name:    "an inverted range is an error",
			sprint:  newDatedSprint("DEMO-TEAM-S-0001", SprintPlanned, "2026-09-14", "2026-09-01"),
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.sprint.Path = ".pmngr/sprints/DEMO-TEAM-S-0001.md"
			codes := codesOf(tc.sprint.Validate(in))
			if got := has(codes, CodeSprintDates); got != tc.wantErr {
				t.Fatalf("E-SPRINT-DATES = %v, want %v (codes %v)", got, tc.wantErr, codes)
			}
		})
	}
}

func TestDraftSprintDayArithmetic(t *testing.T) {
	draft := newDatedSprint("DEMO-TEAM-S-0001", SprintPlanned, "", "")
	now := sprintDay(2026, time.September, 7)
	if got := draft.TotalDays(); got != 0 {
		t.Fatalf("TotalDays of a draft = %d, want 0", got)
	}
	if got := draft.RemainingDays(now); got != 0 {
		t.Fatalf("RemainingDays of a draft = %d, want 0", got)
	}
	if !draft.IsDraft() {
		t.Fatal("a dateless sprint is a draft")
	}

	half := newDatedSprint("DEMO-TEAM-S-0002", SprintPlanned, "2026-09-01", "")
	if got := half.TotalDays(); got != 0 {
		t.Fatalf("TotalDays of a half-dated sprint = %d, want 0", got)
	}
	if got := half.RemainingDays(now); got != 0 {
		t.Fatalf("RemainingDays of a half-dated sprint = %d, want 0", got)
	}
}

func TestDraftSprintsNeverOverlap(t *testing.T) {
	dateless := newDatedSprint("DEMO-TEAM-S-0001", SprintPlanned, "", "")
	other := newDatedSprint("DEMO-TEAM-S-0002", SprintActive, "2026-09-01", "2026-09-14")
	inside := newDatedSprint("DEMO-TEAM-S-0003", SprintPlanned, "2026-09-07", "2026-09-20")

	tests := []struct {
		name string
		a, b *Sprint
		want bool
	}{
		{name: "a draft never collides", a: dateless, b: other, want: false},
		{name: "and the collision is symmetric", a: other, b: dateless, want: false},
		{name: "two dated ranges that share a day collide", a: inside, b: other, want: true},
		{name: "a sprint never collides with itself", a: other, b: other, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Overlaps(tc.b); got != tc.want {
				t.Fatalf("Overlaps = %v, want %v", got, tc.want)
			}
		})
	}

	// The set-level rule agrees with the pairwise one: a draft raises no
	// W-SPRINT-OVERLAP warning however many dated sprints surround it.
	for _, s := range []*Sprint{dateless, other} {
		s.Path = ".pmngr/sprints/" + s.ID + ".md"
	}
	diags := ValidateSprintSet([]*Sprint{dateless, other})
	for _, d := range diags {
		if d.Code == CodeSprintOverlap {
			t.Fatalf("a draft must not warn: %+v", d)
		}
	}
}

func TestSprintOverlapMessageNamesTheOtherSprintAndTheEscapeHatch(t *testing.T) {
	candidate := newDatedSprint("DEMO-TEAM-S-0003", SprintPlanned, "2026-09-07", "2026-09-20")
	other := newDatedSprint("DEMO-TEAM-S-0002", SprintActive, "2026-09-01", "2026-09-14")
	got := SprintOverlapMessage(candidate, other)
	for _, want := range []string{
		"DEMO-TEAM-S-0003", "DEMO-TEAM-S-0002", "2026-09-01", "2026-09-14",
		"demo-scrum", "remove them to keep",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the refusal must name %q: %s", want, got)
		}
	}
	if OverlappingSprint(candidate, []*Sprint{other}) != other {
		t.Fatal("OverlappingSprint must find the collision the message describes")
	}
	draft := newDatedSprint("DEMO-TEAM-S-0004", SprintPlanned, "", "")
	if got := OverlappingSprint(draft, []*Sprint{other, candidate}); got != nil {
		t.Fatalf("a draft collides with nothing, got %v", got.ID)
	}
}

func TestSortSprintsForListing(t *testing.T) {
	now := sprintDay(2026, time.September, 7)
	current := newDatedSprint("DEMO-TEAM-S-0002", SprintActive, "2026-09-01", "2026-09-14")
	upcomingLate := newDatedSprint("DEMO-TEAM-S-0004", SprintPlanned, "2026-10-01", "2026-10-14")
	upcomingSoon := newDatedSprint("DEMO-TEAM-S-0005", SprintPlanned, "2026-09-15", "2026-09-28")
	draftB := newDatedSprint("DEMO-TEAM-S-0007", SprintPlanned, "", "")
	draftA := newDatedSprint("DEMO-TEAM-S-0006", SprintPlanned, "", "")
	done := newDatedSprint("DEMO-TEAM-S-0001", SprintClosed, "2026-08-01", "2026-08-14")

	list := []*Sprint{done, draftB, upcomingLate, draftA, current, upcomingSoon}
	SortSprintsForListing(list, now)
	want := []string{
		"DEMO-TEAM-S-0002", // current
		"DEMO-TEAM-S-0005", // upcoming, earlier start first
		"DEMO-TEAM-S-0004",
		"DEMO-TEAM-S-0006", // drafts share a zero start, so the id breaks the tie
		"DEMO-TEAM-S-0007",
		"DEMO-TEAM-S-0001", // completed
	}
	for i, id := range want {
		if list[i].ID != id {
			t.Fatalf("position %d = %s, want %s (order %v)", i, list[i].ID, id, sprintIDsOf(list))
		}
	}

	only := FilterSprintsByStatus(list, now, []SprintStatus{SprintStatusCurrent, SprintStatusUpcoming})
	if got := sprintIDsOf(only); strings.Join(got, ",") !=
		"DEMO-TEAM-S-0002,DEMO-TEAM-S-0005,DEMO-TEAM-S-0004" {
		t.Fatalf("filtered = %v", got)
	}
	if got := FilterSprintsByStatus(list, now, nil); len(got) != len(list) {
		t.Fatalf("an empty filter keeps everything, got %d", len(got))
	}
	if got := FilterSprintsByStatus(list, now, []SprintStatus{SprintStatusDraft}); len(got) != 2 {
		t.Fatalf("drafts = %v", sprintIDsOf(got))
	}
}

func sprintIDsOf(sprints []*Sprint) []string {
	out := make([]string, 0, len(sprints))
	for _, s := range sprints {
		out = append(out, s.ID)
	}
	return out
}
