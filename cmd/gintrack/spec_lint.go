package main

import (
	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
)

// specLintFlags mirrors the flags of docs/07 section 4.20.
type specLintFlags struct {
	asJSON bool
}

// specLintResult is the finding list of one spec.
type specLintResult struct {
	ID          core.ItemID       `json:"id"`
	Path        string            `json:"path,omitempty"`
	Diagnostics []core.Diagnostic `json:"diagnostics"`
}

// specLintPayload is what `gintrack spec lint --json` prints.
type specLintPayload struct {
	Specs    []specLintResult `json:"specs"`
	Errors   int              `json:"errors"`
	Warnings int              `json:"warnings"`
}

func newSpecLintCommand(flags *globalFlags) *cobra.Command {
	local := &specLintFlags{}
	cmd := &cobra.Command{
		Use:   "lint [spec...]",
		Short: "Check specs and their requirement blocks",
		Long: `Validate specs the way every write does (docs/03 sections 21.2 to 21.4 and
21.9): the requirement blocks and the requirements: map, and the LINT-REQ-*
grammar rules at the severity specs.lint in project.yaml gives each one. With
no argument every spec of the workspace is checked.

The exit code is 3 only when a finding has the error severity; warnings are
printed and the command still exits 0, so a project raises a rule to error in
specs.lint to make CI fail on it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSpecLint(cmd, flags, local, args)
		},
	}
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func runSpecLint(cmd *cobra.Command, flags *globalFlags, local *specLintFlags, args []string) error {
	for _, arg := range args {
		if !isSpecID(arg) {
			return usagef("%q is not a spec id: want <KEY>-SP-<NNNN>", arg)
		}
	}
	s, err := openSpecSpace(cmd, flags, specSpaceOptions{})
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	ids := make([]core.ItemID, 0, len(args))
	for _, arg := range args {
		ids = append(ids, core.ItemID(arg))
	}
	paths := map[core.ItemID]string{}
	if len(ids) == 0 {
		for _, m := range s.projectMounts() {
			cursor := ""
			for {
				page, err := dispatch[struct {
					Items []struct {
						ID   core.ItemID `json:"id"`
						Path string      `json:"path"`
					} `json:"items"`
					NextCursor string `json:"nextCursor"`
				}](ctx, s.space, "item.list", map[string]any{
					"vaultId": m.ID, "type": []string{string(core.TypeSpec)},
					"sort": "id", "order": "asc", "cursor": cursor,
				})
				if err != nil {
					return err
				}
				for _, it := range page.Items {
					ids = append(ids, it.ID)
					paths[it.ID] = it.Path
				}
				if page.NextCursor == "" {
					break
				}
				cursor = page.NextCursor
			}
		}
	}

	payload := specLintPayload{Specs: make([]specLintResult, 0, len(ids))}
	for _, id := range ids {
		diags, err := dispatch[[]core.Diagnostic](ctx, s.space, "item.validate", map[string]any{"id": id})
		if err != nil {
			return err
		}
		if diags == nil {
			diags = []core.Diagnostic{}
		}
		res := specLintResult{ID: id, Path: paths[id], Diagnostics: diags}
		for _, d := range diags {
			if res.Path == "" {
				res.Path = d.Path
			}
			if d.Severity == core.SeverityError {
				payload.Errors++
			} else {
				payload.Warnings++
			}
		}
		payload.Specs = append(payload.Specs, res)
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		if err := render(p.JSON(payload)); err != nil {
			return err
		}
	} else {
		for _, r := range payload.Specs {
			for _, d := range r.Diagnostics {
				p.Printf("%-7s %s  %s\n", d.Severity, r.ID, d.String())
			}
		}
		p.Printf("%s checked: %s, %s\n", plural(len(payload.Specs), "spec", "specs"),
			plural(payload.Errors, "error", "errors"), plural(payload.Warnings, "warning", "warnings"))
	}
	if payload.Errors > 0 {
		return failf(exitValidation, "spec lint: %s", plural(payload.Errors, "error", "errors"))
	}
	return nil
}
