package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

// SnapshotSchema is the version of the serialized index this build writes and
// reads (docs/03 section 15, docs/04 section 6.2).
const SnapshotSchema = 1

// SnapshotGenerator names the producer of a snapshot. The CLI overrides it with
// its own version string; the default carries none so that a snapshot written by
// two builds of the same core is byte-identical.
var SnapshotGenerator = "gintrack-core"

// Snapshot is the serializable form of the whole index: the local, richer form
// of docs/03 section 15, extended with the per-file staleness metadata the
// browser cache needs (R-IDX-3).
//
// It carries front-matter-derived data only — never a body (R-IDX-2) — and it is
// always reconstructible from the files alone (R-IDX-1).
type Snapshot struct {
	Schema      int                             `json:"schema"`
	Generated   Timestamp                       `json:"generated"`
	Generator   string                          `json:"generator"`
	Fingerprint string                          `json:"fingerprint"`
	Projects    []SnapshotProject               `json:"projects"`
	Counts      map[ItemType]int                `json:"counts"`
	MaxIDs      map[ProjectKey]map[ItemType]int `json:"max_ids,omitempty"`
	Items       []SnapshotItem                  `json:"items"`
	Comments    []SnapshotComment               `json:"comments,omitempty"`
	Pages       []SnapshotPage                  `json:"pages,omitempty"`
	Files       map[string]FileMeta             `json:"files,omitempty"`
	Diagnostics []Diagnostic                    `json:"diagnostics,omitempty"`
}

// SnapshotProject records where a project was found.
type SnapshotProject struct {
	Key         ProjectKey `json:"key"`
	Name        string     `json:"name,omitempty"`
	DocsPath    string     `json:"docs_path"`
	BacklogPath string     `json:"backlog_path"`
	ConfigPath  string     `json:"config_path"`
}

// ACCount is the acceptance-criteria checkbox tally of an item body. Only the
// counts are stored, never the text (R-SNAP-1).
type ACCount struct {
	Total int `json:"total"`
	Done  int `json:"done"`
}

// SnapshotItem is one item as stored in a snapshot.
type SnapshotItem struct {
	ID        ItemID         `json:"id"`
	Type      ItemType       `json:"type"`
	Project   ProjectKey     `json:"project,omitempty"`
	Title     string         `json:"title"`
	Status    Status         `json:"status,omitempty"`
	Category  StatusCategory `json:"category,omitempty"`
	Priority  Priority       `json:"priority,omitempty"`
	Parent    ItemID         `json:"parent,omitempty"`
	Epic      ItemID         `json:"epic,omitempty"`
	Milestone ItemID         `json:"milestone,omitempty"`
	Sprint    string         `json:"sprint,omitempty"`
	Assignees []string       `json:"assignees,omitempty"`
	Author    string         `json:"author,omitempty"`
	Owner     string         `json:"owner,omitempty"`
	Labels    []string       `json:"labels,omitempty"`
	Estimate  *float64       `json:"estimate,omitempty"`
	Effort    *float64       `json:"effort,omitempty"`
	Spent     *float64       `json:"spent,omitempty"`
	Created   Timestamp      `json:"created,omitempty"`
	Updated   Timestamp      `json:"updated,omitempty"`
	Started   Timestamp      `json:"started,omitempty"`
	Closed    Timestamp      `json:"closed,omitempty"`
	Start     Date           `json:"start,omitempty"`
	Due       Date           `json:"due,omitempty"`
	Links     []Link         `json:"links,omitempty"`
	Refs      []Wikilink     `json:"refs,omitempty"`
	AC        *ACCount       `json:"ac,omitempty"`
	Comments  int            `json:"comments,omitempty"`
	Deleted   bool           `json:"deleted,omitempty"`
	Path      string         `json:"path"`
	Rev       Rev            `json:"rev"`
}

// SnapshotComment is one comment file as stored in a snapshot.
type SnapshotComment struct {
	Item      ItemID      `json:"item"`
	Author    string      `json:"author,omitempty"`
	Created   Timestamp   `json:"created,omitempty"`
	Updated   Timestamp   `json:"updated,omitempty"`
	InReplyTo string      `json:"in_reply_to,omitempty"`
	Kind      CommentKind `json:"kind,omitempty"`
	Path      string      `json:"path"`
	Rev       Rev         `json:"rev"`
}

// SnapshotPage is one knowledge-base page as stored in a snapshot.
type SnapshotPage struct {
	Path     string     `json:"path"`
	RelPath  string     `json:"rel_path"`
	Project  ProjectKey `json:"project,omitempty"`
	Title    string     `json:"title"`
	Tags     []string   `json:"tags,omitempty"`
	Headings []Heading  `json:"headings,omitempty"`
	Links    []Wikilink `json:"links,omitempty"`
	External []string   `json:"external,omitempty"`
	Updated  Timestamp  `json:"updated,omitempty"`
	Size     int64      `json:"size,omitempty"`
	Rev      Rev        `json:"rev"`
}

// Snapshot serializes the index. The result is deterministic: every list is
// sorted, every map is rendered with sorted keys by encoding/json, and no field
// depends on the order files happened to be read in.
func (ix *Index) Snapshot() Snapshot {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	snap := Snapshot{
		Schema:      SnapshotSchema,
		Generated:   ix.stats.BuiltAt,
		Generator:   SnapshotGenerator,
		Fingerprint: ix.fingerprint,
		Counts:      make(map[ItemType]int, len(ix.counts)),
		Files:       make(map[string]FileMeta, len(ix.files)),
		Diagnostics: append([]Diagnostic(nil), ix.diagnostics...),
	}
	for _, p := range ix.projects {
		snap.Projects = append(snap.Projects, SnapshotProject{
			Key:         p.Key,
			Name:        p.Name,
			DocsPath:    p.DocsPath,
			BacklogPath: p.BacklogPath,
			ConfigPath:  p.ConfigPath,
		})
	}
	for k, v := range ix.counts {
		snap.Counts[k] = v
	}
	if len(ix.maxIDs) > 0 {
		snap.MaxIDs = make(map[ProjectKey]map[ItemType]int, len(ix.maxIDs))
		for key, byType := range ix.maxIDs {
			m := make(map[ItemType]int, len(byType))
			for t, n := range byType {
				m[t] = n
			}
			snap.MaxIDs[key] = m
		}
	}
	for p, meta := range ix.files {
		snap.Files[p] = meta
	}

	for _, id := range sortedIDs(ix.byID) {
		it := ix.byID[id]
		entry := SnapshotItem{
			ID: it.ID, Type: it.Type, Project: ix.projectOf(it), Title: it.Title,
			Status: it.Status, Category: ix.categoryOf(it), Priority: it.Priority,
			Parent: it.Parent, Epic: it.Epic, Milestone: it.Milestone, Sprint: it.Sprint,
			Assignees: it.Assignees, Author: it.Author, Owner: it.Owner, Labels: it.Labels,
			Estimate: it.Estimate, Effort: it.Effort, Spent: it.Spent,
			Created: it.Created, Updated: it.Updated, Started: it.Started, Closed: it.Closed,
			Start: it.Start, Due: it.Due, Links: it.Links, Refs: ix.itemLinks[it.Path],
			Comments: len(ix.commentsByItem[it.ID]), Deleted: it.Deleted,
			Path: it.Path, Rev: it.Rev,
		}
		entry.AC = ix.itemAC[it.Path]
		snap.Items = append(snap.Items, entry)
	}
	for _, p := range sortedPaths(ix.commentsByPath) {
		c := ix.commentsByPath[p]
		snap.Comments = append(snap.Comments, SnapshotComment{
			Item: c.Item, Author: c.Author, Created: c.Created, Updated: c.Updated,
			InReplyTo: c.InReplyTo, Kind: c.Kind, Path: c.Path, Rev: c.Rev,
		})
	}
	for _, p := range sortedPaths(ix.pagesByPath) {
		page := ix.pagesByPath[p]
		snap.Pages = append(snap.Pages, SnapshotPage{
			Path: page.Path, RelPath: page.RelPath, Project: page.Project, Title: page.Title,
			Tags: page.Tags, Headings: page.Headings, Links: page.Links, External: page.External,
			Updated: page.Updated, Size: page.Size, Rev: page.Rev,
		})
	}
	return snap
}

// Load hydrates the index from a snapshot without touching the file system.
//
// Bodies are not part of a snapshot, so a hydrated index answers structural
// queries (filters, the graph, the tree) immediately while `text` filters and
// Search stay empty until the affected files are re-read. The caller pairs this
// with an incremental Build, which re-reads only the files whose size or
// modification time no longer match (R-IDX-3).
func (ix *Index) Load(snap Snapshot) error {
	if snap.Schema != SnapshotSchema {
		return fmt.Errorf("load snapshot: schema %d is not %d", snap.Schema, SnapshotSchema)
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()

	configs := make(map[ProjectKey]*ProjectConfig, len(ix.projects))
	for _, p := range ix.projects {
		configs[p.Key] = p.Config
	}
	ix.reset()
	ix.projects = nil
	for _, p := range snap.Projects {
		ix.projects = append(ix.projects, ProjectRef{
			Key: p.Key, Name: p.Name, DocsPath: p.DocsPath,
			BacklogPath: p.BacklogPath, ConfigPath: p.ConfigPath,
			Config: configs[p.Key],
		})
	}
	for p, meta := range snap.Files {
		ix.files[p] = meta
	}
	for i := range snap.Items {
		e := snap.Items[i]
		it := &Item{
			ID: e.ID, Type: e.Type, Title: e.Title, Status: e.Status, Priority: e.Priority,
			Parent: e.Parent, Epic: e.Epic, Milestone: e.Milestone, Sprint: e.Sprint,
			Assignees: e.Assignees, Author: e.Author, Owner: e.Owner, Labels: e.Labels,
			Estimate: e.Estimate, Effort: e.Effort, Spent: e.Spent,
			Created: e.Created, Updated: e.Updated, Started: e.Started, Closed: e.Closed,
			Start: e.Start, Due: e.Due, Links: e.Links, Deleted: e.Deleted,
			Path: e.Path, Rev: e.Rev,
		}
		ix.itemsByPath[it.Path] = it
		ix.itemLinks[it.Path] = e.Refs
		ix.itemAC[it.Path] = e.AC
		ix.itemCategory[it.Path] = e.Category
		ix.fileProject[it.Path] = e.Project
	}
	for i := range snap.Comments {
		e := snap.Comments[i]
		ix.commentsByPath[e.Path] = &Comment{
			Item: e.Item, Author: e.Author, Created: e.Created, Updated: e.Updated,
			InReplyTo: e.InReplyTo, Kind: e.Kind, Path: e.Path, Rev: e.Rev,
		}
	}
	for i := range snap.Pages {
		e := snap.Pages[i]
		ix.pagesByPath[e.Path] = &KBPage{
			Path: e.Path, RelPath: e.RelPath, Project: e.Project, Title: e.Title,
			Tags: e.Tags, Headings: e.Headings, Links: e.Links, External: e.External,
			Updated: e.Updated, Size: e.Size, Rev: e.Rev,
		}
		ix.fileProject[e.Path] = e.Project
	}
	// Only the findings a rebuild cannot recompute are restored; the derived ones
	// are produced again below, and restoring them too would double them.
	for _, d := range snap.Diagnostics {
		if isDerivedCode(d.Code) || d.Path == "" {
			continue
		}
		ix.fileDiags[d.Path] = append(ix.fileDiags[d.Path], d)
	}
	ix.stats.BuiltAt = snap.Generated
	ix.rebuild()
	return nil
}

// isDerivedCode reports the diagnostics a rebuild recomputes from the entries.
func isDerivedCode(c Code) bool {
	switch c {
	case CodeIDDuplicate, CodeWarnCounterStale,
		idxCodeRefDangling, idxCodeLinkBroken, idxCodeLinkAmbiguous, idxCodeCommentOrphan:
		return true
	default:
		return false
	}
}

// categoryOf maps an item's status to its coarse category. The caller holds the
// lock.
func (ix *Index) categoryOf(it *Item) StatusCategory {
	key := ix.projectOf(it)
	for _, p := range ix.projects {
		if p.Key == key && p.Config != nil {
			if c := p.Config.CategoryOf(it.Status); c != "" {
				return c
			}
		}
	}
	// Without a configuration — a snapshot hydrated before the project.yaml was
	// read — the category recorded in the snapshot is the best answer there is.
	return ix.itemCategory[it.Path]
}

// EncodeSnapshot renders a snapshot as deterministic JSON: two-space
// indentation, keys in the fixed order of the structs (and sorted for maps), no
// HTML escaping and a trailing newline (R-SNAP-2).
func EncodeSnapshot(s Snapshot) ([]byte, error) { return encodeJSON(s) }

// DecodeSnapshot parses a snapshot document.
func DecodeSnapshot(data []byte) (Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	return s, nil
}

// encodeJSON is the one JSON writer of this package, so that every artifact it
// produces is byte-stable.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode json: %w", err)
	}
	return buf.Bytes(), nil
}

// computeFingerprint hashes what the index was built from: the schema, the
// project keys and the (path, size, rev) triple of every indexed file. Two
// indexes with the same fingerprint hold the same data, which is what lets the
// browser trust an IndexedDB cache entry without re-parsing (docs/02 section 7.4).
//
// The caller holds the write lock.
func (ix *Index) computeFingerprint() {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "schema:%d\n", SnapshotSchema)
	for _, p := range ix.projects {
		_, _ = fmt.Fprintf(h, "project:%s@%s\n", p.Key, p.DocsPath)
	}
	for _, p := range sortedPaths(ix.files) {
		meta := ix.files[p]
		_, _ = fmt.Fprintf(h, "file:%s:%d:%s\n", p, meta.Size, meta.Rev)
	}
	sum := h.Sum(nil)
	ix.fingerprint = revPrefix + hex.EncodeToString(sum)[:revHexLen]
}

// checklistCounts tallies the GFM task list of a body: how many checkboxes it
// has and how many are ticked. It is what feeds the "ac" field of a snapshot.
func checklistCounts(body string) (total, done int) {
	var fence string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		rest, ok := trimListMarker(trimmed)
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(rest, "[ ] "), rest == "[ ]":
			total++
		case strings.HasPrefix(rest, "[x] "), strings.HasPrefix(rest, "[X] "), rest == "[x]", rest == "[X]":
			total++
			done++
		}
	}
	return total, done
}

// trimListMarker removes a bullet or ordered list marker from a trimmed line.
func trimListMarker(line string) (string, bool) {
	for _, marker := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(line, marker) {
			return strings.TrimSpace(line[len(marker):]), true
		}
	}
	return "", false
}

// SnapshotSource records the git state a project snapshot was built from
// (R-SNAP-4).
type SnapshotSource struct {
	Commit string `json:"commit,omitempty"`
	Branch string `json:"branch,omitempty"`
	Dirty  bool   `json:"dirty"`
}

// ProjectSnapshotOptions tunes the committed per-project snapshot.
type ProjectSnapshotOptions struct {
	// GeneratedBy is the handle of whoever produced the file.
	GeneratedBy string
	// Repo and DefaultBranch describe where the project lives, from team.yaml.
	Repo          string
	DefaultBranch string
	// Source is the git state the snapshot was built from.
	Source *SnapshotSource
	// IncludeClosed keeps items in a terminal status regardless of their age.
	// When false (the default), a terminal item not updated within MaxAge is
	// omitted to bound the file size (R-SNAP-3).
	IncludeClosed bool
	// MaxAge defaults to 30 days when zero.
	MaxAge time.Duration
	// Now is the instant the age is measured from; it defaults to the index clock.
	Now time.Time
}

// ProjectSnapshot is the reduced, committed form of docs/04 section 6.2: what a
// team board renders for a project nobody has cloned.
type ProjectSnapshot struct {
	Schema      int                   `json:"schema"`
	Project     ProjectSnapshotMeta   `json:"project"`
	Generated   Timestamp             `json:"generated"`
	GeneratedBy string                `json:"generated_by,omitempty"`
	Generator   string                `json:"generator"`
	Source      *SnapshotSource       `json:"source,omitempty"`
	Workflow    []SnapshotStatus      `json:"workflow,omitempty"`
	Labels      []SnapshotLabel       `json:"labels,omitempty"`
	Counts      map[ItemType]int      `json:"counts"`
	Items       []ProjectSnapshotItem `json:"items"`
}

// ProjectSnapshotMeta identifies the project a snapshot describes.
type ProjectSnapshotMeta struct {
	Key           ProjectKey `json:"key"`
	Name          string     `json:"name,omitempty"`
	Repo          string     `json:"repo,omitempty"`
	DefaultBranch string     `json:"default_branch,omitempty"`
	DocsPath      string     `json:"docs_path"`
}

// SnapshotStatus is one status of the workflow, as published.
type SnapshotStatus struct {
	ID       Status         `json:"id"`
	Name     string         `json:"name,omitempty"`
	Category StatusCategory `json:"category"`
	Terminal bool           `json:"terminal,omitempty"`
}

// SnapshotLabel is one label of the catalog, as published.
type SnapshotLabel struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// ProjectSnapshotItem is one card of a committed snapshot: front-matter-derived
// fields only (R-SNAP-1).
type ProjectSnapshotItem struct {
	ID        ItemID         `json:"id"`
	Type      ItemType       `json:"type"`
	Title     string         `json:"title"`
	Status    Status         `json:"status,omitempty"`
	Category  StatusCategory `json:"category,omitempty"`
	Priority  Priority       `json:"priority,omitempty"`
	Parent    ItemID         `json:"parent,omitempty"`
	Milestone ItemID         `json:"milestone,omitempty"`
	Sprint    string         `json:"sprint,omitempty"`
	Assignees []string       `json:"assignees,omitempty"`
	Labels    []string       `json:"labels,omitempty"`
	Estimate  *float64       `json:"estimate,omitempty"`
	Due       Date           `json:"due,omitempty"`
	Updated   Timestamp      `json:"updated,omitempty"`
	Path      string         `json:"path"`
	Rev       Rev            `json:"rev"`
	AC        *ACCount       `json:"ac,omitempty"`
}

// defaultSnapshotMaxAge is how long a closed item stays in a committed snapshot.
const defaultSnapshotMaxAge = 30 * 24 * time.Hour

// ProjectSnapshot builds the committed snapshot of one project. Items are
// sorted by id so that regeneration produces a minimal diff (R-SNAP-2).
func (ix *Index) ProjectSnapshot(key ProjectKey, opts ProjectSnapshotOptions) (ProjectSnapshot, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	var ref ProjectRef
	found := false
	for _, p := range ix.projects {
		if p.Key == key {
			ref, found = p, true
			break
		}
	}
	if !found {
		return ProjectSnapshot{}, fmt.Errorf("project %q: %w", key, ErrItemNotFound)
	}
	now := opts.Now
	if now.IsZero() {
		now = ix.now()
	}
	maxAge := opts.MaxAge
	if maxAge <= 0 {
		maxAge = defaultSnapshotMaxAge
	}

	out := ProjectSnapshot{
		Schema: SnapshotSchema,
		Project: ProjectSnapshotMeta{
			Key: ref.Key, Name: ref.Name, Repo: opts.Repo,
			DefaultBranch: opts.DefaultBranch, DocsPath: ref.DocsPath,
		},
		Generated:   ix.stats.BuiltAt,
		GeneratedBy: opts.GeneratedBy,
		Generator:   SnapshotGenerator,
		Source:      opts.Source,
		Counts:      make(map[ItemType]int),
	}
	if ref.Config != nil {
		for _, s := range ref.Config.Workflow.Statuses {
			out.Workflow = append(out.Workflow, SnapshotStatus{
				ID: s.ID, Name: s.Name, Category: s.Category, Terminal: s.Terminal,
			})
		}
		for _, l := range ref.Config.Labels {
			out.Labels = append(out.Labels, SnapshotLabel{Name: l.Name, Color: l.Color})
		}
	}

	for _, id := range sortedIDs(ix.byID) {
		it := ix.byID[id]
		if ix.projectOf(it) != key || it.Deleted {
			continue
		}
		category := ix.categoryOf(it)
		terminal := category == CategoryDone || category == CategoryCancelled
		if !opts.IncludeClosed && terminal {
			if it.Updated.IsZero() || now.Sub(it.Updated.Time) > maxAge {
				continue
			}
		}
		entry := ProjectSnapshotItem{
			ID: it.ID, Type: it.Type, Title: it.Title, Status: it.Status, Category: category,
			Priority: it.Priority, Parent: it.Parent, Milestone: it.Milestone, Sprint: it.Sprint,
			Assignees: it.Assignees, Labels: it.Labels, Estimate: it.Estimate,
			Due: it.Due, Updated: it.Updated, Path: it.Path, Rev: it.Rev,
		}
		if entry.Parent == "" {
			entry.Parent = it.Epic
		}
		entry.AC = ix.itemAC[it.Path]
		out.Counts[it.Type]++
		out.Items = append(out.Items, entry)
	}
	return out, nil
}

// EncodeProjectSnapshot renders a committed snapshot with the formatting
// R-SNAP-2 requires.
func EncodeProjectSnapshot(s ProjectSnapshot) ([]byte, error) { return encodeJSON(s) }

// ProjectSnapshotPath is where a committed snapshot lives in the team
// repository: .pmngr/index/<KEY>.json.
func ProjectSnapshotPath(teamBacklogPath string, key ProjectKey) string {
	return path.Join(teamBacklogPath, indexDirName, string(key)+".json")
}

// decodeJSON is the one JSON reader of this package. It rejects trailing
// content so that a truncated or concatenated document is an error rather than
// a half-read snapshot.
func decodeJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	if dec.More() {
		return errors.New("decode json: trailing content after the document")
	}
	return nil
}
