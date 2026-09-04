package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
)

// itemCommentFlags mirrors the flags of docs/07 section 4.5.
type itemCommentFlags struct {
	body   string
	author string
	dryRun bool
	asJSON bool
}

// newItemCommentCommand appends a comment to an item.
func newItemCommentCommand(flags *globalFlags) *cobra.Command {
	local := &itemCommentFlags{}

	cmd := &cobra.Command{
		Use:   "comment <id>",
		Short: "Comment on an item",
		Long: `Append a comment to the thread of an item.

Comments are one file each under .pmngr/comments/<ITEM-ID>/, named
"<YYYYMMDDTHHMMSSZ>-<author>.md", so two people commenting at once never
conflict in git.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runItemComment(cmd, flags, local, args[0])
		},
	}

	f := cmd.Flags()
	f.StringVar(&local.body, "body", "", `comment Markdown ("-" reads standard input)`)
	f.StringVar(&local.author, "author", "", "author handle (default: git.authorName from the configuration)")
	f.BoolVar(&local.dryRun, "dry-run", false, "print the file that would be written, write nothing")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runItemComment writes one comment file.
func runItemComment(cmd *cobra.Command, flags *globalFlags, local *itemCommentFlags, raw string) error {
	body, err := readBody(cmd, local.body)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return usagef(`--body is required (pass "-" to read the comment from standard input)`)
	}
	v, err := openItemVault(cmd, flags)
	if err != nil {
		return err
	}
	id, project, err := resolveItem(v, raw)
	if err != nil {
		return err
	}
	store, overlay, err := v.storeFor(project, local.dryRun)
	if err != nil {
		return err
	}
	comment, err := store.AddComment(cmd.Context(), id, core.CommentDraft{
		Author: commentAuthor(local.author, flags.config()),
		Body:   body,
		Kind:   core.CommentKindComment,
	})
	if err != nil {
		return fmt.Errorf("comment: %w", err)
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(newCommentPayload(*comment)))
	}
	if local.dryRun {
		reportDryRun(p, overlay)
		return nil
	}
	p.Printf("commented on %s  %s\n", id, displayPath(comment.Path))
	return nil
}
