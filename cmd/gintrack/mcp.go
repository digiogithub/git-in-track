package main

import (
	"fmt"
	"log/slog"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
	"github.com/digiogithub/git-in-track/internal/mcp"
	corevault "github.com/digiogithub/git-in-track/internal/vault"
)

// mcpFlags mirrors the flags of docs/07 section 4.9 and docs/08 section 2.1.
type mcpFlags struct {
	allowWrite bool
	agent      string
	repos      []string
	listTools  bool
}

// newMCPCommand runs the Model Context Protocol server over stdio. The command
// owns no logic beyond mounting the workspace and handing it to internal/mcp.
func newMCPCommand(build buildInfo, flags *globalFlags) *cobra.Command {
	local := &mcpFlags{}

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the backlog to AI agents over the Model Context Protocol",
		Long: `Mcp speaks the Model Context Protocol over stdin and stdout, so that an agent
runtime can spawn it as a tool server. It exposes the workspace's backlog and
knowledge base as typed tools: list_items, search_items, get_item, create_epic,
create_story, create_task, create_milestone, update_item, add_comment,
move_on_board, list_kb_pages, get_kb_page and search_kb.

The server is read-only unless --allow-write is given, and the write tools are
then advertised alongside the read ones. Writes go through the same validation
the web UI goes through, and land in the working tree as ordinary file changes.

Nothing but protocol frames is written to stdout; logs go to stderr.

The same tools are served over streamable HTTP at POST /mcp by
` + "`gintrack serve --mcp-http`" + `, which is what to use when the companion is
already running: one index and one watcher, shared with the web UI.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCP(cmd, build, flags, local)
		},
	}

	cmd.Flags().BoolVar(&local.allowWrite, "allow-write", false,
		"advertise the write tools; without it the server is read-only")
	cmd.Flags().StringVar(&local.agent, "agent", "",
		"agent name recorded as the author of comments it writes")
	cmd.Flags().StringArrayVar(&local.repos, "repo", nil,
		"serve this repository without registering it; repeatable")
	cmd.Flags().BoolVar(&local.listTools, "list-tools", false,
		"print the tools this server would advertise and exit")
	return cmd
}

// runMCP mounts the workspace and serves it over stdio until the process is
// interrupted or the client disconnects.
func runMCP(cmd *cobra.Command, build buildInfo, flags *globalFlags, local *mcpFlags) error {
	res, err := flags.resolve()
	if err != nil {
		return err
	}
	repos, err := mcpRepos(res, local.repos)
	if err != nil {
		return err
	}

	space, roots, err := mountWorkspace(repos, build.Version)
	if err != nil {
		return err
	}
	srv, err := mcp.New(mcp.Options{
		Core:       space,
		Version:    build.Version,
		Agent:      local.agent,
		AllowWrite: local.allowWrite,
		Roots:      roots,
		// stdio carries protocol frames on stdout and nothing else, so the
		// logger is pinned to stderr whatever the global configuration says.
		Logger: slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil)),
	})
	if err != nil {
		return fmt.Errorf("start the MCP server: %w", err)
	}

	if local.listTools {
		for _, name := range srv.Tools() {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), name); err != nil {
				return fmt.Errorf("print the tool list: %w", err)
			}
		}
		return nil
	}

	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"gintrack mcp %s: workspace %s, %d repositories, %d tools (%s)\n",
		build.Version, res.Workspace, len(repos), len(srv.Tools()), writeMode(local.allowWrite))

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := srv.ServeStdio(ctx); err != nil {
		return fmt.Errorf("serve MCP over stdio: %w", err)
	}
	return nil
}

// writeMode renders the posture on the startup line.
func writeMode(allowWrite bool) string {
	if allowWrite {
		return "writes enabled"
	}
	return "read-only"
}

// mcpRepos resolves the repositories to serve: the ad-hoc --repo paths first,
// then the ones registered in the workspace.
func mcpRepos(res *config.Resolution, extra []string) ([]config.Repo, error) {
	var repos []config.Repo
	seen := map[string]bool{}
	for _, p := range extra {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", p, err)
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		repos = append(repos, config.Repo{
			ID: filepath.Base(abs), Path: abs, Role: config.RoleProject,
		})
	}
	for _, repo := range res.Config.WorkspaceRepos(res.Workspace) {
		if seen[repo.Path] {
			continue
		}
		seen[repo.Path] = true
		repos = append(repos, repo)
	}
	if len(repos) == 0 {
		return nil, notFoundf(
			"no repository is registered in workspace %q: run `gintrack add <path>` or pass --repo",
			res.Workspace)
	}
	return repos, nil
}

// mountWorkspace opens every repository as a vault and attaches it to one
// workspace — the same corevault.Workspace the companion server and the browser
// worker drive, so an agent and a human see one implementation of every query.
// It also returns the host directories the path guard confines paths to.
func mountWorkspace(repos []config.Repo, version string) (*corevault.Workspace, []string, error) {
	space := corevault.NewWorkspace()
	space.SetVersion(version)
	roots := make([]string, 0, len(repos))
	for _, repo := range repos {
		fsys, err := osfs.New(repo.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("open %s: %w", repo.ID, err)
		}
		v, err := corevault.Open(fsys, filepath.Base(filepath.Clean(repo.Path)))
		if err != nil {
			return nil, nil, fmt.Errorf("index %s: %w", repo.ID, err)
		}
		role := string(repo.Role)
		if role == "" {
			role = corevault.RoleProject
		}
		if _, err := space.Attach(repo.ID, role, v); err != nil {
			return nil, nil, fmt.Errorf("attach %s: %w", repo.ID, err)
		}
		roots = append(roots, fsys.Root())
	}
	return space, roots, nil
}
