package main

import (
	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
)

// indexFlags mirrors the flags of docs/07 section 4.4 this phase implements.
// The watcher lives in the server process, so --watch is not offered here.
type indexFlags struct {
	full   bool
	asJSON bool
}

// indexRepo is the per-repository result of an index build.
type indexRepo struct {
	ID          string           `json:"id"`
	Path        string           `json:"path"`
	Projects    []string         `json:"projects"`
	Files       int              `json:"files"`
	Parsed      int              `json:"parsed"`
	Items       int              `json:"items"`
	Comments    int              `json:"comments"`
	Pages       int              `json:"pages"`
	ByType      map[string]int   `json:"byType,omitempty"`
	Errors      int              `json:"errors"`
	Warnings    int              `json:"warnings"`
	DurationMs  int64            `json:"durationMs"`
	Diagnostics []diagnosticInfo `json:"diagnostics,omitempty"`
	Error       string           `json:"error,omitempty"`

	// stats keeps the build the text renderer prints from.
	stats core.IndexStats
}

// indexPayload is what `gintrack index --json` prints.
type indexPayload struct {
	Workspace string      `json:"workspace"`
	Full      bool        `json:"full"`
	Repos     []indexRepo `json:"repos"`
	Items     int         `json:"items"`
	Errors    int         `json:"errors"`
	Warnings  int         `json:"warnings"`
}

// diagnosticInfo is one finding, in the shape doctor and index both print.
type diagnosticInfo struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Field    string `json:"field,omitempty"`
	Line     int    `json:"line,omitempty"`
	Message  string `json:"message"`
}

// newDiagnosticInfo projects a core diagnostic onto its payload.
func newDiagnosticInfo(d core.Diagnostic) diagnosticInfo {
	return diagnosticInfo{
		Code:     string(d.Code),
		Severity: string(d.Severity),
		Path:     d.Path,
		Field:    d.Field,
		Line:     d.Line,
		Message:  d.Message,
	}
}

// newIndexCommand rebuilds the index of one or every registered repository.
func newIndexCommand(flags *globalFlags) *cobra.Command {
	local := &indexFlags{}

	cmd := &cobra.Command{
		Use:   "index [id]",
		Short: "Index the registered repositories and report what was found",
		Long: `Read every backlog file of the registered repositories and report the counts,
the timing and the diagnostics the parse produced.

Pass a repository id to index a single one. Keeping the index warm while files
change is the job of ` + "`gintrack serve`" + `, which runs the same indexer behind a
file watcher.`,
		Args: rangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIndex(cmd, flags, local, args)
		},
	}

	cmd.Flags().BoolVar(&local.full, "full", false, "ignore the cache and re-parse every file")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runIndex builds the index of every selected repository.
func runIndex(cmd *cobra.Command, flags *globalFlags, local *indexFlags, args []string) error {
	res, err := flags.resolve()
	if err != nil {
		return err
	}
	repos := res.Config.WorkspaceRepos(res.Workspace)
	if len(args) == 1 {
		repo, ok := res.Config.Repo(args[0])
		if !ok {
			return notFoundf("no repository %q is registered", args[0])
		}
		repos = []config.Repo{repo}
	}
	if len(repos) == 0 {
		return notFoundf("no repository is registered in workspace %q: run `gintrack add <path>`", res.Workspace)
	}

	payload := indexPayload{Workspace: res.Workspace, Full: local.full}
	for _, repo := range repos {
		row := indexRepo{ID: repo.ID, Path: repo.Path}
		view, err := openRepo(cmd.Context(), repo, local.full)
		if err != nil {
			row.Error = err.Error()
			payload.Errors++
			payload.Repos = append(payload.Repos, row)
			continue
		}
		s := view.Stats
		row.Projects = view.Keys()
		row.Files, row.Parsed, row.Items = s.Files, s.Parsed, s.Items
		row.Comments, row.Pages = s.Comments, s.Pages
		row.Errors, row.Warnings = s.Errors, s.Warnings
		row.DurationMs = s.Duration.Milliseconds()
		row.stats = s
		row.ByType = make(map[string]int, len(s.ByType))
		for t, n := range s.ByType {
			row.ByType[string(t)] = n
		}
		for _, d := range view.Index.Warnings() {
			row.Diagnostics = append(row.Diagnostics, newDiagnosticInfo(d))
		}
		payload.Items += s.Items
		payload.Errors += s.Errors
		payload.Warnings += s.Warnings
		payload.Repos = append(payload.Repos, row)
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	for _, row := range payload.Repos {
		if row.Error != "" {
			p.Warnf("%s  %s\n", row.ID, row.Error)
			continue
		}
		p.Printf("%s  %s  %s  parsed in %s  %s\n",
			row.ID,
			plural(row.Items, "item", "items"),
			plural(row.Files, "file", "files"),
			humanDuration(row.stats.Duration),
			countsByType(row.stats.ByType),
		)
	}
	p.Printf("%s, %s\n", plural(payload.Errors, "error", "errors"), plural(payload.Warnings, "warning", "warnings"))
	if payload.Errors > 0 || payload.Warnings > 0 {
		p.Printf("run `gintrack doctor` for the details\n")
	}
	return nil
}
