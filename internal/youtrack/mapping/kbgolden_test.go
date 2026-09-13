package mapping

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// The knowledge-base goldens are plain text rather than JSON: a transform whose
// output is Markdown has to be reviewed as Markdown, and the acceptance
// criterion is that a human reads the diff rather than regenerating it. They
// share the -update flag declared in golden_test.go.

const kbBaseURL = "https://yt.example.com/youtrack"

// kbTestOptions are the options every knowledge-base golden maps with: a fixed
// base URL, no synced_at because this package never reads a clock, and an index
// of four pages covering every resolution outcome.
func kbTestOptions() PageOptions {
	return PageOptions{
		Options: Options{BaseURL: kbBaseURL},
		Pages: NewPageIndex([]PageRef{
			{Slug: "handbook/index", Title: "Release handbook", ArticleID: "ACME-A-7",
				URL: kbBaseURL + "/article/ACME-A-7"},
			{Slug: "architecture/overview", Title: "Architecture overview", ArticleID: "ACME-A-3",
				URL: kbBaseURL + "/article/ACME-A-3"},
			{Slug: "operations/runbook", Title: "Runbook", ArticleID: "ACME-A-4"},
			// Known to the index but never published: a link to it must
			// degrade to text rather than to a dead article link.
			{Slug: "planning/backlog-grooming", Title: "Backlog grooming"},
		}),
	}
}

// loadPage parses a Markdown fixture as the knowledge-base page it represents.
func loadPage(t *testing.T, name, pagePath string) core.KBPage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading the fixture %s: %v", name, err)
	}
	relPath := strings.TrimPrefix(pagePath, "docs/")
	return *core.ParsePage(pagePath, relPath, raw)
}

// loadText reads a Markdown fixture verbatim.
func loadText(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading the fixture %s: %v", name, err)
	}
	return strings.TrimRight(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
}

func TestPageToArticleGolden(t *testing.T) {
	cases := []struct {
		name     string
		fixture  string
		pagePath string
		golden   string
	}{
		{
			name:     "page with front matter, links, attachments and feedback",
			fixture:  "kb-handbook.md",
			pagePath: "docs/handbook/index.md",
			golden:   "kb-handbook.up.golden.txt",
		},
		{
			name:     "page without front matter or feedback",
			fixture:  "kb-notes.md",
			pagePath: "docs/notes.md",
			golden:   "kb-notes.up.golden.txt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := loadPage(t, tc.fixture, tc.pagePath)
			payload, warnings := PageToArticle(page, kbTestOptions())
			compareGoldenText(t, tc.golden, renderPayload(payload, warnings))
		})
	}
}

func TestArticleToPageGolden(t *testing.T) {
	existing := loadPage(t, "kb-handbook.md", "docs/handbook/index.md")
	article := youtrack.Article{
		ID:         "42-7",
		IDReadable: "ACME-A-7",
		Summary:    "Release handbook, revised",
		Content:    loadText(t, "kb-article.md"),
	}
	body, front, warnings := ArticleToPage(article, existing, kbTestOptions())
	compareGoldenText(t, "kb-article.down.golden.txt", renderPage(body, front, warnings))
}

// TestPageRoundTripGolden records the full page -> article -> page cycle. The
// result is deliberately not identical to the input: a wikilink that resolves
// to nothing is flattened to text on the way up and cannot come back, which is
// the documented cost of never publishing a dead link. Everything else — front
// matter, resolvable links, attachment paths and the feedback block — returns
// as it was.
func TestPageRoundTripGolden(t *testing.T) {
	opts := kbTestOptions()
	page := loadPage(t, "kb-handbook.md", "docs/handbook/index.md")

	payload, upWarnings := PageToArticle(page, opts)
	article := youtrack.Article{
		ID:         "42-7",
		IDReadable: "ACME-A-7",
		Summary:    payload.Summary,
		Content:    payload.Content,
	}
	body, front, downWarnings := ArticleToPage(article, page, opts)

	var b strings.Builder
	b.WriteString(renderPage(body, front, downWarnings))
	b.WriteString(renderSection("warnings going up", strings.Join(warningLines(upWarnings), "\n")))
	compareGoldenText(t, "kb-handbook.roundtrip.golden.txt", b.String())
}

// TestPageRoundTripIsIdempotent is the stability claim the story makes: one
// round trip normalises, a second one changes nothing. Identity after the first
// pass is impossible — an unresolvable wikilink is flattened on purpose — so
// what must hold is that the transform reaches a fixed point immediately.
func TestPageRoundTripIsIdempotent(t *testing.T) {
	opts := kbTestOptions()
	page := loadPage(t, "kb-handbook.md", "docs/handbook/index.md")

	roundTrip := func(p core.KBPage) core.KBPage {
		t.Helper()
		payload, _ := PageToArticle(p, opts)
		article := youtrack.Article{IDReadable: "ACME-A-7", Summary: payload.Summary, Content: payload.Content}
		body, front, _ := ArticleToPage(article, p, opts)
		return *core.ParsePage(p.Path, p.RelPath, renderFile(t, front, body))
	}

	once := roundTrip(page)
	twice := roundTrip(once)
	if once.Body != twice.Body {
		t.Errorf("the round trip is not idempotent.\n--- once ---\n%s\n--- twice ---\n%s", once.Body, twice.Body)
	}
	if got, want := fmt.Sprint(twice.FrontMatter), fmt.Sprint(once.FrontMatter); got != want {
		t.Errorf("the front matter is not idempotent.\ngot  %s\nwant %s", got, want)
	}
	if !EqualContent(page.Body, once.Body) {
		// The two differ, and must: this asserts the difference is real
		// content rather than the feedback block alone.
		t.Log("the first round trip changed the content, as the flattened wikilinks require")
	}
}

// renderFile assembles front matter and body back into a page file, the way a
// vault write does, so that the round trip can be re-parsed by core.
func renderFile(t *testing.T, front map[string]any, body string) []byte {
	t.Helper()
	block, err := yaml.Marshal(front)
	if err != nil {
		t.Fatalf("encoding the front matter: %v", err)
	}
	return []byte("---\n" + string(block) + "---\n\n" + body + "\n")
}

// renderPayload renders an outgoing payload as the reviewable golden text.
func renderPayload(p ArticlePayload, warnings []Warning) string {
	var b strings.Builder
	b.WriteString(renderSection("summary", p.Summary))
	b.WriteString(renderSection("content", p.Content))
	lines := make([]string, 0, len(p.Attachments))
	for _, a := range p.Attachments {
		lines = append(lines, a.Name+" <- "+a.Path)
	}
	b.WriteString(renderSection("attachments", strings.Join(lines, "\n")))
	b.WriteString(renderSection("warnings", strings.Join(warningLines(warnings), "\n")))
	return b.String()
}

// renderPage renders an incoming page as the reviewable golden text.
func renderPage(body string, front map[string]any, warnings []Warning) string {
	var b strings.Builder
	keys := make([]string, 0, len(front))
	for k := range front {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var fm strings.Builder
	for _, k := range keys {
		out, err := yaml.Marshal(map[string]any{k: front[k]})
		if err != nil {
			fm.WriteString(k + ": <unencodable>\n")
			continue
		}
		fm.Write(out)
	}
	b.WriteString(renderSection("front matter", strings.TrimRight(fm.String(), "\n")))
	b.WriteString(renderSection("body", body))
	b.WriteString(renderSection("warnings", strings.Join(warningLines(warnings), "\n")))
	return b.String()
}

// renderSection writes one labeled section of a golden file. An empty section
// is written as such rather than omitted, so that a value disappearing shows up
// in the diff.
func renderSection(name, value string) string {
	if value == "" {
		return "=== " + name + " (empty) ===\n"
	}
	return "=== " + name + " ===\n" + value + "\n"
}

// compareGoldenText compares reviewable text with its golden file, or rewrites
// the file when -update is set. Read the diff: never regenerate blindly.
func compareGoldenText(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s (run go test -update to create it): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("%s does not match the golden file.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}
