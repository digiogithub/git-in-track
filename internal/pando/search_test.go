package pando

import (
	"context"
	"errors"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSearchKBParsesRecordedPayload(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolKBSearch, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult(kbSearchTOON), nil
	})
	c := newTestClient(t, f, nil)

	hits, err := c.SearchKB(context.Background(), "phase 9", KBSearchOptions{})
	if err != nil {
		t.Fatalf("SearchKB() error = %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}

	first := hits[0]
	if first.FilePath != "11-roadmap.md" {
		t.Errorf("FilePath = %q", first.FilePath)
	}
	if first.Rank != 1 {
		t.Errorf("Rank = %d, want 1", first.Rank)
	}
	if first.Score != 0.01639344262295082 {
		t.Errorf("Score = %v", first.Score)
	}
	// The escapes inside the quoted chunk must survive decoding.
	if !contains(first.Chunk, "### Cadence") || !contains(first.Chunk, "\n") {
		t.Errorf("Chunk did not decode its escapes: %q", first.Chunk)
	}
	if got := first.Metadata["source_path"]; got != "/www/git-in-track/docs/11-roadmap.md" {
		t.Errorf("Metadata[source_path] = %v", got)
	}
	want := time.Date(2026, 9, 13, 21, 20, 28, 0, time.UTC)
	if !first.UpdatedAt.Equal(want) {
		t.Errorf("UpdatedAt = %v, want %v", first.UpdatedAt, want)
	}
	if !first.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", first.CreatedAt, want)
	}

	second := hits[1]
	if len(second.Tags) != 2 || second.Tags[0] != "index" || second.Tags[1] != "planning" {
		t.Errorf("Tags = %v", second.Tags)
	}
	if second.Links != 4 || second.Backlinks != 2 {
		t.Errorf("Links/Backlinks = %d/%d, want 4/2", second.Links, second.Backlinks)
	}
}

// Pando caps limit at 20 and would silently clamp; the client clamps first so
// what it asked for is what it gets, and so a caller reading the wire sees the
// real request.
func TestSearchKBSendsDocumentedParameters(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolKBSearch, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult(kbSearchTOON), nil
	})
	c := newTestClient(t, f, nil)

	exclude := false
	_, err := c.SearchKB(context.Background(), "phase 9", KBSearchOptions{
		Limit:           500,
		Tags:            []string{"planning"},
		ExcludeOutdated: &exclude,
		Scope:           "project/",
		PathPrefix:      "corpus/git-in-track/",
		SortByDate:      true,
	})
	if err != nil {
		t.Fatalf("SearchKB() error = %v", err)
	}
	calls := f.callsFor(toolKBSearch)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	args := calls[0]
	if args["query"] != "phase 9" {
		t.Errorf("query = %v", args["query"])
	}
	if args["limit"] != float64(maxKBSearchLimit) {
		t.Errorf("limit = %v, want it clamped to %d", args["limit"], maxKBSearchLimit)
	}
	if args["exclude_outdated"] != false {
		t.Errorf("exclude_outdated = %v", args["exclude_outdated"])
	}
	if args["scope"] != "project/" {
		t.Errorf("scope = %v", args["scope"])
	}
	// path_prefix is the parameter Pando added on 2026-09-14; it is the only
	// way to keep a search inside the exported corpus.
	if args["path_prefix"] != "corpus/git-in-track/" {
		t.Errorf("path_prefix = %v", args["path_prefix"])
	}
	if args["sort_by_date"] != true {
		t.Errorf("sort_by_date = %v", args["sort_by_date"])
	}
	tags, ok := args["tags"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "planning" {
		t.Errorf("tags = %v", args["tags"])
	}
}

func TestSearchKBOmitsUnsetOptions(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolKBSearch, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult(kbSearchTOON), nil
	})
	c := newTestClient(t, f, nil)
	if _, err := c.SearchKB(context.Background(), "q", KBSearchOptions{}); err != nil {
		t.Fatalf("SearchKB() error = %v", err)
	}
	args := f.callsFor(toolKBSearch)[0]
	for _, k := range []string{"limit", "tags", "exclude_outdated", "scope", "path_prefix", "sort_by_date"} {
		if _, ok := args[k]; ok {
			t.Errorf("%s was sent although it was not set; Pando's own default must win", k)
		}
	}
}

// An empty result set is a successful call. Pando answers it with a sentence,
// not with an empty array, which is exactly the trap this test pins.
func TestSearchKBEmptyResultIsNotAnError(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolKBSearch, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult("No documents found matching the query."), nil
	})
	c := newTestClient(t, f, nil)
	hits, err := c.SearchKB(context.Background(), "nothing", KBSearchOptions{})
	if err != nil {
		t.Fatalf("SearchKB() error = %v, want nil", err)
	}
	if len(hits) != 0 {
		t.Fatalf("got %d hits, want 0", len(hits))
	}
}

func TestToolErrorIsDistinctFromEmptyResult(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolKBSearch, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return errorResult("kb search error: database is locked"), nil
	})
	c := newTestClient(t, f, nil)
	hits, err := c.SearchKB(context.Background(), "q", KBSearchOptions{})
	if !errors.Is(err, ErrToolFailed) {
		t.Fatalf("SearchKB() error = %v, want ErrToolFailed", err)
	}
	if hits != nil {
		t.Fatalf("got %v hits with an error", hits)
	}
	if !contains(err.Error(), "database is locked") {
		t.Errorf("the tool's own message was lost: %v", err)
	}
}

// Pando adds fields to these results without warning. A client that broke on
// one would break on a Pando upgrade.
func TestUnknownFieldsAreTolerated(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolKBSearch, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult("count: 1\n" +
			"brand_new_top_level: 42\n" +
			"results[1]:\n" +
			"  - file_path: a.md\n" +
			"    chunk_content: hello\n" +
			"    score: 0.5\n" +
			"    rank: 1\n" +
			"    brand_new_field: surprise\n" +
			"    nested_novelty:\n" +
			"      a: 1\n"), nil
	})
	c := newTestClient(t, f, nil)
	hits, err := c.SearchKB(context.Background(), "q", KBSearchOptions{})
	if err != nil {
		t.Fatalf("SearchKB() error = %v", err)
	}
	if len(hits) != 1 || hits[0].FilePath != "a.md" || hits[0].Chunk != "hello" {
		t.Fatalf("hits = %#v", hits)
	}
}

func TestSearchKBRejectsEmptyQuery(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	c := newTestClient(t, f, nil)
	if _, err := c.SearchKB(context.Background(), "   ", KBSearchOptions{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("SearchKB() error = %v, want ErrInvalidOptions", err)
	}
	if f.callCount() != 0 {
		t.Fatal("an empty query reached Pando")
	}
}

func TestSearchCodeParsesStructuredMetadata(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolCodeSearch, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return metadataResult(
			"1. 0.98 func internal/server/auth.go:42 authMiddleware\n2 results (offset 0)",
			map[string]any{
				"count": 2, "total": 2, "offset": 0, "limit": 20, "has_more": false,
				"results": []map[string]any{
					{
						"symbol_type": "function", "name": "authMiddleware",
						"name_path": "server.authMiddleware", "file_path": "internal/server/auth.go",
						"start_line": 42, "signature": "func authMiddleware(next http.Handler) http.Handler",
						"kind": "func", "score": 0.98, "rank": 1,
						"a_field_from_a_future_pando": true,
					},
					{
						"symbol_type": "method", "name": "ServeHTTP",
						"file_path": "internal/server/server.go", "start_line": 9,
						"kind": "method", "score": 0.41, "rank": 2,
					},
				},
			}), nil
	})
	c := newTestClient(t, f, func(o *Options) { o.ProjectID = "www_git-in-track" })

	hits, err := c.SearchCode(context.Background(), "", "authentication", CodeSearchOptions{})
	if err != nil {
		t.Fatalf("SearchCode() error = %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	if hits[0].NamePath != "server.authMiddleware" || hits[0].StartLine != 42 || hits[0].Score != 0.98 {
		t.Fatalf("hits[0] = %#v", hits[0])
	}
	if hits[1].SymbolType != "method" || hits[1].Rank != 2 {
		t.Fatalf("hits[1] = %#v", hits[1])
	}
	args := f.callsFor(toolCodeSearch)[0]
	if args["project_id"] != "www_git-in-track" {
		t.Errorf("project_id = %v, want the configured default", args["project_id"])
	}
	// include_docs is always sent: Pando's default of false silently drops
	// every Markdown hit, which is not what a caller would assume.
	if _, ok := args["include_docs"]; !ok {
		t.Error("include_docs was not sent explicitly")
	}
}

func TestSearchCodeClampsLimitAndTakesFilters(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolCodeSearch, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult("No symbols found matching the query."), nil
	})
	c := newTestClient(t, f, nil)
	hits, err := c.SearchCode(context.Background(), "proj", "q", CodeSearchOptions{
		Limit:       9000,
		Offset:      20,
		Languages:   []string{"go"},
		SymbolTypes: []string{"function"},
		MinScore:    0.25,
		IncludeDocs: true,
	})
	if err != nil {
		t.Fatalf("SearchCode() error = %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("got %d hits, want 0", len(hits))
	}
	args := f.callsFor(toolCodeSearch)[0]
	if args["limit"] != float64(maxCodeSearchHits) {
		t.Errorf("limit = %v, want it clamped to %d", args["limit"], maxCodeSearchHits)
	}
	if args["offset"] != float64(20) {
		t.Errorf("offset = %v", args["offset"])
	}
	if args["min_score"] != 0.25 {
		t.Errorf("min_score = %v", args["min_score"])
	}
	if args["include_docs"] != true {
		t.Errorf("include_docs = %v", args["include_docs"])
	}
}

func TestSearchCodeWithoutAProjectIsNotConfigured(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	c := newTestClient(t, f, nil)
	if _, err := c.SearchCode(context.Background(), "", "q", CodeSearchOptions{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("SearchCode() error = %v, want ErrNotConfigured", err)
	}
}

func TestListProjectsParsesRecordedPayload(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolCodeProjects, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult(projectsTOON), nil
	})
	c := newTestClient(t, f, nil)
	got, err := c.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d projects, want 2", len(got))
	}
	if got[1].ProjectID != "www_git-in-track" || got[1].RootPath != "/www/git-in-track" {
		t.Errorf("projects[1] = %#v", got[1])
	}
	if got[0].LanguageStats["go"] != 1352 {
		t.Errorf("LanguageStats = %v", got[0].LanguageStats)
	}
	if got[0].IndexingStatus != "completed" {
		t.Errorf("IndexingStatus = %q", got[0].IndexingStatus)
	}
	if got[0].LastIndexedAt.IsZero() {
		t.Error("LastIndexedAt did not parse")
	}
}

func TestListProjectsEmpty(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolCodeProjects, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult("No indexed projects found."), nil
	})
	c := newTestClient(t, f, nil)
	got, err := c.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d projects, want 0", len(got))
	}
}

func TestIndexProject(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolCodeIndex, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult(indexJobTOON), nil
	})
	c := newTestClient(t, f, nil)
	job, err := c.IndexProject(context.Background(), "/www/git-in-track", "git-in-track")
	if err != nil {
		t.Fatalf("IndexProject() error = %v", err)
	}
	if job != "idx-7f3c9a" {
		t.Fatalf("job = %q", job)
	}
	args := f.callsFor(toolCodeIndex)[0]
	if args["project_path"] != "/www/git-in-track" || args["project_name"] != "git-in-track" {
		t.Fatalf("args = %v", args)
	}
	if _, err := c.IndexProject(context.Background(), " ", ""); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("IndexProject() with no path error = %v, want ErrInvalidOptions", err)
	}
}

func TestMalformedResultIsUnreachableNotEmpty(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolCodeProjects, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult("this is: not [ valid \" toon"), nil
	})
	c := newTestClient(t, f, nil)
	if _, err := c.ListProjects(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("ListProjects() error = %v, want ErrUnreachable", err)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
