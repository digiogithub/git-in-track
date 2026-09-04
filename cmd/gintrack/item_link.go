package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
)

// itemLinkFlags mirrors the flags of docs/07 section 4.5.
type itemLinkFlags struct {
	remove  bool
	inverse bool
	dryRun  bool
	asJSON  bool
}

// linkPayload is what `gintrack item link --json` prints.
type linkPayload struct {
	ID      core.ItemID   `json:"id"`
	Kind    core.LinkKind `json:"kind"`
	Target  string        `json:"target"`
	Removed bool          `json:"removed"`
	Inverse *linkSide     `json:"inverse,omitempty"`
	Rev     core.Rev      `json:"rev"`
	DryRun  bool          `json:"dryRun,omitempty"`
}

// linkSide is the counterpart the inverse relation was written on.
type linkSide struct {
	ID   core.ItemID   `json:"id"`
	Kind core.LinkKind `json:"kind"`
	Rev  core.Rev      `json:"rev"`
}

// newItemLinkCommand manages the typed relations between items.
func newItemLinkCommand(flags *globalFlags) *cobra.Command {
	local := &itemLinkFlags{inverse: true}

	cmd := &cobra.Command{
		Use:   "link <id> <relation> <target>",
		Short: "Link two items",
		Long: `Add or remove a typed relation: blocks, blocked_by, relates_to or duplicates.

The inverse relation is written on the counterpart when it lives in the same
workspace, so the two files never disagree about the relation between them.`,
		Args: exactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runItemLink(cmd, flags, local, args[0], args[1], args[2])
		},
	}

	f := cmd.Flags()
	f.BoolVar(&local.remove, "remove", false, "remove the relation instead of adding it")
	f.BoolVar(&local.inverse, "inverse", true, "write the inverse relation on the counterpart")
	f.BoolVar(&local.dryRun, "dry-run", false, "print the files that would be written, write nothing")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runItemLink adds or removes one relation, and its inverse on the counterpart.
func runItemLink(cmd *cobra.Command, flags *globalFlags, local *itemLinkFlags, raw, relation, target string) error {
	kind := core.LinkKind(strings.TrimSpace(relation))
	if !kind.Valid() {
		return usagef("unknown relation %q: use blocks, blocked_by, relates_to or duplicates", relation)
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

	link := core.Link{Kind: kind, Target: strings.TrimSpace(target)}
	it, err := store.Update(cmd.Context(), id, linkPatch(link, local.remove), "")
	if err != nil {
		return fmt.Errorf("link: %w", err)
	}

	payload := linkPayload{ID: it.ID, Kind: kind, Target: link.Target, Removed: local.remove, Rev: it.Rev, DryRun: local.dryRun}
	if local.inverse {
		side, err := writeInverse(cmd, v, local, core.ItemID(link.Target), core.Link{Kind: kind.Inverse(), Target: string(id)})
		if err != nil {
			return err
		}
		payload.Inverse = side
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	if local.dryRun {
		reportDryRun(p, overlay)
		return nil
	}
	verb := "linked"
	if local.remove {
		verb = "unlinked"
	}
	p.Printf("%s %s  %s %s\n", verb, it.ID, kind, link.Target)
	if payload.Inverse != nil {
		p.Printf("%s %s  %s %s\n", verb, payload.Inverse.ID, payload.Inverse.Kind, it.ID)
	}
	return nil
}

// linkPatch turns a relation into the patch that adds or removes it.
func linkPatch(link core.Link, remove bool) core.ItemPatch {
	if remove {
		return core.ItemPatch{RemoveLinks: []core.Link{link}}
	}
	return core.ItemPatch{AddLinks: []core.Link{link}}
}

// writeInverse mirrors a relation onto its counterpart when that item lives in
// the workspace. A target outside the workspace is left alone: the relation is
// still recorded on the item the user named.
func writeInverse(cmd *cobra.Command, v *vault, local *itemLinkFlags, target core.ItemID, link core.Link) (*linkSide, error) {
	if !target.Valid() || link.Kind == "" {
		return nil, nil
	}
	project, err := v.projectOf(target)
	if err != nil {
		return nil, nil //nolint:nilerr // a target outside the workspace is not an error
	}
	if _, err := v.Index.Item(target); err != nil {
		return nil, nil //nolint:nilerr // an unknown counterpart is reported by doctor, not here
	}
	store, _, err := v.storeFor(project, local.dryRun)
	if err != nil {
		return nil, err
	}
	it, err := store.Update(cmd.Context(), target, linkPatch(link, local.remove), "")
	if err != nil {
		return nil, fmt.Errorf("link the counterpart %s: %w", target, err)
	}
	return &linkSide{ID: it.ID, Kind: link.Kind, Rev: it.Rev}, nil
}
