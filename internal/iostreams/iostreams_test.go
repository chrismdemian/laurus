package iostreams

import (
	"testing"
)

func TestTest_Constructor(t *testing.T) {
	ios, in, out, errOut := Test()

	if ios.In == nil || ios.Out == nil || ios.ErrOut == nil {
		t.Fatal("expected non-nil streams")
	}
	if in == nil || out == nil || errOut == nil {
		t.Fatal("expected non-nil buffers")
	}
	if ios.IsTerminal {
		t.Error("expected IsTerminal=false for test IOStreams")
	}
	if ios.Width != 80 {
		t.Errorf("expected Width=80, got %d", ios.Width)
	}
	if ios.ColorEnabled {
		t.Error("expected ColorEnabled=false for test IOStreams")
	}
	if ios.IsJSON {
		t.Error("expected IsJSON=false for test IOStreams")
	}
}

func TestTerminalWidth(t *testing.T) {
	ios, _, _, _ := Test()
	ios.Width = 120

	if got := ios.TerminalWidth(); got != 120 {
		t.Errorf("TerminalWidth() = %d, want 120", got)
	}
}

func TestColorDetection_NOCOLOREnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "xterm-256color")

	ios := System()
	if ios.ColorEnabled {
		t.Error("expected ColorEnabled=false when NO_COLOR is set")
	}
}

func TestColorDetection_DumbTerm(t *testing.T) {
	// Ensure NO_COLOR is not set
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")

	ios := System()
	if ios.ColorEnabled {
		t.Error("expected ColorEnabled=false when TERM=dumb")
	}
}

func TestPager_NotTerminal(t *testing.T) {
	ios, _, out, _ := Test()
	ios.PagerCmd = "less -R"

	if err := ios.StartPager(); err != nil {
		t.Fatalf("StartPager() error: %v", err)
	}

	// Out should remain the buffer (pager is a no-op for non-terminal)
	if ios.Out != out {
		t.Error("expected Out to remain unchanged for non-terminal")
	}
	if ios.pagerProcess != nil {
		t.Error("expected no pager process for non-terminal")
	}
}

func TestPager_JSONMode(t *testing.T) {
	ios, _, out, _ := Test()
	ios.IsTerminal = true
	ios.IsJSON = true
	ios.PagerCmd = "less -R"

	if err := ios.StartPager(); err != nil {
		t.Fatalf("StartPager() error: %v", err)
	}

	if ios.Out != out {
		t.Error("expected Out to remain unchanged in JSON mode")
	}
}

func TestPager_EmptyCmd(t *testing.T) {
	ios, _, out, _ := Test()
	ios.IsTerminal = true
	ios.PagerCmd = ""

	if err := ios.StartPager(); err != nil {
		t.Fatalf("StartPager() error: %v", err)
	}

	if ios.Out != out {
		t.Error("expected Out to remain unchanged with empty PagerCmd")
	}
}

func TestSystem_Defaults(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("PAGER", "")

	ios := System()

	if ios.In == nil || ios.Out == nil || ios.ErrOut == nil {
		t.Fatal("expected non-nil streams from System()")
	}
	if ios.Width <= 0 {
		t.Errorf("expected positive Width, got %d", ios.Width)
	}
	if ios.PagerCmd != "less -R" {
		t.Errorf("expected default PagerCmd='less -R', got %q", ios.PagerCmd)
	}
	if ios.IsJSON {
		t.Error("expected IsJSON=false by default")
	}
}
