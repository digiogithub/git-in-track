package core

import (
	"context"
	"math"
	"strings"
	"testing"
)

// kpiVault builds a one-project index from files, plus a team-less second
// project so the counts are shown to stay per project.
func kpiVault(t *testing.T, files map[string]string) *Index {
	t.Helper()
	fsys := NewMemFS()
	for p, data := range files {
		if err := fsys.MkdirAll(p[:strings.LastIndex(p, "/")]); err != nil {
			t.Fatal(err)
		}
		if err := fsys.WriteFile(p, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	projects, err := DiscoverProjects(fsys, ".")
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(fsys, projects)
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatalf("Build(): %v", err)
	}
	return ix
}

func kpiItem(id, typ, status, extra string) string {
	return "---\nid: " + id + "\ntype: " + typ + "\ntitle: " + id + "\nstatus: " + status +
		"\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n" + extra + "---\n\nBody.\n"
}

const kpiRequirementBlock = "\nThe system SHALL act.\n\n#### Scenario: acts\n- **WHEN** asked\n- **THEN** it acts\n\n"

func kpiSpec(id, status, entries string) string {
	return "---\nid: " + id + "\ntype: spec\ntitle: " + id + "\nstatus: " + status +
		"\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n" + entries + "---\n\n" +
		"## Requirements\n\n### " + id + ".R1 — One\n" + kpiRequirementBlock +
		"### " + id + ".R2 — Two\n" + kpiRequirementBlock
}

func kpiFiles(stories map[string]string) map[string]string {
	files := map[string]string{
		"docs/.pmngr/project.yaml": "schema: 2\nkey: ACME\nname: Acme\nworkflow:\n  statuses:\n" +
			"    - {id: inbox, category: triage}\n    - {id: todo, category: todo}\n" +
			"    - {id: doing, category: in_progress}\n    - {id: done, category: done}\n" +
			"    - {id: cancelled, category: cancelled}\n",
		"docs/guide.md":    "# Guide\n",
		"docs/kb/howto.md": "# How to\n",
		"other/.pmngr/project.yaml": "schema: 2\nkey: OTHER\nname: Other\nworkflow:\n  statuses:\n" +
			"    - {id: todo, category: todo}\n    - {id: done, category: done}\n",
		"other/readme.md": "# Other\n",
		"other/.pmngr/stories/OTHER-US-0001-elsewhere.md":  kpiItem("OTHER-US-0001", "story", "done", ""),
		"docs/.pmngr/milestones/ACME-M-0001-one.md":        kpiItem("ACME-M-0001", "milestone", "done", ""),
		"docs/.pmngr/milestones/ACME-M-0002-two.md":        kpiItem("ACME-M-0002", "milestone", "todo", ""),
		"docs/.pmngr/epics/ACME-EP-0001-open.md":           kpiItem("ACME-EP-0001", "epic", "doing", ""),
		"docs/.pmngr/epics/ACME-EP-0002-cancelled.md":      kpiItem("ACME-EP-0002", "epic", "cancelled", ""),
		"docs/.pmngr/epics/ACME-EP-0003-deleted.md":        kpiItem("ACME-EP-0003", "epic", "todo", "deleted: true\n"),
		"docs/.pmngr/stories/ACME-US-0090-in-the-inbox.md": kpiItem("ACME-US-0090", "story", "inbox", ""),
		"docs/.pmngr/specs/ACME-SP-0002-cancelled-spec.md": kpiSpec("ACME-SP-0002", "cancelled", ""),
		"docs/.pmngr/specs/ACME-SP-0001-allocation.md":     kpiSpec("ACME-SP-0001", "todo", ""),
	}
	for name, data := range stories {
		files["docs/.pmngr/stories/"+name] = data
	}
	return files
}

func TestProjectKPIs(t *testing.T) {
	t.Parallel()

	t.Run("counts by category and leaves out deleted and triage items", func(t *testing.T) {
		t.Parallel()
		ix := kpiVault(t, kpiFiles(map[string]string{
			"ACME-US-0001-a.md": kpiItem("ACME-US-0001", "story", "done", ""),
			"ACME-US-0002-b.md": kpiItem("ACME-US-0002", "story", "todo", ""),
			"ACME-US-0003-c.md": kpiItem("ACME-US-0003", "story", "doing", ""),
			"ACME-US-0004-d.md": kpiItem("ACME-US-0004", "story", "cancelled", ""),
		}))
		got, ok := ix.ProjectKPIs("ACME")
		if !ok {
			t.Fatal("ProjectKPIs(ACME) found no project")
		}
		if got.KBPages != 2 {
			t.Errorf("KBPages = %d, want 2", got.KBPages)
		}
		if got.Milestones != (KPICount{Total: 2, Open: 1}) {
			t.Errorf("Milestones = %+v, want {2 1}", got.Milestones)
		}
		if got.Epics != (KPICount{Total: 2, Open: 1}) {
			t.Errorf("Epics = %+v, want {2 1}", got.Epics)
		}
		want := StoryKPIs{Total: 4, Open: 2, Done: 1, Cancelled: 1}
		if got.Stories != want {
			t.Errorf("Stories = %+v, want %+v", got.Stories, want)
		}
		if got.Progress.Weighted || got.Progress.Done != 1 || got.Progress.Scope != 3 {
			t.Errorf("Progress = %+v, want 1 of 3 stories by count", got.Progress)
		}
		if math.Abs(got.Progress.Ratio-1.0/3) > 1e-9 {
			t.Errorf("Ratio = %v, want 1/3", got.Progress.Ratio)
		}
	})

	t.Run("weights by estimate, an unestimated story weighing the mean", func(t *testing.T) {
		t.Parallel()
		ix := kpiVault(t, kpiFiles(map[string]string{
			"ACME-US-0001-a.md": kpiItem("ACME-US-0001", "story", "done", "estimate: 8\n"),
			"ACME-US-0002-b.md": kpiItem("ACME-US-0002", "story", "todo", "estimate: 2\n"),
			"ACME-US-0003-c.md": kpiItem("ACME-US-0003", "story", "todo", ""),
			"ACME-US-0004-d.md": kpiItem("ACME-US-0004", "story", "cancelled", "estimate: 13\n"),
		}))
		got, _ := ix.ProjectKPIs("ACME")
		// Mean of 8 and 2 is 5: scope 8 + 2 + 5 = 15, done 8.
		want := KPIProgress{Ratio: 8.0 / 15, Weighted: true, Done: 8, Scope: 15}
		if got.Progress != want {
			t.Errorf("Progress = %+v, want %+v", got.Progress, want)
		}
		if got.Stories.Estimated != 2 {
			t.Errorf("Estimated = %d, want 2", got.Stories.Estimated)
		}
	})

	t.Run("no stories is zero progress", func(t *testing.T) {
		t.Parallel()
		ix := kpiVault(t, kpiFiles(nil))
		got, _ := ix.ProjectKPIs("ACME")
		if got.Progress != (KPIProgress{}) {
			t.Errorf("Progress = %+v, want zero", got.Progress)
		}
	})

	t.Run("a spec is unverified until every live requirement is stamped on its text", func(t *testing.T) {
		t.Parallel()
		files := kpiFiles(nil)
		got, _ := kpiVault(t, files).ProjectKPIs("ACME")
		want := SpecKPIs{Total: 2, Unverified: 1, Requirements: 2, UnverifiedRequirements: 2}
		if got.Specs != want {
			t.Fatalf("Specs = %+v, want %+v", got.Specs, want)
		}

		// Stamp R1 on its current block rev and cancel R2: the spec is verified.
		views, err := kpiVault(t, files).Requirements(RequirementFilter{Spec: "ACME-SP-0001"})
		if err != nil || len(views) != 2 {
			t.Fatalf("Requirements() = %v, %v", views, err)
		}
		stamp := func(rev Rev) string {
			return "requirements:\n  R1:\n    status: done\n    verified: {rev: \"" + string(rev) +
				"\", commit: 9c1f0a2e5b7d4c3e8f1a6b2d9e0c7f4a3b5d8e21, at: 2026-10-01T09:12:00Z, by: claude}\n" +
				"  R2:\n    status: cancelled\n"
		}
		files["docs/.pmngr/specs/ACME-SP-0001-allocation.md"] = kpiSpec("ACME-SP-0001", "todo", stamp(views[0].BlockRev))
		got, _ = kpiVault(t, files).ProjectKPIs("ACME")
		want = SpecKPIs{Total: 2, Unverified: 0, Requirements: 2, UnverifiedRequirements: 0}
		if got.Specs != want {
			t.Errorf("stamped: Specs = %+v, want %+v", got.Specs, want)
		}

		// A stamp of another text is stale: R1 needs verifying again.
		files["docs/.pmngr/specs/ACME-SP-0001-allocation.md"] = kpiSpec("ACME-SP-0001", "todo", stamp("sha256:0000000000000000"))
		got, _ = kpiVault(t, files).ProjectKPIs("ACME")
		want = SpecKPIs{Total: 2, Unverified: 1, Requirements: 2, UnverifiedRequirements: 1}
		if got.Specs != want {
			t.Errorf("stale stamp: Specs = %+v, want %+v", got.Specs, want)
		}
	})

	t.Run("an unknown project is not found", func(t *testing.T) {
		t.Parallel()
		if _, ok := kpiVault(t, kpiFiles(nil)).ProjectKPIs("NOPE"); ok {
			t.Error("ProjectKPIs(NOPE) reported a project")
		}
	})
}
