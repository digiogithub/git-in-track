package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultLimit and MaxLimit bound a page of results (docs/08 section 4.6).
const (
	DefaultLimit = 20
	MaxLimit     = 200
)

// DefaultFields is the projection item_list applies when the caller asks for
// none.
var DefaultFields = []string{"id", "type", "title", "status", "priority", "assignees", "updated"}

// Unassigned is the assignee filter value that matches items with no assignee.
const Unassigned = "none"

// MeToken is the assignee filter value that stands for the configured user.
const MeToken = "@me"

// Filter selects items. Filters are AND across fields and OR within a repeated
// field, which is the semantic of the MCP item_list tool and of GET /api/v1/items.
type Filter struct {
	Projects   []ProjectKey `json:"project,omitempty"`
	Types      []ItemType   `json:"type,omitempty"`
	Statuses   []Status     `json:"status,omitempty"`
	Priorities []Priority   `json:"priority,omitempty"`
	// Assignees matches any of the handles; "none" matches an unassigned item
	// and "@me" is replaced by Me.
	Assignees []string `json:"assignee,omitempty"`
	Me        string   `json:"me,omitempty"`
	Labels    []string `json:"label,omitempty"`
	Parent    ItemID   `json:"parent,omitempty"`
	Milestone ItemID   `json:"milestone,omitempty"`
	// Text is a case-insensitive substring matched against title, labels and body.
	Text string `json:"text,omitempty"`
	// UpdatedSince keeps items updated at or after this instant.
	UpdatedSince Timestamp `json:"updated_since,omitempty"`
	// IncludeDeleted keeps items whose front matter says deleted: true. They are
	// excluded by default.
	IncludeDeleted bool `json:"include_deleted,omitempty"`

	// Sort is a comma-separated list of keys, each optionally prefixed with "-"
	// for descending order. Supported keys: updated, created, priority, id,
	// title, status. The default is "-updated".
	Sort string `json:"sort,omitempty"`
	// Limit bounds the page; 0 means DefaultLimit and anything above MaxLimit is
	// clamped.
	Limit int `json:"limit,omitempty"`
	// Cursor continues a previous page. It is opaque and must be echoed verbatim.
	Cursor string `json:"cursor,omitempty"`
	// Fields is the projection ProjectFields applies. It does not affect matching.
	Fields []string `json:"fields,omitempty"`
}

// Page is one page of results plus what a caller needs to ask for the next one.
type Page[T any] struct {
	Items      []T    `json:"items"`
	Total      int    `json:"total"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	NextCursor string `json:"nextCursor,omitempty"`
	Truncated  bool   `json:"truncated"`
}

// cursor is the decoded form of the opaque pagination token: the sort it was
// produced for, the offset it stands at and the id of the last item returned.
// The id is what makes paging stable when an item is inserted mid-scroll; the
// offset is the fallback when that item is gone.
type cursor struct {
	Sort   string `json:"s"`
	Offset int    `json:"o"`
	LastID ItemID `json:"i,omitempty"`
}

func encodeCursor(c cursor) string {
	data, err := json.Marshal(c)
	if err != nil { // unreachable: the struct is plain data
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(s string) (cursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}, fmt.Errorf("decode cursor: %w", err)
	}
	var c cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return cursor{}, fmt.Errorf("decode cursor: %w", err)
	}
	return c, nil
}

// Items returns the items matching a filter, sorted and paginated.
//
// Ordering is total: the requested keys first, the id last, so that two calls
// with the same filter over the same index return the same order and a cursor
// means the same thing on both.
func (ix *Index) Items(ctx context.Context, f Filter) (Page[Item], error) {
	if err := checkCancelled(ctx); err != nil {
		return Page[Item]{}, err
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	keys, sortSpec, err := parseSort(f.Sort)
	if err != nil {
		return Page[Item]{}, err
	}
	matched := make([]*Item, 0, len(ix.byID))
	for _, id := range sortedIDs(ix.byID) {
		it := ix.byID[id]
		if ix.matches(it, f) {
			matched = append(matched, it)
		}
	}
	sortItems(matched, keys)

	limit := f.Limit
	switch {
	case limit <= 0:
		limit = DefaultLimit
	case limit > MaxLimit:
		limit = MaxLimit
	}

	offset := 0
	if f.Cursor != "" {
		c, err := decodeCursor(f.Cursor)
		if err != nil {
			return Page[Item]{}, err
		}
		if c.Sort != sortSpec {
			return Page[Item]{}, fmt.Errorf("cursor was issued for sort %q, not %q", c.Sort, sortSpec)
		}
		offset = c.Offset
		if c.LastID != "" {
			for i, it := range matched {
				if it.ID == c.LastID {
					offset = i + 1
					break
				}
			}
		}
	}
	if offset > len(matched) {
		offset = len(matched)
	}

	end := offset + limit
	if end > len(matched) {
		end = len(matched)
	}
	page := Page[Item]{
		Items:     make([]Item, 0, end-offset),
		Total:     len(matched),
		Limit:     limit,
		Offset:    offset,
		Truncated: end < len(matched),
	}
	for _, it := range matched[offset:end] {
		page.Items = append(page.Items, cloneItem(it))
	}
	if page.Truncated {
		page.NextCursor = encodeCursor(cursor{Sort: sortSpec, Offset: end, LastID: matched[end-1].ID})
	}
	return page, nil
}

// matches applies every filter clause to one item. The caller holds the lock.
func (ix *Index) matches(it *Item, f Filter) bool {
	if it.Deleted && !f.IncludeDeleted {
		return false
	}
	if len(f.Projects) > 0 && !containsKey(f.Projects, ix.projectOf(it)) {
		return false
	}
	if len(f.Types) > 0 && !containsType(f.Types, it.Type) {
		return false
	}
	if len(f.Statuses) > 0 && !containsStatus(f.Statuses, it.Status) {
		return false
	}
	if len(f.Priorities) > 0 && !containsPriority(f.Priorities, it.Priority) {
		return false
	}
	if len(f.Assignees) > 0 && !matchAssignees(it.Assignees, f) {
		return false
	}
	for _, want := range f.Labels {
		if !containsFold(it.Labels, want) {
			return false
		}
	}
	if f.Parent != "" && it.Parent != f.Parent && it.Epic != f.Parent {
		return false
	}
	if f.Milestone != "" && it.Milestone != f.Milestone {
		return false
	}
	if !f.UpdatedSince.IsZero() {
		if it.Updated.IsZero() || it.Updated.Before(f.UpdatedSince.Time) {
			return false
		}
	}
	if f.Text != "" && !matchesText(it, f.Text) {
		return false
	}
	return true
}

// matchesText is the case-insensitive substring search of item_list's `text`
// parameter, over the title, the labels and the body.
func matchesText(it *Item, text string) bool {
	needle := strings.ToLower(strings.TrimSpace(text))
	if needle == "" {
		return true
	}
	if strings.Contains(strings.ToLower(it.Title), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(string(it.ID)), needle) {
		return true
	}
	for _, l := range it.Labels {
		if strings.Contains(strings.ToLower(l), needle) {
			return true
		}
	}
	return strings.Contains(strings.ToLower(it.Body), needle)
}

// matchAssignees implements the "none" and "@me" tokens of item_list.
func matchAssignees(assignees []string, f Filter) bool {
	for _, want := range f.Assignees {
		switch want {
		case Unassigned:
			if len(assignees) == 0 {
				return true
			}
		case MeToken:
			if f.Me != "" && containsFold(assignees, f.Me) {
				return true
			}
		default:
			if containsFold(assignees, want) {
				return true
			}
		}
	}
	return false
}

// sortKey is one parsed component of a sort specification.
type sortKey struct {
	field string
	desc  bool
}

// parseSort decodes a sort specification and returns it with its normalized
// spelling, which is what a cursor is bound to.
func parseSort(spec string) ([]sortKey, string, error) {
	if strings.TrimSpace(spec) == "" {
		spec = "-updated"
	}
	var keys []sortKey
	var parts []string
	for _, raw := range strings.Split(spec, ",") {
		field := strings.TrimSpace(raw)
		if field == "" {
			continue
		}
		desc := false
		switch field[0] {
		case '-':
			desc, field = true, field[1:]
		case '+':
			field = field[1:]
		}
		switch field {
		case "updated", "created", "priority", "id", "title", "status", "due":
		default:
			return nil, "", fmt.Errorf("unknown sort key %q", field)
		}
		keys = append(keys, sortKey{field: field, desc: desc})
		if desc {
			parts = append(parts, "-"+field)
			continue
		}
		parts = append(parts, field)
	}
	if len(keys) == 0 {
		keys = []sortKey{{field: "updated", desc: true}}
		parts = []string{"-updated"}
	}
	return keys, strings.Join(parts, ","), nil
}

// sortItems applies the sort keys, with the id as the final tiebreaker so that
// the order is total and therefore reproducible.
func sortItems(items []*Item, keys []sortKey) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		for _, k := range keys {
			cmp := compareItems(a, b, k.field)
			if cmp == 0 {
				continue
			}
			if k.desc {
				return cmp > 0
			}
			return cmp < 0
		}
		return a.ID < b.ID
	})
}

// compareItems orders two items on one field.
func compareItems(a, b *Item, field string) int {
	switch field {
	case "updated":
		return compareTime(a.Updated.Time, b.Updated.Time)
	case "created":
		return compareTime(a.Created.Time, b.Created.Time)
	case "due":
		return compareTime(a.Due.Time, b.Due.Time)
	case "priority":
		return priorityRank(a.Priority) - priorityRank(b.Priority)
	case "title":
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	case "status":
		return strings.Compare(string(a.Status), string(b.Status))
	default:
		return strings.Compare(string(a.ID), string(b.ID))
	}
}

func compareTime(a, b time.Time) int {
	switch {
	case a.Equal(b):
		return 0
	case a.Before(b):
		return -1
	default:
		return 1
	}
}

// priorityRank ranks a priority so that a descending sort puts critical first,
// which is what `sort=-priority` means to a user.
func priorityRank(p Priority) int {
	switch p {
	case PriorityCritical:
		return 4
	case PriorityHigh:
		return 3
	case PriorityMedium:
		return 2
	case PriorityLow:
		return 1
	default:
		return 0
	}
}

// Item returns one item by id.
func (ix *Index) Item(id ItemID) (*Item, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	it, ok := ix.byID[id]
	if !ok {
		return nil, fmt.Errorf("%s: %w", id, ErrItemNotFound)
	}
	out := cloneItem(it)
	return &out, nil
}

// Children returns the direct children of an item, sorted by id.
func (ix *Index) Children(id ItemID) []Item {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	kids := ix.children[id]
	out := make([]Item, 0, len(kids))
	for _, kid := range kids {
		if it, ok := ix.byID[kid]; ok {
			out = append(out, cloneItem(it))
		}
	}
	return out
}

// Comments returns the comment thread of an item, oldest first.
func (ix *Index) Comments(id ItemID) []Comment {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	list := ix.commentsByItem[id]
	out := make([]Comment, 0, len(list))
	for _, c := range list {
		out = append(out, cloneComment(c))
	}
	return out
}

// CommentCount returns how many comments an item has.
func (ix *Index) CommentCount(id ItemID) int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.commentsByItem[id])
}

// Milestones returns every milestone, sorted by due date and then by id.
func (ix *Index) Milestones() []Item {
	return ix.itemsOfType(TypeMilestone, func(a, b *Item) bool {
		if !a.Due.Equal(b.Due.Time) {
			if a.Due.IsZero() != b.Due.IsZero() {
				return b.Due.IsZero()
			}
			return a.Due.Before(b.Due.Time)
		}
		return a.ID < b.ID
	})
}

// Epics returns every epic, sorted by id.
func (ix *Index) Epics() []Item {
	return ix.itemsOfType(TypeEpic, func(a, b *Item) bool { return a.ID < b.ID })
}

func (ix *Index) itemsOfType(t ItemType, less func(a, b *Item) bool) []Item {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var picked []*Item
	for _, id := range sortedIDs(ix.byID) {
		if ix.byID[id].Type == t {
			picked = append(picked, ix.byID[id])
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return less(picked[i], picked[j]) })
	out := make([]Item, 0, len(picked))
	for _, it := range picked {
		out = append(out, cloneItem(it))
	}
	return out
}

// Page returns the metadata of one knowledge-base page.
func (ix *Index) Page(p string) (*KBPage, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	page, ok := ix.pagesByPath[path.Clean(p)]
	if !ok {
		return nil, false
	}
	out := *page
	out.Tags = append([]string(nil), page.Tags...)
	out.Headings = append([]Heading(nil), page.Headings...)
	out.Links = append([]Wikilink(nil), page.Links...)
	out.External = append([]string(nil), page.External...)
	return &out, true
}

// Pages returns every indexed knowledge-base page, sorted by path.
func (ix *Index) Pages() []*KBPage {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	out := make([]*KBPage, 0, len(ix.pagesByPath))
	for _, p := range sortedPaths(ix.pagesByPath) {
		out = append(out, ix.pagesByPath[p])
	}
	return out
}

// KbTree returns the knowledge base as a tree of folders and pages. With one
// project the tree is rooted at its documentation folder; with several it is
// rooted at the vault and each project contributes a subtree.
func (ix *Index) KbTree() *TreeNode {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	pages := make([]*KBPage, 0, len(ix.pagesByPath))
	for _, p := range sortedPaths(ix.pagesByPath) {
		pages = append(pages, ix.pagesByPath[p])
	}
	root := "."
	if len(ix.projects) == 1 {
		root = ix.projects[0].DocsPath
	}
	return buildTree(root, pages)
}

// SearchHit is one result of Search.
type SearchHit struct {
	Kind    string     `json:"kind"` // "item" or "page"
	ID      ItemID     `json:"id,omitempty"`
	Path    string     `json:"path"`
	Title   string     `json:"title"`
	Project ProjectKey `json:"project,omitempty"`
	Score   float64    `json:"score"`
	Snippet string     `json:"snippet,omitempty"`
}

// Search weights per field, from the ranking rules of docs/02 section 8: a title
// match outranks a label match, which outranks a body match, and an exact id
// match short-circuits to the top.
const (
	scoreID    = 100.0
	scoreTitle = 3.0
	scoreLabel = 2.0
	scoreBody  = 1.0
)

// Search runs a simple ranked search over items and knowledge-base pages.
//
// Every term must match somewhere; the score is the sum of the field weights of
// the fields it matched. Ties are broken by id and then by path, so the result
// order is stable. This is the v1 substring engine: the inverted index of
// docs/02 section 8 replaces it behind the same signature.
func (ix *Index) Search(q string, limit int) []SearchHit {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	terms := strings.Fields(strings.ToLower(q))
	if len(terms) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	var hits []SearchHit

	for _, id := range sortedIDs(ix.byID) {
		it := ix.byID[id]
		if it.Deleted {
			continue
		}
		title := strings.ToLower(it.Title)
		labels := strings.ToLower(strings.Join(it.Labels, " "))
		body := strings.ToLower(it.Body)
		idText := strings.ToLower(string(it.ID))
		score := 0.0
		matchedAll := true
		for _, t := range terms {
			switch {
			case idText == t:
				score += scoreID
			case strings.Contains(title, t):
				score += scoreTitle
			case strings.Contains(labels, t):
				score += scoreLabel
			case strings.Contains(body, t) || strings.Contains(idText, t):
				score += scoreBody
			default:
				matchedAll = false
			}
			if !matchedAll {
				break
			}
		}
		if !matchedAll || score == 0 {
			continue
		}
		hits = append(hits, SearchHit{
			Kind: "item", ID: it.ID, Path: it.Path, Title: it.Title,
			Project: ix.projectOf(it), Score: score, Snippet: snippet(it.Body, terms[0]),
		})
	}

	for _, p := range sortedPaths(ix.pagesByPath) {
		page := ix.pagesByPath[p]
		title := strings.ToLower(page.Title)
		tags := strings.ToLower(strings.Join(page.Tags, " "))
		body := strings.ToLower(page.Body)
		score := 0.0
		matchedAll := true
		for _, t := range terms {
			switch {
			case strings.Contains(title, t):
				score += scoreTitle
			case strings.Contains(tags, t):
				score += scoreLabel
			case strings.Contains(body, t):
				score += scoreBody
			default:
				matchedAll = false
			}
			if !matchedAll {
				break
			}
		}
		if !matchedAll || score == 0 {
			continue
		}
		hits = append(hits, SearchHit{
			Kind: "page", Path: page.Path, Title: page.Title,
			Project: page.Project, Score: score, Snippet: snippet(page.Body, terms[0]),
		})
	}

	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Path < b.Path
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// snippetRadius is how much context a snippet keeps around the match.
const snippetRadius = 60

// snippet extracts a one-line excerpt of body around the first occurrence of
// term. It returns the empty string when the term is not in the body.
func snippet(body, term string) string {
	flat := strings.Join(strings.Fields(body), " ")
	i := strings.Index(strings.ToLower(flat), term)
	if i < 0 {
		return ""
	}
	start := i - snippetRadius
	if start < 0 {
		start = 0
	}
	end := i + len(term) + snippetRadius
	if end > len(flat) {
		end = len(flat)
	}
	out := flat[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(flat) {
		out += "…"
	}
	return out
}

// ProjectFields projects an item onto the field names a caller asked for, which
// is what keeps an agent's item_list response small (docs/08 section 4.6). An
// unknown field name is ignored; an empty list means DefaultFields.
func ProjectFields(it *Item, fields []string) map[string]any {
	if it == nil {
		return nil
	}
	if len(fields) == 0 {
		fields = DefaultFields
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "id":
			out[f] = it.ID
		case "type":
			out[f] = it.Type
		case "project":
			if key, _, _, err := ParseItemID(string(it.ID)); err == nil {
				out[f] = key
			}
		case "title":
			out[f] = it.Title
		case "status":
			out[f] = it.Status
		case "priority":
			out[f] = nilIfEmpty(string(it.Priority))
		case "parent":
			parent := it.Parent
			if parent == "" {
				parent = it.Epic
			}
			out[f] = nilIfEmpty(string(parent))
		case "milestone":
			out[f] = nilIfEmpty(string(it.Milestone))
		case "sprint":
			out[f] = nilIfEmpty(it.Sprint)
		case "assignees":
			out[f] = append([]string(nil), it.Assignees...)
		case "labels":
			out[f] = append([]string(nil), it.Labels...)
		case "author":
			out[f] = nilIfEmpty(it.Author)
		case "owner":
			out[f] = nilIfEmpty(it.Owner)
		case "estimate":
			out[f] = floatOrNil(it.Estimate)
		case "effort":
			out[f] = floatOrNil(it.Effort)
		case "spent":
			out[f] = floatOrNil(it.Spent)
		case "created":
			out[f] = timeOrNil(it.Created)
		case "updated":
			out[f] = timeOrNil(it.Updated)
		case "started":
			out[f] = timeOrNil(it.Started)
		case "closed":
			out[f] = timeOrNil(it.Closed)
		case "start":
			out[f] = dateOrNil(it.Start)
		case "due":
			out[f] = dateOrNil(it.Due)
		case "links":
			out[f] = append([]Link(nil), it.Links...)
		case "path":
			out[f] = it.Path
		case "rev":
			out[f] = it.Rev
		case "body":
			out[f] = it.Body
		case "deleted":
			out[f] = it.Deleted
		case "custom":
			out[f] = it.Custom
		}
	}
	return out
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func floatOrNil(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func timeOrNil(ts Timestamp) any {
	if ts.IsZero() {
		return nil
	}
	return ts.String()
}

func dateOrNil(d Date) any {
	if d.IsZero() {
		return nil
	}
	return d.String()
}

// ParseUpdatedSince decodes the `updatedSince` parameter, which is either an
// ISO 8601 instant or a duration such as "7d", "12h" or "30m" counted back from
// now (docs/08 section 4.6).
func ParseUpdatedSince(s string, now time.Time) (Timestamp, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Timestamp{}, nil
	}
	if ts, err := ParseTimestamp(s); err == nil {
		return ts, nil
	}
	unit := s[len(s)-1]
	value, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || value < 0 {
		return Timestamp{}, fmt.Errorf("parse updatedSince %q: want an ISO 8601 instant or a duration such as 7d", s)
	}
	var d time.Duration
	switch unit {
	case 'd':
		d = time.Duration(value) * 24 * time.Hour
	case 'h':
		d = time.Duration(value) * time.Hour
	case 'm':
		d = time.Duration(value) * time.Minute
	case 'w':
		d = time.Duration(value) * 7 * 24 * time.Hour
	default:
		return Timestamp{}, fmt.Errorf("parse updatedSince %q: unknown unit %q", s, string(unit))
	}
	return NewTimestamp(now.Add(-d)), nil
}

// cloneItem returns a deep enough copy that a caller cannot mutate the index by
// modifying what it received.
func cloneItem(it *Item) Item {
	out := *it
	out.Assignees = append([]string(nil), it.Assignees...)
	out.Labels = append([]string(nil), it.Labels...)
	out.Links = append([]Link(nil), it.Links...)
	out.Attachments = append([]string(nil), it.Attachments...)
	out.Custom = cloneMap(it.Custom)
	out.Extra = cloneMap(it.Extra)
	out.Estimate = clonePtr(it.Estimate)
	out.Effort = clonePtr(it.Effort)
	out.Spent = clonePtr(it.Spent)
	return out
}

func cloneComment(c *Comment) Comment {
	out := *c
	out.Attachments = append([]string(nil), c.Attachments...)
	out.Extra = cloneMap(c.Extra)
	if c.Reactions != nil {
		out.Reactions = make(map[string][]string, len(c.Reactions))
		for k, v := range c.Reactions {
			out.Reactions[k] = append([]string(nil), v...)
		}
	}
	return out
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func clonePtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	f := *v
	return &f
}

func containsKey(list []ProjectKey, want ProjectKey) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func containsType(list []ItemType, want ItemType) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func containsStatus(list []Status, want Status) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func containsPriority(list []Priority, want Priority) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}
