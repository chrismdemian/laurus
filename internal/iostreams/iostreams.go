// Package iostreams provides terminal abstraction for color, pager, and stdout/stderr.
package iostreams

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

// IOStreams bundles the three standard I/O streams along with terminal metadata.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	IsTerminal   bool
	Width        int
	ColorEnabled bool
	IsJSON       bool
	PagerCmd     string

	pagerProcess *exec.Cmd
	pagerPipe    io.WriteCloser
	origOut      io.Writer
}

// System returns an IOStreams wired to the real terminal.
func System() *IOStreams {
	stdoutFd := int(os.Stdout.Fd())
	isTerm := term.IsTerminal(stdoutFd)

	width := 80
	if isTerm {
		if w, _, err := term.GetSize(stdoutFd); err == nil && w > 0 {
			width = w
		}
	}

	_, noColorSet := os.LookupEnv("NO_COLOR")
	colorEnabled := isTerm && !noColorSet && os.Getenv("TERM") != "dumb"

	pagerCmd := os.Getenv("PAGER")
	if pagerCmd == "" {
		pagerCmd = "less -R"
	}

	return &IOStreams{
		In:           os.Stdin,
		Out:          os.Stdout,
		ErrOut:       os.Stderr,
		IsTerminal:   isTerm,
		Width:        width,
		ColorEnabled: colorEnabled,
		PagerCmd:     pagerCmd,
	}
}

// Test returns an IOStreams backed by buffers for use in tests.
// Returns (ios, stdin, stdout, stderr).
func Test() (*IOStreams, *bytes.Buffer, *bytes.Buffer, *bytes.Buffer) {
	in := new(bytes.Buffer)
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)

	return &IOStreams{
		In:           in,
		Out:          out,
		ErrOut:       errOut,
		IsTerminal:   false,
		Width:        80,
		ColorEnabled: false,
	}, in, out, errOut
}

// TerminalWidth returns the current terminal width.
func (s *IOStreams) TerminalWidth() int {
	return s.Width
}

// StartPager pipes Out through the configured pager command.
// It is a no-op when not connected to a terminal, in JSON mode, or when PagerCmd is empty.
func (s *IOStreams) StartPager() error {
	if !s.IsTerminal || s.IsJSON || s.PagerCmd == "" {
		return nil
	}

	parts := strings.Fields(s.PagerCmd)
	//nolint:gosec // pager command is user-configured via $PAGER
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdout = s.Out
	cmd.Stderr = s.ErrOut

	pipe, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	s.origOut = s.Out
	s.Out = pipe
	s.pagerPipe = pipe
	s.pagerProcess = cmd
	return nil
}

// StopPager closes the pager pipe and waits for the pager process to exit.
func (s *IOStreams) StopPager() {
	if s.pagerPipe == nil {
		return
	}

	_ = s.pagerPipe.Close()
	_ = s.pagerProcess.Wait()

	s.Out = s.origOut
	s.pagerPipe = nil
	s.pagerProcess = nil
	s.origOut = nil
}
