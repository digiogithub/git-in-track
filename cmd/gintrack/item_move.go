package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
)

// itemMoveFlags mirrors the flags of docs/07 section 4.5.
type itemMoveFlags struct {
	rev     string
	comment string
	force   bool
	dryRun  bool
	asJSON  bool
}

// newItemMoveCommand changes the status of an item.
func newItemMoveCommand(flags *globalFlags) *cobra.Command {
	local := &itemMoveFlags{}

	cmd := &cobra.Command{
		Use:   "move <id> <status>",
		Short: "Change the status of an item",
		Long: `Move an item along the workflow declared in project.yaml.

A transition the workflow does not allow fails with exit 5; --force records the
move anyway. The started and closed timestamps are stamped by the core as the
item enters or leaves a terminal status.`,
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runItemMove(cmd, flags, local, args[0], args[1])
		},
	}

	f := cmd.Flags()
	f.StringVar(&local.rev, "rev", "", "expected revision; a mismatch fails with exit 5")
	f.StringVar(&local.comment, "comment", "", "add a comment describing the transition")
	f.BoolVar(&local.force, "force", false, "bypass the workflow transition check")
	f.BoolVar(&local.dryRun, "dry-run", false, "print the file that would be written, write nothing")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runItemMove applies a status change and the optional transition comment.
func runItemMove(cmd *cobra.Command, flags *globalFlags, local *itemMoveFlags, raw, status string) error {
	v, err := openItemVault(cmd, flags)
	if err != nil {
		return err
	}
	id, project, err := resolveItem(v, raw)
	if err != nil {
		return err
	}
	before, err := v.Index.Item(id)
	if err != nil {
		return fail(exitNotFound, err)
	}
	from := before.Status

	store, overlay, err := v.storeFor(project, local.dryRun)
	if err != nil {
		return err
	}
	it, err := store.MoveWith(cmd.Context(), id, core.Status(status), core.Rev(local.rev), core.MoveOptions{Force: local.force})
	if err != nil {
		return fmt.Errorf("move: %w", err)
	}
	if local.comment != "" {
		if _, err := store.AddComment(cmd.Context(), id, core.CommentDraft{
			Author: commentAuthor("", flags.config()),
			Body:   local.comment,
			Kind:   core.CommentKindStatusChange,
		}); err != nil {
			return fmt.Errorf("comment on the transition: %w", err)
		}
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		payload := newItemWritePayload(it, local.dryRun)
		payload.From = from
		return render(p.JSON(payload))
	}
	if local.dryRun {
		reportDryRun(p, overlay)
		return nil
	}
	p.Printf("%s  %s -> %s\n", it.ID, orDash(string(from)), it.Status)
	return nil
}
