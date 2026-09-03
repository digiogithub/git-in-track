package core

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Clock is the injectable source of the current time. The core never calls
// time.Now directly so that the same code is deterministic in tests and usable
// under GOOS=js, where the host clock is the browser's.
type Clock interface {
	Now() time.Time
}

// ClockFunc adapts a function to the Clock interface.
type ClockFunc func() time.Time

// Now returns the current time.
func (f ClockFunc) Now() time.Time { return f() }

// SystemClock is the default clock: the host wall clock, in UTC.
var SystemClock Clock = ClockFunc(time.Now)

// The folders of a backlog. They are created lazily: a missing folder is an
// empty one, never an error (R-LOC-3).
const (
	EpicsDirName       = "epics"
	StoriesDirName     = "stories"
	TasksDirName       = "tasks"
	MilestonesDirName  = "milestones"
	CommentsDirName    = "comments"
	AttachmentsDirName = "attachments"
)

// Sentinel errors of ID allocation.
var (
	// ErrIDRangeExhausted reports that every number of the ranges assigned to a
	// user under id_allocation.strategy: ranges is already taken.
	ErrIDRangeExhausted = errors.New("id range exhausted")

	// ErrNoTypeCode reports an item type that has no place in the id grammar,
	// such as a comment (R-ID-4).
	ErrNoTypeCode = errors.New("item type has no id type code")
)

// ItemDirName returns the backlog folder an item type lives in.
func ItemDirName(t ItemType) (string, bool) {
	switch t {
	case TypeEpic:
		return EpicsDirName, true
	case TypeStory:
		return StoriesDirName, true
	case TypeTask:
		return TasksDirName, true
	case TypeMilestone:
		return MilestonesDirName, true
	case TypeComment:
		return CommentsDirName, true
	default:
		return "", false
	}
}

// itemDirNames lists the four item folders in a stable order, so that two runs
// of a scan visit the tree identically.
func itemDirNames() []string {
	return []string{EpicsDirName, MilestonesDirName, StoriesDirName, TasksDirName}
}

// BacklogDir normalises the folder a caller points at: it accepts either the
// .pmngr folder itself or the documentation folder that contains it, and always
// returns the .pmngr folder.
func BacklogDir(projectDir string) string {
	p := path.Clean(projectDir)
	if path.Base(p) == BacklogDirName {
		return p
	}
	return path.Join(p, BacklogDirName)
}

// IDAllocator hands out the next free id of a project, collision-safe under
// concurrent writers.
//
// It is the per-project form of the interface in docs/07 section 6.5: the
// project key is bound at construction time (a vault mounts one project per
// allocator), so it is not repeated on every call.
type IDAllocator interface {
	// Next reserves the next id for a type, e.g. ACME-US-0043.
	Next(ctx context.Context, t ItemType) (ItemID, error)
	// Peek returns the next id without reserving it.
	Peek(ctx context.Context, t ItemType) (ItemID, error)
	// Reconcile rescans the tree and repairs the counters after a git merge
	// brought in ids allocated by someone else.
	Reconcile(ctx context.Context) (Reconciliation, error)
}

// Allocator implements IDAllocator over an FS, following the algorithm of
// docs/03 section 4.1: the counters in project.yaml are a hint, the scan of the
// backlog always wins, and reserved ranges participate in the maximum.
//
// It is safe for concurrent use: every allocation takes a mutex, and the number
// handed out is remembered in-process so that two goroutines never receive the
// same id even when neither has written its file yet.
type Allocator struct {
	mu       sync.Mutex
	fs       FS
	backlog  string
	cfg      *ProjectConfig
	user     string
	reserved map[TypeCode]int
}

// Ensure Allocator satisfies the interface at compile time.
var _ IDAllocator = (*Allocator)(nil)

// NewAllocator returns an allocator for the project rooted at projectDir, which
// may be either the .pmngr folder or the documentation folder containing it.
// cfg supplies the project key, the counter hints and the reserved ranges; it
// must not be nil.
func NewAllocator(fs FS, projectDir string, cfg *ProjectConfig) *Allocator {
	return &Allocator{
		fs:       fs,
		backlog:  BacklogDir(projectDir),
		cfg:      cfg,
		reserved: make(map[TypeCode]int),
	}
}

// SetUser selects the person allocation runs as. It only matters under
// id_allocation.strategy: ranges, where each handle owns a block of numbers.
func (a *Allocator) SetUser(user string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.user = user
}

// Next reserves and returns the next free id of a type.
func (a *Allocator) Next(ctx context.Context, t ItemType) (ItemID, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.allocate(ctx, t, true)
}

// Peek returns the id Next would hand out, without reserving it.
func (a *Allocator) Peek(ctx context.Context, t ItemType) (ItemID, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.allocate(ctx, t, false)
}

// reserveNext is the internal, already-locked variant used by the renumber
// planner, which needs successive free numbers without touching project.yaml.
func (a *Allocator) reserveNext(ctx context.Context, t ItemType) (ItemID, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	code, ok := TypeCodeFor(t)
	if !ok {
		return "", fmt.Errorf("allocate %s: %w", t, ErrNoTypeCode)
	}
	n, err := a.nextNumber(ctx, t, code)
	if err != nil {
		return "", err
	}
	a.reserved[code] = n
	return FormatItemID(a.cfg.Key, code, n), nil
}

// allocate computes the next id and, when reserve is set, remembers it and
// optionally writes the counter hint back to project.yaml.
func (a *Allocator) allocate(ctx context.Context, t ItemType, reserve bool) (ItemID, error) {
	code, ok := TypeCodeFor(t)
	if !ok {
		return "", fmt.Errorf("allocate %s: %w", t, ErrNoTypeCode)
	}
	if !ValidProjectKey(a.cfg.Key) {
		return "", fmt.Errorf("allocate %s: project key %q does not match [A-Z][A-Z0-9]{1,9}", t, a.cfg.Key)
	}
	n, err := a.nextNumber(ctx, t, code)
	if err != nil {
		return "", err
	}
	if !reserve {
		return FormatItemID(a.cfg.Key, code, n), nil
	}
	a.reserved[code] = n
	if a.cfg.IDAllocation.WriteCounters {
		if err := a.writeCounter(t, n); err != nil {
			return "", err
		}
	}
	if a.cfg.IDAllocation.Counters == nil {
		a.cfg.IDAllocation.Counters = make(map[ItemType]int)
	}
	a.cfg.IDAllocation.Counters[t] = n
	return FormatItemID(a.cfg.Key, code, n), nil
}

// nextNumber implements steps 2 to 4 of docs/03 section 4.1, plus the reserved
// ranges of section 4.5 and the per-user ranges of the ranges strategy.
func (a *Allocator) nextNumber(ctx context.Context, t ItemType, code TypeCode) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, wrapContext("allocate", err)
	}
	items, err := scanItems(a.fs, a.backlog)
	if err != nil {
		return 0, fmt.Errorf("allocate %s: %w", t, err)
	}
	numbers := numbersByCode(items, a.cfg.Key)
	numbers[code] = append(numbers[code], redirectNumbers(a.cfg, code)...)
	if r, ok := a.reserved[code]; ok {
		numbers[code] = append(numbers[code], r)
	}

	if ranges := a.userRanges(t); len(ranges) > 0 {
		return nextInRanges(numbers[code], ranges)
	}

	maxSeen := maxInt(numbers[code])
	if hint := a.cfg.IDAllocation.Counters[t]; hint > maxSeen {
		maxSeen = hint
	}
	// Reserved ranges participate in max_seen so that a client that never sees
	// the reserving user's files still allocates above the block (docs/03 4.5).
	blocks := a.blockedRanges(t)
	for _, r := range blocks {
		if r.To > maxSeen {
			maxSeen = r.To
		}
	}
	n := maxSeen + 1
	// Belt and braces: a badly ordered or overlapping block still cannot swallow
	// the number we are about to hand out.
	for {
		r, inside := insideRange(n, blocks)
		if !inside {
			return n, nil
		}
		n = r.To + 1
	}
}

// userRanges returns the ranges the current user allocates from, or nil when the
// project does not use the ranges strategy or the user has no block.
func (a *Allocator) userRanges(t ItemType) []IDRange {
	if !strings.EqualFold(a.cfg.IDAllocation.Strategy, "ranges") || a.user == "" {
		return nil
	}
	byType, ok := a.cfg.IDAllocation.Ranges[a.user]
	if !ok {
		return nil
	}
	ranges := append([]IDRange(nil), byType[t]...)
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].From < ranges[j].From })
	return ranges
}

// blockedRanges returns the ranges the current user must not allocate from: the
// reserved blocks of section 4.5 plus, under the ranges strategy, every other
// user's block.
func (a *Allocator) blockedRanges(t ItemType) []IDRange {
	blocks := append([]IDRange(nil), a.cfg.IDAllocation.Reserved[t]...)
	for handle, byType := range a.cfg.IDAllocation.Ranges {
		if handle == a.user {
			continue
		}
		blocks = append(blocks, byType[t]...)
	}
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].From != blocks[j].From {
			return blocks[i].From < blocks[j].From
		}
		return blocks[i].To < blocks[j].To
	})
	return blocks
}

// nextInRanges returns the first free number inside the ordered ranges.
func nextInRanges(taken []int, ranges []IDRange) (int, error) {
	for _, r := range ranges {
		n := r.From
		for _, v := range taken {
			if v >= r.From && v <= r.To && v >= n {
				n = v + 1
			}
		}
		if n <= r.To {
			return n, nil
		}
	}
	return 0, ErrIDRangeExhausted
}

// insideRange reports whether n falls inside one of the ranges.
func insideRange(n int, ranges []IDRange) (IDRange, bool) {
	for _, r := range ranges {
		if n >= r.From && n <= r.To {
			return r, true
		}
	}
	return IDRange{}, false
}

// redirectNumbers returns the numbers of both sides of every redirect of a type,
// so that a renumbered id is never handed out again (R-ID-3).
func redirectNumbers(cfg *ProjectConfig, code TypeCode) []int {
	var out []int
	for from, to := range cfg.IDAllocation.Redirects {
		for _, id := range []ItemID{from, to} {
			key, c, n, err := ParseItemID(string(id))
			if err != nil || c != code || key != cfg.Key {
				continue
			}
			out = append(out, n)
		}
	}
	return out
}

// maxInt returns the largest value of a slice, or zero when it is empty.
func maxInt(values []int) int {
	m := 0
	for _, v := range values {
		if v > m {
			m = v
		}
	}
	return m
}

// Reconciliation is the outcome of a scan that repairs the counter hints after a
// merge brought in ids allocated by someone else.
type Reconciliation struct {
	// Scanned is the highest number found per type.
	Scanned map[ItemType]int `json:"scanned"`
	// Counters is the value of every counter after reconciliation.
	Counters map[ItemType]int `json:"counters"`
	// Stale lists the types whose counter was below the scanned maximum
	// (W-PROJ-COUNTER-STALE).
	Stale []ItemType `json:"stale,omitempty"`
	// Duplicates lists every id claimed by more than one file.
	Duplicates []Duplicate `json:"duplicates,omitempty"`
	// Written reports whether project.yaml was rewritten.
	Written bool `json:"written"`
}

// Reconcile rescans the backlog, reports duplicate ids and repairs the counter
// hints in project.yaml when id_allocation.write_counters is enabled. It is what
// gintrack doctor calls before renumbering.
func (a *Allocator) Reconcile(ctx context.Context) (Reconciliation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Reconciliation{}, wrapContext("reconcile", err)
	}
	items, err := scanItems(a.fs, a.backlog)
	if err != nil {
		return Reconciliation{}, fmt.Errorf("reconcile: %w", err)
	}
	rec := Reconciliation{
		Scanned:    make(map[ItemType]int),
		Counters:   make(map[ItemType]int),
		Duplicates: duplicatesOf(items),
	}
	numbers := numbersByCode(items, a.cfg.Key)
	for _, t := range []ItemType{TypeEpic, TypeStory, TypeTask, TypeMilestone} {
		code, ok := TypeCodeFor(t)
		if !ok {
			continue
		}
		scanned := maxInt(append(numbers[code], redirectNumbers(a.cfg, code)...))
		rec.Scanned[t] = scanned
		hint := a.cfg.IDAllocation.Counters[t]
		if hint < scanned {
			rec.Stale = append(rec.Stale, t)
		}
		counter := scanned
		if hint > counter {
			counter = hint
		}
		rec.Counters[t] = counter
	}
	sort.Slice(rec.Stale, func(i, j int) bool { return rec.Stale[i] < rec.Stale[j] })

	if a.cfg.IDAllocation.WriteCounters {
		for _, t := range rec.Stale {
			if err := a.writeCounter(t, rec.Counters[t]); err != nil {
				return rec, err
			}
			rec.Written = true
		}
	}
	if a.cfg.IDAllocation.Counters == nil {
		a.cfg.IDAllocation.Counters = make(map[ItemType]int)
	}
	for t, n := range rec.Counters {
		if n > a.cfg.IDAllocation.Counters[t] {
			a.cfg.IDAllocation.Counters[t] = n
		}
	}
	return rec, nil
}

// writeCounter rewrites id_allocation.counters.<type> in project.yaml, editing
// the YAML node tree so that comments, key order and formatting survive.
func (a *Allocator) writeCounter(t ItemType, n int) error {
	p := path.Join(a.backlog, ProjectFileName)
	data, err := a.fs.ReadFile(p)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			// No project.yaml to update: the scan is authoritative anyway.
			return nil
		}
		return fmt.Errorf("write counter %s: %w", t, err)
	}
	out, err := setYAMLPath(data, []string{"id_allocation", "counters", string(t)}, strconv.Itoa(n))
	if err != nil {
		return fmt.Errorf("write counter %s: %w", t, err)
	}
	if out == nil {
		return nil
	}
	if err := writeFileAtomic(a.fs, p, out); err != nil {
		return fmt.Errorf("write counter %s: %w", t, err)
	}
	return nil
}

// scannedItem is the cheap projection of an item file: enough to allocate ids,
// detect duplicates and decide which of two files keeps a colliding id.
type scannedItem struct {
	Path    string
	ID      ItemID
	Type    ItemType
	Title   string
	Created Timestamp
	Code    TypeCode
	Key     ProjectKey
	Number  int
}

// scanItems lists every item file of a backlog and reads the few front-matter
// fields allocation and collision repair need. Files that do not parse are still
// reported when their name carries a well-formed id, because a broken file that
// claims an id still takes that id.
//
// Nested folders are ignored (R-LOC-4), as are non-Markdown files (R-LOC-6) and
// the temporary files of an interrupted atomic write.
func scanItems(fsys FS, backlog string) ([]scannedItem, error) {
	var out []scannedItem
	for _, dir := range itemDirNames() {
		full := path.Join(backlog, dir)
		entries, err := fsys.ReadDir(full)
		if err != nil {
			if errors.Is(err, ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("scan %s: %w", full, err)
		}
		for _, e := range entries {
			if e.IsDir || !isItemFileName(e.Name) {
				continue
			}
			it, err := scanItemFile(fsys, path.Join(full, e.Name))
			if err != nil {
				return nil, err
			}
			if it.ID == "" {
				continue
			}
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// isItemFileName reports whether a directory entry is an item file.
func isItemFileName(name string) bool {
	return strings.HasSuffix(name, ".md") && !strings.HasPrefix(name, ".")
}

// scanItemFile reads one item file and extracts its identity. A file whose front
// matter is broken degrades to the id carried by its name.
func scanItemFile(fsys FS, p string) (scannedItem, error) {
	data, err := fsys.ReadFile(p)
	if err != nil {
		return scannedItem{}, fmt.Errorf("scan %s: %w", p, err)
	}
	it := scannedItem{Path: p}
	if fm, _, err := ParseDocument(data); err == nil {
		it.ID = ItemID(stringOf(fm["id"]))
		it.Type = ItemType(stringOf(fm["type"]))
		it.Title = stringOf(fm["title"])
		if s := stringOf(fm["created"]); s != "" {
			if ts, err := ParseTimestamp(s); err == nil {
				it.Created = ts
			}
		}
	}
	if it.ID == "" {
		it.ID = IDFromFileName(p)
	}
	if it.ID != "" {
		key, code, n, err := ParseItemID(string(it.ID))
		if err == nil {
			it.Key, it.Code, it.Number = key, code, n
			if it.Type == "" {
				if t, ok := ItemTypeFor(code); ok {
					it.Type = t
				}
			}
		}
	}
	return it, nil
}

// numbersByCode groups the numeric part of every scanned id by type code,
// keeping only the ids of the given project key.
func numbersByCode(items []scannedItem, key ProjectKey) map[TypeCode][]int {
	out := make(map[TypeCode][]int, 4)
	for _, it := range items {
		if it.Code == "" || (key != "" && it.Key != key) {
			continue
		}
		out[it.Code] = append(out[it.Code], it.Number)
	}
	return out
}

// setYAMLPath sets a scalar at a nested key path of a YAML document, creating
// the intermediate mappings when they are absent, and re-encodes the document.
// Editing the node tree rather than a decoded struct is what keeps the comments,
// the key order and the hand-written formatting of project.yaml intact.
//
// It returns nil when the document already holds that value.
func setYAMLPath(data []byte, keys []string, value string) ([]byte, error) {
	if len(keys) == 0 {
		return nil, errors.New("set yaml path: empty path")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ProjectFileName, err)
	}
	root := documentMapping(&doc)
	if root == nil {
		return nil, fmt.Errorf("parse %s: not a mapping", ProjectFileName)
	}
	parent := root
	for _, k := range keys[:len(keys)-1] {
		next, err := yamlEnsureMapping(parent, k)
		if err != nil {
			return nil, err
		}
		parent = next
	}
	leaf := keys[len(keys)-1]
	if existing, ok := yamlMapGet(parent, leaf); ok {
		if existing.Kind == yaml.ScalarNode && existing.Value == value {
			return nil, nil
		}
		existing.Kind = yaml.ScalarNode
		existing.Tag = ""
		existing.Style = 0
		existing.Value = value
		existing.Content = nil
	} else {
		yamlMapSet(parent, leaf, yamlScalar(value))
	}
	return encodeYAMLNode(&doc)
}

// documentMapping returns the mapping node of a parsed YAML document, creating
// one when the document is empty.
func documentMapping(doc *yaml.Node) *yaml.Node {
	if doc.Kind == 0 {
		// Empty file: build a document holding an empty mapping.
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		return doc.Content[0]
	}
	node := doc
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			node.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		}
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

// yamlMapGet returns the value node of a key of a mapping node.
func yamlMapGet(m *yaml.Node, key string) (*yaml.Node, bool) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1], true
		}
	}
	return nil, false
}

// yamlMapSet appends a key to a mapping node, or replaces its value.
func yamlMapSet(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content, yamlScalar(key), value)
}

// yamlEnsureMapping returns the mapping stored under a key, creating an empty
// one when the key is absent or holds null.
func yamlEnsureMapping(m *yaml.Node, key string) (*yaml.Node, error) {
	if v, ok := yamlMapGet(m, key); ok {
		if v.Kind == yaml.MappingNode {
			return v, nil
		}
		if v.Tag == "!!null" || (v.Kind == yaml.ScalarNode && v.Value == "") {
			v.Kind = yaml.MappingNode
			v.Tag = "!!map"
			v.Value = ""
			v.Content = nil
			return v, nil
		}
		return nil, fmt.Errorf("set yaml path: %q is not a mapping", key)
	}
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	m.Content = append(m.Content, yamlScalar(key), node)
	return node, nil
}

// yamlScalar builds a plain scalar node.
func yamlScalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: value}
}

// encodeYAMLNode renders a node tree with the two-space indentation the project
// files use, and a single trailing newline.
func encodeYAMLNode(n *yaml.Node) ([]byte, error) {
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	return Canonicalize([]byte(b.String())), nil
}
