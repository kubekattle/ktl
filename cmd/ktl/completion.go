// File: cmd/ktl/completion.go
// Brief: CLI command wiring and implementation for 'completion'.

// completion.go registers the 'ktl completion' command that emits shell-specific autocompletion scripts.
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate autocompletion scripts for your shell. Source the output or save it to
one of the completion directories supported by your shell to enable ktl resource-aware tab completion.`,
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}
	cmd.Example = `  # Enable bash completion for the current session
  source <(ktl completion bash)

  # Persist zsh completions
  ktl completion zsh > ~/.oh-my-zsh/completions/_ktl`
	return cmd
}
