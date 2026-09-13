package core

import (
	"strings"
	"testing"
	"time"
)

// snapshotFixtureView renders the metrics fixture of metrics_test.go as a
// sprint view, so that a snapshot and the live numbers are computed from one
// and the same input.
func snapshotFixtureView(t *testing.T, s *Sprint, cards []BoardCard, now time.Time) SprintView {
	t.Helper()
	view := SprintView{Cards: cards, Backlog: []BoardCard{}, Diagnostics: []Diagnostic{}}
	for i := range view.Cards {
		view.Cards[i].InSprint = true
	}
	view.Sprint = SummarizeSprint(s, view.Cards, now)
	return view
}

func TestBuildSprintSnapshotArithmeticMatchesTheLiveView(t *testing.T) {
	sprint := metricsFixtureSprint(t)
	cards := metricsFixtureCards()
	now := metricsFixtureNow(t)
	view := snapshotFixtureView(t, sprint, cards, now)
	metrics := BuildSprintMetrics(sprint, MetricsInput{
		Cards:      cards,
		History:    metricsFixtureHistory(t),
		Provenance: MetricsProvenance{Source: MetricsSourceGit},
		Now:        now,
	})

	snap := BuildSprintSnapshot(sprint, view, metrics, now)

	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "the totals are the live metrics, frozen",
			check: func(t *testing.T) {
				live := view.Sprint.Metrics
				if snap.Totals.Items != live.Items || snap.Totals.Resolved != live.Resolved ||
					snap.Totals.Done != live.Done || snap.Totals.Unresolved != live.Unresolved {
					t.Fatalf("totals = %+v, live = %+v", snap.Totals, live)
				}
				if !nearly(snap.Totals.Points, live.Points) ||
					!nearly(snap.Totals.CommittedPoints, live.CommittedPoints) ||
					!nearly(snap.Totals.DonePoints, live.DonePoints) {
					t.Fatalf("point totals = %+v, live = %+v", snap.Totals, live)
				}
				// The fixture: three items, two done for 8 of 10 points, 8 of
				// which were committed.
				if snap.Totals.Items != 3 || snap.Totals.Done != 2 ||
					!nearly(snap.Totals.Points, 10) || !nearly(snap.Totals.DonePoints, 8) ||
					!nearly(snap.Totals.CommittedPoints, 8) {
					t.Fatalf("totals = %+v", snap.Totals)
				}
			},
		},
		{
			name: "the per-status distribution counts every resolved card once",
			check: func(t *testing.T) {
				if snap.ByStatus["done"] != 2 || snap.ByStatus["todo"] != 1 {
					t.Fatalf("by_status = %v", snap.ByStatus)
				}
				total := 0
				for _, n := range snap.ByStatus {
					total += n
				}
				if total != snap.Totals.Resolved {
					t.Fatalf("by_status sums to %d, resolved = %d", total, snap.Totals.Resolved)
				}
			},
		},
		{
			name: "the burndown is the frozen observed series",
			check: func(t *testing.T) {
				if len(snap.Burndown) != 5 {
					t.Fatalf("burndown = %d points, want 5", len(snap.Burndown))
				}
				if snap.Burndown[0].Date.String() != "2026-03-02" ||
					snap.Burndown[4].Date.String() != "2026-03-06" {
					t.Fatalf("burndown range = %s .. %s",
						snap.Burndown[0].Date, snap.Burndown[4].Date)
				}
				last := snap.Burndown[4]
				if last.Completed != 2 || !nearly(last.RemainingPoints, 2) || last.Remaining != 1 {
					t.Fatalf("the last frozen day = %+v", last)
				}
			},
		},
		{
			name: "the provenance of the history is carried through",
			check: func(t *testing.T) {
				if snap.Provenance.Source != MetricsSourceGit || snap.Provenance.Approximate {
					t.Fatalf("provenance = %+v", snap.Provenance)
				}
				if snap.Provenance.Items != metrics.Provenance.Items ||
					snap.Provenance.Covered != metrics.Provenance.Covered {
					t.Fatalf("provenance counters = %+v, live = %+v",
						snap.Provenance, metrics.Provenance)
				}
			},
		},
		{
			name: "the closing instant comes from the caller",
			check: func(t *testing.T) {
				if snap.Version != SprintSnapshotVersion {
					t.Fatalf("version = %d", snap.Version)
				}
				if !snap.ClosedAt.Equal(NewTimestamp(now).Time) {
					t.Fatalf("closed_at = %s, want %s", snap.ClosedAt, NewTimestamp(now))
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.check)
	}
}

func TestBuildSprintSnapshotDistributionsAndUnresolvedRefs(t *testing.T) {
	sprint := metricsFixtureSprint(t)
	sprint.Items = append(sprint.Items, "WEB/WEB-US-0031")
	cards := metricsFixtureCards()
	cards[0].Assignees = []string{"alice", "bob", "alice"}
	cards[0].Labels = []string{"core"}
	cards[1].Assignees = []string{"alice"}
	cards[1].Labels = []string{"core", "docs"}
	cards[2].Assignees = []string{"bob"}
	cards[2].Labels = []string{"docs"}
	// A reference no clone and no snapshot could grade: it carries no status.
	cards = append(cards, BoardCard{
		Ref: "WEB/WEB-US-0031", Project: "WEB", Item: "WEB-US-0031",
		Assignees: []string{"carol"}, Labels: []string{"web"},
		Reason: "not cloned on this machine",
	})
	now := metricsFixtureNow(t)
	view := snapshotFixtureView(t, sprint, cards, now)
	snap := BuildSprintSnapshot(sprint, view, SprintMetricsView{}, now)

	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "an assignee listed twice on one card counts once",
			check: func(t *testing.T) {
				alice := snap.ByAssignee["alice"]
				if alice.Total != 2 || alice.Done != 2 || !nearly(alice.Points, 8) {
					t.Fatalf("alice = %+v", alice)
				}
				bob := snap.ByAssignee["bob"]
				if bob.Total != 2 || bob.Done != 1 || !nearly(bob.Points, 5) {
					t.Fatalf("bob = %+v", bob)
				}
			},
		},
		{
			name: "labels are distributed the same way",
			check: func(t *testing.T) {
				core := snap.ByLabel["core"]
				if core.Total != 2 || core.Done != 2 || !nearly(core.Points, 8) {
					t.Fatalf("core = %+v", core)
				}
				docs := snap.ByLabel["docs"]
				if docs.Total != 2 || docs.Done != 1 || !nearly(docs.Points, 7) {
					t.Fatalf("docs = %+v", docs)
				}
			},
		},
		{
			name: "an unresolved reference is reported and never counted as work",
			check: func(t *testing.T) {
				if snap.Totals.Items != 4 || snap.Totals.Resolved != 3 || snap.Totals.Unresolved != 1 {
					t.Fatalf("totals = %+v", snap.Totals)
				}
				if _, counted := snap.ByAssignee["carol"]; counted {
					t.Fatalf("an unresolved card must not reach a distribution: %v", snap.ByAssignee)
				}
				if _, counted := snap.ByLabel["web"]; counted {
					t.Fatalf("an unresolved card must not reach a distribution: %v", snap.ByLabel)
				}
			},
		},
		{
			name: "a snapshot built without history is marked approximate",
			check: func(t *testing.T) {
				if snap.Provenance.Source != MetricsSourceNone || !snap.Provenance.Approximate {
					t.Fatalf("provenance = %+v", snap.Provenance)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.check)
	}
}

func TestBuildSprintSnapshotCapsTheBurndownAtOnePointPerSprintDay(t *testing.T) {
	sprint := metricsFixtureSprint(t)
	now := metricsFixtureNow(t)
	view := snapshotFixtureView(t, sprint, metricsFixtureCards(), now)

	live := Burndown{Points: []BurndownPoint{}}
	// Twelve observed points over a five-day sprint, two of them repeating a
	// date: the cap keeps five, and the repeat never doubles a day.
	for i := 0; i < 12; i++ {
		date := sprintDate("2026-03-02").AddDate(0, 0, i)
		if i == 3 {
			date = sprintDate("2026-03-02").Time
		}
		live.Points = append(live.Points, BurndownPoint{
			Date: NewDate(date), Day: i + 1, Observed: true, Items: 3, Completed: i % 3,
		})
	}
	live.Points = append(live.Points, BurndownPoint{
		Date: NewDate(sprintDate("2026-03-20").Time), Day: 99, Observed: false,
	})

	snap := BuildSprintSnapshot(sprint, view, SprintMetricsView{Burndown: live}, now)
	if got := len(snap.Burndown); got != sprint.TotalDays() {
		t.Fatalf("burndown = %d points, want %d", got, sprint.TotalDays())
	}
	seen := map[string]bool{}
	for i, p := range snap.Burndown {
		if seen[p.Date.String()] {
			t.Fatalf("day %s is frozen twice", p.Date)
		}
		seen[p.Date.String()] = true
		if i > 0 && !snap.Burndown[i-1].Date.Before(p.Date.Time) {
			t.Fatalf("the frozen series is not in date order: %v", snap.Burndown)
		}
		if p.Date.String() == "2026-03-20" {
			t.Fatal("an unobserved day must never be frozen")
		}
	}
}

func TestSprintSnapshotRoundTrip(t *testing.T) {
	const file = `---
id: DEMO-TEAM-S-0001
type: sprint
board: demo-scrum
state: closed
start: 2026-03-02
end: 2026-03-06
items:
  - DEMO/DEMO-US-0001
  - DEMO/DEMO-US-0002
snapshot:
  version: 1
  closed_at: 2026-03-06T23:00:00Z
  totals:
    items: 2
    resolved: 2
    done: 1
    unresolved: 0
    points: 8
    committed_points: 8
    done_points: 3
  by_status:
    done: 1
    todo: 1
  by_assignee:
    alice: { total: 2, done: 1, points: 8 }
  by_label:
    core: { total: 1, done: 1, points: 3 }
  burndown:
    - { date: 2026-03-02, remaining: 2, remaining_points: 8, ideal: 8, completed: 0, unknown: 0 }
    - { date: 2026-03-03, remaining: 1, remaining_points: 5, ideal: 6, completed: 1, unknown: 0 }
  provenance:
    source: git
    approximate: false
    from: 2026-02-25
    commits: 12
    items: 2
    covered: 2
    note: Reconstructed from the git history of the item files.
  vendor_extension: kept as it was found
---

## Goal

Ship the thing.
`

	sprint, err := ParseSprint(".pmngr/sprints/DEMO-TEAM-S-0001.md", []byte(file))
	if err != nil {
		t.Fatalf("ParseSprint: %v", err)
	}

	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "the block is decoded",
			check: func(t *testing.T) {
				snap := sprint.Snapshot
				if snap == nil {
					t.Fatal("the snapshot was dropped")
				}
				if snap.Version != 1 || snap.ClosedAt.String() != "2026-03-06T23:00:00Z" {
					t.Fatalf("snapshot header = %+v", snap)
				}
				if snap.Totals.Items != 2 || !nearly(snap.Totals.DonePoints, 3) {
					t.Fatalf("totals = %+v", snap.Totals)
				}
				if snap.ByStatus["done"] != 1 || snap.ByAssignee["alice"].Total != 2 ||
					snap.ByLabel["core"].Done != 1 {
					t.Fatalf("distributions = %v %v %v", snap.ByStatus, snap.ByAssignee, snap.ByLabel)
				}
				if len(snap.Burndown) != 2 || snap.Burndown[1].Completed != 1 ||
					!nearly(snap.Burndown[1].RemainingPoints, 5) {
					t.Fatalf("burndown = %+v", snap.Burndown)
				}
				if snap.Provenance.Source != MetricsSourceGit || snap.Provenance.Commits != 12 ||
					snap.Provenance.From.String() != "2026-02-25" {
					t.Fatalf("provenance = %+v", snap.Provenance)
				}
			},
		},
		{
			name: "an unknown key inside the block survives",
			check: func(t *testing.T) {
				if got := sprint.Snapshot.Extra["vendor_extension"]; got != "kept as it was found" {
					t.Fatalf("extra = %v", sprint.Snapshot.Extra)
				}
				if _, leaked := sprint.Extra["snapshot"]; leaked {
					t.Fatalf("the modeled block must not land in the top-level extras: %v", sprint.Extra)
				}
			},
		},
		{
			name: "the file round-trips byte for byte",
			check: func(t *testing.T) {
				data, err := SerializeSprint(sprint)
				if err != nil {
					t.Fatalf("SerializeSprint: %v", err)
				}
				if string(data) != file {
					t.Fatalf("round trip differs:\n--- want ---\n%s\n--- got ---\n%s", file, data)
				}
			},
		},
		{
			name: "the snapshot sits between retro and created in the key order",
			check: func(t *testing.T) {
				full := *sprint
				full.Retro = "DEMO-TEAM-R-0001"
				full.Created = at(t, "2026-03-01T08:00:00Z")
				full.Author = "alice"
				data, err := SerializeSprint(&full)
				if err != nil {
					t.Fatalf("SerializeSprint: %v", err)
				}
				text := string(data)
				retro := strings.Index(text, "\nretro:")
				snap := strings.Index(text, "\nsnapshot:")
				created := strings.Index(text, "\ncreated:")
				if retro < 0 || snap < 0 || created < 0 || retro >= snap || snap >= created {
					t.Fatalf("key order is wrong (retro %d, snapshot %d, created %d):\n%s",
						retro, snap, created, text)
				}
			},
		},
		{
			name: "an unknown future version parses without loss",
			check: func(t *testing.T) {
				future := strings.Replace(file, "  version: 1\n", "  version: 7\n", 1)
				parsed, err := ParseSprint(".pmngr/sprints/DEMO-TEAM-S-0001.md", []byte(future))
				if err != nil {
					t.Fatalf("ParseSprint: %v", err)
				}
				if parsed.Snapshot.Version != 7 {
					t.Fatalf("version = %d, want 7", parsed.Snapshot.Version)
				}
				data, err := SerializeSprint(parsed)
				if err != nil {
					t.Fatalf("SerializeSprint: %v", err)
				}
				if string(data) != future {
					t.Fatalf("a future version was not preserved:\n%s", data)
				}
			},
		},
		{
			name: "an absent version defaults to 1",
			check: func(t *testing.T) {
				without := strings.Replace(file, "  version: 1\n", "", 1)
				parsed, err := ParseSprint(".pmngr/sprints/DEMO-TEAM-S-0001.md", []byte(without))
				if err != nil {
					t.Fatalf("ParseSprint: %v", err)
				}
				if parsed.Snapshot.Version != SprintSnapshotVersion {
					t.Fatalf("version = %d, want %d", parsed.Snapshot.Version, SprintSnapshotVersion)
				}
			},
		},
		{
			name: "a sprint without a snapshot emits no block at all",
			check: func(t *testing.T) {
				open := newDatedSprint("DEMO-TEAM-S-0002", SprintActive, "2026-03-09", "2026-03-13")
				data, err := SerializeSprint(open)
				if err != nil {
					t.Fatalf("SerializeSprint: %v", err)
				}
				if strings.Contains(string(data), "snapshot:") {
					t.Fatalf("an open sprint stores no series (ADR-017):\n%s", data)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.check)
	}
}

// TestSnapshotAndLiveMetricsAgree is the promise the snapshot makes: reading a
// closed sprint from its frozen block gives the same series the history walk
// gave at the moment it was frozen, so nobody has to choose between the two.
func TestSnapshotAndLiveMetricsAgree(t *testing.T) {
	sprint := metricsFixtureSprint(t)
	cards := metricsFixtureCards()
	now := metricsFixtureNow(t)
	live := BuildSprintMetrics(sprint, MetricsInput{
		Cards:      cards,
		History:    metricsFixtureHistory(t),
		Provenance: MetricsProvenance{Source: MetricsSourceGit},
		Now:        now,
	})
	view := snapshotFixtureView(t, sprint, cards, now)

	snap := BuildSprintSnapshot(sprint, view, live, now)
	sprint.State = SprintClosed
	sprint.Snapshot = &snap

	frozen, ok := SprintMetricsFromSnapshot(sprint, view.Sprint)
	if !ok {
		t.Fatal("a sprint carrying a snapshot must answer from it")
	}
	if len(frozen.Burndown.Points) != len(live.Burndown.Points) {
		t.Fatalf("frozen %d points, live %d",
			len(frozen.Burndown.Points), len(live.Burndown.Points))
	}
	if !nearly(frozen.Burndown.CommittedPoints, live.Burndown.CommittedPoints) {
		t.Fatalf("committed points: frozen %v, live %v",
			frozen.Burndown.CommittedPoints, live.Burndown.CommittedPoints)
	}
	for i, want := range live.Burndown.Points {
		got := frozen.Burndown.Points[i]
		if got.Date.String() != want.Date.String() {
			t.Fatalf("point %d date: frozen %s, live %s", i, got.Date, want.Date)
		}
		if !nearly(got.Remaining, want.Remaining) || !nearly(got.Ideal, want.Ideal) {
			t.Fatalf("point %d: frozen %+v, live %+v", i, got, want)
		}
		if got.Completed != want.Completed || got.Unknown != want.Unknown ||
			got.Items != want.Items {
			t.Fatalf("point %d counters: frozen %+v, live %+v", i, got, want)
		}
	}

	if frozen.Provenance.Source != MetricsSourceSnapshot {
		t.Fatalf("provenance source = %q, want %q",
			frozen.Provenance.Source, MetricsSourceSnapshot)
	}
	if !strings.Contains(frozen.Provenance.Note, "frozen when the sprint was closed") {
		t.Fatalf("the note must distinguish frozen from live: %q", frozen.Provenance.Note)
	}
	if strings.Contains(live.Provenance.Note, "frozen") {
		t.Fatalf("a live reading must not claim to be frozen: %q", live.Provenance.Note)
	}

	open := metricsFixtureSprint(t)
	if _, ok := SprintMetricsFromSnapshot(open, view.Sprint); ok {
		t.Fatal("a sprint with no snapshot falls through to the history walk")
	}
}
