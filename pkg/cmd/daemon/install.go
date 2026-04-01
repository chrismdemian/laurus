package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

func newCmdInstall(f *cmdutil.Factory) *cobra.Command {
	var interval int

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install background notification polling",
		Long:  "Install a scheduled task that runs 'laurus watch --once' at a regular interval.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return installRun(f, interval)
		},
	}

	cmd.Flags().IntVar(&interval, "interval", 15, "Polling interval in minutes")

	return cmd
}

func installRun(f *cmdutil.Factory, intervalMinutes int) error {
	ios := f.IOStreams()

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	switch runtime.GOOS {
	case "linux":
		return installLinux(ios, exe, intervalMinutes)
	case "darwin":
		return installDarwin(ios, exe, intervalMinutes)
	case "windows":
		return installWindows(ios, exe, intervalMinutes)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// --- Linux (systemd user timer) ---

const systemdServiceTmpl = `[Unit]
Description=Laurus Canvas notification poll

[Service]
Type=oneshot
ExecStart={{.Exe}} watch --once
`

const systemdTimerTmpl = `[Unit]
Description=Laurus Canvas notification timer

[Timer]
OnBootSec=2min
OnUnitActiveSec={{.Interval}}min
Persistent=true

[Install]
WantedBy=timers.target
`

func systemdUserDir() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "systemd", "user")
}

func installLinux(ios *iostreams.IOStreams, exe string, interval int) error {
	dir := systemdUserDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating systemd user dir: %w", err)
	}

	data := struct {
		Exe      string
		Interval int
	}{exe, interval}

	servicePath := filepath.Join(dir, "laurus-watch.service")
	if err := writeTemplate(servicePath, systemdServiceTmpl, data); err != nil {
		return err
	}

	timerPath := filepath.Join(dir, "laurus-watch.timer")
	if err := writeTemplate(timerPath, systemdTimerTmpl, data); err != nil {
		return err
	}

	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if err := run("systemctl", "--user", "enable", "--now", "laurus-watch.timer"); err != nil {
		return fmt.Errorf("enabling timer: %w", err)
	}

	_, _ = fmt.Fprintf(ios.Out, "Installed systemd user timer (every %d minutes).\n", interval)
	_, _ = fmt.Fprintf(ios.Out, "  Service: %s\n", servicePath)
	_, _ = fmt.Fprintf(ios.Out, "  Timer:   %s\n", timerPath)
	return nil
}

// --- macOS (launchd) ---

const launchdPlistTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.laurus.watch</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.Exe}}</string>
		<string>watch</string>
		<string>--once</string>
	</array>
	<key>StartInterval</key>
	<integer>{{.IntervalSec}}</integer>
	<key>RunAtLoad</key>
	<true/>
	<key>StandardOutPath</key>
	<string>/tmp/laurus-watch.log</string>
	<key>StandardErrorPath</key>
	<string>/tmp/laurus-watch.log</string>
</dict>
</plist>
`

func installDarwin(ios *iostreams.IOStreams, exe string, interval int) error {
	dir := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating LaunchAgents dir: %w", err)
	}

	plistPath := filepath.Join(dir, "com.laurus.watch.plist")

	data := struct {
		Exe         string
		IntervalSec int
	}{exe, interval * 60}

	if err := writeTemplate(plistPath, launchdPlistTmpl, data); err != nil {
		return err
	}

	// Unload first in case it's already loaded (ignore error).
	_ = run("launchctl", "bootout", fmt.Sprintf("gui/%d/com.laurus.watch", os.Getuid()))
	if err := run("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plistPath); err != nil {
		return fmt.Errorf("loading plist: %w", err)
	}

	_, _ = fmt.Fprintf(ios.Out, "Installed launchd agent (every %d minutes).\n", interval)
	_, _ = fmt.Fprintf(ios.Out, "  Plist: %s\n", plistPath)
	return nil
}

// --- Windows (Task Scheduler) ---

func installWindows(ios *iostreams.IOStreams, exe string, interval int) error {
	// schtasks /create /tn "Laurus Watch" /tr "<exe> watch --once" /sc MINUTE /mo <interval> /f
	args := []string{
		"/create",
		"/tn", "Laurus Watch",
		"/tr", fmt.Sprintf(`"%s" watch --once`, exe),
		"/sc", "MINUTE",
		"/mo", fmt.Sprintf("%d", interval),
		"/f",
	}
	if err := run("schtasks", args...); err != nil {
		return fmt.Errorf("creating scheduled task: %w", err)
	}

	_, _ = fmt.Fprintf(ios.Out, "Installed Windows scheduled task (every %d minutes).\n", interval)
	_, _ = fmt.Fprintln(ios.Out, "  Task: Laurus Watch")
	return nil
}

// --- Helpers ---

func writeTemplate(path, tmpl string, data any) error {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
