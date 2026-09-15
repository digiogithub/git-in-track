package pandosync

import (
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

func testItem() core.Item {
	return core.Item{
		ID:        "GIT-US-0073",
		Type:      core.TypeStory,
		Title:     "Corpus exporter: keep a Pando-indexable copy",
		Status:    "in_progress",
		Parent:    "GIT-EP-0019",
		Milestone: "GIT-M-0013",
		Labels:    []string{"server", "performance"},
		Updated:   core.NewTimestamp(time.Date(2026, 9, 13, 21, 17, 27, 0, time.UTC)),
		Body:      "## Description\n\nSee [[GIT-US-0024]] for the path rules.\n",
	}
}

func testPage() *core.KBPage {
	return &core.KBPage{
		Path:    "docs/21-semantic-search.md",
		RelPath: "21-semantic-search.md",
		Project: "GIT",
		Title:   "Semantic search",
		Tags:    []string{"pando", "search"},
		Updated: core.NewTimestamp(time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)),
		Body:    "# Semantic search\n\nLinks to [[03-data-model]].\n",
	}
}

func TestRenderItemFrontMatterAndIdentityLine(t *testing.T) {
	got := string(RenderItem(ptr(testItem()), "GIT"))

	for _, want := range []string{
		"---\n",
		"id: GIT-US-0073\n",
		"type: story\n",
		"status: in_progress\n",
		"milestone: GIT-M-0013\n",
		"parent: GIT-EP-0019\n",
		"project: GIT\n",
		"updated: \"2026-09-13T21:17:27Z\"\n",
		"tags: [GIT-US-0073, server, performance, story, in_progress]\n",
		"aliases: [",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered item is missing %q:\n%s", want, got)
		}
	}

	// The id must survive into the chunk text, not only into the front matter,
	// because Pando keeps no metadata but tags and aliases.
	body := got[strings.Index(got, "---\n\n")+len("---\n\n"):]
	first, _, _ := strings.Cut(body, "\n")
	if want := "GIT-US-0073" + idSeparator + "Corpus exporter: keep a Pando-indexable copy"; first != want {
		t.Errorf("identity line = %q, want %q", first, want)
	}
	if !strings.Contains(got, "[[GIT-US-0024]]") {
		t.Error("wikilink did not survive the export")
	}
}

func TestRenderItemWithoutLabelsOrOptionalFields(t *testing.T) {
	it := core.Item{ID: "GIT-T-0122", Type: core.TypeTask, Title: "Serializer", Status: "todo"}
	got := string(RenderItem(&it, "GIT"))

	if strings.Contains(got, "milestone:") || strings.Contains(got, "parent:") || strings.Contains(got, "updated:") {
		t.Errorf("empty fields must be omitted, never written as null:\n%s", got)
	}
	if !strings.Contains(got, "tags: [GIT-T-0122, task, todo]\n") {
		t.Errorf("tags should hold the id, the type and the status:\n%s", got)
	}
	if !strings.HasSuffix(got, "GIT-T-0122"+idSeparator+"Serializer\n") {
		t.Errorf("an item with no body should end on its identity line:\n%s", got)
	}
}

func TestRenderIsByteStable(t *testing.T) {
	it := testItem()
	first := RenderItem(&it, "GIT")
	for i := 0; i < 5; i++ {
		if got := RenderItem(&it, "GIT"); string(got) != string(first) {
			t.Fatalf("render %d differs:\n%s\n---\n%s", i, first, got)
		}
	}
	pg := testPage()
	firstPage := RenderPage(pg, "GIT")
	for i := 0; i < 5; i++ {
		if got := RenderPage(pg, "GIT"); string(got) != string(firstPage) {
			t.Fatalf("page render %d differs", i)
		}
	}
}

func TestRenderPage(t *testing.T) {
	got := string(RenderPage(testPage(), "GIT"))
	for _, want := range []string{
		"path: 21-semantic-search.md\n",
		"project: GIT\n",
		"tags: [pando, search]\n",
		"Page: 21-semantic-search" + idSeparator + "Semantic search\n",
		"[[03-data-model]]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered page is missing %q:\n%s", want, got)
		}
	}
}

func TestRenderEscapesAwkwardTitles(t *testing.T) {
	it := core.Item{
		ID:    "GIT-US-0001",
		Type:  core.TypeStory,
		Title: "- yes: \"no\" #maybe",
		Body:  "body\n",
	}
	got := string(RenderItem(&it, "GIT"))
	fm, body, err := core.SplitFrontMatter([]byte(got))
	if err != nil {
		t.Fatalf("the rendered document is not parseable: %v\n%s", err, got)
	}
	parsed, _, err := core.ParseDocument([]byte(got))
	if err != nil {
		t.Fatalf("front matter %q does not parse: %v", fm, err)
	}
	if parsed["title"] != it.Title {
		t.Errorf("title round trip = %q, want %q", parsed["title"], it.Title)
	}
	if !strings.Contains(body, it.Title) {
		t.Errorf("the identity line should carry the title verbatim:\n%s", body)
	}
}

func TestRenderItemMultilineTitleStaysOnOneLine(t *testing.T) {
	it := core.Item{ID: "GIT-US-0002", Type: core.TypeStory, Title: "one\ntwo"}
	got := string(RenderItem(&it, "GIT"))
	body := got[strings.Index(got, "---\n\n")+len("---\n\n"):]
	if lines := strings.Split(strings.TrimRight(body, "\n"), "\n"); len(lines) != 1 {
		t.Errorf("identity line spans %d lines:\n%q", len(lines), body)
	}
}

func ptr[T any](v T) *T { return &v }
