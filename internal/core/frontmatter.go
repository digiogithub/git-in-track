package core

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// delimiter is the front-matter fence, both opening and closing (R-FMT-1, R-FMT-2).
const delimiter = "---"

// fmFirstLine is the file line number the front matter starts on. The fence
// occupies line 1, so a node reported on line N of the block is on line N+1 of
// the file.
const fmFirstLine = 1

// canonicalKeyOrder is the order writers MUST use, from docs/03 section 3.2,
// extended with the milestone-only keys (start, owner) and the comment-only keys
// (item, in_reply_to, kind, reactions) in the region they belong to. Unknown keys
// follow, sorted lexicographically.
var canonicalKeyOrder = []string{
	"id", "type", "item", "title", "status", "priority",
	"parent", "epic", "milestone", "sprint",
	"assignees", "author", "owner", "labels",
	"estimate", "effort", "spent",
	"created", "updated", "started", "closed", "start", "due",
	"links", "blocks", "depends_on", "in_reply_to", "kind", "reactions",
	"attachments", "custom", "deleted",
}

// knownKeys is the set of front-matter keys this version understands. Everything
// else is preserved verbatim in Item.Extra or Comment.Extra.
var knownKeys = func() map[string]bool {
	m := make(map[string]bool, len(canonicalKeyOrder))
	for _, k := range canonicalKeyOrder {
		m[k] = true
	}
	return m
}()

// SplitFrontMatter separates the YAML front-matter block from the Markdown body.
// It tolerates a UTF-8 BOM and CRLF line endings (R-FMT-1, R-FMT-4): the returned
// block and body always use LF. The body keeps its bytes, minus the blank lines
// that surround it.
//
// It returns an error wrapping ErrInvalidFrontMatter when the file does not start
// with a fence or the fence is never closed.
func SplitFrontMatter(data []byte) (frontMatter []byte, body string, err error) {
	clean := bytes.TrimPrefix(data, bom)
	clean = bytes.ReplaceAll(clean, []byte("\r\n"), []byte("\n"))

	rest, ok := bytes.CutPrefix(clean, []byte(delimiter+"\n"))
	if !ok {
		if bytes.Equal(bytes.TrimRight(clean, "\n"), []byte(delimiter)) {
			return nil, "", fmt.Errorf("%w: front matter is not closed", ErrInvalidFrontMatter)
		}
		return nil, "", fmt.Errorf("%w: file does not start with %q", ErrInvalidFrontMatter, delimiter)
	}

	end := indexClosingFence(rest)
	if end < 0 {
		return nil, "", fmt.Errorf("%w: front matter is not closed", ErrInvalidFrontMatter)
	}
	block := rest[:end]
	after := rest[end:]
	// Skip the closing fence line itself.
	if i := bytes.IndexByte(after, '\n'); i >= 0 {
		after = after[i+1:]
	} else {
		after = nil
	}
	return block, strings.Trim(string(after), "\n"), nil
}

// indexClosingFence returns the offset of the closing fence line, or -1.
func indexClosingFence(rest []byte) int {
	for offset := 0; offset < len(rest); {
		lineEnd := bytes.IndexByte(rest[offset:], '\n')
		var line []byte
		if lineEnd < 0 {
			line = rest[offset:]
		} else {
			line = rest[offset : offset+lineEnd]
		}
		if string(bytes.TrimRight(line, " \t")) == delimiter {
			return offset
		}
		if lineEnd < 0 {
			return -1
		}
		offset += lineEnd + 1
	}
	return -1
}

// ParseDocument splits a file and decodes its front matter into a generic map.
// Unquoted ISO 8601 scalars arrive as time.Time, exactly as YAML defines them
// (R-TIME-4); the typed parsers normalise them.
func ParseDocument(data []byte) (frontMatter map[string]any, body string, err error) {
	block, body, err := SplitFrontMatter(data)
	if err != nil {
		return nil, "", err
	}
	node, err := decodeMappingNode(block)
	if err != nil {
		return nil, "", err
	}
	fm := map[string]any{}
	if len(node.Content) > 0 {
		if err := node.Decode(&fm); err != nil {
			return nil, "", fmt.Errorf("%w: %w", ErrInvalidFrontMatter, err)
		}
	}
	return fm, body, nil
}

// decodeMappingNode parses the block into a YAML mapping node and rejects the
// constructs the data model forbids: anchors, aliases and multiple documents
// (R-FMT-3).
func decodeMappingNode(block []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(block))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, errEOF) || strings.Contains(err.Error(), "EOF") {
			// An empty front-matter block is an empty mapping.
			return &yaml.Node{Kind: yaml.MappingNode}, nil
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalidFrontMatter, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("%w: front matter must be a single YAML document", ErrInvalidFrontMatter)
	}
	node := &doc
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return &yaml.Node{Kind: yaml.MappingNode}, nil
		}
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: front matter must be a mapping", ErrInvalidFrontMatter)
	}
	if err := rejectAnchors(node); err != nil {
		return nil, err
	}
	return node, nil
}

// errEOF is the sentinel yaml.v3 returns for an empty input.
var errEOF = errors.New("EOF")

// rejectAnchors walks a node tree and refuses anchors and aliases.
func rejectAnchors(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return fmt.Errorf("%w: anchors and aliases are not supported (line %d)", ErrInvalidFrontMatter, n.Line)
	}
	for _, c := range n.Content {
		if err := rejectAnchors(c); err != nil {
			return err
		}
	}
	return nil
}

// keyLines maps every top-level front-matter key to the file line of its value,
// so that parse errors can point at the offending line.
func keyLines(node *yaml.Node) map[string]int {
	lines := make(map[string]int, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		lines[node.Content[i].Value] = node.Content[i].Line + fmFirstLine
	}
	return lines
}

// ParseItem parses the bytes of an item file into an Item. The path is used for
// error messages, for the id-vs-filename check (E-ID-FILENAME) and is stored on
// the result; it is vault-relative and uses forward slashes.
//
// Structural problems (missing or malformed front matter, an id that does not
// match the grammar, a date that is not ISO 8601) are returned as one or more
// *ParseError joined together, all wrapping ErrInvalidFrontMatter. Everything
// that needs the project configuration or the rest of the vault — unknown
// statuses, dangling references, workflow transitions — is left to the validator.
func ParseItem(path string, data []byte) (*Item, error) {
	block, body, err := SplitFrontMatter(data)
	if err != nil {
		return nil, newParseError(path, 1, "", CodeFMMissing, strings.TrimPrefix(err.Error(), ErrInvalidFrontMatter.Error()+": "), nil)
	}
	node, err := decodeMappingNode(block)
	if err != nil {
		return nil, newParseError(path, 1, "", CodeFMYAML, strings.TrimPrefix(err.Error(), ErrInvalidFrontMatter.Error()+": "), nil)
	}
	fm := map[string]any{}
	if len(node.Content) > 0 {
		if err := node.Decode(&fm); err != nil {
			return nil, newParseError(path, 1, "", CodeFMYAML, err.Error(), nil)
		}
	}

	p := &fieldReader{path: path, fm: fm, lines: keyLines(node)}
	it := &Item{
		Path: path,
		Body: body,
		Rev:  ComputeRev(data),
	}

	it.Type = ItemType(p.str("type"))
	if it.Type == "" {
		p.fail("type", CodeFMType, "missing")
	} else if !it.Type.Valid() {
		p.fail("type", CodeFMType, fmt.Sprintf("unknown item type %q", it.Type))
	}

	rawID := p.str("id")
	if rawID == "" {
		if it.Type != TypeComment {
			p.fail("id", CodeIDMissing, "missing")
		}
	} else {
		it.ID = ItemID(rawID)
		_, code, _, idErr := ParseItemID(rawID)
		switch {
		case idErr != nil:
			p.fail("id", CodeIDGrammar, fmt.Sprintf("%q does not match <KEY>-<EP|US|T|M>-<NNNN>", rawID))
		default:
			if want, ok := TypeCodeFor(it.Type); ok && want != code {
				p.fail("id", CodeIDTypeCode, fmt.Sprintf("type code %q does not match type %q", code, it.Type))
			}
			if named := IDFromFileName(path); named != "" && named != it.ID {
				p.fail("id", CodeIDFilename, fmt.Sprintf("file name claims %q but the id field says %q", named, it.ID))
			}
		}
	}

	it.Title = p.str("title")
	if it.Type != TypeComment {
		switch {
		case strings.TrimSpace(it.Title) == "":
			p.fail("title", CodeTitle, "missing or empty")
		case len(it.Title) > 200:
			p.fail("title", CodeTitle, "longer than 200 characters")
		}
	}

	it.Status = Status(p.str("status"))
	it.Priority = Priority(p.str("priority"))
	if it.Priority != "" && !it.Priority.Valid() {
		p.fail("priority", CodeEnum, fmt.Sprintf("unknown priority %q", it.Priority))
	}
	it.Parent = ItemID(p.str("parent"))
	it.Epic = ItemID(p.str("epic"))
	it.Milestone = ItemID(p.str("milestone"))
	it.Sprint = p.str("sprint")
	it.Assignees = p.strList("assignees")
	it.Author = p.str("author")
	it.Owner = p.str("owner")
	it.Labels = p.strList("labels")
	it.Estimate = p.number("estimate")
	it.Effort = p.number("effort")
	it.Spent = p.number("spent")
	it.Created = p.timestamp("created")
	it.Updated = p.timestamp("updated")
	it.Started = p.timestamp("started")
	it.Closed = p.timestamp("closed")
	it.Start = p.date("start")
	it.Due = p.date("due")
	it.Links = p.links("links")
	it.Links = append(it.Links, p.aliasLinks("blocks", LinkBlocks)...)
	it.Links = append(it.Links, p.aliasLinks("depends_on", LinkBlockedBy)...)
	it.Attachments = p.strList("attachments")
	it.Custom = p.mapping("custom")
	it.Deleted = p.boolean("deleted")
	it.Extra = p.extra()

	if err := p.err(); err != nil {
		return nil, err
	}
	return it, nil
}

// ParseComment parses one comment file. Comments have no id: the containing
// folder names the item they belong to, and the file name carries the timestamp
// and the author (section 11).
func ParseComment(path string, data []byte) (*Comment, error) {
	block, body, err := SplitFrontMatter(data)
	if err != nil {
		return nil, newParseError(path, 1, "", CodeFMMissing, strings.TrimPrefix(err.Error(), ErrInvalidFrontMatter.Error()+": "), nil)
	}
	node, err := decodeMappingNode(block)
	if err != nil {
		return nil, newParseError(path, 1, "", CodeFMYAML, strings.TrimPrefix(err.Error(), ErrInvalidFrontMatter.Error()+": "), nil)
	}
	fm := map[string]any{}
	if len(node.Content) > 0 {
		if err := node.Decode(&fm); err != nil {
			return nil, newParseError(path, 1, "", CodeFMYAML, err.Error(), nil)
		}
	}

	p := &fieldReader{path: path, fm: fm, lines: keyLines(node)}
	c := &Comment{Path: path, Body: body, Rev: ComputeRev(data)}

	if t := ItemType(p.str("type")); t != "" && t != TypeComment {
		p.fail("type", CodeFMType, fmt.Sprintf("want %q, got %q", TypeComment, t))
	}
	c.Item = ItemID(p.str("item"))
	if c.Item == "" {
		p.fail("item", CodeIDMissing, "missing")
	} else if !c.Item.Valid() {
		p.fail("item", CodeIDGrammar, fmt.Sprintf("%q does not match the id grammar", c.Item))
	}
	if folder := commentFolder(path); folder != "" && c.Item != "" && ItemID(folder) != c.Item {
		p.fail("item", CodeCommentMismatch, fmt.Sprintf("folder %q does not match item %q", folder, c.Item))
	}
	c.Author = p.str("author")
	c.Created = p.timestamp("created")
	c.Updated = p.timestamp("updated")
	c.InReplyTo = p.str("in_reply_to")
	c.Kind = CommentKind(p.str("kind"))
	if c.Kind != "" && !c.Kind.Valid() {
		p.fail("kind", CodeEnum, fmt.Sprintf("unknown comment kind %q", c.Kind))
	}
	c.Reactions = p.reactions("reactions")
	c.Attachments = p.strList("attachments")
	c.Extra = p.extra()

	if err := p.err(); err != nil {
		return nil, err
	}
	return c, nil
}

// commentFolder returns the item id folder of a comment path, or "".
func commentFolder(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2]
}

// SerializeItem renders an Item back to file bytes in the canonical form: keys in
// the order of docs/03 section 3.2, empty values omitted, unquoted ISO 8601
// dates, flow style for short scalar lists, one link per line, LF endings and a
// single trailing newline (R-FMT-5, R-FMT-6).
//
// Serializing a parsed file is idempotent: parsing the result and serializing it
// again produces the same bytes.
func SerializeItem(it *Item) ([]byte, error) {
	if it == nil {
		return nil, errors.New("serialize item: nil item")
	}
	w := &fmWriter{}
	w.scalar("id", string(it.ID))
	w.scalar("type", string(it.Type))
	w.scalar("title", it.Title)
	w.scalar("status", string(it.Status))
	w.scalar("priority", string(it.Priority))
	w.scalar("parent", string(it.Parent))
	w.scalar("epic", string(it.Epic))
	w.scalar("milestone", string(it.Milestone))
	w.scalar("sprint", it.Sprint)
	w.stringList("assignees", it.Assignees)
	w.scalar("author", it.Author)
	w.scalar("owner", it.Owner)
	w.stringList("labels", it.Labels)
	w.number("estimate", it.Estimate)
	w.number("effort", it.Effort)
	w.number("spent", it.Spent)
	w.timestamp("created", it.Created)
	w.timestamp("updated", it.Updated)
	w.timestamp("started", it.Started)
	w.timestamp("closed", it.Closed)
	w.date("start", it.Start)
	w.date("due", it.Due)
	w.links("links", it.Links)
	w.stringList("attachments", it.Attachments)
	if err := w.mapping("custom", it.Custom); err != nil {
		return nil, fmt.Errorf("serialize item %s: %w", it.Path, err)
	}
	if it.Deleted {
		w.raw("deleted", "true")
	}
	if err := w.extra(it.Extra); err != nil {
		return nil, fmt.Errorf("serialize item %s: %w", it.Path, err)
	}
	return assemble(w.String(), it.Body), nil
}

// SerializeComment renders a Comment back to file bytes in canonical form.
func SerializeComment(c *Comment) ([]byte, error) {
	if c == nil {
		return nil, errors.New("serialize comment: nil comment")
	}
	w := &fmWriter{}
	w.scalar("type", string(TypeComment))
	w.scalar("item", string(c.Item))
	w.scalar("author", c.Author)
	w.timestamp("created", c.Created)
	w.timestamp("updated", c.Updated)
	w.scalar("in_reply_to", c.InReplyTo)
	w.scalar("kind", string(c.Kind))
	if len(c.Reactions) > 0 {
		reactions := make(map[string]any, len(c.Reactions))
		for emoji, people := range c.Reactions {
			list := make([]any, 0, len(people))
			for _, h := range people {
				list = append(list, h)
			}
			reactions[emoji] = list
		}
		if err := w.mapping("reactions", reactions); err != nil {
			return nil, fmt.Errorf("serialize comment %s: %w", c.Path, err)
		}
	}
	w.stringList("attachments", c.Attachments)
	if err := w.extra(c.Extra); err != nil {
		return nil, fmt.Errorf("serialize comment %s: %w", c.Path, err)
	}
	return assemble(w.String(), c.Body), nil
}

// assemble wraps a rendered front-matter block and a body into file bytes.
func assemble(block, body string) []byte {
	var b strings.Builder
	b.WriteString(delimiter)
	b.WriteString("\n")
	b.WriteString(block)
	b.WriteString(delimiter)
	b.WriteString("\n")
	body = strings.Trim(body, "\n")
	if body != "" {
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// fieldReader reads typed values out of a decoded front-matter map, collecting
// every problem instead of stopping at the first one.
type fieldReader struct {
	path   string
	fm     map[string]any
	lines  map[string]int
	used   map[string]bool
	errors []error
}

func (p *fieldReader) mark(key string) {
	if p.used == nil {
		p.used = make(map[string]bool, len(p.fm))
	}
	p.used[key] = true
}

func (p *fieldReader) fail(field string, code Code, msg string) {
	p.errors = append(p.errors, newParseError(p.path, p.lines[field], field, code, msg, nil))
}

// err returns the collected problems joined into a single error, or nil.
func (p *fieldReader) err() error {
	if len(p.errors) == 0 {
		return nil
	}
	return errors.Join(p.errors...)
}

// value returns the raw value of a key and marks it as understood.
func (p *fieldReader) value(key string) (any, bool) {
	p.mark(key)
	v, ok := p.fm[key]
	if !ok || v == nil {
		return nil, false
	}
	return v, true
}

func (p *fieldReader) str(key string) string {
	v, ok := p.value(key)
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case int:
		return fmt.Sprintf("%d", t)
	case float64:
		return trimFloat(t)
	case bool:
		return fmt.Sprintf("%t", t)
	default:
		p.fail(key, CodeFieldType, fmt.Sprintf("want a string, got %T", v))
		return ""
	}
}

func (p *fieldReader) strList(key string) []string {
	v, ok := p.value(key)
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case string:
		// A human writing `labels: core` means a list of one.
		if strings.TrimSpace(t) == "" {
			return nil
		}
		return []string{strings.TrimSpace(t)}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			switch s := e.(type) {
			case string:
				out = append(out, strings.TrimSpace(s))
			case int:
				out = append(out, fmt.Sprintf("%d", s))
			case float64:
				out = append(out, trimFloat(s))
			default:
				p.fail(key, CodeFieldType, fmt.Sprintf("want a list of strings, got an element of type %T", e))
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		p.fail(key, CodeFieldType, fmt.Sprintf("want a list of strings, got %T", v))
		return nil
	}
}

func (p *fieldReader) number(key string) *float64 {
	v, ok := p.value(key)
	if !ok {
		return nil
	}
	f, err := toFloat(v)
	if err != nil {
		p.fail(key, CodeFieldType, fmt.Sprintf("want a number, got %T", v))
		return nil
	}
	return &f
}

func (p *fieldReader) boolean(key string) bool {
	v, ok := p.value(key)
	if !ok {
		return false
	}
	b, isBool := v.(bool)
	if !isBool {
		p.fail(key, CodeFieldType, fmt.Sprintf("want a boolean, got %T", v))
		return false
	}
	return b
}

func (p *fieldReader) timestamp(key string) Timestamp {
	v, ok := p.value(key)
	if !ok {
		return Timestamp{}
	}
	switch t := v.(type) {
	case time.Time:
		return NewTimestamp(t)
	case string:
		ts, err := ParseTimestamp(t)
		if err != nil {
			p.fail(key, CodeDateFormat, fmt.Sprintf("%q is not an ISO 8601 UTC timestamp (%s)", t, TimestampLayout))
			return Timestamp{}
		}
		return ts
	default:
		p.fail(key, CodeDateFormat, fmt.Sprintf("want a timestamp, got %T", v))
		return Timestamp{}
	}
}

func (p *fieldReader) date(key string) Date {
	v, ok := p.value(key)
	if !ok {
		return Date{}
	}
	switch t := v.(type) {
	case time.Time:
		return NewDate(t)
	case string:
		d, err := ParseDate(t)
		if err != nil {
			p.fail(key, CodeDateFormat, fmt.Sprintf("%q is not a date (%s)", t, DateLayout))
			return Date{}
		}
		return d
	default:
		p.fail(key, CodeDateFormat, fmt.Sprintf("want a date, got %T", v))
		return Date{}
	}
}

func (p *fieldReader) links(key string) []Link {
	v, ok := p.value(key)
	if !ok {
		return nil
	}
	list, isList := v.([]any)
	if !isList {
		p.fail(key, CodeFieldType, fmt.Sprintf("want a list of relations, got %T", v))
		return nil
	}
	out := make([]Link, 0, len(list))
	for _, e := range list {
		m, isMap := e.(map[string]any)
		if !isMap {
			p.fail(key, CodeFieldType, fmt.Sprintf("want {kind, target}, got %T", e))
			continue
		}
		l := Link{
			Kind:   LinkKind(stringOf(m["kind"])),
			Target: stringOf(m["target"]),
			Note:   stringOf(m["note"]),
		}
		if l.Kind == "" || l.Target == "" {
			p.fail(key, CodeFieldType, "a relation needs both kind and target")
			continue
		}
		if !l.Kind.Valid() {
			p.fail(key, CodeEnum, fmt.Sprintf("unknown relation kind %q", l.Kind))
			continue
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// aliasLinks reads the convenience shorthands `blocks:` and `depends_on:` and
// normalises them into links, which is how they are written back (section 12.2).
func (p *fieldReader) aliasLinks(key string, kind LinkKind) []Link {
	targets := p.strList(key)
	out := make([]Link, 0, len(targets))
	for _, t := range targets {
		out = append(out, Link{Kind: kind, Target: t})
	}
	return out
}

func (p *fieldReader) mapping(key string) map[string]any {
	v, ok := p.value(key)
	if !ok {
		return nil
	}
	m, isMap := v.(map[string]any)
	if !isMap {
		p.fail(key, CodeFieldType, fmt.Sprintf("want a mapping, got %T", v))
		return nil
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func (p *fieldReader) reactions(key string) map[string][]string {
	m := p.mapping(key)
	if m == nil {
		return nil
	}
	out := make(map[string][]string, len(m))
	for emoji, v := range m {
		switch t := v.(type) {
		case []any:
			people := make([]string, 0, len(t))
			for _, e := range t {
				people = append(people, stringOf(e))
			}
			out[emoji] = people
		case string:
			out[emoji] = []string{t}
		default:
			p.fail(key, CodeFieldType, fmt.Sprintf("want a list of handles for %q, got %T", emoji, v))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// extra returns every key this version does not understand, so that a file
// written by a newer binary survives a round trip untouched (R-CF-4).
func (p *fieldReader) extra() map[string]any {
	var out map[string]any
	for k, v := range p.fm {
		if knownKeys[k] || v == nil {
			continue
		}
		if out == nil {
			out = make(map[string]any)
		}
		out[k] = v
	}
	return out
}

// stringOf renders a scalar decoded from YAML as a string.
func stringOf(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case int:
		return fmt.Sprintf("%d", t)
	case float64:
		return trimFloat(t)
	case bool:
		return fmt.Sprintf("%t", t)
	case time.Time:
		return NewTimestamp(t).String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// toFloat coerces the numeric shapes yaml.v3 produces.
func toFloat(v any) (float64, error) {
	switch t := v.(type) {
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case float64:
		return t, nil
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(t), "%g", &f); err != nil {
			return 0, fmt.Errorf("parse number %q: %w", t, err)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("want a number, got %T", v)
	}
}

// trimFloat renders a float without a trailing ".0", so an estimate of 8 is
// written as `8` and never as `8.0`.
func trimFloat(f float64) string {
	s := fmt.Sprintf("%g", f)
	return s
}
