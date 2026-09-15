package pandosync

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
)

// Source is the read seam the exporter needs: everything it exports and nothing
// else. *vault.Vault satisfies it once it exposes Pages(); a test satisfies it
// with two slices.
type Source interface {
	// Items returns every indexed item, deleted ones included, with bodies.
	Items() []core.Item
	// Pages returns every indexed knowledge-base page, with bodies.
	Pages() []*core.KBPage
}

// Lookup is an optional refinement of [Source]. When a Source implements it,
// Apply resolves one document through it instead of cloning the whole index for
// a single changed file, which is what keeps an incremental update cheap on a
// ten-thousand-item corpus.
type Lookup interface {
	// Item returns one item by id.
	Item(id core.ItemID) (core.Item, bool)
	// Page returns one page by its vault-relative path.
	Page(vaultPath string) (*core.KBPage, bool)
}

// Kind is the sort of change an [Event] reports.
type Kind string

// The event kinds the exporter understands. The server hub speaks its own
// event vocabulary; adapting it to these is the caller's job, so that this
// package never imports internal/server.
const (
	// ItemChanged means the item with the given ID was created or edited.
	ItemChanged Kind = "item.changed"
	// ItemRemoved means the item with the given ID no longer exists.
	ItemRemoved Kind = "item.removed"
	// PageChanged means the knowledge-base page at the given Path was created
	// or edited. Path is vault-relative.
	PageChanged Kind = "page.changed"
	// PageRemoved means the knowledge-base page at the given Path is gone.
	PageRemoved Kind = "page.removed"
	// Overflow means the caller lost events and can no longer say which
	// documents are stale. It triggers a full re-export, because a dropped
	// subscriber that keeps exporting incrementally desyncs forever.
	Overflow Kind = "overflow"
)

// Event is one incremental change handed to [Exporter.Apply].
type Event struct {
	// Kind selects which of the fields below matters.
	Kind Kind
	// ID is the item this event is about, for ItemChanged and ItemRemoved.
	ID core.ItemID
	// Path is the vault-relative page path, for PageChanged and PageRemoved.
	Path string
}

// Stats is the outcome of one export, full or incremental.
type Stats struct {
	// Items and Pages are how many source documents were considered. They are
	// zero for an incremental event, which considers exactly one.
	Items int `json:"items"`
	Pages int `json:"pages"`
	// Written, Removed and Skipped count corpus files. Skipped is the
	// content-hash hit: the file on disk already held these bytes, so it was
	// left alone and Pando's own mtime skip stays effective.
	Written int `json:"written"`
	Removed int `json:"removed"`
	Skipped int `json:"skipped"`
	// Duration is the wall time the export took.
	Duration time.Duration `json:"duration"`
	// At is when the export finished. It is zero for a Stats that never ran.
	At time.Time `json:"at"`
	// Full reports whether this was a full export rather than one event.
	Full bool `json:"full"`
}

// Progress is reported while a full export runs, so that a caller can publish
// it on its own event bus without this package knowing about one.
type Progress struct {
	// Done and Total count source documents, items and pages together.
	Done  int `json:"done"`
	Total int `json:"total"`
}

// Options configure an [Exporter].
type Options struct {
	// Dir is the corpus root on disk. It must be outside every repository the
	// companion serves: everything written here is derived data, safe to delete
	// and never committed. Ignored when FS is set.
	Dir string
	// FS overrides Dir with a file system rooted at the corpus. Tests pass a
	// core.MemFS; production leaves it nil and gets an osfs.FS over Dir.
	FS core.FS
	// Source is where items and pages are read from. Required.
	Source Source
	// Project is the project key used for a document whose own project cannot
	// be determined. Items normally carry it in their id and pages in their
	// front matter, so this is a fallback, not an override.
	Project string
	// Clock stamps Stats. Nil means time.Now.
	Clock func() time.Time
	// Logger receives one line per refused path or failed write. Nil means
	// slog.Default().
	Logger *slog.Logger
	// OnProgress, when set, is called during a full export. It must not block.
	OnProgress func(Progress)
}

// Exporter mirrors the backlog into the corpus directory. It is safe for
// concurrent use: a full export and an incremental Apply never interleave.
type Exporter struct {
	fs      core.FS
	dir     string
	src     Source
	lookup  Lookup
	project string
	now     func() time.Time
	log     *slog.Logger
	onProg  func(Progress)

	// mu serializes every corpus mutation, so that an Apply arriving while a
	// full export runs waits rather than racing it into a half-pruned tree.
	mu sync.Mutex
	// hashes maps a corpus path to the hash of the bytes last written there. It
	// is the content-hash skip: a hit avoids even reading the file back.
	hashes map[string][sha256.Size]byte
	// pages maps a vault-relative page path to its corpus path, so that a
	// removal event can find the file of a page that is already gone from the
	// index.
	pages map[string]string

	statsMu sync.Mutex
	last    Stats
}

// New returns an exporter writing under opts.Dir.
func New(opts Options) (*Exporter, error) {
	if opts.Source == nil {
		return nil, errors.New("pandosync: no source to export from")
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	fsys := opts.FS
	dir := opts.Dir
	if fsys == nil {
		if dir == "" {
			return nil, errors.New("pandosync: no corpus directory")
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, fmt.Errorf("pandosync: resolve corpus directory %s: %w", dir, err)
		}
		dir = abs
		if err := refuseRepository(dir); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("pandosync: create corpus directory %s: %w", dir, err)
		}
		fsys, err = osfs.New(dir)
		if err != nil {
			return nil, fmt.Errorf("pandosync: open corpus directory %s: %w", dir, err)
		}
	}
	e := &Exporter{
		fs:      fsys,
		dir:     dir,
		src:     opts.Source,
		project: opts.Project,
		now:     opts.Clock,
		log:     opts.Logger,
		onProg:  opts.OnProgress,
		hashes:  make(map[string][sha256.Size]byte),
		pages:   make(map[string]string),
	}
	if l, ok := opts.Source.(Lookup); ok {
		e.lookup = l
	}
	return e, nil
}

// refuseRepository refuses a corpus root that is itself a git working tree. The
// corpus is derived data and must never be committed, and Pando's directory
// walk has no dot-directory exclusion, so a repository root would also feed it
// the whole of .git.
func refuseRepository(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return fmt.Errorf("pandosync: corpus directory %s is a git repository; "+
			"the corpus is derived data and must live outside every repository", dir)
	}
	return nil
}

// Dir returns the corpus root, empty when the exporter writes to a supplied FS.
func (e *Exporter) Dir() string { return e.dir }

// LastExport returns the outcome of the most recent full export, or the zero
// Stats when none has finished. It is what the settings card shows.
func (e *Exporter) LastExport() Stats {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	return e.last
}

// ExportAll rewrites the whole corpus: every item and every page whose bytes
// changed, then a prune of every corpus file whose source is gone.
//
// It is cancellable. A cancelled export leaves the corpus consistent — every
// file it did write is complete — but incomplete, and the caller should run it
// again rather than fall back to incremental updates.
func (e *Exporter) ExportAll(ctx context.Context) (Stats, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := e.now()
	items := e.src.Items()
	pages := e.src.Pages()
	stats := Stats{Full: true}

	desired := make(map[string][]byte, len(items)+len(pages))
	pageIndex := make(map[string]string, len(pages))
	total := len(items) + len(pages)
	done := 0

	for i := range items {
		it := &items[i]
		if it.Deleted {
			continue
		}
		p, err := e.itemPathOf(it.ID)
		if err != nil {
			e.log.Warn("pandosync: item not exported", "id", it.ID, "error", err)
			continue
		}
		desired[p] = RenderItem(it, e.projectOf(it))
		stats.Items++
	}
	done += len(items)
	e.progress(done, total)

	for _, pg := range pages {
		if pg == nil {
			continue
		}
		p, err := pagePath(pageRelPath(pg), e.pageProjectOf(pg))
		if err != nil {
			e.log.Warn("pandosync: page not exported", "path", pg.Path, "error", err)
			continue
		}
		desired[p] = RenderPage(pg, e.pageProjectOf(pg))
		pageIndex[pg.Path] = p
		stats.Pages++
	}
	done += len(pages)
	e.progress(done, total)

	for _, p := range sortedKeys(desired) {
		if err := ctx.Err(); err != nil {
			return e.finish(stats, start), err
		}
		written, err := e.writeIfChanged(p, desired[p])
		switch {
		case err != nil:
			e.log.Warn("pandosync: write failed", "path", p, "error", err)
		case written:
			stats.Written++
		default:
			stats.Skipped++
		}
	}

	removed, err := e.prune(ctx, desired)
	stats.Removed = removed
	e.pages = pageIndex
	out := e.finish(stats, start)
	if err != nil {
		return out, err
	}
	e.statsMu.Lock()
	e.last = out
	e.statsMu.Unlock()
	return out, nil
}

// Apply handles one incremental change. An Overflow event runs a full export,
// because a caller that dropped events cannot say what is stale.
//
// A failure is returned but never poisons the exporter: the next event over the
// same document retries the write.
func (e *Exporter) Apply(ctx context.Context, ev Event) (Stats, error) {
	if ev.Kind == Overflow {
		e.log.Warn("pandosync: event overflow, re-exporting the whole corpus")
		return e.ExportAll(ctx)
	}
	stats, needFull, err := e.applyOne(ctx, ev)
	if needFull {
		return e.ExportAll(ctx)
	}
	return stats, err
}

// applyOne handles one event under the corpus lock. It reports needFull when
// the event cannot be resolved incrementally and only a full export can restore
// the corpus; the caller runs that outside the lock.
func (e *Exporter) applyOne(ctx context.Context, ev Event) (stats Stats, needFull bool, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := e.now()
	if err := ctx.Err(); err != nil {
		return e.finish(stats, start), false, err
	}

	switch ev.Kind {
	case ItemChanged:
		it, ok := e.findItem(ev.ID)
		if !ok || it.Deleted {
			stats, err = e.applyRemoveItem(stats, start, ev.ID)
			return stats, false, err
		}
		p, pathErr := e.itemPathOf(it.ID)
		if pathErr != nil {
			return e.finish(stats, start), false, pathErr
		}
		stats.Items = 1
		stats, err = e.applyWrite(stats, start, p, RenderItem(&it, e.projectOf(&it)))
		return stats, false, err

	case ItemRemoved:
		stats, err = e.applyRemoveItem(stats, start, ev.ID)
		return stats, false, err

	case PageChanged:
		pg, ok := e.findPage(ev.Path)
		if !ok {
			return e.applyRemovePage(stats, start, ev.Path)
		}
		p, pathErr := pagePath(pageRelPath(pg), e.pageProjectOf(pg))
		if pathErr != nil {
			return e.finish(stats, start), false, pathErr
		}
		e.pages[pg.Path] = p
		stats.Pages = 1
		stats, err = e.applyWrite(stats, start, p, RenderPage(pg, e.pageProjectOf(pg)))
		return stats, false, err

	case PageRemoved:
		return e.applyRemovePage(stats, start, ev.Path)

	default:
		return e.finish(stats, start), false, fmt.Errorf("pandosync: unknown event kind %q", ev.Kind)
	}
}

// applyWrite writes one document and reports whether the bytes were new.
func (e *Exporter) applyWrite(stats Stats, start time.Time, p string, data []byte) (Stats, error) {
	written, err := e.writeIfChanged(p, data)
	if err != nil {
		return e.finish(stats, start), err
	}
	if written {
		stats.Written = 1
	} else {
		stats.Skipped = 1
	}
	return e.finish(stats, start), nil
}

// applyRemoveItem unlinks the document of an item that is gone.
func (e *Exporter) applyRemoveItem(stats Stats, start time.Time, id core.ItemID) (Stats, error) {
	p, err := e.itemPathOf(id)
	if err != nil {
		return e.finish(stats, start), err
	}
	removed, err := e.removeDoc(p)
	if err != nil {
		return e.finish(stats, start), err
	}
	if removed {
		stats.Removed = 1
	}
	return e.finish(stats, start), nil
}

// applyRemovePage unlinks the document of a page that is gone. A page the
// exporter never saw cannot be located from its vault path alone — the corpus
// path depends on the docs folder it sat in — so that case falls back to a full
// export, which prunes it. Silently doing nothing would desync.
func (e *Exporter) applyRemovePage(stats Stats, start time.Time, vaultPath string) (Stats, bool, error) {
	p, ok := e.pages[vaultPath]
	if !ok {
		e.log.Info("pandosync: removed page is unknown, re-exporting", "path", vaultPath)
		return e.finish(stats, start), true, nil
	}
	removed, err := e.removeDoc(p)
	if err != nil {
		return e.finish(stats, start), false, err
	}
	delete(e.pages, vaultPath)
	if removed {
		stats.Removed = 1
	}
	return e.finish(stats, start), false, nil
}

// findItem resolves one item, through the Lookup fast path when the source has
// one and by scanning otherwise.
func (e *Exporter) findItem(id core.ItemID) (core.Item, bool) {
	if e.lookup != nil {
		return e.lookup.Item(id)
	}
	items := e.src.Items()
	for i := range items {
		if items[i].ID == id {
			return items[i], true
		}
	}
	return core.Item{}, false
}

// findPage resolves one page by its vault-relative path.
func (e *Exporter) findPage(vaultPath string) (*core.KBPage, bool) {
	if e.lookup != nil {
		return e.lookup.Page(vaultPath)
	}
	clean := path.Clean(vaultPath)
	for _, pg := range e.src.Pages() {
		if pg != nil && path.Clean(pg.Path) == clean {
			return pg, true
		}
	}
	return nil, false
}

// itemPathOf is itemPath with the exporter's fallback project applied.
func (e *Exporter) itemPathOf(id core.ItemID) (string, error) {
	return itemPath(id, e.project)
}

// projectOf is the project key written into an item's document.
func (e *Exporter) projectOf(it *core.Item) string { return projectOfItem(it.ID, e.project) }

// pageProjectOf is the project key a page is filed under.
func (e *Exporter) pageProjectOf(pg *core.KBPage) string {
	if pg.Project != "" {
		return string(pg.Project)
	}
	return e.project
}

// writeIfChanged writes the document only when its bytes differ from what is
// already there, so that an unchanged item keeps its mtime and Pando's own
// mtime skip keeps working.
func (e *Exporter) writeIfChanged(p string, data []byte) (bool, error) {
	want := sha256.Sum256(data)
	if have, ok := e.hashes[p]; ok && have == want {
		if _, err := e.fs.Stat(p); err == nil {
			return false, nil
		}
	} else if existing, err := e.fs.ReadFile(p); err == nil {
		if sha256.Sum256(existing) == want {
			e.hashes[p] = want
			return false, nil
		}
	}
	if err := e.atomicWrite(p, data); err != nil {
		delete(e.hashes, p)
		return false, err
	}
	e.hashes[p] = want
	return true, nil
}

// atomicWrite puts the bytes in a sibling temporary file and renames it into
// place, so that an importer scanning the corpus never reads a half-written
// document. The temporary file is a dot file without the .md extension, which
// keeps it out of the walk in the window before the rename.
func (e *Exporter) atomicWrite(p string, data []byte) error {
	if dir := path.Dir(p); dir != "." && dir != "" {
		if err := e.fs.MkdirAll(dir); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	tmp := tempPathFor(p)
	if err := e.fs.WriteFile(tmp, data); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := e.fs.Rename(tmp, p); err != nil {
		_ = e.fs.Remove(tmp)
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}

// removeDoc unlinks one corpus document and prunes the directories it emptied.
func (e *Exporter) removeDoc(p string) (bool, error) {
	err := e.fs.Remove(p)
	switch {
	case err == nil:
	case errors.Is(err, core.ErrNotExist):
		delete(e.hashes, p)
		return false, nil
	default:
		return false, fmt.Errorf("remove %s: %w", p, err)
	}
	delete(e.hashes, p)
	e.pruneEmptyDirs(path.Dir(p))
	return true, nil
}

// prune removes every corpus file that no source produced this round, plus any
// temporary file a previous crash left behind.
func (e *Exporter) prune(ctx context.Context, desired map[string][]byte) (int, error) {
	found, err := e.walk(ctx, ".")
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, p := range found {
		if _, keep := desired[p]; keep {
			continue
		}
		gone, err := e.removeDoc(p)
		if err != nil {
			e.log.Warn("pandosync: prune failed", "path", p, "error", err)
			continue
		}
		if gone {
			removed++
		}
	}
	return removed, nil
}

// walk lists every corpus file worth pruning under dir, depth first.
func (e *Exporter) walk(ctx context.Context, dir string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	entries, err := e.fs.ReadDir(dir)
	if err != nil {
		if errors.Is(err, core.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var out []string
	for _, entry := range entries {
		child := entry.Name
		if dir != "." {
			child = dir + "/" + entry.Name
		}
		if entry.IsDir {
			sub, err := e.walk(ctx, child)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
			continue
		}
		if isTempName(entry.Name) || path.Ext(entry.Name) == mdExt {
			out = append(out, child)
		}
	}
	return out, nil
}

// pruneEmptyDirs walks up from dir removing directories the deletion emptied,
// stopping at the corpus root and at the first directory that still has content.
func (e *Exporter) pruneEmptyDirs(dir string) {
	for dir != "." && dir != "/" && dir != "" {
		entries, err := e.fs.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := e.fs.Remove(dir); err != nil {
			return
		}
		dir = path.Dir(dir)
	}
}

// progress reports how far a full export has got, ignoring a nil callback.
func (e *Exporter) progress(done, total int) {
	if e.onProg != nil {
		e.onProg(Progress{Done: done, Total: total})
	}
}

// finish stamps a Stats with its duration and end time.
func (e *Exporter) finish(s Stats, start time.Time) Stats {
	end := e.now()
	s.At = end
	s.Duration = end.Sub(start)
	return s
}

// sortedKeys returns the map keys in a stable order, so that an export writes
// files in the same sequence on every run and a test can rely on it.
func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
