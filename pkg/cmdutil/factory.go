// Package cmdutil provides shared cobra helpers, error types, and factories.
package cmdutil

import (
	"github.com/chrismdemian/laurus/internal/auth"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/config"
	"github.com/chrismdemian/laurus/internal/iostreams"
)

// Factory provides lazy-loaded access to shared infrastructure for CLI commands.
type Factory struct {
	IOStreams func() *iostreams.IOStreams
	Config    func() (*config.Config, error)
	Auth      func(canvasURL string) (*auth.TokenData, error)
	Client    func() (*canvas.Client, error)
	Version   string
}
