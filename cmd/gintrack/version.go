package main

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/web"
)

// newVersionCommand prints the build information, in text or as JSON.
func newVersionCommand(build buildInfo) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version, commit and build date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := versionInfo(build)
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(info); err != nil {
					return fmt.Errorf("encode version: %w", err)
				}
				return nil
			}
			// A failed write to a terminal is not worth an exit code.
			line := func(format string, args ...any) { _, _ = fmt.Fprintf(out, format, args...) }
			line("gintrack %s\n", info.Version)
			line("commit:   %s\n", info.Commit)
			line("built:    %s\n", info.Date)
			line("by:       %s\n", info.BuiltBy)
			line("go:       %s %s/%s\n", info.Go, info.OS, info.Arch)
			line("ui:       %s\n", info.UI)
			line("core:     schema v%d\n", info.Schema)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// versionInfoPayload is what `gintrack version --json` prints.
type versionInfoPayload struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	BuiltBy string `json:"builtBy"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	UI      string `json:"ui"`
	Schema  int    `json:"schema"`
}

// versionInfo collects everything the version command reports.
func versionInfo(build buildInfo) versionInfoPayload {
	ui := "not built"
	if web.Built() {
		ui = "embedded"
	}
	return versionInfoPayload{
		Version: build.Version,
		Commit:  build.Commit,
		Date:    build.Date,
		BuiltBy: build.BuiltBy,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		UI:      ui,
		Schema:  core.SupportedSchema,
	}
}
