package pando

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Pando's tool names and its own parameter caps. The caps are applied here so
// a caller that asks for more gets the most Pando can give rather than a
// server-side surprise.
const (
	toolKBSearch      = "kb_search_documents"
	toolCodeSearch    = "code_hybrid_search"
	toolCodeProjects  = "code_list_projects"
	toolCodeIndex     = "code_index_project"
	maxKBSearchLimit  = 20
	maxCodeSearchHits = 50
)

// toolResult is the decoded envelope of one tools/call response. It keeps the
// MCP result out of every other function in the package.
type toolResult struct {
	tool string
	text string
	// metadata is Pando's structuredContent.metadata: a JSON document some
	// tools carry alongside their human-readable text.
	metadata []byte
	// unread is set when the text was a cached-response stub that could not
	// be paged back while the metadata survived; decoding the text answers it.
	unread error
}

func newToolResult(tool string, res *mcpsdk.CallToolResult) (*toolResult, error) {
	text := textOf(res)
	if res.IsError {
		return nil, &toolError{Tool: tool, Message: strings.TrimSpace(text)}
	}
	out := &toolResult{tool: tool, text: text}
	if sc, ok := res.StructuredContent.(map[string]any); ok {
		if raw, ok := sc["metadata"].(string); ok && raw != "" {
			out.metadata = []byte(raw)
		}
	}
	return out, nil
}

// decode renders the tool's text content into the JSON data model. Pando emits
// TOON, so this is not a JSON unmarshal; see toon.go.
func (r *toolResult) decode() (any, error) {
	if r.unread != nil {
		return nil, r.unread
	}
	v, err := decodeStructured(r.text)
	if err != nil {
		return nil, fmt.Errorf("%w: %s returned a result this client cannot read: %w", ErrUnreadable, r.tool, err)
	}
	return v, nil
}

// object decodes the text content and insists it is an object. Pando answers
// "No documents found matching the query." and friends with a bare sentence
// rather than an empty result set, so a plain string that starts with "No " is
// reported as "no hits" instead of as a malformed result.
func (r *toolResult) object() (obj map[string]any, ok bool, err error) {
	v, err := r.decode()
	if err != nil {
		return nil, false, err
	}
	if obj, ok := v.(map[string]any); ok {
		return obj, true, nil
	}
	if strings.HasPrefix(strings.TrimSpace(r.text), "No ") {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("%w: %s returned an unexpected result: %.120q", ErrUnreadable, r.tool, r.text)
}

// remarshal moves a decoded document into a typed struct. Unknown fields are
// ignored, which is the whole point: Pando adds fields to these results without
// warning and this client must not break when it does.
func remarshal(tool string, v, into any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrUnreachable, tool, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("%w: %s returned a result this client cannot read: %w", ErrUnreadable, tool, err)
	}
	return nil
}

type kbHitWire struct {
	FilePath     string         `json:"file_path"`
	ChunkContent string         `json:"chunk_content"`
	Score        float64        `json:"score"`
	Rank         int            `json:"rank"`
	Tags         []string       `json:"tags"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
	Metadata     map[string]any `json:"metadata"`
	Links        int            `json:"links"`
	Backlinks    int            `json:"backlinks"`
}

type kbResultWire struct {
	Count   int         `json:"count"`
	Results []kbHitWire `json:"results"`
}

// SearchKB runs kb_search_documents. An empty result set is not an error: it
// returns a nil slice and a nil error, which is how a caller tells "nothing
// matched" from "the tool failed" (ErrToolFailed).
func (c *Client) SearchKB(ctx context.Context, query string, o KBSearchOptions) ([]KBHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: an empty query", ErrInvalidOptions)
	}
	args := map[string]any{"query": query}
	if o.Limit > 0 {
		args["limit"] = min(o.Limit, maxKBSearchLimit)
	}
	if len(o.Tags) > 0 {
		args["tags"] = o.Tags
	}
	if o.ExcludeOutdated != nil {
		args["exclude_outdated"] = *o.ExcludeOutdated
	}
	if o.Scope != "" {
		args["scope"] = o.Scope
	}
	if o.PathPrefix != "" {
		args["path_prefix"] = o.PathPrefix
	}
	if o.SortByDate {
		args["sort_by_date"] = true
	}

	res, err := c.call(ctx, toolKBSearch, args)
	if err != nil {
		return nil, err
	}
	obj, ok, err := res.object()
	if err != nil || !ok {
		return nil, err
	}
	var wire kbResultWire
	if err := remarshal(toolKBSearch, obj, &wire); err != nil {
		return nil, err
	}
	hits := make([]KBHit, 0, len(wire.Results))
	for _, r := range wire.Results {
		hits = append(hits, KBHit{
			FilePath:  r.FilePath,
			Chunk:     r.ChunkContent,
			Score:     r.Score,
			Rank:      r.Rank,
			Tags:      r.Tags,
			Metadata:  r.Metadata,
			CreatedAt: parseTime(r.CreatedAt),
			UpdatedAt: parseTime(r.UpdatedAt),
			Links:     r.Links,
			Backlinks: r.Backlinks,
		})
	}
	return hits, nil
}

type codeHitWire struct {
	SymbolType string  `json:"symbol_type"`
	Name       string  `json:"name"`
	NamePath   string  `json:"name_path"`
	FilePath   string  `json:"file_path"`
	StartLine  int     `json:"start_line"`
	Signature  string  `json:"signature"`
	Snippet    string  `json:"snippet"`
	Kind       string  `json:"kind"`
	Score      float64 `json:"score"`
	Rank       int     `json:"rank"`
}

type codeResultWire struct {
	Count   int           `json:"count"`
	Total   int           `json:"total"`
	Results []codeHitWire `json:"results"`
}

// SearchCode runs code_hybrid_search against one indexed project. An empty
// projectID falls back to Options.ProjectID.
//
// Unlike the knowledge-base tool, code_hybrid_search renders its text content
// as compact human-readable lines and puts the machine-readable result in
// structuredContent.metadata, so that is what is parsed here.
func (c *Client) SearchCode(ctx context.Context, projectID, query string, o CodeSearchOptions) ([]CodeHit, error) {
	if projectID == "" {
		projectID = c.opts.ProjectID
	}
	if projectID == "" {
		return nil, fmt.Errorf("%w: no code project id", ErrNotConfigured)
	}
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: an empty query", ErrInvalidOptions)
	}
	args := map[string]any{
		"project_id": projectID,
		"query":      query,
		// Always explicit: Pando's default excludes Markdown, which is not
		// what a caller asking for documentation would expect.
		"include_docs": o.IncludeDocs,
	}
	if o.Limit > 0 {
		args["limit"] = min(o.Limit, maxCodeSearchHits)
	}
	if o.Offset > 0 {
		args["offset"] = o.Offset
	}
	if len(o.Languages) > 0 {
		args["languages"] = o.Languages
	}
	if len(o.SymbolTypes) > 0 {
		args["symbol_types"] = o.SymbolTypes
	}
	if o.MinScore > 0 {
		args["min_score"] = o.MinScore
	}

	res, err := c.call(ctx, toolCodeSearch, args)
	if err != nil {
		return nil, err
	}

	var wire codeResultWire
	switch {
	case len(res.metadata) > 0:
		if err := json.Unmarshal(res.metadata, &wire); err != nil {
			return nil, fmt.Errorf("%w: %s returned metadata this client cannot read: %w",
				ErrUnreadable, toolCodeSearch, err)
		}
	default:
		// No metadata: either there were no hits (Pando answers with a bare
		// sentence) or a future Pando renders the result as structured text.
		obj, ok, err := res.object()
		if err != nil || !ok {
			return nil, err
		}
		if err := remarshal(toolCodeSearch, obj, &wire); err != nil {
			return nil, err
		}
	}

	hits := make([]CodeHit, 0, len(wire.Results))
	for _, r := range wire.Results {
		hits = append(hits, CodeHit(r))
	}
	return hits, nil
}

type projectWire struct {
	ProjectID      string         `json:"project_id"`
	Name           string         `json:"name"`
	RootPath       string         `json:"root_path"`
	IndexingStatus string         `json:"indexing_status"`
	LanguageStats  map[string]int `json:"language_stats"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	LastIndexedAt  string         `json:"last_indexed_at"`
}

type projectsWire struct {
	Count    int           `json:"count"`
	Projects []projectWire `json:"projects"`
}

// ListProjects runs code_list_projects.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	res, err := c.call(ctx, toolCodeProjects, map[string]any{})
	if err != nil {
		return nil, err
	}
	obj, ok, err := res.object()
	if err != nil || !ok {
		return nil, err
	}
	var wire projectsWire
	if err := remarshal(toolCodeProjects, obj, &wire); err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(wire.Projects))
	for _, p := range wire.Projects {
		out = append(out, Project{
			ProjectID:      p.ProjectID,
			Name:           p.Name,
			RootPath:       p.RootPath,
			IndexingStatus: p.IndexingStatus,
			LanguageStats:  p.LanguageStats,
			CreatedAt:      parseTime(p.CreatedAt),
			UpdatedAt:      parseTime(p.UpdatedAt),
			LastIndexedAt:  parseTime(p.LastIndexedAt),
		})
	}
	return out, nil
}

type indexJobWire struct {
	JobID     string `json:"job_id"`
	ProjectID string `json:"project_id"`
	Status    string `json:"status"`
}

// IndexProject runs code_index_project and returns the identifier of the
// indexing job Pando started. Indexing is asynchronous: the job is still
// running when this returns.
//
// name may be empty, in which case Pando derives the project id from path with
// the same rule as SanitizeProjectID.
func (c *Client) IndexProject(ctx context.Context, path, name string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: an empty project path", ErrInvalidOptions)
	}
	args := map[string]any{"project_path": path}
	if name != "" {
		args["project_name"] = name
	}
	res, err := c.call(ctx, toolCodeIndex, args)
	if err != nil {
		return "", err
	}
	obj, ok, err := res.object()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w: %s returned no job: %.120q", ErrUnreadable, toolCodeIndex, res.text)
	}
	var wire indexJobWire
	if err := remarshal(toolCodeIndex, obj, &wire); err != nil {
		return "", err
	}
	if wire.JobID == "" {
		return "", fmt.Errorf("%w: %s returned no job id", ErrUnreadable, toolCodeIndex)
	}
	return wire.JobID, nil
}

// parseTime accepts Pando's RFC 3339 timestamps and returns the zero time for
// anything it cannot read, because a timestamp is never worth failing a search
// over.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}
