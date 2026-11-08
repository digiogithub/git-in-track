package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The gintrack-hub tools (ADR-043 §6, GIT-US-0212): knowledge-base writes and
// the shared memory of ADR-044, plus a token-budgeted project context. They
// are registered only when Options.HubTools is set, so the local `gintrack
// mcp` keeps its tool count; the hub's Guard authorizes every call they make.

// WriteKBPageInput creates or replaces one knowledge-base page.
type WriteKBPageInput struct {
	Project string `json:"project" jsonschema:"Project (or team) key owning the page"`
	Path    string `json:"path" jsonschema:"Path relative to the project's documentation folder, for example architecture/auth.md"`
	Text    string `json:"text" jsonschema:"The whole page, Markdown"`
	Rev     string `json:"rev,omitempty" jsonschema:"The rev get_kb_page returned. Required to replace an existing page; omit only to create a new one"`
}

// KBWriteResult is what a page write produced.
type KBWriteResult struct {
	Path string `json:"path"`
	Rev  string `json:"rev"`
}

// MemoryInput is one memory an agent records.
type MemoryInput struct {
	Project    string   `json:"project" jsonschema:"Project key, or the team key for organization memory"`
	Key        string   `json:"key" jsonschema:"Stable slug the memory is upserted by, unique in its scope, e.g. customer-x-invoicing"`
	Content    string   `json:"content" jsonschema:"The fact, in a sentence or a short paragraph"`
	Title      string   `json:"title,omitempty" jsonschema:"One line; derived from the content when omitted"`
	Labels     []string `json:"labels,omitempty"`
	Supersedes []string `json:"supersedes,omitempty" jsonschema:"Keys or ids of active memories this one replaces"`
	SourceRef  string   `json:"sourceRef,omitempty" jsonschema:"Where the fact comes from: an item id, a page path, a URL"`
}

// UpdateMemoryInput changes an existing memory under an optimistic lock.
type UpdateMemoryInput struct {
	Project string   `json:"project"`
	Key     string   `json:"key"`
	Rev     string   `json:"rev" jsonschema:"The rev of the memory as you read it"`
	Content string   `json:"content"`
	Title   string   `json:"title,omitempty"`
	Labels  []string `json:"labels,omitempty"`
}

// ForgetMemoryInput withdraws a memory.
type ForgetMemoryInput struct {
	Project string `json:"project"`
	Key     string `json:"key"`
	Rev     string `json:"rev,omitempty" jsonschema:"The rev as you read it; recommended"`
}

// ReviewMemoryInput accepts or rejects a proposed memory.
type ReviewMemoryInput struct {
	ID  string `json:"id" jsonschema:"Memory id, e.g. ACME-MEM-0042"`
	Rev string `json:"rev"`
}

// SearchMemoryInput lists the memories of a scope matching words.
type SearchMemoryInput struct {
	Project  string   `json:"project"`
	Query    string   `json:"query,omitempty" jsonschema:"Words to find in key, title or content; omit for every memory"`
	Statuses []string `json:"statuses,omitempty" jsonschema:"proposed, active, superseded, cancelled, forgotten; default active"`
	Limit    int      `json:"limit,omitempty" jsonschema:"1 to 100; default 20"`
}

// ProjectContextInput asks for the context an agent needs to start on a project.
type ProjectContextInput struct {
	Project string `json:"project"`
	Budget  int    `json:"budget,omitempty" jsonschema:"Approximate token budget, 200 to 8000; default 1500"`
}

// MemoryRow is one memory, compactly.
type MemoryRow struct {
	ID      string   `json:"id"`
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	Status  string   `json:"status"`
	Version int64    `json:"version"`
	Rev     string   `json:"rev"`
	Labels  []string `json:"labels,omitempty"`
	Content string   `json:"content,omitempty"`
}

// MemoryWriteOutcome is the outcome of one memory write.
type MemoryWriteOutcome struct {
	Outcome string `json:"outcome"`
	ID      string `json:"id,omitempty"`
	Status  string `json:"status,omitempty"`
	Version int64  `json:"version,omitempty"`
	Rev     string `json:"rev,omitempty"`
	// Note explains a write the server changed, such as a memory recorded as a
	// proposal because the caller may only propose.
	Note string `json:"note,omitempty"`
}

// MemoryList is the answer of search_memory.
type MemoryList struct {
	Memories []MemoryRow `json:"memories"`
	Total    int         `json:"total"`
}

// ProjectContext is the answer of project_context.
type ProjectContext struct {
	Project   string      `json:"project"`
	Memories  []MemoryRow `json:"memories"`
	Pages     []PageEntry `json:"pages"`
	Pending   int         `json:"pendingProposals"`
	Truncated bool        `json:"truncated"`
}

// registerHubTools declares the hub-only surface.
func registerHubTools(s *Server) {
	if !s.hubTools {
		return
	}
	register(s, toolDef{
		Name: "write_kb_page", Title: "Write a knowledge-base page", Write: true,
		Description: "Create a knowledge-base page, or replace one quoting the rev get_kb_page returned. " +
			"Without a rev an existing page is never overwritten: the call fails with already_exists.",
	}, writeKBPage)
	register(s, toolDef{
		Name: "add_memory", Title: "Record a memory", Write: true,
		Description: "Record a fact in the shared memory, active at once when you may write it. When you may " +
			"only propose, it is recorded as a proposal for a person to review, and the answer says so.",
	}, func(ctx context.Context, s *Server, in MemoryInput) (MemoryWriteOutcome, error) {
		return putMemory(ctx, s, in, "active")
	})
	register(s, toolDef{
		Name: "propose_memory", Title: "Propose a memory", Write: true,
		Description: "Propose a fact for the shared memory. A person accepts or rejects it in the inbox; until " +
			"then nobody else sees it.",
	}, func(ctx context.Context, s *Server, in MemoryInput) (MemoryWriteOutcome, error) {
		return putMemory(ctx, s, in, "proposed")
	})
	register(s, toolDef{
		Name: "update_memory", Title: "Update a memory", Write: true,
		Description: "Change the content of a memory, quoting its rev. A stale rev fails with stale_revision.",
	}, updateMemory)
	register(s, toolDef{
		Name: "forget_memory", Title: "Forget a memory", Write: true,
		Description: "Withdraw a memory that no longer holds. The file and its history stay; a proposal is withdrawn instead.",
	}, forgetMemory)
	register(s, toolDef{
		Name: "accept_memory", Title: "Accept a proposed memory", Write: true,
		Description: "Accept a proposed memory, making it active and visible to everyone in its scope.",
	}, func(ctx context.Context, s *Server, in ReviewMemoryInput) (MemoryWriteOutcome, error) {
		return reviewMemory(ctx, s, in, "accept")
	})
	register(s, toolDef{
		Name: "reject_memory", Title: "Reject a proposed memory", Write: true,
		Description: "Reject a proposed memory. It can be proposed again later.",
	}, func(ctx context.Context, s *Server, in ReviewMemoryInput) (MemoryWriteOutcome, error) {
		return reviewMemory(ctx, s, in, "reject")
	})
	register(s, toolDef{
		Name: "search_memory", Title: "Search the shared memory",
		Description: "List the memories of a project or team that contain the words, active ones by default.",
		Untrusted:   true,
	}, searchMemory)
	register(s, toolDef{
		Name: "project_context", Title: "Get the context of a project",
		Description: "What an agent needs to start on a project, inside a token budget: its active memories, the " +
			"titles of its knowledge-base pages and how many proposals wait for review. Read a page with get_kb_page.",
		Untrusted: true,
	}, projectContext)
}

// memoryRecord is the core's memory item, as much of it as these tools read.
type memoryRecord struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Status string   `json:"status"`
	Rev    string   `json:"rev"`
	Labels []string `json:"labels"`
	Body   string   `json:"body"`
	Memory *struct {
		Key     string `json:"key"`
		Version int64  `json:"version"`
	} `json:"memory"`
}

func (m memoryRecord) row(withContent bool) MemoryRow {
	r := MemoryRow{ID: m.ID, Title: m.Title, Status: m.Status, Rev: m.Rev, Labels: m.Labels}
	if m.Memory != nil {
		r.Key, r.Version = m.Memory.Key, m.Memory.Version
	}
	if withContent {
		r.Content = strings.TrimSpace(m.Body)
	}
	return r
}

// putResult is the core's memory.put answer.
type putResult struct {
	Results []struct {
		Outcome string `json:"outcome"`
		ID      string `json:"id"`
		Version int64  `json:"version"`
		Rev     string `json:"rev"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"results"`
}

// memoryWrite sends one entry and turns its per-item answer into the tool's.
func memoryWrite(ctx context.Context, s *Server, tool, project string, entry map[string]any) (MemoryWriteOutcome, error) {
	raw, err := s.dispatchRaw(ctx, "memory.put", map[string]any{"project": project, "items": []any{entry}})
	if err != nil {
		return MemoryWriteOutcome{}, err
	}
	var put putResult
	if err := reencode(raw, &put); err != nil || len(put.Results) != 1 {
		return MemoryWriteOutcome{}, failf("internal", "memory.put answered %d results", len(put.Results))
	}
	r := put.Results[0]
	if r.Outcome == "rejected" {
		return MemoryWriteOutcome{}, failf(r.Code, "%s", r.Message)
	}
	out := MemoryWriteOutcome{Outcome: r.Outcome, ID: r.ID, Version: r.Version, Rev: r.Rev}
	if r.ID != "" {
		if got, err := dispatch[memoryRecord](ctx, s, "memory.get", map[string]any{"project": project, "id": r.ID}); err == nil {
			out.Status = got.Status
		}
	}
	if r.Outcome != "duplicate" {
		s.announce(ctx, WriteEvent{Tool: tool, Method: "memory.put", ItemID: r.ID, Project: project, Op: "updated", Result: raw})
	}
	return out, nil
}

func putMemory(ctx context.Context, s *Server, in MemoryInput, status string) (MemoryWriteOutcome, error) {
	if strings.TrimSpace(in.Content) == "" {
		return MemoryWriteOutcome{}, invalidField("content", "a memory needs content", "Customer X is invoiced quarterly.")
	}
	entry := map[string]any{
		"key": in.Key, "content": in.Content, "title": in.Title, "labels": in.Labels,
		"status": status, "supersedes": in.Supersedes, "author": s.authorName(""),
		"source": map[string]any{"kind": "agent", "ref": firstNonEmpty(in.SourceRef, s.agent)},
	}
	tool := "add_memory"
	if status == "proposed" {
		tool = "propose_memory"
	}
	out, err := memoryWrite(ctx, s, tool, in.Project, entry)
	if err == nil && status == "active" && out.Status == "proposed" {
		out.Note = "recorded as a proposal: you may only propose memories in this scope, and a person reviews it in the inbox"
	}
	return out, err
}

func updateMemory(ctx context.Context, s *Server, in UpdateMemoryInput) (MemoryWriteOutcome, error) {
	if in.Rev == "" {
		return MemoryWriteOutcome{}, invalidField("rev", "update_memory needs the rev you read", "sha256:…")
	}
	return memoryWrite(ctx, s, "update_memory", in.Project, map[string]any{
		"key": in.Key, "content": in.Content, "title": in.Title, "labels": in.Labels, "rev": in.Rev,
	})
}

func forgetMemory(ctx context.Context, s *Server, in ForgetMemoryInput) (MemoryWriteOutcome, error) {
	return memoryWrite(ctx, s, "forget_memory", in.Project, map[string]any{"op": "forget", "key": in.Key, "rev": in.Rev})
}

func reviewMemory(ctx context.Context, s *Server, in ReviewMemoryInput, action string) (MemoryWriteOutcome, error) {
	project, _, found := strings.Cut(in.ID, "-MEM-")
	if !found {
		return MemoryWriteOutcome{}, invalidField("id", "a memory id looks like ACME-MEM-0042", "ACME-MEM-0042")
	}
	current, err := dispatch[memoryRecord](ctx, s, "memory.get", map[string]any{"project": project, "id": in.ID})
	if err != nil {
		return MemoryWriteOutcome{}, err
	}
	if current.Status != "proposed" {
		return MemoryWriteOutcome{}, failf("invalid_request", "%s is %s, not a proposal", in.ID, current.Status)
	}
	result, err := s.dispatchRaw(ctx, "inbox.triage", map[string]any{"id": in.ID, "rev": in.Rev, "action": action})
	if err != nil {
		return MemoryWriteOutcome{}, err
	}
	s.announce(ctx, WriteEvent{Tool: action + "_memory", Method: "inbox.triage", ItemID: in.ID, Op: "updated", Result: result})
	after, err := dispatch[memoryRecord](ctx, s, "memory.get", map[string]any{"project": project, "id": in.ID})
	if err != nil {
		return MemoryWriteOutcome{}, err
	}
	row := after.row(false)
	return MemoryWriteOutcome{Outcome: "updated", ID: row.ID, Status: row.Status, Version: row.Version, Rev: row.Rev}, nil
}

func searchMemory(ctx context.Context, s *Server, in SearchMemoryInput) (MemoryList, error) {
	statuses := in.Statuses
	if len(statuses) == 0 {
		statuses = []string{"active"}
	}
	listed, err := dispatch[struct {
		Items []memoryRecord `json:"items"`
	}](ctx, s, "memory.list", map[string]any{"project": in.Project, "statuses": statuses})
	if err != nil {
		return MemoryList{}, err
	}
	words := strings.Fields(strings.ToLower(in.Query))
	out := MemoryList{Memories: []MemoryRow{}}
	for _, m := range listed.Items {
		row := m.row(true)
		hay := strings.ToLower(row.Key + " " + row.Title + " " + row.Content)
		match := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				match = false
				break
			}
		}
		if match {
			out.Memories = append(out.Memories, row)
		}
	}
	out.Total = len(out.Memories)
	if limit := boundedLimit(in.Limit); len(out.Memories) > limit {
		out.Memories = out.Memories[:limit]
	}
	return out, nil
}

// projectContext assembles the starting context of a project, cheapest first,
// and stops at the budget. A token is estimated as four characters.
func projectContext(ctx context.Context, s *Server, in ProjectContextInput) (ProjectContext, error) {
	budget := in.Budget
	switch {
	case budget == 0:
		budget = 1500
	case budget < 200:
		budget = 200
	case budget > 8000:
		budget = 8000
	}
	remaining := budget * 4
	out := ProjectContext{Project: in.Project, Memories: []MemoryRow{}, Pages: []PageEntry{}}

	listed, err := dispatch[struct {
		Items []memoryRecord `json:"items"`
	}](ctx, s, "memory.list", map[string]any{"project": in.Project})
	if err != nil {
		return ProjectContext{}, err
	}
	for _, m := range listed.Items {
		if m.Status == "proposed" {
			out.Pending++
		}
	}
	for _, m := range listed.Items {
		if m.Status != "active" {
			continue
		}
		row := m.row(true)
		cost := len(row.Key) + len(row.Title) + len(row.Content) + 40
		if cost > remaining {
			out.Truncated = true
			break
		}
		remaining -= cost
		out.Memories = append(out.Memories, row)
	}

	if nodes, err := dispatch[[]kbNode](ctx, s, "kb.tree", map[string]any{"project": in.Project}); err == nil {
		var pages []PageEntry
		for _, n := range nodes {
			collectPages(n, &pages)
		}
		sort.Slice(pages, func(i, j int) bool { return pages[i].Path < pages[j].Path })
		for _, p := range pages {
			cost := len(p.Path) + len(p.Title) + 20
			if cost > remaining {
				out.Truncated = true
				break
			}
			remaining -= cost
			out.Pages = append(out.Pages, p)
		}
	}
	return out, nil
}

// writeKBPage writes a page through kb.write. Without a rev it only creates:
// the core would overwrite an existing page blindly, so the tool checks first.
func writeKBPage(ctx context.Context, s *Server, in WriteKBPageInput) (KBWriteResult, error) {
	if _, err := s.guard.Check("path", in.Path); err != nil {
		return KBWriteResult{}, err
	}
	if in.Project == "" {
		return KBWriteResult{}, invalidField("project", "write_kb_page needs the project that owns the page", "ACME")
	}
	tree, err := dispatch[[]kbNode](ctx, s, "kb.tree", map[string]any{"project": in.Project})
	if err != nil {
		return KBWriteResult{}, err
	}
	var pages []PageEntry
	for _, n := range tree {
		collectPages(n, &pages)
	}
	full := ""
	for _, p := range pages {
		if strings.HasSuffix(p.Path, "/"+strings.TrimPrefix(in.Path, "/")) || p.Path == in.Path {
			full = p.Path
		}
	}
	if full != "" && in.Rev == "" {
		return KBWriteResult{}, failf("already_exists",
			"%s already exists: read it with get_kb_page and pass its rev to replace it", in.Path)
	}
	docs, vaultID, err := s.docsFolder(ctx, in.Project)
	if err != nil {
		return KBWriteResult{}, err
	}
	target := full
	if target == "" {
		target = strings.TrimPrefix(docs+"/"+strings.TrimPrefix(in.Path, "/"), "./")
	}
	result, err := s.dispatchRaw(ctx, "kb.write", map[string]any{
		"path": target, "text": in.Text, "rev": in.Rev, "vaultId": vaultID,
	})
	if err != nil {
		return KBWriteResult{}, err
	}
	var written struct {
		Page struct {
			Path string `json:"path"`
			Rev  string `json:"rev"`
		} `json:"page"`
	}
	if err := reencode(result, &written); err != nil {
		return KBWriteResult{}, failf("internal", "kb.write answered an unexpected shape: %v", err)
	}
	s.announce(ctx, WriteEvent{Tool: "write_kb_page", Method: "kb.write", Project: in.Project, Op: "updated", Result: result})
	return KBWriteResult{Path: written.Page.Path, Rev: written.Page.Rev}, nil
}

// docsFolder is the documentation folder of a project and the repository that
// holds it, from project.list.
func (s *Server) docsFolder(ctx context.Context, project string) (docs, vaultID string, err error) {
	listed, err := dispatch[[]struct {
		Key      string `json:"key"`
		DocsPath string `json:"docsPath"`
		VaultID  string `json:"vaultId"`
	}](ctx, s, "project.list", map[string]any{"project": project})
	if err != nil {
		return "", "", err
	}
	for _, p := range listed {
		if p.Key == project {
			if p.DocsPath == "" {
				p.DocsPath = "."
			}
			return p.DocsPath, p.VaultID, nil
		}
	}
	return "", "", failf("not_found", "project %s is not open", project)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// reencode projects a core value onto a local type through JSON.
func reencode(v, dst any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}
