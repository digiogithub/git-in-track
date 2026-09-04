package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/digiogithub/git-in-track/internal/config"
)

// configPathPayload is what `gintrack config path --json` prints.
type configPathPayload struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	StateDir  string `json:"stateDir"`
	CacheDir  string `json:"cacheDir"`
	Workspace string `json:"workspace"`
}

// newConfigCommand groups the configuration subcommands.
func newConfigCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and create the configuration file",
		Long: `Show where the configuration lives, what the effective values are after the
precedence chain has been applied, or write a default file.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usageError(cmd.Help())
		},
	}
	cmd.AddCommand(
		newConfigPathCommand(flags),
		newConfigShowCommand(flags),
		newConfigInitCommand(flags),
	)
	return cmd
}

// newConfigPathCommand prints the resolved configuration path.
func newConfigPathCommand(flags *globalFlags) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "path",
		Short: "Print the configuration file path",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := flags.resolve()
			p := flags.printer(cmd, asJSON)
			if err != nil {
				// The path is still useful when the file itself is broken.
				path, pathErr := config.DefaultPath(flags.reader())
				if pathErr != nil {
					return err
				}
				p.Printf("%s\n", path)
				return err
			}
			if p.JSONMode() {
				return render(p.JSON(configPathPayload{
					Path:      res.Path,
					Exists:    res.Exists,
					StateDir:  config.StateDir(res.Path),
					CacheDir:  res.Config.CacheDir(res.Path),
					Workspace: res.Workspace,
				}))
			}
			p.Printf("%s\n", res.Path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// newConfigShowCommand prints the effective configuration.
func newConfigShowCommand(flags *globalFlags) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration",
		Long: `Print the configuration after the precedence chain has been applied: flags over
GINTRACK_* variables over the file over the built-in defaults.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := flags.resolve()
			if err != nil {
				return err
			}
			p := flags.printer(cmd, asJSON)
			if p.JSONMode() {
				return render(p.JSON(res.Config))
			}
			data, err := yaml.Marshal(res.Config)
			if err != nil {
				return fmt.Errorf("encode the configuration: %w", err)
			}
			p.Printf("# %s\n%s", res.Path, data)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// newConfigInitCommand writes a default configuration file.
func newConfigInitCommand(flags *globalFlags) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a default configuration file",
		Long: `Create the configuration file with the built-in defaults, mode 0600, creating
the state directory if it is missing. An existing file is never overwritten
unless --force says so.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := flags.resolve()
			if err != nil {
				return err
			}
			if res.Exists && !force {
				return failf(exitFailure, "%s already exists: pass --force to replace it", res.Path)
			}
			if err := config.Save(res.Path, config.Default()); err != nil {
				return fmt.Errorf("write the configuration: %w", err)
			}
			flags.printer(cmd, false).Printf("wrote %s\n", res.Path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing file")
	return cmd
}
