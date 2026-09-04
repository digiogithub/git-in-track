package main

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
)

// itemListFlags mirrors the flags of docs/07 section 4.5.
type itemListFlags struct {
	projects     []string
	types        []string
	statuses     []string
	assignees    []string
	labels       []string
	priorities   []string
	parent       string
	milestone    string
	text         string
	updatedSince string
	sort         string
	limit        int
	offset       int
	fields       string
	all          bool
	asJSON       bool
}

// itemListPayload is what `gintrack item list --json` prints.
type itemListPayload struct {
	Items      []any  `json:"items"`
	Total      int    `json:"total"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// newItemListCommand queries the backlog.
func newItemListCommand(flags *globalFlags) *cobra.Command {
	local := &itemListFlags{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List backlog items",
		Long: `List the items of the workspace. Repeated filters are OR within a field and AND
across fields, which is the semantic of the REST API and of the MCP item_list
tool, so the three answer the same question the same way.`,
		Aliases: []string{"ls"},
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runItemList(cmd, flags, local)
		},
	}

	f := cmd.Flags()
	f.StringArrayVar(&local.projects, "project", nil, "project key (repeatable)")
	f.StringArrayVar(&local.types, "type", nil, "epic, story, task or milestone (repeatable)")
	f.StringArrayVar(&local.statuses, "status", nil, "status name (repeatable)")
	f.StringArrayVar(&local.assignees, "assignee", nil, `assignee handle, "none" for unassigned (repeatable)`)
	f.StringArrayVar(&local.labels, "label", nil, "label (repeatable)")
	f.StringArrayVar(&local.priorities, "priority", nil, "critical, high, medium or low (repeatable)")
	f.StringVar(&local.parent, "parent", "", "parent item id")
	f.StringVar(&local.milestone, "milestone", "", "milestone id")
	f.StringVar(&local.text, "text", "", "full-text query over the title, the labels and the body")
	f.StringVar(&local.updatedSince, "updated-since", "", "RFC 3339 instant or a duration such as 7d")
	f.StringVar(&local.sort, "sort", "", `sort keys: updated, created, priority, id, title (prefix "-" to reverse)`)
	f.IntVar(&local.limit, "limit", core.DefaultLimit, "maximum number of items")
	f.IntVar(&local.offset, "offset", 0, "skip this many items")
	f.StringVar(&local.fields, "fields", "", "comma-separated fields for --json output")
	f.BoolVar(&local.all, "all", false, "include soft-deleted items")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runItemList builds a core filter from the flags and renders the page.
func runItemList(cmd *cobra.Command, flags *globalFlags, local *itemListFlags) error {
	v, err := openItemVault(cmd, flags)
	if err != nil {
		return err
	}
	filter, err := buildFilter(local)
	if err != nil {
		return err
	}
	// The core paginates with an opaque cursor; --offset is served by asking for
	// the window that contains it and dropping the head, which keeps the total
	// order of the result identical to what a cursor walk would return.
	filter.Limit = local.limit + local.offset
	page, err := v.Index.Items(cmd.Context(), filter)
	if err != nil {
		return failf(exitUsage, "%v", err)
	}
	items := page.Items
	if local.offset > 0 {
		if local.offset >= len(items) {
			items = nil
		} else {
			items = items[local.offset:]
		}
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		fields := splitList(local.fields)
		payload := itemListPayload{
			Items:      make([]any, 0, len(items)),
			Total:      page.Total,
			Limit:      local.limit,
			Offset:     local.offset,
			NextCursor: page.NextCursor,
		}
		for i := range items {
			payload.Items = append(payload.Items, projectItem(&items[i], fields))
		}
		return render(p.JSON(payload))
	}
	if len(items) == 0 {
		p.Printf("no item matches\n")
		return nil
	}
	if err := p.Table(itemHeaders, itemRows(items, time.Now())); err != nil {
		return render(err)
	}
	if page.Total > len(items)+local.offset {
		p.Printf("%d of %d items (use --limit and --offset for more)\n", len(items), page.Total)
	}
	return nil
}

// buildFilter turns the flags into the core filter.
func buildFilter(local *itemListFlags) (core.Filter, error) {
	f := core.Filter{
		Parent:         core.ItemID(local.parent),
		Milestone:      core.ItemID(local.milestone),
		Text:           local.text,
		Sort:           local.sort,
		IncludeDeleted: local.all,
	}
	for _, p := range local.projects {
		f.Projects = append(f.Projects, core.ProjectKey(strings.ToUpper(p)))
	}
	for _, t := range local.types {
		typ := core.ItemType(t)
		if !typ.Valid() || typ == core.TypeComment {
			return f, usagef("unknown item type %q: use epic, story, task or milestone", t)
		}
		f.Types = append(f.Types, typ)
	}
	for _, s := range local.statuses {
		f.Statuses = append(f.Statuses, core.Status(s))
	}
	f.Assignees = append(f.Assignees, local.assignees...)
	f.Labels = append(f.Labels, local.labels...)
	for _, p := range local.priorities {
		prio := core.Priority(p)
		if !prio.Valid() {
			return f, usagef("unknown priority %q: use critical, high, medium or low", p)
		}
		f.Priorities = append(f.Priorities, prio)
	}
	if local.updatedSince != "" {
		ts, err := core.ParseUpdatedSince(local.updatedSince, time.Now())
		if err != nil {
			return f, usagef("%v", err)
		}
		f.UpdatedSince = ts
	}
	if local.limit < 0 || local.offset < 0 {
		return f, usagef("--limit and --offset must not be negative")
	}
	return f, nil
}

// projectItem renders one item for the JSON payload, honoring --fields.
func projectItem(it *core.Item, fields []string) any {
	if len(fields) == 0 {
		return newItemPayload(*it)
	}
	out := core.ProjectFields(it, fields)
	if _, ok := out["path"]; ok {
		repo, rel := repoPath(it.Path)
		out["path"] = rel
		out["repo"] = repo
	}
	return out
}

// splitList splits a comma-separated flag value.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
