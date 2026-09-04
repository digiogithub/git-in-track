package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
)

// itemNewFlags mirrors the flags of docs/07 section 4.5.
type itemNewFlags struct {
	project   string
	typ       string
	title     string
	parent    string
	status    string
	assignees []string
	labels    []string
	priority  string
	estimate  float64
	effort    float64
	milestone string
	due       string
	body      string
	dryRun    bool
	asJSON    bool
}

// itemWritePayload is what the write subcommands print with --json.
type itemWritePayload struct {
	ID     core.ItemID `json:"id"`
	Repo   string      `json:"repo"`
	Path   string      `json:"path"`
	Rev    core.Rev    `json:"rev"`
	Status core.Status `json:"status,omitempty"`
	From   core.Status `json:"from,omitempty"`
	DryRun bool        `json:"dryRun,omitempty"`
}

// newItemWritePayload projects a written item onto its payload.
func newItemWritePayload(it *core.Item, dryRun bool) itemWritePayload {
	repo, rel := repoPath(it.Path)
	return itemWritePayload{ID: it.ID, Repo: repo, Path: rel, Rev: it.Rev, Status: it.Status, DryRun: dryRun}
}

// newItemNewCommand creates an item.
func newItemNewCommand(flags *globalFlags) *cobra.Command {
	local := &itemNewFlags{}

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create an item",
		Long: `Create an epic, a story, a task or a milestone.

The id is allocated by the core, which takes the maximum of the counter in
project.yaml and the highest id on disk, so two writers never collide. The
project defaults fill whatever the flags leave out.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runItemNew(cmd, flags, local)
		},
	}

	f := cmd.Flags()
	f.StringVar(&local.project, "project", "", "project key (required when the workspace holds more than one)")
	f.StringVar(&local.typ, "type", "", "epic, story, task or milestone (required)")
	f.StringVar(&local.title, "title", "", "title (required)")
	f.StringVar(&local.parent, "parent", "", "epic of a story, story of a task")
	f.StringVar(&local.status, "status", "", "initial status (default: the first status of the workflow)")
	f.StringArrayVar(&local.assignees, "assignee", nil, "assignee handle (repeatable)")
	f.StringArrayVar(&local.labels, "label", nil, "label (repeatable)")
	f.StringVar(&local.priority, "priority", "", "critical, high, medium or low")
	f.Float64Var(&local.estimate, "estimate", 0, "estimate in story points")
	f.Float64Var(&local.effort, "effort", 0, "effort in hours")
	f.StringVar(&local.milestone, "milestone", "", "milestone id")
	f.StringVar(&local.due, "due", "", "due date, YYYY-MM-DD")
	f.StringVar(&local.body, "body", "", `body Markdown ("-" reads standard input)`)
	f.BoolVar(&local.dryRun, "dry-run", false, "print the file that would be written, write nothing")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runItemNew builds a draft from the flags and asks the store to create it.
func runItemNew(cmd *cobra.Command, flags *globalFlags, local *itemNewFlags) error {
	if strings.TrimSpace(local.title) == "" {
		return usagef("--title is required")
	}
	typ := core.ItemType(strings.TrimSpace(local.typ))
	if !typ.Valid() || typ == core.TypeComment {
		return usagef("--type is required: use epic, story, task or milestone")
	}
	body, err := readBody(cmd, local.body)
	if err != nil {
		return err
	}

	v, err := openItemVault(cmd, flags)
	if err != nil {
		return err
	}
	project, err := pickProject(v, local.project)
	if err != nil {
		return err
	}
	store, overlay, err := v.storeFor(project, local.dryRun)
	if err != nil {
		return err
	}

	draft := core.ItemDraft{
		Type:      typ,
		Title:     local.title,
		Status:    core.Status(local.status),
		Priority:  core.Priority(local.priority),
		Parent:    core.ItemID(local.parent),
		Milestone: core.ItemID(local.milestone),
		Assignees: local.assignees,
		Labels:    local.labels,
		Body:      body,
	}
	if cmd.Flags().Changed("estimate") {
		draft.Estimate = &local.estimate
	}
	if cmd.Flags().Changed("effort") {
		draft.Effort = &local.effort
	}
	if local.due != "" {
		due, err := core.ParseDate(local.due)
		if err != nil {
			return failf(exitValidation, "--due: %v", err)
		}
		draft.Due = due
	}

	it, err := store.Create(cmd.Context(), draft)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(newItemWritePayload(it, local.dryRun)))
	}
	if local.dryRun {
		reportDryRun(p, overlay)
		return nil
	}
	p.Printf("created %s  %s\n", it.ID, displayPath(it.Path))
	return nil
}

// pickProject returns the project a write goes to: the one named by --project,
// or the only registered one.
func pickProject(v *vault, key string) (projectView, error) {
	if strings.TrimSpace(key) == "" {
		return v.only()
	}
	return v.project(core.ProjectKey(strings.ToUpper(strings.TrimSpace(key))))
}
