package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
)

// itemGetFlags mirrors the flags of docs/07 section 4.5.
type itemGetFlags struct {
	body     bool
	comments bool
	asJSON   bool
}

// itemGetPayload is what `gintrack item get --json` prints.
type itemGetPayload struct {
	Item     itemPayload      `json:"item"`
	Children []string         `json:"children,omitempty"`
	Comments []commentPayload `json:"comments,omitempty"`
}

// newItemGetCommand prints one item.
func newItemGetCommand(flags *globalFlags) *cobra.Command {
	local := &itemGetFlags{body: true}

	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Print one item",
		Long: `Print the front matter and the body of an item, exactly as the file holds it.

The rev is the content hash the optimistic-locking flags of item edit and item
move expect.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runItemGet(cmd, flags, local, args[0])
		},
	}

	cmd.Flags().BoolVar(&local.body, "body", true, "include the Markdown body")
	cmd.Flags().BoolVar(&local.comments, "comments", false, "include the comment thread")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runItemGet reads one item from the index and renders it.
func runItemGet(cmd *cobra.Command, flags *globalFlags, local *itemGetFlags, raw string) error {
	v, err := openItemVault(cmd, flags)
	if err != nil {
		return err
	}
	id, _, err := resolveItem(v, raw)
	if err != nil {
		return err
	}
	it, err := v.Index.Item(id)
	if err != nil {
		return fail(exitNotFound, err)
	}
	item := *it
	if !local.body {
		item.Body = ""
	}

	payload := itemGetPayload{Item: newItemPayload(item)}
	for _, child := range v.Index.Children(id) {
		payload.Children = append(payload.Children, string(child.ID))
	}
	if local.comments {
		for _, c := range v.Index.Comments(id) {
			payload.Comments = append(payload.Comments, newCommentPayload(c))
		}
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}

	line := func(label, value string) {
		if value != "" {
			p.Printf("%-10s %s\n", label+":", value)
		}
	}
	line("id", string(item.ID))
	line("type", string(item.Type))
	line("title", item.Title)
	line("status", string(item.Status))
	line("priority", string(item.Priority))
	line("parent", string(item.Parent))
	line("milestone", string(item.Milestone))
	line("assignees", strings.Join(item.Assignees, ", "))
	line("labels", strings.Join(item.Labels, ", "))
	line("created", item.Created.String())
	line("updated", item.Updated.String())
	line("links", renderLinks(item.Links))
	line("children", strings.Join(payload.Children, ", "))
	line("path", displayPath(it.Path))
	line("rev", string(item.Rev))
	if local.body && strings.TrimSpace(item.Body) != "" {
		p.Printf("\n%s\n", strings.TrimRight(item.Body, "\n"))
	}
	for _, c := range payload.Comments {
		p.Printf("\n--- %s  %s\n%s\n", c.Author, c.Created.String(), strings.TrimRight(c.Body, "\n"))
	}
	return nil
}

// renderLinks renders the typed relations of an item on one line.
func renderLinks(links []core.Link) string {
	parts := make([]string, 0, len(links))
	for _, l := range links {
		parts = append(parts, string(l.Kind)+" "+l.Target)
	}
	return strings.Join(parts, ", ")
}
