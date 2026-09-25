package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	corevault "github.com/digiogithub/git-in-track/internal/vault"
)

// This file is the `gintrack inbox` command tree and the small seam both it and
// `gintrack sprint` reach the core through.
//
// Nothing here decides anything about triage: every rule — what a triage status
// is, which fields an action owns, when a snooze expires — lives in
// internal/core behind the "inbox.list" and "inbox.triage" methods of
// internal/vault. The commands resolve the configuration, mount the workspace,
// call one method and print (GIT-US-0066, GIT-T-0071).

// ------------------------------------------------------------- the seam ----

// openSpace mounts every repository registered in the workspace as one
// corevault.Workspace, which is the same object the MCP server and the
// companion drive. A command therefore runs the identical implementation of a
// query whichever surface asked for it.
//
// The version string is empty on purpose: it is only reported by the MCP
// handshake, and no command-line answer carries it.
func openSpace(res *config.Resolution) (*corevault.Workspace, error) {
	repos, err := mcpRepos(res, nil)
	if err != nil {
		return nil, err
	}
	space, _, err := mountWorkspace(repos, "")
	if err != nil {
		return nil, err
	}
	return space, nil
}

// dispatch runs one core method and decodes its answer into T.
//
// The round trip through JSON is deliberate: the wire shape of a method is its
// contract, so a command that decodes the contract cannot drift from what the
// REST API and the MCP server return for the same call.
func dispatch[T any](
	ctx context.Context, space *corevault.Workspace, method string, params any,
) (T, error) {
	var out T
	raw, err := json.Marshal(params)
	if err != nil {
		return out, fmt.Errorf("encode the parameters of %s: %w", method, err)
	}
	result, err := space.Dispatch(ctx, method, raw)
	if err != nil {
		return out, vaultError(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return out, fmt.Errorf("encode the answer of %s: %w", method, err)
	}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return out, fmt.Errorf("decode the answer of %s: %w", method, err)
	}
	return out, nil
}

// vaultError maps the stable error catalog of internal/vault onto the process
// exit codes of docs/07 section 4, so that a missing sprint is always 4 and a
// stale revision is always 5 whichever command reported it.
func vaultError(err error) error {
	var coded *corevault.Error
	if !errors.As(err, &coded) {
		return err
	}
	switch coded.Code {
	case "not_found":
		return fail(exitNotFound, err)
	case "invalid_request", "invalid_front_matter", "validation", "read_only":
		return fail(exitValidation, err)
	case core.StaleRevisionCode, "duplicate_id", "conflict",
		corevault.SprintOverlapCode, corevault.SprintActiveCode,
		corevault.SprintTargetCompletedCode:
		return fail(exitConflict, err)
	default:
		return fail(exitFailure, err)
	}
}

// openSpaceFor resolves the configuration and mounts the workspace for one
// command invocation.
func openSpaceFor(flags *globalFlags) (*corevault.Workspace, error) {
	res, err := flags.resolve()
	if err != nil {
		return nil, err
	}
	return openSpace(res)
}

// --------------------------------------------------------------- payloads --

// inboxRowPayload is one queue entry as `--json` reports it. It is a declared
// shape rather than a dump of the core item: the columns a triage queue is
// scripted against are stable, the internals of an item are not.
type inboxRowPayload struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// Triage is the inbox state: pending, accepted, rejected, snoozed or
	// duplicate. It is not the workflow status above.
	Triage       string   `json:"triage"`
	Project      string   `json:"project"`
	Source       string   `json:"source,omitempty"`
	Received     string   `json:"received,omitempty"`
	SnoozedUntil string   `json:"snoozedUntil,omitempty"`
	DuplicateOf  string   `json:"duplicateOf,omitempty"`
	Assignees    []string `json:"assignees,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	// Path is the item's file inside the repository that holds it.
	Path    string `json:"path,omitempty"`
	Rev     string `json:"rev,omitempty"`
	Updated string `json:"updated,omitempty"`
}

// inboxListPayload is what `gintrack inbox list --json` prints.
type inboxListPayload struct {
	Items      []inboxRowPayload `json:"items"`
	Total      int               `json:"total"`
	Pending    int               `json:"pending"`
	Counts     map[string]int    `json:"counts"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

// inboxTriagePayload is what the three triage commands print with `--json`.
type inboxTriagePayload struct {
	Item    inboxRowPayload `json:"item"`
	Action  string          `json:"action"`
	Pending int             `json:"pending"`
	Written []string        `json:"written"`
}

// newInboxRow renders one core item as a queue entry.
func newInboxRow(it core.Item) inboxRowPayload {
	row := inboxRowPayload{
		ID:        string(it.ID),
		Type:      string(it.Type),
		Title:     it.Title,
		Status:    string(it.Status),
		Triage:    string(core.InboxPending),
		Assignees: it.Assignees,
		Labels:    it.Labels,
		Path:      it.Path,
		Rev:       string(it.Rev),
		Updated:   it.Updated.String(),
	}
	if key, _, _, err := core.ParseItemID(string(it.ID)); err == nil {
		row.Project = string(key)
	}
	if in := it.Inbox; in != nil {
		if in.Status != "" {
			row.Triage = string(in.Status)
		}
		row.Source = in.Source
		row.Received = in.Received.String()
		row.SnoozedUntil = in.SnoozedUntil.String()
		row.DuplicateOf = string(in.DuplicateOf)
	}
	return row
}

// -------------------------------------------------------------- the tree ---

// newInboxCommand groups the triage surface.
func newInboxCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "Read and triage the inbox queue",
		Long: `Read and triage the submissions waiting in the inbox.

An inbox item is an ordinary item whose status belongs to the reserved triage
category; the ` + "`inbox:`" + ` block records how it arrived and what the triager
decided. Accepting one moves it into the ordinary workflow, rejecting one
cancels it, and snoozing one hides it until a date — every decision goes
through the same core the web application and the MCP server use.

Every subcommand takes --json, and in that mode the human lines go to standard
error so that ` + "`gintrack inbox list --json | jq`" + ` stays safe.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usageError(cmd.Help())
		},
	}
	cmd.AddCommand(
		newInboxListCommand(flags),
		newInboxAddCommand(flags),
		newInboxAcceptCommand(flags),
		newInboxRejectCommand(flags),
		newInboxSnoozeCommand(flags),
	)
	return cmd
}

// ------------------------------------------------------------------ list ---

// inboxListFlags mirrors the flags of the inbox listing.
type inboxListFlags struct {
	status  string
	project string
	limit   int
	asJSON  bool
}

// inboxStatusAll is the filter value that keeps every triage state.
const inboxStatusAll = "all"

// newInboxListCommand prints the triage queue.
func newInboxListCommand(flags *globalFlags) *cobra.Command {
	local := &inboxListFlags{}

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List the submissions waiting for triage",
		Aliases: []string{"ls"},
		Long: `List the inbox queue of the workspace, newest decision first.

--status filters on the triage state, not on the workflow status: pending,
snoozed, rejected, accepted, duplicate, or all for the whole queue. A snoozed
item whose date has arrived is counted and listed as pending, because that is
what a reader sees.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInboxList(cmd, flags, local)
		},
	}

	f := cmd.Flags()
	f.StringVar(&local.status, "status", string(core.InboxPending),
		"triage state: pending, snoozed, rejected, accepted, duplicate or all")
	f.StringVar(&local.project, "project", "", "project key")
	f.IntVar(&local.limit, "limit", core.DefaultLimit, "maximum number of items")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// inboxHeaders are the columns of the queue table.
var inboxHeaders = []string{"ID", "TYPE", "TITLE", "TRIAGE", "SOURCE", "RECEIVED", "UNTIL"}

// runInboxList renders one page of the queue.
func runInboxList(cmd *cobra.Command, flags *globalFlags, local *inboxListFlags) error {
	status, err := parseInboxFilter(local.status)
	if err != nil {
		return err
	}
	if local.limit < 0 {
		return usagef("--limit must not be negative")
	}
	space, err := openSpaceFor(flags)
	if err != nil {
		return err
	}
	params := map[string]any{"limit": local.limit}
	if status != "" {
		params["status"] = []string{status}
	}
	if key := strings.TrimSpace(local.project); key != "" {
		params["project"] = strings.ToUpper(key)
	}
	page, err := dispatch[corevault.InboxPage](cmd.Context(), space, "inbox.list", params)
	if err != nil {
		return err
	}

	payload := inboxListPayload{
		Items:      make([]inboxRowPayload, 0, len(page.Items)),
		Total:      page.Total,
		Pending:    page.Pending,
		Counts:     map[string]int{},
		NextCursor: page.NextCursor,
	}
	for _, it := range page.Items {
		payload.Items = append(payload.Items, newInboxRow(it))
	}
	for _, state := range core.InboxStatuses() {
		payload.Counts[string(state)] = page.Counts[state]
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	if len(payload.Items) == 0 {
		p.Printf("the inbox is empty\n")
		return nil
	}
	rows := make([][]string, 0, len(payload.Items))
	for _, row := range payload.Items {
		rows = append(rows, []string{
			row.ID, row.Type, row.Title, row.Triage,
			orDash(row.Source), orDash(row.Received), orDash(row.SnoozedUntil),
		})
	}
	if err := p.Table(inboxHeaders, rows); err != nil {
		return render(err)
	}
	p.Printf("%s, %s waiting\n",
		plural(len(payload.Items), "item", "items"),
		plural(payload.Pending, "submission", "submissions"))
	return nil
}

// parseInboxFilter validates the --status value and returns the triage state to
// filter on, empty meaning every state.
func parseInboxFilter(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" || value == inboxStatusAll {
		return "", nil
	}
	if !core.InboxStatus(value).Valid() {
		return "", usagef(
			"unknown triage state %q: use pending, snoozed, rejected, accepted, duplicate or all", raw)
	}
	return value, nil
}

// ------------------------------------------------------------------- add ---

// inboxAddFlags mirrors the flags of `gintrack inbox add`.
//
// It is deliberately thinner than `gintrack item new`: a submission is
// something that arrived, so its status, its parent and its milestone are the
// triager's to choose and not the submitter's, exactly as the MCP tool
// create_inbox_item is thin for the same reason.
type inboxAddFlags struct {
	project  string
	typ      string
	title    string
	body     string
	source   string
	priority string
	labels   []string
	author   string
	asJSON   bool
}

// inboxAddPayload is what `gintrack inbox add --json` prints.
type inboxAddPayload struct {
	Item    inboxRowPayload `json:"item"`
	Written []string        `json:"written"`
}

// inboxSourceCLI is the `inbox.source` a submission filed from the terminal
// records when the caller named none. It is free text like every other source,
// and it names the surface rather than the person: the person is the author.
const inboxSourceCLI = "cli"

// newInboxAddCommand files a submission into the triage queue.
func newInboxAddCommand(flags *globalFlags) *cobra.Command {
	local := &inboxAddFlags{}

	cmd := &cobra.Command{
		Use:     "add --title <title>",
		Short:   "File a submission into the inbox",
		Aliases: []string{"new"},
		Long: `File something into the triage queue instead of straight into the backlog: a
bug report, a request, anything that still needs a human decision.

The item is created in the project's triage status and marked pending; nobody
has to accept it for it to be recorded. A project that declares no triage status
has no inbox and the command refuses with no_triage_status.

--status and --parent are deliberately absent: a submission has not been triaged
yet, so those are decisions for ` + "`gintrack inbox accept`" + `. --body reads standard
input when it is "-", which is how every other body flag in this CLI reads a
piped body, so a bug report can be piped in:

    cat report.md | gintrack inbox add --title "Checkout hangs" --body -

--project is required only when the workspace holds more than one project.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInboxAdd(cmd, flags, local)
		},
	}

	f := cmd.Flags()
	f.StringVar(&local.project, "project", "", "project key (required when the workspace holds more than one)")
	f.StringVar(&local.title, "title", "", "one-line summary of what arrived (required)")
	f.StringVar(&local.typ, "type", string(core.TypeStory), "epic, story, task or milestone")
	f.StringVar(&local.body, "body", "", `body Markdown ("-" reads standard input)`)
	f.StringVar(&local.source, "source", "", `where the submission came from (default "`+inboxSourceCLI+`")`)
	f.StringVar(&local.priority, "priority", "", "critical, high, medium or low")
	f.StringArrayVar(&local.labels, "label", nil, "label (repeatable)")
	f.StringVar(&local.author, "author", "", "author recorded on the item (default: the configured user)")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runInboxAdd creates one item carrying an `inbox` block.
//
// Every rule it appears to apply belongs to the core: whether the project has a
// triage status, which status a submission lands in and what the `inbox` block
// holds are all decided behind "item.create". The command validates only what a
// bad invocation must not reach the core as — an empty title and a type an id
// cannot pin — so that those exit 2 rather than 1.
func runInboxAdd(cmd *cobra.Command, flags *globalFlags, local *inboxAddFlags) error {
	if strings.TrimSpace(local.title) == "" {
		return usagef("--title is required: a submission needs a one-line summary")
	}
	typ := core.ItemType(strings.TrimSpace(local.typ))
	if !typ.Valid() || typ == core.TypeComment || typ == core.TypeSpec {
		return usagef("--type must be epic, story, task or milestone")
	}
	body, err := readBody(cmd, local.body)
	if err != nil {
		return err
	}
	source := strings.TrimSpace(local.source)
	if source == "" {
		source = inboxSourceCLI
	}

	space, err := openSpaceFor(flags)
	if err != nil {
		return err
	}
	params := map[string]any{
		"type":   string(typ),
		"title":  strings.TrimSpace(local.title),
		"body":   body,
		"labels": local.labels,
		"inbox":  map[string]any{"source": source},
	}
	if key := strings.TrimSpace(local.project); key != "" {
		params["project"] = strings.ToUpper(key)
	}
	if priority := strings.TrimSpace(local.priority); priority != "" {
		params["priority"] = priority
	}
	if author := strings.TrimSpace(local.author); author != "" {
		params["author"] = author
	}

	created, err := dispatch[struct {
		Item   core.Item          `json:"item"`
		Writes corevault.WriteSet `json:"writes"`
	}](cmd.Context(), space, "item.create", params)
	if err != nil {
		return err
	}

	payload := inboxAddPayload{
		Item:    newInboxRow(created.Item),
		Written: make([]string, 0, len(created.Writes.Written)),
	}
	for _, f := range created.Writes.Written {
		payload.Written = append(payload.Written, f.Path)
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	p.Printf("filed %s  %s\n", payload.Item.ID, displayPath(created.Item.Path))
	p.Printf("%s, source %s\n", payload.Item.Triage, orDash(payload.Item.Source))
	return nil
}

// ---------------------------------------------------------------- triage ---

// inboxTriageFlags mirrors the fields the four triage actions own between them.
type inboxTriageFlags struct {
	status string
	typ    string
	parent string
	until  string
	asJSON bool
}

// newInboxAcceptCommand moves a submission into the ordinary backlog.
func newInboxAcceptCommand(flags *globalFlags) *cobra.Command {
	local := &inboxTriageFlags{}

	cmd := &cobra.Command{
		Use:   "accept <id>",
		Short: "Accept a submission into the backlog",
		Long: `Accept a submission: its triage state becomes accepted and its workflow status
leaves the triage category.

--status picks the status it lands in; without it the project's initial status
is used. --parent files it under an epic or a story. --type is accepted only to
confirm the type the id already pins, because an item id pins its type for
life.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInboxTriage(cmd, flags, local, corevault.TriageAccept, args[0])
		},
	}
	f := cmd.Flags()
	f.StringVar(&local.status, "status", "", "workflow status to accept into (default: the project's initial status)")
	f.StringVar(&local.typ, "type", "", "item type, which must match the type the id pins")
	f.StringVar(&local.parent, "parent", "", "epic or story to file the item under")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// newInboxRejectCommand cancels a submission.
func newInboxRejectCommand(flags *globalFlags) *cobra.Command {
	local := &inboxTriageFlags{}

	cmd := &cobra.Command{
		Use:   "reject <id>",
		Short: "Reject a submission",
		Long: `Reject a submission: its triage state becomes rejected and its workflow status
becomes the project's cancelled status.

Rejecting is a status, never a deletion: the file, its id and its history stay
exactly where they are.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInboxTriage(cmd, flags, local, corevault.TriageReject, args[0])
		},
	}
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// newInboxSnoozeCommand hides a submission until a date.
func newInboxSnoozeCommand(flags *globalFlags) *cobra.Command {
	local := &inboxTriageFlags{}

	cmd := &cobra.Command{
		Use:   "snooze <id> --until <date>",
		Short: "Snooze a submission until a date",
		Long: `Snooze a submission until a date, given as YYYY-MM-DD.

Expiry is a comparison against the clock at read time and never a scheduler: on
the day it arrives the item is listed and counted as pending again, with no
process having to run in between.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(local.until) == "" {
				return usagef("--until is required: snoozing needs the date the item comes back on")
			}
			return runInboxTriage(cmd, flags, local, corevault.TriageSnooze, args[0])
		},
	}
	f := cmd.Flags()
	f.StringVar(&local.until, "until", "", "date the item comes back on, YYYY-MM-DD")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runInboxTriage applies one decision. The item is read first because a triage
// is a rev-checked write: the core refuses one whose revision has moved, and
// quoting the revision we just read is what makes a concurrent edit visible
// instead of silently overwritten.
func runInboxTriage(
	cmd *cobra.Command, flags *globalFlags, local *inboxTriageFlags,
	action corevault.InboxTriageAction, raw string,
) error {
	id := strings.TrimSpace(raw)
	if !core.ItemID(id).Valid() {
		return failf(exitValidation, "%q is not an item id: want <KEY>-<EP|US|T|M|SP>-<NNNN>", raw)
	}
	space, err := openSpaceFor(flags)
	if err != nil {
		return err
	}
	current, err := dispatch[core.Item](cmd.Context(), space, "item.get", map[string]any{"id": id})
	if err != nil {
		return err
	}
	params := corevault.InboxTriageParams{
		ID:           id,
		Rev:          string(current.Rev),
		Action:       string(action),
		Status:       strings.TrimSpace(local.status),
		Type:         strings.TrimSpace(local.typ),
		Parent:       strings.TrimSpace(local.parent),
		SnoozedUntil: strings.TrimSpace(local.until),
	}
	result, err := dispatch[corevault.InboxTriageResult](cmd.Context(), space, "inbox.triage", params)
	if err != nil {
		return err
	}

	payload := inboxTriagePayload{
		Item:    newInboxRow(result.Item),
		Action:  string(result.Action),
		Pending: result.Pending,
		Written: make([]string, 0, len(result.Writes.Written)),
	}
	for _, f := range result.Writes.Written {
		payload.Written = append(payload.Written, f.Path)
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	p.Printf("%s %s (%s)\n", triageVerb(result.Action), payload.Item.ID, triageDetail(payload))
	p.Printf("%s still waiting\n", plural(payload.Pending, "submission", "submissions"))
	return nil
}

// triageVerb renders a decision in the past tense.
func triageVerb(action corevault.InboxTriageAction) string {
	switch action {
	case corevault.TriageAccept:
		return "accepted"
	case corevault.TriageReject:
		return "rejected"
	case corevault.TriageSnooze:
		return "snoozed"
	case corevault.TriageDuplicate:
		return "marked as duplicate"
	default:
		return "triaged"
	}
}

// triageDetail renders the field the decision that was taken owns.
func triageDetail(payload inboxTriagePayload) string {
	switch corevault.InboxTriageAction(payload.Action) {
	case corevault.TriageSnooze:
		return "until " + orDash(payload.Item.SnoozedUntil)
	case corevault.TriageDuplicate:
		return "duplicate of " + orDash(payload.Item.DuplicateOf)
	default:
		return "status " + orDash(payload.Item.Status)
	}
}
