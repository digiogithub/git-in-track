package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixtureTime is the modification time every file of a fixture file system gets,
// so that a snapshot golden file does not depend on when the repository was
// checked out.
var fixtureTime = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// basicFixture is the vault used by most tests of this file.
const basicFixture = "../../testdata/fixtures/project-basic"

// testDirFS is a read-only FS over a real directory. It exists only in tests:
// the package itself must stay free of "os" and "path/filepath" so that it
// compiles for GOOS=js GOARCH=wasm.
type testDirFS struct{ root string }

func (f testDirFS) host(p string) string { return filepath.Join(f.root, filepath.FromSlash(p)) }

func (f testDirFS) ReadFile(p string) ([]byte, error) {
	data, err := os.ReadFile(f.host(p))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", p, ErrNotExist)
	}
	return data, err
}

func (f testDirFS) WriteFile(string, []byte) error { return ErrReadOnly }
func (f testDirFS) Remove(string) error            { return ErrReadOnly }
func (f testDirFS) Rename(string, string) error    { return ErrReadOnly }
func (f testDirFS) MkdirAll(string) error          { return ErrReadOnly }

func (f testDirFS) Stat(p string) (FileInfo, error) {
	info, err := os.Stat(f.host(p))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FileInfo{}, fmt.Errorf("stat %s: %w", p, ErrNotExist)
		}
		return FileInfo{}, err
	}
	return FileInfo{Name: info.Name(), Size: info.Size(), ModTime: info.ModTime(), IsDir: info.IsDir()}, nil
}

func (f testDirFS) ReadDir(p string) ([]DirEntry, error) {
	entries, err := os.ReadDir(f.host(p))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read dir %s: %w", p, ErrNotExist)
		}
		return nil, err
	}
	out := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, DirEntry{Name: e.Name(), IsDir: e.IsDir()})
	}
	return out, nil
}

// loadFixture copies a directory of the repository into a MemFS with pinned
// modification times, so that every test sees the same (size, mtime) triples.
func loadFixture(t testing.TB, root string) *MemFS {
	t.Helper()
	m := NewMemFS()
	m.Now = func() time.Time { return fixtureTime }
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return m.WriteFile(filepath.ToSlash(rel), data)
	})
	if err != nil {
		t.Fatalf("load fixture %s: %v", root, err)
	}
	return m
}

// buildFixtureIndex discovers and builds the basic fixture in memory.
func buildFixtureIndex(t testing.TB) (*Index, *MemFS) {
	t.Helper()
	m := loadFixture(t, basicFixture)
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	ix := NewIndex(m, projects)
	ix.Now = func() time.Time { return fixtureTime }
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ix, m
}

func TestDiscoverProjects(t *testing.T) {
	m := loadFixture(t, basicFixture)
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("got %d projects, want 1", len(projects))
	}
	p := projects[0]
	switch {
	case p.Key != "DEMO":
		t.Errorf("key = %q, want DEMO", p.Key)
	case p.DocsPath != "docs":
		t.Errorf("docs path = %q, want docs", p.DocsPath)
	case p.BacklogPath != "docs/.pmngr":
		t.Errorf("backlog path = %q", p.BacklogPath)
	case p.ConfigPath != "docs/.pmngr/project.yaml":
		t.Errorf("config path = %q", p.ConfigPath)
	case p.Config == nil:
		t.Fatal("config was not loaded")
	case p.Config.InitialStatus() != "backlog":
		t.Errorf("initial status = %q", p.Config.InitialStatus())
	}
	for _, d := range p.Diagnostics {
		if d.Severity == SeverityError {
			t.Errorf("unexpected project error: %s", d)
		}
	}
}

func TestDiscoverProjectsSkipsNoiseDirectories(t *testing.T) {
	m := NewMemFSFromMap(map[string]string{
		".git/config":                          "[core]\n",
		"node_modules/pkg/.pmngr/project.yaml": "schema: 1\nkey: NOPE\n",
		"docs/.pmngr/project.yaml":             "schema: 1\nkey: OK\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\n    - {id: done, category: done}\n",
	})
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].Key != "OK" {
		t.Fatalf("got %+v, want one project OK", projects)
	}
}

func TestBuildFixture(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	stats := ix.Stats()

	if stats.Items != 5 {
		t.Errorf("items = %d, want 5", stats.Items)
	}
	if stats.Comments != 1 {
		t.Errorf("comments = %d, want 1", stats.Comments)
	}
	if stats.Pages != 2 {
		t.Errorf("pages = %d, want 2", stats.Pages)
	}
	want := map[ItemType]int{TypeEpic: 1, TypeStory: 2, TypeTask: 1, TypeMilestone: 1}
	for typ, n := range want {
		if stats.ByType[typ] != n {
			t.Errorf("by type %s = %d, want %d", typ, stats.ByType[typ], n)
		}
	}
	if stats.Errors != 0 {
		for _, d := range ix.Warnings() {
			if d.Severity == SeverityError {
				t.Errorf("unexpected error diagnostic: %s", d)
			}
		}
	}
	if !strings.HasPrefix(ix.Fingerprint(), "sha256:") {
		t.Errorf("fingerprint = %q", ix.Fingerprint())
	}

	it, err := ix.Item("DEMO-US-0001")
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if it.Title != "Guest checkout" || it.Status != "in_progress" {
		t.Errorf("item = %+v", it)
	}
	if it.Body == "" {
		t.Error("body was not kept in memory")
	}
	if got := ix.CommentCount("DEMO-US-0001"); got != 1 {
		t.Errorf("comment count = %d, want 1", got)
	}
}

func TestBuildIsIncrementalWithoutFull(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	stats, err := ix.Build(context.Background(), false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if stats.Parsed != 0 {
		t.Errorf("parsed %d files on an unchanged tree, want 0", stats.Parsed)
	}
	if stats.Items != 5 {
		t.Errorf("items = %d, want 5", stats.Items)
	}
}

func TestBuildRepositoryBacklog(t *testing.T) {
	root := "../.."
	if _, err := os.Stat(filepath.Join(root, "docs", ".pmngr", "project.yaml")); err != nil {
		t.Skipf("repository backlog not available: %v", err)
	}
	fs := testDirFS{root: root}
	projects, err := DiscoverProjects(fs, "docs")
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].Key != "GIT" {
		t.Fatalf("got %+v, want the GIT project", projects)
	}
	ix := NewIndex(fs, projects)
	stats, err := ix.Build(context.Background(), true)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if stats.Items < 30 {
		t.Errorf("items = %d, want the dogfood backlog", stats.Items)
	}
	if stats.Pages < 10 {
		t.Errorf("pages = %d, want the planning documents", stats.Pages)
	}
	if _, err := ix.Item("GIT-US-0007"); err != nil {
		t.Errorf("GIT-US-0007: %v", err)
	}
	for _, d := range ix.Warnings() {
		if d.Severity == SeverityError {
			t.Errorf("unexpected error diagnostic: %s", d)
		}
	}
}

func TestApplyFileEventsAddsModifiesAndRemoves(t *testing.T) {
	ix, m := buildFixtureIndex(t)
	ctx := context.Background()
	const newPath = "docs/.pmngr/stories/DEMO-US-0003-track-refunds.md"

	t.Run("create", func(t *testing.T) {
		body := "---\nid: DEMO-US-0003\ntype: story\ntitle: Track refunds\nstatus: todo\n" +
			"created: 2026-09-02T09:00:00Z\nupdated: 2026-09-02T09:00:00Z\nparent: DEMO-EP-0001\n---\n\n" +
			"## Description\n\nRefunds are tracked in [[architecture/overview]].\n"
		if err := m.WriteFile(newPath, []byte(body)); err != nil {
			t.Fatalf("write: %v", err)
		}
		delta, err := ix.ApplyFileEvents(ctx, []FileEvent{{Kind: FileCreated, Path: newPath}})
		if err != nil {
			t.Fatalf("ApplyFileEvents: %v", err)
		}
		if len(delta.Added) != 1 || delta.Added[0] != "DEMO-US-0003" {
			t.Fatalf("added = %v", delta.Added)
		}
		if len(delta.Updated) != 0 || len(delta.Removed) != 0 {
			t.Errorf("delta = %+v", delta)
		}
		if ix.Stats().Items != 6 {
			t.Errorf("items = %d, want 6", ix.Stats().Items)
		}
		refs := ix.LinkGraph().References(ItemNode("DEMO-US-0003"))
		if len(refs) != 1 || !refs[0].Resolved {
			t.Errorf("references = %+v", refs)
		}
	})

	t.Run("modify", func(t *testing.T) {
		body := "---\nid: DEMO-US-0003\ntype: story\ntitle: Track refunds\nstatus: in_progress\n" +
			"created: 2026-09-02T09:00:00Z\nupdated: 2026-09-02T11:00:00Z\nparent: DEMO-EP-0001\n---\n\n" +
			"## Description\n\nRefunds are tracked in [[architecture/overview]].\n"
		if err := m.WriteFile(newPath, []byte(body)); err != nil {
			t.Fatalf("write: %v", err)
		}
		delta, err := ix.ApplyFileEvents(ctx, []FileEvent{{Kind: FileModified, Path: newPath}})
		if err != nil {
			t.Fatalf("ApplyFileEvents: %v", err)
		}
		if len(delta.Updated) != 1 || delta.Updated[0] != "DEMO-US-0003" {
			t.Fatalf("updated = %v", delta.Updated)
		}
		it, err := ix.Item("DEMO-US-0003")
		if err != nil {
			t.Fatalf("Item: %v", err)
		}
		if it.Status != "in_progress" {
			t.Errorf("status = %q", it.Status)
		}
	})

	t.Run("rename keeps the item", func(t *testing.T) {
		const renamed = "docs/.pmngr/stories/DEMO-US-0003-track-every-refund.md"
		if err := m.Rename(newPath, renamed); err != nil {
			t.Fatalf("rename: %v", err)
		}
		delta, err := ix.ApplyFileEvents(ctx, []FileEvent{
			{Kind: FileRenamed, Path: renamed, OldPath: newPath},
		})
		if err != nil {
			t.Fatalf("ApplyFileEvents: %v", err)
		}
		if len(delta.Removed) != 0 {
			t.Errorf("removed = %v, want none: a rename keeps the id", delta.Removed)
		}
		if len(delta.Updated) != 1 || delta.Updated[0] != "DEMO-US-0003" {
			t.Errorf("updated = %v", delta.Updated)
		}
		it, err := ix.Item("DEMO-US-0003")
		if err != nil {
			t.Fatalf("Item: %v", err)
		}
		if it.Path != renamed {
			t.Errorf("path = %q, want %q", it.Path, renamed)
		}
		if err := m.Rename(renamed, newPath); err != nil {
			t.Fatalf("rename back: %v", err)
		}
		if _, err := ix.ApplyFileEvents(ctx, []FileEvent{{Kind: FileRenamed, Path: newPath, OldPath: renamed}}); err != nil {
			t.Fatalf("ApplyFileEvents: %v", err)
		}
	})

	t.Run("remove", func(t *testing.T) {
		if err := m.Remove(newPath); err != nil {
			t.Fatalf("remove: %v", err)
		}
		delta, err := ix.ApplyFileEvents(ctx, []FileEvent{{Kind: FileRemoved, Path: newPath}})
		if err != nil {
			t.Fatalf("ApplyFileEvents: %v", err)
		}
		if len(delta.Removed) != 1 || delta.Removed[0] != "DEMO-US-0003" {
			t.Fatalf("removed = %v", delta.Removed)
		}
		if _, err := ix.Item("DEMO-US-0003"); !errors.Is(err, ErrItemNotFound) {
			t.Errorf("Item after removal: %v", err)
		}
		if ix.Stats().Items != 5 {
			t.Errorf("items = %d, want 5", ix.Stats().Items)
		}
	})
}

func TestApplyFileEventsOnPagesAndComments(t *testing.T) {
	ix, m := buildFixtureIndex(t)
	ctx := context.Background()

	const page = "docs/architecture/refunds.md"
	if err := m.WriteFile(page, []byte("# Refunds\n\nSee [[DEMO-US-0001]].\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	const comment = "docs/.pmngr/comments/DEMO-US-0002/20260902T080000Z-jose.md"
	body := "---\nitem: DEMO-US-0002\nauthor: jose\ncreated: 2026-09-02T08:00:00Z\ntype: comment\n---\n\nLooks good.\n"
	if err := m.WriteFile(comment, []byte(body)); err != nil {
		t.Fatalf("write: %v", err)
	}
	delta, err := ix.ApplyFileEvents(ctx, []FileEvent{
		{Kind: FileCreated, Path: page},
		{Kind: FileCreated, Path: comment},
	})
	if err != nil {
		t.Fatalf("ApplyFileEvents: %v", err)
	}
	if len(delta.PagesAdded) != 1 || delta.PagesAdded[0] != page {
		t.Errorf("pages added = %v", delta.PagesAdded)
	}
	if len(delta.CommentsChanged) != 1 || delta.CommentsChanged[0] != "DEMO-US-0002" {
		t.Errorf("comments changed = %v", delta.CommentsChanged)
	}
	if got := ix.CommentCount("DEMO-US-0002"); got != 1 {
		t.Errorf("comment count = %d, want 1", got)
	}
	backlinks := ix.LinkGraph().Backlinks(ItemNode("DEMO-US-0001"))
	found := false
	for _, b := range backlinks {
		if b.From == PageNode(page) {
			found = true
		}
	}
	if !found {
		t.Errorf("backlinks = %+v, want one from %s", backlinks, page)
	}
}

func TestApplyFileEventsIgnoresUnknownPaths(t *testing.T) {
	ix, m := buildFixtureIndex(t)
	if err := m.WriteFile("README.md", []byte("# outside every project\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	delta, err := ix.ApplyFileEvents(context.Background(), []FileEvent{{Kind: FileCreated, Path: "README.md"}})
	if err != nil {
		t.Fatalf("ApplyFileEvents: %v", err)
	}
	if !delta.Empty() {
		t.Errorf("delta = %+v, want empty", delta)
	}
}

func TestBuildRecordsDuplicateIDs(t *testing.T) {
	m := loadFixture(t, basicFixture)
	const dup = "docs/.pmngr/stories/DEMO-US-0001-zz-duplicate.md"
	original, err := m.ReadFile("docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := m.WriteFile(dup, original); err != nil {
		t.Fatalf("write: %v", err)
	}
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	ix := NewIndex(m, projects)
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	var found *Diagnostic
	for i, d := range ix.Warnings() {
		if d.Code == CodeIDDuplicate {
			found = &ix.Warnings()[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no %s diagnostic in %v", CodeIDDuplicate, ix.Warnings())
	}
	if found.Path != dup {
		t.Errorf("diagnostic path = %q, want %q", found.Path, dup)
	}
	if !strings.Contains(found.Message, "docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md") {
		t.Errorf("message does not name the other file: %s", found.Message)
	}
	// The first file in path order keeps the id.
	it, err := ix.Item("DEMO-US-0001")
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if it.Path != "docs/.pmngr/stories/DEMO-US-0001-guest-checkout.md" {
		t.Errorf("winner = %q", it.Path)
	}
}

func TestBuildRecordsStaleCounter(t *testing.T) {
	m := loadFixture(t, basicFixture)
	const story = "docs/.pmngr/stories/DEMO-US-0099-far-ahead.md"
	body := "---\nid: DEMO-US-0099\ntype: story\ntitle: Far ahead\nstatus: todo\n" +
		"created: 2026-09-02T09:00:00Z\nupdated: 2026-09-02T09:00:00Z\n---\n\n## Description\n\nAhead of the counter.\n"
	if err := m.WriteFile(story, []byte(body)); err != nil {
		t.Fatalf("write: %v", err)
	}
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	ix := NewIndex(m, projects)
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	found := false
	for _, d := range ix.Warnings() {
		if d.Code == CodeWarnCounterStale && strings.Contains(d.Field, "story") {
			found = true
			if d.Severity != SeverityWarning {
				t.Errorf("severity = %q, want warning", d.Severity)
			}
		}
	}
	if !found {
		t.Errorf("no %s diagnostic in %v", CodeWarnCounterStale, ix.Warnings())
	}
}

func TestBuildKeepsGoingOnParseErrors(t *testing.T) {
	m := loadFixture(t, basicFixture)
	const broken = "docs/.pmngr/tasks/DEMO-T-0002-broken.md"
	if err := m.WriteFile(broken, []byte("---\nid: DEMO-T-0002\ntype: task\n: not yaml\n---\n\nbody\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	ix := NewIndex(m, projects)
	stats, err := ix.Build(context.Background(), true)
	if err != nil {
		t.Fatalf("Build must not fail on a broken file: %v", err)
	}
	if stats.Items != 5 {
		t.Errorf("items = %d, want the 5 healthy ones", stats.Items)
	}
	if stats.Errors == 0 {
		t.Error("the broken file produced no error diagnostic")
	}
	found := false
	for _, d := range ix.Warnings() {
		if d.Path == broken && d.Severity == SeverityError {
			found = true
		}
	}
	if !found {
		t.Errorf("no diagnostic for %s", broken)
	}
}

func TestBuildReportsLayoutProblems(t *testing.T) {
	m := loadFixture(t, basicFixture)
	if err := m.WriteFile("docs/.pmngr/stories/archive/DEMO-US-0050-old.md", []byte("---\nid: DEMO-US-0050\ntype: story\ntitle: Old\n---\n\nold\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := m.WriteFile("docs/.pmngr/stories/notes.txt", []byte("scratch")); err != nil {
		t.Fatalf("write: %v", err)
	}
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	ix := NewIndex(m, projects)
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	codes := map[Code]bool{}
	for _, d := range ix.Warnings() {
		codes[d.Code] = true
	}
	if !codes[idxCodeLayoutNested] {
		t.Errorf("missing %s", idxCodeLayoutNested)
	}
	if !codes[idxCodeLayoutStray] {
		t.Errorf("missing %s", idxCodeLayoutStray)
	}
	if _, err := ix.Item("DEMO-US-0050"); !errors.Is(err, ErrItemNotFound) {
		t.Error("a nested item must not be indexed")
	}
}

func TestBuildCancelledContext(t *testing.T) {
	m := loadFixture(t, basicFixture)
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ix := NewIndex(m, projects)
	if _, err := ix.Build(ctx, true); !errors.Is(err, context.Canceled) {
		t.Errorf("Build error = %v, want context.Canceled", err)
	}
}

func TestConcurrentReadsDuringBuild(t *testing.T) {
	ix, _ := buildFixtureIndex(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			if _, err := ix.Items(context.Background(), Filter{}); err != nil {
				t.Errorf("Items: %v", err)
				return
			}
			ix.Warnings()
			ix.Stats()
		}
	}()
	for i := 0; i < 10; i++ {
		if _, err := ix.Build(context.Background(), false); err != nil {
			t.Fatalf("Build: %v", err)
		}
	}
	<-done
}

// generatedVault builds an in-memory vault with n items spread over epics,
// stories and tasks, plus one knowledge-base page per 20 items.
func generatedVault(n int) *MemFS {
	m := NewMemFS()
	m.Now = func() time.Time { return fixtureTime }
	var cfg strings.Builder
	cfg.WriteString("schema: 1\nkey: BENCH\nname: Benchmark\ndocs:\n  path: docs\n")
	cfg.WriteString("workflow:\n  initial: todo\n  statuses:\n    - {id: todo, category: todo}\n")
	cfg.WriteString("    - {id: in_progress, category: in_progress}\n    - {id: done, category: done, terminal: true}\n")
	_ = m.WriteFile("docs/.pmngr/project.yaml", []byte(cfg.String()))

	statuses := []string{"todo", "in_progress", "done"}
	priorities := []string{"critical", "high", "medium", "low"}
	for i := 1; i <= n; i++ {
		var folder, code, typ string
		switch {
		case i%20 == 0:
			folder, code, typ = "epics", "EP", "epic"
		case i%3 == 0:
			folder, code, typ = "tasks", "T", "task"
		default:
			folder, code, typ = "stories", "US", "story"
		}
		id := fmt.Sprintf("BENCH-%s-%04d", code, i)
		title := fmt.Sprintf("Generated item %d", i)
		body := fmt.Sprintf(`---
id: %s
type: %s
title: %s
status: %s
priority: %s
assignees: [dev%d]
labels: [generated, batch%d]
created: 2026-08-%02dT08:00:00Z
updated: 2026-09-%02dT08:00:00Z
---

## Description

Item %d of the generated corpus, referring to [[docs/page-%d]].

## Acceptance Criteria

- [x] Generated
- [ ] Verified
`, id, typ, title, statuses[i%len(statuses)], priorities[i%len(priorities)],
			i%7, i%5, (i%27)+1, (i%27)+1, i, i%50)
		_ = m.WriteFile(fmt.Sprintf("docs/.pmngr/%s/%s-%s.md", folder, id, Slugify(title)), []byte(body))
		if i%20 == 0 {
			page := fmt.Sprintf("---\ntitle: Page %d\ntags: [generated]\n---\n\n# Page %d\n\nSee [[%s]].\n", i/20, i/20, id)
			_ = m.WriteFile(fmt.Sprintf("docs/page-%d.md", i/20), []byte(page))
		}
	}
	return m
}

func BenchmarkBuild2000Items(b *testing.B) {
	m := generatedVault(2000)
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		b.Fatalf("DiscoverProjects: %v", err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix := NewIndex(m, projects)
		stats, err := ix.Build(ctx, true)
		if err != nil {
			b.Fatalf("Build: %v", err)
		}
		if stats.Items != 2000 {
			b.Fatalf("items = %d, want 2000", stats.Items)
		}
	}
}

func BenchmarkApplyOneFileEvent(b *testing.B) {
	m := generatedVault(2000)
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		b.Fatalf("DiscoverProjects: %v", err)
	}
	ix := NewIndex(m, projects)
	ctx := context.Background()
	if _, err := ix.Build(ctx, true); err != nil {
		b.Fatalf("Build: %v", err)
	}
	target := ""
	for _, p := range m.Paths() {
		if strings.HasPrefix(p, "docs/.pmngr/stories/") {
			target = p
			break
		}
	}
	data, err := m.ReadFile(target)
	if err != nil {
		b.Fatalf("read: %v", err)
	}
	events := []FileEvent{{Kind: FileModified, Path: target}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := m.WriteFile(target, data); err != nil {
			b.Fatalf("write: %v", err)
		}
		if _, err := ix.ApplyFileEvents(ctx, events); err != nil {
			b.Fatalf("ApplyFileEvents: %v", err)
		}
	}
}
