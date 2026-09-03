package core

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// flowWidth is the width above which a scalar list is written as a block
// sequence instead of the compact [a, b] flow form.
const flowWidth = 80

// fmWriter renders a front-matter block. It writes YAML by hand rather than
// through yaml.Marshal because the canonical form is stricter than the encoder's
// defaults: a fixed key order, unquoted ISO 8601 dates (R-TIME-4), flow style for
// short lists and one relation per line.
type fmWriter struct {
	b strings.Builder
}

// String returns the rendered block, which always ends with a newline when it is
// not empty.
func (w *fmWriter) String() string { return w.b.String() }

// raw writes an already rendered value.
func (w *fmWriter) raw(key, rendered string) {
	w.b.WriteString(key)
	w.b.WriteString(": ")
	w.b.WriteString(rendered)
	w.b.WriteString("\n")
}

// scalar writes a string value, omitting the key when the value is empty
// (docs/03 section 3.2: empty values are omitted, never written as null).
func (w *fmWriter) scalar(key, value string) {
	if value == "" {
		return
	}
	w.raw(key, yamlString(value))
}

// number writes an optional numeric value.
func (w *fmWriter) number(key string, value *float64) {
	if value == nil {
		return
	}
	w.raw(key, trimFloat(*value))
}

// timestamp writes an unquoted ISO 8601 timestamp.
func (w *fmWriter) timestamp(key string, ts Timestamp) {
	if ts.IsZero() {
		return
	}
	w.raw(key, ts.String())
}

// date writes an unquoted ISO 8601 date.
func (w *fmWriter) date(key string, d Date) {
	if d.IsZero() {
		return
	}
	w.raw(key, d.String())
}

// stringList writes a list of scalars, compact when it fits on one line.
func (w *fmWriter) stringList(key string, items []string) {
	if len(items) == 0 {
		return
	}
	rendered := make([]string, 0, len(items))
	for _, s := range items {
		rendered = append(rendered, yamlFlowString(s))
	}
	flow := "[" + strings.Join(rendered, ", ") + "]"
	if len(key)+2+len(flow) <= flowWidth {
		w.raw(key, flow)
		return
	}
	w.b.WriteString(key)
	w.b.WriteString(":\n")
	for _, r := range rendered {
		w.b.WriteString("  - ")
		w.b.WriteString(r)
		w.b.WriteString("\n")
	}
}

// links writes the typed relations, one per line, as flow mappings so that a
// change to one relation is a one-line diff.
func (w *fmWriter) links(key string, links []Link) {
	if len(links) == 0 {
		return
	}
	w.b.WriteString(key)
	w.b.WriteString(":\n")
	for _, l := range links {
		parts := []string{
			"kind: " + yamlFlowString(string(l.Kind)),
			"target: " + yamlFlowString(l.Target),
		}
		if l.Note != "" {
			parts = append(parts, "note: "+yamlFlowString(l.Note))
		}
		w.b.WriteString("  - { ")
		w.b.WriteString(strings.Join(parts, ", "))
		w.b.WriteString(" }\n")
	}
}

// mapping writes a nested mapping such as custom:, with its keys sorted.
func (w *fmWriter) mapping(key string, m map[string]any) error {
	if len(m) == 0 {
		return nil
	}
	w.b.WriteString(key)
	w.b.WriteString(":\n")
	return w.writeMapBody(2, m)
}

// extra writes the keys this version does not understand, sorted
// lexicographically after every known key (docs/03 section 3.2).
func (w *fmWriter) extra(m map[string]any) error {
	if len(m) == 0 {
		return nil
	}
	for _, k := range sortedKeys(m) {
		if err := w.writeKeyValue(0, k, m[k]); err != nil {
			return err
		}
	}
	return nil
}

// writeMapBody writes the entries of a mapping at the given indentation.
func (w *fmWriter) writeMapBody(indent int, m map[string]any) error {
	for _, k := range sortedKeys(m) {
		if err := w.writeKeyValue(indent, k, m[k]); err != nil {
			return err
		}
	}
	return nil
}

// writeKeyValue writes one entry of arbitrary shape.
func (w *fmWriter) writeKeyValue(indent int, key string, v any) error {
	pad := strings.Repeat(" ", indent)
	switch t := v.(type) {
	case nil:
		return nil
	case map[string]any:
		if len(t) == 0 {
			return nil
		}
		w.b.WriteString(pad + yamlString(key) + ":\n")
		return w.writeMapBody(indent+2, t)
	case []any:
		return w.writeList(indent, key, t)
	default:
		if lit, ok := scalarLiteral(v); ok {
			w.b.WriteString(pad + yamlString(key) + ": " + lit + "\n")
			return nil
		}
		return w.writeFallback(indent, key, v)
	}
}

// writeList writes a sequence, compact when every element is a short scalar.
func (w *fmWriter) writeList(indent int, key string, list []any) error {
	if len(list) == 0 {
		return nil
	}
	pad := strings.Repeat(" ", indent)
	rendered := make([]string, 0, len(list))
	allScalar := true
	for _, e := range list {
		lit, ok := scalarLiteral(e)
		if !ok {
			allScalar = false
			break
		}
		rendered = append(rendered, lit)
	}
	if allScalar {
		flow := "[" + strings.Join(rendered, ", ") + "]"
		if indent+len(key)+2+len(flow) <= flowWidth {
			w.b.WriteString(pad + yamlString(key) + ": " + flow + "\n")
			return nil
		}
		w.b.WriteString(pad + yamlString(key) + ":\n")
		for _, r := range rendered {
			w.b.WriteString(pad + "  - " + r + "\n")
		}
		return nil
	}
	return w.writeFallback(indent, key, list)
}

// writeFallback renders a value the hand-written emitter does not model with the
// YAML encoder, then indents the result into place. It keeps unknown structures
// intact instead of dropping them.
func (w *fmWriter) writeFallback(indent int, key string, v any) error {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(map[string]any{key: v}); err != nil {
		return fmt.Errorf("encode field %q: %w", key, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode field %q: %w", key, err)
	}
	pad := strings.Repeat(" ", indent)
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			w.b.WriteString("\n")
			continue
		}
		w.b.WriteString(pad + line + "\n")
	}
	return nil
}

// sortedKeys returns the keys of a mapping in lexicographic order, which is what
// makes the output stable across runs and platforms.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// scalarLiteral renders the scalar shapes a decoded YAML document can hold.
// It reports false for composite values.
func scalarLiteral(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return yamlFlowString(t), true
	case bool:
		return strconv.FormatBool(t), true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case uint64:
		return strconv.FormatUint(t, 10), true
	case float64:
		return trimFloat(t), true
	case time.Time:
		// A value that is exactly midnight UTC came from a date-only scalar in
		// every document this tool writes, so it is rendered back as a date.
		u := t.UTC()
		if u.Hour() == 0 && u.Minute() == 0 && u.Second() == 0 && u.Nanosecond() == 0 {
			return NewDate(u).String(), true
		}
		return NewTimestamp(u).String(), true
	default:
		return "", false
	}
}

// yamlString renders a string in block context: plain when that round-trips as a
// string, double-quoted otherwise.
func yamlString(s string) string {
	if plainSafe(s, false) {
		return s
	}
	return strconv.Quote(s)
}

// yamlFlowString renders a string that may end up inside a [] or {} collection,
// where the separators are additionally forbidden in a plain scalar.
func yamlFlowString(s string) string {
	if plainSafe(s, true) {
		return s
	}
	return strconv.Quote(s)
}

var (
	yamlNullRE = regexp.MustCompile(`^(~|null|Null|NULL)$`)
	yamlBoolRE = regexp.MustCompile(`^(?i:y|n|yes|no|true|false|on|off)$`)
	yamlNumRE  = regexp.MustCompile(`^[-+]?(` +
		`0b[01_]+|0o?[0-7_]+|0x[0-9a-fA-F_]+|` +
		`[0-9][0-9_]*(\.[0-9_]*)?([eE][-+]?[0-9]+)?|` +
		`\.[0-9_]+([eE][-+]?[0-9]+)?|` +
		`\.(?i:inf|nan)` +
		`)$`)
	yamlTimeRE = regexp.MustCompile(`^\d{4}-\d{1,2}-\d{1,2}([Tt ].*)?$`)
)

// plainIndicators are the characters that cannot open a plain YAML scalar.
const plainIndicators = "-?:,[]{}#&*!|>'\"%@`"

// plainSafe reports whether s can be written as a plain (unquoted) YAML scalar
// that reads back as exactly the same string. It is deliberately conservative:
// quoting one string too many costs a pair of quotes, guessing wrong costs data.
func plainSafe(s string, flow bool) bool {
	if s == "" {
		return false
	}
	if s != strings.TrimSpace(s) {
		return false
	}
	if strings.ContainsAny(s, "\n\r\t") {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	if first, _ := utf8.DecodeRuneInString(s); strings.ContainsRune(plainIndicators, first) {
		return false
	}
	if strings.Contains(s, ": ") || strings.HasSuffix(s, ":") || strings.Contains(s, " #") {
		return false
	}
	if flow && strings.ContainsAny(s, ",[]{}") {
		return false
	}
	if yamlNullRE.MatchString(s) || yamlBoolRE.MatchString(s) ||
		yamlNumRE.MatchString(s) || yamlTimeRE.MatchString(s) {
		return false
	}
	return true
}
