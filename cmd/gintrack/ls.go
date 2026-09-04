package main

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
)

// lsFlags mirrors the flags of docs/07 section 4.3.
type lsFlags struct {
	all    bool
	asJSON bool
}

// repoInfo is the registration part of a repository payload.
type repoInfo struct {
	ID      string      `json:"id"`
	Path    string      `json:"path"`
	Role    config.Role `json:"role"`
	Docs    string      `json:"docs"`
	Enabled bool        `json:"enabled"`
}

// newRepoInfo projects a registration onto its payload.
func newRepoInfo(r config.Repo) repoInfo {
	return repoInfo{ID: r.ID, Path: r.Path, Role: r.Role, Docs: r.DocsFolder, Enabled: r.Enabled}
}

// lsRepo is one row of `gintrack ls --json`.
type lsRepo struct {
	repoInfo
	Workspace string   `json:"workspace"`
	Git       bool     `json:"git"`
	Projects  []string `json:"projects"`
	Items     int      `json:"items"`
	Pages     int      `json:"pages"`
	Errors    int      `json:"errors"`
	Warnings  int      `json:"warnings"`
	Error     string   `json:"error,omitempty"`
}

// lsPayload is what `gintrack ls --json` prints.
type lsPayload struct {
	Workspace string   `json:"workspace"`
	Repos     []lsRepo `json:"repos"`
}

// newLsCommand lists the registered repositories.
func newLsCommand(flags *globalFlags) *cobra.Command {
	local := &lsFlags{}

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List the registered repositories",
		Long: `List the repositories registered in the active workspace, with the role, the
documentation folder, the project keys they hold and how many items they carry.

Counting the items reads every backlog file, so the command is as fast as an
index build.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLs(cmd, flags, local)
		},
	}

	cmd.Flags().BoolVar(&local.all, "all", false, "list every workspace")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runLs indexes every registered repository and renders one row each.
func runLs(cmd *cobra.Command, flags *globalFlags, local *lsFlags) error {
	res, err := flags.resolve()
	if err != nil {
		return err
	}
	p := flags.printer(cmd, local.asJSON)

	rows := make([]lsRepo, 0, len(res.Config.Repos))
	for _, ws := range workspaceNames(res, local.all) {
		for _, repo := range res.Config.WorkspaceRepos(ws) {
			row := lsRepo{repoInfo: newRepoInfo(repo), Workspace: ws, Git: config.IsGitRepo(repo.Path)}
			view, err := openRepo(cmd.Context(), repo, false)
			if err != nil {
				row.Error = err.Error()
			} else {
				row.Projects = view.Keys()
				row.Items = view.Stats.Items
				row.Pages = view.Stats.Pages
				row.Errors = view.Stats.Errors
				row.Warnings = view.Stats.Warnings
			}
			rows = append(rows, row)
		}
	}

	if p.JSONMode() {
		return render(p.JSON(lsPayload{Workspace: res.Workspace, Repos: rows}))
	}
	if len(rows) == 0 {
		p.Printf("no repository is registered in workspace %q\nregister one with `gintrack add <path>`\n", res.Workspace)
		return nil
	}
	headers := []string{"ID", "ROLE", "PATH", "DOCS", "KEYS", "ITEMS"}
	if local.all {
		headers = append([]string{"WORKSPACE"}, headers...)
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		cells := []string{r.ID, string(r.Role), r.Path, orDash(r.Docs), orDash(joinOrDash(r.Projects)), strconv.Itoa(r.Items)}
		if local.all {
			cells = append([]string{r.Workspace}, cells...)
		}
		table = append(table, cells)
	}
	if err := p.Table(headers, table); err != nil {
		return render(err)
	}
	for _, r := range rows {
		if r.Error != "" {
			p.Warnf("warning: %s: %s\n", r.ID, r.Error)
		}
	}
	return nil
}

// workspaceNames returns the workspaces a listing covers.
func workspaceNames(res *config.Resolution, all bool) []string {
	if !all {
		return []string{res.Workspace}
	}
	names := make([]string, 0, len(res.Config.Workspaces))
	for _, w := range res.Config.Workspaces {
		names = append(names, w.Name)
	}
	return names
}
