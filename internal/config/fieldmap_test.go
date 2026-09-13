package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The nested `field_map`, tasks GIT-T-0131 and GIT-T-0136.

// fixtureWithComments is a project.yaml carrying hand-written comments, a key
// this package does not model and a flat field map, which is what the surgical
// writer has to leave intact.
const fixtureWithComments = `# The project of the field-map tests.
schema: 1
key: ACME

# A section internal/config knows nothing about.
workflow:
  initial: backlog

integrations:
  # Where the issues came from.
  youtrack:
    url: https://yt.example.com/youtrack
    project: ACME
    field_map:
      priority: Priority
`

// writeFixture puts the fixture in a temporary file and returns its path.
func writeFixture(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "project.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // a test fixture
		t.Fatalf("write the fixture: %v", err)
	}
	return path
}

// TestFieldMapReadsBothSpellings covers the compatibility rule: a project.yaml
// written before per-value mapping existed still loads, and means the same
// thing as the long form of the same statement.
func TestFieldMapReadsBothSpellings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		yaml  string
		field string
		want  map[string]string
	}{
		{
			name:  "the flat form is a field name",
			yaml:  "      status: State\n",
			field: "State",
		},
		{
			name:  "the nested form says the same thing",
			yaml:  "      status:\n        field: State\n",
			field: "State",
		},
		{
			name: "the nested form carries a value map",
			yaml: "      status:\n        field: State\n        values:\n" +
				"          In Progress: in_progress\n          Fixed: done\n",
			field: "State",
			want:  map[string]string{"In Progress": "in_progress", "Fixed": "done"},
		},
		{
			name:  "a value map alone leaves the field name to the default",
			yaml:  "      status:\n        values:\n          Fixed: done\n",
			field: "",
			want:  map[string]string{"Fixed": "done"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc := "key: ACME\nintegrations:\n  youtrack:\n    url: https://yt.example.com\n" +
				"    project: ACME\n    field_map:\n" + tc.yaml
			link, err := ParseYouTrackLink([]byte(doc), "project.yaml")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if link == nil {
				t.Fatal("the block was not read at all")
			}
			if got := link.FieldMap["status"].Field; got != tc.field {
				t.Errorf("field = %q, want %q", got, tc.field)
			}
			got := link.FieldMap.ValuesFor("status")
			if len(got) != len(tc.want) {
				t.Fatalf("values = %v, want %v", got, tc.want)
			}
			for from, to := range tc.want {
				if got[from] != to {
					t.Errorf("values[%q] = %q, want %q", from, got[from], to)
				}
			}
		})
	}
}

// TestFieldMapNamesIsTheFlatProjection covers the adapter every consumer that
// only wants field names takes.
func TestFieldMapNamesIsTheFlatProjection(t *testing.T) {
	t.Parallel()

	m := FieldMap{
		"status":   {Field: "State", Values: map[string]string{"Fixed": "done"}},
		"priority": {Field: "Priority"},
		"type":     {Values: map[string]string{"Bug": "task"}},
	}
	names := m.Names()
	if names["status"] != "State" || names["priority"] != "Priority" {
		t.Errorf("names = %v", names)
	}
	if _, ok := names["type"]; ok {
		t.Error("an entry with no field name must not appear in the flat projection")
	}
	if got := FieldMap(nil).Names(); got != nil {
		t.Errorf("an empty map projects to %v, want nil", got)
	}
}

// TestFieldMapValidation covers the refusals: an unknown git-in-track field, a
// value map on a field whose values are not enumerable, and a value that maps
// to nothing. Each one names the exact key it is about.
func TestFieldMapValidation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		m    FieldMap
		want string
	}{
		{
			name: "an unknown git-in-track field",
			m:    FieldMap{"statuz": {Field: "State"}},
			want: "field_map.statuz",
		},
		{
			name: "a retired key that never reached any code",
			m:    FieldMap{"labels": {Field: "Tags"}},
			want: "field_map.labels",
		},
		{
			name: "another retired key",
			m:    FieldMap{"sprint": {Field: "Sprint"}},
			want: "field_map.sprint",
		},
		{
			name: "a value map where values cannot be mapped",
			m:    FieldMap{"assignee": {Field: "Assignee", Values: map[string]string{"jose": "jose"}}},
			want: "field_map.assignee.values",
		},
		{
			name: "a value that maps to nothing",
			m:    FieldMap{"status": {Field: "State", Values: map[string]string{"Fixed": "   "}}},
			want: "field_map.status.values.Fixed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The map is validated as written rather than as normalized:
			// Normalized drops a blank target, and the point of this case is
			// that a user who wrote one is told so.
			var errs FieldErrors
			add := func(field, format string, args ...any) {
				errs = append(errs, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
			}
			tc.m.validate(add)
			if len(errs) == 0 {
				t.Fatalf("%v was accepted", tc.m)
			}
			found := false
			for _, e := range errs {
				if e.Field == tc.want {
					found = true
				}
			}
			if !found {
				t.Errorf("errors = %v, want one about %s", errs, tc.want)
			}
		})
	}
}

// TestSaveFieldMapPreservesTheFile covers GIT-T-0136: saving a field map edits
// the node tree, so every comment and every unrelated key survives, and an
// entry with no value map keeps the readable flat spelling.
func TestSaveFieldMapPreservesTheFile(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, fixtureWithComments)
	link, err := LoadYouTrackLink(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	link.FieldMap = FieldMap{
		"priority": {Field: "Priority"},
		"status":   {Field: "State", Values: map[string]string{"In Progress": "in_progress", "Fixed": "done"}},
	}
	changed, err := SaveYouTrackLink(path, *link)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !changed {
		t.Fatal("the write reported no change")
	}
	got, err := os.ReadFile(path) //nolint:gosec // the file this test just wrote
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	out := string(got)
	for _, want := range []string{
		"# The project of the field-map tests.",
		"# A section internal/config knows nothing about.",
		"# Where the issues came from.",
		"initial: backlog",
		"priority: Priority",
		"field: State",
		"In Progress: in_progress",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the rewritten file lost %q:\n%s", want, out)
		}
	}

	reloaded, err := LoadYouTrackLink(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.FieldMap.ValuesFor("status")["Fixed"] != "done" {
		t.Errorf("the value map did not survive the round trip: %v", reloaded.FieldMap)
	}
	if reloaded.FieldMap["priority"].Field != "Priority" {
		t.Errorf("the flat entry did not survive: %v", reloaded.FieldMap)
	}
}

// TestSaveFieldMapIsANoOpWhenNothingChanged covers the rule that keeps a
// settings write out of the git history: saving what the file already says
// rewrites nothing.
func TestSaveFieldMapIsANoOpWhenNothingChanged(t *testing.T) {
	t.Parallel()

	path := writeFixture(t, fixtureWithComments)
	link, err := LoadYouTrackLink(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := SaveYouTrackLink(path, *link); err != nil {
		t.Fatalf("first save: %v", err)
	}
	before, err := os.ReadFile(path) //nolint:gosec // the file this test just wrote
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	changed, err := SaveYouTrackLink(path, *link)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if changed {
		t.Error("saving an unchanged link rewrote the file")
	}
	after, err := os.ReadFile(path) //nolint:gosec // the file this test just wrote
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("the file changed:\n%s\n---\n%s", before, after)
	}
}

// TestFieldMapJSON covers the wire shape: an entry is an object, and the flat
// string a client may still send is accepted.
func TestFieldMapJSON(t *testing.T) {
	t.Parallel()

	var m FieldMap
	if err := json.Unmarshal([]byte(`{"status":{"field":"State","values":{"Fixed":"done"}},"priority":"Priority"}`), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m["priority"].Field != "Priority" {
		t.Errorf("the flat JSON form was not accepted: %v", m)
	}
	if m.ValuesFor("status")["Fixed"] != "done" {
		t.Errorf("the value map was not decoded: %v", m)
	}
	raw, err := json.Marshal(FieldMap{"status": {Field: "State"}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(raw) != `{"status":{"field":"State"}}` {
		t.Errorf("encoded as %s, want a stable object shape", raw)
	}
}
