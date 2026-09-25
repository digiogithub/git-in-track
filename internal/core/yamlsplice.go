package core

import (
	"bytes"
	"reflect"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// spliceYAMLPath is the byte-level fast path of setYAMLPath (GIT-US-0153). It
// edits the original bytes rather than re-encoding the node tree, so a counter
// or redirect update leaves every other byte of project.yaml as the author
// wrote it: alignment, blank lines, folded scalars and flow mappings alike.
//
// A full re-encode is not only cosmetic damage. yaml.v3 re-emits what it
// parsed, so a hand-written flow entry such as
// `{ name: core, description: Shared Go core (model, parser, index) }` - whose
// unquoted commas already split the description into extra keys - came back as
// `description: Shared Go core (model, parser: ”, index): ”`, turning a latent
// ambiguity into visible corruption on the next id allocation.
//
// Two shapes are handled: the leaf exists as a single-line plain scalar (its
// token is replaced), or the path ends in block mappings whose deepest existing
// one gets the missing keys as new lines after its last entry. Anything else,
// and any edit whose result does not decode to exactly the original document
// with that one value set, returns ok=false so the caller falls back to the
// node re-encode.
func spliceYAMLPath(data []byte, doc *yaml.Node, keys []string, value string) (out []byte, ok bool) {
	root := documentMapping(doc)
	if root == nil || isFlow(root) || len(root.Content) == 0 {
		return nil, false
	}
	rendered, ok := plainScalar(value)
	if !ok {
		return nil, false
	}
	parent := root
	depth := 0
	for ; depth < len(keys)-1; depth++ {
		next, found := yamlMapGet(parent, keys[depth])
		if !found {
			break
		}
		if next.Kind != yaml.MappingNode || isFlow(next) || len(next.Content) == 0 {
			return nil, false
		}
		parent = next
	}
	if depth == len(keys)-1 {
		if existing, found := yamlMapGet(parent, keys[depth]); found {
			out, ok = replaceScalar(data, existing, rendered)
			if !ok || !sameExceptPath(data, out, keys, rendered) {
				return nil, false
			}
			return out, true
		}
	}
	out, ok = insertEntries(data, parent, keys[depth:], rendered)
	if !ok || !sameExceptPath(data, out, keys, rendered) {
		return nil, false
	}
	return out, true
}

// isFlow reports whether a collection node is written in flow style.
func isFlow(n *yaml.Node) bool { return n.Style&yaml.FlowStyle != 0 }

// plainScalar renders a value the way the encoder would, and reports whether
// that rendering is a single-line plain scalar that is safe to splice in.
func plainScalar(value string) (string, bool) {
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", false
	}
	b, err := yaml.Marshal(yamlScalar(value))
	if err != nil {
		return "", false
	}
	s := strings.TrimSuffix(string(b), "\n")
	if s != value {
		return "", false
	}
	return s, true
}

// replaceScalar overwrites the source token of a single-line plain scalar.
func replaceScalar(data []byte, n *yaml.Node, rendered string) ([]byte, bool) {
	if n.Kind != yaml.ScalarNode || n.Style != 0 || n.Value == "" || strings.ContainsAny(n.Value, "\r\n") {
		return nil, false
	}
	start, ok := offsetOf(data, n.Line, n.Column)
	if !ok || !bytes.HasPrefix(data[start:], []byte(n.Value)) {
		return nil, false
	}
	end := start + len(n.Value)
	if end < len(data) && !strings.ContainsRune(" \t\r\n#", rune(data[end])) {
		return nil, false
	}
	out := make([]byte, 0, len(data)-len(n.Value)+len(rendered))
	out = append(out, data[:start]...)
	out = append(out, rendered...)
	return append(out, data[end:]...), true
}

// insertEntries adds the missing keys of a path under a block mapping, as new
// lines after the mapping's last entry, at its indentation plus two spaces per
// level of nesting.
func insertEntries(data []byte, m *yaml.Node, keys []string, rendered string) ([]byte, bool) {
	last := lastLine(m)
	if last == 0 {
		return nil, false
	}
	lineStart, ok := offsetOf(data, last, 1)
	if !ok {
		return nil, false
	}
	eol := "\n"
	lineEnd := len(data)
	if i := bytes.IndexByte(data[lineStart:], '\n'); i >= 0 {
		lineEnd = lineStart + i + 1
		if i > 0 && data[lineStart+i-1] == '\r' {
			eol = "\r\n"
		}
	}
	indent := m.Content[0].Column - 1
	var add strings.Builder
	for i, k := range keys {
		renderedKey, ok := plainScalar(k)
		if !ok {
			return nil, false
		}
		add.WriteString(strings.Repeat(" ", indent+2*i))
		add.WriteString(renderedKey)
		add.WriteString(":")
		if i == len(keys)-1 {
			add.WriteString(" ")
			add.WriteString(rendered)
		}
		add.WriteString(eol)
	}
	out := make([]byte, 0, len(data)+add.Len()+len(eol))
	out = append(out, data[:lineEnd]...)
	if lineEnd == len(data) && (len(data) == 0 || data[len(data)-1] != '\n') {
		out = append(out, eol...)
	}
	out = append(out, add.String()...)
	return append(out, data[lineEnd:]...), true
}

// lastLine returns the source line a node ends on, or 0 when that cannot be
// told from the node tree: a block scalar, a multi-line scalar, or a flow
// collection spread over several lines.
func lastLine(n *yaml.Node) int {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || strings.ContainsAny(n.Value, "\r\n") {
			return 0
		}
		return n.Line
	case yaml.AliasNode:
		return n.Line
	case yaml.MappingNode, yaml.SequenceNode:
		if len(n.Content) == 0 {
			if isFlow(n) {
				return n.Line
			}
			return 0
		}
		end := lastLine(n.Content[len(n.Content)-1])
		if isFlow(n) && end != n.Line {
			return 0
		}
		return end
	}
	return 0
}

// offsetOf converts a 1-based yaml.v3 line and column (counted in characters)
// into a byte offset of data.
func offsetOf(data []byte, line, column int) (int, bool) {
	if line < 1 || column < 1 {
		return 0, false
	}
	off := 0
	for l := 1; l < line; l++ {
		i := bytes.IndexByte(data[off:], '\n')
		if i < 0 {
			return 0, false
		}
		off += i + 1
	}
	for c := 1; c < column; c++ {
		if off >= len(data) || data[off] == '\n' {
			return 0, false
		}
		_, size := utf8.DecodeRune(data[off:])
		off += size
	}
	return off, true
}

// sameExceptPath is the guard that keeps a byte edit honest: the spliced
// document must decode to exactly the original document with the value set at
// the key path, and to nothing else.
func sameExceptPath(before, after []byte, keys []string, rendered string) bool {
	var want, got map[string]any
	if yaml.Unmarshal(before, &want) != nil || yaml.Unmarshal(after, &got) != nil || want == nil {
		return false
	}
	var leaf any
	if yaml.Unmarshal([]byte(rendered), &leaf) != nil {
		return false
	}
	m := want
	for _, k := range keys[:len(keys)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			if m[k] != nil {
				return false
			}
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[keys[len(keys)-1]] = leaf
	return reflect.DeepEqual(want, got)
}
