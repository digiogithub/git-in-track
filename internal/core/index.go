package core

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// Diagnostic codes this file produces that are not declared in errors.go. They
// are unexported and prefixed so that the shared catalog can grow without
// colliding with the indexer.
const (
	idxCodeIDKey         Code = "E-ID-KEY"
	idxCodeRefDangling   Code = "W-REF-DANGLING"
	idxCodeLinkBroken    Code = "W-LINK-BROKEN"
	idxCodeLinkAmbiguous Code = "W-LINK-AMBIGUOUS"
	idxCodeLayoutNested  Code = "W-LAYOUT-NESTED"
	idxCodeLayoutStray   Code = "W-LAYOUT-STRAY"
	idxCodeCommentOrphan Code = "W-CMT-ORPHAN"
)

// Warning is a finding recorded while indexing. It is a Diagnostic: the index
// never fails on a broken file, it records what is wrong and keeps going
// (docs/02 section 7.1, step 6).
type Warning = Diagnostic

// itemFolders maps the four flat item folders of a backlog to the item type the
// files in them must declare (R-LOC-3, R-LOC-4).
var itemFolders = []struct {
	Dir  string
	Type ItemType
}{
	{"epics", TypeEpic},
	{"stories", TypeStory},
	{"tasks", TypeTask},
	{"milestones", TypeMilestone},
}

// commentsDirName is the one folder of a backlog with a level of subfolders.
const commentsDirName = "comments"

// attachmentsDirName holds binaries and is never parsed.
const attachmentsDirName = "attachments"

// indexDirName and indexFileName are derived artifacts inside a backlog.
const (
	indexDirName  = "index"
	indexFileName = "index.json"
)

// maxWalkDepth bounds the knowledge-base walk so that a symlink loop reported by
// a host file system cannot hang the worker.
const maxWalkDepth = 32

// ProjectRef is a project backlog discovered in a vault: where its documentation
// folder is, where its .pmngr/ folder is, and what project.yaml says.
type ProjectRef struct {
	Key ProjectKey `json:"key"`
	// Name is the display name from project.yaml.
	Name string `json:"name"`
	// DocsPath is the vault-relative documentation folder, "." for the root.
	DocsPath string `json:"docs_path"`
	// BacklogPath is DocsPath + "/.pmngr".
	BacklogPath string `json:"backlog_path"`
	// ConfigPath is BacklogPath + "/project.yaml".
	ConfigPath string `json:"config_path"`
	// Config is the parsed configuration. It is nil only when project.yaml could
	// not be decoded at all, in which case Diagnostics says why.
	Config *ProjectConfig `json:"-"`
	// Diagnostics are the findings of project.yaml validation.
	Diagnostics []Diagnostic `json:"-"`
}

// DiscoverProjects walks a vault from root and returns every project backlog it
// finds: a `.pmngr/` folder holding a `project.yaml` (R-LOC-2). Dot folders other
// than `.pmngr`, `node_modules` and `.git` are never descended into.
//
// The result is sorted by documentation folder path, so two runs over the same
// tree return the same order. A project whose project.yaml fails validation is
// still returned, with its findings in Diagnostics: the app opens read-only
// rather than pretending the folder does not exist (docs/03 section 6.3).
func DiscoverProjects(fs FS, root string) ([]ProjectRef, error) {
	if fs == nil {
		return nil, errors.New("discover projects: nil file system")
	}
	if root == "" {
		root = "."
	}
	var out []ProjectRef
	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		if depth > maxWalkDepth {
			return nil
		}
		entries, err := readDirTolerant(fs, dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir {
				continue
			}
			child := joinPath(dir, e.Name)
			if e.Name == BacklogDirName {
				ref, ok, err := loadProjectRef(fs, dir, child)
				if err != nil {
					return err
				}
				if ok {
					out = append(out, ref)
				}
				continue
			}
			if skipDirName(e.Name) {
				continue
			}
			if err := walk(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(path.Clean(root), 0); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DocsPath < out[j].DocsPath })
	return out, nil
}

// loadProjectRef reads the project.yaml of a candidate backlog folder.
func loadProjectRef(fs FS, docsPath, backlogPath string) (ProjectRef, bool, error) {
	configPath := joinPath(backlogPath, ProjectFileName)
	data, err := fs.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return ProjectRef{}, false, nil
		}
		return ProjectRef{}, false, fmt.Errorf("read %s: %w", configPath, err)
	}
	ref := ProjectRef{
		DocsPath:    docsPath,
		BacklogPath: backlogPath,
		ConfigPath:  configPath,
	}
	cfg, cfgErr := LoadProjectConfig(data)
	if cfg == nil {
		ref.Diagnostics = []Diagnostic{{
			Code:     CodeProjSchema,
			Severity: SeverityError,
			Path:     configPath,
			Message:  fmt.Sprintf("cannot decode %s: %v", ProjectFileName, cfgErr),
		}}
		return ref, true, nil
	}
	ref.Config = cfg
	ref.Key = cfg.Key
	ref.Name = cfg.Name
	for _, d := range cfg.Validate() {
		d.Path = configPath
		ref.Diagnostics = append(ref.Diagnostics, d)
	}
	return ref, true, nil
}

// skipDirName reports the directories a vault walk never descends into.
func skipDirName(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "vendor":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// FileEventKind is what happened to a file.
type FileEventKind string

// The file event kinds accepted by ApplyFileEvents. They map one-to-one to the
// events a watcher (fsnotify natively, the File System Observer or a diff of a
// re-scan in the browser) reports.
const (
	FileCreated  FileEventKind = "create"
	FileModified FileEventKind = "write"
	FileRemoved  FileEventKind = "remove"
	FileRenamed  FileEventKind = "rename"
)

// FileEvent is one change to one file, with vault-relative paths.
type FileEvent struct {
	Kind FileEventKind `json:"kind"`
	Path string        `json:"path"`
	// OldPath is the previous path of a rename; it is ignored for other kinds.
	OldPath string `json:"old_path,omitempty"`
}

// IndexDelta is what changed in the index after applying a batch of file events.
// The lists are sorted and free of duplicates so that a UI can invalidate exactly
// the affected caches (docs/02 section 7.3, step 4).
type IndexDelta struct {
	Added   []ItemID `json:"added,omitempty"`
	Updated []ItemID `json:"updated,omitempty"`
	Removed []ItemID `json:"removed,omitempty"`

	PagesAdded   []string `json:"pages_added,omitempty"`
	PagesUpdated []string `json:"pages_updated,omitempty"`
	PagesRemoved []string `json:"pages_removed,omitempty"`

	// CommentsChanged lists the items whose comment thread changed.
	CommentsChanged []ItemID `json:"comments_changed,omitempty"`
	// ProjectsChanged lists the projects whose project.yaml was reloaded.
	ProjectsChanged []ProjectKey `json:"projects_changed,omitempty"`
}

// Empty reports whether the delta changed nothing.
func (d IndexDelta) Empty() bool {
	return len(d.Added) == 0 && len(d.Updated) == 0 && len(d.Removed) == 0 &&
		len(d.PagesAdded) == 0 && len(d.PagesUpdated) == 0 && len(d.PagesRemoved) == 0 &&
		len(d.CommentsChanged) == 0 && len(d.ProjectsChanged) == 0
}

// IndexStats summarizes a build. It is what `gintrack index --stats` prints and
// what the worker posts back to the UI when a pass finishes.
type IndexStats struct {
	Projects int              `json:"projects"`
	Files    int              `json:"files"`
	Items    int              `json:"items"`
	Comments int              `json:"comments"`
	Pages    int              `json:"pages"`
	ByType   map[ItemType]int `json:"by_type,omitempty"`
	Errors   int              `json:"errors"`
	Warnings int              `json:"warnings"`
	// Parsed counts the files actually read during the last pass; on an
	// incremental build it is much smaller than Files.
	Parsed   int           `json:"parsed"`
	Full     bool          `json:"full"`
	BuiltAt  Timestamp     `json:"built_at"`
	Duration time.Duration `json:"duration_ns"`
}

// FileMeta is the staleness triple of an indexed file (R-IDX-3): its size, its
// modification time and the rev of its contents.
type FileMeta struct {
	Size    int64     `json:"size"`
	ModTime Timestamp `json:"mtime,omitempty"`
	Rev     Rev       `json:"rev,omitempty"`
}

// Index is the in-memory projection of one or more project backlogs plus their
// knowledge bases. It is safe for concurrent use: readers take a read lock, a
// build or an incremental update takes the write lock.
//
// Bodies are kept in memory in this version so that text filters and Search work
// without touching the file system again; snapshots never carry them (R-IDX-2).
type Index struct {
	mu       sync.RWMutex
	fs       FS
	projects []ProjectRef

	// Primary stores, keyed by vault-relative path.
	itemsByPath    map[string]*Item
	commentsByPath map[string]*Comment
	pagesByPath    map[string]*KBPage
	itemLinks      map[string][]Wikilink     // wikilinks found in an item body
	itemAC         map[string]*ACCount       // acceptance-criteria tally of an item body
	itemCategory   map[string]StatusCategory // category hint restored from a snapshot
	fileProject    map[string]ProjectKey
	files          map[string]FileMeta
	fileDiags      map[string][]Diagnostic

	// Derived state, rebuilt after every pass.
	byID           map[ItemID]*Item
	commentsByItem map[ItemID][]*Comment
	children       map[ItemID][]ItemID
	counts         map[ItemType]int
	maxIDs         map[ProjectKey]map[ItemType]int
	graph          *Graph
	derivedDiags   []Diagnostic
	diagnostics    []Diagnostic
	stats          IndexStats
	fingerprint    string

	// Now supplies the build timestamp. It defaults to time.Now and exists so
	// that tests can pin a deterministic snapshot.
	Now func() time.Time
}

// NewIndex returns an empty index over a vault. Call Build to populate it, or
// Load to hydrate it from a snapshot.
func NewIndex(fs FS, projects []ProjectRef) *Index {
	ix := &Index{fs: fs, Now: time.Now}
	ix.projects = append([]ProjectRef(nil), projects...)
	ix.reset()
	return ix
}

// reset drops every entry. The caller holds the write lock.
func (ix *Index) reset() {
	ix.itemsByPath = make(map[string]*Item)
	ix.commentsByPath = make(map[string]*Comment)
	ix.pagesByPath = make(map[string]*KBPage)
	ix.itemLinks = make(map[string][]Wikilink)
	ix.itemAC = make(map[string]*ACCount)
	ix.itemCategory = make(map[string]StatusCategory)
	ix.fileProject = make(map[string]ProjectKey)
	ix.files = make(map[string]FileMeta)
	ix.fileDiags = make(map[string][]Diagnostic)
	ix.byID = make(map[ItemID]*Item)
	ix.commentsByItem = make(map[ItemID][]*Comment)
	ix.children = make(map[ItemID][]ItemID)
	ix.counts = make(map[ItemType]int)
	ix.maxIDs = make(map[ProjectKey]map[ItemType]int)
	ix.graph = newGraph()
	ix.derivedDiags = nil
	ix.diagnostics = nil
}

func (ix *Index) now() time.Time {
	if ix.Now == nil {
		return time.Now()
	}
	return ix.Now()
}

// Projects returns the projects the index covers.
func (ix *Index) Projects() []ProjectRef {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return append([]ProjectRef(nil), ix.projects...)
}

// Project returns the discovered project with the given key.
func (ix *Index) Project(key ProjectKey) (ProjectRef, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	for _, p := range ix.projects {
		if p.Key == key {
			return p, true
		}
	}
	return ProjectRef{}, false
}

// Stats returns the statistics of the last pass.
func (ix *Index) Stats() IndexStats {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.stats.clone()
}

func (s IndexStats) clone() IndexStats {
	out := s
	out.ByType = make(map[ItemType]int, len(s.ByType))
	for k, v := range s.ByType {
		out.ByType[k] = v
	}
	return out
}

// Warnings returns every finding recorded while indexing, ordered by path and
// code. It includes error-severity findings such as a file that failed to parse,
// because a build never stops on one: the diagnostic is the report.
func (ix *Index) Warnings() []Warning {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return append([]Warning(nil), ix.diagnostics...)
}

// LinkGraph returns the current link graph.
func (ix *Index) LinkGraph() *Graph {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.graph
}

// Fingerprint is a stable hash of everything the index was built from: the
// project keys and the (path, size, rev) triple of every indexed file. A cache
// entry whose fingerprint differs from a fresh scan is stale.
func (ix *Index) Fingerprint() string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.fingerprint
}

// Build scans every project of the vault and (re)builds the index.
//
// With full set, every file is read and parsed. Otherwise a file whose size and
// modification time match the entry already held is kept as it is, which is what
// makes a warm open cheap (R-IDX-3). Parse failures are recorded and never abort
// the pass; the returned error is reserved for a file system that cannot be read
// and for a cancelled context.
func (ix *Index) Build(ctx context.Context, full bool) (IndexStats, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	start := ix.now()
	if full {
		ix.reset()
	}
	pass := &buildPass{seen: make(map[string]bool, len(ix.files)), full: full}
	for i := range ix.projects {
		if err := checkCancelled(ctx); err != nil {
			return ix.stats.clone(), err
		}
		p := ix.projects[i]
		if err := ix.scanBacklog(ctx, p, pass); err != nil {
			return ix.stats.clone(), err
		}
		if err := ix.scanKnowledgeBase(ctx, p, pass); err != nil {
			return ix.stats.clone(), err
		}
	}
	ix.prune(pass.seen)
	ix.rebuild()

	ix.stats.Full = full
	ix.stats.Parsed = pass.parsed
	ix.stats.BuiltAt = NewTimestamp(start)
	ix.stats.Duration = ix.now().Sub(start)
	return ix.stats.clone(), nil
}

// buildPass carries the mutable state of one scan.
type buildPass struct {
	seen   map[string]bool
	parsed int
	full   bool
}

// scanBacklog indexes .pmngr/ of one project.
func (ix *Index) scanBacklog(ctx context.Context, p ProjectRef, pass *buildPass) error {
	pass.seen[p.ConfigPath] = true
	if info, err := ix.fs.Stat(p.ConfigPath); err == nil {
		ix.files[p.ConfigPath] = FileMeta{Size: info.Size, ModTime: NewTimestamp(info.ModTime)}
	}

	known := map[string]bool{
		ProjectFileName:    true,
		commentsDirName:    true,
		attachmentsDirName: true,
		indexDirName:       true,
		indexFileName:      true,
	}
	for _, f := range itemFolders {
		known[f.Dir] = true
		if err := ix.scanItemFolder(ctx, p, f.Dir, f.Type, pass); err != nil {
			return err
		}
	}
	if err := ix.scanComments(ctx, p, pass); err != nil {
		return err
	}

	entries, err := readDirTolerant(ix.fs, p.BacklogPath)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if known[e.Name] {
			continue
		}
		stray := joinPath(p.BacklogPath, e.Name)
		ix.setFileDiags(stray, []Diagnostic{{
			Code:     idxCodeLayoutStray,
			Severity: SeverityWarning,
			Path:     stray,
			Message:  "file is not part of the backlog layout and is ignored",
		}})
		pass.seen[stray] = true
		ix.files[stray] = FileMeta{}
	}
	return nil
}

// scanItemFolder indexes one flat item folder.
func (ix *Index) scanItemFolder(ctx context.Context, p ProjectRef, dir string, typ ItemType, pass *buildPass) error {
	base := joinPath(p.BacklogPath, dir)
	entries, err := readDirTolerant(ix.fs, base)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := checkCancelled(ctx); err != nil {
			return err
		}
		full := joinPath(base, e.Name)
		if e.IsDir {
			// R-LOC-4: item folders are flat; a nested folder is reported and
			// its Markdown files are not indexed.
			if err := ix.reportNested(full, pass, 0); err != nil {
				return err
			}
			continue
		}
		if !isMarkdown(e.Name) {
			ix.markStray(full, pass)
			continue
		}
		if err := ix.loadItemFile(p, typ, full, pass); err != nil {
			return err
		}
	}
	return nil
}

// reportNested records W-LAYOUT-NESTED for every Markdown file under a folder
// that should not exist.
func (ix *Index) reportNested(dir string, pass *buildPass, depth int) error {
	if depth > maxWalkDepth {
		return nil
	}
	entries, err := readDirTolerant(ix.fs, dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		full := joinPath(dir, e.Name)
		if e.IsDir {
			if err := ix.reportNested(full, pass, depth+1); err != nil {
				return err
			}
			continue
		}
		pass.seen[full] = true
		ix.files[full] = FileMeta{}
		ix.setFileDiags(full, []Diagnostic{{
			Code:     idxCodeLayoutNested,
			Severity: SeverityWarning,
			Path:     full,
			Message:  "item folders are flat; this file is not indexed",
		}})
	}
	return nil
}

// markStray records W-LAYOUT-STRAY for a non-Markdown file under .pmngr/.
func (ix *Index) markStray(full string, pass *buildPass) {
	pass.seen[full] = true
	ix.files[full] = FileMeta{}
	ix.setFileDiags(full, []Diagnostic{{
		Code:     idxCodeLayoutStray,
		Severity: SeverityWarning,
		Path:     full,
		Message:  "only Markdown files are indexed here",
	}})
}

// scanComments indexes comments/<ITEM-ID>/*.md.
func (ix *Index) scanComments(ctx context.Context, p ProjectRef, pass *buildPass) error {
	base := joinPath(p.BacklogPath, commentsDirName)
	folders, err := readDirTolerant(ix.fs, base)
	if err != nil {
		return err
	}
	for _, folder := range folders {
		if err := checkCancelled(ctx); err != nil {
			return err
		}
		full := joinPath(base, folder.Name)
		if !folder.IsDir {
			ix.markStray(full, pass)
			continue
		}
		entries, err := readDirTolerant(ix.fs, full)
		if err != nil {
			return err
		}
		for _, e := range entries {
			filePath := joinPath(full, e.Name)
			if e.IsDir || !isMarkdown(e.Name) {
				ix.markStray(filePath, pass)
				continue
			}
			if err := ix.loadCommentFile(p, filePath, pass); err != nil {
				return err
			}
		}
	}
	return nil
}

// scanKnowledgeBase indexes every Markdown page of the documentation folder that
// lives outside .pmngr/.
func (ix *Index) scanKnowledgeBase(ctx context.Context, p ProjectRef, pass *buildPass) error {
	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		if depth > maxWalkDepth {
			return nil
		}
		if err := checkCancelled(ctx); err != nil {
			return err
		}
		entries, err := readDirTolerant(ix.fs, dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			full := joinPath(dir, e.Name)
			if e.IsDir {
				if e.Name == BacklogDirName || skipDirName(e.Name) {
					continue
				}
				if err := walk(full, depth+1); err != nil {
					return err
				}
				continue
			}
			if !isMarkdown(e.Name) {
				continue
			}
			if err := ix.loadPageFile(p, full, pass); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(p.DocsPath, 0)
}

// loadItemFile reads and parses one item file, reusing the cached entry when the
// file has not changed.
func (ix *Index) loadItemFile(p ProjectRef, typ ItemType, filePath string, pass *buildPass) error {
	pass.seen[filePath] = true
	data, meta, reused, err := ix.readIfChanged(filePath, pass, func() bool {
		_, ok := ix.itemsByPath[filePath]
		return ok
	})
	if err != nil || reused {
		return err
	}

	ix.fileProject[filePath] = p.Key
	it, parseErr := ParseItem(filePath, data)
	if parseErr != nil {
		delete(ix.itemsByPath, filePath)
		delete(ix.itemLinks, filePath)
		delete(ix.itemAC, filePath)
		ix.setFileDiags(filePath, []Diagnostic{diagnosticOf(parseErr, filePath)})
		ix.files[filePath] = meta
		return nil
	}

	var diags []Diagnostic
	if it.Type != typ {
		diags = append(diags, Diagnostic{
			Code: CodeFMType, Severity: SeverityError, Path: filePath, Field: "type",
			Message: fmt.Sprintf("type %q does not match the %s/ folder", it.Type, path.Base(path.Dir(filePath))),
		})
	}
	if key, _, _, err := ParseItemID(string(it.ID)); err == nil && p.Key != "" && key != p.Key {
		diags = append(diags, Diagnostic{
			Code: idxCodeIDKey, Severity: SeverityError, Path: filePath, Field: "id",
			Message: fmt.Sprintf("id prefix %q does not match the project key %q", key, p.Key),
		})
	}
	if want := FileName(it.ID, it.Title); it.ID != "" && want != path.Base(filePath) {
		diags = append(diags, Diagnostic{
			Code: CodeWarnSlugStale, Severity: SeverityWarning, Path: filePath,
			Message: fmt.Sprintf("file name should be %q", want),
		})
	}

	meta.Rev = it.Rev
	ix.files[filePath] = meta
	ix.itemsByPath[filePath] = it
	_, ix.itemLinks[filePath], _ = scanMarkdown(it.Body)
	delete(ix.itemAC, filePath)
	if total, done := checklistCounts(it.Body); total > 0 {
		ix.itemAC[filePath] = &ACCount{Total: total, Done: done}
	}
	ix.setFileDiags(filePath, diags)
	return nil
}

// loadCommentFile reads and parses one comment file.
func (ix *Index) loadCommentFile(p ProjectRef, filePath string, pass *buildPass) error {
	pass.seen[filePath] = true
	data, meta, reused, err := ix.readIfChanged(filePath, pass, func() bool {
		_, ok := ix.commentsByPath[filePath]
		return ok
	})
	if err != nil || reused {
		return err
	}

	ix.fileProject[filePath] = p.Key
	c, parseErr := ParseComment(filePath, data)
	if parseErr != nil {
		delete(ix.commentsByPath, filePath)
		ix.setFileDiags(filePath, []Diagnostic{diagnosticOf(parseErr, filePath)})
		ix.files[filePath] = meta
		return nil
	}
	meta.Rev = c.Rev
	ix.files[filePath] = meta
	ix.commentsByPath[filePath] = c
	ix.setFileDiags(filePath, nil)
	return nil
}

// loadPageFile reads and indexes one knowledge-base page.
func (ix *Index) loadPageFile(p ProjectRef, filePath string, pass *buildPass) error {
	pass.seen[filePath] = true
	data, meta, reused, err := ix.readIfChanged(filePath, pass, func() bool {
		_, ok := ix.pagesByPath[filePath]
		return ok
	})
	if err != nil || reused {
		return err
	}

	rel := relativeTo(p.DocsPath, filePath)
	page := ParsePage(filePath, rel, data)
	page.Project = p.Key
	if page.Updated.IsZero() {
		page.Updated = meta.ModTime
	}
	meta.Rev = page.Rev
	ix.files[filePath] = meta
	ix.fileProject[filePath] = p.Key
	ix.pagesByPath[filePath] = page
	ix.setFileDiags(filePath, nil)
	return nil
}

// readIfChanged stats a file and reads it unless the cached entry is still
// valid. It reports reused when the caller can keep the entry it already has.
func (ix *Index) readIfChanged(filePath string, pass *buildPass, cached func() bool) (data []byte, meta FileMeta, reused bool, err error) {
	info, err := ix.fs.Stat(filePath)
	if err != nil && !errors.Is(err, ErrNotExist) {
		return nil, FileMeta{}, false, fmt.Errorf("stat %s: %w", filePath, err)
	}
	meta = FileMeta{Size: info.Size, ModTime: NewTimestamp(info.ModTime)}
	if !pass.full && cached() {
		if prev, ok := ix.files[filePath]; ok && prev.Size == meta.Size && prev.ModTime.Equal(meta.ModTime.Time) {
			return nil, prev, true, nil
		}
	}
	data, err = ix.fs.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			ix.forget(filePath)
			delete(pass.seen, filePath)
			return nil, meta, true, nil
		}
		return nil, meta, false, fmt.Errorf("read %s: %w", filePath, err)
	}
	pass.parsed++
	meta.Size = int64(len(data))
	return data, meta, false, nil
}

// prune drops every entry whose file was not seen during the pass.
func (ix *Index) prune(seen map[string]bool) {
	for p := range ix.files {
		if !seen[p] {
			ix.forget(p)
		}
	}
}

// forget removes every trace of one file path.
func (ix *Index) forget(filePath string) {
	delete(ix.files, filePath)
	delete(ix.itemsByPath, filePath)
	delete(ix.commentsByPath, filePath)
	delete(ix.pagesByPath, filePath)
	delete(ix.itemLinks, filePath)
	delete(ix.itemAC, filePath)
	delete(ix.itemCategory, filePath)
	delete(ix.fileProject, filePath)
	delete(ix.fileDiags, filePath)
}

// setFileDiags replaces the findings attached to one file.
func (ix *Index) setFileDiags(filePath string, diags []Diagnostic) {
	if len(diags) == 0 {
		delete(ix.fileDiags, filePath)
		return
	}
	ix.fileDiags[filePath] = diags
}

// ApplyFileEvents re-parses only the files a watcher reported and returns what
// changed. A rename is handled as a removal plus an insertion, with id
// continuity coming from the front matter, so a card does not disappear from a
// board when a file is renamed (docs/02 section 7.3).
func (ix *Index) ApplyFileEvents(ctx context.Context, events []FileEvent) (IndexDelta, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	before := ix.identitySnapshot()
	beforePages := ix.pageSnapshot()
	beforeComments := ix.commentSnapshot()
	pass := &buildPass{seen: make(map[string]bool), full: true}
	var changedProjects []ProjectKey

	for _, ev := range events {
		if err := checkCancelled(ctx); err != nil {
			return IndexDelta{}, err
		}
		switch ev.Kind {
		case FileRemoved:
			ix.forget(path.Clean(ev.Path))
		case FileRenamed:
			if ev.OldPath != "" {
				ix.forget(path.Clean(ev.OldPath))
			}
			if err := ix.reload(path.Clean(ev.Path), pass, &changedProjects); err != nil {
				return IndexDelta{}, err
			}
		case FileCreated, FileModified:
			if err := ix.reload(path.Clean(ev.Path), pass, &changedProjects); err != nil {
				return IndexDelta{}, err
			}
		default:
			return IndexDelta{}, fmt.Errorf("apply file events: unknown kind %q", ev.Kind)
		}
	}

	ix.rebuild()
	ix.stats.Parsed = pass.parsed
	ix.stats.Full = false

	delta := ix.diffIdentities(before, beforePages, beforeComments)
	delta.ProjectsChanged = dedupKeys(changedProjects)
	return delta, nil
}

// reload re-reads one path, dispatching on where it sits in the layout. A path
// outside every project, or one that no longer exists, is simply forgotten.
func (ix *Index) reload(filePath string, pass *buildPass, changedProjects *[]ProjectKey) error {
	p, kind, typ, ok := ix.classify(filePath)
	if !ok {
		ix.forget(filePath)
		return nil
	}
	if _, err := ix.fs.Stat(filePath); err != nil {
		if errors.Is(err, ErrNotExist) {
			ix.forget(filePath)
			return nil
		}
		return fmt.Errorf("stat %s: %w", filePath, err)
	}
	switch kind {
	case fileItem:
		return ix.loadItemFile(p, typ, filePath, pass)
	case fileComment:
		return ix.loadCommentFile(p, filePath, pass)
	case filePage:
		return ix.loadPageFile(p, filePath, pass)
	case fileConfig:
		data, err := ix.fs.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", filePath, err)
		}
		cfg, cfgErr := LoadProjectConfig(data)
		if cfg != nil {
			for i := range ix.projects {
				if ix.projects[i].ConfigPath != filePath {
					continue
				}
				ix.projects[i].Config = cfg
				ix.projects[i].Key = cfg.Key
				ix.projects[i].Name = cfg.Name
				ix.projects[i].Diagnostics = nil
				for _, d := range cfg.Validate() {
					d.Path = filePath
					ix.projects[i].Diagnostics = append(ix.projects[i].Diagnostics, d)
				}
				*changedProjects = append(*changedProjects, cfg.Key)
			}
		} else if cfgErr != nil {
			ix.setFileDiags(filePath, []Diagnostic{{
				Code: CodeProjSchema, Severity: SeverityError, Path: filePath,
				Message: fmt.Sprintf("cannot decode %s: %v", ProjectFileName, cfgErr),
			}})
		}
		return nil
	default:
		return nil
	}
}

// fileKind is where a path sits in the layout.
type fileKind int

const (
	fileOther fileKind = iota
	fileItem
	fileComment
	filePage
	fileConfig
)

// classify locates a path inside the projects the index covers.
func (ix *Index) classify(filePath string) (ProjectRef, fileKind, ItemType, bool) {
	for _, p := range ix.projects {
		if filePath == p.ConfigPath {
			return p, fileConfig, "", true
		}
		if rel, ok := underDir(p.BacklogPath, filePath); ok {
			parts := strings.Split(rel, "/")
			switch {
			case len(parts) == 2 && isMarkdown(parts[1]):
				for _, f := range itemFolders {
					if f.Dir == parts[0] {
						return p, fileItem, f.Type, true
					}
				}
			case len(parts) == 3 && parts[0] == commentsDirName && isMarkdown(parts[2]):
				return p, fileComment, "", true
			}
			return p, fileOther, "", false
		}
		if _, ok := underDir(p.DocsPath, filePath); ok && isMarkdown(filePath) {
			return p, filePage, "", true
		}
	}
	return ProjectRef{}, fileOther, "", false
}

// identity is the pair the delta compares: two entries with the same rev at the
// same path are the same item.
type identity struct {
	rev  Rev
	path string
}

func (ix *Index) identitySnapshot() map[ItemID]identity {
	out := make(map[ItemID]identity, len(ix.byID))
	for id, it := range ix.byID {
		out[id] = identity{rev: it.Rev, path: it.Path}
	}
	return out
}

func (ix *Index) pageSnapshot() map[string]Rev {
	out := make(map[string]Rev, len(ix.pagesByPath))
	for p, page := range ix.pagesByPath {
		out[p] = page.Rev
	}
	return out
}

// diffIdentities turns the before/after states into a delta.
func (ix *Index) diffIdentities(before map[ItemID]identity, beforePages map[string]Rev, beforeComments map[ItemID]string) IndexDelta {
	var d IndexDelta
	after := ix.identitySnapshot()
	for id, now := range after {
		prev, ok := before[id]
		switch {
		case !ok:
			d.Added = append(d.Added, id)
		case prev != now:
			d.Updated = append(d.Updated, id)
		}
	}
	for id := range before {
		if _, ok := after[id]; !ok {
			d.Removed = append(d.Removed, id)
		}
	}
	afterPages := ix.pageSnapshot()
	for p, rev := range afterPages {
		prev, ok := beforePages[p]
		switch {
		case !ok:
			d.PagesAdded = append(d.PagesAdded, p)
		case prev != rev:
			d.PagesUpdated = append(d.PagesUpdated, p)
		}
	}
	for p := range beforePages {
		if _, ok := afterPages[p]; !ok {
			d.PagesRemoved = append(d.PagesRemoved, p)
		}
	}
	sortIDs(d.Added)
	sortIDs(d.Updated)
	sortIDs(d.Removed)
	sort.Strings(d.PagesAdded)
	sort.Strings(d.PagesUpdated)
	sort.Strings(d.PagesRemoved)

	afterComments := ix.commentSnapshot()
	for id, sum := range afterComments {
		if beforeComments[id] != sum {
			d.CommentsChanged = append(d.CommentsChanged, id)
		}
	}
	for id := range beforeComments {
		if _, ok := afterComments[id]; !ok {
			d.CommentsChanged = append(d.CommentsChanged, id)
		}
	}
	sortIDs(d.CommentsChanged)
	return d
}

// commentSnapshot summarizes every comment thread as one comparable string.
func (ix *Index) commentSnapshot() map[ItemID]string {
	out := make(map[ItemID]string, len(ix.commentsByItem))
	for id, list := range ix.commentsByItem {
		var b strings.Builder
		for _, c := range list {
			b.WriteString(c.Path)
			b.WriteString("@")
			b.WriteString(string(c.Rev))
			b.WriteString(";")
		}
		out[id] = b.String()
	}
	return out
}

// rebuild recomputes every derived structure from the primary stores.
//
// It is O(items + pages) and runs after both a full build and an incremental
// update, which is what keeps the two paths impossible to drift apart. On the
// corpora this tool targets that pass costs a few milliseconds; making the
// derived state incremental per file is a later optimisation, not a correctness
// requirement.
func (ix *Index) rebuild() {
	ix.byID = make(map[ItemID]*Item, len(ix.itemsByPath))
	ix.commentsByItem = make(map[ItemID][]*Comment, len(ix.commentsByPath))
	ix.children = make(map[ItemID][]ItemID)
	ix.counts = make(map[ItemType]int)
	ix.maxIDs = make(map[ProjectKey]map[ItemType]int)
	ix.derivedDiags = nil
	graph := newGraph()

	// Items, in path order so that the first file claiming a duplicate id always
	// wins, whatever the map iteration order was.
	for _, p := range sortedPaths(ix.itemsByPath) {
		it := ix.itemsByPath[p]
		if it.ID == "" {
			continue
		}
		if prev, ok := ix.byID[it.ID]; ok {
			ix.derivedDiags = append(ix.derivedDiags, Diagnostic{
				Code: CodeIDDuplicate, Severity: SeverityError, Path: p, Field: "id",
				Message: fmt.Sprintf("id %s is also declared by %s", it.ID, prev.Path),
			})
			continue
		}
		ix.byID[it.ID] = it
		ix.counts[it.Type]++
		key := ix.projectOf(it)
		if _, _, n, err := ParseItemID(string(it.ID)); err == nil {
			if ix.maxIDs[key] == nil {
				ix.maxIDs[key] = make(map[ItemType]int, len(itemFolders))
			}
			if n > ix.maxIDs[key][it.Type] {
				ix.maxIDs[key][it.Type] = n
			}
		}
	}

	// Comments, sorted by path, which is chronological because the file name
	// starts with a UTC timestamp (docs/03 section 11.1).
	for _, p := range sortedPaths(ix.commentsByPath) {
		c := ix.commentsByPath[p]
		if c.Item == "" {
			continue
		}
		ix.commentsByItem[c.Item] = append(ix.commentsByItem[c.Item], c)
	}

	// Hierarchy and typed links.
	for _, id := range sortedIDs(ix.byID) {
		it := ix.byID[id]
		parent := it.Parent
		if parent == "" {
			parent = it.Epic
		}
		if parent != "" {
			graph.addParent(it.ID, parent)
		}
		if it.Milestone != "" {
			graph.addMilestone(it.ID, it.Milestone)
		}
		for _, l := range it.Links {
			graph.addLink(it.ID, l)
		}
	}
	ix.resolveReferences(graph)
	graph.finish()
	for parent, kids := range graph.children {
		ix.children[parent] = kids
	}
	ix.graph = graph

	ix.checkReferentialIntegrity()
	ix.checkCounters()
	ix.collectDiagnostics()
	ix.computeFingerprint()
	ix.updateCounts()
}

// resolveReferences turns every wikilink of every page and item body into a
// graph edge, recording the ones that do not resolve (R-WIKI-2).
func (ix *Index) resolveReferences(graph *Graph) {
	byRel := make(map[ProjectKey]map[string]*KBPage)
	byBase := make(map[ProjectKey]map[string][]*KBPage)
	for _, p := range sortedPaths(ix.pagesByPath) {
		page := ix.pagesByPath[p]
		if byRel[page.Project] == nil {
			byRel[page.Project] = make(map[string]*KBPage)
			byBase[page.Project] = make(map[string][]*KBPage)
		}
		byRel[page.Project][page.Slug()] = page
		base := strings.TrimSuffix(path.Base(page.RelPath), ".md")
		byBase[page.Project][base] = append(byBase[page.Project][base], page)
	}

	resolve := func(from NodeID, sourcePath string, scope ProjectKey, links []Wikilink) {
		for _, w := range links {
			project := w.Project
			if project == "" {
				project = scope
			}
			ref := Reference{From: from, Target: w.Target, Anchor: w.Anchor, Text: w.Text, Project: project}
			switch {
			case w.IsItem():
				if it, ok := ix.byID[w.Item]; ok {
					ref.To = ItemNode(it.ID)
					ref.Resolved = true
				}
			default:
				slug := strings.TrimSuffix(w.Target, ".md")
				if page, ok := byRel[project][slug]; ok {
					ref.To = PageNode(page.Path)
					ref.Resolved = true
					break
				}
				matches := byBase[project][path.Base(slug)]
				switch len(matches) {
				case 0:
				case 1:
					ref.To = PageNode(matches[0].Path)
					ref.Resolved = true
				default:
					ix.derivedDiags = append(ix.derivedDiags, Diagnostic{
						Code: idxCodeLinkAmbiguous, Severity: SeverityWarning, Path: sourcePath,
						Message: fmt.Sprintf("wikilink [[%s]] matches %d pages", w.Raw, len(matches)),
					})
				}
			}
			if !ref.Resolved && ref.To == "" {
				ix.derivedDiags = append(ix.derivedDiags, Diagnostic{
					Code: idxCodeLinkBroken, Severity: SeverityWarning, Path: sourcePath,
					Message: fmt.Sprintf("wikilink [[%s]] does not resolve", w.Raw),
				})
			}
			graph.addReference(ref)
		}
	}

	for _, p := range sortedPaths(ix.pagesByPath) {
		page := ix.pagesByPath[p]
		resolve(PageNode(page.Path), page.Path, page.Project, page.Links)
	}
	for _, p := range sortedPaths(ix.itemsByPath) {
		it := ix.itemsByPath[p]
		if it.ID == "" || ix.byID[it.ID] != it {
			continue
		}
		resolve(ItemNode(it.ID), it.Path, ix.projectOf(it), ix.itemLinks[p])
	}
}

// checkReferentialIntegrity reports dangling parents, milestones and link
// targets, and comment folders with no item (W-REF-DANGLING, W-CMT-ORPHAN).
func (ix *Index) checkReferentialIntegrity() {
	dangling := func(it *Item, field string, target string) {
		if target == "" {
			return
		}
		bare := ItemID(bareTarget(target))
		if _, ok := ix.byID[bare]; ok {
			return
		}
		// A qualified reference into a project this vault does not hold is a
		// remote reference, not a dangling one (docs/03 section 12.4).
		if key := targetProject(target); key != "" && key != ix.projectOf(it) && !ix.hasProject(key) {
			return
		}
		ix.derivedDiags = append(ix.derivedDiags, Diagnostic{
			Code: idxCodeRefDangling, Severity: SeverityWarning, Path: it.Path, Field: field,
			Message: fmt.Sprintf("%s points at unknown item %s", field, target),
		})
	}
	for _, id := range sortedIDs(ix.byID) {
		it := ix.byID[id]
		parent := it.Parent
		field := "parent"
		if parent == "" && it.Epic != "" {
			parent, field = it.Epic, "epic"
		}
		dangling(it, field, string(parent))
		dangling(it, "milestone", string(it.Milestone))
		for _, l := range it.Links {
			dangling(it, "links."+string(l.Kind), l.Target)
		}
	}
	for _, item := range sortedCommentKeys(ix.commentsByItem) {
		if _, ok := ix.byID[item]; ok {
			continue
		}
		ix.derivedDiags = append(ix.derivedDiags, Diagnostic{
			Code: idxCodeCommentOrphan, Severity: SeverityWarning,
			Path:    path.Dir(ix.commentsByItem[item][0].Path),
			Message: fmt.Sprintf("comments folder of unknown item %s", item),
		})
	}
}

// checkCounters reports a project.yaml counter below the highest id scanned
// (W-PROJ-COUNTER-STALE). The scan always wins; the counter is a hint
// (docs/03 section 4.1).
func (ix *Index) checkCounters() {
	for _, p := range ix.projects {
		if p.Config == nil {
			continue
		}
		for _, f := range itemFolders {
			maxSeen := ix.maxIDs[p.Key][f.Type]
			hint := p.Config.IDAllocation.Counters[f.Type]
			if maxSeen > hint {
				ix.derivedDiags = append(ix.derivedDiags, Diagnostic{
					Code: CodeWarnCounterStale, Severity: SeverityWarning, Path: p.ConfigPath,
					Field:   "id_allocation.counters." + string(f.Type),
					Message: fmt.Sprintf("counter %d is below the highest id scanned %d", hint, maxSeen),
				})
			}
		}
	}
}

// collectDiagnostics merges the per-file findings with the derived ones and the
// project configuration ones, in a deterministic order.
func (ix *Index) collectDiagnostics() {
	out := make([]Diagnostic, 0, len(ix.fileDiags)+len(ix.derivedDiags))
	for _, p := range ix.projects {
		out = append(out, p.Diagnostics...)
	}
	for _, p := range sortedPaths(ix.fileDiags) {
		out = append(out, ix.fileDiags[p]...)
	}
	out = append(out, ix.derivedDiags...)
	sortIndexDiagnostics(out)
	ix.diagnostics = out
}

// updateCounts refreshes the parts of IndexStats derived from the stores.
func (ix *Index) updateCounts() {
	byType := make(map[ItemType]int, len(ix.counts))
	for k, v := range ix.counts {
		byType[k] = v
	}
	errCount, warnCount := 0, 0
	for _, d := range ix.diagnostics {
		if d.Severity == SeverityError {
			errCount++
			continue
		}
		warnCount++
	}
	ix.stats.Projects = len(ix.projects)
	ix.stats.Files = len(ix.files)
	ix.stats.Items = len(ix.byID)
	ix.stats.Comments = len(ix.commentsByPath)
	ix.stats.Pages = len(ix.pagesByPath)
	ix.stats.ByType = byType
	ix.stats.Errors = errCount
	ix.stats.Warnings = warnCount
}

// projectOf returns the project key an item belongs to: the key embedded in its
// id, or the project whose folder holds the file.
func (ix *Index) projectOf(it *Item) ProjectKey {
	if key, _, _, err := ParseItemID(string(it.ID)); err == nil {
		return key
	}
	return ix.fileProject[it.Path]
}

// hasProject reports whether the vault holds a project with that key.
func (ix *Index) hasProject(key ProjectKey) bool {
	for _, p := range ix.projects {
		if p.Key == key {
			return true
		}
	}
	return false
}

// diagnosticOf turns a parse failure into a Diagnostic.
func diagnosticOf(err error, filePath string) Diagnostic {
	var pe *ParseError
	if errors.As(err, &pe) {
		return pe.Diagnostic()
	}
	return Diagnostic{
		Code: CodeFMYAML, Severity: SeverityError, Path: filePath, Message: err.Error(),
	}
}

// checkCancelled reports a cancelled or expired context as a wrapped error, so
// that a long pass can stop between two files without losing the cause.
func checkCancelled(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("index pass: %w", err)
	}
	return nil
}

// readDirTolerant lists a directory, treating a missing one as empty (R-LOC-3).
func readDirTolerant(fs FS, dir string) ([]DirEntry, error) {
	entries, err := fs.ReadDir(dir)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// joinPath joins two vault-relative path elements.
func joinPath(base, elem string) string {
	if base == "" || base == "." {
		return elem
	}
	return base + "/" + elem
}

// underDir reports whether p sits under dir and returns the remainder.
func underDir(dir, p string) (string, bool) {
	if dir == "" || dir == "." {
		return p, true
	}
	if p == dir {
		return "", true
	}
	if !strings.HasPrefix(p, dir+"/") {
		return "", false
	}
	return p[len(dir)+1:], true
}

// relativeTo returns p relative to dir, or p when it is not under it.
func relativeTo(dir, p string) string {
	if rel, ok := underDir(dir, p); ok {
		return rel
	}
	return p
}

// isMarkdown reports whether a file name is a Markdown document.
func isMarkdown(name string) bool { return strings.HasSuffix(name, ".md") }

// sortedPaths returns the keys of a path-keyed map, sorted.
func sortedPaths[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedIDs returns the keys of an id-keyed map, sorted.
func sortedIDs[V any](m map[ItemID]V) []ItemID {
	out := make([]ItemID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortIDs(out)
	return out
}

func sortedCommentKeys(m map[ItemID][]*Comment) []ItemID {
	out := make([]ItemID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortIDs(out)
	return out
}

func sortIDs(ids []ItemID) {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
}

func dedupKeys(in []ProjectKey) []ProjectKey {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool { return in[i] < in[j] })
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

// sortIndexDiagnostics orders findings by path, code, field and message so that
// two builds of the same tree report them identically.
func sortIndexDiagnostics(d []Diagnostic) {
	sort.SliceStable(d, func(i, j int) bool {
		a, b := d[i], d[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		return a.Message < b.Message
	})
}
