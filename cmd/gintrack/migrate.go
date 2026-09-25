package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
)

// migrateFlags mirrors the flags of docs/07 section 4.21.
type migrateFlags struct {
	to      int
	project string
	all     bool
	dryRun  bool
	asJSON  bool
}

// migrateProject is one project.yaml in the report.
type migrateProject struct {
	Key     core.ProjectKey `json:"key"`
	Repo    string          `json:"repo"`
	Path    string          `json:"path"`
	From    int             `json:"from"`
	To      int             `json:"to"`
	Changed bool            `json:"changed"`
	Removed []string        `json:"removed,omitempty"`
	Added   []string        `json:"added,omitempty"`
}

// migratePayload is what `gintrack migrate --json` prints.
type migratePayload struct {
	To       int              `json:"to"`
	DryRun   bool             `json:"dryRun,omitempty"`
	Projects []migrateProject `json:"projects"`
	Changed  int              `json:"changed"`
}

// newMigrateCommand raises the schema of project.yaml explicitly (R-EVO-4).
func newMigrateCommand(flags *globalFlags) *cobra.Command {
	local := &migrateFlags{}

	cmd := &cobra.Command{
		Use:   "migrate --to <schema>",
		Short: "Raise the schema of a project explicitly",
		Long: `Raise project.yaml to a newer schema before anything needs it, so that the
upgrade is its own reviewable change instead of a line riding along with the
first spec (docs/03 section 21.10).

The only migration today is --to 2, the spec layer of ADR-037. It changes the
one "schema:" line of project.yaml and no other file; running it again on a
project already at the target changes nothing. A downgrade is refused: there is
no way back from schema 2 other than reverting the commit.

With one project in the workspace no selector is needed; otherwise name it with
--project, or pass --all to migrate every project of the workspace. Nothing is
written unless every selected project can be migrated. The command does not
commit: review the diff and commit it on its own.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMigrate(cmd, flags, local)
		},
	}

	f := cmd.Flags()
	f.IntVar(&local.to, "to", 0, "the target schema (required)")
	f.StringVar(&local.project, "project", "", "migrate one project, by key")
	f.BoolVar(&local.all, "all", false, "migrate every project of the workspace")
	f.BoolVar(&local.dryRun, "dry-run", false, "print the diff summary, write nothing")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runMigrate plans every selected project, then writes them all or none.
func runMigrate(cmd *cobra.Command, flags *globalFlags, local *migrateFlags) error {
	if !cmd.Flags().Changed("to") {
		return usagef("--to is required: gintrack migrate --to %d", core.SupportedSchema)
	}
	if local.all && local.project != "" {
		return usagef("--project and --all are mutually exclusive")
	}
	v, err := openItemVault(cmd, flags)
	if err != nil {
		return err
	}
	targets, err := migrateTargets(v, local)
	if err != nil {
		return err
	}

	type planned struct {
		view projectView
		raw  []byte
		plan *core.SchemaMigration
	}
	plans := make([]planned, 0, len(targets))
	for _, t := range targets {
		raw, err := v.FS.ReadFile(t.Ref.ConfigPath)
		if err != nil {
			return fmt.Errorf("%s: %w", displayPath(t.Ref.ConfigPath), err)
		}
		plan, err := core.PlanSchemaMigration(raw, local.to)
		if err != nil {
			return migrateError(t, err)
		}
		plans = append(plans, planned{view: t, raw: raw, plan: plan})
	}

	payload := migratePayload{To: local.to, DryRun: local.dryRun, Projects: []migrateProject{}}
	for _, pl := range plans {
		repo, rel := repoPath(pl.view.Ref.ConfigPath)
		payload.Projects = append(payload.Projects, migrateProject{
			Key: pl.view.Ref.Key, Repo: repo, Path: rel,
			From: pl.plan.From, To: pl.plan.To, Changed: pl.plan.Changed(),
			Removed: pl.plan.Removed, Added: pl.plan.Added,
		})
		if pl.plan.Changed() {
			payload.Changed++
		}
	}

	if !local.dryRun {
		for _, pl := range plans {
			if !pl.plan.Changed() {
				continue
			}
			// The file-level rev: the bytes must be the ones the plan was made
			// from, or someone wrote in between and the plan is stale.
			now, err := v.FS.ReadFile(pl.view.Ref.ConfigPath)
			if err != nil {
				return fmt.Errorf("%s: %w", displayPath(pl.view.Ref.ConfigPath), err)
			}
			if !bytes.Equal(now, pl.raw) {
				return failf(exitConflict, "%s changed while it was being migrated: run the command again",
					displayPath(pl.view.Ref.ConfigPath))
			}
			if err := v.FS.WriteFile(pl.view.Ref.ConfigPath, pl.plan.Data); err != nil {
				return fmt.Errorf("%s: %w", displayPath(pl.view.Ref.ConfigPath), err)
			}
		}
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	printMigrate(p.Printf, payload)
	return nil
}

// migrateTargets selects the projects the invocation names.
func migrateTargets(v *vault, local *migrateFlags) ([]projectView, error) {
	switch {
	case local.project != "":
		p, err := v.project(core.ProjectKey(strings.TrimSpace(local.project)))
		if err != nil {
			return nil, err
		}
		return []projectView{p}, nil
	case local.all:
		if len(v.Projects) == 0 {
			return nil, notFoundf("no project is registered in workspace %q", v.Workspace)
		}
		return v.Projects, nil
	case len(v.Projects) == 0:
		return nil, notFoundf("no project is registered in workspace %q", v.Workspace)
	case len(v.Projects) > 1:
		return nil, usagef("--project or --all is required: the workspace holds %s", strings.Join(v.keys(), ", "))
	}
	return v.Projects, nil
}

// migrateError maps a refused plan onto the documented exit codes: a target
// this build cannot write is a bad invocation, a downgrade or a project.yaml
// without a schema is a validation error.
func migrateError(t projectView, err error) error {
	wrapped := fmt.Errorf("%s (%s): %w", t.Ref.Key, displayPath(t.Ref.ConfigPath), err)
	var refusal *core.SchemaMigrationError
	switch {
	case !errors.As(err, &refusal):
		return wrapped
	case refusal.To < core.InitialSchema || refusal.To > core.SupportedSchema:
		return fail(exitUsage, wrapped)
	default:
		return fail(exitValidation, wrapped)
	}
}

// printMigrate renders the human report: one line per project and the lines
// that change, like a diff.
func printMigrate(printf func(string, ...any), payload migratePayload) {
	for _, pr := range payload.Projects {
		where := pr.Repo + ":" + pr.Path
		if !pr.Changed {
			printf("%s  %s  already at schema %d, nothing to do\n", pr.Key, where, pr.From)
			continue
		}
		verb := "migrated"
		if payload.DryRun {
			verb = "would migrate"
		}
		printf("%s  %s  %s schema %d -> %d\n", pr.Key, where, verb, pr.From, pr.To)
		for _, l := range pr.Removed {
			printf("  - %s\n", l)
		}
		for _, l := range pr.Added {
			printf("  + %s\n", l)
		}
	}
	switch {
	case payload.DryRun:
		printf("nothing was changed (--dry-run)\n")
	case payload.Changed > 0:
		printf("%s migrated: review the diff and commit it on its own\n",
			plural(payload.Changed, "project", "projects"))
	}
}
