package core

import (
	"math"
	"testing"
	"time"
)

// The fixture of this file is the hand-computed reference the acceptance
// criteria of GIT-US-0028 ask for: a five-day sprint, two committed items and
// one pulled in mid-sprint, with every transition written down. Every number
// asserted below was worked out on paper from that table before it was coded.
//
//	sprint      DEMO-TEAM-S-0001, 2026-03-02 .. 2026-03-06 (5 days)
//	committed   DEMO/DEMO-US-0001 (3 pts), DEMO/DEMO-US-0002 (5 pts)
//	added       DEMO/DEMO-US-0003 (2 pts), created on day 3
//
//	US-0001  created 2026-02-25 09:00 todo
//	         in_progress 2026-03-02 10:00, done 2026-03-03 16:00
//	US-0002  created 2026-02-26 09:00 todo
//	         in_progress 2026-03-04 09:00, done 2026-03-06 11:00
//	US-0003  created 2026-03-04 08:00 todo, never started

func at(t *testing.T, s string) Timestamp {
	t.Helper()
	ts, err := ParseTimestamp(s)
	if err != nil {
		t.Fatalf("parse timestamp %q: %v", s, err)
	}
	return ts
}

func points(v float64) *float64 { return &v }

func metricsFixtureSprint(t *testing.T) *Sprint {
	t.Helper()
	start, err := ParseDate("2026-03-02")
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	end, err := ParseDate("2026-03-06")
	if err != nil {
		t.Fatalf("parse end: %v", err)
	}
	return &Sprint{
		ID: "DEMO-TEAM-S-0001", Type: "sprint", Board: "delivery", State: SprintActive,
		Start: start, End: end,
		Committed: []string{"DEMO/DEMO-US-0001", "DEMO/DEMO-US-0002"},
		Items:     []string{"DEMO/DEMO-US-0001", "DEMO/DEMO-US-0002", "DEMO/DEMO-US-0003"},
	}
}

func metricsFixtureCards() []BoardCard {
	return []BoardCard{
		{
			Ref: "DEMO/DEMO-US-0001", Project: "DEMO", Item: "DEMO-US-0001", Declared: true,
			Status: "done", Category: CategoryDone, Estimate: points(3),
		},
		{
			Ref: "DEMO/DEMO-US-0002", Project: "DEMO", Item: "DEMO-US-0002", Declared: true,
			Status: "done", Category: CategoryDone, Estimate: points(5),
		},
		{
			Ref: "DEMO/DEMO-US-0003", Project: "DEMO", Item: "DEMO-US-0003", Declared: true,
			Status: "todo", Category: CategoryTodo, Estimate: points(2),
		},
	}
}

func metricsFixtureHistory(t *testing.T) []ItemHistory {
	t.Helper()
	return []ItemHistory{
		{Ref: "DEMO/DEMO-US-0001", Complete: true, Observations: []ItemObservation{
			{At: at(t, "2026-02-25T09:00:00Z"), Status: "todo", Category: CategoryTodo, Estimate: points(3)},
			{At: at(t, "2026-03-02T10:00:00Z"), Status: "in_progress", Category: CategoryInProgress, Estimate: points(3)},
			{At: at(t, "2026-03-03T16:00:00Z"), Status: "done", Category: CategoryDone, Estimate: points(3)},
		}},
		{Ref: "DEMO/DEMO-US-0002", Complete: true, Observations: []ItemObservation{
			{At: at(t, "2026-02-26T09:00:00Z"), Status: "todo", Category: CategoryTodo, Estimate: points(5)},
			{At: at(t, "2026-03-04T09:00:00Z"), Status: "in_progress", Category: CategoryInProgress, Estimate: points(5)},
			{At: at(t, "2026-03-06T11:00:00Z"), Status: "done", Category: CategoryDone, Estimate: points(5)},
		}},
		{Ref: "DEMO/DEMO-US-0003", Complete: true, Observations: []ItemObservation{
			{At: at(t, "2026-03-04T08:00:00Z"), Status: "todo", Category: CategoryTodo, Estimate: points(2)},
		}},
	}
}

func metricsFixtureNow(t *testing.T) time.Time {
	t.Helper()
	return at(t, "2026-03-06T23:00:00Z").Time
}

func nearly(got, want float64) bool { return math.Abs(got-want) < 0.005 }

func TestBuildSprintMetricsBurndownMatchesTheReference(t *testing.T) {
	t.Parallel()

	view := BuildSprintMetrics(metricsFixtureSprint(t), MetricsInput{
		Cards:      metricsFixtureCards(),
		History:    metricsFixtureHistory(t),
		Provenance: MetricsProvenance{Source: MetricsSourceGit},
		Now:        metricsFixtureNow(t),
	})

	if got := len(view.Burndown.Points); got != 5 {
		t.Fatalf("points = %d, want 5", got)
	}
	if !nearly(view.Burndown.CommittedPoints, 8) {
		t.Errorf("committed points = %v, want 8", view.Burndown.CommittedPoints)
	}

	cases := []struct {
		date                        string
		ideal, remaining, scope     float64
		done                        float64
		items, completed, unknownAt int
	}{
		{"2026-03-02", 8, 8, 8, 0, 2, 0, 0},
		{"2026-03-03", 6, 5, 8, 3, 2, 1, 0},
		{"2026-03-04", 4, 7, 10, 3, 3, 1, 0},
		{"2026-03-05", 2, 7, 10, 3, 3, 1, 0},
		{"2026-03-06", 0, 2, 10, 8, 3, 2, 0},
	}
	for i, want := range cases {
		got := view.Burndown.Points[i]
		t.Run(want.date, func(t *testing.T) {
			if got.Date.String() != want.date {
				t.Fatalf("date = %s, want %s", got.Date, want.date)
			}
			if !got.Observed {
				t.Fatalf("day %d must be observed", got.Day)
			}
			if !nearly(got.Ideal, want.ideal) {
				t.Errorf("ideal = %v, want %v", got.Ideal, want.ideal)
			}
			if !nearly(got.Remaining, want.remaining) {
				t.Errorf("remaining = %v, want %v", got.Remaining, want.remaining)
			}
			if !nearly(got.Scope, want.scope) {
				t.Errorf("scope = %v, want %v", got.Scope, want.scope)
			}
			if !nearly(got.Done, want.done) {
				t.Errorf("done = %v, want %v", got.Done, want.done)
			}
			if got.Items != want.items || got.Completed != want.completed || got.Unknown != want.unknownAt {
				t.Errorf("items/completed/unknown = %d/%d/%d, want %d/%d/%d",
					got.Items, got.Completed, got.Unknown, want.items, want.completed, want.unknownAt)
			}
		})
	}
}

func TestBuildSprintMetricsCumulativeFlowMatchesTheReference(t *testing.T) {
	t.Parallel()

	view := BuildSprintMetrics(metricsFixtureSprint(t), MetricsInput{
		Cards:      metricsFixtureCards(),
		History:    metricsFixtureHistory(t),
		Provenance: MetricsProvenance{Source: MetricsSourceGit},
		Now:        metricsFixtureNow(t),
	})

	want := []struct {
		date  string
		bands map[FlowBand]int
		total int
	}{
		{"2026-03-02", map[FlowBand]int{FlowInProgress: 1, FlowTodo: 1}, 2},
		{"2026-03-03", map[FlowBand]int{FlowDone: 1, FlowTodo: 1}, 2},
		{"2026-03-04", map[FlowBand]int{FlowDone: 1, FlowInProgress: 1, FlowTodo: 1}, 3},
		{"2026-03-05", map[FlowBand]int{FlowDone: 1, FlowInProgress: 1, FlowTodo: 1}, 3},
		{"2026-03-06", map[FlowBand]int{FlowDone: 2, FlowTodo: 1}, 3},
	}
	if len(view.Flow.Days) != len(want) {
		t.Fatalf("days = %d, want %d", len(view.Flow.Days), len(want))
	}
	for i, expected := range want {
		day := view.Flow.Days[i]
		t.Run(expected.date, func(t *testing.T) {
			if day.Date.String() != expected.date {
				t.Fatalf("date = %s, want %s", day.Date, expected.date)
			}
			if day.Total != expected.total {
				t.Errorf("total = %d, want %d", day.Total, expected.total)
			}
			for _, band := range FlowBands() {
				if day.Counts[band] != expected.bands[band] {
					t.Errorf("band %s = %d, want %d", band, day.Counts[band], expected.bands[band])
				}
			}
		})
	}
	// The bands stack bottom first, finished work at the bottom, so the top of
	// the stack is the scope.
	if view.Flow.Bands[0] != FlowDone || view.Flow.Bands[len(view.Flow.Bands)-1] != FlowUnknown {
		t.Errorf("bands = %v", view.Flow.Bands)
	}
}

func TestBuildSprintMetricsFlowStatsMatchTheReference(t *testing.T) {
	t.Parallel()

	view := BuildSprintMetrics(metricsFixtureSprint(t), MetricsInput{
		Cards:      metricsFixtureCards(),
		History:    metricsFixtureHistory(t),
		Provenance: MetricsProvenance{Source: MetricsSourceGit},
		Now:        metricsFixtureNow(t),
	})
	stats := view.Stats

	for _, tc := range []struct {
		name string
		got  float64
		want float64
	}{
		{"throughput", float64(stats.Throughput), 2},
		{"throughput per week", stats.ThroughputPerWeek, 2.8},
		// US-0001 took 30 h from in_progress to done, US-0002 took 50 h.
		{"cycle time mean", stats.CycleTime.Mean, 1.67},
		{"cycle time median", stats.CycleTime.Median, 1.25},
		{"cycle time p85", stats.CycleTime.P85, 2.08},
		{"cycle time min", stats.CycleTime.Min, 1.25},
		{"cycle time max", stats.CycleTime.Max, 2.08},
		// Lead time runs from creation: 6 d 7 h and 8 d 2 h.
		{"lead time mean", stats.LeadTime.Mean, 7.19},
		{"lead time min", stats.LeadTime.Min, 6.29},
		{"lead time max", stats.LeadTime.Max, 8.08},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !nearly(tc.got, tc.want) {
				t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
			}
		})
	}
	if stats.CycleTime.Count != 2 || stats.LeadTime.Count != 2 {
		t.Errorf("samples = %d cycle, %d lead, want 2 and 2", stats.CycleTime.Count, stats.LeadTime.Count)
	}
	if stats.Excluded != 0 {
		t.Errorf("excluded = %d, want 0", stats.Excluded)
	}
}

func TestBuildSprintMetricsDegradesHonestly(t *testing.T) {
	t.Parallel()

	sprint := metricsFixtureSprint(t)
	cards := metricsFixtureCards()
	now := metricsFixtureNow(t)

	t.Run("no history reports every day as unknown", func(t *testing.T) {
		view := BuildSprintMetrics(sprint, MetricsInput{Cards: cards, Now: now})
		if view.Provenance.Source != MetricsSourceNone || !view.Provenance.Approximate {
			t.Fatalf("provenance = %+v", view.Provenance)
		}
		if view.Provenance.Note == "" {
			t.Error("a metric without history must explain itself")
		}
		for _, point := range view.Burndown.Points {
			if point.Unknown != 3 || point.Remaining != 0 || point.Completed != 0 {
				t.Fatalf("day %d = %+v, want three unknown references and no work", point.Day, point)
			}
		}
		for _, day := range view.Flow.Days {
			if day.Counts[FlowUnknown] != 3 {
				t.Fatalf("day %d unknown = %d, want 3", day.Day, day.Counts[FlowUnknown])
			}
		}
		if view.Stats.Throughput != 0 {
			t.Errorf("throughput = %d, want 0: nothing can be claimed without history", view.Stats.Throughput)
		}
	})

	t.Run("the updated approximation places only the last transition", func(t *testing.T) {
		approx := append([]BoardCard(nil), cards...)
		approx[0].Updated = at(t, "2026-03-03T16:00:00Z")
		approx[1].Updated = at(t, "2026-03-06T11:00:00Z")
		approx[2].Updated = at(t, "2026-03-04T08:00:00Z")
		history := ApproximateHistories(approx)
		if len(history) != 3 {
			t.Fatalf("histories = %d, want 3", len(history))
		}
		for _, h := range history {
			if h.Complete {
				t.Fatalf("%s: an approximation is never complete", h.Ref)
			}
		}
		view := BuildSprintMetrics(sprint, MetricsInput{
			Cards: approx, History: history,
			Provenance: MetricsProvenance{Source: MetricsSourceUpdated}, Now: now,
		})
		if !view.Provenance.Approximate {
			t.Error("the updated approximation must be flagged as approximate")
		}
		// Day 1 knows nothing yet: every item was written later.
		if got := view.Burndown.Points[0].Unknown; got != 3 {
			t.Errorf("day 1 unknown = %d, want 3", got)
		}
		// The last day knows all three, because all three were written by then.
		last := view.Burndown.Points[4]
		if last.Unknown != 0 || !nearly(last.Remaining, 2) || !nearly(last.Done, 8) {
			t.Errorf("last day = %+v, want two points remaining and eight done", last)
		}
	})

	t.Run("a truncated git history is approximate", func(t *testing.T) {
		history := metricsFixtureHistory(t)
		for i := range history {
			history[i].Complete = false
		}
		view := BuildSprintMetrics(sprint, MetricsInput{
			Cards: cards, History: history,
			Provenance: MetricsProvenance{Source: MetricsSourceGit, Truncated: true}, Now: now,
		})
		if !view.Provenance.Approximate {
			t.Error("a truncated history must be flagged as approximate")
		}
		if view.Provenance.Covered != 3 {
			t.Errorf("covered = %d, want 3", view.Provenance.Covered)
		}
	})

	t.Run("future days carry no measurement", func(t *testing.T) {
		view := BuildSprintMetrics(sprint, MetricsInput{
			Cards: cards, History: metricsFixtureHistory(t),
			Provenance: MetricsProvenance{Source: MetricsSourceGit},
			Now:        at(t, "2026-03-03T12:00:00Z").Time,
		})
		for i, point := range view.Burndown.Points {
			observed := i < 2
			if point.Observed != observed {
				t.Errorf("day %d observed = %v, want %v", point.Day, point.Observed, observed)
			}
			if !observed && (point.Scope != 0 || point.Items != 0) {
				t.Errorf("day %d must carry no measurement: %+v", point.Day, point)
			}
			if point.Ideal == 0 && point.Day != 5 {
				t.Errorf("day %d: the ideal line exists for every day", point.Day)
			}
		}
	})

	t.Run("a sprint with no dates produces no series", func(t *testing.T) {
		view := BuildSprintMetrics(&Sprint{ID: "DEMO-TEAM-S-0002", Items: []string{"DEMO/DEMO-US-0001"}},
			MetricsInput{Cards: cards, Now: now})
		if len(view.Burndown.Points) != 0 || len(view.Flow.Days) != 0 {
			t.Errorf("points = %d, days = %d, want none", len(view.Burndown.Points), len(view.Flow.Days))
		}
	})
}

func TestHistoriesFromRevisions(t *testing.T) {
	t.Parallel()

	item := func(status string, estimate string) []byte {
		return []byte("---\nid: DEMO-US-0001\ntype: story\ntitle: One\nstatus: " +
			status + "\nestimate: " + estimate + "\n---\n\nBody.\n")
	}
	categoryOf := func(_ ProjectKey, status Status) StatusCategory {
		switch status {
		case "done":
			return CategoryDone
		case "in_progress":
			return CategoryInProgress
		default:
			return CategoryTodo
		}
	}

	for _, tc := range []struct {
		name     string
		revs     []ItemRevision
		complete bool
		want     []ItemObservation
	}{
		{
			name:     "revisions are sorted and parsed",
			complete: true,
			revs: []ItemRevision{
				{Ref: "DEMO/DEMO-US-0001", At: at(t, "2026-03-03T16:00:00Z"), Data: item("done", "3")},
				{Ref: "DEMO/DEMO-US-0001", At: at(t, "2026-03-01T09:00:00Z"), Data: item("todo", "2")},
			},
			want: []ItemObservation{
				{At: at(t, "2026-03-01T09:00:00Z"), Status: "todo", Category: CategoryTodo, Estimate: points(2)},
				{At: at(t, "2026-03-03T16:00:00Z"), Status: "done", Category: CategoryDone, Estimate: points(3)},
			},
		},
		{
			name:     "a deletion ends the history",
			complete: true,
			revs: []ItemRevision{
				{Ref: "DEMO/DEMO-US-0001", At: at(t, "2026-03-01T09:00:00Z"), Data: item("todo", "2")},
				{Ref: "DEMO/DEMO-US-0001", At: at(t, "2026-03-02T09:00:00Z"), Deleted: true},
			},
			want: []ItemObservation{
				{At: at(t, "2026-03-01T09:00:00Z"), Status: "todo", Category: CategoryTodo, Estimate: points(2)},
				{At: at(t, "2026-03-02T09:00:00Z"), Deleted: true},
			},
		},
		{
			name:     "a revision that does not parse is skipped, not guessed at",
			complete: false,
			revs: []ItemRevision{
				{Ref: "DEMO/DEMO-US-0001", At: at(t, "2026-03-01T09:00:00Z"), Data: item("todo", "2")},
				{Ref: "DEMO/DEMO-US-0001", At: at(t, "2026-03-02T09:00:00Z"), Data: []byte("not a file")},
			},
			want: []ItemObservation{
				{At: at(t, "2026-03-01T09:00:00Z"), Status: "todo", Category: CategoryTodo, Estimate: points(2)},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := HistoriesFromRevisions(tc.revs, tc.complete, categoryOf)
			if len(got) != 1 {
				t.Fatalf("histories = %d, want 1", len(got))
			}
			if got[0].Complete != tc.complete {
				t.Errorf("complete = %v, want %v", got[0].Complete, tc.complete)
			}
			if len(got[0].Observations) != len(tc.want) {
				t.Fatalf("observations = %d, want %d", len(got[0].Observations), len(tc.want))
			}
			for i, want := range tc.want {
				obs := got[0].Observations[i]
				if !obs.At.Equal(want.At.Time) || obs.Status != want.Status ||
					obs.Category != want.Category || obs.Deleted != want.Deleted {
					t.Errorf("observation %d = %+v, want %+v", i, obs, want)
				}
				if !nearly(obs.Points(), want.Points()) {
					t.Errorf("observation %d points = %v, want %v", i, obs.Points(), want.Points())
				}
			}
		})
	}
}
