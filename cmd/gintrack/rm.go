package main

import (
	"github.com/spf13/cobra"
)

// rmPayload is what `gintrack rm --json` prints.
type rmPayload struct {
	Removed repoInfo `json:"removed"`
	Config  string   `json:"config"`
}

// newRmCommand unregisters a repository.
func newRmCommand(flags *globalFlags) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "rm <id>",
		Short: "Unregister a repository",
		Long: `Remove a repository from the configuration. The working tree is never touched:
only the registration disappears, and adding it again restores it.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := flags.resolve()
			if err != nil {
				return err
			}
			removed, ok := res.Config.RemoveRepo(args[0])
			if !ok {
				return notFoundf("no repository %q is registered", args[0])
			}
			if err := flags.save(res); err != nil {
				return err
			}
			p := flags.printer(cmd, asJSON)
			if p.JSONMode() {
				return render(p.JSON(rmPayload{Removed: newRepoInfo(removed), Config: res.Path}))
			}
			p.Printf("removed %s repository %s  %s\n", removed.Role, removed.ID, removed.Path)
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}
