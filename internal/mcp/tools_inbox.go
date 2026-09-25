package mcp

import (
	"context"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The inbox tools: submit something into a project's triage queue, read the
// queue, and decide what happens to one entry (story GIT-US-0056, ADR-033).
//
// Like every other tool in this package they are shims: they validate the
// framing, dispatch one method of the shared core and project the answer. What
// a triage state means, when a snooze has expired and which fields an action
// owns are decided once, in internal/vault and internal/core.

// ------------------------------------------------------------------ input ---

// CreateInboxItemInput is a submission: something that arrived and has not been
// triaged yet. It is deliberately thin — status, parent and type are not the
// submitter's to choose, they are the triager's.
type CreateInboxItemInput struct {
	Project string `json:"project,omitempty" jsonschema:"Project key; required when the workspace holds more than one"`
	Title   string `json:"title" jsonschema:"One-line summary of what arrived"`
	Body    string `json:"body,omitempty" jsonschema:"Markdown body: the report, the request, the context"`
	Type    string `json:"type,omitempty" jsonschema:"epic, story, task or milestone; default story"`
	// Source is free text, never an enumeration: it records where the
	// submission came from so a triager can weigh it.
	Source   string   `json:"source,omitempty" jsonschema:"Where the submission came from, for example web, mcp or a form name; default the agent name"`
	Priority string   `json:"priority,omitempty"`
	Labels   []string `json:"labels,omitempty"`
	Author   string   `json:"author,omitempty" jsonschema:"Defaults to the agent name the server was started with"`
}

// ListInboxInput filters the triage queue. `status` is the triage state, not
// the workflow status: everything this tool returns sits in triage already.
type ListInboxInput struct {
	Project  string   `json:"project,omitempty" jsonschema:"Project key, for example ACME"`
	Status   []string `json:"status,omitempty" jsonschema:"Triage states: pending, accepted, rejected, snoozed or duplicate"`
	Type     []string `json:"type,omitempty" jsonschema:"epic, story, task or milestone"`
	Label    []string `json:"label,omitempty"`
	Assignee string   `json:"assignee,omitempty"`
	Text     string   `json:"text,omitempty" jsonschema:"Substring match over title and body"`
	Sort     string   `json:"sort,omitempty" jsonschema:"Field to sort by; default updated"`
	Order    string   `json:"order,omitempty" jsonschema:"asc or desc; default desc"`
	Limit    int      `json:"limit,omitempty" jsonschema:"Page size, 1 to 100; default 20"`
	Cursor   string   `json:"cursor,omitempty" jsonschema:"nextCursor from the previous page"`
	Fields   []string `json:"fields,omitempty" jsonschema:"Fields to project; id and rev are always returned"`
}

// InboxPage is one page of the triage queue plus the counts a badge needs.
type InboxPage struct {
	Items      []Item `json:"items"`
	Total      int    `json:"total" jsonschema:"Number of entries the filter matches"`
	NextCursor string `json:"nextCursor,omitempty" jsonschema:"Pass back as cursor to fetch the next page"`
	// Pending counts what is still waiting across the whole queue, with expired
	// snoozes already counted as pending again.
	Pending int            `json:"pending"`
	Counts  map[string]int `json:"counts,omitempty" jsonschema:"Entries per triage state across the whole queue"`
}

// TriageInboxItemInput applies exactly one decision to one inbox entry.
type TriageInboxItemInput struct {
	ID string `json:"id" jsonschema:"Permanent item id, for example ACME-US-0042"`
	// Rev is mandatory, as on every write: triage is a decision about a state of
	// the item, and deciding on a state you have not seen is the bug this
	// prevents.
	Rev    string `json:"rev" jsonschema:"Required. The rev returned by the read this decision is based on; \"*\" decides on whatever is there now"`
	Action string `json:"action" jsonschema:"accept, reject, snooze or duplicate"`
	// Status and Parent belong to accept alone.
	Status string `json:"status,omitempty" jsonschema:"accept only: the workflow status to accept into; default the project's initial status"`
	Parent string `json:"parent,omitempty" jsonschema:"accept only: the owning epic or story"`
	// SnoozedUntil belongs to snooze alone. Expiry is evaluated when the queue
	// is read; nothing runs on a timer.
	SnoozedUntil string `json:"snoozedUntil,omitempty" jsonschema:"snooze only: the date the entry returns to the queue, YYYY-MM-DD"`
	// DuplicateOf belongs to duplicate alone.
	DuplicateOf string `json:"duplicateOf,omitempty" jsonschema:"duplicate only: the item this one repeats"`
}

// TriageResult is what a decision answers with.
type TriageResult struct {
	Item    Item     `json:"item"`
	Action  string   `json:"action"`
	Pending int      `json:"pending" jsonschema:"Entries still waiting in this project's queue"`
	Changed []string `json:"changed,omitempty" jsonschema:"Vault-relative paths written by this call"`
}

// ---------------------------------------------------------------- registry --

// registerInboxTools declares the triage half of the surface.
func registerInboxTools(s *Server) {
	register(s, toolDef{
		Name:  "create_inbox_item",
		Title: "Submit an item into a project's inbox",
		Description: "File something into a project's triage queue instead of straight into the " +
			"backlog: a bug report, a request, anything that still needs a human decision. The item " +
			"is created in the project's triage status and marked pending; nobody has to accept it " +
			"for it to be recorded. Use this rather than create_story when you are reporting " +
			"something rather than planning it. A project that declares no triage status has no " +
			"inbox and refuses with no_triage_status.",
		Write: true,
	}, createInboxItem)

	register(s, toolDef{
		Name:  "list_inbox",
		Title: "List a project's triage queue",
		Description: "Read the inbox: the items waiting for a triage decision, with the counts per " +
			"triage state across the whole queue. A snoozed entry whose date has arrived is listed " +
			"as pending again — expiry is decided when the queue is read, not by a scheduler. " +
			"Every entry carries a rev, the token triage_inbox_item must quote.",
		Untrusted: true,
	}, listInbox)

	register(s, toolDef{
		Name:  "triage_inbox_item",
		Title: "Decide what happens to an inbox item",
		Description: "Apply exactly one decision to one inbox entry, in a single rev-checked write. " +
			"accept moves it out of triage into the ordinary workflow — it does not decide the rest " +
			"for the user, so a person still picks the details; reject moves it to the project's " +
			"cancelled status and never deletes it; snooze sets the date it comes back on; " +
			"duplicate records duplicateOf and writes the duplicates link with its duplicated_by " +
			"inverse on the target. rev is required: quote the one the read this decision is based " +
			"on returned.",
		Write: true,
	}, triageInboxItem)
}

// ---------------------------------------------------------------- handlers --

// createInboxItem files one submission into a project's triage queue.
func createInboxItem(ctx context.Context, s *Server, in CreateInboxItemInput) (WriteResult, error) {
	if strings.TrimSpace(in.Title) == "" {
		return WriteResult{}, invalidField("title", "a submission needs a title",
			"Checkout hangs on Safari after the address step")
	}
	itemType := core.ItemType(strings.TrimSpace(in.Type))
	if itemType == "" {
		itemType = core.TypeStory
	}
	// A spec is never an inbox target (ADR-037 section 1).
	if !itemType.Valid() || itemType == core.TypeComment || itemType == core.TypeSpec {
		return WriteResult{}, invalidField("type", "a submission is an epic, a story, a task or a milestone",
			string(core.TypeStory))
	}
	source := strings.TrimSpace(in.Source)
	if source == "" {
		source = s.agent
	}
	draft := map[string]any{
		"project":  in.Project,
		"type":     string(itemType),
		"title":    in.Title,
		"body":     in.Body,
		"priority": in.Priority,
		"labels":   in.Labels,
		"author":   s.authorName(in.Author),
		"inbox":    map[string]any{"source": source},
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
		Tool: "create_inbox_item", Method: "item.create",
		ItemID: out.Item.ID, Op: "created", Result: result,
	})
	return out, nil
}

// listInbox answers the triage queue. The core owns the position of the walk
// and the clock: this handler never decides what "still snoozed" means. It does
// bind the core cursor to every filter and to the sort, as listItems does.
func listInbox(ctx context.Context, s *Server, in ListInboxInput) (InboxPage, error) {
	filter := fingerprint("list_inbox", in.Project, in.Status, in.Type, in.Label,
		in.Assignee, in.Text, sortField(in.Sort), sortOrder(in.Order))
	inner, err := unwrapCursor(in.Cursor, filter)
	if err != nil {
		return InboxPage{}, err
	}
	params := map[string]any{
		"project":  in.Project,
		"status":   in.Status,
		"type":     in.Type,
		"label":    in.Label,
		"assignee": in.Assignee,
		"text":     in.Text,
		"sort":     sortField(in.Sort),
		"order":    sortOrder(in.Order),
		"limit":    boundedLimit(in.Limit),
		"cursor":   inner,
	}
	page, err := dispatch[struct {
		Items      []core.Item    `json:"items"`
		NextCursor string         `json:"nextCursor"`
		Total      int            `json:"total"`
		Pending    int            `json:"pending"`
		Counts     map[string]int `json:"counts"`
	}](ctx, s, "inbox.list", params)
	if err != nil {
		return InboxPage{}, err
	}
	out := InboxPage{
		Items: make([]Item, 0, len(page.Items)), Total: page.Total,
		NextCursor: wrapCursor(page.NextCursor, filter), Pending: page.Pending, Counts: page.Counts,
	}
	for _, it := range page.Items {
		brief := itemOf(it)
		brief.Body = ""
		out.Items = append(out.Items, projectItem(brief, withoutBody(in.Fields)))
	}
	return out, nil
}

// triageInboxItem applies one decision. Everything the action means happens in
// the vault, as one write inside one lock.
func triageInboxItem(ctx context.Context, s *Server, in TriageInboxItemInput) (TriageResult, error) {
	if strings.TrimSpace(in.ID) == "" {
		return TriageResult{}, invalidField("id", "triage_inbox_item needs an item id", "ACME-US-0042")
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action == "" {
		return TriageResult{}, invalidField("action", "triage_inbox_item needs a decision",
			"accept, reject, snooze or duplicate")
	}
	rev, err := requiredRev("rev", in.Rev)
	if err != nil {
		return TriageResult{}, err
	}
	result, err := s.dispatchRaw(ctx, "inbox.triage", map[string]any{
		"id": in.ID, "rev": rev, "action": action,
		"status": in.Status, "parent": in.Parent,
		"snoozedUntil": in.SnoozedUntil, "duplicateOf": in.DuplicateOf,
	})
	if err != nil {
		return TriageResult{}, err
	}
	payload, err := decodeResult[struct {
		Item    core.Item `json:"item"`
		Action  string    `json:"action"`
		Pending int       `json:"pending"`
		Writes  writeSet  `json:"writes"`
	}](result)
	if err != nil {
		return TriageResult{}, err
	}
	item := itemOf(payload.Item)
	item.Body = ""
	out := TriageResult{
		Item:    projectItem(item, append(append([]string{}, defaultItemFields...), "path", "links")),
		Action:  payload.Action,
		Pending: payload.Pending,
		Changed: payload.Writes.paths(),
	}
	s.announce(ctx, WriteEvent{
		Tool: "triage_inbox_item", Method: "inbox.triage",
		ItemID: out.Item.ID, Op: "moved", Result: result,
	})
	return out, nil
}
