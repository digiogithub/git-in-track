package pando

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// TOON (Token-Oriented Object Notation) decoding.
//
// Pando renders every structured tool result through
// internal/llm/tools.NewStructuredResponse, which prefers TOON, falls back to
// TOML and only then to indented JSON. The result therefore reaches an MCP
// client as a *text* content block in a format that is neither JSON nor YAML,
// and a client that wants typed structs has to decode it.
//
// This is a decoder for the subset of TOON v4.1 that Pando's encoder can
// actually emit: object bodies, inline primitive arrays, list form, tabular
// form with nested field groups, and keyed tabular form. Comma is the only
// delimiter Pando configures, and it is the only one accepted here; a document
// whose header declares the tab or pipe delimiter is rejected rather than
// silently mis-split.
//
// Decoding produces the JSON data model (map[string]any, []any, string,
// float64, bool, nil) so that the typed structs in this package can be filled
// by a json round-trip and tolerate unknown fields.

// unquotedKeyRE is the TOON §7.3 unquoted-key pattern.
var unquotedKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// toonNumberRE is the TOON §4 decoder number grammar, before the
// forbidden-leading-zero check.
var toonNumberRE = regexp.MustCompile(`^-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?$`)

// toonIndentStep is the indentation width Pando's encoder uses. It is only a
// fallback: wherever the document itself reveals the child indentation, the
// document wins.
const toonIndentStep = 2

type toonLine struct {
	indent int
	text   string
	no     int
}

type toonParser struct {
	lines []toonLine
	pos   int
	// step is the document's indentation width, inferred from the smallest
	// positive indentation it uses. It is needed for list items, whose first
	// field is carried on the hyphen line and therefore stands one level
	// deeper than the hyphen itself (§10).
	step int
}

// toonField is one entry of a tabular header's field list. A node with children
// is a nested field group (§9.3).
type toonField struct {
	name     string
	children []toonField
}

// toonHeader is a parsed "key", "key[N]", "key[N]{a,b}" or "key[N:]{a,b}"
// opening, together with whatever followed the colon on the same line.
type toonHeader struct {
	key      string
	isArray  bool
	length   int
	keyed    bool
	fields   []toonField
	inline   string
	hasValue bool
}

// decodeStructured decodes a Pando tool result body. JSON is tried first
// because FormatStructuredData falls back to indented JSON, and TOON otherwise.
func decodeStructured(src string) (any, error) {
	trimmed := strings.TrimSpace(src)
	if strings.HasPrefix(trimmed, "{") {
		var v any
		if err := json.Unmarshal([]byte(trimmed), &v); err == nil {
			return v, nil
		}
	}
	return decodeTOON(src)
}

func decodeTOON(src string) (any, error) {
	p := newTOONParser(src)
	v, err := p.parseDocument()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.lines) {
		return nil, fmt.Errorf("pando: toon: line %d: unexpected content %q", p.lines[p.pos].no, p.lines[p.pos].text)
	}
	return v, nil
}

func newTOONParser(src string) *toonParser {
	src = strings.TrimPrefix(src, "\ufeff")
	raw := strings.Split(src, "\n")
	lines := make([]toonLine, 0, len(raw))
	for i, text := range raw {
		text = strings.TrimSuffix(text, "\r")
		text = strings.TrimRight(text, " \t")
		indent := 0
		j := 0
		for j < len(text) {
			switch text[j] {
			case ' ':
				indent++
			case '\t':
				// Non-strict mode: one leading tab is one indentation level.
				indent += toonIndentStep
			default:
				goto done
			}
			j++
		}
	done:
		body := text[j:]
		if body == "" {
			continue
		}
		if body[0] == '#' { // §5.1 comment line
			continue
		}
		lines = append(lines, toonLine{indent: indent, text: body, no: i + 1})
	}
	step := 0
	for _, ln := range lines {
		if ln.indent > 0 && (step == 0 || ln.indent < step) {
			step = ln.indent
		}
	}
	if step == 0 {
		step = toonIndentStep
	}
	return &toonParser{lines: lines, step: step}
}

func (p *toonParser) parseDocument() (any, error) {
	if len(p.lines) == 0 {
		return map[string]any{}, nil
	}
	first := p.lines[0]
	if first.text == "[]" && len(p.lines) == 1 {
		p.pos = 1
		return []any{}, nil
	}
	if h, err := parseTOONHeader(first.text); err == nil {
		p.pos++
		v, err := p.headerValue(h, first.indent)
		if err != nil {
			return nil, err
		}
		if h.key == "" && h.isArray {
			return v, nil
		}
		obj := map[string]any{h.key: v}
		rest, err := p.parseObjectInto(obj, first.indent)
		if err != nil {
			return nil, err
		}
		return rest, nil
	}
	if len(p.lines) == 1 {
		p.pos = 1
		return parseTOONScalar(first.text)
	}
	return nil, fmt.Errorf("pando: toon: line %d: cannot parse %q as a document", first.no, first.text)
}

// parseObject reads consecutive field lines at indent into a fresh object.
func (p *toonParser) parseObject(indent int) (map[string]any, error) {
	return p.parseObjectInto(map[string]any{}, indent)
}

func (p *toonParser) parseObjectInto(obj map[string]any, indent int) (map[string]any, error) {
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.indent != indent {
			if ln.indent < indent {
				break
			}
			return nil, fmt.Errorf("pando: toon: line %d: unexpected indentation", ln.no)
		}
		if isTOONListItem(ln.text) {
			break
		}
		h, err := parseTOONHeader(ln.text)
		if err != nil {
			return nil, fmt.Errorf("pando: toon: line %d: %w", ln.no, err)
		}
		p.pos++
		v, err := p.headerValue(h, indent)
		if err != nil {
			return nil, err
		}
		// Duplicate keys are last-write-wins (§14).
		obj[h.key] = v
	}
	return obj, nil
}

// headerValue resolves the value a header introduces. indent is the
// indentation of the header's own line.
func (p *toonParser) headerValue(h *toonHeader, indent int) (any, error) {
	if !h.isArray {
		if !h.hasValue {
			child := p.childIndent(indent)
			if child < 0 {
				return map[string]any{}, nil
			}
			return p.parseObject(child)
		}
		if h.inline == "[]" {
			return []any{}, nil
		}
		return parseTOONScalar(h.inline)
	}

	if len(h.fields) > 0 {
		child := p.childIndent(indent)
		if child < 0 {
			if h.length == 0 {
				if h.keyed {
					return map[string]any{}, nil
				}
				return []any{}, nil
			}
			return nil, fmt.Errorf("pando: toon: array of %d declared with no rows", h.length)
		}
		if h.keyed {
			return p.parseKeyedTabular(h, child)
		}
		return p.parseTabular(h, child)
	}

	if h.hasValue {
		cells, err := splitTOONCells(h.inline)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(cells))
		for _, c := range cells {
			v, err := parseTOONScalar(strings.TrimSpace(c))
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}

	child := p.childIndent(indent)
	if child < 0 {
		return []any{}, nil
	}
	return p.parseListItems(child)
}

// childIndent reports the indentation of the block nested under the line just
// consumed, or -1 when there is none.
func (p *toonParser) childIndent(indent int) int {
	if p.pos >= len(p.lines) {
		return -1
	}
	if p.lines[p.pos].indent <= indent {
		return -1
	}
	return p.lines[p.pos].indent
}

func (p *toonParser) parseTabular(h *toonHeader, indent int) (any, error) {
	rows := make([]any, 0, h.length)
	for len(rows) < h.length {
		if p.pos >= len(p.lines) || p.lines[p.pos].indent != indent {
			return nil, fmt.Errorf("pando: toon: tabular array declared %d rows, found %d", h.length, len(rows))
		}
		ln := p.lines[p.pos]
		p.pos++
		row, err := rowObject(h.fields, ln.text)
		if err != nil {
			return nil, fmt.Errorf("pando: toon: line %d: %w", ln.no, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (p *toonParser) parseKeyedTabular(h *toonHeader, indent int) (any, error) {
	out := map[string]any{}
	for i := 0; i < h.length; i++ {
		if p.pos >= len(p.lines) || p.lines[p.pos].indent != indent {
			return nil, fmt.Errorf("pando: toon: keyed array declared %d entries, found %d", h.length, i)
		}
		ln := p.lines[p.pos]
		p.pos++
		key, rest, err := splitTOONEntryKey(ln.text)
		if err != nil {
			return nil, fmt.Errorf("pando: toon: line %d: %w", ln.no, err)
		}
		row, err := rowObject(h.fields, rest)
		if err != nil {
			return nil, fmt.Errorf("pando: toon: line %d: %w", ln.no, err)
		}
		out[key] = row
	}
	return out, nil
}

func (p *toonParser) parseListItems(indent int) (any, error) {
	items := []any{}
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.indent != indent || !isTOONListItem(ln.text) {
			break
		}
		rest := ""
		if len(ln.text) > 1 {
			rest = ln.text[2:]
		}
		// The first field of an object item, or a nested array header, stands
		// one level deeper than the hyphen that carries it (§10).
		content := indent + p.step
		if rest == "" {
			p.pos++
			items = append(items, map[string]any{})
			continue
		}
		if h, err := parseTOONHeader(rest); err == nil {
			// Rewrite the hyphen line as a plain line at the content
			// indentation so the object body reads uniformly.
			p.lines[p.pos] = toonLine{indent: content, text: rest, no: ln.no}
			if h.key == "" && h.isArray {
				p.pos++
				v, err := p.headerValue(h, content)
				if err != nil {
					return nil, err
				}
				items = append(items, v)
				continue
			}
			obj, err := p.parseObject(content)
			if err != nil {
				return nil, err
			}
			items = append(items, obj)
			continue
		}
		p.pos++
		v, err := parseTOONScalar(rest)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, nil
}

func isTOONListItem(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

// rowObject maps one tabular row onto the header's field tree, depth first.
func rowObject(fields []toonField, row string) (map[string]any, error) {
	cells, err := splitTOONCells(row)
	if err != nil {
		return nil, err
	}
	i := 0
	obj, err := buildTOONRow(fields, cells, &i)
	if err != nil {
		return nil, err
	}
	if i != len(cells) {
		return nil, fmt.Errorf("row has %d cells, header declares %d", len(cells), i)
	}
	return obj, nil
}

func buildTOONRow(fields []toonField, cells []string, i *int) (map[string]any, error) {
	obj := make(map[string]any, len(fields))
	for _, f := range fields {
		if f.children == nil {
			if *i >= len(cells) {
				return nil, fmt.Errorf("row is missing a cell for field %q", f.name)
			}
			v, err := parseTOONScalar(strings.TrimSpace(cells[*i]))
			if err != nil {
				return nil, err
			}
			*i++
			obj[f.name] = v
			continue
		}
		sub, err := buildTOONRow(f.children, cells, i)
		if err != nil {
			return nil, err
		}
		obj[f.name] = sub
	}
	return obj, nil
}

// parseTOONHeader parses the part of a line before (and including) the colon
// that opens a value. It fails when the line is not a field line at all, which
// is how a list item is told apart from a primitive.
func parseTOONHeader(text string) (*toonHeader, error) {
	h := &toonHeader{}
	i := 0
	if i < len(text) && text[i] == '"' {
		key, n, err := scanTOONQuoted(text[i:])
		if err != nil {
			return nil, err
		}
		h.key = key
		i += n
	} else {
		start := i
		for i < len(text) && text[i] != ':' && text[i] != '[' {
			i++
		}
		h.key = text[start:i]
		if h.key != "" && !unquotedKeyRE.MatchString(h.key) {
			return nil, fmt.Errorf("invalid key %q", h.key)
		}
	}
	if i < len(text) && text[i] == '[' {
		end := strings.IndexByte(text[i:], ']')
		if end < 0 {
			return nil, fmt.Errorf("unterminated array header")
		}
		inner := text[i+1 : i+end]
		i += end + 1
		h.isArray = true
		digits := 0
		for digits < len(inner) && inner[digits] >= '0' && inner[digits] <= '9' {
			digits++
		}
		if digits == 0 {
			return nil, fmt.Errorf("array header %q has no length", inner)
		}
		n, err := strconv.Atoi(inner[:digits])
		if err != nil {
			return nil, fmt.Errorf("array header %q has no length", inner)
		}
		h.length = n
		rest := inner[digits:]
		if strings.HasPrefix(rest, ":") {
			h.keyed = true
			rest = rest[1:]
		}
		if rest != "" {
			// A non-empty delimiter symbol means tab or pipe, which this
			// decoder does not accept (Pando always configures comma).
			return nil, fmt.Errorf("unsupported TOON delimiter %q", rest)
		}
	}
	if i < len(text) && text[i] == '{' {
		end, err := matchTOONBrace(text[i:])
		if err != nil {
			return nil, err
		}
		fields, err := parseTOONFieldList(text[i+1 : i+end])
		if err != nil {
			return nil, err
		}
		h.fields = fields
		i += end + 1
	}
	if i >= len(text) || text[i] != ':' {
		return nil, fmt.Errorf("not a field line")
	}
	i++
	if i < len(text) {
		rest := strings.TrimLeft(text[i:], " ")
		if rest != "" {
			h.inline = rest
			h.hasValue = true
		}
	}
	if h.key == "" && !h.isArray {
		return nil, fmt.Errorf("empty key")
	}
	return h, nil
}

func parseTOONFieldList(s string) ([]toonField, error) {
	parts, err := splitTOONTopLevel(s)
	if err != nil {
		return nil, err
	}
	out := make([]toonField, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty field name in header")
		}
		name := part
		var children []toonField
		if idx := strings.IndexByte(part, '{'); idx >= 0 && !strings.HasPrefix(part, `"`) {
			end, err := matchTOONBrace(part[idx:])
			if err != nil {
				return nil, err
			}
			children, err = parseTOONFieldList(part[idx+1 : idx+end])
			if err != nil {
				return nil, err
			}
			if children == nil {
				children = []toonField{}
			}
			name = part[:idx]
		} else if strings.HasPrefix(part, `"`) {
			key, n, err := scanTOONQuoted(part)
			if err != nil {
				return nil, err
			}
			name = key
			if rest := part[n:]; rest != "" {
				if !strings.HasPrefix(rest, "{") {
					return nil, fmt.Errorf("unexpected %q after quoted field name", rest)
				}
				end, err := matchTOONBrace(rest)
				if err != nil {
					return nil, err
				}
				children, err = parseTOONFieldList(rest[1:end])
				if err != nil {
					return nil, err
				}
				if children == nil {
					children = []toonField{}
				}
			}
			out = append(out, toonField{name: name, children: children})
			continue
		}
		out = append(out, toonField{name: name, children: children})
	}
	return out, nil
}

// matchTOONBrace returns the index of the '}' closing the '{' at s[0].
func matchTOONBrace(s string) (int, error) {
	depth := 0
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inQuote = false
			}
			continue
		}
		switch c {
		case '"':
			inQuote = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("unterminated field list")
}

// splitTOONTopLevel splits a field list body on commas that are neither inside
// quotes nor inside a nested field group.
func splitTOONTopLevel(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	var out []string
	depth := 0
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inQuote = false
			}
			continue
		}
		switch c {
		case '"':
			inQuote = true
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	if inQuote || depth != 0 {
		return nil, fmt.Errorf("unterminated field list")
	}
	return append(out, s[start:]), nil
}

// splitTOONCells splits a row or an inline array on commas outside quotes.
func splitTOONCells(s string) ([]string, error) {
	var out []string
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inQuote = false
			}
			continue
		}
		switch c {
		case '"':
			inQuote = true
		case ',':
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated string")
	}
	return append(out, s[start:]), nil
}

// splitTOONEntryKey splits "key: cells" of a keyed tabular entry.
func splitTOONEntryKey(text string) (key, rest string, err error) {
	if strings.HasPrefix(text, `"`) {
		key, n, err := scanTOONQuoted(text)
		if err != nil {
			return "", "", err
		}
		rest := text[n:]
		if !strings.HasPrefix(rest, ":") {
			return "", "", fmt.Errorf("keyed entry is missing its colon")
		}
		return key, strings.TrimLeft(rest[1:], " "), nil
	}
	idx := strings.IndexByte(text, ':')
	if idx < 0 {
		return "", "", fmt.Errorf("keyed entry is missing its colon")
	}
	key = text[:idx]
	if !unquotedKeyRE.MatchString(key) {
		return "", "", fmt.Errorf("invalid entry key %q", key)
	}
	return key, strings.TrimLeft(text[idx+1:], " "), nil
}

// scanTOONQuoted reads the quoted string at the start of s and returns its
// value and the number of bytes consumed.
func scanTOONQuoted(s string) (value string, n int, err error) {
	if s == "" || s[0] != '"' {
		return "", 0, fmt.Errorf("expected a quoted string")
	}
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			var v string
			if err := json.Unmarshal([]byte(s[:i+1]), &v); err != nil {
				return "", 0, fmt.Errorf("invalid quoted string: %w", err)
			}
			return v, i + 1, nil
		}
	}
	return "", 0, fmt.Errorf("unterminated string")
}

// parseTOONScalar resolves one token to null, a bool, a number or a string.
func parseTOONScalar(token string) (any, error) {
	if token == "" {
		return "", nil
	}
	if token[0] == '"' {
		v, n, err := scanTOONQuoted(token)
		if err != nil {
			return nil, err
		}
		if n != len(token) {
			return nil, fmt.Errorf("pando: toon: trailing content after quoted value")
		}
		return v, nil
	}
	switch token {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if f, ok := parseTOONNumber(token); ok {
		return f, nil
	}
	return token, nil
}

func parseTOONNumber(token string) (float64, bool) {
	if !toonNumberRE.MatchString(token) {
		return 0, false
	}
	digits := strings.TrimPrefix(token, "-")
	if len(digits) >= 2 && digits[0] == '0' && digits[1] >= '0' && digits[1] <= '9' {
		return 0, false
	}
	f, err := strconv.ParseFloat(token, 64)
	if err != nil {
		return 0, false
	}
	if f == 0 {
		return 0, true
	}
	return f, true
}
