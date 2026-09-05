package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/server"
	"github.com/digiogithub/git-in-track/web"
)

// serveFlags mirrors the flags of docs/07 section 4.1.
type serveFlags struct {
	port        int
	bind        string
	noOpen      bool
	token       string
	dev         bool
	watch       bool
	idleTimeout time.Duration
	repos       []string
	// mcpHTTP mounts the Model Context Protocol server at POST /mcp, and
	// mcpAllowWrite advertises its write tools (docs/08 section 2.2).
	mcpHTTP       bool
	mcpAllowWrite bool
	mcpAgent      string
}

// browserDelay is how long the banner waits before opening the browser, so that
// the listener is accepting connections by the time the page loads.
const browserDelay = 300 * time.Millisecond

// newServeCommand starts the local server: the embedded web app, the REST API,
// the event stream and the file watcher. The command owns no logic beyond
// turning flags and configuration into server options.
func newServeCommand(build buildInfo) *cobra.Command {
	flags := &serveFlags{}

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the web application and the local API",
		Long: `Serve starts the companion process: the embedded web application, the local
REST API, the WebSocket event stream and the file watcher over every registered
repository.

Every route but /api/v1/health requires the bearer token printed on start.
Pass --token none to disable authentication, which is refused unless the bind
address is a loopback interface. Use --repo <path> to serve a repository
without registering it in the configuration.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, build, flags)
		},
	}

	cmd.Flags().IntVar(&flags.port, "port", server.DefaultPort, "port to listen on")
	cmd.Flags().StringVar(&flags.bind, "bind", server.DefaultBind, "address to bind to")
	cmd.Flags().BoolVar(&flags.noOpen, "no-open", false, "do not open the browser")
	cmd.Flags().StringVar(&flags.token, "token", "", `bearer token; "new" generates one, "none" disables authentication`)
	cmd.Flags().BoolVar(&flags.dev, "dev", false, "development mode: allow the Vite dev server origin")
	cmd.Flags().BoolVar(&flags.watch, "watch", true, "watch the repositories for changes")
	cmd.Flags().DurationVar(&flags.idleTimeout, "idle-timeout", 0, "exit after this idle duration; 0 disables it")
	cmd.Flags().StringArrayVar(&flags.repos, "repo", nil,
		"serve this repository without registering it; repeatable")
	cmd.Flags().BoolVar(&flags.mcpHTTP, "mcp-http", false,
		"serve the Model Context Protocol at POST /mcp")
	cmd.Flags().BoolVar(&flags.mcpAllowWrite, "mcp-allow-write", false,
		"advertise the MCP write tools; without it /mcp is read-only")
	cmd.Flags().StringVar(&flags.mcpAgent, "mcp-agent", "",
		"agent name recorded as the author of comments written through /mcp")
	return cmd
}

// runServe builds the options, starts the server and blocks until the process
// is interrupted.
func runServe(cmd *cobra.Command, build buildInfo, flags *serveFlags) error {
	res, err := resolveServeConfig(cmd)
	if err != nil {
		return err
	}
	cfg := res.Config

	token, generated, err := serveToken(flags.token, cfg.Server.Token)
	if err != nil {
		return err
	}
	if generated && flags.token == "" {
		persistToken(cmd, res, token)
	}

	repos, err := mountList(cfg, res.Workspace, flags.repos)
	if err != nil {
		return err
	}

	ui, err := web.DistFS()
	if err != nil {
		return fmt.Errorf("open the embedded web bundle: %w", err)
	}

	opts := server.Options{
		Bind:        pick(cmd, "bind", flags.bind, cfg.Server.Bind),
		Port:        pickInt(cmd, "port", flags.port, cfg.Server.Port),
		Token:       token,
		Dev:         flags.dev,
		OpenBrowser: !flags.noOpen && cfg.Server.OpenBrowser,
		IdleTimeout: pickDuration(cmd, "idle-timeout", flags.idleTimeout, cfg.Server.IdleTimeout),
		Version:     build.Version,
		Commit:      build.Commit,
		UI:          ui,
		Logger:      slog.Default(),
		Repos:       repos,
		Workspace:   res.Workspace,
		Watch:       pickBool(cmd, "watch", flags.watch, cfg.Index.Watch),
		Debounce:    cfg.Index.Debounce,
		// Commit-on-save and the git backend (docs/06-git-sync.md section 3.3).
		// ConfigPath is what makes a settings change made in the web UI
		// survive a restart.
		Git:        cfg.Git,
		ConfigPath: res.Path,
		// The MCP endpoint is off unless asked for, on the command line or in
		// the `mcp:` section of the configuration.
		MCPHTTP:       pickBool(cmd, "mcp-http", flags.mcpHTTP, cfg.MCP.Enabled),
		MCPAllowWrite: pickBool(cmd, "mcp-allow-write", flags.mcpAllowWrite, cfg.MCP.AllowWrite),
		MCPAgent:      flags.mcpAgent,
	}
	srv, err := server.New(opts)
	if err != nil {
		return fmt.Errorf("start the server: %w", err)
	}

	printBanner(cmd, build, srv, token, opts)

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if opts.OpenBrowser {
		go openWhenReady(ctx, cmd, srv, token)
	}

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// resolveServeConfig loads the effective configuration. The command reads the
// persistent flags off the root command so that `gintrack --config x serve`
// behaves like every other command in the tree.
func resolveServeConfig(cmd *cobra.Command) (*config.Resolution, error) {
	flags := config.Flags{}
	if root := cmd.Root(); root != nil {
		pf := root.PersistentFlags()
		if value, err := pf.GetString("config"); err == nil {
			flags.ConfigPath = value
		}
		if value, err := pf.GetString("workspace"); err == nil {
			flags.Workspace = value
		}
	}
	res, err := config.Resolve(flags, config.Env())
	if err != nil {
		return nil, fmt.Errorf("load the configuration: %w", err)
	}
	return res, nil
}

// mountList turns the configuration and the --repo flags into the repositories
// the server mounts. An ad-hoc --repo wins over a registration with the same
// path, so that serving a checkout twice never indexes it twice.
func mountList(cfg *config.Config, workspace string, extra []string) ([]server.Repo, error) {
	var repos []server.Repo
	seen := make(map[string]bool)

	for _, path := range extra {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", path, err)
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		repos = append(repos, server.Repo{
			ID:   filepath.Base(abs),
			Path: abs,
			Role: string(config.RoleProject),
		})
	}

	for _, repo := range cfg.WorkspaceRepos(workspace) {
		if seen[repo.Path] {
			continue
		}
		seen[repo.Path] = true
		repos = append(repos, server.Repo{
			ID:          repo.ID,
			Path:        repo.Path,
			Role:        string(repo.Role),
			DocsFolder:  repo.DocsFolder,
			DocsFolders: repo.DeclaredDocsFolders(),
		})
	}
	return repos, nil
}

// resolveToken turns the --token flag into the token the server uses: "none"
// disables authentication, "new" and an empty flag generate a fresh one, and
// anything else is taken literally.
func resolveToken(flag string) (string, error) {
	switch flag {
	case "none":
		return "", nil
	case "", "new":
		token, err := server.GenerateToken()
		if err != nil {
			return "", fmt.Errorf("generate a token: %w", err)
		}
		return token, nil
	default:
		return flag, nil
	}
}

// serveToken picks the token of this run: the flag wins, then the one stored in
// the configuration, then a fresh one. It reports whether it had to generate
// it, which is what tells the caller to persist it.
func serveToken(flag, stored string) (token string, generated bool, err error) {
	if flag == "" && stored != "" {
		return stored, false, nil
	}
	token, err = resolveToken(flag)
	if err != nil {
		return "", false, err
	}
	return token, flag == "" || flag == "new", nil
}

// persistToken stores a freshly generated token in the configuration file, as
// docs/07 section 5.1 describes. Failing to write it is a warning: the token is
// printed on the banner and the server works without the file.
func persistToken(cmd *cobra.Command, res *config.Resolution, token string) {
	res.Config.Server.Token = token
	if err := config.Save(res.Path, res.Config); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: the token could not be stored in %s: %v\n", res.Path, err)
	}
}

// printBanner writes the startup summary of docs/07 section 4.1.
func printBanner(cmd *cobra.Command, build buildInfo, srv *server.Server, token string, opts server.Options) {
	out := cmd.OutOrStdout()
	// Banner writes go to a terminal; a failed write is not worth an exit code.
	banner := func(format string, args ...any) { _, _ = fmt.Fprintf(out, format, args...) }

	ui := "embedded"
	if !web.Built() {
		ui = "not built"
	}
	banner("git-in-track %s (commit %s, ui: %s)\n", build.Version, build.Commit, ui)

	repos := srv.Repos()
	items, pages := 0, 0
	var projects []string
	for _, repo := range repos {
		items += repo.Items
		pages += repo.Pages
		projects = append(projects, repo.Projects...)
	}
	banner("workspace: %s — %s, %s\n", opts.Workspace,
		plural(len(repos), "repository", "repositories"), plural(len(projects), "project", "projects"))
	for _, repo := range repos {
		if repo.Err != nil {
			banner("  %-12s %s — not indexed: %v\n", repo.ID, repo.Path, repo.Err)
			continue
		}
		banner("  %-12s %s — %v, %d items, %d pages\n", repo.ID, repo.Path, repo.Projects, repo.Items, repo.Pages)
	}
	banner("indexed:   %d items, %d pages\n", items, pages)
	if opts.MCPHTTP {
		banner("mcp:        %s/mcp (%s)\n", srv.URL(), writeMode(opts.MCPAllowWrite))
	}
	banner("listening on %s\n", srv.URL())
	if token != "" {
		banner("token:      %s\n", token)
		banner("open:       %s/?token=%s\n", srv.URL(), token)
	} else {
		banner("open:       %s   (authentication disabled)\n", srv.URL())
	}
	if !web.Built() {
		banner("note:       the web UI is not built into this binary; run `make web` and rebuild\n")
	}
	if opts.Watch {
		banner("watching for changes — press Ctrl+C to stop\n")
		return
	}
	banner("press Ctrl+C to stop\n")
}

// openWhenReady opens the browser once the listener has had a moment to come
// up. A desktop that cannot be asked is reported and then forgotten: the URL is
// on the banner either way.
func openWhenReady(ctx context.Context, cmd *cobra.Command, srv *server.Server, token string) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(browserDelay):
	}
	target := srv.URL() + "/"
	if token != "" {
		target += "?token=" + token
	}
	if err := server.OpenBrowser(ctx, target); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not open a browser: %v\n", err)
	}
}

// pick returns the flag value when it was given, the configured one otherwise.
func pick(cmd *cobra.Command, name, flag, configured string) string {
	if cmd.Flags().Changed(name) || configured == "" {
		return flag
	}
	return configured
}

// pickInt is pick for an integer flag.
func pickInt(cmd *cobra.Command, name string, flag, configured int) int {
	if cmd.Flags().Changed(name) || configured == 0 {
		return flag
	}
	return configured
}

// pickBool is pick for a boolean flag, where "not given" cannot be told from
// "given as false" by the value alone.
func pickBool(cmd *cobra.Command, name string, flag, configured bool) bool {
	if cmd.Flags().Changed(name) {
		return flag
	}
	return configured
}

// pickDuration is pick for a duration flag.
func pickDuration(cmd *cobra.Command, name string, flag, configured time.Duration) time.Duration {
	if cmd.Flags().Changed(name) || configured == 0 {
		return flag
	}
	return configured
}
