// Command gintrack-hub is the optional self-hosted multi-user server of
// ADR-043. It clones one organization's repositories from their forge and
// serves them through the same API, MCP endpoint and web app as the companion.
//
// It is a separate binary on purpose: gintrack itself stays a local tool with
// no server (docs/01-vision-and-scope.md §6), and the hub is opt-in.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/digiogithub/git-in-track/internal/hub"
	"github.com/digiogithub/git-in-track/web"
)

// Set at build time with -ldflags "-X main.version=… -X main.commit=…".
var (
	version = "dev"
	commit  = "none"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "gintrack-hub:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("gintrack-hub", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "hub.yaml", "hub configuration file")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err //nolint:wrapcheck // flag already names the problem
	}
	if *showVersion {
		_, _ = fmt.Fprintf(stderr, "gintrack-hub %s (%s)\n", version, commit)
		return nil
	}

	cfg, err := hub.Load(*configPath)
	if err != nil {
		return err //nolint:wrapcheck // Load names the file and the field
	}
	ui, err := web.DistFS()
	if err != nil {
		return err //nolint:wrapcheck // already says what failed
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	h, err := hub.New(ctx, cfg, hub.Deps{
		UI: ui, Version: version, Commit: commit,
		Logger: slog.New(slog.NewTextHandler(stderr, nil)),
	})
	if err != nil {
		return err //nolint:wrapcheck // New names the repository or the step
	}
	return h.Run(ctx) //nolint:wrapcheck // Run wraps its own failure
}
