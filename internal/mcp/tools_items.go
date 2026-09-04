package mcp

import (
	"context"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The backlog tools: the ones an agent uses to find work, read it, create it,
// change it and report on it. Every one of them is a shim: it validates the
// framing (page size, path, projection), dispatches one method of the shared
// core, and projects the answer. Filters, workflow rules, id allocation and rev
// computation all happen in internal/core, once.

// ------------------------------------------------------------------ input ---

// ListItemsInput filters the backlog. Filters are AND across fields and OR
// within a repeated field, exactly as the REST API and the web UI apply them.
type ListItemsInput struct {
	Project      string   `json:"project,omitempty" jsonschema:"Project key, for example ACME"`
	Type         []string `json:"type,omitempty" jsonschema:"epic, story, task or milestone"`
	Status       []string `json:"status,omitempty" jsonschema:"Workflow statuses declared by the project"`
	Category     []string `json:"category,omitempty" jsonschema:"Coarse status categories: todo, in_progress, done, cancelled"`
	Priority     []string `json:"priority,omitempty" jsonschema:"critical, high, medium or low"`
	Assignee     string   `json:"assignee,omitempty"`
	Label        []string `json:"label,omitempty"`
	Parent       string   `json:"parent,omitempty" jsonschema:"Id of the owning epic or story"`
	Milestone    string   `json:"milestone,omitempty"`
	Text         string   `json:"text,omitempty" jsonschema:"Substring match over title and body"`
	UpdatedSince string   `json:"updatedSince,omitempty" jsonschema:"RFC 3339 timestamp or a duration such as 7d"`
	Sort         string   `json:"sort,omitempty" jsonschema:"Field to sort by; default updated"`
	Order        string   `json:"order,omitempty" jsonschema:"asc or desc; default desc"`
	Limit        int      `json:"limit,omitempty" jsonschema:"Page size, 1 to 100; default 20"`
	Cursor       string   `json:"cursor,omitempty" jsonschema:"nextCursor from the previous page"`
	Fields       []string `json:"fields,omitempty" jsonschema:"Fields to project; id and rev are always returned. Bodies are never returned by this tool"`
}

// ItemPage is one page of items.
type ItemPage struct {
	Items      []Item `json:"items"`
	Total      int    `json:"total" jsonschema:"Number of items the filter matches"`
	NextCursor string `json:"nextCursor,omitempty" jsonschema:"Pass back as cursor to fetch the next page"`
}

// SearchItemsInput is a ranked full-text query over the backlog.
type SearchItemsInput struct {
	Query   string `json:"query" jsonschema:"Words to search for in ids, titles, labels and bodies"`
	Project string `json:"project,omitempty"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Page size, 1 to 100; default 20"`
	Cursor  string `json:"cursor,omitempty"`
}

// HitPage is one page of ranked results.
type HitPage struct {
	Results    []Hit  `json:"results"`
	Total      int    `json:"total"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// GetItemInput addresses one item.
type GetItemInput struct {
	ID      string   `json:"id" jsonschema:"Permanent item id, for example ACME-US-0042"`
	Include []string `json:"include,omitempty" jsonschema:"Extra sections to return: body, comments, children"`
	Fields  []string `json:"fields,omitempty" jsonschema:"Front-matter fields to project; id and rev are always returned"`
}

// ItemResult is one item with the optional sections a read asked for.
type ItemResult struct {
	Item     Item      `json:"item"`
	Comments []Comment `json:"comments,omitempty"`
	Children []Item    `json:"children,omitempty"`
}

// CreateItemInput is the draft of a new epic, story or task. The id is never
// supplied by the caller: it is allocated by the core, which is the only thing
// that can do it without racing another writer.
type CreateItemInput struct {
	Project   string   `json:"project,omitempty" jsonschema:"Project key; required when the workspace holds more than one"`
	Title     string   `json:"title" jsonschema:"One-line summary"`
	Body      string   `json:"body,omitempty" jsonschema:"Markdown body, conventionally ## Description, ## Acceptance Criteria and ## Notes"`
	Status    string   `json:"status,omitempty" jsonschema:"A status the project declares; default is the initial status of its workflow"`
	Priority  string   `json:"priority,omitempty"`
	Parent    string   `json:"parent,omitempty" jsonschema:"Owning epic for a story, owning story for a task"`
	Milestone string   `json:"milestone,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Estimate  *float64 `json:"estimate,omitempty" jsonschema:"Story points"`
	Due       string   `json:"due,omitempty" jsonschema:"Date as YYYY-MM-DD"`
	Author    string   `json:"author,omitempty" jsonschema:"Defaults to the agent name the server was started with"`
}

// UpdateItemInput is a sparse patch. Only the keys present are changed, which
// is what keeps an agent's diff to the lines it meant to touch.
type UpdateItemInput struct {
	ID    string `json:"id"`
	Rev   string `json:"rev,omitempty" jsonschema:"The rev returned by the read this change is based on"`
	Title string `json:"title,omitempty"`
	// Status is applied through the workflow, so an undeclared transition is
	// refused with the transitions the project does allow.
	Status    string   `json:"status,omitempty" jsonschema:"Target status; the project workflow validates the transition"`
	Priority  string   `json:"priority,omitempty"`
	Parent    string   `json:"parent,omitempty"`
	Milestone string   `json:"milestone,omitempty"`
	Assignees []string `json:"assignees,omitempty" jsonschema:"Replaces the assignee list"`
	Labels    []string `json:"labels,omitempty" jsonschema:"Replaces the label list"`
	Estimate  *float64 `json:"estimate,omitempty"`
	Effort    *float64 `json:"effort,omitempty"`
	Due       string   `json:"due,omitempty"`
	Body      *string  `json:"body,omitempty" jsonschema:"Replaces the whole Markdown body"`
	Unset     []string `json:"unset,omitempty" jsonschema:"Front-matter fields to remove"`
}

// WriteResult is what a mutation answers with: the item as it now stands, and
// the files that changed.
type WriteResult struct {
	Item    Item     `json:"item"`
	Changed []string `json:"changed,omitempty" jsonschema:"Vault-relative paths written by this call"`
}

// AddCommentInput appends one comment to an item's thread. Comments are
// separate files; nothing is ever appended to an item body.
type AddCommentInput struct {
	ID     string `json:"id"`
	Body   string `json:"body" jsonschema:"Markdown text of the comment"`
	Author string `json:"author,omitempty" jsonschema:"Defaults to the agent name the server was started with"`
}

// CommentResult is the comment a write created.
type CommentResult struct {
	Comment Comment  `json:"comment"`
	Changed []string `json:"changed,omitempty"`
}

// ---------------------------------------------------------------- registry --

// registerItemTools declares the backlog half of the surface.
func registerItemTools(s *Server) {
	register(s, toolDef{
		Name:  "list_items",
		Title: "List backlog items",
		Description: "List epics, stories, tasks and milestones with structured filters. " +
			"This is the cheap way to answer \"what should I work on?\": filter, project the fields " +
			"you need and walk the cursor, instead of reading Markdown files. Bodies are never returned; " +
			"use get_item for one. Every entry carries a rev, the token a later write must quote.",
		Untrusted: true,
	}, listItems)

	register(s, toolDef{
		Name:  "search_items",
		Title: "Search backlog items",
		Description: "Ranked full-text search over item ids, titles, labels and bodies. " +
			"Use it when you do not know which item you need; use list_items when you can express " +
			"the question as a filter.",
		Untrusted: true,
	}, searchItems)

	register(s, toolDef{
		Name:  "get_item",
		Title: "Get one backlog item",
		Description: "Read one item by id: front matter always, plus the Markdown body, the comment " +
			"thread and the child items when include asks for them. The rev it returns is the token a " +
			"later update_item, add_comment or move_on_board must quote.",
		Untrusted: true,
	}, getItem)

	register(s, toolDef{
		Name:        "create_epic",
		Title:       "Create an epic",
		Description: "Create an epic. The id is allocated by the tool; never propose one.",
		Write:       true,
	}, createTool(core.TypeEpic))

	register(s, toolDef{
		Name:  "create_story",
		Title: "Create a story",
		Description: "Create a user story, optionally under an epic given as parent. " +
			"The id is allocated by the tool; never propose one.",
		Write: true,
	}, createTool(core.TypeStory))

	register(s, toolDef{
		Name:  "create_task",
		Title: "Create a task",
		Description: "Create a task, optionally under a story given as parent. " +
			"The id is allocated by the tool; never propose one.",
		Write: true,
	}, createTool(core.TypeTask))

	register(s, toolDef{
		Name:  "update_item",
		Title: "Update a backlog item",
		Description: "Apply a sparse patch to one item: only the keys you pass are changed, so the " +
			"diff a human reviews stays small. A status change is validated against the project workflow, " +
			"which refuses a transition the project does not declare.",
		Write: true,
	}, updateItem)

	register(s, toolDef{
		Name:  "add_comment",
		Title: "Comment on a backlog item",
		Description: "Append one comment to an item's thread as a separate file, attributed to the " +
			"agent. Prefer a comment over editing an item body when you are reporting progress or " +
			"raising a question.",
		Write: true,
	}, addComment)
}

// ---------------------------------------------------------------- handlers --

// listItems answers a filtered, paginated query. The core owns the cursor: it
// is passed through untouched, so a walk stays consistent with what the index
// knows rather than with a snapshot this package took.
func listItems(ctx context.Context, s *Server, in ListItemsInput) (ItemPage, error) {
	limit := boundedLimit(in.Limit)
	params := map[string]any{
		"project":      in.Project,
		"type":         in.Type,
		"status":       in.Status,
		"category":     in.Category,
		"priority":     in.Priority,
		"assignee":     in.Assignee,
		"label":        in.Label,
		"parent":       in.Parent,
		"milestone":    in.Milestone,
		"text":         in.Text,
		"updatedSince": in.UpdatedSince,
		"sort":         sortField(in.Sort),
		"order":        sortOrder(in.Order),
		"limit":        limit,
		"cursor":       in.Cursor,
	}
	page, err := dispatch[struct {
		Items      []core.Item `json:"items"`
		NextCursor string      `json:"nextCursor"`
		Total      int         `json:"total"`
	}](ctx, s, "item.list", params)
	if err != nil {
		return ItemPage{}, err
	}
	out := ItemPage{Items: make([]Item, 0, len(page.Items)), Total: page.Total, NextCursor: page.NextCursor}
	for _, it := range page.Items {
		// A list never returns bodies, however the projection is spelled: the
		// bodies of a page of items are the bulk of a vault.
		brief := itemOf(it)
		brief.Body = ""
		out.Items = append(out.Items, projectItem(brief, withoutBody(in.Fields)))
	}
	return out, nil
}

// searchItems runs the ranked search and keeps the item hits. The core answers
// the whole ranking at once, so the page is cut here, against a cursor bound to
// the query.
func searchItems(ctx context.Context, s *Server, in SearchItemsInput) (HitPage, error) {
	if strings.TrimSpace(in.Query) == "" {
		return HitPage{}, invalidField("query", "search needs a query", "oidc discovery cache")
	}
	hits, err := searchHits(ctx, s, in.Query, in.Project)
	if err != nil {
		return HitPage{}, err
	}
	items := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if h.Kind != "item" {
			continue
		}
		items = append(items, h)
	}
	limit := boundedLimit(in.Limit)
	filter := fingerprint("search_items", in.Query, in.Project)
	offset, err := decodeCursor(in.Cursor, filter)
	if err != nil {
		return HitPage{}, err
	}
	page, next := slice(items, offset, limit, filter)
	// A hit is an item, so it carries a rev like every other item this server
	// returns; the ranking itself does not compute one.
	for i := range page {
		page[i].Rev, page[i].Status = s.itemRev(ctx, page[i].ID)
	}
	return HitPage{Results: page, Total: len(items), NextCursor: next}, nil
}

// getItem reads one item and the sections the caller asked for.
func getItem(ctx context.Context, s *Server, in GetItemInput) (ItemResult, error) {
	if strings.TrimSpace(in.ID) == "" {
		return ItemResult{}, invalidField("id", "get_item needs an item id", "ACME-US-0042")
	}
	it, err := dispatch[core.Item](ctx, s, "item.get", map[string]any{"id": in.ID})
	if err != nil {
		return ItemResult{}, err
	}
	brief := itemOf(it)
	fields := in.Fields
	if !includes(in.Include, "body") {
		brief.Body = ""
		fields = withoutBody(fields)
	} else {
		brief.Body = it.Body
		if len(fields) > 0 && !includes(fields, "body") {
			fields = append(append([]string{}, fields...), "body")
		}
		if len(fields) == 0 {
			fields = append(append([]string{}, defaultItemFields...), "body", "path", "links")
		}
	}
	out := ItemResult{Item: projectItem(brief, fields)}

	if includes(in.Include, "comments") {
		thread, err := dispatch[[]core.Comment](ctx, s, "comment.list", map[string]any{"id": in.ID})
		if err != nil {
			return ItemResult{}, err
		}
		out.Comments = make([]Comment, 0, len(thread))
		for _, c := range thread {
			out.Comments = append(out.Comments, commentOf(c))
		}
	}
	if includes(in.Include, "children") {
		kids, err := dispatch[[]core.Item](ctx, s, "item.children", map[string]any{"id": in.ID})
		if err != nil {
			return ItemResult{}, err
		}
		out.Children = make([]Item, 0, len(kids))
		for _, kid := range kids {
			brief := itemOf(kid)
			brief.Body = ""
			out.Children = append(out.Children, projectItem(brief, nil))
		}
	}
	return out, nil
}

// createTool builds the handler of create_epic, create_story and create_task.
// The three tools differ only in the type they fix, which is deliberate: an
// agent picking a tool by name cannot file a task as an epic by mistyping a
// field.
func createTool(itemType core.ItemType) func(context.Context, *Server, CreateItemInput) (WriteResult, error) {
	return func(ctx context.Context, s *Server, in CreateItemInput) (WriteResult, error) {
		if strings.TrimSpace(in.Title) == "" {
			return WriteResult{}, invalidField("title", "a new "+string(itemType)+" needs a title",
				"Wire OIDC discovery endpoint")
		}
		draft := map[string]any{
			"project":   in.Project,
			"type":      string(itemType),
			"title":     in.Title,
			"body":      in.Body,
			"status":    in.Status,
			"priority":  in.Priority,
			"parent":    in.Parent,
			"milestone": in.Milestone,
			"assignees": in.Assignees,
			"labels":    in.Labels,
			"estimate":  in.Estimate,
			"due":       in.Due,
			"author":    s.authorName(in.Author),
		}
		result, err := s.dispatchRaw(ctx, "item.create", draft)
		if err != nil {
			return WriteResult{}, err
		}
		out, err := writeResultOf(result)
		if err != nil {
			return WriteResult{}, err
		}
		s.announce(ctx, WriteEvent{
			Tool: "create_" + string(itemType), Method: "item.create",
			ItemID: out.Item.ID, Op: "created", Result: result,
		})
		return out, nil
	}
}

// updateItem applies a sparse patch. A status change goes through "item.move"
// rather than the patch, because that is the method that validates the
// transition against the project's workflow; doing both in one call keeps the
// agent's mental model simple and the validation identical to the UI's.
func updateItem(ctx context.Context, s *Server, in UpdateItemInput) (WriteResult, error) {
	if strings.TrimSpace(in.ID) == "" {
		return WriteResult{}, invalidField("id", "update_item needs an item id", "ACME-US-0042")
	}
	set := map[string]any{}
	putString(set, "title", in.Title)
	putString(set, "priority", in.Priority)
	putString(set, "parent", in.Parent)
	putString(set, "milestone", in.Milestone)
	putString(set, "due", in.Due)
	if in.Assignees != nil {
		set["assignees"] = in.Assignees
	}
	if in.Labels != nil {
		set["labels"] = in.Labels
	}
	if in.Estimate != nil {
		set["estimate"] = in.Estimate
	}
	if in.Effort != nil {
		set["effort"] = in.Effort
	}
	patchEmpty := len(set) == 0 && in.Body == nil && len(in.Unset) == 0
	if patchEmpty && in.Status == "" {
		return WriteResult{}, invalidField("set", "update_item was given nothing to change",
			map[string]any{"status": "in_progress", "labels": []string{"auth"}})
	}

	rev := in.Rev
	var result any
	var err error
	if !patchEmpty {
		patch := map[string]any{"set": set, "unset": in.Unset}
		if in.Body != nil {
			patch["body"] = *in.Body
		}
		result, err = s.dispatchRaw(ctx, "item.update",
			map[string]any{"id": in.ID, "rev": rev, "patch": patch})
		if err != nil {
			return WriteResult{}, err
		}
		out, convErr := writeResultOf(result)
		if convErr != nil {
			return WriteResult{}, convErr
		}
		// The status move below is a second write, and it has to quote the rev
		// this one produced rather than the stale one the caller sent.
		rev = out.Item.Rev
		s.announce(ctx, WriteEvent{
			Tool: "update_item", Method: "item.update",
			ItemID: out.Item.ID, Op: "updated", Result: result,
		})
		if in.Status == "" {
			return out, nil
		}
	}

	result, err = s.dispatchRaw(ctx, "item.move",
		map[string]any{"id": in.ID, "status": in.Status, "rev": rev})
	if err != nil {
		return WriteResult{}, err
	}
	out, err := writeResultOf(result)
	if err != nil {
		return WriteResult{}, err
	}
	s.announce(ctx, WriteEvent{
		Tool: "update_item", Method: "item.move",
		ItemID: out.Item.ID, Op: "moved", Result: result,
	})
	return out, nil
}

// addComment appends one comment file to an item's thread.
func addComment(ctx context.Context, s *Server, in AddCommentInput) (CommentResult, error) {
	if strings.TrimSpace(in.ID) == "" {
		return CommentResult{}, invalidField("id", "add_comment needs an item id", "ACME-US-0042")
	}
	if strings.TrimSpace(in.Body) == "" {
		return CommentResult{}, invalidField("body", "a comment needs a body",
			"Implemented discovery caching; static fallback still pending.")
	}
	result, err := s.dispatchRaw(ctx, "comment.add", map[string]any{
		"id": in.ID, "body": in.Body, "author": s.authorName(in.Author),
	})
	if err != nil {
		return CommentResult{}, err
	}
	payload, err := decodeResult[struct {
		Comment core.Comment `json:"comment"`
		Writes  writeSet     `json:"writes"`
	}](result)
	if err != nil {
		return CommentResult{}, err
	}
	out := CommentResult{Comment: commentOf(payload.Comment), Changed: payload.Writes.paths()}
	s.announce(ctx, WriteEvent{
		Tool: "add_comment", Method: "comment.add",
		ItemID: in.ID, Op: "commented", Result: result,
	})
	return out, nil
}

// ----------------------------------------------------------------- helpers --

// itemRev reads the current revision and status of an item, for the results
// that carry an item id but not the item itself. A lookup that fails leaves
// both empty rather than failing the whole page: the hit is still useful.
func (s *Server) itemRev(ctx context.Context, id string) (rev, status string) {
	if id == "" {
		return "", ""
	}
	it, err := dispatch[core.Item](ctx, s, "item.get", map[string]any{"id": id})
	if err != nil {
		return "", ""
	}
	return string(it.Rev), string(it.Status)
}

// searchHits runs the shared ranked search.
func searchHits(ctx context.Context, s *Server, query, project string) ([]Hit, error) {
	hits, err := dispatch[[]struct {
		Kind    string  `json:"kind"`
		ID      string  `json:"id"`
		Path    string  `json:"path"`
		Title   string  `json:"title"`
		Snippet string  `json:"snippet"`
		Score   float64 `json:"score"`
		Project string  `json:"project"`
	}](ctx, s, "search", map[string]any{"q": query, "project": project, "limit": maxSearchHits})
	if err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		out = append(out, Hit{
			Kind: h.Kind, ID: h.ID, Path: h.Path, Title: h.Title,
			Snippet: h.Snippet, Score: h.Score, Project: h.Project,
		})
	}
	return out, nil
}

// maxSearchHits caps the ranking the core is asked for. It is larger than a
// page so that a cursor walk has something to walk, and small enough that no
// single call ranks a whole vault into memory twice.
const maxSearchHits = 200

// sortField maps the tool's sort argument onto the core's field name, defaulting
// to the most useful order for an agent: what changed last, first.
func sortField(field string) string {
	if strings.TrimSpace(field) == "" {
		return "updated"
	}
	return strings.TrimSpace(field)
}

// sortOrder defaults to descending, which pairs with the default sort field.
func sortOrder(order string) string {
	if strings.EqualFold(order, "asc") {
		return "asc"
	}
	return "desc"
}

// withoutBody removes the body from a projection, for the tools that never
// return one.
func withoutBody(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.EqualFold(strings.TrimSpace(f), "body") {
			continue
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// putString adds a value to a patch only when it was given, so an absent
// argument is never written as an empty string.
func putString(set map[string]any, key, value string) {
	if value != "" {
		set[key] = value
	}
}
