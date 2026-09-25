package main

import (
	"fmt"
	"log/slog"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/mcp"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/server"
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
knowledge base as typed tools: thirty-two of them with writes enabled,
thirteen without.

The thirteen read-only tools are list_items, search_items, search_semantic,
get_item, list_requirements, spec_context, spec_coverage, spec_impact,
trace_requirement, list_inbox,
list_kb_pages, get_kb_page and search_kb. search_semantic ranks by meaning rather than by substring and needs
the Pando backend configured under "search.pando"; without one it refuses and
names search_items as the fallback, so an empty answer is never mistaken for
"nothing matches".

The nineteen that need writes are create_epic, create_story, create_task,
create_milestone, create_spec, create_requirement, create_inbox_item,
update_item, update_requirement, verify_requirement, add_comment, move_on_board, triage_inbox_item,
close_sprint, transfer_sprint_items, import_youtrack_issues,
push_comment_to_youtrack, publish_kb_page_to_youtrack and
sync_kb_page_from_youtrack.

Specs are addressed one requirement at a time: list_requirements returns
compact rows, get_item on a requirement ref (ACME-SP-0003.R2) returns only that
block and its entry, and update_requirement quotes the requirement's own rev,
so a write to one requirement never races another of the same spec.
spec_context returns what a story requires — its linked requirements with
one-line statements, scenarios, coverage and related pages, within a token
budget — and spec_coverage one coverage row per requirement. spec_impact
reports the requirements a diff affects, trace_requirement the
code, tests and work traced to one requirement, and verify_requirement stamps
a requirement whose linked tests all passed in the results "gintrack spec
ingest" recorded. The trace, coverage and impact backends are the companion's,
installed here too; without Pando the impact report answers its first tier and
reports the other two unavailable.

Three of them are the inbox: list_inbox reads a project's triage queue,
create_inbox_item files something into it and triage_inbox_item decides one
entry. Reach for create_inbox_item rather than create_story whenever the work
has not been agreed with a human: a report, a request, anything an agent noticed
on its own. It files the item as pending and leaves the type, the parent and the
status to whoever triages it. create_story is for work somebody already decided
to do.

The server is read-only unless writes are enabled, either with --allow-write or
with "mcp.allowWrite: true" in the configuration file, and the write tools are
then advertised alongside the read ones. Writes go through the same validation
the web UI goes through, and land in the working tree as ordinary file changes.
Run with --list-tools to see which surface a client would get.

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
		"advertise the write tools; defaults to mcp.allowWrite in the configuration")
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
	// The flag wins when it was typed; otherwise the configuration decides, so
	// that a user who enabled writes once does not have to teach every agent
	// runtime to pass the flag.
	allowWrite := local.allowWrite
	if !cmd.Flags().Changed("allow-write") {
		allowWrite = res.Config.MCP.AllowWrite
	}

	space, mounts, err := mountWorkspace(repos, build.Version)
	if err != nil {
		return err
	}
	roots := make([]string, 0, len(mounts))
	for _, m := range mounts {
		roots = append(roots, m.root)
	}
	logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil))
	fresh := newMCPFreshness(mounts, logger)
	srv, err := mcp.New(mcp.Options{
		Core:       space,
		Version:    build.Version,
		Agent:      local.agent,
		AllowWrite: allowWrite,
		Roots:      roots,
		BeforeCall: fresh.beforeCall,
		// stdio carries protocol frames on stdout and nothing else, so the
		// logger is pinned to stderr whatever the global configuration says.
		Logger: logger,
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
	// Semantic search goes through the same constructor `gintrack serve` uses,
	// so search_semantic reaches Pando over stdio too; with no Pando configured
	// it keeps answering `unavailable` (GIT-US-0121). The impact seam gets the
	// same client and searcher, so spec_impact tiers 2 and 3 answer over stdio
	// as they do over HTTP (GIT-US-0147).
	semantic := installMCPSemantic(res.Config, space, mounts, logger)
	defer func() { _ = semantic.Close() }()
	installMCPTraceSeams(mounts, res.Config.Git.Backend, res.Config.CacheDir(res.Path), semantic, logger)

	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"gintrack mcp %s: workspace %s, %d repositories, %d tools (%s)\n",
		build.Version, res.Workspace, len(repos), len(srv.Tools()), writeMode(allowWrite))

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	stopWatch := fresh.start(ctx)
	defer stopWatch()
	if err := srv.ServeStdio(ctx); err != nil {
		return fmt.Errorf("serve MCP over stdio: %w", err)
	}
	return nil
}

// installMCPTraceSeams gives every mounted repository the requirement trace,
// coverage and impact seams the companion installs (GIT-US-0124), through the
// companion's own constructor, so trace_requirement, verify_requirement and
// spec_impact answer over stdio exactly as they do over HTTP. The coverage
// evidence is the test-result cache `gintrack spec ingest` fills under the
// same cache directory. A repository git cannot open keeps trace and coverage
// and answers the impact query `unavailable`. Impact tiers 2 and 3 read the
// Pando client and semantic searcher of pandoHost at call time, the ones
// installMCPSemantic built (GIT-US-0147); a nil host, or one with no Pando
// configured, leaves them unavailable while tier 1 answers.
func installMCPTraceSeams(mounts []mcpMount, backend config.Backend, cacheDir string, pandoHost *server.SemanticHost, log *slog.Logger) {
	for _, m := range mounts {
		seams := server.TraceSeams{
			Root: m.root, CacheDir: cacheDir, ProjectID: pando.SanitizeProjectID(m.root),
			CallGraph: pandoHost.CallGraph, Semantic: pandoHost.Semantic,
		}
		repo, err := gitops.Open(m.root, gitops.Options{Backend: gitops.Kind(backend)})
		if err != nil {
			log.Debug("repository has no readable git history; the impact query is unavailable",
				"repo", m.id, "reason", err)
		} else {
			seams.Git = repo
		}
		server.InstallTraceSeams(m.vlt, seams)
	}
}

// installMCPSemantic installs the Pando-backed semantic searcher on the
// workspace from the `search.pando` section, with its tokens resolved the same
// way `gintrack serve` resolves them. It returns the host that hands the same
// Pando client and searcher to the impact seam and closes the Pando session;
// it is never nil.
func installMCPSemantic(cfg *config.Config, space *corevault.Workspace, mounts []mcpMount, log *slog.Logger) *server.SemanticHost {
	repos := make([]server.SemanticRepo, 0, len(mounts))
	for _, m := range mounts {
		repos = append(repos, server.SemanticRepo{
			ID: m.id, Path: m.root, Role: m.role, DocsFolders: m.docs, Vault: m.vlt,
		})
	}
	return server.InstallSemanticSearch(searchSettings(cfg).Pando, space, repos, log)
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
// It also returns the mounted repositories: their host directories are what the
// path guard confines paths to, and their vaults are what the watcher keeps
// current.
func mountWorkspace(repos []config.Repo, version string) (*corevault.Workspace, []mcpMount, error) {
	return mountWorkspaceOver(repos, version, nil)
}

// mountWorkspaceOver is mountWorkspace with every repository's file system
// passed through wrap first, when wrap is not nil. `gintrack spec verify`
// without --commit wraps each one in an in-memory overlay, so the stamp write
// path runs for real and nothing reaches the disk.
func mountWorkspaceOver(
	repos []config.Repo, version string, wrap func(core.FS) core.FS,
) (*corevault.Workspace, []mcpMount, error) {
	space := corevault.NewWorkspace()
	space.SetVersion(version)
	mounts := make([]mcpMount, 0, len(repos))
	for _, repo := range repos {
		fsys, err := osfs.New(repo.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("open %s: %w", repo.ID, err)
		}
		docs := declaredDocsFolders(repo)
		var files core.FS = fsys
		if wrap != nil {
			files = wrap(fsys)
		}
		v, err := corevault.OpenWithDocs(files, filepath.Base(filepath.Clean(repo.Path)), docs)
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
		mounts = append(mounts, mcpMount{id: repo.ID, root: fsys.Root(), role: role, docs: docs, vlt: v})
	}
	return space, mounts, nil
}

// declaredDocsFolders lists every documentation folder a registration
// declares, DocsFolder first, so that a folder deeper than discovery reaches —
// a monorepo's apps/api/docs — is indexed here as it is by the companion.
func declaredDocsFolders(repo config.Repo) []string {
	out := make([]string, 0, len(repo.DocsFolders)+1)
	seen := map[string]bool{}
	for _, folder := range append([]string{repo.DocsFolder}, repo.DocsFolders...) {
		if folder == "" || seen[folder] {
			continue
		}
		seen[folder] = true
		out = append(out, folder)
	}
	return out
}
