package auth

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/auth"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/config"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// userProfile is a minimal struct for the /users/self/profile response.
type userProfile struct {
	Name     string `json:"name"`
	TimeZone string `json:"time_zone"`
}

// NewCmdLogin returns the auth login command.
func NewCmdLogin(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in to Canvas LMS",
		Long: `Authenticate with a Canvas LMS instance using an API access token.

To generate a token: Canvas > Account > Settings > New Access Token`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return loginRun(f)
		},
	}
}

func loginRun(f *cmdutil.Factory) error {
	const defaultExpiryDays = 120

	var canvasURL string
	var customURL string
	var token string

	// Step 1: Select Canvas instance
	selectForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Canvas instance").
				Description("Choose your institution or enter a custom URL").
				Options(
					huh.NewOption("University of Toronto (q.utoronto.ca)", "https://q.utoronto.ca"),
					huh.NewOption("Instructure (canvas.instructure.com)", "https://canvas.instructure.com"),
					huh.NewOption("Custom URL", "custom"),
				).
				Value(&canvasURL),
		),
	).WithAccessible(os.Getenv("ACCESSIBLE") != "")

	if err := selectForm.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return nil
		}
		return err
	}

	if canvasURL == "custom" {
		customForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Canvas URL").
					Description("e.g., https://canvas.myschool.edu").
					Placeholder("https://canvas.example.edu").
					Validate(func(s string) error {
						if !strings.HasPrefix(s, "https://") && !strings.HasPrefix(s, "http://") {
							return fmt.Errorf("URL must start with https:// or http://")
						}
						if len(s) < 10 {
							return fmt.Errorf("URL too short")
						}
						return nil
					}).
					Value(&customURL),
			),
		).WithAccessible(os.Getenv("ACCESSIBLE") != "")

		if err := customForm.Run(); err != nil {
			if err == huh.ErrUserAborted {
				return nil
			}
			return err
		}
		canvasURL = customURL
	}

	// Step 2: Token input
	tokenForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("API Token").
				Description("Canvas > Account > Settings > New Access Token").
				EchoMode(huh.EchoModePassword).
				Placeholder("xxxx~...").
				Validate(func(s string) error {
					if len(s) < 10 {
						return fmt.Errorf("token looks too short")
					}
					return nil
				}).
				Value(&token),
		),
	).WithAccessible(os.Getenv("ACCESSIBLE") != "")

	if err := tokenForm.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return nil
		}
		return err
	}

	// Step 3: Validate token by calling the profile endpoint
	fmt.Println("Validating token...")
	client := canvas.NewClient(canvasURL, token, f.Version)
	profile, err := canvas.Get[userProfile](context.Background(), client, "/api/v1/users/self/profile", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nMake sure you copied the full token from Canvas > Account > Settings.")
		return fmt.Errorf("token validation failed: %w", err)
	}

	// Step 4: Store token
	expiresAt := time.Now().Add(time.Duration(defaultExpiryDays) * 24 * time.Hour)
	if err := auth.Store(canvasURL, token, expiresAt); err != nil {
		return fmt.Errorf("storing token: %w", err)
	}

	// Step 5: Save Canvas URL to config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	cfg.CanvasURL = canvasURL
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	// Step 6: Success
	days := auth.DaysRemaining(&auth.TokenData{ExpiresAt: expiresAt})
	fmt.Printf("\nLogged in as %s\n", profile.Name)
	fmt.Printf("Canvas URL:  %s\n", canvasURL)
	fmt.Printf("Timezone:    %s\n", profile.TimeZone)
	fmt.Printf("Token expires: %s (%d days remaining)\n", expiresAt.Format("2006-01-02"), days)

	return nil
}
