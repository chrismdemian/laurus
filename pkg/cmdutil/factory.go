// Package cmdutil provides shared cobra helpers, error types, and factories.
package cmdutil

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/auth"
	"github.com/chrismdemian/laurus/internal/cache"
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
	Cache     func() (*cache.DB, error)
	Version   string
}

// IsCached returns true if the --cached persistent flag is set on the command.
func IsCached(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("cached")
	return v
}

// PrintCacheFreshness writes a freshness indicator to w based on sync metadata.
// Output goes to stderr so it doesn't interfere with --json or piping.
func PrintCacheFreshness(w io.Writer, db *cache.DB, rt cache.ResourceType, courseID int64) {
	meta, err := db.GetSyncMeta(rt, courseID)
	if err != nil || meta.LastSyncAt.IsZero() {
		_, _ = fmt.Fprintln(w, "(never synced)")
		return
	}
	age := RelativeTime(meta.LastSyncAt)
	if db.IsStale(rt, courseID) {
		_, _ = fmt.Fprintf(w, "(cached %s -- may be stale)\n", age)
	} else {
		_, _ = fmt.Fprintf(w, "(cached %s)\n", age)
	}
}
