// Package completion implements shell completion script generation.
package completion

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdCompletion returns the completion command.
func NewCmdCompletion(_ *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion scripts",
		Long:      longUsage,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return completionRun(cmd, args[0])
		},
	}
	return cmd
}

func completionRun(cmd *cobra.Command, shell string) error {
	root := cmd.Root()
	switch shell {
	case "bash":
		return root.GenBashCompletionV2(os.Stdout, true)
	case "zsh":
		return root.GenZshCompletion(os.Stdout)
	case "fish":
		return root.GenFishCompletion(os.Stdout, true)
	case "powershell":
		return root.GenPowerShellCompletionWithDesc(os.Stdout)
	default:
		return fmt.Errorf("unsupported shell: %s (use bash, zsh, fish, or powershell)", shell)
	}
}

const longUsage = `Generate shell completion scripts for Laurus.

To load completions:

Bash:
  $ source <(laurus completion bash)
  # Or permanently:
  $ laurus completion bash > /etc/bash_completion.d/laurus

Zsh:
  $ laurus completion zsh > "${fpath[1]}/_laurus"
  # Then reload: compinit

Fish:
  $ laurus completion fish | source
  # Or permanently:
  $ laurus completion fish > ~/.config/fish/completions/laurus.fish

PowerShell:
  PS> laurus completion powershell | Out-String | Invoke-Expression
  # Or add to $PROFILE`
