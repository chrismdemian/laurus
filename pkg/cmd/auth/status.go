package auth

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/auth"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdStatus returns the auth status command.
func NewCmdStatus(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return statusRun(f)
		},
	}
}

func statusRun(f *cmdutil.Factory) error {
	cfg, err := f.Config()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.CanvasURL == "" {
		fmt.Println("Not logged in. Run 'laurus auth login' to authenticate.")
		return nil
	}

	td, err := f.Auth(cfg.CanvasURL)
	if err != nil {
		fmt.Printf("Canvas URL: %s\n", cfg.CanvasURL)
		fmt.Println("Token:      missing or invalid")
		fmt.Println("\nRun 'laurus auth login' to re-authenticate.")
		return nil
	}

	// Try to fetch user profile for display name
	client := canvas.NewClient(cfg.CanvasURL, td.Token, f.Version)
	profile, profileErr := canvas.Get[userProfile](context.Background(), client, "/api/v1/users/self/profile", nil)

	fmt.Printf("Canvas URL: %s\n", cfg.CanvasURL)
	if profileErr == nil {
		fmt.Printf("Logged in as: %s\n", profile.Name)
	}

	if !td.ExpiresAt.IsZero() {
		days := auth.DaysRemaining(td)
		fmt.Printf("Token expires: %s (%d days remaining)\n", td.ExpiresAt.Format("2006-01-02"), days)

		if auth.IsExpiringSoon(td) {
			fmt.Println("\nWarning: Token expires soon! Generate a new one at Canvas > Account > Settings.")
		}
	} else {
		fmt.Println("Token expiry: unknown (set via CANVAS_TOKEN env var)")
	}

	return nil
}
