package core

import (
	"bytes"
	"reflect"
	"slices"
	"sort"
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

// SpliceYAMLBlock sets the value at a key path of a YAML document to a node,
// editing the original bytes: only the lines of that key's block are rendered
// again, and every other byte stays as the author wrote it (GIT-US-0154). It
// is the block-level sibling of spliceYAMLPath, for writers that own a whole
// section of project.yaml, such as the `integrations.youtrack` block.
//
// An existing key is replaced from its own line to the end of its block,
// comment lines indented under it included; a missing key is added after the
// last entry of its deepest existing parent mapping, or at the end of the file
// when not even the top-level key exists. Only the added or replaced block is
// encoded, with the two-space indentation the project files use.
//
// The same guard as spliceYAMLPath keeps the edit honest: the result must
// decode to exactly the original document with the value set at the path, and
// carry exactly the comments that document would carry. Any other shape, and
// any result that fails the guard, returns ok=false so the caller falls back
// to re-encoding its node tree.
func SpliceYAMLBlock(data []byte, keys []string, value *yaml.Node) (out []byte, ok bool) {
	if len(keys) == 0 || value == nil {
		return nil, false
	}
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil {
		return nil, false
	}
	root := documentMapping(&doc)
	if root == nil || isFlow(root) {
		return nil, false
	}
	eol := "\n"
	if i := bytes.IndexByte(data, '\n'); i > 0 && data[i-1] == '\r' {
		eol = "\r\n"
	}
	parent := root
	for depth, key := range keys {
		idx := yamlMapIndex(parent, key)
		if idx < 0 {
			out, ok = insertBlock(data, parent, parent == root, keys[depth:], value, eol)
			break
		}
		if depth == len(keys)-1 {
			out, ok = replaceBlock(data, parent.Content[idx], parent.Content[idx+1], value, eol)
			break
		}
		next := parent.Content[idx+1]
		if next.Kind != yaml.MappingNode || isFlow(next) || len(next.Content) == 0 {
			return nil, false
		}
		parent = next
	}
	if !ok || !blockSpliceHolds(data, out, keys, value) {
		return nil, false
	}
	return out, true
}

// yamlMapIndex returns the index of a key in a mapping node's content, or -1.
func yamlMapIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// replaceBlock renders `key: value` over the lines an existing entry spans.
func replaceBlock(data []byte, key, old, value *yaml.Node, eol string) ([]byte, bool) {
	end := lastLine(old)
	if key.Kind != yaml.ScalarNode || key.Line < 1 || end < key.Line {
		return nil, false
	}
	start, ok := offsetOf(data, key.Line, 1)
	if !ok {
		return nil, false
	}
	indent := key.Column - 1
	// The key must open its line: an entry written after a `- ` or inside a
	// flow collection has no lines of its own to replace.
	if len(bytes.TrimLeft(data[start:start+indent], " ")) != 0 {
		return nil, false
	}
	lines := bytes.SplitAfter(data[start:], []byte("\n"))
	span := end - key.Line + 1
	if span > len(lines) {
		return nil, false
	}
	// Comment lines indented under the key belong to its block, and yaml.v3
	// hands them to the node tree, which renders them again.
	for i := span; i < len(lines); i++ {
		trimmed := bytes.TrimLeft(lines[i], " ")
		if len(bytes.TrimSpace(trimmed)) == 0 {
			continue
		}
		if trimmed[0] != '#' || len(lines[i])-len(trimmed) <= indent {
			break
		}
		span = i + 1
	}
	stop := start
	for _, l := range lines[:span] {
		stop += len(l)
	}
	head := &yaml.Node{Kind: yaml.ScalarNode, Tag: key.Tag, Style: key.Style, Value: key.Value, LineComment: key.LineComment}
	block, ok := renderBlock([]string{key.Value}, head, value, indent, eol)
	if !ok {
		return nil, false
	}
	out := make([]byte, 0, len(data)+len(block))
	out = append(out, data[:start]...)
	out = append(out, block...)
	if stop < len(data) && !bytes.HasSuffix(block, []byte("\n")) {
		out = append(out, eol...)
	}
	return append(out, data[stop:]...), true
}

// insertBlock renders the missing keys of a path, nested, holding the value:
// after the last entry of their parent mapping, or at the end of the file for
// a top-level key.
func insertBlock(data []byte, parent *yaml.Node, top bool, keys []string, value *yaml.Node, eol string) ([]byte, bool) {
	at, indent := len(data), 0
	if !top {
		last := lastLine(parent)
		if last == 0 {
			return nil, false
		}
		lineStart, ok := offsetOf(data, last, 1)
		if !ok {
			return nil, false
		}
		at = len(data)
		if i := bytes.IndexByte(data[lineStart:], '\n'); i >= 0 {
			at = lineStart + i + 1
		}
		indent = parent.Content[0].Column - 1
	}
	block, ok := renderBlock(keys, nil, value, indent, eol)
	if !ok {
		return nil, false
	}
	out := make([]byte, 0, len(data)+len(block)+len(eol))
	out = append(out, data[:at]...)
	if at > 0 && data[at-1] != '\n' {
		out = append(out, eol...)
	}
	out = append(out, block...)
	return append(out, data[at:]...), true
}

// renderBlock encodes `k1: {k2: ... value}` as block YAML indented by indent
// spaces. head, when set, is the node the first key is written with.
func renderBlock(keys []string, head, value *yaml.Node, indent int, eol string) ([]byte, bool) {
	node := value
	for i := len(keys) - 1; i >= 0; i-- {
		key := yamlScalar(keys[i])
		if i == 0 && head != nil {
			key = head
		}
		node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{key, node}}
	}
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if enc.Encode(node) != nil || enc.Close() != nil {
		return nil, false
	}
	pad := strings.Repeat(" ", indent)
	var out strings.Builder
	for _, line := range strings.SplitAfter(b.String(), "\n") {
		if line == "" {
			continue
		}
		body := strings.TrimSuffix(line, "\n")
		if body != "" {
			out.WriteString(pad)
			out.WriteString(body)
		}
		if strings.HasSuffix(line, "\n") {
			out.WriteString(eol)
		}
	}
	return []byte(out.String()), true
}

// blockSpliceHolds is the guard of SpliceYAMLBlock: the spliced bytes decode to
// the original document with the value set at the path, and carry the comments
// of that document - none lost, none doubled.
func blockSpliceHolds(before, after []byte, keys []string, value *yaml.Node) bool {
	var want, got yaml.Node
	if yaml.Unmarshal(before, &want) != nil || yaml.Unmarshal(after, &got) != nil {
		return false
	}
	m := documentMapping(&want)
	if m == nil {
		return false
	}
	for _, k := range keys[:len(keys)-1] {
		next, ok := yamlMapGet(m, k)
		if !ok {
			next = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			m.Content = append(m.Content, yamlScalar(k), next)
		}
		if next.Kind != yaml.MappingNode {
			return false
		}
		m = next
	}
	yamlMapSet(m, keys[len(keys)-1], value)

	var wantValue, gotValue any
	if want.Decode(&wantValue) != nil || got.Decode(&gotValue) != nil {
		return false
	}
	if !reflect.DeepEqual(wantValue, gotValue) {
		return false
	}
	wantComments, gotComments := yamlComments(&want, nil), yamlComments(&got, nil)
	sort.Strings(wantComments)
	sort.Strings(gotComments)
	return slices.Equal(wantComments, gotComments)
}

// yamlComments lists every comment line of a node tree. Sorted, two lists
// compare the comments of two trees regardless of which node yaml.v3 hangs a
// comment on.
func yamlComments(n *yaml.Node, acc []string) []string {
	for _, c := range []string{n.HeadComment, n.LineComment, n.FootComment} {
		for _, line := range strings.Split(c, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				acc = append(acc, line)
			}
		}
	}
	for _, child := range n.Content {
		acc = yamlComments(child, acc)
	}
	return acc
}
