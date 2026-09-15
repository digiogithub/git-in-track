package pando

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mustReadTestdata(name string) string {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		panic(err)
	}
	return string(b)
}

var (
	kbSearchTOON = mustReadTestdata("kb_search.toon")
	projectsTOON = mustReadTestdata("code_list_projects.toon")
	indexJobTOON = mustReadTestdata("code_index_project.toon")
)

func TestDecodeTOONScalarsAndObjects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   string
		want any
	}{
		{"empty document", "", map[string]any{}},
		{"flat object", "a: 1\nb: two\nc: true\nd: null", map[string]any{
			"a": 1.0, "b": "two", "c": true, "d": nil,
		}},
		{"quoted value keeps its commas and colons", `k: "a, b: c"`, map[string]any{"k": "a, b: c"}},
		{"quoted value keeps its escapes", `k: "line\none\ttab"`, map[string]any{"k": "line\none\ttab"}},
		{"quoted key", `"odd key": 1`, map[string]any{"odd key": 1.0}},
		{"nested object", "outer:\n  inner: 1\n  deeper:\n    x: y", map[string]any{
			"outer": map[string]any{"inner": 1.0, "deeper": map[string]any{"x": "y"}},
		}},
		{"empty nested object", "outer:\nnext: 1", map[string]any{
			"outer": map[string]any{}, "next": 1.0,
		}},
		{"inline primitive array", "tags[2]: index,planning", map[string]any{
			"tags": []any{"index", "planning"},
		}},
		{"inline array with a quoted comma", `tags[2]: "a,b",c`, map[string]any{
			"tags": []any{"a,b", "c"},
		}},
		{"explicit empty array", "tags: []", map[string]any{"tags": []any{}}},
		{"comments and blank lines are ignored", "# a comment\n\na: 1\n# another\nb: 2", map[string]any{
			"a": 1.0, "b": 2.0,
		}},
		{"CRLF input", "a: 1\r\nb: 2\r\n", map[string]any{"a": 1.0, "b": 2.0}},
		{"numbers that are not numbers stay strings", "a: 007\nb: +1\nc: 1.2.3", map[string]any{
			"a": "007", "b": "+1", "c": "1.2.3",
		}},
		{"exponent", "a: 1e-7", map[string]any{"a": 1e-7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeTOON(tc.in)
			if err != nil {
				t.Fatalf("decodeTOON() error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("decodeTOON() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestDecodeTOONListForm(t *testing.T) {
	t.Parallel()
	// A list-form array: the objects are not uniform, so the encoder cannot
	// use the tabular form. This is the shape kb_search_documents produces.
	in := "items[3]:\n  - a: 1\n    b: 2\n  - a: 3\n  - c:\n      d: 4"
	got, err := decodeTOON(in)
	if err != nil {
		t.Fatalf("decodeTOON() error = %v", err)
	}
	want := map[string]any{"items": []any{
		map[string]any{"a": 1.0, "b": 2.0},
		map[string]any{"a": 3.0},
		map[string]any{"c": map[string]any{"d": 4.0}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decodeTOON() = %#v, want %#v", got, want)
	}
}

func TestDecodeTOONListFormPrimitivesAndNestedArrays(t *testing.T) {
	t.Parallel()
	in := "items[3]:\n  - plain\n  - \"quoted, value\"\n  - [2]: 1,2"
	got, err := decodeTOON(in)
	if err != nil {
		t.Fatalf("decodeTOON() error = %v", err)
	}
	want := map[string]any{"items": []any{"plain", "quoted, value", []any{1.0, 2.0}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decodeTOON() = %#v, want %#v", got, want)
	}
}

func TestDecodeTOONTabularForm(t *testing.T) {
	t.Parallel()
	in := "users[2]{id,name,role}:\n  1,Alice,admin\n  2,\"Bob, Jr\",user"
	got, err := decodeTOON(in)
	if err != nil {
		t.Fatalf("decodeTOON() error = %v", err)
	}
	want := map[string]any{"users": []any{
		map[string]any{"id": 1.0, "name": "Alice", "role": "admin"},
		map[string]any{"id": 2.0, "name": "Bob, Jr", "role": "user"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decodeTOON() = %#v, want %#v", got, want)
	}
}

func TestDecodeTOONNestedFieldGroups(t *testing.T) {
	t.Parallel()
	in := "orders[1]{id,customer{name,country},total}:\n  7,Ada,GB,19.5"
	got, err := decodeTOON(in)
	if err != nil {
		t.Fatalf("decodeTOON() error = %v", err)
	}
	want := map[string]any{"orders": []any{map[string]any{
		"id":       7.0,
		"customer": map[string]any{"name": "Ada", "country": "GB"},
		"total":    19.5,
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decodeTOON() = %#v, want %#v", got, want)
	}
}

func TestDecodeTOONKeyedTabularForm(t *testing.T) {
	t.Parallel()
	in := "stats[2:]{age,city}:\n  ada: 36,London\n  bob: 41,Leeds"
	got, err := decodeTOON(in)
	if err != nil {
		t.Fatalf("decodeTOON() error = %v", err)
	}
	want := map[string]any{"stats": map[string]any{
		"ada": map[string]any{"age": 36.0, "city": "London"},
		"bob": map[string]any{"age": 41.0, "city": "Leeds"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decodeTOON() = %#v, want %#v", got, want)
	}
}

func TestDecodeTOONRootArray(t *testing.T) {
	t.Parallel()
	got, err := decodeTOON("[2]{a,b}:\n  1,2\n  3,4")
	if err != nil {
		t.Fatalf("decodeTOON() error = %v", err)
	}
	want := []any{
		map[string]any{"a": 1.0, "b": 2.0},
		map[string]any{"a": 3.0, "b": 4.0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decodeTOON() = %#v, want %#v", got, want)
	}
}

// Pando always configures the comma delimiter. A document that declares tab or
// pipe is rejected rather than silently mis-split into one giant cell.
func TestDecodeTOONRejectsOtherDelimiters(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"users[2|]{id,name}:\n  1|a\n  2|b", "a[2\t]: 1\t2\nb: 3"} {
		if _, err := decodeTOON(in); err == nil {
			t.Fatalf("decodeTOON(%q) succeeded, want an error", in)
		}
	}
}

func TestDecodeTOONRejectsMalformedInput(t *testing.T) {
	t.Parallel()
	// Accepting any of these would hand a caller silently wrong data.
	for _, in := range []string{
		"a: \"unterminated",
		"a[2]{x,y}:\n  1,2",
		"a[1]{x,y}:\n  1,2,3",
		"a[1]{x,y}:\n  1",
		"a[2:]{x}:\n  k: 1",
		"a{x,y:\n  1,2",
	} {
		if _, err := decodeTOON(in); err == nil {
			t.Errorf("decodeTOON(%q) succeeded, want an error", in)
		}
	}
}

// decodeStructured must still read plain JSON, which is what Pando's formatter
// falls back to when TOON encoding fails.
func TestDecodeStructuredAcceptsJSON(t *testing.T) {
	t.Parallel()
	got, err := decodeStructured(`{"count":1,"results":[{"file_path":"a.md","score":0.5}]}`)
	if err != nil {
		t.Fatalf("decodeStructured() error = %v", err)
	}
	obj, ok := got.(map[string]any)
	if !ok || obj["count"] != 1.0 {
		t.Fatalf("decodeStructured() = %#v", got)
	}
}

func TestDecodeRecordedPayloads(t *testing.T) {
	t.Parallel()
	for name, payload := range map[string]string{
		"kb_search":          kbSearchTOON,
		"code_list_projects": projectsTOON,
		"code_index_project": indexJobTOON,
	} {
		if _, err := decodeStructured(payload); err != nil {
			t.Errorf("decodeStructured(%s) error = %v", name, err)
		}
	}
	if !strings.Contains(kbSearchTOON, "results[2]") {
		t.Fatal("the recorded kb payload no longer looks like a list-form array")
	}
}
