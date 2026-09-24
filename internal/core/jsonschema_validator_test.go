package core

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// This file is a deliberately small JSON Schema evaluator: just the keywords
// the shipped schemas use, so that the drift tests can validate documents
// without a third-party validator. schemaKeywords is the closed list;
// TestJSONSchemaFilesAreWellFormed fails on any other keyword, so a schema can
// never lean on a keyword this evaluator would silently skip.

// schemaKeywords are the keywords the evaluator understands. Annotations
// (title, description, deprecated, format, default) carry no assertion here.
var schemaKeywords = map[string]bool{
	"$schema": true, "$id": true, "$ref": true, "$defs": true, "$comment": true,
	"title": true, "description": true, "deprecated": true, "format": true, "default": true,
	"type": true, "const": true, "enum": true, "pattern": true, "minLength": true, "maxLength": true,
	"minimum": true, "maximum": true, "required": true, "properties": true, "patternProperties": true,
	"additionalProperties": true, "propertyNames": true, "items": true, "minItems": true, "maxItems": true,
	"anyOf": true,
}

// schemaSet holds every shipped schema, parsed, keyed by file name.
type schemaSet struct {
	docs map[string]map[string]any
}

// loadSchemaSet parses every embedded schema.
func loadSchemaSet(t *testing.T) *schemaSet {
	t.Helper()
	s := &schemaSet{docs: map[string]map[string]any{}}
	for _, name := range JSONSchemaNames() {
		data, err := JSONSchema(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s is not a JSON object: %v", name, err)
		}
		s.docs[name] = doc
	}
	return s
}

// node is a schema position: the document it sits in (to resolve relative
// $refs) and the schema value itself, a bool or an object.
type node struct {
	doc    string
	schema any
}

// root returns the top-level schema of a document.
func (s *schemaSet) root(doc string) node { return node{doc: doc, schema: s.docs[doc]} }

// resolve follows one $ref: "#/$defs/x" within the document, or
// "other.json#/$defs/x" to a sibling file (both resolve against the same base).
func (s *schemaSet) resolve(from, ref string) (node, error) {
	file, pointer, _ := strings.Cut(ref, "#")
	if file == "" {
		file = from
	}
	doc, ok := s.docs[file]
	if !ok {
		return node{}, fmt.Errorf("$ref %q: no schema %q", ref, file)
	}
	var cur any = doc
	for _, part := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		if part == "" {
			continue
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return node{}, fmt.Errorf("$ref %q: %q is not an object", ref, part)
		}
		if cur, ok = m[part]; !ok {
			return node{}, fmt.Errorf("$ref %q: no %q", ref, part)
		}
	}
	return node{doc: file, schema: cur}, nil
}

// deref follows $ref chains until a schema without one (used by the structural
// tests that read properties rather than validate values).
func (s *schemaSet) deref(t *testing.T, n node) node {
	t.Helper()
	for i := 0; i < 16; i++ {
		m, ok := n.schema.(map[string]any)
		if !ok {
			return n
		}
		ref, ok := m["$ref"].(string)
		if !ok {
			return n
		}
		next, err := s.resolve(n.doc, ref)
		if err != nil {
			t.Fatal(err)
		}
		n = next
	}
	t.Fatalf("$ref chain too deep in %s", n.doc)
	return n
}

// child returns the sub-schema at a key of an object schema, dereferenced.
func (s *schemaSet) child(t *testing.T, n node, keys ...string) node {
	t.Helper()
	for _, k := range keys {
		n = s.deref(t, n)
		var v any
		switch m := n.schema.(type) {
		case map[string]any:
			found, ok := m[k]
			if !ok {
				t.Fatalf("%s: no %q", n.doc, k)
			}
			v = found
		case []any:
			i, err := strconv.Atoi(k)
			if err != nil || i < 0 || i >= len(m) {
				t.Fatalf("%s: no element %q", n.doc, k)
			}
			v = m[i]
		default:
			t.Fatalf("%s: %q is not reachable in a %T", n.doc, k, n.schema)
		}
		n = node{doc: n.doc, schema: v}
	}
	return s.deref(t, n)
}

// validate reports every violation of value against n, one line each.
func (s *schemaSet) validate(n node, value any, at string) []string {
	var errs []string
	s.check(n, value, at, &errs)
	return errs
}

func (s *schemaSet) check(n node, value any, at string, errs *[]string) {
	fail := func(format string, args ...any) {
		*errs = append(*errs, at+": "+fmt.Sprintf(format, args...))
	}
	switch sch := n.schema.(type) {
	case bool:
		if !sch {
			fail("not allowed")
		}
		return
	case map[string]any:
		if ref, ok := sch["$ref"].(string); ok {
			target, err := s.resolve(n.doc, ref)
			if err != nil {
				fail("%v", err)
				return
			}
			s.check(target, value, at, errs)
		}
		s.checkObjectSchema(n.doc, sch, value, at, errs, fail)
	default:
		fail("schema is %T, want an object or a boolean", n.schema)
	}
}

func (s *schemaSet) checkObjectSchema(doc string, sch map[string]any, value any, at string, errs *[]string, fail func(string, ...any)) {
	if raw, ok := sch["type"]; ok && !typeMatches(raw, value) {
		fail("want type %v, got %s", raw, jsonTypeOf(value))
		return
	}
	if c, ok := sch["const"]; ok && !jsonEqual(c, value) {
		fail("want %v, got %v", c, value)
	}
	if list, ok := sch["enum"].([]any); ok {
		found := false
		for _, e := range list {
			if jsonEqual(e, value) {
				found = true
				break
			}
		}
		if !found {
			fail("%v is not one of %v", value, list)
		}
	}
	if anyOf, ok := sch["anyOf"].([]any); ok {
		matched := false
		for _, branch := range anyOf {
			if len(s.validate(node{doc: doc, schema: branch}, value, at)) == 0 {
				matched = true
				break
			}
		}
		if !matched {
			fail("%v matches no branch of anyOf", value)
		}
	}
	switch v := value.(type) {
	case string:
		checkString(sch, v, fail)
	case float64:
		if lo, ok := sch["minimum"].(float64); ok && v < lo {
			fail("%v is below the minimum %v", v, lo)
		}
		if hi, ok := sch["maximum"].(float64); ok && v > hi {
			fail("%v is above the maximum %v", v, hi)
		}
	case []any:
		if lo, ok := sch["minItems"].(float64); ok && float64(len(v)) < lo {
			fail("%d items, want at least %v", len(v), lo)
		}
		if hi, ok := sch["maxItems"].(float64); ok && float64(len(v)) > hi {
			fail("%d items, want at most %v", len(v), hi)
		}
		if items, ok := sch["items"]; ok {
			for i, e := range v {
				s.check(node{doc: doc, schema: items}, e, fmt.Sprintf("%s[%d]", at, i), errs)
			}
		}
	case map[string]any:
		s.checkObject(doc, sch, v, at, errs, fail)
	}
}

func checkString(sch map[string]any, v string, fail func(string, ...any)) {
	n := float64(len([]rune(v)))
	if lo, ok := sch["minLength"].(float64); ok && n < lo {
		fail("%q is shorter than %v", v, lo)
	}
	if hi, ok := sch["maxLength"].(float64); ok && n > hi {
		fail("%q is longer than %v", v, hi)
	}
	if p, ok := sch["pattern"].(string); ok {
		re, err := regexp.Compile(p)
		if err != nil {
			fail("pattern %q: %v", p, err)
			return
		}
		if !re.MatchString(v) {
			fail("%q does not match %s", v, p)
		}
	}
}

func (s *schemaSet) checkObject(doc string, sch, v map[string]any, at string, errs *[]string, fail func(string, ...any)) {
	if req, ok := sch["required"].([]any); ok {
		for _, r := range req {
			if _, has := v[r.(string)]; !has {
				fail("missing required %q", r)
			}
		}
	}
	props, _ := sch["properties"].(map[string]any)
	patterns, _ := sch["patternProperties"].(map[string]any)
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sub := at + "." + k
		if names, ok := sch["propertyNames"]; ok {
			s.check(node{doc: doc, schema: names}, k, sub+" (key)", errs)
		}
		matched := false
		if p, ok := props[k]; ok {
			matched = true
			s.check(node{doc: doc, schema: p}, v[k], sub, errs)
		}
		for pattern, p := range patterns {
			if regexp.MustCompile(pattern).MatchString(k) {
				matched = true
				s.check(node{doc: doc, schema: p}, v[k], sub, errs)
			}
		}
		if !matched {
			if extra, ok := sch["additionalProperties"]; ok {
				s.check(node{doc: doc, schema: extra}, v[k], sub, errs)
			}
		}
	}
}

// typeMatches applies the type keyword, a name or a list of names.
func typeMatches(raw, value any) bool {
	names := []any{raw}
	if list, ok := raw.([]any); ok {
		names = list
	}
	got := jsonTypeOf(value)
	for _, n := range names {
		switch {
		case n == got:
			return true
		case n == "number" && got == "integer":
			return true
		}
	}
	return false
}

// jsonTypeOf names the JSON type of a decoded value; a whole number is an
// integer, as JSON Schema counts it.
func jsonTypeOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		if x == math.Trunc(x) {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func jsonEqual(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(x) == string(y)
}

// yamlToJSON converts a YAML node into the value a JSON Schema sees: what an
// editor's YAML language server validates. Timestamps stay strings, exactly
// as they are written.
func yamlToJSON(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return yamlToJSON(n.Content[0])
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			v, err := yamlToJSON(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			out[n.Content[i].Value] = v
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := yamlToJSON(c)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!null":
			return nil, nil
		case "!!bool":
			return strconv.ParseBool(strings.ToLower(n.Value))
		case "!!int", "!!float":
			var f float64
			if err := n.Decode(&f); err != nil {
				return nil, fmt.Errorf("line %d: %w", n.Line, err)
			}
			return f, nil
		default:
			return n.Value, nil
		}
	default:
		return nil, fmt.Errorf("line %d: unsupported YAML node kind %v", n.Line, n.Kind)
	}
}

// frontMatterJSON returns the front matter of a Markdown file as a JSON value.
func frontMatterJSON(t *testing.T, data []byte) any {
	t.Helper()
	block, _, err := SplitFrontMatter(data)
	if err != nil {
		t.Fatalf("split front matter: %v", err)
	}
	return yamlJSON(t, block)
}

// yamlJSON parses a YAML document into a JSON value.
func yamlJSON(t *testing.T, data []byte) any {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse yaml: %v", err)
	}
	v, err := yamlToJSON(&doc)
	if err != nil {
		t.Fatalf("convert yaml: %v", err)
	}
	return v
}
