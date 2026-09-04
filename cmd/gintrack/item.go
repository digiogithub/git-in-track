package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/cmd/gintrack/output"
	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
)

// stdinMarker is the flag value that reads the body from standard input.
const stdinMarker = "-"

// newItemCommand groups the scriptable backlog surface.
func newItemCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "item",
		Short: "Read and write backlog items",
		Long: `Read and write the epics, stories, tasks and milestones of the registered
repositories. Every subcommand takes --json, and every write goes through the
same core the web application and the MCP server use, so the three can never
disagree about what a file should look like.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usageError(cmd.Help())
		},
	}
	cmd.AddCommand(
		newItemListCommand(flags),
		newItemGetCommand(flags),
		newItemNewCommand(flags),
		newItemEditCommand(flags),
		newItemMoveCommand(flags),
		newItemCommentCommand(flags),
		newItemLinkCommand(flags),
	)
	return cmd
}

// itemPayload is an item as the command line reports it: the core item with a
// repository-relative path and the repository it came from.
type itemPayload struct {
	core.Item
	Repo string `json:"repo"`
}

// newItemPayload rewrites the vault path of an item into a repository and a
// path inside it.
func newItemPayload(it core.Item) itemPayload {
	repo, rel := repoPath(it.Path)
	it.Path = rel
	return itemPayload{Item: it, Repo: repo}
}

// commentPayload is a comment as the command line reports it.
type commentPayload struct {
	core.Comment
	Repo string `json:"repo"`
	Ref  string `json:"ref"`
}

// newCommentPayload rewrites the vault path of a comment.
func newCommentPayload(c core.Comment) commentPayload {
	repo, rel := repoPath(c.Path)
	ref := c.Ref()
	c.Path = rel
	return commentPayload{Comment: c, Repo: repo, Ref: ref}
}

// itemRows renders items as the rows of the table docs/07 section 4.5 shows.
func itemRows(items []core.Item, now time.Time) [][]string {
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		rows = append(rows, []string{
			string(it.ID),
			string(it.Type),
			it.Title,
			orDash(string(it.Status)),
			orDash(strings.Join(it.Assignees, ",")),
			orDash(string(it.Priority)),
			ago(it.Updated, now),
		})
	}
	return rows
}

// itemHeaders are the columns of the item table.
var itemHeaders = []string{"ID", "TYPE", "TITLE", "STATUS", "ASSIGNEE", "PRIORITY", "UPDATED"}

// openItemVault mounts the workspace and indexes it for an item command.
func openItemVault(cmd *cobra.Command, flags *globalFlags) (*vault, error) {
	res, err := flags.resolve()
	if err != nil {
		return nil, err
	}
	return openVault(cmd.Context(), res, true)
}

// resolveItem finds the project an item id belongs to and returns both.
func resolveItem(v *vault, raw string) (core.ItemID, projectView, error) {
	id := core.ItemID(strings.TrimSpace(raw))
	if !id.Valid() {
		return "", projectView{}, failf(exitValidation, "%q is not an item id: want <KEY>-<EP|US|T|M>-<NNNN>", raw)
	}
	p, err := v.projectOf(id)
	if err != nil {
		return "", projectView{}, err
	}
	return id, p, nil
}

// storeFor returns the store a write command uses. A dry run gets a store over
// an in-memory overlay, so the write path runs for real and touches nothing.
func (v *vault) storeFor(p projectView, dryRun bool) (*core.FileStore, *overlayFS, error) {
	if p.Ref.Config == nil {
		return nil, nil, failf(exitValidation, "%s: %s is invalid, fix it before writing", p.Ref.Key, p.Ref.ConfigPath)
	}
	if !dryRun {
		store, err := v.store(p)
		return store, nil, err
	}
	if v.dry == nil {
		v.dry = newOverlayFS(v.FS)
	}
	return core.NewStore(v.dry, p.Ref.BacklogPath, p.Ref.Config), v.dry, nil
}

// reportDryRun prints the files a dry run would have written. The Markdown
// files are shown in full, because they are what the user asked to see; the
// bookkeeping a write touches on the side — the id counters in project.yaml —
// is only named, so that the preview stays readable.
func reportDryRun(p *output.Printer, overlay *overlayFS) {
	for _, c := range overlay.Changes() {
		switch {
		case c.Removed:
			p.Printf("would remove %s\n", displayPath(c.Path))
		case strings.HasSuffix(c.Path, ".md"):
			p.Printf("would write %s\n", displayPath(c.Path))
			p.Printf("%s\n", strings.TrimRight(string(c.Data), "\n"))
		default:
			p.Printf("would update %s\n", displayPath(c.Path))
		}
	}
	p.Printf("nothing was changed (--dry-run)\n")
}

// readBody returns the body a flag carries, reading standard input for "-".
func readBody(cmd *cobra.Command, value string) (string, error) {
	if value != stdinMarker {
		return value, nil
	}
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", fmt.Errorf("read the body from standard input: %w", err)
	}
	return string(data), nil
}

// commentAuthor returns the handle a comment is attributed to.
func commentAuthor(flag string, cfg *config.Config) string {
	if flag != "" {
		return flag
	}
	if cfg.Git.AuthorName != "" {
		return cfg.Git.AuthorName
	}
	return ""
}
