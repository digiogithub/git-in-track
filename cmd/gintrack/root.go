package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/cmd/gintrack/output"
	"github.com/digiogithub/git-in-track/internal/config"
)

// buildInfo carries the values the linker stamps into the binary.
type buildInfo struct {
	Version string
	Commit  string
	Date    string
	BuiltBy string
}

// globalFlags are the flags every command shares, plus the configuration they
// resolve to. One command tree resolves the configuration once, so that every
// command in an invocation sees the same effective values.
type globalFlags struct {
	logLevel   string
	noColor    bool
	quiet      bool
	verbose    bool
	configPath string
	workspace  string

	// env is the environment reader; tests replace it. A nil value means the
	// process environment.
	env config.Reader

	resolution *config.Resolution
	resolveErr error
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
  gintrack add <path>   register a repository
  gintrack ls           list what is registered
  gintrack serve        open the web UI on http://127.0.0.1:7317`),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       build.Version,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return configureLogging(flags)
		},
	}

	cmd.SetVersionTemplate("{{.Version}}\n")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError(err)
	})
	pf := cmd.PersistentFlags()
	pf.StringVar(&flags.configPath, "config", "", "configuration file (default: the per-platform path)")
	pf.StringVarP(&flags.workspace, "workspace", "w", "", "workspace to act on")
	pf.StringVar(&flags.logLevel, "log-level", "info", "log level: debug, info, warn or error")
	pf.BoolVar(&flags.noColor, "no-color", false, "disable colored output")
	pf.BoolVarP(&flags.quiet, "quiet", "q", false, "print only what was asked for")
	pf.BoolVarP(&flags.verbose, "verbose", "v", false, "log at the debug level")

	cmd.AddCommand(
		newServeCommand(build),
		newVersionCommand(build),
		newCompletionCommand(),
		newAddCommand(flags),
		newLsCommand(flags),
		newRmCommand(flags),
		newIndexCommand(flags),
		newSnapshotCommand(flags),
		newItemCommand(flags),
		newDoctorCommand(flags),
		newConfigCommand(flags),
	)
	return cmd
}

// configureLogging installs the process-wide structured logger.
func configureLogging(flags *globalFlags) error {
	name := flags.logLevel
	switch {
	case flags.verbose:
		name = "debug"
	case flags.quiet && name == "info":
		name = "error"
	}
	var level slog.Level
	switch strings.ToLower(name) {
	case "debug":
		level = slog.LevelDebug
	case "info", "":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return usageError(fmt.Errorf("unknown log level %q: use debug, info, warn or error", flags.logLevel))
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
	return nil
}

// reader returns the environment the configuration is resolved against.
func (g *globalFlags) reader() config.Reader {
	if g.env != nil {
		return g.env
	}
	return config.Env()
}

// resolve returns the effective configuration, resolving it once per command
// tree: flags beat GINTRACK_* variables, which beat the file, which beats the
// built-in defaults.
func (g *globalFlags) resolve() (*config.Resolution, error) {
	if g.resolution == nil && g.resolveErr == nil {
		g.resolution, g.resolveErr = config.Resolve(config.Flags{
			ConfigPath: g.configPath,
			Workspace:  g.workspace,
		}, g.reader())
	}
	if g.resolveErr != nil {
		return nil, g.resolveErr
	}
	return g.resolution, nil
}

// save writes the resolved configuration back to the file it came from.
func (g *globalFlags) save(res *config.Resolution) error {
	if err := config.Save(res.Path, res.Config); err != nil {
		return fmt.Errorf("save the configuration: %w", err)
	}
	return nil
}

// config returns the effective configuration; commands call it after resolve.
func (g *globalFlags) config() *config.Config {
	if g.resolution != nil {
		return g.resolution.Config
	}
	return config.Default()
}

// printer builds the renderer a command prints through.
func (g *globalFlags) printer(cmd *cobra.Command, asJSON bool) *output.Printer {
	p := output.New(cmd.OutOrStdout(), cmd.ErrOrStderr(), asJSON)
	p.SetQuiet(g.quiet)
	return p
}
