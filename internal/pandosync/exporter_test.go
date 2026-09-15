package pandosync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// stubSource is the narrow read seam a test drives by hand. It takes a lock so
// that a concurrency test can mutate it while an export reads it.
type stubSource struct {
	mu    sync.Mutex
	items []core.Item
	pages []*core.KBPage
}

func (s *stubSource) Items() []core.Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]core.Item(nil), s.items...)
}

func (s *stubSource) Pages() []*core.KBPage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*core.KBPage(nil), s.pages...)
}

func (s *stubSource) setItems(items ...core.Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = items
}

func (s *stubSource) setPages(pages ...*core.KBPage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pages = pages
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestExporter returns an exporter over an in-memory corpus.
func newTestExporter(t *testing.T, src Source) (*Exporter, *core.MemFS) {
	t.Helper()
	fsys := core.NewMemFS()
	e, err := New(Options{FS: fsys, Source: src, Project: "GIT", Logger: quietLogger()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e, fsys
}

func corpusFiles(t *testing.T, fsys *core.MemFS) []string {
	t.Helper()
	var out []string
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := fsys.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			child := entry.Name
			if dir != "." {
				child = dir + "/" + entry.Name
			}
			if entry.IsDir {
				walk(child)
				continue
			}
			out = append(out, child)
		}
	}
	walk(".")
	sort.Strings(out)
	return out
}

func TestExportAllWritesTheCorpusLayout(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem(), core.Item{ID: "ACME-T-0001", Type: core.TypeTask, Title: "Other project"})
	src.setPages(testPage())
	e, fsys := newTestExporter(t, src)

	stats, err := e.ExportAll(context.Background())
	if err != nil {
		t.Fatalf("ExportAll: %v", err)
	}
	want := []string{
		"ACME/items/ACME-T-0001.md",
		"GIT/items/GIT-US-0073.md",
		"GIT/kb/21-semantic-search.md",
	}
	if got := corpusFiles(t, fsys); !equal(got, want) {
		t.Errorf("corpus = %v, want %v", got, want)
	}
	if stats.Items != 2 || stats.Pages != 1 || stats.Written != 3 || stats.Skipped != 0 {
		t.Errorf("stats = %+v, want 2 items, 1 page, 3 written", stats)
	}
	if !stats.Full {
		t.Error("a full export should say so in its Stats")
	}
	if e.LastExport().Written != 3 {
		t.Errorf("LastExport = %+v, want the full export", e.LastExport())
	}

	data, err := fsys.ReadFile("GIT/items/GIT-US-0073.md")
	if err != nil {
		t.Fatalf("read exported item: %v", err)
	}
	if !strings.Contains(string(data), "tags: [GIT-US-0073, server, performance, story, in_progress]") {
		t.Errorf("exported item lost its tags:\n%s", data)
	}
}

func TestExportAllSkipsUnchangedDocuments(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem())
	src.setPages(testPage())
	e, fsys := newTestExporter(t, src)

	if _, err := e.ExportAll(context.Background()); err != nil {
		t.Fatalf("first export: %v", err)
	}
	before, err := fsys.Stat("GIT/items/GIT-US-0073.md")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	second, err := e.ExportAll(context.Background())
	if err != nil {
		t.Fatalf("second export: %v", err)
	}
	if second.Written != 0 || second.Skipped != 2 {
		t.Errorf("second export = %+v, want nothing written and 2 skipped", second)
	}
	after, err := fsys.Stat("GIT/items/GIT-US-0073.md")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !after.ModTime.Equal(before.ModTime) {
		t.Error("an unchanged document was rewritten; Pando's mtime skip depends on it not being")
	}
}

// A fresh exporter has no hash cache, so the skip must also work from the bytes
// already on disk: that is the case after a restart.
func TestExportAllSkipsFromDiskAfterRestart(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem())
	e, fsys := newTestExporter(t, src)
	if _, err := e.ExportAll(context.Background()); err != nil {
		t.Fatalf("first export: %v", err)
	}

	restarted, err := New(Options{FS: fsys, Source: src, Project: "GIT", Logger: quietLogger()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stats, err := restarted.ExportAll(context.Background())
	if err != nil {
		t.Fatalf("export after restart: %v", err)
	}
	if stats.Written != 0 || stats.Skipped != 1 {
		t.Errorf("export after restart = %+v, want nothing written", stats)
	}
}

func TestExportAllPrunesVanishedSourcesAndEmptyDirectories(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem(), core.Item{ID: "ACME-T-0001", Type: core.TypeTask, Title: "Doomed"})
	src.setPages(testPage())
	e, fsys := newTestExporter(t, src)
	if _, err := e.ExportAll(context.Background()); err != nil {
		t.Fatalf("first export: %v", err)
	}

	src.setItems(testItem())
	src.setPages()
	stats, err := e.ExportAll(context.Background())
	if err != nil {
		t.Fatalf("second export: %v", err)
	}
	if stats.Removed != 2 {
		t.Errorf("removed = %d, want 2", stats.Removed)
	}
	if got, want := corpusFiles(t, fsys), []string{"GIT/items/GIT-US-0073.md"}; !equal(got, want) {
		t.Errorf("corpus = %v, want %v", got, want)
	}
	if _, err := fsys.Stat("ACME"); !errors.Is(err, core.ErrNotExist) {
		t.Error("a directory emptied by the prune should be removed too")
	}
	if _, err := fsys.Stat("GIT/kb"); !errors.Is(err, core.ErrNotExist) {
		t.Error("the emptied kb directory should be removed")
	}
}

// A leftover temporary file from a crash between write and rename must be
// pruned: Pando's walk has no dot-file exclusion.
func TestExportAllPrunesLeftoverTemporaryFiles(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem())
	e, fsys := newTestExporter(t, src)
	if err := fsys.WriteFile("GIT/items/.GIT-US-0099.md.tmp", []byte("half")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := e.ExportAll(context.Background()); err != nil {
		t.Fatalf("ExportAll: %v", err)
	}
	if got, want := corpusFiles(t, fsys), []string{"GIT/items/GIT-US-0073.md"}; !equal(got, want) {
		t.Errorf("corpus = %v, want %v", got, want)
	}
}

func TestExportAllRefusesPathsThatEscapeTheCorpus(t *testing.T) {
	src := &stubSource{}
	src.setPages(
		&core.KBPage{Path: "docs/evil.md", RelPath: "../../../etc/passwd.md", Project: "GIT", Title: "Escape"},
		&core.KBPage{Path: "docs/evil2.md", RelPath: "a/../../b.md", Project: "GIT", Title: "Escape too"},
		&core.KBPage{Path: "docs/evil3.md", RelPath: "C:/windows/system.md", Project: "GIT", Title: "Volume"},
		&core.KBPage{Path: "docs/evil4.md", RelPath: "/absolute.md", Project: "GIT", Title: "Absolute"},
		&core.KBPage{Path: "docs/ok.md", RelPath: "nested/ok.md", Project: "GIT", Title: "Fine"},
	)
	src.setItems(core.Item{ID: "../../escape", Type: core.TypeTask, Title: "Bad id"})
	e, fsys := newTestExporter(t, src)

	stats, err := e.ExportAll(context.Background())
	if err != nil {
		t.Fatalf("a refused path must not fail the export: %v", err)
	}
	if stats.Items != 0 {
		t.Errorf("an item with a malformed id must not be exported, got %d", stats.Items)
	}
	if got, want := corpusFiles(t, fsys), []string{"GIT/kb/nested/ok.md"}; !equal(got, want) {
		t.Errorf("corpus = %v, want only the safe page", got)
	}
}

func TestPagePathRefusals(t *testing.T) {
	for _, rel := range []string{"", "..", "../x.md", "a/../../b.md", "/abs.md", "C:/x.md", "a\\b.md", "a\x00b.md"} {
		if got, err := pagePath(rel, "GIT"); err == nil {
			t.Errorf("pagePath(%q) = %q, want a refusal", rel, got)
		} else {
			var forbidden *ErrForbiddenPath
			if !errors.As(err, &forbidden) {
				t.Errorf("pagePath(%q) error = %T, want *ErrForbiddenPath", rel, err)
			}
		}
	}
	got, err := pagePath("guides/setup", "GIT")
	if err != nil {
		t.Fatalf("pagePath: %v", err)
	}
	if got != "GIT/kb/guides/setup.md" {
		t.Errorf("pagePath = %q", got)
	}
}

// spyFS records the order of operations so that a test can prove the final path
// only ever appears through a rename, never through a direct write.
type spyFS struct {
	core.FS
	mu      sync.Mutex
	ops     []string
	failOn  string
	failErr error
}

func (s *spyFS) record(op string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops = append(s.ops, op)
}

func (s *spyFS) operations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ops...)
}

func (s *spyFS) WriteFile(p string, data []byte) error {
	s.record("write " + p)
	s.mu.Lock()
	fail := s.failOn != "" && strings.Contains(p, s.failOn)
	err := s.failErr
	s.mu.Unlock()
	if fail {
		return err
	}
	return s.FS.WriteFile(p, data)
}

func (s *spyFS) Rename(from, to string) error {
	s.record("rename " + from + " -> " + to)
	return s.FS.Rename(from, to)
}

func TestWritesAreAtomic(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem())
	spy := &spyFS{FS: core.NewMemFS()}
	e, err := New(Options{FS: spy, Source: src, Project: "GIT", Logger: quietLogger()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := e.ExportAll(context.Background()); err != nil {
		t.Fatalf("ExportAll: %v", err)
	}

	const final = "GIT/items/GIT-US-0073.md"
	const tmp = "GIT/items/.GIT-US-0073.md.tmp"
	want := []string{"write " + tmp, "rename " + tmp + " -> " + final}
	if got := spy.operations(); !equal(got, want) {
		t.Errorf("operations = %v, want %v", got, want)
	}
	if strings.HasSuffix(tmp, mdExt) {
		t.Error("the temporary file must not carry the .md extension an importer walks for")
	}
}

func TestWriteFailureDoesNotPoisonTheExporter(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem())
	spy := &spyFS{FS: core.NewMemFS(), failOn: "GIT-US-0073", failErr: errors.New("disk full")}
	e, err := New(Options{FS: spy, Source: src, Project: "GIT", Logger: quietLogger()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stats, err := e.ExportAll(context.Background())
	if err != nil {
		t.Fatalf("a failed write must not fail the whole export: %v", err)
	}
	if stats.Written != 0 {
		t.Errorf("written = %d, want 0", stats.Written)
	}

	spy.mu.Lock()
	spy.failOn = ""
	spy.mu.Unlock()
	if _, err := e.Apply(context.Background(), Event{Kind: ItemChanged, ID: "GIT-US-0073"}); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if _, err := spy.ReadFile("GIT/items/GIT-US-0073.md"); err != nil {
		t.Errorf("the retry should have written the document: %v", err)
	}
}

func TestApplyItemChangedRewritesExactlyOneFile(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem(), core.Item{ID: "GIT-T-0122", Type: core.TypeTask, Title: "Serializer"})
	e, fsys := newTestExporter(t, src)
	if _, err := e.ExportAll(context.Background()); err != nil {
		t.Fatalf("ExportAll: %v", err)
	}
	untouched, err := fsys.Stat("GIT/items/GIT-T-0122.md")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	changed := testItem()
	changed.Status = "in_review"
	src.setItems(changed, core.Item{ID: "GIT-T-0122", Type: core.TypeTask, Title: "Serializer"})

	stats, err := e.Apply(context.Background(), Event{Kind: ItemChanged, ID: "GIT-US-0073"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if stats.Written != 1 || stats.Removed != 0 {
		t.Errorf("stats = %+v, want exactly one write", stats)
	}
	data, err := fsys.ReadFile("GIT/items/GIT-US-0073.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "status: in_review") {
		t.Errorf("the changed item was not rewritten:\n%s", data)
	}
	after, err := fsys.Stat("GIT/items/GIT-T-0122.md")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !after.ModTime.Equal(untouched.ModTime) {
		t.Error("an unrelated document was rewritten")
	}
}

// A burst of events for the same unchanged document must not produce a burst of
// writes: the content hash absorbs it even when the caller does not debounce.
func TestApplyBurstOverTheSameItemWritesOnce(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem())
	e, _ := newTestExporter(t, src)

	written := 0
	for i := 0; i < 5; i++ {
		stats, err := e.Apply(context.Background(), Event{Kind: ItemChanged, ID: "GIT-US-0073"})
		if err != nil {
			t.Fatalf("Apply %d: %v", i, err)
		}
		written += stats.Written
	}
	if written != 1 {
		t.Errorf("writes = %d, want 1", written)
	}
}

func TestApplyRemovals(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem())
	src.setPages(testPage())
	e, fsys := newTestExporter(t, src)
	if _, err := e.ExportAll(context.Background()); err != nil {
		t.Fatalf("ExportAll: %v", err)
	}

	src.setItems()
	src.setPages()

	if stats, err := e.Apply(context.Background(), Event{Kind: ItemRemoved, ID: "GIT-US-0073"}); err != nil {
		t.Fatalf("Apply: %v", err)
	} else if stats.Removed != 1 {
		t.Errorf("item removal stats = %+v", stats)
	}
	if stats, err := e.Apply(context.Background(), Event{Kind: PageRemoved, Path: "docs/21-semantic-search.md"}); err != nil {
		t.Fatalf("Apply: %v", err)
	} else if stats.Removed != 1 {
		t.Errorf("page removal stats = %+v", stats)
	}
	if got := corpusFiles(t, fsys); len(got) != 0 {
		t.Errorf("corpus = %v, want empty", got)
	}
}

// An item.changed event for an item that is already gone is a removal: the
// watcher reports a deletion that way when the index has moved on.
func TestApplyItemChangedForAVanishedItemRemovesTheFile(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem())
	e, fsys := newTestExporter(t, src)
	if _, err := e.ExportAll(context.Background()); err != nil {
		t.Fatalf("ExportAll: %v", err)
	}
	src.setItems()

	stats, err := e.Apply(context.Background(), Event{Kind: ItemChanged, ID: "GIT-US-0073"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if stats.Removed != 1 {
		t.Errorf("stats = %+v, want a removal", stats)
	}
	if got := corpusFiles(t, fsys); len(got) != 0 {
		t.Errorf("corpus = %v, want empty", got)
	}
}

func TestApplyPageChanged(t *testing.T) {
	src := &stubSource{}
	e, fsys := newTestExporter(t, src)
	src.setPages(testPage())

	stats, err := e.Apply(context.Background(), Event{Kind: PageChanged, Path: "docs/21-semantic-search.md"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if stats.Written != 1 {
		t.Errorf("stats = %+v, want one write", stats)
	}
	if _, err := fsys.ReadFile("GIT/kb/21-semantic-search.md"); err != nil {
		t.Errorf("the page was not exported: %v", err)
	}
}

// An overflow means the caller lost events, so incremental updates can no
// longer be trusted: the exporter must re-export everything.
func TestApplyOverflowTriggersAFullExport(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem())
	e, fsys := newTestExporter(t, src)
	if _, err := e.ExportAll(context.Background()); err != nil {
		t.Fatalf("ExportAll: %v", err)
	}

	// Drift the corpus behind the source the way a dropped subscriber would:
	// one item added and one page added, with no event delivered for either.
	src.setItems(testItem(), core.Item{ID: "GIT-T-0122", Type: core.TypeTask, Title: "Missed"})
	src.setPages(testPage())

	stats, err := e.Apply(context.Background(), Event{Kind: Overflow})
	if err != nil {
		t.Fatalf("Apply overflow: %v", err)
	}
	if !stats.Full {
		t.Error("an overflow must run a full export")
	}
	if stats.Written != 2 {
		t.Errorf("stats = %+v, want the two missed documents written", stats)
	}
	want := []string{"GIT/items/GIT-T-0122.md", "GIT/items/GIT-US-0073.md", "GIT/kb/21-semantic-search.md"}
	if got := corpusFiles(t, fsys); !equal(got, want) {
		t.Errorf("corpus = %v, want %v", got, want)
	}
}

// A removal for a page the exporter never indexed cannot be resolved to a
// corpus path, so it must fall back to a full export rather than desync.
func TestApplyPageRemovedForAnUnknownPageReExports(t *testing.T) {
	src := &stubSource{}
	src.setItems(testItem())
	e, fsys := newTestExporter(t, src)
	if err := fsys.WriteFile("GIT/kb/stale.md", []byte("stale")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stats, err := e.Apply(context.Background(), Event{Kind: PageRemoved, Path: "docs/never-seen.md"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !stats.Full {
		t.Error("an unresolvable removal must fall back to a full export")
	}
	if got, want := corpusFiles(t, fsys), []string{"GIT/items/GIT-US-0073.md"}; !equal(got, want) {
		t.Errorf("corpus = %v, want %v", got, want)
	}
}

func TestApplyRejectsAnUnknownKind(t *testing.T) {
	src := &stubSource{}
	e, _ := newTestExporter(t, src)
	if _, err := e.Apply(context.Background(), Event{Kind: "nonsense"}); err == nil {
		t.Error("an unknown event kind should be reported, not silently ignored")
	}
}

func TestExportAllIsCancellable(t *testing.T) {
	src := &stubSource{}
	items := make([]core.Item, 0, 200)
	for i := 1; i <= 200; i++ {
		items = append(items, core.Item{
			ID:    core.ItemID(fmt.Sprintf("GIT-T-%04d", i)),
			Type:  core.TypeTask,
			Title: fmt.Sprintf("Task %d", i),
		})
	}
	src.setItems(items...)
	e, _ := newTestExporter(t, src)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.ExportAll(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("ExportAll error = %v, want context.Canceled", err)
	}
}

// Apply must be safe while a full export runs: the two serialize rather than
// racing each other into a half-pruned tree. Run with -race.
func TestApplyDuringExportAllIsSafe(t *testing.T) {
	src := &stubSource{}
	items := make([]core.Item, 0, 300)
	for i := 1; i <= 300; i++ {
		items = append(items, core.Item{
			ID:    core.ItemID(fmt.Sprintf("GIT-T-%04d", i)),
			Type:  core.TypeTask,
			Title: fmt.Sprintf("Task %d", i),
		})
	}
	src.setItems(items...)
	src.setPages(testPage())
	e, fsys := newTestExporter(t, src)

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.ExportAll(ctx); err != nil {
				t.Errorf("ExportAll: %v", err)
			}
		}()
	}
	for i := 1; i <= 40; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ev := Event{Kind: ItemChanged, ID: core.ItemID(fmt.Sprintf("GIT-T-%04d", n))}
			if _, err := e.Apply(ctx, ev); err != nil {
				t.Errorf("Apply: %v", err)
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := e.Apply(ctx, Event{Kind: Overflow}); err != nil {
			t.Errorf("Apply overflow: %v", err)
		}
		_ = e.LastExport()
	}()
	wg.Wait()

	if got := len(corpusFiles(t, fsys)); got != 301 {
		t.Errorf("corpus holds %d files, want 301", got)
	}
}

func TestNewRefusesACorpusInsideARepository(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := New(Options{Dir: dir, Source: &stubSource{}, Logger: quietLogger()}); err == nil {
		t.Error("the corpus must never be written inside a repository")
	}
}

func TestNewOnDiskCreatesTheCorpusDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pando-kb")
	src := &stubSource{}
	src.setItems(testItem())
	e, err := New(Options{Dir: dir, Source: src, Project: "GIT", Logger: quietLogger()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if e.Dir() != dir {
		t.Errorf("Dir = %q, want %q", e.Dir(), dir)
	}
	if _, err := e.ExportAll(context.Background()); err != nil {
		t.Fatalf("ExportAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "GIT", "items", "GIT-US-0073.md")); err != nil {
		t.Errorf("the document was not written to disk: %v", err)
	}
}

func TestNewRejectsAnIncompleteConfiguration(t *testing.T) {
	if _, err := New(Options{Dir: t.TempDir()}); err == nil {
		t.Error("New without a source should fail")
	}
	if _, err := New(Options{Source: &stubSource{}}); err == nil {
		t.Error("New without a directory or an FS should fail")
	}
}

// The full export of a large corpus must stay well inside the cold-index budget
// of docs/02-architecture.md section 9, and must not be quadratic.
func TestExportAllLargeCorpusStaysWithinBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	src := &stubSource{}
	items := make([]core.Item, 0, 10000)
	for i := 1; i <= 10000; i++ {
		items = append(items, core.Item{
			ID:     core.ItemID(fmt.Sprintf("GIT-T-%04d", i)),
			Type:   core.TypeTask,
			Title:  fmt.Sprintf("Task %d", i),
			Status: "todo",
			Body:   strings.Repeat("Some body text with a [[GIT-US-0073]] wikilink.\n", 10),
		})
	}
	src.setItems(items...)
	e, _ := newTestExporter(t, src)

	start := time.Now()
	stats, err := e.ExportAll(context.Background())
	if err != nil {
		t.Fatalf("ExportAll: %v", err)
	}
	if stats.Written != 10000 {
		t.Fatalf("written = %d, want 10000", stats.Written)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("full export of 10000 items took %s", elapsed)
	}

	start = time.Now()
	second, err := e.ExportAll(context.Background())
	if err != nil {
		t.Fatalf("second export: %v", err)
	}
	if second.Written != 0 || second.Skipped != 10000 {
		t.Errorf("second export = %+v, want everything skipped", second)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("unchanged re-export of 10000 items took %s", elapsed)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
