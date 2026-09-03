package main

import (
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

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
	idleTimeout time.Duration
}

// newServeCommand starts the local server: the embedded web app plus the REST
// API. The command owns no logic beyond translating flags into server options.
func newServeCommand(build buildInfo) *cobra.Command {
	flags := &serveFlags{}

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the web application and the local API",
		Long: `Serve starts the companion process: the embedded web application, the local
REST API and, in later phases, the file watcher and the git integration.

Every route but /api/v1/health requires the bearer token printed on start.
Pass --token none to disable authentication, which is refused unless the bind
address is a loopback interface.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, build, flags)
		},
	}

	cmd.Flags().IntVar(&flags.port, "port", server.DefaultPort, "port to listen on")
	cmd.Flags().StringVar(&flags.bind, "bind", server.DefaultBind, "address to bind to")
	cmd.Flags().BoolVar(&flags.noOpen, "no-open", false, "do not open the browser")
	cmd.Flags().StringVar(&flags.token, "token", "", `bearer token; "new" generates one, "none" disables authentication`)
	cmd.Flags().BoolVar(&flags.dev, "dev", false, "development mode: allow the Vite dev server origin")
	cmd.Flags().DurationVar(&flags.idleTimeout, "idle-timeout", 0, "exit after this idle duration; 0 disables it")
	return cmd
}

// runServe builds the options, starts the server and blocks until the process
// is interrupted.
func runServe(cmd *cobra.Command, build buildInfo, flags *serveFlags) error {
	token, err := resolveToken(flags.token)
	if err != nil {
		return err
	}

	ui, err := web.DistFS()
	if err != nil {
		return fmt.Errorf("open the embedded web bundle: %w", err)
	}

	srv, err := server.New(server.Options{
		Bind:        flags.bind,
		Port:        flags.port,
		Token:       token,
		Dev:         flags.dev,
		OpenBrowser: !flags.noOpen,
		IdleTimeout: flags.idleTimeout,
		Version:     build.Version,
		Commit:      build.Commit,
		UI:          ui,
		Logger:      slog.Default(),
	})
	if err != nil {
		return fmt.Errorf("start the server: %w", err)
	}

	// Banner writes go to a terminal; a failed write is not worth an exit code.
	out := cmd.OutOrStdout()
	banner := func(format string, args ...any) { _, _ = fmt.Fprintf(out, format, args...) }
	banner("git-in-track %s (commit %s)\n", build.Version, build.Commit)
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
	banner("press Ctrl+C to stop\n")

	// Opening the browser needs the operating system and arrives with the rest
	// of the companion in Phase 2; until then the URL above is the way in.

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// resolveToken turns the --token flag into the token the server uses.
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
