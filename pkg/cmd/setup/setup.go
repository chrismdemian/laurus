// Package setup implements the first-run onboarding wizard.
package setup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/pkg/browser"
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

// NewCmdSetup returns the setup command.
func NewCmdSetup(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up Laurus for the first time",
		Long:  "Interactive wizard that connects Laurus to your Canvas LMS instance.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return setupRun(f)
		},
	}
	return cmd
}

func setupRun(f *cmdutil.Factory) error {
	const defaultExpiryDays = 120

	fmt.Println()
	fmt.Println("  Laurus — Canvas LMS from your terminal")
	fmt.Println("  ───────────────────────────────────────")
	fmt.Println()

	// Step 1: Select Canvas instance
	var canvasURL string
	var customURL string
	selectForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Which Canvas instance does your school use?").
				Options(
					huh.NewOption("University of Toronto (q.utoronto.ca)", "https://q.utoronto.ca"),
					huh.NewOption("Instructure (canvas.instructure.com)", "https://canvas.instructure.com"),
					huh.NewOption("Other / Custom URL", "custom"),
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
		canvasURL = strings.TrimRight(customURL, "/")
	}

	// Step 2: Open browser for token generation
	tokenURL := canvasURL + "/profile/settings"
	var openBrowser bool
	browserForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Open Canvas settings in your browser?").
				Description("You'll need to generate an API token under 'Approved Integrations'.").
				Affirmative("Yes, open browser").
				Negative("No, I have a token already").
				Value(&openBrowser),
		),
	).WithAccessible(os.Getenv("ACCESSIBLE") != "")

	if err := browserForm.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return nil
		}
		return err
	}

	if openBrowser {
		_ = browser.OpenURL(tokenURL)
		fmt.Println()
		fmt.Println("  In Canvas: Account → Settings → + New Access Token")
		fmt.Println("  Copy the token and paste it below.")
		fmt.Println()
	}

	// Step 3: Token input
	var token string
	tokenForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("API Token").
				Description("Paste the token you generated in Canvas").
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

	// Step 4: Validate token
	fmt.Println("Validating token...")
	client := canvas.NewClient(canvasURL, token, f.Version)
	profile, err := canvas.Get[userProfile](context.Background(), client, "/api/v1/users/self/profile", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nMake sure you copied the full token from Canvas > Account > Settings > Approved Integrations.")
		return fmt.Errorf("token validation failed: %w", err)
	}

	// Step 5: Store token and save config
	expiresAt := time.Now().Add(time.Duration(defaultExpiryDays) * 24 * time.Hour)
	if err := auth.Store(canvasURL, token, expiresAt); err != nil {
		return fmt.Errorf("storing token: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	cfg.CanvasURL = canvasURL
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	// Step 6: Success
	fmt.Println()
	fmt.Printf("  You're all set, %s!\n", profile.Name)
	fmt.Printf("  Connected to %s (%s)\n", canvasURL, profile.TimeZone)
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Println("    laurus sync          Cache your courses and assignments")
	fmt.Println("    laurus next          See your next deadline")
	fmt.Println("    laurus grades        View all your grades")
	fmt.Println("    laurus doctor        Check everything is working")
	fmt.Println()

	return nil
}
