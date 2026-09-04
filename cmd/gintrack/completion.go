package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newCompletionCommand generates a shell completion script.
//
// Completions for item ids, project keys, statuses, labels and board slugs are
// registered by the commands that own them, as those commands are implemented.
func newCompletionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate a shell completion script",
		Long: `Generate a shell completion script for gintrack.

  bash:        gintrack completion bash > /etc/bash_completion.d/gintrack
  zsh:         gintrack completion zsh > "${fpath[1]}/_gintrack"
  fish:        gintrack completion fish > ~/.config/fish/completions/gintrack.fish
  powershell:  gintrack completion powershell | Out-String | Invoke-Expression`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			switch args[0] {
			case "bash":
				if err := cmd.Root().GenBashCompletionV2(out, true); err != nil {
					return fmt.Errorf("generate bash completion: %w", err)
				}
			case "zsh":
				if err := cmd.Root().GenZshCompletion(out); err != nil {
					return fmt.Errorf("generate zsh completion: %w", err)
				}
			case "fish":
				if err := cmd.Root().GenFishCompletion(out, true); err != nil {
					return fmt.Errorf("generate fish completion: %w", err)
				}
			case "powershell":
				if err := cmd.Root().GenPowerShellCompletionWithDesc(out); err != nil {
					return fmt.Errorf("generate powershell completion: %w", err)
				}
			}
			return nil
		},
	}
}
