// Package update implements the self-update command.
package update

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/update"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdUpdate returns the update command.
func NewCmdUpdate(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update Laurus to the latest version",
		Long:  "Check GitHub Releases for a newer version and replace the current binary.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return updateRun(f)
		},
	}
	return cmd
}

func updateRun(f *cmdutil.Factory) error {
	ios := f.IOStreams()

	if f.Version == "dev" {
		fmt.Fprintln(ios.ErrOut, "Skipping update check for dev build.")
		return nil
	}

	fmt.Fprintf(ios.ErrOut, "Checking for updates (current: %s)...\n", f.Version)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := update.CheckLatest(ctx, f.Version)
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}

	// Cache the result regardless of outcome
	_ = update.SaveCachedCheck(result.LatestVersion, result.CurrentVersion)

	if !result.HasUpdate {
		fmt.Fprintf(ios.Out, "Already up to date (%s).\n", f.Version)
		return nil
	}

	fmt.Fprintf(ios.Out, "New version available: %s → %s\n", f.Version, result.LatestVersion)
	fmt.Fprintf(ios.ErrOut, "Downloading and installing...\n")

	if err := update.Apply(ctx, result.Release); err != nil {
		return fmt.Errorf("applying update: %w", err)
	}

	fmt.Fprintf(ios.Out, "Successfully updated to %s. Please restart laurus.\n", result.LatestVersion)
	return nil
}
