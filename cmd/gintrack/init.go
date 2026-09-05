package main

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
)

// initFlags mirrors the flags of docs/07 section 4.1.
type initFlags struct {
	key         string
	name        string
	description string
	timezone    string
	docs        string
	register    bool
	team        bool
	asJSON      bool
}

// initPayload is what `gintrack init --json` prints.
type initPayload struct {
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	DocsFolder  string    `json:"docsFolder"`
	BacklogPath string    `json:"backlogPath"`
	ConfigPath  string    `json:"configPath"`
	Repo        *repoInfo `json:"repo,omitempty"`
}

// newInitCommand scaffolds a backlog in a repository that has none.
func newInitCommand(flags *globalFlags) *cobra.Command {
	local := &initFlags{}

	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Create a project backlog in a repository",
		Long: `Write a new .pmngr/ backlog into a repository that does not have one yet.

The documentation folder is where the project's Markdown lives; the backlog is
the .pmngr/ folder inside it. Nothing is overwritten: a folder that already
holds a project.yaml is refused.

The command is fully non-interactive, so agents and scripts can use it:

  gintrack init . --key ACME --name "ACME Platform"
  gintrack init ../api --key API --docs docs --register

Project discovery probes the repository root and its first-level directories, so
a backlog deeper than that — a monorepo's apps/api/docs — is found only when the
registration declares it. --register does that declaration for you.`,
		Args: rangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			return runInit(cmd, flags, local, target)
		},
	}

	cmd.Flags().StringVar(&local.key, "key", "", "project key, matching [A-Z][A-Z0-9]{1,9} (required)")
	cmd.Flags().StringVar(&local.name, "name", "", "human name of the project; defaults to the key")
	cmd.Flags().StringVar(&local.description, "description", "", "one paragraph shown in project pickers")
	cmd.Flags().StringVar(&local.timezone, "timezone", "", "IANA timezone used to present date-only fields")
	cmd.Flags().StringVar(&local.docs, "docs", "docs", `documentation folder, relative to the repository root ("." for the root)`)
	cmd.Flags().BoolVar(&local.register, "register", false, "register the repository in the active workspace as well")
	cmd.Flags().BoolVar(&local.team, "team", false, "with --register, register it as a team repository")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runInit writes the backlog and, with --register, registers the repository.
func runInit(cmd *cobra.Command, flags *globalFlags, local *initFlags, target string) error {
	key := core.ProjectKey(strings.TrimSpace(local.key))
	if key == "" {
		return usagef("--key is required: a project key matches [A-Z][A-Z0-9]{1,9}, for example ACME")
	}
	if !core.ValidProjectKey(key) {
		return failf(exitValidation, "%q is not a project key: it must match [A-Z][A-Z0-9]{1,9}", key)
	}

	abs, err := config.Expand(target, flags.reader())
	if err != nil {
		return fmt.Errorf("resolve %s: %w", target, err)
	}
	fsys, err := osfs.New(abs)
	if err != nil {
		return fmt.Errorf("open %s: %w", abs, err)
	}
	docs := path.Clean(filepath.ToSlash(strings.TrimSpace(local.docs)))
	if docs == "" {
		docs = "."
	}

	ref, err := core.CreateProject(fsys, docs, core.NewProject{
		Key:         key,
		Name:        local.name,
		Description: local.description,
		Timezone:    local.timezone,
	})
	switch {
	case errors.Is(err, core.ErrProjectExists):
		return failf(exitConflict, "%s already holds a project: %v", abs, err)
	case errors.Is(err, core.ErrProjectKey):
		return failf(exitValidation, "%v", err)
	case err != nil:
		return fmt.Errorf("create the project in %s: %w", abs, err)
	}

	payload := initPayload{
		Key:         string(ref.Key),
		Name:        ref.Name,
		DocsFolder:  ref.DocsPath,
		BacklogPath: ref.BacklogPath,
		ConfigPath:  ref.ConfigPath,
	}

	if local.register {
		role := config.RoleProject
		if local.team {
			role = config.RoleTeam
		}
		res, resErr := flags.resolve()
		if resErr != nil {
			return resErr
		}
		repo, addErr := res.Config.AddRepoWithOptions(abs, config.AddOptions{
			Role:        role,
			DocsFolder:  ref.DocsPath,
			DocsFolders: []string{ref.DocsPath},
			Workspace:   res.Workspace,
			Env:         flags.reader(),
		})
		if addErr != nil {
			return fmt.Errorf("register %s: %w", abs, addErr)
		}
		if saveErr := flags.save(res); saveErr != nil {
			return saveErr
		}
		info := newRepoInfo(repo)
		payload.Repo = &info
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	p.Printf("created project %s (%s) in %s\n", payload.Key, payload.Name, payload.ConfigPath)
	if payload.Repo != nil {
		p.Printf("registered %s repository %s  %s\n", payload.Repo.Role, payload.Repo.ID, payload.Repo.Path)
	} else {
		p.Printf("register the repository with `gintrack add %s --docs %s`\n", abs, payload.DocsFolder)
	}
	return nil
}
