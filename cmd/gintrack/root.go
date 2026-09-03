package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// buildInfo carries the values the linker stamps into the binary.
type buildInfo struct {
	Version string
	Commit  string
	Date    string
	BuiltBy string
}

// globalFlags are the flags every command shares.
type globalFlags struct {
	logLevel string
	noColor  bool
}

// Execute runs the command tree and returns the error the caller reports.
func Execute(build buildInfo) error {
	root := newRootCommand(build)
	//nolint:wrapcheck // cobra's error is already contextual and main prints it verbatim
	return root.Execute()
}

// newRootCommand builds the command tree.
func newRootCommand(build buildInfo) *cobra.Command {
	flags := &globalFlags{}

	cmd := &cobra.Command{
		Use:   "gintrack",
		Short: "Git-native, Markdown-first project management",
		Long: strings.TrimSpace(`
gintrack serves the git-in-track web application and its local API from your own
repositories. Epics, stories, tasks, milestones and comments are Markdown files
with YAML front matter; git is the only synchronization mechanism.

Start with:
  gintrack serve      open the web UI on http://127.0.0.1:7317
  gintrack version    print the build information`),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       build.Version,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return configureLogging(flags)
		},
	}

	cmd.SetVersionTemplate("{{.Version}}\n")
	cmd.PersistentFlags().StringVar(&flags.logLevel, "log-level", "info", "log level: debug, info, warn or error")
	cmd.PersistentFlags().BoolVar(&flags.noColor, "no-color", false, "disable colored output")

	cmd.AddCommand(
		newServeCommand(build),
		newVersionCommand(build),
		newCompletionCommand(),
	)
	return cmd
}

// configureLogging installs the process-wide structured logger.
func configureLogging(flags *globalFlags) error {
	var level slog.Level
	switch strings.ToLower(flags.logLevel) {
	case "debug":
		level = slog.LevelDebug
	case "info", "":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return fmt.Errorf("unknown log level %q: use debug, info, warn or error", flags.logLevel)
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
	return nil
}
