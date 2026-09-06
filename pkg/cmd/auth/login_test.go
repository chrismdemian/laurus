package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/chrismdemian/laurus/internal/config"
	"github.com/chrismdemian/laurus/internal/onboard"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

func TestLogin_NoTTYFailsFast(t *testing.T) {
	t.Setenv(onboard.EnvToken, "")
	t.Setenv(onboard.EnvURL, "")
	f := &cmdutil.Factory{Config: func() (*config.Config, error) { return &config.Config{}, nil }}

	done := make(chan error, 1)
	go func() { done <- loginRun(f, loginOptions{}) }()
	select {
	case err := <-done:
		if !errors.Is(err, onboard.ErrNoTTY) {
			t.Fatalf("err = %v, want ErrNoTTY", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loginRun hung: a prompt was constructed without a TTY")
	}
}
