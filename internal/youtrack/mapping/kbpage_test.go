package mapping

import (
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

func TestPageToArticleTitleLivesOnlyInSummary(t *testing.T) {
	cases := []struct {
		name        string
		page        core.KBPage
		wantSummary string
		wantContent string
		wantWarning string
	}{
		{
			name:        "a leading H1 repeating the title is removed",
			page:        core.KBPage{Title: "Runbook", Body: "# Runbook\n\nStep one."},
			wantSummary: "Runbook",
			wantContent: "Step one.",
		},
		{
			name:        "a leading H1 differing from the title is dropped and reported",
			page:        core.KBPage{Title: "Runbook", Body: "# Operations runbook\n\nStep one."},
			wantSummary: "Runbook",
			wantContent: "Step one.",
			wantWarning: "summary: Operations runbook:",
		},
		{
			name:        "no title falls back to the leading H1",
			page:        core.KBPage{Body: "# Operations runbook\n\nStep one."},
			wantSummary: "Operations runbook",
			wantContent: "Step one.",
		},
		{
			name:        "an H1 further down is a section, not a title",
			page:        core.KBPage{Title: "Runbook", Body: "Step one.\n\n# Appendix"},
			wantSummary: "Runbook",
			wantContent: "Step one.\n\n# Appendix",
		},
		{
			name:        "no title and no H1 is reported",
			page:        core.KBPage{Body: "Step one."},
			wantSummary: "",
			wantContent: "Step one.",
			wantWarning: "summary: the page has neither a title nor a leading H1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, warnings := PageToArticle(tc.page, PageOptions{})
			if payload.Summary != tc.wantSummary {
				t.Errorf("summary = %q, want %q", payload.Summary, tc.wantSummary)
			}
			if payload.Content != tc.wantContent {
				t.Errorf("content = %q, want %q", payload.Content, tc.wantContent)
			}
			if strings.Contains(payload.Content, "# "+payload.Summary) {
				t.Errorf("the title is duplicated as an H1 in the content: %q", payload.Content)
			}
			assertWarning(t, warnings, tc.wantWarning)
		})
	}
}

func TestPageToArticleStripsFrontMatterAndFeedback(t *testing.T) {
	raw := []byte("---\ntitle: Runbook\n---\n\nStep one.\n\n" +
		feedbackBegin + "\n\n---\n\n## Feedback\n\n" +
		`<!-- gintrack:feedback:note id="fb-1" anchor="sha256:aa" lines="1-1" author="jose" -->` +
		"\n### jose feedback: fb-1\n\nRename this step.\n\n" + feedbackEnd + "\n")
	page := *core.ParsePage("docs/runbook.md", "runbook.md", raw)

	payload, _ := PageToArticle(page, PageOptions{})
	for _, forbidden := range []string{"---\ntitle:", feedbackBegin, feedbackEnd, "## Feedback", "Rename this step"} {
		if strings.Contains(payload.Content, forbidden) {
			t.Errorf("the payload leaks %q:\n%s", forbidden, payload.Content)
		}
	}
	if payload.Content != "Step one." {
		t.Errorf("content = %q, want %q", payload.Content, "Step one.")
	}
}

// A page that merely documents the feedback format in a code sample has no
// feedback block, and its sample must reach YouTrack intact.
func TestPageToArticleKeepsFeedbackMarkersInsideCodeFences(t *testing.T) {
	body := "Documented like this:\n\n```\n" + feedbackBegin + "\n" + feedbackEnd + "\n```"
	payload, _ := PageToArticle(core.KBPage{Title: "Format", Body: body}, PageOptions{})
	if payload.Content != body {
		t.Errorf("content = %q, want the body unchanged", payload.Content)
	}
}

func TestPageToArticleAttachments(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantContent string
		wantRefs    []AttachmentRef
		wantWarning string
	}{
		{
			name:        "a relative image becomes a bare name",
			body:        "![d](./images/d.png)",
			wantContent: "![d](d.png)",
			wantRefs:    []AttachmentRef{{Name: "d.png", Path: "docs/kb/images/d.png"}},
		},
		{
			name:        "a parent-relative link is resolved against the page folder",
			body:        "[pack](../assets/p.pdf)",
			wantContent: "[pack](p.pdf)",
			wantRefs:    []AttachmentRef{{Name: "p.pdf", Path: "docs/assets/p.pdf"}},
		},
		{
			name:        "a root-relative reference is vault-relative",
			body:        "![d](/shared/d.png)",
			wantContent: "![d](d.png)",
			wantRefs:    []AttachmentRef{{Name: "d.png", Path: "shared/d.png"}},
		},
		{
			name:        "a URL is left alone",
			body:        "![d](https://example.com/d.png)",
			wantContent: "![d](https://example.com/d.png)",
		},
		{
			name:        "a link to another page is a wikilink's business",
			body:        "[other](./other.md)",
			wantContent: "[other](./other.md)",
		},
		{
			name:        "a reference climbing out of the vault is refused",
			body:        "![d](../../../etc/passwd.png)",
			wantContent: "![d](../../../etc/passwd.png)",
			wantWarning: "points outside the vault",
		},
		{
			name:        "two files sharing a base name cannot both resolve",
			body:        "![a](./a/d.png)\n\n![b](./b/d.png)",
			wantContent: "![a](d.png)\n\n![b](./b/d.png)",
			wantRefs:    []AttachmentRef{{Name: "d.png", Path: "docs/kb/a/d.png"}},
			wantWarning: "already attached under the name d.png",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := core.KBPage{Path: "docs/kb/page.md", Title: "Page", Body: tc.body}
			payload, warnings := PageToArticle(page, PageOptions{})
			if payload.Content != tc.wantContent {
				t.Errorf("content = %q, want %q", payload.Content, tc.wantContent)
			}
			if len(payload.Attachments) != len(tc.wantRefs) {
				t.Fatalf("attachments = %v, want %v", payload.Attachments, tc.wantRefs)
			}
			for i, want := range tc.wantRefs {
				if payload.Attachments[i] != want {
					t.Errorf("attachment %d = %v, want %v", i, payload.Attachments[i], want)
				}
			}
			assertWarning(t, warnings, tc.wantWarning)
		})
	}
}

func TestArticleToPagePreservesFeedbackVerbatim(t *testing.T) {
	block := feedbackBegin + "\n\n---\n\n## Feedback\n\n" +
		`<!-- gintrack:feedback:note id="fb-1" anchor="sha256:aa" lines="1-1" author="jose" -->` +
		"\n### jose feedback: fb-1\n\nRename this step.\n\n" + feedbackEnd
	existing := core.KBPage{Path: "docs/runbook.md", Title: "Runbook", Body: "Step one.\n\n" + block}
	article := youtrack.Article{IDReadable: "ACME-A-9", Summary: "Runbook", Content: "Step one, revised."}

	body, front, _ := ArticleToPage(article, existing, PageOptions{Options: Options{BaseURL: kbBaseURL}})
	if want := "Step one, revised.\n\n" + block; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
	if front["title"] != "Runbook" {
		t.Errorf("title = %v, want Runbook", front["title"])
	}
	if strings.HasPrefix(body, "# ") {
		t.Errorf("the title was re-emitted as an H1: %q", body)
	}
}

// The block is local commentary and core.PruneKbFeedback rewrites it on every
// write, so its absence or its movement upstream is never a remote change.
func TestEqualContentIgnoresTheFeedbackBlock(t *testing.T) {
	content := "Step one.\n\nStep two."
	withBlock := content + "\n\n" + feedbackBegin + "\n\n## Feedback\n\nnote\n\n" + feedbackEnd
	other := content + "\n\n" + feedbackBegin + "\n\n## Feedback\n\na different note\n\n" + feedbackEnd

	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "a pruned block is not a change", a: withBlock, b: content, want: true},
		{name: "a different note is not a change", a: withBlock, b: other, want: true},
		{name: "front matter is not content", a: "---\ntitle: x\n---\n\n" + content, b: content, want: true},
		{name: "trailing blank lines are not content", a: content + "\n\n\n", b: content, want: true},
		{name: "a real edit is a change", a: withBlock, b: "Step one.\n\nStep three.", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EqualContent(tc.a, tc.b); got != tc.want {
				t.Errorf("EqualContent = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestArticleToPageMergesTheExternalReference(t *testing.T) {
	existing := *core.ParsePage("docs/p.md", "p.md", []byte(
		"---\ntitle: P\nexternal:\n  - system: notion\n    id: abc\n  - system: youtrack\n    id: ACME-A-9\n---\n\nBody.\n"))
	article := youtrack.Article{IDReadable: "ACME-A-9", Summary: "P", Content: "Body."}

	_, front, _ := ArticleToPage(article, existing, PageOptions{Options: Options{BaseURL: kbBaseURL}})
	list, ok := front["external"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("external = %#v, want two entries", front["external"])
	}
	got := core.ParsePage("docs/p.md", "p.md", []byte("---\nexternal:\n"+
		"  - system: notion\n    id: abc\n  - system: youtrack\n    id: ACME-A-9\n"+
		"    url: "+kbBaseURL+"/article/ACME-A-9\n---\n\nBody.\n")).ExternalRefs
	if len(got) != 2 {
		t.Fatalf("the fixture is wrong: %v", got)
	}
	entry, _ := list[1].(map[string]any)
	if entry["system"] != "youtrack" || entry["id"] != "ACME-A-9" ||
		entry["url"] != kbBaseURL+"/article/ACME-A-9" {
		t.Errorf("the youtrack entry was not refreshed in place: %#v", list)
	}
	if first, _ := list[0].(map[string]any); first["system"] != "notion" {
		t.Errorf("another system's entry was not kept: %#v", list)
	}
}

func TestArticlePayloadInput(t *testing.T) {
	in := ArticlePayload{Summary: "S", Content: "C"}.Input()
	if in.Summary == nil || *in.Summary != "S" {
		t.Errorf("summary = %v, want S", in.Summary)
	}
	if in.Content == nil || *in.Content != "C" {
		t.Errorf("content = %v, want C", in.Content)
	}
	if in.ProjectID != "" || in.ParentArticleID != nil {
		t.Error("Input must leave the placement of the article to the caller")
	}
}

// assertWarning checks that a substring appears in the rendered warnings, or
// that there are none when want is empty.
func assertWarning(t *testing.T, warnings []Warning, want string) {
	t.Helper()
	lines := strings.Join(warningLines(warnings), "\n")
	if want == "" {
		if lines != "" {
			t.Errorf("unexpected warnings:\n%s", lines)
		}
		return
	}
	if !strings.Contains(lines, want) {
		t.Errorf("warnings do not mention %q:\n%s", want, lines)
	}
}
