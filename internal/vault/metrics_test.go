package vault

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The metrics surface of GIT-US-0028 over a workspace, in both hosting modes.
// The point of these cases is the provenance: the same call answers with a
// reconstruction where a history reader is installed and with a stated
// approximation where none is, and never with a series it made up.

// stubHistory is a HistorySource that replays a fixed set of revisions for
// whatever path it is asked about, so the test can assert the wiring without a
// git repository.
type stubHistory struct {
	revisions map[string][]RepoRevision
	fail      bool
	asked     [][]string
}

func (s *stubHistory) FileHistory(_ context.Context, _ string, paths []string) (RepoHistory, error) {
	s.asked = append(s.asked, append([]string(nil), paths...))
	if s.fail {
		return RepoHistory{}, failf("internal", "no history here")
	}
	out := RepoHistory{Commits: 0}
	for _, path := range paths {
		revs, ok := s.revisions[path]
		if !ok {
			continue
		}
		out.Revisions = append(out.Revisions, revs...)
		out.Commits += len(revs)
		for _, rev := range revs {
			if out.Oldest.IsZero() || rev.At.Before(out.Oldest) {
				out.Oldest = rev.At
			}
		}
	}
	return out, nil
}

// guestCheckoutRevisions is the history of the fixture's only cloned story,
// written by hand: todo before the sprint, in progress on its second day, done
// on its fourth.
func guestCheckoutRevisions() map[string][]RepoRevision {
	const path = "docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md"
	item := func(status string) []byte {
		return []byte("---\nid: DEMO-US-0001\ntype: story\ntitle: Guest checkout\n" +
			"status: " + status + "\nestimate: 8\n---\n\nBody.\n")
	}
	stamp := func(s string) time.Time {
		at, err := time.Parse(time.RFC3339, s)
		if err != nil {
			panic(err)
		}
		return at
	}
	return map[string][]RepoRevision{path: {
		{Path: path, At: stamp("2026-08-20T09:00:00Z"), Data: item("todo")},
		{Path: path, At: stamp("2026-08-25T10:00:00Z"), Data: item("in_progress")},
		{Path: path, At: stamp("2026-08-27T16:00:00Z"), Data: item("done")},
	}}
}

func TestWorkspaceSprintMetrics(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		t.Run("without a history reader the series is a stated approximation", func(t *testing.T) {
			view := decode[core.SprintMetricsView](t, wsCall(t, w, "sprint.metrics",
				map[string]any{"id": "DEMO-TEAM-S-0001"}))
			if view.Provenance.Source != core.MetricsSourceUpdated {
				t.Fatalf("source = %q, want %q", view.Provenance.Source, core.MetricsSourceUpdated)
			}
			if !view.Provenance.Approximate {
				t.Error("an approximation must be flagged as one")
			}
			if !strings.Contains(view.Provenance.Note, "updated") {
				t.Errorf("note = %q: it must name the approximation", view.Provenance.Note)
			}
			if view.Provenance.Items != 3 {
				t.Errorf("items = %d, want 3", view.Provenance.Items)
			}
			if len(view.Burndown.Points) != 14 || len(view.Flow.Days) != 14 {
				t.Fatalf("points = %d, days = %d, want 14 of each",
					len(view.Burndown.Points), len(view.Flow.Days))
			}
			if len(view.Items) != 3 {
				t.Errorf("items table = %d rows, want 3", len(view.Items))
			}
			if view.Sprint.ID != "DEMO-TEAM-S-0001" {
				t.Errorf("sprint = %q", view.Sprint.ID)
			}
		})

		t.Run("a history reader is asked only about the cloned item files", func(t *testing.T) {
			stub := &stubHistory{revisions: guestCheckoutRevisions()}
			w.SetHistorySource(stub)
			defer w.SetHistorySource(nil)

			view := decode[core.SprintMetricsView](t, wsCall(t, w, "sprint.metrics",
				map[string]any{"id": "DEMO-TEAM-S-0001"}))
			if view.Provenance.Source != core.MetricsSourceGit {
				t.Fatalf("source = %q, want %q", view.Provenance.Source, core.MetricsSourceGit)
			}
			if view.Provenance.Approximate {
				t.Error("a complete git history is not an approximation")
			}
			// The snapshot-resolved WEB card lives in a repository nobody
			// cloned, so it has no readable history and stays unknown.
			if view.Provenance.Covered != 1 {
				t.Errorf("covered = %d, want 1", view.Provenance.Covered)
			}
			if len(stub.asked) != 1 {
				t.Fatalf("history was asked %d times, want once", len(stub.asked))
			}
			for _, path := range stub.asked[0] {
				if strings.Contains(path, "WEB") {
					t.Errorf("the reader was asked about a repository nobody cloned: %s", path)
				}
			}
			// Day 2 of the sprint is 2026-08-25: the story is in progress, so
			// its eight points are still remaining.
			day2 := view.Burndown.Points[1]
			if day2.Date.String() != "2026-08-25" {
				t.Fatalf("day 2 = %s", day2.Date)
			}
			if day2.Remaining != 8 || day2.Completed != 0 {
				t.Errorf("day 2 = %+v, want eight points remaining", day2)
			}
			// Day 4 is 2026-08-27: it was finished at 16:00 that day.
			day4 := view.Burndown.Points[3]
			if day4.Done != 8 || day4.Completed != 1 {
				t.Errorf("day 4 = %+v, want the story done", day4)
			}
			if view.Stats.Throughput != 1 {
				t.Errorf("throughput = %d, want 1", view.Stats.Throughput)
			}
			if view.Stats.CycleTime.Count != 1 || view.Stats.CycleTime.Max < 2 {
				t.Errorf("cycle time = %+v, want one sample of about 2.25 days", view.Stats.CycleTime)
			}
		})

		t.Run("a reader that fails falls back rather than failing the call", func(t *testing.T) {
			w.SetHistorySource(&stubHistory{fail: true})
			defer w.SetHistorySource(nil)

			view := decode[core.SprintMetricsView](t, wsCall(t, w, "sprint.metrics",
				map[string]any{"id": "DEMO-TEAM-S-0001"}))
			if view.Provenance.Source != core.MetricsSourceUpdated {
				t.Errorf("source = %q, want the approximation", view.Provenance.Source)
			}
		})

		t.Run("an unknown sprint is not found", func(t *testing.T) {
			code, _ := wsFail(t, w, "sprint.metrics", map[string]any{"id": "DEMO-TEAM-S-0099"})
			if code != "not_found" {
				t.Errorf("code = %q, want not_found", code)
			}
		})
	})
}

// refusingHistory is a HistorySource that fails the test if it is ever asked:
// a sprint that carries a snapshot must answer from the frozen block and walk
// no history at all (R-MET-12).
type refusingHistory struct{ t *testing.T }

func (r refusingHistory) FileHistory(context.Context, string, []string) (RepoHistory, error) {
	r.t.Helper()
	r.t.Fatal("a snapshot-backed sprint must not touch the history source")
	return RepoHistory{}, nil
}

// TestSprintCloseFreezesTheSnapshot covers R-SPR-11 and R-MET-12 end to end:
// the close writes the block once, a re-close leaves it alone, and the metrics
// read it back instead of reconstructing anything.
func TestSprintCloseFreezesTheSnapshot(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		t.Run("a dry run freezes nothing", func(t *testing.T) {
			view := sprintOf(t, w, "DEMO-TEAM-S-0001")
			result := decode[SprintResult](t, wsCall(t, w, "sprint.close", map[string]any{
				"id": "DEMO-TEAM-S-0001", "rev": string(view.Sprint.Rev), "dryRun": true,
			}))
			if !result.DryRun || result.Sprint.Sprint.Snapshot != nil {
				t.Fatalf("a dry run writes nothing: %+v", result.Sprint.Sprint.Snapshot)
			}
			if again := sprintOf(t, w, "DEMO-TEAM-S-0001"); again.Sprint.Snapshot != nil {
				t.Fatal("the sprint file gained a snapshot from a preview")
			}
		})

		var closedAt core.Timestamp
		t.Run("closing writes the snapshot with the state", func(t *testing.T) {
			view := sprintOf(t, w, "DEMO-TEAM-S-0001")
			result := decode[SprintResult](t, wsCall(t, w, "sprint.close", map[string]any{
				"id": "DEMO-TEAM-S-0001", "rev": string(view.Sprint.Rev),
			}))
			snap := result.Sprint.Sprint.Snapshot
			if result.Sprint.Sprint.State != core.SprintClosed || snap == nil {
				t.Fatalf("close = %+v", result.Sprint.Sprint)
			}
			if snap.ClosedAt.IsZero() || snap.Totals.Items != 3 {
				t.Fatalf("snapshot = %+v", snap)
			}
			// No history source is installed in either mode here, which is
			// browser-only mode: the frozen numbers say they are approximate.
			if !snap.Provenance.Approximate {
				t.Fatalf("provenance = %+v, want approximate", snap.Provenance)
			}
			closedAt = snap.ClosedAt
			// It survives the round-trip through the file.
			stored := sprintOf(t, w, "DEMO-TEAM-S-0001")
			if stored.Sprint.Snapshot == nil || stored.Sprint.Snapshot.ClosedAt != closedAt {
				t.Fatalf("stored snapshot = %+v", stored.Sprint.Snapshot)
			}
		})

		t.Run("closing again never recomputes it", func(t *testing.T) {
			view := sprintOf(t, w, "DEMO-TEAM-S-0001")
			result := decode[SprintResult](t, wsCall(t, w, "sprint.close", map[string]any{
				"id": "DEMO-TEAM-S-0001", "rev": string(view.Sprint.Rev),
			}))
			snap := result.Sprint.Sprint.Snapshot
			if snap == nil || snap.ClosedAt != closedAt {
				t.Fatalf("the snapshot was recomputed: %+v", snap)
			}
		})

		t.Run("the metrics read the frozen block and walk no history", func(t *testing.T) {
			w.SetHistorySource(refusingHistory{t: t})
			defer w.SetHistorySource(nil)

			view := decode[core.SprintMetricsView](t, wsCall(t, w, "sprint.metrics",
				map[string]any{"id": "DEMO-TEAM-S-0001"}))
			if view.Provenance.Source != core.MetricsSourceSnapshot {
				t.Fatalf("source = %q, want %q", view.Provenance.Source, core.MetricsSourceSnapshot)
			}
			if !strings.Contains(view.Provenance.Note, "frozen") {
				t.Fatalf("note = %q: it must say the numbers were frozen", view.Provenance.Note)
			}
		})

		t.Run("an open sprint still reconstructs its series", func(t *testing.T) {
			wsCall(t, w, "sprint.create", map[string]any{
				"board": "demo-scrum", "title": "Open", "start": "2031-02-03", "end": "2031-02-16",
			})
			open := decode[SprintListResult](t, wsCall(t, w, "sprint.list", map[string]any{"board": "demo-scrum"}))
			var id string
			for _, s := range open.Sprints {
				if s.Title == "Open" {
					id = s.ID
				}
			}
			if id == "" {
				t.Fatalf("sprints = %+v", open.Sprints)
			}
			view := decode[core.SprintMetricsView](t, wsCall(t, w, "sprint.metrics", map[string]any{"id": id}))
			if view.Provenance.Source != core.MetricsSourceUpdated {
				t.Fatalf("source = %q, want the live approximation", view.Provenance.Source)
			}
		})
	})
}
