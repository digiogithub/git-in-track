package core

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSerializeLongListsUseBlockStyle(t *testing.T) {
	t.Parallel()

	labels := []string{
		"a-very-long-label-name-one", "a-very-long-label-name-two",
		"a-very-long-label-name-three", "a-very-long-label-name-four",
	}
	it := &Item{ID: "ACME-US-0042", Type: TypeStory, Title: "Long lists", Status: "todo", Labels: labels}
	out, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}
	if !strings.Contains(string(out), "labels:\n  - a-very-long-label-name-one\n") {
		t.Errorf("a list too long for one line must be written as a block:\n%s", out)
	}
	back, err := ParseItem("stories/ACME-US-0042-long-lists.md", out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(back.Labels) != len(labels) {
		t.Errorf("labels = %v", back.Labels)
	}
}

func TestSerializeNestedExtras(t *testing.T) {
	t.Parallel()

	it := &Item{
		ID:     "ACME-US-0042",
		Type:   TypeStory,
		Title:  "Nested extras",
		Status: "todo",
		Custom: map[string]any{
			"flags":     []any{true, false},
			"count":     3,
			"ratio":     1.5,
			"reviewers": []any{map[string]any{"handle": "jose", "role": "lead"}},
			"reviewed":  time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
			"deadline":  time.Date(2026, 9, 15, 17, 30, 0, 0, time.UTC),
			"nested":    map[string]any{"deep": map[string]any{"deeper": "value"}},
		},
	}
	out, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}
	for _, want := range []string{
		"count: 3", "ratio: 1.5", "flags: [true, false]",
		"reviewed: 2026-09-15", "deadline: 2026-09-15T17:30:00Z",
		"deeper: value", "handle: jose",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output lost %q:\n%s", want, out)
		}
	}
	back, err := ParseItem("stories/ACME-US-0042-nested-extras.md", out)
	if err != nil {
		t.Fatalf("re-parse:\n%s\n%v", out, err)
	}
	twice, err := SerializeItem(back)
	if err != nil {
		t.Fatalf("re-serialize: %v", err)
	}
	if string(twice) != string(out) {
		t.Errorf("not idempotent\n--- once ---\n%s\n--- twice ---\n%s", out, twice)
	}
}

func TestSerializeDeletedAndNumbers(t *testing.T) {
	t.Parallel()

	estimate, effort, spent := 8.0, 20.0, 11.5
	it := &Item{
		ID: "ACME-T-0107", Type: TypeTask, Title: "Numbers", Status: "done",
		Estimate: &estimate, Effort: &effort, Spent: &spent, Deleted: true,
	}
	out, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}
	for _, want := range []string{"estimate: 8\n", "effort: 20\n", "spent: 11.5\n", "deleted: true\n"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output lost %q:\n%s", want, out)
		}
	}
	back, err := ParseItem("tasks/ACME-T-0107-numbers.md", out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if !back.Deleted || back.Estimate == nil || *back.Estimate != 8 || back.Spent == nil || *back.Spent != 11.5 {
		t.Errorf("round trip = %#v", back)
	}
}

func TestSerializeNilItem(t *testing.T) {
	t.Parallel()

	if _, err := SerializeItem(nil); err == nil {
		t.Error("SerializeItem(nil) succeeded, want an error")
	}
	if _, err := SerializeComment(nil); err == nil {
		t.Error("SerializeComment(nil) succeeded, want an error")
	}
}

func TestParseCommentErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		in   string
		want Code
	}{
		{
			name: "missing item",
			path: "comments/ACME-US-0042/20260901T104512Z-jose.md",
			in:   "---\ntype: comment\nauthor: jose\n---\n\nText.\n",
			want: CodeIDMissing,
		},
		{
			name: "item does not match the folder",
			path: "comments/ACME-US-0042/20260901T104512Z-jose.md",
			in:   "---\ntype: comment\nitem: ACME-US-0043\nauthor: jose\n---\n\nText.\n",
			want: CodeCommentMismatch,
		},
		{
			name: "wrong type",
			path: "comments/ACME-US-0042/20260901T104512Z-jose.md",
			in:   "---\ntype: story\nitem: ACME-US-0042\nauthor: jose\n---\n\nText.\n",
			want: CodeFMType,
		},
		{
			name: "unknown kind",
			path: "comments/ACME-US-0042/20260901T104512Z-jose.md",
			in:   "---\ntype: comment\nitem: ACME-US-0042\nauthor: jose\nkind: shout\n---\n\nText.\n",
			want: CodeEnum,
		},
		{
			name: "malformed id",
			path: "comments/nonsense/20260901T104512Z-jose.md",
			in:   "---\ntype: comment\nitem: nonsense\nauthor: jose\n---\n\nText.\n",
			want: CodeIDGrammar,
		},
		{
			name: "no front matter",
			path: "comments/ACME-US-0042/20260901T104512Z-jose.md",
			in:   "Just text.\n",
			want: CodeFMMissing,
		},
		{
			name: "malformed yaml",
			path: "comments/ACME-US-0042/20260901T104512Z-jose.md",
			in:   "---\nitem: [unbalanced\n---\n",
			want: CodeFMYAML,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := ParseComment(tt.path, []byte(tt.in))
			if err == nil {
				t.Fatalf("ParseComment() = %#v, want %s", c, tt.want)
			}
			if !containsCode(parseErrorCodes(err), tt.want) {
				t.Errorf("codes = %v, want %s (%v)", parseErrorCodes(err), tt.want, err)
			}
		})
	}
}

func TestParseCommentDefaults(t *testing.T) {
	t.Parallel()

	const src = "---\ntype: comment\nitem: ACME-US-0042\nauthor: jose\ncreated: 2026-09-01T10:45:12Z\nx-source: slack\n---\n\nText.\n"
	c, err := ParseComment("comments/ACME-US-0042/20260901T104512Z-jose.md", []byte(src))
	if err != nil {
		t.Fatalf("ParseComment(): %v", err)
	}
	if c.Kind != "" {
		t.Errorf("kind = %q, want it absent rather than defaulted at read time", c.Kind)
	}
	if c.Extra["x-source"] != "slack" {
		t.Errorf("extra = %#v", c.Extra)
	}
	out, err := SerializeComment(c)
	if err != nil {
		t.Fatalf("SerializeComment(): %v", err)
	}
	if !strings.Contains(string(out), "x-source: slack") {
		t.Errorf("unknown keys were dropped:\n%s", out)
	}
	if c.Rev != ComputeRev([]byte(src)) {
		t.Errorf("rev = %q", c.Rev)
	}
}

func TestParseErrorFormatting(t *testing.T) {
	t.Parallel()

	pe := newParseError("stories/ACME-US-0042-x.md", 7, "created", CodeDateFormat, "not ISO 8601", errors.New("cause"))
	got := pe.Error()
	for _, want := range []string{"stories/ACME-US-0042-x.md:7", "E-DATE-FORMAT", `field "created"`, "not ISO 8601"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, want it to contain %q", got, want)
		}
	}
	if !errors.Is(pe, ErrInvalidFrontMatter) {
		t.Error("a parse error must wrap ErrInvalidFrontMatter")
	}
	d := pe.Diagnostic()
	if d.Code != CodeDateFormat || d.Severity != SeverityError || d.Line != 7 {
		t.Errorf("Diagnostic() = %#v", d)
	}
	if s := d.String(); !strings.Contains(s, "E-DATE-FORMAT") || !strings.Contains(s, ":7") {
		t.Errorf("Diagnostic.String() = %q", s)
	}
	bare := Diagnostic{Code: CodeWarnNoDone, Severity: SeverityWarning, Message: "no done status"}
	if s := bare.String(); s != "W-PROJ-NO-DONE no done status" {
		t.Errorf("Diagnostic.String() = %q", s)
	}
	de := &DiagnosticError{Diagnostic: d}
	if !strings.Contains(de.Error(), "E-DATE-FORMAT") {
		t.Errorf("DiagnosticError.Error() = %q", de.Error())
	}
}

func TestDateJSON(t *testing.T) {
	t.Parallel()

	d := NewDate(time.Date(2026, 9, 15, 23, 59, 0, 0, time.UTC))
	data, err := json.Marshal(map[string]Date{"due": d})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `{"due":"2026-09-15"}` {
		t.Errorf("json = %s", data)
	}
	var back map[string]Date
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back["due"] != d {
		t.Errorf("round trip = %v, want %v", back["due"], d)
	}
	var zero map[string]Date
	if err := json.Unmarshal([]byte(`{"due":null}`), &zero); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if !zero["due"].IsZero() {
		t.Error("null must decode to the zero date")
	}
	if err := json.Unmarshal([]byte(`{"due":"15/09/2026"}`), &zero); err == nil {
		t.Error("a malformed date must fail to decode")
	}
}

func TestItemTypesAndRevString(t *testing.T) {
	t.Parallel()

	types := ItemTypes()
	if len(types) != 5 || types[0] != TypeEpic {
		t.Errorf("ItemTypes() = %v", types)
	}
	r := ComputeRev([]byte("x"))
	if r.String() != string(r) {
		t.Error("Rev.String must return the token")
	}
	if ItemID("ACME-US-0042").String() != "ACME-US-0042" {
		t.Error("ItemID.String must return the id")
	}
	if ProjectKey("ACME").String() != "ACME" {
		t.Error("ProjectKey.String must return the key")
	}
}
