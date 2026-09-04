package mcp

import (
	"context"
	"sort"
	"strings"
)

// The knowledge-base tools. The backlog lives under `.pmngr/`; everything else
// under a project's documentation folder, and under a team repository's
// knowledge folder, is the knowledge base — ordinary Markdown pages an agent
// reads for context.
//
// These are the only tools that take a path, so they are the only ones that go
// through the path guard. A path that leaves the vault root is refused before
// the core is asked anything.

// ListKBPagesInput lists the pages of the knowledge base.
type ListKBPagesInput struct {
	Project string `json:"project,omitempty" jsonschema:"Project key; omit for every mounted repository"`
	Prefix  string `json:"prefix,omitempty" jsonschema:"Only pages under this vault-relative folder, for example docs/architecture"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Page size, 1 to 100; default 20"`
	Cursor  string `json:"cursor,omitempty"`
}

// PageEntry is one page in a listing: enough to decide whether to read it.
type PageEntry struct {
	Path  string `json:"path"`
	Title string `json:"title,omitempty"`
}

// PageList is one page of the knowledge-base listing.
type PageList struct {
	Pages      []PageEntry `json:"pages"`
	Total      int         `json:"total"`
	NextCursor string      `json:"nextCursor,omitempty"`
}

// GetKBPageInput addresses one knowledge-base page.
type GetKBPageInput struct {
	Path string `json:"path" jsonschema:"Vault-relative path, for example docs/architecture/auth.md"`
	// Project narrows the search to one repository. Paths are relative to a
	// repository root, so two repositories can hold the same path; without a
	// project the tool reads the first repository that has it.
	Project string `json:"project,omitempty" jsonschema:"Project key owning the page"`
	Body    bool   `json:"body,omitempty" jsonschema:"Return the Markdown body; omit for metadata and links only"`
}

// SearchKBInput is a ranked query over the knowledge base.
type SearchKBInput struct {
	Query   string `json:"query" jsonschema:"Words to search for in page titles and bodies"`
	Project string `json:"project,omitempty"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Page size, 1 to 100; default 20"`
	Cursor  string `json:"cursor,omitempty"`
}

// registerKBTools declares the knowledge-base half of the surface.
func registerKBTools(s *Server) {
	register(s, toolDef{
		Name:  "list_kb_pages",
		Title: "List knowledge-base pages",
		Description: "List the Markdown pages of the knowledge base, optionally under one folder. " +
			"Backlog items are not pages: reach them with list_items. Titles only; read a page with get_kb_page.",
		Untrusted: true,
	}, listKBPages)

	register(s, toolDef{
		Name:  "get_kb_page",
		Title: "Read a knowledge-base page",
		Description: "Read one knowledge-base page by its vault-relative path, with its wikilink " +
			"neighborhood. The body is returned only when body is true. Paths are confined to the " +
			"repositories this server mounts; anything outside them is refused.",
		Untrusted: true,
	}, getKBPage)

	register(s, toolDef{
		Name:        "search_kb",
		Title:       "Search the knowledge base",
		Description: "Ranked full-text search over knowledge-base pages, with an excerpt around each match.",
		Untrusted:   true,
	}, searchKB)
}

// listKBPages flattens the knowledge-base tree the core reports into a sorted,
// paginated list of pages. Flattening is projection, not domain logic: the tree
// itself, its scoping and its exclusions all come from the index.
func listKBPages(ctx context.Context, s *Server, in ListKBPagesInput) (PageList, error) {
	prefix := ""
	if in.Prefix != "" {
		clean, err := s.guard.Check("prefix", in.Prefix)
		if err != nil {
			return PageList{}, err
		}
		prefix = clean
	}
	var pages []PageEntry
	for _, params := range s.kbScopes(ctx, in.Project) {
		nodes, err := dispatch[[]kbNode](ctx, s, "kb.tree", params)
		if err != nil {
			// One repository that cannot answer must not hide the others: a
			// workspace holding a broken clone still lists the pages of the
			// clones that work.
			continue
		}
		for _, n := range nodes {
			collectPages(n, &pages)
		}
	}
	if prefix != "" {
		kept := pages[:0]
		for _, p := range pages {
			if p.Path == prefix || strings.HasPrefix(p.Path, prefix+"/") {
				kept = append(kept, p)
			}
		}
		pages = kept
	}
	// A stable order is what makes a cursor walk meaningful and two identical
	// calls return identical bytes.
	sort.Slice(pages, func(i, j int) bool { return pages[i].Path < pages[j].Path })
	pages = dedupePages(pages)

	filter := fingerprint("list_kb_pages", in.Project, prefix)
	offset, err := decodeCursor(in.Cursor, filter)
	if err != nil {
		return PageList{}, err
	}
	page, next := slice(pages, offset, boundedLimit(in.Limit), filter)
	return PageList{Pages: page, Total: len(pages), NextCursor: next}, nil
}

// dedupePages drops the repeats a merge across repositories can produce when
// two of them hold the same path. The list is already sorted.
func dedupePages(pages []PageEntry) []PageEntry {
	out := pages[:0]
	for i, p := range pages {
		if i > 0 && pages[i-1].Path == p.Path {
			continue
		}
		out = append(out, p)
	}
	return out
}

// kbScopes returns the parameter sets a knowledge-base read has to try. A call
// that names a project routes straight to the repository that owns it; a call
// that names none is answered by every open repository, because a path is
// relative to a repository root and the tool cannot know which one was meant.
func (s *Server) kbScopes(ctx context.Context, project string) []map[string]any {
	if project != "" {
		return []map[string]any{{"project": project}}
	}
	ids := s.vaultIDs(ctx)
	if len(ids) == 0 {
		return []map[string]any{{}}
	}
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]any{"vaultId": id})
	}
	return out
}

// vaultIDs lists the repositories the workspace has open. A core that does not
// implement the workspace surface — a single vault in a test, the browser
// before a second folder is opened — reports none, and the caller then makes
// one unrouted call.
func (s *Server) vaultIDs(ctx context.Context) []string {
	listed, err := dispatch[struct {
		Vaults []struct {
			ID string `json:"id"`
		} `json:"vaults"`
	}](ctx, s, "workspace.list", map[string]any{})
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(listed.Vaults))
	for _, v := range listed.Vaults {
		out = append(out, v.ID)
	}
	return out
}

// kbNode is one node of the tree the core reports.
type kbNode struct {
	Path     string   `json:"path"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	Children []kbNode `json:"children"`
}

// collectPages walks a tree node, appending its pages.
func collectPages(n kbNode, out *[]PageEntry) {
	if n.Kind == "page" {
		title := n.Title
		if title == "" {
			title = n.Name
		}
		*out = append(*out, PageEntry{Path: n.Path, Title: title})
	}
	for _, child := range n.Children {
		collectPages(child, out)
	}
}

// getKBPage reads one page. The path is checked before anything else: the MCP
// server must never become a general file-read primitive, so a path argument is
// confined to the mounted repositories whether or not the index would have
// found it anyway.
func getKBPage(ctx context.Context, s *Server, in GetKBPageInput) (Page, error) {
	clean, err := s.guard.Check("path", in.Path)
	if err != nil {
		return Page{}, err
	}
	page, err := s.readPage(ctx, clean, in.Project)
	if err != nil {
		return Page{}, err
	}
	out := Page{
		Path: page.Path, Title: page.Title, Rev: page.Rev, Project: page.Project,
		Outgoing: page.Outgoing, Backlinks: page.Backlinks,
	}
	if in.Body {
		out.Body = page.Body
	}
	return out, nil
}

// kbPageResult is the answer of "kb.page".
type kbPageResult struct {
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Rev       string   `json:"rev"`
	Outgoing  []string `json:"outgoing"`
	Backlinks []string `json:"backlinks"`
	Project   string   `json:"project"`
}

// readPage reads one page from the repository that owns it, trying each open
// repository when the caller named no project. The last failure is reported, so
// an unknown path still comes back as not_found rather than as nothing.
func (s *Server) readPage(ctx context.Context, clean, project string) (kbPageResult, error) {
	var last error
	for _, params := range s.kbScopes(ctx, project) {
		params["path"] = clean
		page, err := dispatch[kbPageResult](ctx, s, "kb.page", params)
		if err != nil {
			last = err
			continue
		}
		return page, nil
	}
	if last == nil {
		last = failf(codeNotFound, "page %s is not indexed", clean)
	}
	return kbPageResult{}, last
}

// searchKB runs the shared ranked search and keeps the page hits.
func searchKB(ctx context.Context, s *Server, in SearchKBInput) (HitPage, error) {
	if strings.TrimSpace(in.Query) == "" {
		return HitPage{}, invalidField("query", "search needs a query", "token refresh rotation")
	}
	hits, err := searchHits(ctx, s, in.Query, in.Project)
	if err != nil {
		return HitPage{}, err
	}
	pages := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if h.Kind != "page" {
			continue
		}
		pages = append(pages, h)
	}
	filter := fingerprint("search_kb", in.Query, in.Project)
	offset, err := decodeCursor(in.Cursor, filter)
	if err != nil {
		return HitPage{}, err
	}
	page, next := slice(pages, offset, boundedLimit(in.Limit), filter)
	for i := range page {
		page[i].Rev = s.pageRev(ctx, page[i].Path, page[i].Project)
	}
	return HitPage{Results: page, Total: len(pages), NextCursor: next}, nil
}

// pageRev reads the revision of a page, for the results that carry a path but
// not the page itself. A lookup that fails leaves the rev empty rather than
// failing the page of results.
func (s *Server) pageRev(ctx context.Context, p, project string) string {
	clean, err := s.guard.Check("path", p)
	if err != nil {
		return ""
	}
	page, err := s.readPage(ctx, clean, project)
	if err != nil {
		return ""
	}
	return page.Rev
}
