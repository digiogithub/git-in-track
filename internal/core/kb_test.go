package core

import (
	"reflect"
	"testing"
)

func TestParseWikilink(t *testing.T) {
	tests := []struct {
		raw  string
		want Wikilink
	}{
		{
			raw:  "architecture/sso-overview",
			want: Wikilink{Raw: "architecture/sso-overview", Target: "architecture/sso-overview"},
		},
		{
			raw:  "sso-overview",
			want: Wikilink{Raw: "sso-overview", Target: "sso-overview"},
		},
		{
			raw:  "ACME-US-0042",
			want: Wikilink{Raw: "ACME-US-0042", Target: "ACME-US-0042", Item: "ACME-US-0042", Project: "ACME"},
		},
		{
			raw: "ACME-US-0042|the SSO story",
			want: Wikilink{
				Raw: "ACME-US-0042|the SSO story", Target: "ACME-US-0042",
				Item: "ACME-US-0042", Project: "ACME", Text: "the SSO story",
			},
		},
		{
			raw: "ACME-US-0042#20260901T104512Z-jose",
			want: Wikilink{
				Raw: "ACME-US-0042#20260901T104512Z-jose", Target: "ACME-US-0042",
				Item: "ACME-US-0042", Project: "ACME", Anchor: "20260901T104512Z-jose",
			},
		},
		{
			raw: "WEB/WEB-US-0031",
			want: Wikilink{
				Raw: "WEB/WEB-US-0031", Target: "WEB-US-0031",
				Item: "WEB-US-0031", Project: "WEB",
			},
		},
		{
			raw: "ACME:architecture/sso-overview",
			want: Wikilink{
				Raw:    "ACME:architecture/sso-overview",
				Target: "architecture/sso-overview", Project: "ACME",
			},
		},
		{
			raw: "architecture/sso-overview#Session revocation",
			want: Wikilink{
				Raw:    "architecture/sso-overview#Session revocation",
				Target: "architecture/sso-overview", Anchor: "Session revocation",
			},
		},
		{
			raw:  "notes/2026-09-03",
			want: Wikilink{Raw: "notes/2026-09-03", Target: "notes/2026-09-03"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got := ParseWikilink(tc.raw, false)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
			if got.IsItem() != (tc.want.Item != "") {
				t.Errorf("IsItem = %v", got.IsItem())
			}
		})
	}

	t.Run("embed", func(t *testing.T) {
		got := ParseWikilink("diagram", true)
		if !got.Embed {
			t.Error("embed flag was lost")
		}
	})
}

func TestParsePage(t *testing.T) {
	const doc = "---\n" +
		"title: Architecture overview\n" +
		"tags: [architecture, sso]\n" +
		"updated: 2026-08-30T16:02:11Z\n" +
		"---\n\n" +
		"# Ignored because the front matter wins\n\n" +
		"Body with [[Auth Overview]] and [a link](https://openid.net/specs/).\n\n" +
		"## Session revocation\n\n" +
		"Inline `[[NotIndexed]]` code and a fence:\n\n" +
		"```mermaid\ngraph TD; A-->B;\n[[AlsoNotIndexed]]\n```\n\n" +
		"### Deep heading\n"

	p := ParsePage("docs/architecture/overview.md", "architecture/overview.md", []byte(doc))
	if p.Title != "Architecture overview" {
		t.Errorf("title = %q", p.Title)
	}
	if !reflect.DeepEqual(p.Tags, []string{"architecture", "sso"}) {
		t.Errorf("tags = %v", p.Tags)
	}
	if p.Updated.String() != "2026-08-30T16:02:11Z" {
		t.Errorf("updated = %q", p.Updated)
	}
	if p.Slug() != "architecture/overview" {
		t.Errorf("slug = %q", p.Slug())
	}
	wantHeadings := []Heading{
		{Level: 1, Text: "Ignored because the front matter wins", Anchor: "ignored-because-the-front-matter-wins"},
		{Level: 2, Text: "Session revocation", Anchor: "session-revocation"},
		{Level: 3, Text: "Deep heading", Anchor: "deep-heading"},
	}
	if !reflect.DeepEqual(p.Headings, wantHeadings) {
		t.Errorf("headings = %+v", p.Headings)
	}
	if len(p.Links) != 1 || p.Links[0].Target != "Auth Overview" {
		t.Errorf("links = %+v", p.Links)
	}
	if !reflect.DeepEqual(p.External, []string{"https://openid.net/specs/"}) {
		t.Errorf("external = %v", p.External)
	}
	if !p.Rev.Valid() {
		t.Errorf("rev = %q", p.Rev)
	}
}

func TestParsePageTitleFallbacks(t *testing.T) {
	tests := []struct {
		name string
		path string
		doc  string
		want string
	}{
		{name: "first h1", path: "docs/a.md", doc: "# From the heading\n\ntext\n", want: "From the heading"},
		{name: "file name", path: "docs/some-notes.md", doc: "no heading at all\n", want: "some notes"},
		{
			name: "front matter without a mapping is ignored",
			path: "docs/b.md",
			doc:  "---\n- one\n- two\n---\n\n# Heading wins\n",
			want: "Heading wins",
		},
		{name: "no front matter", path: "docs/c.md", doc: "# Plain\n", want: "Plain"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := ParsePage(tc.path, tc.path, []byte(tc.doc))
			if p.Title != tc.want {
				t.Errorf("title = %q, want %q", p.Title, tc.want)
			}
		})
	}
}

func TestParsePageTagsForms(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want []string
	}{
		{name: "sequence", doc: "---\ntags: [a, b]\n---\n\ntext\n", want: []string{"a", "b"}},
		{name: "comma separated", doc: "---\ntags: a, b\n---\n\ntext\n", want: []string{"a", "b"}},
		{name: "single", doc: "---\ntags: solo\n---\n\ntext\n", want: []string{"solo"}},
		{name: "absent", doc: "---\ntitle: x\n---\n\ntext\n", want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParsePage("docs/p.md", "p.md", []byte(tc.doc)).Tags
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("tags = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChecklistCounts(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantTotal int
		wantDone  int
	}{
		{
			name:      "mixed",
			body:      "## Acceptance Criteria\n\n- [x] one\n- [ ] two\n- [X] three\n",
			wantTotal: 3, wantDone: 2,
		},
		{name: "none", body: "## Description\n\nplain text\n"},
		{
			name:      "code fences are skipped",
			body:      "```md\n- [x] not a criterion\n```\n\n- [ ] real\n",
			wantTotal: 1, wantDone: 0,
		},
		{
			name:      "other bullets",
			body:      "* [x] star\n+ [ ] plus\n- regular bullet\n",
			wantTotal: 2, wantDone: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			total, done := checklistCounts(tc.body)
			if total != tc.wantTotal || done != tc.wantDone {
				t.Errorf("got %d/%d, want %d/%d", done, total, tc.wantDone, tc.wantTotal)
			}
		})
	}
}

func TestBuildTreeOrdersFoldersFirst(t *testing.T) {
	pages := []*KBPage{
		{Path: "docs/index.md", RelPath: "index.md", Title: "Index"},
		{Path: "docs/architecture/overview.md", RelPath: "architecture/overview.md", Title: "Overview"},
		{Path: "docs/architecture/adr/0001-storage.md", RelPath: "architecture/adr/0001-storage.md", Title: "ADR 1"},
		{Path: "docs/guides/setup.md", RelPath: "guides/setup.md", Title: "Setup"},
	}
	tree := buildTree("docs", pages)
	var names []string
	for _, c := range tree.Children {
		names = append(names, c.Name)
	}
	if !reflect.DeepEqual(names, []string{"architecture", "guides", "index.md"}) {
		t.Fatalf("children = %v", names)
	}
	arch := tree.Children[0]
	var archNames []string
	for _, c := range arch.Children {
		archNames = append(archNames, c.Name)
	}
	if !reflect.DeepEqual(archNames, []string{"adr", "overview.md"}) {
		t.Errorf("architecture children = %v", archNames)
	}
	if got := arch.Children[0].Children[0].Path; got != "docs/architecture/adr/0001-storage.md" {
		t.Errorf("nested leaf = %q", got)
	}
}
