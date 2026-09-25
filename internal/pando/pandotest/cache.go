// Package pandotest emulates the parts of Pando's MCP server that tests of
// the Pando client need and that the go-sdk server does not give for free.
// Today that is Pando's response cache: the interceptor that replaces a large
// tool result with a `[Response cached: …]` stub, and the cache_read tool that
// pages it back (Pando internal/llm/tools/cache_interceptor.go, cache.go and
// cache_read.go, v1.0.1). The text formats are reproduced byte for byte,
// because they are what the client parses.
//
// It is a test helper that lives outside a _test.go file so that the tests of
// internal/server can drive a real *pando.Client against it too. Nothing in a
// production build imports it.
package pandotest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Pando's thresholds and page sizes.
const (
	// ThresholdBytes and ThresholdLines are the sizes at which Pando caches a
	// result: either one is enough.
	ThresholdBytes = 15000
	ThresholdLines = 300
	// previewLines is how many lines the stub shows inline.
	previewLines = 200
	// defaultPageLines and maxPageLines are cache_read's default and cap.
	defaultPageLines = 200
	maxPageLines     = 500
	// ReadToolName is the pager's tool name.
	ReadToolName = "cache_read"
)

// Cache is one session's response cache. Pando keys it by MCP session; a test
// that rebuilds its server with a new Cache reproduces a restart, after which
// every earlier cache id is unknown.
type Cache struct {
	mu      sync.Mutex
	entries map[string][]string
	// reads counts cache_read calls, so a test can bound the paging.
	reads int
	// drop makes Intercept forget every entry it stores, the way Pando's LRU
	// eviction or a restart between the stub and the first page would.
	drop bool
	// corrupt, when set, rewrites every cache_read answer before it is sent.
	corrupt func(string) string
}

// SetCorrupt installs a rewrite of every cache_read answer, for the tests of
// a page the client must refuse. nil removes it.
func (c *Cache) SetCorrupt(fn func(string) string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.corrupt = fn
}

// NewCache returns an empty cache.
func NewCache() *Cache { return &Cache{entries: map[string][]string{}} }

// ReadCount reports how many cache_read calls were served.
func (c *Cache) ReadCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

// DropEntries makes every later stub point at an entry that is already gone.
func (c *Cache) DropEntries(drop bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.drop = drop
}

// Intercept is Pando's InterceptToolResponse: a successful text result at or
// past either threshold is stored and replaced by a stub that keeps its
// structuredContent. Anything else is returned unchanged.
func (c *Cache) Intercept(tool string, res *mcpsdk.CallToolResult) *mcpsdk.CallToolResult {
	if res == nil || res.IsError || tool == ReadToolName {
		return res
	}
	content := textOf(res)
	if len(content) < ThresholdBytes && strings.Count(content, "\n")+1 < ThresholdLines {
		return res
	}
	id := newID()
	lines := strings.Split(content, "\n")
	c.mu.Lock()
	if !c.drop {
		c.entries[id] = lines
	}
	c.mu.Unlock()

	shown := min(previewLines, len(lines))
	var sb strings.Builder
	fmt.Fprintf(&sb, "[Response cached: %d lines, %d bytes → cache_id: %q | tool: %s]\n",
		len(lines), len(content), id, tool)
	fmt.Fprintf(&sb, "[Showing lines 1-%d of %d. Use cache_read tool for more pages.]\n\n", shown, len(lines))
	for i, line := range lines[:shown] {
		fmt.Fprintf(&sb, "%6d|%s\n", i+1, line)
	}
	if shown < len(lines) {
		fmt.Fprintf(&sb, "\n[%d more lines available. Call: cache_read(cache_id=%q, offset=%d)]\n",
			len(lines)-shown, id, shown)
	}
	return &mcpsdk.CallToolResult{
		Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: sb.String()}},
		StructuredContent: res.StructuredContent,
	}
}

// pageMeta is Pando's PaginationMetadata, which cache_read returns as
// structuredContent.metadata.
type pageMeta struct {
	CacheID       string `json:"cache_id"`
	TotalLines    int    `json:"total_lines"`
	TotalBytes    int    `json:"total_bytes"`
	Offset        int    `json:"offset"`
	Limit         int    `json:"limit"`
	HasMore       bool   `json:"has_more"`
	ReturnedLines int    `json:"returned_lines"`
	ToolName      string `json:"tool_name"`
}

// Register adds the cache_read tool to srv.
func (c *Cache) Register(srv *mcpsdk.Server) {
	srv.AddTool(&mcpsdk.Tool{
		Name:        ReadToolName,
		Description: "Read paginated content from the session cache.",
		InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		var p struct {
			CacheID string `json:"cache_id"`
			Offset  int    `json:"offset"`
			Limit   int    `json:"limit"`
		}
		if len(req.Params.Arguments) > 0 {
			_ = json.Unmarshal(req.Params.Arguments, &p)
		}
		return c.read(p.CacheID, p.Offset, p.Limit), nil
	})
}

// read is cache_read in pagination mode.
func (c *Cache) read(id string, offset, limit int) *mcpsdk.CallToolResult {
	c.mu.Lock()
	c.reads++
	lines, ok := c.entries[id]
	corrupt := c.corrupt
	c.mu.Unlock()
	if id == "" {
		return errorResult("cache_id is required")
	}
	if !ok {
		return errorResult("cache read error: cache entry not found: " + id)
	}
	if limit <= 0 {
		limit = defaultPageLines
	}
	limit = min(limit, maxPageLines)
	offset = max(offset, 0)
	end := min(offset+limit, len(lines))
	var page []string
	if offset < len(lines) {
		page = lines[offset:end]
	}
	total := len(strings.Join(lines, "\n"))
	meta := pageMeta{
		CacheID: id, TotalLines: len(lines), TotalBytes: total, Offset: offset, Limit: limit,
		HasMore: offset < len(lines) && end < len(lines), ReturnedLines: len(page), ToolName: "fake",
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[Cache page: lines %d-%d of %d | tool: %s | cache_id: %s]\n",
		offset+1, offset+meta.ReturnedLines, meta.TotalLines, meta.ToolName, id)
	if meta.ReturnedLines == 0 {
		sb.WriteString("[No more content — end of cached response]\n")
	} else {
		sb.WriteString("\n")
		for i, line := range strings.Split(strings.Join(page, "\n"), "\n") {
			fmt.Fprintf(&sb, "%6d|%s\n", offset+i+1, line)
		}
	}
	if meta.HasMore {
		fmt.Fprintf(&sb, "\n[Has more content. Use cache_read with cache_id=%q, offset=%d to continue]",
			id, offset+meta.ReturnedLines)
	} else {
		sb.WriteString("\n[End of cached content]")
	}
	text := sb.String()
	if corrupt != nil {
		text = corrupt(text)
	}
	raw, _ := json.Marshal(meta)
	return &mcpsdk.CallToolResult{
		Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
		StructuredContent: map[string]any{"metadata": string(raw)},
	}
}

func errorResult(s string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: s}}, IsError: true}
}

func textOf(res *mcpsdk.CallToolResult) string {
	var b strings.Builder
	for _, content := range res.Content {
		if tc, ok := content.(*mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// newID is a random cache id, as Pando's uuid.New() is: every run differs,
// which is exactly what a determinism test needs to see.
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// KBHit is one row of KBSearchTOON.
type KBHit struct {
	FilePath string
	Chunk    string
	Score    float64
}

// KBSearchTOON renders hits the way Pando's kb_search_documents does: a TOON
// document with a count and a results list. Chunks are quoted, so any text
// survives.
func KBSearchTOON(hits ...KBHit) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "count: %d\nresults[%d]:\n", len(hits), len(hits))
	for i, h := range hits {
		fmt.Fprintf(&sb, "  - chunk_content: %s\n", strconv.Quote(h.Chunk))
		fmt.Fprintf(&sb, "    file_path: %s\n", h.FilePath)
		fmt.Fprintf(&sb, "    rank: %d\n", i+1)
		fmt.Fprintf(&sb, "    score: %s\n", strconv.FormatFloat(h.Score, 'g', -1, 64))
	}
	return strings.TrimSuffix(sb.String(), "\n")
}
