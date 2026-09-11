package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
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
	draft := core.CommentDraft{
		Author: commentAuthor(local.author, flags.config()),
		Body:   body,
		Kind:   core.CommentKindComment,
	}
	if draft.Author == "" {
		draft.AuthorName, draft.AuthorEmail = repoIdentity(cmd.Context(), project.Repo.Path, flags.config().Git)
	}
	comment, err := store.AddComment(cmd.Context(), id, draft)
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

// repoIdentity reads the git identity of the repository an item lives in —
// user.name and user.email, or the configured overrides — so a comment written
// with no --author is attributed to the person git would attribute a commit
// to. A folder that is not a repository, or one with no identity, yields
// nothing and the comment falls back to the "unknown" handle.
func repoIdentity(ctx context.Context, repoPath string, git config.Git) (name, email string) {
	if repoPath == "" {
		return "", ""
	}
	//nolint:contextcheck // Open probes the folder locally and takes no context; Identity below does.
	backend, err := gitops.Open(repoPath, gitops.Options{
		Backend:     gitops.Kind(git.Backend),
		AuthorName:  git.AuthorName,
		AuthorEmail: git.AuthorEmail,
	})
	if err != nil {
		return "", ""
	}
	id, err := backend.Identity(ctx)
	if err != nil || !id.Valid() {
		return "", ""
	}
	return id.Name, id.Email
}
