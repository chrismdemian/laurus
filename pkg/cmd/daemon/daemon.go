// Package daemon implements the daemon command for managing background notification polling.
package daemon

import (
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdDaemon returns the daemon command with install/uninstall/status subcommands.
func NewCmdDaemon(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon <install|uninstall|status>",
		Short: "Manage background notification polling",
		Long: `Install, uninstall, or check the status of background notification polling.

The daemon runs 'laurus watch --once' on a schedule using your OS's native
task scheduler (systemd on Linux, launchd on macOS, Task Scheduler on Windows).`,
	}

	cmd.AddCommand(newCmdInstall(f))
	cmd.AddCommand(newCmdUninstall(f))
	cmd.AddCommand(newCmdStatus(f))

	return cmd
}
