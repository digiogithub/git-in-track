package pando

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/digiogithub/git-in-track/internal/pando/pandotest"
)

// bigKB renders n knowledge-base hits whose chunks are size bytes long.
func bigKB(n, size int) string {
	hits := make([]pandotest.KBHit, n)
	for i := range hits {
		hits[i] = pandotest.KBHit{
			FilePath: fmt.Sprintf("doc-%03d.md", i+1),
			Chunk:    fmt.Sprintf("chunk %03d ", i+1) + strings.Repeat("x", size),
			Score:    1 / float64(i+2),
		}
	}
	return pandotest.KBSearchTOON(hits...)
}

// cacheIDPattern matches the UUID Pando uses as a cache id.
var cacheIDPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// TestSearchKBFollowsCachedResponses is the failing test of GIT-US-0164: a
// result past either of Pando's thresholds reaches the client as a
// `[Response cached …]` stub, and the client pages the full result back.
func TestSearchKBFollowsCachedResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		text  string
		want  int
		pages int // the fewest cache_read calls that can carry it
	}{
		// 16 chunks of about 1 KB: the tier-3 shape of the benchmark.
		{name: "over the byte threshold", text: bigKB(16, 1200), want: 16, pages: 1},
		// Short chunks, but more than 300 lines: several pages of 500.
		{name: "over the line threshold", text: bigKB(260, 10), want: 260, pages: 3},
		// One chunk with escaped newlines: a single line far past 15,000 bytes.
		{name: "one very long line", text: pandotest.KBSearchTOON(pandotest.KBHit{
			FilePath: "multi.md", Chunk: strings.Repeat("line \"quoted\"\n", 2000), Score: 0.5,
		}), want: 1, pages: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFakePando(t, "tok")
			f.setTool(toolKBSearch, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
				return textResult(tc.text), nil
			})
			c := newTestClient(t, f, nil)

			hits, err := c.SearchKB(context.Background(), "impact", KBSearchOptions{Limit: 16})
			if err != nil {
				t.Fatalf("SearchKB() error = %v", err)
			}
			if len(hits) != tc.want {
				t.Fatalf("got %d hits, want %d", len(hits), tc.want)
			}
			if got := f.responseCache().ReadCount(); got < tc.pages {
				t.Errorf("cache_read was called %d times, want at least %d", got, tc.pages)
			}
			last := hits[len(hits)-1]
			if last.FilePath == "" || last.Chunk == "" {
				t.Errorf("the last hit lost its fields: %+v", last)
			}
		})
	}
}

// TestSmallResultsAreNotPaged pins that a result under the thresholds costs
// no extra call.
func TestSmallResultsAreNotPaged(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolKBSearch, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult(kbSearchTOON), nil
	})
	c := newTestClient(t, f, nil)
	if _, err := c.SearchKB(context.Background(), "q", KBSearchOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := f.responseCache().ReadCount(); got != 0 {
		t.Errorf("cache_read was called %d times for a small result", got)
	}
}

// TestUnreadableCachedResponses covers every way the paging can fail: each is
// ErrUnreadable, so IsUnavailable holds, and no message names the cache id.
func TestUnreadableCachedResponses(t *testing.T) {
	t.Parallel()
	big := bigKB(16, 1200)
	for _, tc := range []struct {
		name  string
		setup func(*fakePando)
		text  string
	}{
		{
			name:  "the entry is gone",
			setup: func(f *fakePando) { f.responseCache().DropEntries(true) },
			text:  big,
		},
		{
			name: "a page is numbered wrong",
			setup: func(f *fakePando) {
				f.responseCache().SetCorrupt(func(s string) string {
					return strings.Replace(s, "     2|", "     3|", 1)
				})
			},
			text: big,
		},
		{
			name: "the pages do not add up",
			setup: func(f *fakePando) {
				f.responseCache().SetCorrupt(func(s string) string {
					return strings.Replace(s, "xxxx", "xxxxx", 1)
				})
			},
			text: big,
		},
		{
			name: "a header this client does not know",
			text: "[Response cached: somewhere → cache_id: \"0b1c2d3e-0000-0000-0000-000000000000\"]\n",
		},
		{
			name: "a result larger than the client reads",
			text: "[Response cached: 90000 lines, 900000000 bytes → cache_id: " +
				"\"0b1c2d3e-0000-0000-0000-000000000000\" | tool: kb_search_documents]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFakePando(t, "")
			if tc.setup != nil {
				tc.setup(f)
			}
			f.setTool(toolKBSearch, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
				return textResult(tc.text), nil
			})
			c := newTestClient(t, f, nil)

			_, err := c.SearchKB(context.Background(), "impact", KBSearchOptions{})
			if !errors.Is(err, ErrUnreadable) || !IsUnavailable(err) {
				t.Fatalf("SearchKB() error = %v, want ErrUnreadable", err)
			}
			if id := cacheIDPattern.FindString(err.Error()); id != "" || strings.Contains(err.Error(), "cache_id") {
				t.Errorf("the error names the cache id: %v", err)
			}
			if got := Reason(err); got != "Pando answered with a result this client cannot read" {
				t.Errorf("Reason() = %q", got)
			}
		})
	}
}

// TestCachedStubKeepsItsMetadata pins that a tool read from
// structuredContent.metadata still answers when its text cannot be paged
// back: Pando keeps the metadata on a stub.
func TestCachedStubKeepsItsMetadata(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.responseCache().DropEntries(true)
	f.setTool(toolCodeSearch, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return metadataResult(strings.Repeat("a compact line\n", 400), map[string]any{
			"count": 1,
			"results": []map[string]any{
				{"name": "NextID", "file_path": "internal/core/id.go", "start_line": 12, "score": 0.9},
			},
		}), nil
	})
	c := newTestClient(t, f, func(o *Options) { o.ProjectID = "p" })

	hits, err := c.SearchCode(context.Background(), "", "allocate", CodeSearchOptions{})
	if err != nil {
		t.Fatalf("SearchCode() error = %v", err)
	}
	if len(hits) != 1 || hits[0].Name != "NextID" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestReason(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, ""},
		{ErrNotConfigured, "Pando is not configured"},
		{fmt.Errorf("x: %w", ErrUnauthorized), "Pando rejected the token"},
		{fmt.Errorf("%w: %w", ErrTimeout, context.DeadlineExceeded), "Pando did not answer in time"},
		{context.DeadlineExceeded, "Pando did not answer in time"},
		{errCachePage, "Pando answered with a result this client cannot read"},
		{fmt.Errorf("%w: dial tcp 127.0.0.1:9777: refused", ErrUnreachable), "Pando is unreachable"},
		{ErrToolFailed, ""},
	} {
		if got := Reason(tc.err); got != tc.want {
			t.Errorf("Reason(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
