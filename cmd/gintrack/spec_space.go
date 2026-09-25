package main

import (
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
	corevault "github.com/digiogithub/git-in-track/internal/vault"
)

// This file is what the `gintrack spec` query commands share (GIT-US-0125):
// the workspace they run against. It is the one `gintrack mcp` serves — the
// same corevault.Workspace, with the same requirement trace, coverage and
// impact seams installed by installMCPTraceSeams — so a command and the MCP
// tool of the same name answer from one implementation. Pando is not wired
// here, exactly as over stdio: impact tiers 2 and 3 report unavailable and
// tier 1 still answers.

// specSpace is a mounted workspace for one spec command.
type specSpace struct {
	space *corevault.Workspace
}

// specSpaceOptions shape how the workspace is mounted.
type specSpaceOptions struct {
	// seams installs the trace, coverage and impact backends; lint needs none.
	seams bool
	// dryRun mounts every repository over an in-memory overlay: writes run
	// the real write path and never reach the disk.
	dryRun bool
}

// openSpecSpace resolves the configuration and mounts every registered
// repository.
func openSpecSpace(cmd *cobra.Command, flags *globalFlags, opts specSpaceOptions) (*specSpace, error) {
	res, err := flags.resolve()
	if err != nil {
		return nil, err
	}
	repos, err := mcpRepos(res, nil)
	if err != nil {
		return nil, err
	}
	var wrap func(core.FS) core.FS
	if opts.dryRun {
		wrap = func(fsys core.FS) core.FS { return newOverlayFS(fsys) }
	}
	space, mounts, err := mountWorkspaceOver(repos, "", wrap)
	if err != nil {
		return nil, err
	}
	if opts.seams {
		logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil))
		installMCPTraceSeams(mounts, res.Config.Git.Backend, res.Config.CacheDir(res.Path), logger)
	}
	return &specSpace{space: space}, nil
}

// projectMounts returns the project repositories: the ones that can hold
// specs. A team repository holds boards and sprints only.
func (s *specSpace) projectMounts() []*corevault.Mount {
	var out []*corevault.Mount
	for _, m := range s.space.Mounts() {
		if m.Role != corevault.RoleTeam {
			out = append(out, m)
		}
	}
	return out
}

// needsProject reports a usage error when a query that addresses a single
// repository names none and the workspace holds more than one.
func (s *specSpace) needsProject(project, story string) error {
	if project != "" || story != "" {
		return nil
	}
	if n := len(s.projectMounts()); n > 1 {
		return usagef("--project is required: the workspace holds %d project repositories", n)
	}
	return nil
}

// isSpecID reports whether an argument names a whole spec rather than one of
// its requirements.
func isSpecID(arg string) bool {
	_, code, _, err := core.ParseItemID(strings.TrimSpace(arg))
	return err == nil && code == core.CodeSpec
}
