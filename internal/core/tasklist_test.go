package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskListItems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []TaskListItem
	}{
		{
			name: "empty body has no checkbox",
			body: "",
		},
		{
			name: "prose has no checkbox",
			body: "Just a paragraph mentioning [x] in passing.\n",
		},
		{
			name: "bullets and ordered markers",
			body: "- [ ] one\n* [x] two\n+ [X] three\n1. [ ] four\n2) [x] five\n",
			want: []TaskListItem{
				{Line: 1, Checked: false, Text: "one"},
				{Line: 2, Checked: true, Text: "two"},
				{Line: 3, Checked: true, Text: "three"},
				{Line: 4, Checked: false, Text: "four"},
				{Line: 5, Checked: true, Text: "five"},
			},
		},
		{
			name: "nested items keep their indent",
			body: "- [ ] parent\n  - [x] child\n\t- [ ] tab child\n",
			want: []TaskListItem{
				{Line: 1, Checked: false, Text: "parent"},
				{Line: 2, Checked: true, Text: "child", Indent: "  "},
				{Line: 3, Checked: false, Text: "tab child", Indent: "\t"},
			},
		},
		{
			name: "a fenced block hides its checkboxes",
			body: "- [ ] real\n```\n- [x] code\n```\n- [ ] real again\n",
			want: []TaskListItem{
				{Line: 1, Checked: false, Text: "real"},
				{Line: 5, Checked: false, Text: "real again"},
			},
		},
		{
			name: "a tilde fence hides its checkboxes too",
			body: "~~~text\n- [x] code\n~~~\n- [ ] after\n",
			want: []TaskListItem{{Line: 4, Checked: false, Text: "after"}},
		},
		{
			name: "a bracket pair glued to the label is not a checkbox",
			body: "- [x]glued\n- [ ] spaced\n",
			want: []TaskListItem{{Line: 2, Checked: false, Text: "spaced"}},
		},
		{
			name: "an empty label is still a checkbox",
			body: "- [ ]\n",
			want: []TaskListItem{{Line: 1, Checked: false}},
		},
		{
			name: "CRLF line endings",
			body: "- [ ] one\r\n- [x] two\r\n",
			want: []TaskListItem{
				{Line: 1, Checked: false, Text: "one"},
				{Line: 2, Checked: true, Text: "two"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := TaskListItems(tc.body)
			if len(got) != len(tc.want) {
				t.Fatalf("TaskListItems() found %d items, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, want := range tc.want {
				if got[i].Line != want.Line || got[i].Checked != want.Checked ||
					got[i].Text != want.Text || got[i].Indent != want.Indent {
					t.Errorf("item %d = %+v, want %+v", i, got[i], want)
				}
			}
		})
	}
}

func TestSetTaskListItem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		line    int
		checked bool
		want    string
		wantErr bool
	}{
		{
			name: "tick an unchecked box", body: "- [ ] one\n- [ ] two\n", line: 2, checked: true,
			want: "- [ ] one\n- [x] two\n",
		},
		{
			name: "untick a checked box", body: "- [x] one\n", line: 1, checked: false,
			want: "- [ ] one\n",
		},
		{
			name: "an upper-case mark is normalized on write", body: "- [X] one\n", line: 1, checked: false,
			want: "- [ ] one\n",
		},
		{
			name: "setting the state it already holds is not a write", body: "- [x] one\n", line: 1, checked: true,
			want: "- [x] one\n",
		},
		{
			name: "the last line without a trailing newline", body: "intro\n- [ ] one", line: 2, checked: true,
			want: "intro\n- [x] one",
		},
		{
			name: "CRLF terminators survive", body: "- [ ] one\r\n- [ ] two\r\n", line: 1, checked: true,
			want: "- [x] one\r\n- [ ] two\r\n",
		},
		{
			name: "an indented box keeps its indent", body: "- [ ] p\n    - [ ] c\n", line: 2, checked: true,
			want: "- [ ] p\n    - [x] c\n",
		},
		{name: "line zero", body: "- [ ] one\n", line: 0, checked: true, wantErr: true},
		{name: "past the end", body: "- [ ] one\n", line: 9, checked: true, wantErr: true},
		{name: "a prose line", body: "hello\n- [ ] one\n", line: 1, checked: true, wantErr: true},
		{
			name: "a checkbox inside a fence", body: "```\n- [ ] code\n```\n", line: 2, checked: true,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := SetTaskListItem(tc.body, tc.line, tc.checked)
			if tc.wantErr {
				if !errors.Is(err, ErrNotTaskListItem) {
					t.Fatalf("SetTaskListItem() error = %v, want ErrNotTaskListItem", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SetTaskListItem(): %v", err)
			}
			if got != tc.want {
				t.Errorf("SetTaskListItem() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSetTaskListItemTouchesOneLine is the guarantee the detail view rests on:
// a toggle rewrites the addressed line and leaves every other byte of the
// document exactly as it was. The golden file is the whole body after the
// toggle, so a change in behavior shows up as a reviewable diff.
func TestSetTaskListItemTouchesOneLine(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("testdata", "tasklist-body.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	body := string(src)

	items := TaskListItems(body)
	if len(items) != 5 {
		t.Fatalf("the fixture must hold 5 checkboxes, found %d: %+v", len(items), items)
	}

	got, err := SetTaskListItem(body, items[1].Line, true)
	if err != nil {
		t.Fatalf("SetTaskListItem(): %v", err)
	}
	compareGolden(t, "tasklist-toggled.md", []byte(got))

	before, after := strings.Split(body, "\n"), strings.Split(got, "\n")
	if len(before) != len(after) {
		t.Fatalf("the body gained or lost lines: %d -> %d", len(before), len(after))
	}
	var changed []int
	for i := range before {
		if before[i] != after[i] {
			changed = append(changed, i+1)
		}
	}
	if len(changed) != 1 || changed[0] != items[1].Line {
		t.Fatalf("changed lines = %v, want exactly line %d", changed, items[1].Line)
	}
	if len(got) != len(body) {
		t.Errorf("a toggle must not change the length of the body: %d -> %d", len(body), len(got))
	}
}

// TestToggleRoundTripsThroughTheEmitter checks the whole write path: a body
// toggled by the core and serialized by the canonical emitter parses back into
// the same body, with the rest of the file byte-identical.
func TestToggleRoundTripsThroughTheEmitter(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("testdata", "messy-story.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	it, err := ParseItem("stories/ACME-US-0042-messy.md", src)
	if err != nil {
		t.Fatalf("ParseItem(): %v", err)
	}
	it.Body = "## Acceptance Criteria\n\n- [ ] one\n- [ ] two\n"
	baseline, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}

	toggled, err := SetTaskListItem(it.Body, 3, true)
	if err != nil {
		t.Fatalf("SetTaskListItem(): %v", err)
	}
	it.Body = toggled
	out, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}

	back, err := ParseItem("stories/ACME-US-0042-messy.md", out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	// The parser reports a body without its final newline; the emitter puts it
	// back. That normalization is the only difference a round-trip may show.
	if want := strings.TrimRight(toggled, "\n"); back.Body != want {
		t.Errorf("body did not round-trip:\n got %q\nwant %q", back.Body, want)
	}
	if strings.Count(string(baseline), "\n") != strings.Count(string(out), "\n") {
		t.Error("the serialized file gained or lost lines")
	}
}
