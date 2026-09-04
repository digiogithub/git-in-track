package core

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// BoardsDirName is the folder of the team `.pmngr/` that holds boards
// (docs/04 section 5).
const BoardsDirName = "boards"

// BoardWildcard is the `statuses` key that applies to every project, and the
// `projects` entry that means "every non-archived project" (docs/04 5.1, 5.2).
const BoardWildcard = "*"

// BoardKind is the flavor of a board.
type BoardKind string

// The two board kinds. A scrum board is sprint-scoped and is implemented by
// GIT-US-0018; this build parses and validates it but renders kanban only.
const (
	BoardKanban BoardKind = "kanban"
	BoardScrum  BoardKind = "scrum"
)

// boardSlugRE is the grammar of a board id (docs/04 section 5.1).
var boardSlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)

// boardColumnRE is the grammar of a column id (docs/04 section 5.2).
var boardColumnRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Valid reports whether k is one of the known board kinds.
func (k BoardKind) Valid() bool { return k == BoardKanban || k == BoardScrum }

// BoardColumn is one column of a board: a name, a mapping onto per-project
// statuses and an advisory work-in-progress limit (docs/04 section 5.2).
//
// A column maps either explicit statuses or status categories, never both
// (R-COL-2, E-BOARD-COL-MAPPING). Categories are the portable form: they work
// for a project whose workflow this team has never seen.
type BoardColumn struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Statuses maps a project key — or BoardWildcard, the default — onto the
	// statuses of that project this column shows. A project override replaces
	// the wildcard entirely; the two are never merged (R-COL-1).
	Statuses map[string][]Status `yaml:"statuses,omitempty" json:"statuses,omitempty"`
	// Categories is the workflow-independent alternative to Statuses.
	Categories []StatusCategory `yaml:"categories,omitempty" json:"categories,omitempty"`
	// WIP is the advisory limit; 0 means unlimited (R-COL-5).
	WIP       int    `yaml:"wip,omitempty" json:"wip,omitempty"`
	Collapsed bool   `yaml:"collapsed,omitempty" json:"collapsed,omitempty"`
	Color     string `yaml:"color,omitempty" json:"color,omitempty"`
}

// StatusesFor returns the statuses this column shows for one project, in the
// order the board (or the project workflow) declares them. The first entry is
// the status a card acquires when it is dropped into the column (R-MOVE-2).
//
// A categories column needs the project configuration to answer; without one it
// returns nothing, which is exactly the state of a project nobody cloned.
func (c BoardColumn) StatusesFor(key ProjectKey, cfg *ProjectConfig) []Status {
	if len(c.Categories) > 0 {
		if cfg == nil {
			return nil
		}
		var out []Status
		for _, def := range cfg.Workflow.Statuses {
			for _, want := range c.Categories {
				if def.Category == want {
					out = append(out, def.ID)
					break
				}
			}
		}
		return out
	}
	if mapped, ok := c.Statuses[string(key)]; ok {
		return append([]Status(nil), mapped...)
	}
	return append([]Status(nil), c.Statuses[BoardWildcard]...)
}

// Shows reports whether a status of a project belongs to this column.
func (c BoardColumn) Shows(key ProjectKey, cfg *ProjectConfig, status Status) bool {
	return containsStatus(c.StatusesFor(key, cfg), status)
}

// mappingKeys returns the `statuses` keys in emission order: the wildcard first,
// then the project overrides sorted, so that the file is stable across runs.
func (c BoardColumn) mappingKeys() []string {
	keys := make([]string, 0, len(c.Statuses))
	for k := range c.Statuses {
		if k != BoardWildcard {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if _, ok := c.Statuses[BoardWildcard]; ok {
		keys = append([]string{BoardWildcard}, keys...)
	}
	return keys
}

// BoardFilters narrows what a board shows. Every filter is ANDed and an absent
// key imposes no constraint (docs/04 section 5.3).
type BoardFilters struct {
	Projects      []ProjectKey `yaml:"projects,omitempty" json:"projects,omitempty"`
	Types         []ItemType   `yaml:"types,omitempty" json:"types,omitempty"`
	LabelsAny     []string     `yaml:"labels_any,omitempty" json:"labelsAny,omitempty"`
	LabelsAll     []string     `yaml:"labels_all,omitempty" json:"labelsAll,omitempty"`
	LabelsNone    []string     `yaml:"labels_none,omitempty" json:"labelsNone,omitempty"`
	Assignees     []string     `yaml:"assignees,omitempty" json:"assignees,omitempty"`
	Priorities    []Priority   `yaml:"priorities,omitempty" json:"priorities,omitempty"`
	Milestone     string       `yaml:"milestone,omitempty" json:"milestone,omitempty"`
	Sprint        string       `yaml:"sprint,omitempty" json:"sprint,omitempty"`
	DueBefore     Date         `yaml:"due_before,omitempty" json:"dueBefore,omitempty"`
	UpdatedSince  Timestamp    `yaml:"updated_since,omitempty" json:"updatedSince,omitempty"`
	IncludeClosed bool         `yaml:"include_closed,omitempty" json:"includeClosed,omitempty"`
	Query         string       `yaml:"query,omitempty" json:"query,omitempty"`
}

// Empty reports whether the filter block constrains nothing.
func (f BoardFilters) Empty() bool {
	return len(f.Projects) == 0 && len(f.Types) == 0 && len(f.LabelsAny) == 0 &&
		len(f.LabelsAll) == 0 && len(f.LabelsNone) == 0 && len(f.Assignees) == 0 &&
		len(f.Priorities) == 0 && f.Milestone == "" && f.Sprint == "" &&
		f.DueBefore.IsZero() && f.UpdatedSince.IsZero() && !f.IncludeClosed && f.Query == ""
}

// BoardSwimlanes groups the cards of every column into horizontal lanes
// (docs/04 section 5.4).
type BoardSwimlanes struct {
	By            string   `yaml:"by,omitempty" json:"by,omitempty"`
	Order         []string `yaml:"order,omitempty" json:"order,omitempty"`
	CollapseEmpty bool     `yaml:"collapse_empty,omitempty" json:"collapseEmpty,omitempty"`
}

// BoardCardDisplay lists the card fields the board shows.
type BoardCardDisplay struct {
	Show []string `yaml:"show,omitempty" json:"show,omitempty"`
}

// BoardOrder is the `order:` mapping of a board: column id → refs, one ref per
// line (R-ORD-3). It keeps the columns in file order so that rewriting a board
// never reshuffles the untouched half of the mapping, and so that two people
// re-ordering different columns produce non-overlapping diffs.
type BoardOrder struct {
	columns []string
	refs    map[string][]string
}

// NewBoardOrder returns an empty order list.
func NewBoardOrder() *BoardOrder {
	return &BoardOrder{refs: map[string][]string{}}
}

// Columns returns the column ids that carry an order list, in file order.
func (o *BoardOrder) Columns() []string {
	if o == nil {
		return nil
	}
	return append([]string(nil), o.columns...)
}

// Refs returns the ordered refs of a column, empty when it has none.
func (o *BoardOrder) Refs(column string) []string {
	if o == nil {
		return nil
	}
	return append([]string(nil), o.refs[column]...)
}

// Has reports whether the column carries an order list at all, which is what
// tells an explicitly empty list from an absent one.
func (o *BoardOrder) Has(column string) bool {
	if o == nil {
		return false
	}
	_, ok := o.refs[column]
	return ok
}

// Set replaces the order list of a column, appending the column to the mapping
// when it was not listed yet.
func (o *BoardOrder) Set(column string, refs []string) {
	if o.refs == nil {
		o.refs = map[string][]string{}
	}
	if _, ok := o.refs[column]; !ok {
		o.columns = append(o.columns, column)
	}
	o.refs[column] = append([]string(nil), refs...)
}

// Remove drops a ref from every column. It is what a move does before inserting
// the ref at its new position, and what keeps a ref out of two columns at once.
func (o *BoardOrder) Remove(ref string) {
	if o == nil {
		return
	}
	for column, refs := range o.refs {
		kept := refs[:0]
		for _, existing := range refs {
			if existing != ref {
				kept = append(kept, existing)
			}
		}
		o.refs[column] = kept
	}
}

// Insert places a ref at position in a column, clamping the position into
// range. A negative position appends.
func (o *BoardOrder) Insert(column, ref string, position int) {
	refs := o.Refs(column)
	if position < 0 || position > len(refs) {
		position = len(refs)
	}
	out := make([]string, 0, len(refs)+1)
	out = append(out, refs[:position]...)
	out = append(out, ref)
	out = append(out, refs[position:]...)
	o.Set(column, out)
}

// Clone returns a deep copy, so that a failed move can be rolled back onto the
// board it started from.
func (o *BoardOrder) Clone() *BoardOrder {
	if o == nil {
		return nil
	}
	out := &BoardOrder{columns: append([]string(nil), o.columns...), refs: map[string][]string{}}
	for k, v := range o.refs {
		out.refs[k] = append([]string(nil), v...)
	}
	return out
}

// UnmarshalYAML decodes the mapping while keeping the order of its keys.
func (o *BoardOrder) UnmarshalYAML(node *yaml.Node) error {
	o.columns = nil
	o.refs = map[string][]string{}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("order: want a mapping of column id to refs")
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		var refs []string
		if err := node.Content[i+1].Decode(&refs); err != nil {
			return fmt.Errorf("order %q: %w", key, err)
		}
		o.Set(key, refs)
	}
	return nil
}

// Board is the parsed form of `.pmngr/boards/<slug>.md`. It is a view over
// items that live in other repositories and holds no item state of its own
// (docs/04 section 5).
type Board struct {
	ID          string       `yaml:"id" json:"id"`
	Type        string       `yaml:"type" json:"type"`
	Kind        BoardKind    `yaml:"kind" json:"kind"`
	Title       string       `yaml:"title" json:"title"`
	Description string       `yaml:"description,omitempty" json:"description,omitempty"`
	Projects    []ProjectKey `yaml:"projects,omitempty" json:"projects,omitempty"`
	// Sprint and BacklogColumn are scrum-only (docs/04 section 5.5).
	Sprint        string           `yaml:"sprint,omitempty" json:"sprint,omitempty"`
	BacklogColumn string           `yaml:"backlog_column,omitempty" json:"backlogColumn,omitempty"`
	Columns       []BoardColumn    `yaml:"columns" json:"columns"`
	Filters       BoardFilters     `yaml:"filters,omitempty" json:"filters"`
	Swimlanes     BoardSwimlanes   `yaml:"swimlanes,omitempty" json:"swimlanes"`
	Card          BoardCardDisplay `yaml:"card,omitempty" json:"card"`
	Order         *BoardOrder      `yaml:"order,omitempty" json:"-"`
	Created       Timestamp        `yaml:"created,omitempty" json:"created,omitempty"`
	Updated       Timestamp        `yaml:"updated,omitempty" json:"updated,omitempty"`
	Author        string           `yaml:"author,omitempty" json:"author,omitempty"`

	// Extra preserves the front-matter keys this version does not model, as on
	// Item, so that an older binary never damages a newer file.
	Extra map[string]any `yaml:"-" json:"extra,omitempty"`

	// Derived fields, never stored in the file.
	Body string `yaml:"-" json:"body"`
	Path string `yaml:"-" json:"path"`
	Rev  Rev    `yaml:"-" json:"rev"`
}

// boardKnownKeys is the set of front-matter keys Board models. Everything else
// is kept verbatim in Extra.
var boardKnownKeys = map[string]bool{
	"id": true, "type": true, "kind": true, "title": true, "description": true,
	"projects": true, "sprint": true, "backlog_column": true, "columns": true,
	"filters": true, "swimlanes": true, "card": true, "order": true,
	"created": true, "updated": true, "author": true,
}

// OrderList returns the order mapping, never nil, so that callers can mutate it
// without a preliminary check.
func (b *Board) OrderList() *BoardOrder {
	if b.Order == nil {
		b.Order = NewBoardOrder()
	}
	return b.Order
}

// Column returns the column with the given id.
func (b *Board) Column(id string) (BoardColumn, bool) {
	for _, c := range b.Columns {
		if c.ID == id {
			return c, true
		}
	}
	return BoardColumn{}, false
}

// Scope returns the project keys the board shows, given every project the team
// declares. An absent or wildcard `projects` list means all of them, and
// `filters.projects` intersects the result (docs/04 section 5.3).
func (b *Board) Scope(declared []ProjectKey) []ProjectKey {
	scope := declared
	if len(b.Projects) > 0 && !containsKey(b.Projects, BoardWildcard) {
		scope = intersectKeys(declared, b.Projects)
		// A board may name a project the team does not declare; it is reported
		// by Validate and kept here so that its cards render as inert text.
		for _, key := range b.Projects {
			if !containsKey(declared, key) && !containsKey(scope, key) {
				scope = append(scope, key)
			}
		}
	}
	if len(b.Filters.Projects) > 0 {
		scope = intersectKeys(scope, b.Filters.Projects)
	}
	return scope
}

// InScope reports whether a project key is shown by the board.
func (b *Board) InScope(declared []ProjectKey, key ProjectKey) bool {
	return containsKey(b.Scope(declared), key)
}

// intersectKeys keeps the entries of a that also appear in b, in a's order.
func intersectKeys(a, b []ProjectKey) []ProjectKey {
	out := make([]ProjectKey, 0, len(a))
	for _, key := range a {
		if containsKey(b, key) {
			out = append(out, key)
		}
	}
	return out
}

// ParseBoard decodes a board file. Like ParseItem it reports one *ParseError
// carrying the diagnostic code, and it fills the derived Path and Rev.
func ParseBoard(filePath string, data []byte) (*Board, error) {
	block, body, err := SplitFrontMatter(data)
	if err != nil {
		return nil, newParseError(filePath, 0, "", CodeFMMissing, "front matter is missing or unterminated", err)
	}
	var b Board
	if err := yaml.Unmarshal(block, &b); err != nil {
		return nil, newParseError(filePath, 0, "", CodeFMYAML, "front matter is not valid YAML", err)
	}
	fm := map[string]any{}
	if err := yaml.Unmarshal(block, &fm); err != nil {
		return nil, newParseError(filePath, 0, "", CodeFMYAML, "front matter is not a mapping", err)
	}
	for key, value := range fm {
		if boardKnownKeys[key] {
			continue
		}
		if b.Extra == nil {
			b.Extra = map[string]any{}
		}
		b.Extra[key] = value
	}
	if b.Type == "" {
		b.Type = "board"
	}
	b.Body = body
	b.Path = filePath
	b.Rev = ComputeRev(data)
	return &b, nil
}

// SerializeBoard renders a board back to file bytes in canonical form: the key
// order below, one column per block entry and one ref per line under `order:`.
//
// Serializing a parsed board is idempotent, and the emitter is deterministic:
// two people who move different cards produce diffs that touch different lines
// and therefore merge (R-ORD-3).
func SerializeBoard(b *Board) ([]byte, error) {
	if b == nil {
		return nil, errors.New("serialize board: nil board")
	}
	w := &fmWriter{}
	w.scalar("id", b.ID)
	w.scalar("type", b.Type)
	w.scalar("kind", string(b.Kind))
	w.scalar("title", b.Title)
	w.scalar("description", b.Description)
	w.stringList("projects", keyStrings(b.Projects))
	w.scalar("sprint", b.Sprint)
	w.scalar("backlog_column", b.BacklogColumn)
	writeBoardColumns(w, b.Columns)
	writeBoardFilters(w, b.Filters)
	writeBoardSwimlanes(w, b.Swimlanes)
	if len(b.Card.Show) > 0 {
		w.b.WriteString("card:\n")
		w.b.WriteString("  show: [" + strings.Join(b.Card.Show, ", ") + "]\n")
	}
	writeBoardOrder(w, b)
	w.timestamp("created", b.Created)
	w.timestamp("updated", b.Updated)
	w.scalar("author", b.Author)
	if err := w.extra(b.Extra); err != nil {
		return nil, fmt.Errorf("serialize board %s: %w", b.Path, err)
	}
	return assemble(w.String(), b.Body), nil
}

// writeBoardColumns emits the column list, one block mapping per column.
func writeBoardColumns(w *fmWriter, columns []BoardColumn) {
	if len(columns) == 0 {
		return
	}
	w.b.WriteString("columns:\n")
	for _, c := range columns {
		w.b.WriteString("  - id: " + yamlString(c.ID) + "\n")
		if c.Name != "" {
			w.b.WriteString("    name: " + yamlString(c.Name) + "\n")
		}
		if len(c.Categories) > 0 {
			cats := make([]string, 0, len(c.Categories))
			for _, cat := range c.Categories {
				cats = append(cats, string(cat))
			}
			w.b.WriteString("    categories: [" + strings.Join(cats, ", ") + "]\n")
		}
		if keys := c.mappingKeys(); len(keys) > 0 {
			w.b.WriteString("    statuses:\n")
			for _, key := range keys {
				values := make([]string, 0, len(c.Statuses[key]))
				for _, s := range c.Statuses[key] {
					values = append(values, yamlFlowString(string(s)))
				}
				w.b.WriteString("      " + yamlString(key) + ": [" + strings.Join(values, ", ") + "]\n")
			}
		}
		if c.WIP > 0 {
			fmt.Fprintf(&w.b, "    wip: %d\n", c.WIP)
		}
		if c.Collapsed {
			w.b.WriteString("    collapsed: true\n")
		}
		if c.Color != "" {
			w.b.WriteString("    color: " + yamlString(c.Color) + "\n")
		}
	}
}

// writeBoardFilters emits the filter block, omitting the keys that constrain
// nothing.
func writeBoardFilters(w *fmWriter, f BoardFilters) {
	if f.Empty() {
		return
	}
	w.b.WriteString("filters:\n")
	sub := &fmWriter{}
	sub.stringList("projects", keyStrings(f.Projects))
	sub.stringList("types", typeStrings(f.Types))
	sub.stringList("labels_any", f.LabelsAny)
	sub.stringList("labels_all", f.LabelsAll)
	sub.stringList("labels_none", f.LabelsNone)
	sub.stringList("assignees", f.Assignees)
	sub.stringList("priorities", priorityStrings(f.Priorities))
	sub.scalar("milestone", f.Milestone)
	sub.scalar("sprint", f.Sprint)
	sub.date("due_before", f.DueBefore)
	sub.timestamp("updated_since", f.UpdatedSince)
	if f.IncludeClosed {
		sub.raw("include_closed", "true")
	}
	sub.scalar("query", f.Query)
	indentInto(w, sub.String(), "  ")
}

// writeBoardSwimlanes emits the swimlane block.
func writeBoardSwimlanes(w *fmWriter, s BoardSwimlanes) {
	if s.By == "" && len(s.Order) == 0 && !s.CollapseEmpty {
		return
	}
	w.b.WriteString("swimlanes:\n")
	sub := &fmWriter{}
	sub.scalar("by", s.By)
	sub.stringList("order", s.Order)
	if s.CollapseEmpty {
		sub.raw("collapse_empty", "true")
	}
	indentInto(w, sub.String(), "  ")
}

// writeBoardOrder emits the card order, one ref per line. Columns the board no
// longer declares are dropped, which is the pruning of R-ORD-2.
func writeBoardOrder(w *fmWriter, b *Board) {
	if b.Order == nil {
		return
	}
	columns := make([]string, 0, len(b.Columns))
	for _, id := range b.Order.Columns() {
		if _, ok := b.Column(id); ok {
			columns = append(columns, id)
		}
	}
	if len(columns) == 0 {
		return
	}
	w.b.WriteString("order:\n")
	for _, id := range columns {
		refs := b.Order.Refs(id)
		if len(refs) == 0 {
			w.b.WriteString("  " + yamlString(id) + ": []\n")
			continue
		}
		w.b.WriteString("  " + yamlString(id) + ":\n")
		for _, ref := range refs {
			w.b.WriteString("    - " + yamlFlowString(ref) + "\n")
		}
	}
}

// indentInto appends an already rendered block, indented by pad.
func indentInto(w *fmWriter, block, pad string) {
	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		if line == "" {
			continue
		}
		w.b.WriteString(pad + line + "\n")
	}
}

func keyStrings(keys []ProjectKey) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, string(k))
	}
	return out
}

func typeStrings(types []ItemType) []string {
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}
	return out
}

func priorityStrings(list []Priority) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, string(p))
	}
	return out
}

// Validate applies the rules of docs/04 section 5.10. declared is every project
// key of team.yaml and configs holds the workflow of the projects that are
// cloned; both may be empty, in which case the rules that need them are skipped
// rather than guessed.
func (b *Board) Validate(declared []ProjectKey, configs map[ProjectKey]*ProjectConfig) []Diagnostic {
	var out []Diagnostic
	add := func(code Code, sev Severity, field, msg string) {
		out = append(out, Diagnostic{Code: code, Severity: sev, Path: b.Path, Field: field, Message: msg})
	}

	stem := boardStem(b.Path)
	switch {
	case b.ID == "":
		add(CodeBoardID, SeverityError, "id", "missing")
	case !boardSlugRE.MatchString(b.ID):
		add(CodeBoardID, SeverityError, "id",
			fmt.Sprintf("%q does not match [a-z0-9][a-z0-9-]{0,47}", b.ID))
	case stem != "" && stem != b.ID:
		add(CodeBoardID, SeverityError, "id",
			fmt.Sprintf("id %q does not match the file name %q", b.ID, stem))
	}
	if !b.Kind.Valid() {
		add(CodeBoardKind, SeverityError, "kind",
			fmt.Sprintf("%q is neither kanban nor scrum", b.Kind))
	}
	if strings.TrimSpace(b.Title) == "" {
		add(CodeBoardID, SeverityError, "title", "missing")
	}
	if b.Kind == BoardKanban && b.Sprint != "" {
		add(CodeBoardSprintKind, SeverityError, "sprint", "a kanban board cannot be scoped to a sprint")
	}

	if len(b.Columns) == 0 {
		add(CodeBoardColumns, SeverityError, "columns", "a board needs at least one column")
	}
	seen := map[string]bool{}
	for _, c := range b.Columns {
		switch {
		case c.ID == "":
			add(CodeBoardColumns, SeverityError, "columns", "a column has no id")
			continue
		case !boardColumnRE.MatchString(c.ID):
			add(CodeBoardColumns, SeverityError, "columns",
				fmt.Sprintf("column id %q does not match [a-z][a-z0-9_-]{0,31}", c.ID))
			continue
		}
		if seen[c.ID] {
			add(CodeBoardColumns, SeverityError, "columns", fmt.Sprintf("duplicate column id %q", c.ID))
			continue
		}
		seen[c.ID] = true
		hasStatuses := len(c.Statuses) > 0
		hasCategories := len(c.Categories) > 0
		if hasStatuses == hasCategories {
			add(CodeBoardColMapping, SeverityError, "columns",
				fmt.Sprintf("column %q must declare exactly one of statuses or categories", c.ID))
		}
		for _, cat := range c.Categories {
			if !cat.Valid() {
				add(CodeBoardColMapping, SeverityError, "columns",
					fmt.Sprintf("column %q maps the unknown category %q", c.ID, cat))
			}
		}
	}
	if b.BacklogColumn != "" && !seen[b.BacklogColumn] {
		add(CodeBoardColumns, SeverityError, "backlog_column",
			fmt.Sprintf("%q is not a column of this board", b.BacklogColumn))
	}

	// A status that two columns claim would make a card's position ambiguous.
	for key, cfg := range configs {
		if len(declared) > 0 && !b.InScope(declared, key) {
			continue
		}
		owner := map[Status]string{}
		for _, c := range b.Columns {
			for _, s := range c.StatusesFor(key, cfg) {
				if previous, taken := owner[s]; taken {
					add(CodeBoardStatusAmbiguous, SeverityError, "columns",
						fmt.Sprintf("status %s of project %s maps to both %q and %q", s, key, previous, c.ID))
					continue
				}
				owner[s] = c.ID
			}
		}
		if cfg == nil {
			continue
		}
		for _, def := range cfg.Workflow.Statuses {
			if _, mapped := owner[def.ID]; !mapped {
				add(CodeBoardUnmappedStatus, SeverityWarning, "columns",
					fmt.Sprintf("status %s of project %s maps to no column; its items are listed apart", def.ID, key))
			}
		}
	}

	for _, key := range b.Projects {
		if key != BoardWildcard && len(declared) > 0 && !containsKey(declared, key) {
			add(CodeBoardUnknownProject, SeverityWarning, "projects",
				fmt.Sprintf("project %s is not declared in %s", key, TeamFileName))
		}
	}

	for _, column := range b.Order.Columns() {
		if !seen[column] {
			add(CodeBoardColumns, SeverityWarning, "order",
				fmt.Sprintf("order lists the unknown column %q; it is pruned on the next write", column))
		}
		for _, raw := range b.Order.Refs(column) {
			ref, err := ParseRef(raw)
			if err != nil {
				add(CodeBoardRefFormat, SeverityWarning, "order", err.Error())
				continue
			}
			if len(declared) > 0 && !containsKey(declared, ref.Project) {
				add(CodeBoardUnknownProject, SeverityWarning, "order",
					fmt.Sprintf("ref %s names the undeclared project %s", raw, ref.Project))
			}
		}
	}

	sortDiagnostics(out)
	return out
}

// boardStem returns the file-name stem of a board path, empty when there is no
// path to compare the id against.
func boardStem(p string) string {
	if p == "" {
		return ""
	}
	name := p
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return strings.TrimSuffix(name, ".md")
}

// BoardStore reads and writes the boards of a team repository. It is the only
// place that touches `.pmngr/boards/`, and it goes through core.FS so that the
// browser and the companion share one implementation.
type BoardStore struct {
	fs  FS
	dir string
	// Clock supplies the `updated` stamp of a write. Nil means "leave it".
	Clock Clock
}

// NewBoardStore returns a store over the `boards/` folder of a team `.pmngr/`.
// teamDir is TeamRef.TeamDirPath.
func NewBoardStore(fsys FS, teamDir string) *BoardStore {
	return &BoardStore{fs: fsys, dir: joinPath(teamDir, BoardsDirName)}
}

// Dir returns the vault-relative folder the store reads.
func (s *BoardStore) Dir() string { return s.dir }

// PathOf returns the file a board slug is stored in.
func (s *BoardStore) PathOf(id string) string { return joinPath(s.dir, id+".md") }

// List returns every board of the team repository, sorted by id. A file that
// does not parse is skipped and reported through the returned diagnostics
// rather than failing the whole listing.
func (s *BoardStore) List(ctx context.Context) ([]*Board, []Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, wrapContext("board list", err)
	}
	entries, err := s.fs.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", s.dir, err)
	}
	var boards []*Board
	var diags []Diagnostic
	for _, e := range entries {
		if e.IsDir || !isMarkdown(e.Name) {
			continue
		}
		full := joinPath(s.dir, e.Name)
		data, err := s.fs.ReadFile(full)
		if err != nil {
			diags = append(diags, Diagnostic{
				Code: CodeBoardID, Severity: SeverityError, Path: full,
				Message: fmt.Sprintf("cannot read the board: %v", err),
			})
			continue
		}
		board, err := ParseBoard(full, data)
		if err != nil {
			diags = append(diags, diagnosticOf(err, full))
			continue
		}
		boards = append(boards, board)
	}
	sort.SliceStable(boards, func(i, j int) bool { return boards[i].ID < boards[j].ID })
	return boards, diags, nil
}

// Get reads one board by its slug.
func (s *BoardStore) Get(ctx context.Context, id string) (*Board, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("board get", err)
	}
	full := s.PathOf(id)
	data, err := s.fs.ReadFile(full)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, fmt.Errorf("board %s: %w", id, ErrItemNotFound)
		}
		return nil, fmt.Errorf("read %s: %w", full, err)
	}
	return ParseBoard(full, data)
}

// Write persists a board, enforcing the optimistic lock when expected is not
// empty. It returns the board with its new rev.
func (s *BoardStore) Write(ctx context.Context, b *Board, expected Rev) (*Board, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("board write", err)
	}
	if b == nil {
		return nil, errors.New("board write: nil board")
	}
	full := b.Path
	if full == "" {
		full = s.PathOf(b.ID)
		b.Path = full
	}
	if expected != "" {
		current, err := s.fs.ReadFile(full)
		if err != nil && !errors.Is(err, ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", full, err)
		}
		if err == nil {
			if got := ComputeRev(current); got != expected {
				return nil, &StaleRevisionError{Path: full, Expected: expected, Current: got}
			}
		}
	}
	if s.Clock != nil {
		b.Updated = NewTimestamp(s.Clock.Now())
	}
	data, err := SerializeBoard(b)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(s.fs, full, data); err != nil {
		return nil, fmt.Errorf("write %s: %w", full, err)
	}
	b.Rev = ComputeRev(data)
	return b, nil
}
