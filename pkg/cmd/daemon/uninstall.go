package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

func newCmdUninstall(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove background notification polling",
		Long:  "Stop and remove the scheduled task that runs 'laurus watch --once'.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstallRun(f)
		},
	}
}

func uninstallRun(f *cmdutil.Factory) error {
	ios := f.IOStreams()

	switch runtime.GOOS {
	case "linux":
		return uninstallLinux(ios)
	case "darwin":
		return uninstallDarwin(ios)
	case "windows":
		return uninstallWindows(ios)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func uninstallLinux(ios *iostreams.IOStreams) error {
	_ = run("systemctl", "--user", "disable", "--now", "laurus-watch.timer")

	dir := systemdUserDir()
	removed := false
	for _, name := range []string{"laurus-watch.service", "laurus-watch.timer"} {
		p := filepath.Join(dir, name)
		if err := os.Remove(p); err == nil {
			removed = true
		}
	}

	_ = run("systemctl", "--user", "daemon-reload")

	if removed {
		_, _ = fmt.Fprintln(ios.Out, "Removed systemd user timer and service.")
	} else {
		_, _ = fmt.Fprintln(ios.Out, "No systemd timer found — nothing to remove.")
	}
	return nil
}

func uninstallDarwin(ios *iostreams.IOStreams) error {
	_ = run("launchctl", "bootout", fmt.Sprintf("gui/%d/com.laurus.watch", os.Getuid()))

	plistPath := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", "com.laurus.watch.plist")
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing plist: %w", err)
	}

	_, _ = fmt.Fprintln(ios.Out, "Removed launchd agent.")
	return nil
}

func uninstallWindows(ios *iostreams.IOStreams) error {
	if err := run("schtasks", "/delete", "/tn", "Laurus Watch", "/f"); err != nil {
		_, _ = fmt.Fprintln(ios.Out, "No scheduled task found — nothing to remove.")
		return nil
	}

	_, _ = fmt.Fprintln(ios.Out, "Removed Windows scheduled task.")
	return nil
}
