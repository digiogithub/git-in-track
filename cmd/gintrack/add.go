package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
)

// addFlags mirrors the flags of docs/07 section 4.2.
type addFlags struct {
	team   bool
	docs   []string
	noGit  bool
	key    string
	name   string
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
	// Created is the project `--key` scaffolded, absent when none was.
	Created *initPayload `json:"created,omitempty"`
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
repository; only the gintrack configuration file changes.

Every documentation folder detection finds is recorded in the registration, so a
monorepo keeps working under the bounded discovery rule: discovery probes the
repository root and its first-level directories, plus the folders declared here.
Repeat --docs to declare more of them.

A repository with no backlog at all is a repository with nothing to show. Pass
--key to create the project while registering, or run "gintrack init" first:

  gintrack add ~/code/acme --key ACME --name "ACME Platform" --docs docs`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, flags, local, args[0])
		},
	}

	cmd.Flags().BoolVar(&local.team, "team", false, "register as a team repository")
	cmd.Flags().StringArrayVar(&local.docs, "docs", nil, "documentation folder, relative to the repository root (repeatable)")
	cmd.Flags().StringVar(&local.key, "key", "", "create a project with this key when the repository has no backlog")
	cmd.Flags().StringVar(&local.name, "name", "", "human name of the created project; defaults to the key")
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
	primary := ""
	if len(local.docs) > 0 {
		primary = local.docs[0]
	}
	repo, err := res.Config.AddRepoWithOptions(target, config.AddOptions{
		Role:        role,
		DocsFolder:  primary,
		DocsFolders: local.docs,
		NoGit:       local.noGit,
		Workspace:   res.Workspace,
		Env:         flags.reader(),
	})
	if err != nil {
		return fmt.Errorf("register %s: %w", target, err)
	}

	// The registration is edited in place, so that a project created here is
	// declared by the very entry that was just appended.
	registered := &res.Config.Repos[len(res.Config.Repos)-1]
	for i := range res.Config.Repos {
		if res.Config.Repos[i].ID == repo.ID {
			registered = &res.Config.Repos[i]
			break
		}
	}
	created, err := createProjectOnAdd(registered, local)
	if err != nil {
		return err
	}
	repo = *registered
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
			Created:   created,
		}))
	}
	p.Printf("added %s repository %s  %s  (docs: %s, %s)\n",
		repo.Role, repo.ID, repo.Path, repo.DocsFolder, describeProjects(view))
	if !config.IsGitRepo(repo.Path) {
		p.Warnf("warning: %s is not a git working tree; git operations will be unavailable\n", repo.Path)
	}
	for _, candidate := range undeclaredCandidates(repo) {
		p.Warnf("note: %s also holds a backlog; declare it with `gintrack add --docs %s` to index it\n",
			candidate, candidate)
	}
	if created != nil {
		p.Printf("created project %s (%s) in %s\n", created.Key, created.Name, created.ConfigPath)
	}
	if len(view.Projects) == 0 {
		p.Warnf("warning: no .pmngr/project.yaml was found under %s\n", repo.Path)
		p.Warnf("create one with `gintrack init %s --key <KEY>`\n", repo.Path)
	}
	for _, ref := range view.Projects {
		p.Printf("  %s  %s  %s\n", ref.Key, displayName(ref), ref.BacklogPath)
	}
	p.Printf("configuration: %s\n", res.Path)
	return nil
}

// createProjectOnAdd scaffolds the project `--key` asks for, and declares its
// folder on the registration so that discovery keeps finding it however deep it
// is. It returns nil when no key was given.
//
// It refuses to write into a repository that already holds a backlog: a
// repository with projects is opened, never re-created.
func createProjectOnAdd(repo *config.Repo, local *addFlags) (*initPayload, error) {
	key := core.ProjectKey(strings.TrimSpace(local.key))
	if key == "" {
		return nil, nil
	}
	fsys, err := osfs.New(repo.Path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", repo.Path, err)
	}
	// The folder is what --docs asked for, or the conventional docs/. The
	// detected folder is deliberately not used: --key means "there is nothing
	// here yet", and detection may have picked a backlog that is a fixture.
	docs := "docs"
	if len(local.docs) > 0 && strings.TrimSpace(local.docs[0]) != "" {
		docs = strings.TrimSpace(local.docs[0])
	}
	ref, err := core.CreateProject(fsys, docs, core.NewProject{Key: key, Name: local.name})
	switch {
	case errors.Is(err, core.ErrProjectExists):
		return nil, failf(exitConflict, "%s already holds a project: %v", repo.Path, err)
	case errors.Is(err, core.ErrProjectKey):
		return nil, failf(exitValidation, "%v", err)
	case err != nil:
		return nil, fmt.Errorf("create the project in %s: %w", repo.Path, err)
	}
	// Only what the user asked for stays declared: the folder just created and
	// any --docs given by hand. A folder detection guessed is dropped, so that
	// creating a project never silently imports a backlog found elsewhere.
	repo.DocsFolder = ref.DocsPath
	repo.DocsFolders = withDocsFolder(local.docs, ref.DocsPath)
	return &initPayload{
		Key:         string(ref.Key),
		Name:        ref.Name,
		DocsFolder:  ref.DocsPath,
		BacklogPath: ref.BacklogPath,
		ConfigPath:  ref.ConfigPath,
	}, nil
}

// undeclaredCandidates lists the documentation folders detection found that the
// registration does not declare. They are reported rather than declared: a
// backlog under testdata/ is a fixture, not a project of the user (ADR-018).
func undeclaredCandidates(repo config.Repo) []string {
	declared := make(map[string]bool)
	for _, folder := range repo.DeclaredDocsFolders() {
		declared[folder] = true
	}
	var out []string
	for _, candidate := range config.DocsCandidates(repo.Path) {
		if !declared[candidate] {
			out = append(out, candidate)
		}
	}
	return out
}

// withDocsFolder puts a documentation folder at the head of a declared list.
func withDocsFolder(folders []string, folder string) []string {
	out := []string{folder}
	for _, existing := range folders {
		if existing != folder {
			out = append(out, existing)
		}
	}
	return out
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
