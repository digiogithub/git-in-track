package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
)

// addFlags mirrors the flags of docs/07 section 4.2.
type addFlags struct {
	team   bool
	docs   string
	noGit  bool
	asJSON bool
}

// addPayload is what `gintrack add --json` prints.
type addPayload struct {
	Workspace string   `json:"workspace"`
	Repo      repoInfo `json:"repo"`
	Git       bool     `json:"git"`
	Projects  []string `json:"projects"`
	Items     int      `json:"items"`
	Config    string   `json:"config"`
}

// newAddCommand registers a repository in the active workspace.
func newAddCommand(flags *globalFlags) *cobra.Command {
	local := &addFlags{}

	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Register a repository",
		Long: `Register a git working tree so that every other command sees it.

The role and the documentation folder are detected from the markers of the data
model: a team.yaml at the root makes it a team repository, and the folder that
holds .pmngr/project.yaml is the documentation folder. Nothing is written to the
repository; only the gintrack configuration file changes.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, flags, local, args[0])
		},
	}

	cmd.Flags().BoolVar(&local.team, "team", false, "register as a team repository")
	cmd.Flags().StringVar(&local.docs, "docs", "", "documentation folder, relative to the repository root")
	cmd.Flags().BoolVar(&local.noGit, "no-git", false, "register a folder that is not a git working tree")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runAdd resolves the configuration, registers the repository and reports what
// was detected.
func runAdd(cmd *cobra.Command, flags *globalFlags, local *addFlags, target string) error {
	res, err := flags.resolve()
	if err != nil {
		return err
	}
	role := config.RoleProject
	if local.team {
		role = config.RoleTeam
	}
	repo, err := res.Config.AddRepoWithOptions(target, config.AddOptions{
		Role:       role,
		DocsFolder: local.docs,
		NoGit:      local.noGit,
		Workspace:  res.Workspace,
		Env:        flags.reader(),
	})
	if err != nil {
		return fmt.Errorf("register %s: %w", target, err)
	}
	if err := flags.save(res); err != nil {
		return err
	}

	view, err := openRepo(cmd.Context(), repo, true)
	if err != nil {
		return err
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(addPayload{
			Workspace: res.Workspace,
			Repo:      newRepoInfo(repo),
			Git:       config.IsGitRepo(repo.Path),
			Projects:  view.Keys(),
			Items:     view.Stats.Items,
			Config:    res.Path,
		}))
	}
	p.Printf("added %s repository %s  %s  (docs: %s, %s)\n",
		repo.Role, repo.ID, repo.Path, repo.DocsFolder, describeProjects(view))
	if !config.IsGitRepo(repo.Path) {
		p.Warnf("warning: %s is not a git working tree; git operations will be unavailable\n", repo.Path)
	}
	if len(view.Projects) == 0 {
		p.Warnf("warning: no .pmngr/project.yaml was found under %s\n", repo.Path)
	}
	for _, ref := range view.Projects {
		p.Printf("  %s  %s  %s\n", ref.Key, displayName(ref), ref.BacklogPath)
	}
	p.Printf("configuration: %s\n", res.Path)
	return nil
}

// describeProjects summarizes what the freshly indexed repository holds.
func describeProjects(view *repoView) string {
	switch len(view.Projects) {
	case 0:
		return "no project found"
	case 1:
		return plural(view.Stats.Items, "item", "items")
	default:
		return plural(len(view.Projects), "project", "projects") + ", " + plural(view.Stats.Items, "item", "items")
	}
}

// displayName returns the human name of a project, falling back to its key.
func displayName(ref core.ProjectRef) string {
	if ref.Name != "" {
		return ref.Name
	}
	return string(ref.Key)
}
