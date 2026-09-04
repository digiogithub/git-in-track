package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
)

// itemEditFlags mirrors the flags of docs/07 section 4.5.
type itemEditFlags struct {
	title           string
	status          string
	priority        string
	parent          string
	milestone       string
	due             string
	estimate        float64
	effort          float64
	assignees       []string
	addAssignees    []string
	removeAssignees []string
	labels          []string
	addLabels       []string
	removeLabels    []string
	body            string
	appendBody      string
	unset           []string
	rev             string
	dryRun          bool
	asJSON          bool
}

// newItemEditCommand edits an item.
func newItemEditCommand(flags *globalFlags) *cobra.Command {
	local := &itemEditFlags{}

	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit an item",
		Long: `Change the front matter or the body of an item.

--rev turns the edit into a conditional write: when the file changed on disk
since that revision the command fails with exit 5 and nothing is written, which
is the same optimistic lock the API and the MCP tools use.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runItemEdit(cmd, flags, local, args[0])
		},
	}

	f := cmd.Flags()
	f.StringVar(&local.title, "title", "", "new title; the file is renamed to match")
	f.StringVar(&local.status, "status", "", "new status (item move enforces the workflow)")
	f.StringVar(&local.priority, "priority", "", "critical, high, medium or low")
	f.StringVar(&local.parent, "parent", "", "new parent item id")
	f.StringVar(&local.milestone, "milestone", "", "new milestone id")
	f.StringVar(&local.due, "due", "", "due date, YYYY-MM-DD")
	f.Float64Var(&local.estimate, "estimate", 0, "estimate in story points")
	f.Float64Var(&local.effort, "effort", 0, "effort in hours")
	f.StringArrayVar(&local.assignees, "assignee", nil, "replace the assignees (repeatable)")
	f.StringArrayVar(&local.addAssignees, "add-assignee", nil, "add an assignee (repeatable)")
	f.StringArrayVar(&local.removeAssignees, "remove-assignee", nil, "remove an assignee (repeatable)")
	f.StringArrayVar(&local.labels, "label", nil, "replace the labels (repeatable)")
	f.StringArrayVar(&local.addLabels, "add-label", nil, "add a label (repeatable)")
	f.StringArrayVar(&local.removeLabels, "remove-label", nil, "remove a label (repeatable)")
	f.StringVar(&local.body, "body", "", `replace the body ("-" reads standard input)`)
	f.StringVar(&local.appendBody, "append", "", "append to the body")
	f.StringArrayVar(&local.unset, "unset", nil, "clear a front-matter field (repeatable)")
	f.StringVar(&local.rev, "rev", "", "expected revision; a mismatch fails with exit 5")
	f.BoolVar(&local.dryRun, "dry-run", false, "print the file that would be written, write nothing")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runItemEdit builds a patch from the flags and applies it.
func runItemEdit(cmd *cobra.Command, flags *globalFlags, local *itemEditFlags, raw string) error {
	v, err := openItemVault(cmd, flags)
	if err != nil {
		return err
	}
	id, project, err := resolveItem(v, raw)
	if err != nil {
		return err
	}
	patch, err := buildPatch(cmd, local)
	if err != nil {
		return err
	}
	store, overlay, err := v.storeFor(project, local.dryRun)
	if err != nil {
		return err
	}
	it, err := store.Update(cmd.Context(), id, patch, core.Rev(local.rev))
	if err != nil {
		return fmt.Errorf("edit: %w", err)
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(newItemWritePayload(it, local.dryRun)))
	}
	if local.dryRun {
		reportDryRun(p, overlay)
		return nil
	}
	p.Printf("updated %s  %s\n", it.ID, displayPath(it.Path))
	return nil
}

// buildPatch turns the flags into the sparse patch the store applies. A flag
// that was not given stays nil, which is what "leave this field alone" means.
func buildPatch(cmd *cobra.Command, local *itemEditFlags) (core.ItemPatch, error) {
	changed := cmd.Flags().Changed
	patch := core.ItemPatch{
		AddAssignees:    local.addAssignees,
		RemoveAssignees: local.removeAssignees,
		AddLabels:       local.addLabels,
		RemoveLabels:    local.removeLabels,
		BodyAppend:      local.appendBody,
		Unset:           local.unset,
	}
	if changed("title") {
		patch.Title = &local.title
	}
	if changed("status") {
		status := core.Status(local.status)
		patch.Status = &status
	}
	if changed("priority") {
		priority := core.Priority(local.priority)
		patch.Priority = &priority
	}
	if changed("parent") {
		parent := core.ItemID(local.parent)
		patch.Parent = &parent
	}
	if changed("milestone") {
		milestone := core.ItemID(local.milestone)
		patch.Milestone = &milestone
	}
	if changed("estimate") {
		patch.Estimate = &local.estimate
	}
	if changed("effort") {
		patch.Effort = &local.effort
	}
	if changed("assignee") {
		assignees := local.assignees
		patch.Assignees = &assignees
	}
	if changed("label") {
		labels := local.labels
		patch.Labels = &labels
	}
	if changed("due") {
		due, err := core.ParseDate(local.due)
		if err != nil {
			return patch, failf(exitValidation, "--due: %v", err)
		}
		patch.Due = &due
	}
	if changed("body") {
		body, err := readBody(cmd, local.body)
		if err != nil {
			return patch, err
		}
		patch.Body = &body
	}
	if isEmptyPatch(patch) {
		return patch, usagef("nothing to change: pass at least one field")
	}
	return patch, nil
}

// isEmptyPatch reports whether the flags asked for no change at all.
func isEmptyPatch(p core.ItemPatch) bool {
	return p.Title == nil && p.Status == nil && p.Priority == nil && p.Parent == nil &&
		p.Milestone == nil && p.Estimate == nil && p.Effort == nil && p.Due == nil &&
		p.Assignees == nil && p.Labels == nil && p.Body == nil &&
		len(p.AddAssignees) == 0 && len(p.RemoveAssignees) == 0 &&
		len(p.AddLabels) == 0 && len(p.RemoveLabels) == 0 &&
		len(p.Unset) == 0 && strings.TrimSpace(p.BodyAppend) == ""
}
