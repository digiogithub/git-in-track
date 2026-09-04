package core

import (
	"errors"
	"flag"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// update rewrites the golden files instead of comparing against them.
// Golden diffs are reviewed like code; never regenerate them to make a test pass.
var update = flag.Bool("update", false, "rewrite golden files")

func TestSplitFrontMatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		wantFM  string
		wantBdy string
		wantErr bool
	}{
		{
			name:    "front matter and body",
			in:      "---\nid: ACME-US-0042\n---\n\n## Description\n\nText.\n",
			wantFM:  "id: ACME-US-0042\n",
			wantBdy: "## Description\n\nText.",
		},
		{
			name:   "empty body",
			in:     "---\nid: ACME-US-0042\n---\n",
			wantFM: "id: ACME-US-0042\n",
		},
		{
			name:    "crlf is normalised",
			in:      "---\r\nid: ACME-US-0042\r\n---\r\n\r\nText.\r\n",
			wantFM:  "id: ACME-US-0042\n",
			wantBdy: "Text.",
		},
		{
			name:    "bom is stripped",
			in:      bomString + "---\nid: ACME-US-0042\n---\n\nText.\n",
			wantFM:  "id: ACME-US-0042\n",
			wantBdy: "Text.",
		},
		{
			name:    "empty front matter block",
			in:      "---\n---\n\nText.\n",
			wantFM:  "",
			wantBdy: "Text.",
		},
		{
			name:    "a fence inside the body is not a terminator",
			in:      "---\nid: ACME-US-0042\n---\n\nA thematic break:\n\n---\n\nEnd.\n",
			wantFM:  "id: ACME-US-0042\n",
			wantBdy: "A thematic break:\n\n---\n\nEnd.",
		},
		{name: "no front matter", in: "# Title\n\nText.\n", wantErr: true},
		{name: "unterminated", in: "---\nid: ACME-US-0042\n\n# Title\n", wantErr: true},
		{name: "empty file", in: "", wantErr: true},
		{name: "fence only", in: "---\n", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fm, body, err := SplitFrontMatter([]byte(tt.in))
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidFrontMatter) {
					t.Fatalf("err = %v, want ErrInvalidFrontMatter", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SplitFrontMatter(): %v", err)
			}
			if string(fm) != tt.wantFM {
				t.Errorf("front matter = %q, want %q", fm, tt.wantFM)
			}
			if body != tt.wantBdy {
				t.Errorf("body = %q, want %q", body, tt.wantBdy)
			}
		})
	}
}

func TestParseDocument(t *testing.T) {
	t.Parallel()

	fm, body, err := ParseDocument([]byte("---\nid: ACME-US-0042\nlabels: [a, b]\n---\n\nBody.\n"))
	if err != nil {
		t.Fatalf("ParseDocument(): %v", err)
	}
	if fm["id"] != "ACME-US-0042" {
		t.Errorf("id = %v, want ACME-US-0042", fm["id"])
	}
	labels, ok := fm["labels"].([]any)
	if !ok || len(labels) != 2 {
		t.Errorf("labels = %#v, want two elements", fm["labels"])
	}
	if body != "Body." {
		t.Errorf("body = %q, want %q", body, "Body.")
	}
}

func TestParseItem(t *testing.T) {
	t.Parallel()

	const src = `---
id: ACME-US-0042
type: story
title: Login with SSO
status: in_progress
priority: high
parent: ACME-EP-0001
milestone: ACME-M-0003
sprint: ACME-TEAM-S-0007
assignees: [marta, jose]
author: jose
labels: [frontend, security]
estimate: 8
effort: 20
spent: 11.5
created: 2026-08-19T09:04:02Z
updated: 2026-09-01T10:45:12Z
started: 2026-08-28T08:10:00Z
due: 2026-09-15
links:
  - { kind: blocked_by, target: ACME-T-0107 }
attachments: [sso-sequence.png]
custom:
  risk: medium
x-legacy-tracker: JIRA-4711
---

## Description

Body text.
`

	it, err := ParseItem("stories/ACME-US-0042-login-with-sso.md", []byte(src))
	if err != nil {
		t.Fatalf("ParseItem(): %v", err)
	}

	if it.ID != "ACME-US-0042" || it.Type != TypeStory || it.Title != "Login with SSO" {
		t.Errorf("identity = (%q, %q, %q)", it.ID, it.Type, it.Title)
	}
	if it.Status != "in_progress" || it.Priority != PriorityHigh {
		t.Errorf("status = %q, priority = %q", it.Status, it.Priority)
	}
	if it.Parent != "ACME-EP-0001" || it.Milestone != "ACME-M-0003" || it.Sprint != "ACME-TEAM-S-0007" {
		t.Errorf("hierarchy = (%q, %q, %q)", it.Parent, it.Milestone, it.Sprint)
	}
	if len(it.Assignees) != 2 || it.Assignees[0] != "marta" || it.Author != "jose" {
		t.Errorf("people = %v / %q", it.Assignees, it.Author)
	}
	if it.Estimate == nil || *it.Estimate != 8 || it.Spent == nil || *it.Spent != 11.5 {
		t.Errorf("numbers = %v / %v", it.Estimate, it.Spent)
	}
	if got := it.Created.String(); got != "2026-08-19T09:04:02Z" {
		t.Errorf("created = %q", got)
	}
	if got := it.Due.String(); got != "2026-09-15" {
		t.Errorf("due = %q", got)
	}
	if len(it.Links) != 1 || it.Links[0].Kind != LinkBlockedBy || it.Links[0].Target != "ACME-T-0107" {
		t.Errorf("links = %#v", it.Links)
	}
	if it.Custom["risk"] != "medium" {
		t.Errorf("custom = %#v", it.Custom)
	}
	if it.Extra["x-legacy-tracker"] != "JIRA-4711" {
		t.Errorf("extra = %#v", it.Extra)
	}
	if it.Body != "## Description\n\nBody text." {
		t.Errorf("body = %q", it.Body)
	}
	if it.Rev != ComputeRev([]byte(src)) {
		t.Errorf("rev = %q, want the hash of the file bytes", it.Rev)
	}
	if strings.Contains(src, "rev:") {
		t.Error("the fixture must not store rev; it is computed on read")
	}
}

func TestParseItemAcceptsAliasesAndYAMLTimestamps(t *testing.T) {
	t.Parallel()

	const src = `---
id: ACME-T-0107
type: task
title: Add OIDC discovery client
status: todo
created: 2026-08-27T11:20:40Z
updated: "2026-09-02T16:03:19Z"
blocks: [ACME-US-0042]
depends_on: [ACME-T-0106]
labels: backend
---

Body.
`
	it, err := ParseItem("tasks/ACME-T-0107-add-oidc-discovery-client.md", []byte(src))
	if err != nil {
		t.Fatalf("ParseItem(): %v", err)
	}
	if got := it.Updated.String(); got != "2026-09-02T16:03:19Z" {
		t.Errorf("quoted timestamp = %q", got)
	}
	if len(it.Labels) != 1 || it.Labels[0] != "backend" {
		t.Errorf("a bare scalar must be read as a list of one, got %v", it.Labels)
	}
	want := []Link{
		{Kind: LinkBlocks, Target: "ACME-US-0042"},
		{Kind: LinkBlockedBy, Target: "ACME-T-0106"},
	}
	if len(it.Links) != len(want) {
		t.Fatalf("links = %#v, want %#v", it.Links, want)
	}
	for i, l := range it.Links {
		if l != want[i] {
			t.Errorf("links[%d] = %#v, want %#v", i, l, want[i])
		}
	}

	// The aliases are normalised into links on the next write (section 12.2).
	out, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}
	if strings.Contains(string(out), "blocks:") || strings.Contains(string(out), "depends_on:") {
		t.Errorf("aliases survived serialization:\n%s", out)
	}
}

func TestParseItemErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		path string
		want Code
	}{
		{name: "no front matter", file: "no-front-matter.md", want: CodeFMMissing},
		{name: "unterminated", file: "unterminated-front-matter.md", want: CodeFMMissing},
		{name: "malformed yaml", file: "malformed-yaml.md", want: CodeFMYAML},
		{name: "not a mapping", file: "front-matter-not-a-mapping.md", want: CodeFMYAML},
		{name: "anchors", file: "anchors.md", want: CodeFMYAML},
		{name: "bad id", file: "bad-id.md", want: CodeIDGrammar},
		{name: "missing id", file: "missing-id.md", want: CodeIDMissing},
		{name: "wrong type code", file: "wrong-type-code.md", want: CodeIDTypeCode},
		{name: "unknown type", file: "unknown-type.md", want: CodeFMType},
		{name: "missing title", file: "missing-title.md", want: CodeTitle},
		{name: "bad timestamp", file: "bad-timestamp.md", want: CodeDateFormat},
		{name: "bad priority", file: "bad-priority.md", want: CodeEnum},
		{name: "bad link kind", file: "bad-link-kind.md", want: CodeEnum},
		{name: "bad field type", file: "bad-field-type.md", want: CodeFieldType},
		{
			name: "file name does not match the id",
			path: "stories/ACME-US-0099-renamed.md",
			want: CodeIDFilename,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var data []byte
			p := tt.path
			if tt.file == "" {
				data = []byte("---\nid: ACME-US-0042\ntype: story\ntitle: Renamed\nstatus: todo\n---\n")
			} else {
				var err error
				p = filepath.Join("testdata", "invalid", tt.file)
				data, err = os.ReadFile(p)
				if err != nil {
					t.Fatalf("read fixture: %v", err)
				}
			}

			it, err := ParseItem(p, data)
			if err == nil {
				t.Fatalf("ParseItem(%s) = %#v, want error %s", p, it, tt.want)
			}
			if !errors.Is(err, ErrInvalidFrontMatter) {
				t.Errorf("err does not wrap ErrInvalidFrontMatter: %v", err)
			}
			codes := parseErrorCodes(err)
			if !containsCode(codes, tt.want) {
				t.Errorf("codes = %v, want %s (%v)", codes, tt.want, err)
			}
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("err is not a *ParseError: %v", err)
			}
			if pe.Path == "" {
				t.Error("the error must name the file")
			}
		})
	}
}

func TestSerializeItemGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string // file under testdata/
		path   string // vault-relative path the item pretends to live at
		golden string
	}{
		{
			name:   "messy story is normalised",
			source: "messy-story.md",
			path:   "stories/ACME-US-0042-login-with-sso.md",
			golden: "messy-story.md",
		},
		{
			name:   "minimal item",
			source: "minimal-item.md",
			path:   "epics/ACME-EP-0001-single-sign-on.md",
			golden: "minimal-item.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(filepath.Join("testdata", tt.source))
			if err != nil {
				t.Fatalf("read source: %v", err)
			}
			it, err := ParseItem(tt.path, src)
			if err != nil {
				t.Fatalf("ParseItem(): %v", err)
			}
			got, err := SerializeItem(it)
			if err != nil {
				t.Fatalf("SerializeItem(): %v", err)
			}
			compareGolden(t, tt.golden, got)
		})
	}
}

func TestSerializeCommentGolden(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("testdata", "comment.md"))
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	c, err := ParseComment("comments/ACME-US-0042/20260901T104512Z-marta.md", src)
	if err != nil {
		t.Fatalf("ParseComment(): %v", err)
	}
	got, err := SerializeComment(c)
	if err != nil {
		t.Fatalf("SerializeComment(): %v", err)
	}
	compareGolden(t, "comment.md", got)
}

// compareGolden compares got against testdata/golden/<name>, or rewrites the
// golden file when -update is passed.
func compareGolden(t *testing.T, name string, got []byte) {
	t.Helper()

	p := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden (run go test ./internal/core -update to create it): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s", p, got, want)
	}
}

func TestSerializeIsDeterministic(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("testdata", "messy-story.md"))
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	first := ""
	// Map iteration order is randomized per range statement, so repeating the
	// whole parse/serialize cycle is what proves the output is stable.
	for i := 0; i < 50; i++ {
		it, err := ParseItem("stories/ACME-US-0042-login-with-sso.md", src)
		if err != nil {
			t.Fatalf("ParseItem(): %v", err)
		}
		out, err := SerializeItem(it)
		if err != nil {
			t.Fatalf("SerializeItem(): %v", err)
		}
		if i == 0 {
			first = string(out)
			continue
		}
		if string(out) != first {
			t.Fatalf("run %d differs:\n%s\n--- first ---\n%s", i, out, first)
		}
	}
}

func TestRoundTripFixtures(t *testing.T) {
	t.Parallel()

	root := os.DirFS(filepath.Join("..", "..", "testdata", "fixtures", "project-basic", "docs", ".pmngr"))
	files := markdownFiles(t, root)
	if len(files) < 6 {
		t.Fatalf("found %d fixture files, want the full project-basic backlog", len(files))
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := fs.ReadFile(root, name)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			out := roundTrip(t, name, data)
			if string(out) != string(data) {
				t.Errorf("the fixture is not canonical\n--- got ---\n%s\n--- want ---\n%s", out, data)
			}
		})
	}
}

func TestRoundTripDogfoodBacklog(t *testing.T) {
	t.Parallel()

	// The project dogfoods its own format: every file under docs/.pmngr must
	// parse, and one normalisation pass must reach a fixed point.
	root := os.DirFS(filepath.Join("..", "..", "docs", ".pmngr"))
	files := markdownFiles(t, root)
	if len(files) == 0 {
		t.Fatal("no backlog files found under docs/.pmngr")
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := fs.ReadFile(root, name)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			once := roundTrip(t, name, data)
			twice := roundTrip(t, name, once)
			if string(once) != string(twice) {
				t.Errorf("serialization is not idempotent\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
			}
		})
	}
}

// roundTrip parses a file and serializes it again, choosing the parser by the
// folder the file lives in.
func roundTrip(t *testing.T, name string, data []byte) []byte {
	t.Helper()

	if strings.HasPrefix(name, "comments/") {
		c, err := ParseComment(name, data)
		if err != nil {
			t.Fatalf("ParseComment(%s): %v", name, err)
		}
		out, err := SerializeComment(c)
		if err != nil {
			t.Fatalf("SerializeComment(%s): %v", name, err)
		}
		return out
	}
	it, err := ParseItem(name, data)
	if err != nil {
		t.Fatalf("ParseItem(%s): %v", name, err)
	}
	out, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(%s): %v", name, err)
	}
	return out
}

// markdownFiles lists every Markdown file of a backlog tree, sorted.
func markdownFiles(t *testing.T, root fs.FS) []string {
	t.Helper()

	var out []string
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(p) != ".md" {
			return nil
		}
		out = append(out, p)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return out
}

func TestUnknownKeysSurviveARoundTrip(t *testing.T) {
	t.Parallel()

	const src = `---
id: ACME-US-0042
type: story
title: Login with SSO
status: todo
created: 2026-08-19T09:04:02Z
x-vendor-id: 4711
x-vendor:
  owner: northwind
  tags: [alpha, beta]
future_scalar_field: kept
---

Body.
`
	it, err := ParseItem("stories/ACME-US-0042-login-with-sso.md", []byte(src))
	if err != nil {
		t.Fatalf("ParseItem(): %v", err)
	}
	out, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}
	for _, want := range []string{"x-vendor-id: 4711", "x-vendor:", "owner: northwind", "tags: [alpha, beta]", "future_scalar_field: kept"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output lost %q:\n%s", want, out)
		}
	}
	again, err := ParseItem("stories/ACME-US-0042-login-with-sso.md", out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	third, err := SerializeItem(again)
	if err != nil {
		t.Fatalf("re-serialize: %v", err)
	}
	if string(third) != string(out) {
		t.Errorf("not idempotent\n--- first ---\n%s\n--- second ---\n%s", out, third)
	}
}

func TestSerializeQuotesAmbiguousScalars(t *testing.T) {
	t.Parallel()

	it := &Item{
		ID:     "ACME-US-0042",
		Type:   TypeStory,
		Status: "todo",
		Title:  "yes",
		Labels: []string{"a, b", "plain"},
		Custom: map[string]any{"answer": "42", "flag": "true", "when": "2026-09-15"},
	}
	out, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}
	back, err := ParseItem("stories/ACME-US-0042-yes.md", out)
	if err != nil {
		t.Fatalf("re-parse:\n%s\n%v", out, err)
	}
	if back.Title != it.Title {
		t.Errorf("title = %q, want %q", back.Title, it.Title)
	}
	if len(back.Labels) != 2 || back.Labels[0] != "a, b" {
		t.Errorf("labels = %#v", back.Labels)
	}
	for k, v := range it.Custom {
		if back.Custom[k] != v {
			t.Errorf("custom[%q] = %#v, want %#v", k, back.Custom[k], v)
		}
	}
}

func TestSerializeOmitsEmptyValues(t *testing.T) {
	t.Parallel()

	it := &Item{ID: "ACME-EP-0001", Type: TypeEpic, Title: "Foundations", Status: "backlog"}
	out, err := SerializeItem(it)
	if err != nil {
		t.Fatalf("SerializeItem(): %v", err)
	}
	for _, forbidden := range []string{"links:", "labels:", "assignees:", "custom:", "null", "[]", "deleted:"} {
		if strings.Contains(string(out), forbidden) {
			t.Errorf("output contains %q, want empty values omitted:\n%s", forbidden, out)
		}
	}
}

// parseErrorCodes collects the diagnostic codes of every *ParseError in a tree
// of joined errors.
func parseErrorCodes(err error) []Code {
	var out []Code
	var walk func(error)
	walk = func(e error) {
		//nolint:errorlint // this walks a tree of joined errors instead of one chain
		switch t := e.(type) {
		case nil:
			return
		case *ParseError:
			out = append(out, t.Code)
		case interface{ Unwrap() []error }:
			for _, sub := range t.Unwrap() {
				walk(sub)
			}
		case interface{ Unwrap() error }:
			walk(t.Unwrap())
		}
	}
	walk(err)
	return out
}

func containsCode(codes []Code, want Code) bool {
	for _, c := range codes {
		if c == want {
			return true
		}
	}
	return false
}

func FuzzParseItem(f *testing.F) {
	f.Add("---\nid: ACME-US-0042\ntype: story\ntitle: Login\nstatus: todo\n---\n\nBody.\n")
	f.Add("---\n---\n")
	f.Add("---\nid: [1, 2]\n---\n")
	f.Add("no front matter at all")
	f.Add("---\nlinks:\n  - { kind: blocks, target: ACME-US-0001 }\n---\n")
	f.Add(bomString + "---\r\nid: ACME-T-0001\r\ntype: task\r\n---\r\n")

	f.Fuzz(func(t *testing.T, s string) {
		it, err := ParseItem("stories/ACME-US-0042-fuzz.md", []byte(s))
		if err != nil {
			return
		}
		// Anything that parses must serialize, re-parse and reach a fixed point.
		out, err := SerializeItem(it)
		if err != nil {
			t.Fatalf("SerializeItem() after a successful parse: %v", err)
		}
		again, err := ParseItem("stories/ACME-US-0042-fuzz.md", out)
		if err != nil {
			t.Fatalf("re-parsing our own output failed: %v\n%s", err, out)
		}
		twice, err := SerializeItem(again)
		if err != nil {
			t.Fatalf("SerializeItem() on re-parsed item: %v", err)
		}
		if string(out) != string(twice) {
			t.Fatalf("serialization is not idempotent\n--- once ---\n%s\n--- twice ---\n%s", out, twice)
		}
	})
}
