package mapping

import (
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

func TestWikilinksToArticleLinks(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		want        string
		wantWarning string
	}{
		{
			name: "a published page becomes an article link addressed by its slug",
			body: "See [[architecture/overview]].",
			want: "See [architecture/overview](" + kbBaseURL + "/article/ACME-A-3).",
		},
		{
			name: "an alias becomes the link text",
			body: "See [[architecture/overview|the overview]].",
			want: "See [the overview](" + kbBaseURL + "/article/ACME-A-3).",
		},
		{
			name: "a base name resolves when it is unambiguous",
			body: "See [[overview]].",
			want: "See [overview](" + kbBaseURL + "/article/ACME-A-3).",
		},
		{
			name: "an anchor is carried onto the article URL",
			body: "See [[operations/runbook#Rollback]].",
			want: "See [operations/runbook#Rollback](" + kbBaseURL + "/article/ACME-A-4#Rollback).",
		},
		{
			name:        "an unpublished page degrades to its text",
			body:        "See [[planning/backlog-grooming]].",
			want:        "See planning/backlog-grooming.",
			wantWarning: "links: planning/backlog-grooming:",
		},
		{
			name:        "an unknown page degrades to its text",
			body:        "See [[nowhere/at/all|nowhere]].",
			want:        "See nowhere.",
			wantWarning: "links: nowhere/at/all|nowhere:",
		},
		{
			name:        "a backlog item is never a page",
			body:        "See [[GIT-US-0042]].",
			want:        "See GIT-US-0042.",
			wantWarning: "links: GIT-US-0042:",
		},
		{
			name:        "a transclusion is flattened into a link",
			body:        "![[architecture/overview]]",
			want:        "[architecture/overview](" + kbBaseURL + "/article/ACME-A-3)",
			wantWarning: "flattened into a link",
		},
		{
			name: "a bare issue id is never wrapped",
			body: "ACME-42 and GIT-US-0042 are auto-linked by YouTrack.",
			want: "ACME-42 and GIT-US-0042 are auto-linked by YouTrack.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := core.KBPage{Path: "docs/p.md", Title: "P", Body: tc.body}
			payload, warnings := PageToArticle(page, kbTestOptions())
			if payload.Content != tc.want {
				t.Errorf("content = %q, want %q", payload.Content, tc.want)
			}
			assertWarning(t, warnings, tc.wantWarning)
		})
	}
}

func TestArticleLinksToWikilinks(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "a known article URL becomes a wikilink",
			body: "See [architecture/overview](" + kbBaseURL + "/article/ACME-A-3).",
			want: "See [[architecture/overview]].",
		},
		{
			name: "a link text that is not the slug becomes an alias",
			body: "See [the overview](" + kbBaseURL + "/article/ACME-A-3).",
			want: "See [[architecture/overview|the overview]].",
		},
		{
			name: "the REST spelling of the path resolves by article id",
			body: "See [operations/runbook](" + kbBaseURL + "/articles/ACME-A-4).",
			want: "See [[operations/runbook]].",
		},
		{
			name: "a different context path still resolves by article id",
			body: "See [operations/runbook](https://other.example.com/yt/article/ACME-A-4).",
			want: "See [[operations/runbook]].",
		},
		{
			name: "an anchor comes back inside the wikilink",
			body: "See [operations/runbook#Rollback](" + kbBaseURL + "/article/ACME-A-4#Rollback).",
			want: "See [[operations/runbook#Rollback]].",
		},
		{
			name: "an unknown article is left untouched",
			body: "See [something](" + kbBaseURL + "/article/ACME-A-99).",
			want: "See [something](" + kbBaseURL + "/article/ACME-A-99).",
		},
		{
			name: "an unrelated URL is left untouched",
			body: "See [the RFC](https://www.rfc-editor.org/rfc/rfc7231).",
			want: "See [the RFC](https://www.rfc-editor.org/rfc/rfc7231).",
		},
		{
			name: "an image is never turned into a wikilink",
			body: "![shot](" + kbBaseURL + "/article/ACME-A-3)",
			want: "![shot](" + kbBaseURL + "/article/ACME-A-3)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			article := youtrack.Article{IDReadable: "ACME-A-7", Summary: "P", Content: tc.body}
			body, _, _ := ArticleToPage(article, core.KBPage{Path: "docs/p.md"}, kbTestOptions())
			if body != tc.want {
				t.Errorf("body = %q, want %q", body, tc.want)
			}
		})
	}
}

// Code fences and code spans are exempt from every rule of the transform, in
// both directions, so that a page documenting the syntax survives publishing.
func TestPageTransformSkipsCode(t *testing.T) {
	body := "```\n[[architecture/overview]] ![d](./images/d.png)\n```\n\n" +
		"Inline `[[architecture/overview]]` and `![d](./images/d.png)` too."
	payload, warnings := PageToArticle(core.KBPage{Path: "docs/p.md", Title: "P", Body: body}, kbTestOptions())
	if payload.Content != body {
		t.Errorf("content = %q, want the body unchanged", payload.Content)
	}
	if len(payload.Attachments) != 0 {
		t.Errorf("attachments = %v, want none", payload.Attachments)
	}
	assertWarning(t, warnings, "")
}

func TestNewPageIndexBaseNameResolution(t *testing.T) {
	x := NewPageIndex([]PageRef{
		{Slug: "a/overview", ArticleID: "ACME-A-1"},
		{Slug: "b/overview", ArticleID: "ACME-A-2"},
		{Slug: "c/unique", ArticleID: "ACME-A-3", URL: kbBaseURL + "/article/ACME-A-3/"},
		{Slug: "", ArticleID: "ACME-A-4"},
	})
	if _, ok := x.BySlug("overview"); ok {
		t.Error("a base name shared by two pages must resolve to neither")
	}
	if ref, ok := x.BySlug("a/overview"); !ok || ref.ArticleID != "ACME-A-1" {
		t.Errorf("the full slug must still resolve: %v %v", ref, ok)
	}
	if ref, ok := x.BySlug("unique"); !ok || ref.ArticleID != "ACME-A-3" {
		t.Errorf("an unambiguous base name must resolve: %v %v", ref, ok)
	}
	if ref, ok := x.ByURL(kbBaseURL + "/article/ACME-A-3"); !ok || ref.Slug != "c/unique" {
		t.Errorf("a trailing slash must not change the lookup: %v %v", ref, ok)
	}
	if _, ok := x.ByArticle("ACME-A-4"); ok {
		t.Error("a reference without a slug is not addressable")
	}
	var nilIndex *PageIndex
	if _, ok := nilIndex.BySlug("a/overview"); ok {
		t.Error("a nil index must resolve nothing rather than panic")
	}
}
