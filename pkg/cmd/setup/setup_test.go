package setup

import (
	"errors"
	"testing"
	"time"

	"github.com/chrismdemian/laurus/internal/config"
	"github.com/chrismdemian/laurus/internal/onboard"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// Under `go test` stdin and stdout are not terminals, so with no flags and no
// env the command must fail fast instead of constructing a huh form that
// would hang until the test timeout.
func TestSetup_NoTTYFailsFast(t *testing.T) {
	t.Setenv(onboard.EnvToken, "")
	t.Setenv(onboard.EnvURL, "")
	f := &cmdutil.Factory{Config: func() (*config.Config, error) { return &config.Config{}, nil }}

	done := make(chan error, 1)
	go func() { done <- setupRun(f, setupOptions{}) }()
	select {
	case err := <-done:
		if !errors.Is(err, onboard.ErrNoTTY) {
			t.Fatalf("err = %v, want ErrNoTTY", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setupRun hung: a prompt was constructed without a TTY")
	}
}

// A URL alone is not enough: the token is still missing, so headless use must
// still fail fast (and --yes must not change that).
func TestSetup_URLOnlyNoTTYFailsFast(t *testing.T) {
	t.Setenv(onboard.EnvToken, "")
	t.Setenv(onboard.EnvURL, "")
	f := &cmdutil.Factory{Config: func() (*config.Config, error) { return &config.Config{}, nil }}

	done := make(chan error, 1)
	go func() { done <- setupRun(f, setupOptions{url: "https://x.example.edu", yes: true}) }()
	select {
	case err := <-done:
		if !errors.Is(err, onboard.ErrNoTTY) {
			t.Fatalf("err = %v, want ErrNoTTY", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setupRun hung")
	}
}
