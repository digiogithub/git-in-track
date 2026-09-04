package core

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// goldenSnapshot is the serialized index of the basic fixture.
const goldenSnapshot = "testdata/index/snapshot-basic.json"

func TestSnapshotGoldenRoundTrip(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	snap := ix.Snapshot()
	data, err := EncodeSnapshot(snap)
	if err != nil {
		t.Fatalf("EncodeSnapshot: %v", err)
	}

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenSnapshot), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(goldenSnapshot, data, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("golden file %s rewritten", goldenSnapshot)
	}
	want, err := os.ReadFile(goldenSnapshot)
	if err != nil {
		t.Fatalf("read golden (run go test -update to create it): %v", err)
	}
	if !bytes.Equal(data, want) {
		t.Errorf("snapshot differs from %s; run go test ./internal/core -run TestSnapshotGolden -update and review the diff\n--- got ---\n%s", goldenSnapshot, data)
	}

	t.Run("encoding is stable", func(t *testing.T) {
		again, err := EncodeSnapshot(ix.Snapshot())
		if err != nil {
			t.Fatalf("EncodeSnapshot: %v", err)
		}
		if !bytes.Equal(data, again) {
			t.Error("two encodings of the same index differ")
		}
	})

	t.Run("decode", func(t *testing.T) {
		decoded, err := DecodeSnapshot(data)
		if err != nil {
			t.Fatalf("DecodeSnapshot: %v", err)
		}
		if decoded.Schema != SnapshotSchema || decoded.Fingerprint != snap.Fingerprint {
			t.Errorf("decoded = %+v", decoded)
		}
		if len(decoded.Items) != 5 || len(decoded.Pages) != 2 || len(decoded.Comments) != 1 {
			t.Errorf("counts = %d items, %d pages, %d comments",
				len(decoded.Items), len(decoded.Pages), len(decoded.Comments))
		}
		for _, it := range decoded.Items {
			if it.ID == "DEMO-US-0001" {
				if it.AC == nil || it.AC.Total != 4 || it.AC.Done != 2 {
					t.Errorf("acceptance criteria = %+v", it.AC)
				}
				if it.Comments != 1 {
					t.Errorf("comment count = %d", it.Comments)
				}
				if it.Category != CategoryInProgress {
					t.Errorf("category = %q", it.Category)
				}
			}
		}
	})
}

func TestSnapshotCarriesNoBodies(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	data, err := EncodeSnapshot(ix.Snapshot())
	if err != nil {
		t.Fatalf("EncodeSnapshot: %v", err)
	}
	// A phrase that only exists in a body must not reach the snapshot (R-IDX-2).
	if bytes.Contains(data, []byte("Northwind is the pilot customer")) {
		t.Error("a body leaked into the snapshot")
	}
}

func TestLoadHydratesWithoutParsing(t *testing.T) {
	ix, m := buildFixtureIndex(t)
	snap := ix.Snapshot()

	hydrated := NewIndex(m, nil)
	hydrated.Now = func() time.Time { return fixtureTime }
	if err := hydrated.Load(snap); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := hydrated.Stats().Items, ix.Stats().Items; got != want {
		t.Errorf("items = %d, want %d", got, want)
	}
	if got, want := hydrated.Fingerprint(), ix.Fingerprint(); got != want {
		t.Errorf("fingerprint = %q, want %q", got, want)
	}
	it, err := hydrated.Item("DEMO-US-0001")
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if it.Title != "Guest checkout" || it.Status != "in_progress" || it.Path == "" {
		t.Errorf("item = %+v", it)
	}
	if it.Body != "" {
		t.Error("a hydrated item must not claim to have a body")
	}
	if got := hydrated.Children("DEMO-EP-0001"); len(got) != 2 {
		t.Errorf("children = %v", got)
	}
	if got := hydrated.CommentCount("DEMO-US-0001"); got != 1 {
		t.Errorf("comment count = %d", got)
	}
	back := hydrated.LinkGraph().Backlinks(ItemNode("DEMO-EP-0001"))
	if len(back) == 0 {
		t.Error("the wikilink graph was not restored")
	}
	if _, ok := hydrated.Page("docs/architecture/overview.md"); !ok {
		t.Error("pages were not restored")
	}

	t.Run("re-serializing gives the same document", func(t *testing.T) {
		first, err := EncodeSnapshot(snap)
		if err != nil {
			t.Fatalf("EncodeSnapshot: %v", err)
		}
		second, err := EncodeSnapshot(hydrated.Snapshot())
		if err != nil {
			t.Fatalf("EncodeSnapshot: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Errorf("round trip is not stable\n--- first ---\n%s\n--- second ---\n%s", first, second)
		}
	})

	t.Run("an incremental build over a hydrated index parses nothing", func(t *testing.T) {
		projects, err := DiscoverProjects(m, ".")
		if err != nil {
			t.Fatalf("DiscoverProjects: %v", err)
		}
		warm := NewIndex(m, projects)
		warm.Now = func() time.Time { return fixtureTime }
		if err := warm.Load(snap); err != nil {
			t.Fatalf("Load: %v", err)
		}
		stats, err := warm.Build(context.Background(), false)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if stats.Parsed != 0 {
			t.Errorf("parsed %d files, want 0: the cache was not trusted", stats.Parsed)
		}
	})

	t.Run("a changed file is re-read", func(t *testing.T) {
		projects, err := DiscoverProjects(m, ".")
		if err != nil {
			t.Fatalf("DiscoverProjects: %v", err)
		}
		warm := NewIndex(m, projects)
		if err := warm.Load(snap); err != nil {
			t.Fatalf("Load: %v", err)
		}
		const story = "docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md"
		data, err := m.ReadFile(story)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		m.Now = func() time.Time { return fixtureTime.Add(time.Hour) }
		if err := m.WriteFile(story, append(data, []byte("\nAn extra paragraph.\n")...)); err != nil {
			t.Fatalf("write: %v", err)
		}
		defer func() { m.Now = func() time.Time { return fixtureTime } }()
		stats, err := warm.Build(context.Background(), false)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if stats.Parsed != 1 {
			t.Errorf("parsed %d files, want exactly the changed one", stats.Parsed)
		}
		if warm.Fingerprint() == snap.Fingerprint {
			t.Error("the fingerprint did not change with the content")
		}
	})
}

func TestLoadRejectsAnotherSchema(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	snap := ix.Snapshot()
	snap.Schema = 99
	if err := NewIndex(NewMemFS(), nil).Load(snap); err == nil {
		t.Error("want an error for an unsupported schema")
	}
}

func TestFingerprintChangesWithContent(t *testing.T) {
	ix, m := buildFixtureIndex(t)
	before := ix.Fingerprint()

	const story = "docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md"
	data, err := m.ReadFile(story)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := m.WriteFile(story, append(data, []byte("\nmore text\n")...)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ix.ApplyFileEvents(context.Background(), []FileEvent{{Kind: FileModified, Path: story}}); err != nil {
		t.Fatalf("ApplyFileEvents: %v", err)
	}
	if ix.Fingerprint() == before {
		t.Error("the fingerprint did not change")
	}
}

func TestProjectSnapshot(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)

	snap, err := ix.ProjectSnapshot("DEMO", ProjectSnapshotOptions{
		GeneratedBy:   "jose",
		Repo:          "https://github.com/example/demo-shop.git",
		DefaultBranch: "main",
		Source:        &SnapshotSource{Commit: "9c1f0a2e", Branch: "main"},
		Now:           now,
	})
	if err != nil {
		t.Fatalf("ProjectSnapshot: %v", err)
	}
	if snap.Schema != SnapshotSchema || snap.Project.Key != "DEMO" || snap.Project.DocsPath != "docs" {
		t.Errorf("meta = %+v", snap.Project)
	}
	if len(snap.Workflow) != 6 || snap.Workflow[0].ID != "backlog" {
		t.Errorf("workflow = %+v", snap.Workflow)
	}
	if len(snap.Labels) != 3 {
		t.Errorf("labels = %+v", snap.Labels)
	}
	if len(snap.Items) != 5 {
		t.Errorf("items = %d, want 5", len(snap.Items))
	}
	for i := 1; i < len(snap.Items); i++ {
		if snap.Items[i-1].ID >= snap.Items[i].ID {
			t.Fatalf("items are not sorted by id: %v", snap.Items)
		}
	}
	if snap.Counts[TypeStory] != 2 {
		t.Errorf("counts = %v", snap.Counts)
	}

	data, err := EncodeProjectSnapshot(snap)
	if err != nil {
		t.Fatalf("EncodeProjectSnapshot: %v", err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Error("the document must end with a newline")
	}
	if !bytes.Contains(data, []byte(`  "schema": 1`)) {
		t.Error("the document must use two-space indentation")
	}
	if bytes.Contains(data, []byte("As a shopper")) {
		t.Error("a body leaked into a committed snapshot")
	}

	t.Run("closed items age out", func(t *testing.T) {
		m := NewMemFSFromMap(map[string]string{
			"docs/.pmngr/project.yaml": "schema: 1\nkey: OLD\ndocs:\n  path: docs\n" +
				"workflow:\n  statuses:\n    - {id: todo, category: todo}\n" +
				"    - {id: done, category: done, terminal: true}\n",
			"docs/.pmngr/stories/OLD-US-0001-ancient.md": "---\nid: OLD-US-0001\ntype: story\ntitle: Ancient\n" +
				"status: done\ncreated: 2026-01-01T08:00:00Z\nupdated: 2026-01-02T08:00:00Z\n---\n\ndone long ago\n",
			"docs/.pmngr/stories/OLD-US-0002-recent.md": "---\nid: OLD-US-0002\ntype: story\ntitle: Recent\n" +
				"status: done\ncreated: 2026-09-01T08:00:00Z\nupdated: 2026-09-02T08:00:00Z\n---\n\njust done\n",
			"docs/.pmngr/stories/OLD-US-0003-open.md": "---\nid: OLD-US-0003\ntype: story\ntitle: Open\n" +
				"status: todo\ncreated: 2026-01-01T08:00:00Z\nupdated: 2026-01-02T08:00:00Z\n---\n\nstill open\n",
		})
		projects, err := DiscoverProjects(m, ".")
		if err != nil {
			t.Fatalf("DiscoverProjects: %v", err)
		}
		old := NewIndex(m, projects)
		if _, err := old.Build(context.Background(), true); err != nil {
			t.Fatalf("Build: %v", err)
		}
		snap, err := old.ProjectSnapshot("OLD", ProjectSnapshotOptions{Now: now})
		if err != nil {
			t.Fatalf("ProjectSnapshot: %v", err)
		}
		var ids []ItemID
		for _, it := range snap.Items {
			ids = append(ids, it.ID)
		}
		if !reflect.DeepEqual(ids, []ItemID{"OLD-US-0002", "OLD-US-0003"}) {
			t.Errorf("items = %v, want the recent and the open one", ids)
		}
		all, err := old.ProjectSnapshot("OLD", ProjectSnapshotOptions{Now: now, IncludeClosed: true})
		if err != nil {
			t.Fatalf("ProjectSnapshot: %v", err)
		}
		if len(all.Items) != 3 {
			t.Errorf("with IncludeClosed: %d items, want 3", len(all.Items))
		}
	})

	t.Run("unknown project", func(t *testing.T) {
		if _, err := ix.ProjectSnapshot("NOPE", ProjectSnapshotOptions{}); err == nil {
			t.Error("want an error for a project that is not indexed")
		}
	})
}

func TestProjectSnapshotPath(t *testing.T) {
	if got := ProjectSnapshotPath(".pmngr", "ACME"); got != ".pmngr/index/ACME.json" {
		t.Errorf("path = %q", got)
	}
}
