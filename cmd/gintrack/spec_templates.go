package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is `gintrack spec templates` (docs/07 section 4.20, ADR-038,
// GIT-US-0162). The export itself — which file is created, left alone,
// skipped or overwritten — is core.ExportSpecTemplates; the command resolves
// the project and prints the outcome.

func newSpecTemplatesCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "templates",
		Short: "Work with the spec and requirement templates",
		Args:  noArgs,
	}
	cmd.AddCommand(newSpecTemplatesExportCommand(flags))
	return cmd
}

// specTemplatesExportFlags mirrors the flags of docs/07 section 4.20.
type specTemplatesExportFlags struct {
	project string
	force   bool
	dryRun  bool
	asJSON  bool
}

// specTemplatesExportPayload is what `gintrack spec templates export --json`
// prints.
type specTemplatesExportPayload struct {
	Project   string                `json:"project"`
	DryRun    bool                  `json:"dryRun"`
	Templates []core.TemplateExport `json:"templates"`
}

func newSpecTemplatesExportCommand(flags *globalFlags) *cobra.Command {
	local := &specTemplatesExportFlags{}
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write the embedded spec templates to <docs>/.pmngr/templates/ for customisation",
		Long: `Write the spec and requirement templates the binary ships into
<docs>/.pmngr/templates/spec.md and requirement.md of one project, where the
team can edit them. A valid file there wins over the embedded template for
every new spec and requirement, in the web editor, the Add-requirement dialog
and the MCP create tools; an invalid one is reported W-TEMPLATE-INVALID by
gintrack doctor and the embedded template is used instead (ADR-038).

Each file is created when missing and left alone when it already holds the
embedded text, so running the command twice writes nothing. A file that
differs was edited: it is skipped unless --force overwrites it with the
embedded text. --dry-run prints the same outcome and writes nothing.

--project is required only when the workspace holds more than one project.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSpecTemplatesExport(cmd, flags, local)
		},
	}
	f := cmd.Flags()
	f.StringVar(&local.project, "project", "", "project key (required when the workspace holds more than one)")
	f.BoolVar(&local.force, "force", false, "overwrite a template file that was edited")
	f.BoolVar(&local.dryRun, "dry-run", false, "print what would be written, write nothing")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func runSpecTemplatesExport(cmd *cobra.Command, flags *globalFlags, local *specTemplatesExportFlags) error {
	res, err := flags.resolve()
	if err != nil {
		return err
	}
	v, err := openVault(cmd.Context(), res, false)
	if err != nil {
		return err
	}
	var p projectView
	if key := strings.TrimSpace(local.project); key != "" {
		p, err = v.project(core.ProjectKey(strings.ToUpper(key)))
	} else {
		p, err = v.only()
	}
	if err != nil {
		return err
	}
	exported, err := core.ExportSpecTemplates(v.FS, p.Ref.BacklogPath, local.force, local.dryRun)
	if err != nil {
		return fmt.Errorf("export the templates of %s: %w", p.Ref.Key, err)
	}
	payload := specTemplatesExportPayload{Project: string(p.Ref.Key), DryRun: local.dryRun}
	for _, e := range exported {
		_, rel := repoPath(e.Path)
		payload.Templates = append(payload.Templates, core.TemplateExport{Name: e.Name, Path: rel, Action: e.Action})
	}

	out := flags.printer(cmd, local.asJSON)
	if out.JSONMode() {
		return render(out.JSON(payload))
	}
	skipped := false
	for _, e := range exported {
		action := e.Action
		if local.dryRun && (action == core.TemplateCreated || action == core.TemplateOverwritten) {
			action = "would be " + action
		}
		out.Printf("%-11s %s\n", action, displayPath(e.Path))
		skipped = skipped || e.Action == core.TemplateSkipped
	}
	if skipped {
		_, _ = fmt.Fprintln(out.Notes(), "an edited template was kept; pass --force to overwrite it with the embedded text")
	}
	return nil
}
