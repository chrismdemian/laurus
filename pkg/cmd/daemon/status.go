package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

func newCmdStatus(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check if background polling is installed and running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return statusRun(f)
		},
	}
}

func statusRun(f *cmdutil.Factory) error {
	ios := f.IOStreams()

	switch runtime.GOOS {
	case "linux":
		return statusLinux(ios)
	case "darwin":
		return statusDarwin(ios)
	case "windows":
		return statusWindows(ios)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func statusLinux(ios *iostreams.IOStreams) error {
	timerPath := filepath.Join(systemdUserDir(), "laurus-watch.timer")
	if _, err := os.Stat(timerPath); os.IsNotExist(err) {
		_, _ = fmt.Fprintln(ios.Out, "Not installed.")
		return nil
	}

	out, err := exec.Command("systemctl", "--user", "is-active", "laurus-watch.timer").Output()
	state := strings.TrimSpace(string(out))
	if err != nil || state != "active" {
		_, _ = fmt.Fprintf(ios.Out, "Installed but not active (state: %s).\n", state)
		return nil
	}

	_, _ = fmt.Fprintln(ios.Out, "Installed and active.")
	return nil
}

func statusDarwin(ios *iostreams.IOStreams) error {
	plistPath := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", "com.laurus.watch.plist")
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		_, _ = fmt.Fprintln(ios.Out, "Not installed.")
		return nil
	}

	out, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		_, _ = fmt.Fprintln(ios.Out, "Installed (unable to check running state).")
		return nil
	}
	if strings.Contains(string(out), "com.laurus.watch") {
		_, _ = fmt.Fprintln(ios.Out, "Installed and active.")
	} else {
		_, _ = fmt.Fprintln(ios.Out, "Installed but not loaded.")
	}
	return nil
}

func statusWindows(ios *iostreams.IOStreams) error {
	out, err := exec.Command("schtasks", "/query", "/tn", "Laurus Watch", "/fo", "LIST").Output()
	if err != nil {
		_, _ = fmt.Fprintln(ios.Out, "Not installed.")
		return nil
	}

	output := string(out)
	if strings.Contains(output, "Ready") || strings.Contains(output, "Running") {
		_, _ = fmt.Fprintln(ios.Out, "Installed and active.")
	} else {
		_, _ = fmt.Fprintln(ios.Out, "Installed but not active.")
	}
	return nil
}
