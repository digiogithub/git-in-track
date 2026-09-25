package pando

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Pando's response cache (GIT-US-0164).
//
// Pando's MCP server replaces every tool result over 15,000 bytes or 300 lines
// with a stub: a `[Response cached: …]` header, a line-numbered preview of the
// first 200 lines and a pointer to its `cache_read` tool, which pages the full
// result out of a per-session cache (Pando internal/llm/tools/
// cache_interceptor.go and cache_read.go). Nothing turns this off, and a
// 16-chunk knowledge-base search already crosses the threshold. The stub
// keeps the tool's structuredContent.metadata; only the text is replaced.
//
// So call() recognizes the stub and pages the full text back with cache_read,
// over the same session and inside the same deadline as the call itself, and
// the decoders above never see a stub. The paging is bounded (maxCachedBytes,
// maxCachePages) and verified against the sizes the stub declares. A result
// that cannot be reassembled is ErrUnreadable with a fixed message: the cache
// id is a random UUID, and it must not reach a report that is meant to be
// byte-for-byte reproducible.
const (
	// toolCacheRead is Pando's pager for cached responses.
	toolCacheRead = "cache_read"
	// cachePageLines is how many lines one cache_read asks for: Pando's own
	// cap on the parameter.
	cachePageLines = 500
	// maxCachedBytes bounds a reassembled result. A search result this client
	// asks for is tens of kilobytes; anything past this is not one of them.
	maxCachedBytes = 4 << 20
	// maxCachePages bounds the cache_read calls behind one tool call.
	maxCachePages = 64
)

// cachedStubHeader matches the first line of a cached response, as Pando
// v1.0.1 writes it:
//
//	[Response cached: 412 lines, 16433 bytes → cache_id: "<uuid>" | tool: kb_search_documents]
var cachedStubHeader = regexp.MustCompile(
	`^\[Response cached: (\d+) lines, (\d+) bytes → cache_id: ("(?:[^"\\]|\\.)*") \| tool: [^\]]*\]`)

// cachePageHeader matches the first line of a cache_read page:
//
//	[Cache page: lines 1-500 of 812 | tool: kb_search_documents | cache_id: <uuid>]
var cachePageHeader = regexp.MustCompile(`^\[Cache page: lines (\d+)-(\d+) of (\d+) \|`)

// cachedStubPrefix is what every stub starts with; a text that starts with it
// but does not match cachedStubHeader is a stub this client cannot read.
const cachedStubPrefix = "[Response cached:"

// cachedStub is the part of a stub the pager needs.
type cachedStub struct {
	id    string
	lines int
	bytes int
}

// parseCachedStub reports whether text is a cached-response stub and, if it
// is one this client can read, what it points at.
func parseCachedStub(text string) (stub cachedStub, isStub bool, err error) {
	if !strings.HasPrefix(text, cachedStubPrefix) {
		return cachedStub{}, false, nil
	}
	m := cachedStubHeader.FindStringSubmatch(text)
	if m == nil {
		return cachedStub{}, true, errCacheHeader
	}
	lines, err1 := strconv.Atoi(m[1])
	size, err2 := strconv.Atoi(m[2])
	id, err3 := strconv.Unquote(m[3])
	if err1 != nil || err2 != nil || err3 != nil || id == "" || lines <= 0 {
		return cachedStub{}, true, errCacheHeader
	}
	return cachedStub{id: id, lines: lines, bytes: size}, true, nil
}

// Fixed reasons for a cached response that could not be read back. None of
// them names the cache id or anything else Pando chose at random.
var (
	errCacheHeader   = fmt.Errorf("%w: Pando cached the result in a format this client does not know", ErrUnreadable)
	errCacheTooLarge = fmt.Errorf("%w: Pando cached a result larger than this client reads", ErrUnreadable)
	errCachePage     = fmt.Errorf("%w: a page of Pando's cached result could not be read", ErrUnreadable)
	errCacheSize     = fmt.Errorf("%w: Pando's cached result did not add up to the size it declared", ErrUnreadable)
)

// followCache pages the full text of a cached response back with cache_read,
// over sess. The caller's context carries the call's deadline.
func (c *Client) followCache(ctx context.Context, sess *mcpsdk.ClientSession, tool string, stub cachedStub) (string, error) {
	if stub.bytes > maxCachedBytes || stub.lines > maxCachePages*cachePageLines {
		return "", fmt.Errorf("%s: %w", tool, errCacheTooLarge)
	}
	lines := make([]string, 0, stub.lines)
	size := 0
	for page := 0; len(lines) < stub.lines; page++ {
		if page == maxCachePages {
			return "", fmt.Errorf("%s: %w", tool, errCacheTooLarge)
		}
		offset := len(lines)
		res, err := sess.CallTool(ctx, &mcpsdk.CallToolParams{Name: toolCacheRead, Arguments: map[string]any{
			"cache_id": stub.id,
			"offset":   offset,
			"limit":    cachePageLines,
		}})
		if err != nil {
			return "", fmt.Errorf("%s: %w", tool, c.classify(ctx, err))
		}
		if res.IsError {
			// Pando answers "cache entry not found: <id>" once the entry was
			// evicted or the session was rebuilt in between.
			return "", fmt.Errorf("%s: %w", tool, errCachePage)
		}
		got, err := parseCachePage(textOf(res), offset)
		if err != nil {
			return "", fmt.Errorf("%s: %w", tool, err)
		}
		if len(got) == 0 {
			break
		}
		for _, l := range got {
			size += len(l) + 1
		}
		if size > maxCachedBytes+1 {
			return "", fmt.Errorf("%s: %w", tool, errCacheTooLarge)
		}
		lines = append(lines, got...)
	}
	text := strings.Join(lines, "\n")
	if len(lines) != stub.lines || len(text) != stub.bytes {
		return "", fmt.Errorf("%s: %w", tool, errCacheSize)
	}
	return text, nil
}

// parseCachePage takes one cache_read answer apart. Pando writes a header
// line, a blank line and then every returned line as "%6d|<line>", numbered
// from offset+1, followed by a blank line and a trailer. Each number is
// checked, so a page that was not the one asked for is refused rather than
// spliced in.
func parseCachePage(text string, offset int) ([]string, error) {
	rows := strings.Split(text, "\n")
	m := cachePageHeader.FindStringSubmatch(rows[0])
	if m == nil {
		return nil, errCachePage
	}
	first, err1 := strconv.Atoi(m[1])
	last, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil || first != offset+1 {
		return nil, errCachePage
	}
	returned := last - offset
	if returned <= 0 {
		return nil, nil
	}
	if len(rows) < 2+returned || rows[1] != "" {
		return nil, errCachePage
	}
	out := make([]string, 0, returned)
	for i := 0; i < returned; i++ {
		prefix := fmt.Sprintf("%6d|", offset+i+1)
		row := rows[2+i]
		if !strings.HasPrefix(row, prefix) {
			return nil, errCachePage
		}
		out = append(out, row[len(prefix):])
	}
	return out, nil
}

// textOf concatenates the text content blocks of a tool result.
func textOf(res *mcpsdk.CallToolResult) string {
	var b strings.Builder
	for _, content := range res.Content {
		if tc, ok := content.(*mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
