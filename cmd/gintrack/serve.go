package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
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
	// tunnel opens a public tunnel to this server as soon as it is listening
	// (`server.tunnel` in the configuration).
	tunnel bool
	// The background job engine of GIT-US-0084: the worker pool, the batch
	// size, the shared outbound rate limit and the retry budget.
	syncWorkers     int
	syncBatch       int
	syncRate        float64
	syncMaxAttempts int
}

// tunnelPollInterval is how often the banner asks the server whether the tunnel
// has a public URL yet. Provisioning it takes a network round trip, so there is
// nothing to print at banner time.
const tunnelPollInterval = 200 * time.Millisecond

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
without registering it in the configuration.

--tunnel publishes this server on the internet through an anonymous Cloudflare
quick tunnel. It is refused without a token, because the token is then the only
thing standing between the URL and write access to your repositories. The
public hostname and a ready-to-open ?token= link are both printed on this
terminal: share the first, and the second only with whoever may write.`,
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
	cmd.Flags().BoolVar(&flags.tunnel, "tunnel", false,
		"publish this server on the internet through a Cloudflare quick tunnel; requires a token")
	cmd.Flags().IntVar(&flags.syncWorkers, "sync-workers", server.DefaultSyncWorkers,
		"background job workers")
	cmd.Flags().IntVar(&flags.syncBatch, "sync-batch", server.DefaultSyncBatchSize,
		"how many background jobs one handler call receives")
	cmd.Flags().Float64Var(&flags.syncRate, "sync-rate", server.DefaultSyncRate,
		"shared outbound rate limit for background jobs, in requests per second")
	cmd.Flags().IntVar(&flags.syncMaxAttempts, "sync-max-attempts", server.DefaultSyncMaxAttempts,
		"how many attempts a background job takes before it is dead-lettered")
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

	tunnelOn := pickBool(cmd, "tunnel", flags.tunnel, cfg.Server.Tunnel.Enabled)
	if tunnelOn && token == "" {
		// Refused here, before anything listens, so that the reason names the
		// flag the user actually typed. server.New refuses the same combination
		// on its own, which is what covers the configuration-driven path.
		return errors.New("opening a tunnel requires a bearer token: --tunnel (or server.tunnel.enabled) " +
			"publishes this server, with write access to your repositories, to anyone holding the URL; " +
			"drop --token none")
	}

	repos, err := mountList(cfg, res.Workspace, flags.repos)
	if err != nil {
		return err
	}

	ui, err := web.DistFS()
	if err != nil {
		return fmt.Errorf("open the embedded web bundle: %w", err)
	}

	// Resolved and validated before anything binds a port: an operator who
	// typed `--sync-workers 0` must learn it from the exit code, not from a
	// server that came up and then refused every job.
	engine, err := syncEngineSettings(cmd, flags, cfg, config.Env())
	if err != nil {
		return err
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
		// The YouTrack credentials of the projects this workspace serves. They
		// are resolved once here, flag over GINTRACK_YOUTRACK_TOKEN over the
		// 0600 file, and the server hands them out to nobody (ADR-032).
		YouTrack: cfg.YouTrackTokens(),
		// The MCP endpoint is off unless asked for, on the command line or in
		// the `mcp:` section of the configuration.
		MCPHTTP:       pickBool(cmd, "mcp-http", flags.mcpHTTP, cfg.MCP.Enabled),
		MCPAllowWrite: pickBool(cmd, "mcp-allow-write", flags.mcpAllowWrite, cfg.MCP.AllowWrite),
		MCPAgent:      flags.mcpAgent,
		// The public tunnel. Only the startup path may turn it on implicitly;
		// a toggle made in the web UI is never written back to the file.
		Tunnel: config.Tunnel{Enabled: tunnelOn, Provider: cfg.Server.Tunnel.Provider},
		// The background job engine (GIT-US-0084). It starts with the server,
		// idle when nothing has registered a job handler, and drains on the way
		// out.
		SyncEngine: engine,
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
	if opts.Tunnel.Enabled {
		go announceTunnel(ctx, cmd, srv, token)
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
	if opts.Tunnel.Enabled {
		provider := opts.Tunnel.Provider
		if provider == "" {
			provider = config.DefaultTunnelProvider
		}
		// The URL does not exist yet; announceTunnel prints it when it does.
		banner("tunnel:     opening a public %s tunnel…\n", provider)
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

// tunnelReporter is the slice of *server.Server announceTunnel polls. It is an
// interface so the announcement can be tested without a listener.
type tunnelReporter interface {
	TunnelStatus() server.TunnelInfo
}

// announceTunnel prints the public URL once the tunnel has one. The tunnel is
// provisioned after the listener comes up, so the banner cannot carry it; this
// polls the server until the URL exists or the run ends.
//
// It prints the bare public hostname and, below it, the same `open:` link the
// local banner prints: `<url>/?token=…`. The web app only takes the token from
// `?token=` or from Settings, so without that link a browser reaching the
// tunnel gets an empty workspace behind a "needs an access token" banner.
//
// The link carries the one secret protecting the repositories. It goes to this
// terminal only — never to a log line, an event or an API response — and the
// bare URL is printed first so that "share the tunnel" and "share write access
// to my repositories" stay two visibly different lines to copy.
func announceTunnel(ctx context.Context, cmd *cobra.Command, srv tunnelReporter, token string) {
	ticker := time.NewTicker(tunnelPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		switch status := srv.TunnelStatus(); {
		case status.URL != "":
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "tunnel:     %s   (public; the token is still required)\n", status.URL)
			if token != "" {
				_, _ = fmt.Fprintf(out, "open:       %s/?token=%s   (public link; anyone holding it has write access)\n",
					status.URL, token)
			}
			return
		case status.Err != "":
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: the public tunnel could not be opened: %s\n", status.Err)
			return
		}
	}
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

// The environment variables of the background job engine. They sit between the
// command line and the defaults in the precedence chain of docs/07 section 3.3.
const (
	envSyncWorkers     = "GINTRACK_SYNC_WORKERS"
	envSyncBatch       = "GINTRACK_SYNC_BATCH"
	envSyncRate        = "GINTRACK_SYNC_RATE"
	envSyncMaxAttempts = "GINTRACK_SYNC_MAX_ATTEMPTS"
)

// syncEngineSettings resolves the engine configuration: the flag when it was
// given, then the environment variable, then the shipped default. The result is
// validated here so that an impossible value fails the command instead of
// reaching a running server.
//
// The file layer of the chain is missing on purpose: the configuration file has
// no `sync.engine` section yet, and adding one belongs to internal/config
// (GIT-T-0175). The cache directory it does declare is used, so a companion
// with a cache keeps its queue across restarts.
func syncEngineSettings(cmd *cobra.Command, flags *serveFlags, cfg *config.Config, env config.Reader) (server.SyncEngine, error) {
	out := server.SyncEngine{CacheDir: cfg.Index.CacheDir}

	workers, err := pickEnvInt(cmd, "sync-workers", flags.syncWorkers, envSyncWorkers, env)
	if err != nil {
		return server.SyncEngine{}, err
	}
	batch, err := pickEnvInt(cmd, "sync-batch", flags.syncBatch, envSyncBatch, env)
	if err != nil {
		return server.SyncEngine{}, err
	}
	attempts, err := pickEnvInt(cmd, "sync-max-attempts", flags.syncMaxAttempts, envSyncMaxAttempts, env)
	if err != nil {
		return server.SyncEngine{}, err
	}
	rate, err := pickEnvFloat(cmd, "sync-rate", flags.syncRate, envSyncRate, env)
	if err != nil {
		return server.SyncEngine{}, err
	}
	out.Workers, out.BatchSize, out.MaxAttempts, out.Rate = workers, batch, attempts, rate

	if err := out.Validate(); err != nil {
		return server.SyncEngine{}, fmt.Errorf("sync engine: %w", err)
	}
	return out, nil
}

// pickEnvInt is the flag > environment > default chain for a whole number.
func pickEnvInt(cmd *cobra.Command, name string, flag int, key string, env config.Reader) (int, error) {
	if cmd.Flags().Changed(name) {
		return flag, nil
	}
	raw := strings.TrimSpace(env(key))
	if raw == "" {
		return flag, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a whole number", key, raw)
	}
	return value, nil
}

// pickEnvFloat is the same chain for a rate.
func pickEnvFloat(cmd *cobra.Command, name string, flag float64, key string, env config.Reader) (float64, error) {
	if cmd.Flags().Changed(name) {
		return flag, nil
	}
	raw := strings.TrimSpace(env(key))
	if raw == "" {
		return flag, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", key, raw)
	}
	return value, nil
}
