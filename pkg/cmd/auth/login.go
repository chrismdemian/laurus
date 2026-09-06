package auth

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/onboard"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// userProfile is a minimal struct for the /users/self/profile response
// (used by auth status).
type userProfile struct {
	Name     string `json:"name"`
	TimeZone string `json:"time_zone"`
}

// NewCmdLogin returns the auth login command.
func NewCmdLogin(f *cmdutil.Factory) *cobra.Command {
	var opts loginOptions

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to Canvas LMS",
		Long: `Authenticate with a Canvas LMS instance using an API access token.

To generate a token: Canvas > Account > Settings > New Access Token

Non-interactive use (scripts, coding agents): the minimum is a token and the
institution's Canvas URL. Any of these skip the prompts entirely:

  laurus auth login --token <token> --url https://canvas.school.edu
  echo "$TOKEN" | laurus auth login --token-stdin --url https://canvas.school.edu
  CANVAS_TOKEN=<token> CANVAS_URL=https://canvas.school.edu laurus auth login

Without a terminal on stdin and stdout, missing values are an error rather
than a prompt that never returns.`,
		// Errors here are actionable on their own (missing token, rejected
		// token); a usage dump after them only buries the message for agents.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return loginRun(f, opts)
		},
	}

	cmd.Flags().StringVar(&opts.token, "token", "", "Canvas API token (or set CANVAS_TOKEN)")
	cmd.Flags().BoolVar(&opts.tokenStdin, "token-stdin", false, "Read the Canvas API token from stdin, so it never appears in argv")
	cmd.Flags().StringVar(&opts.url, "url", "", "Canvas base URL, e.g. https://q.utoronto.ca (or set CANVAS_URL)")
	cmd.MarkFlagsMutuallyExclusive("token", "token-stdin")

	return cmd
}

type loginOptions struct {
	token      string
	tokenStdin bool
	url        string
}

func loginRun(f *cmdutil.Factory, opts loginOptions) error {
	existingURL := ""
	if cfg, err := f.Config(); err == nil {
		existingURL = cfg.CanvasURL
	}

	resolved, err := onboard.Resolve(onboard.Source{
		FlagToken:      opts.token,
		TokenFromStdin: opts.tokenStdin,
		FlagURL:        opts.url,
		Stdin:          os.Stdin,
		ExistingURL:    existingURL,
	})
	if err != nil {
		return err
	}
	interactive := onboard.IsInteractive(os.Stdin, os.Stdout)
	if err := onboard.Guard(resolved, interactive); err != nil {
		return err
	}

	// Only what is still missing is asked for; with --token and --url (or the
	// env vars) no form is ever constructed.
	if resolved.URL == "" {
		u, ok, err := promptURL()
		if err != nil || !ok {
			return err
		}
		resolved.URL = u
	}
	if resolved.Token == "" {
		tok, ok, err := promptToken()
		if err != nil || !ok {
			return err
		}
		resolved.Token = tok
	}

	fmt.Println("Validating token...")
	res, err := onboard.Configure(context.Background(), onboard.Options{
		URL:     resolved.URL,
		Token:   resolved.Token,
		Version: f.Version,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nMake sure you copied the full token from Canvas > Account > Settings.")
		return err
	}

	fmt.Printf("\nLogged in as %s\n", res.Name)
	fmt.Printf("Canvas URL:  %s\n", res.URL)
	fmt.Printf("Timezone:    %s\n", res.TimeZone)
	fmt.Printf("Token expires: %s (%d days remaining)\n", res.ExpiresAt.Format("2006-01-02"), res.Days)
	return nil
}

// promptURL runs the instance picker. ok is false when the user aborted.
func promptURL() (url string, ok bool, err error) {
	var choice, custom string
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
				Value(&choice),
		),
	).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if err := selectForm.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return "", false, nil
		}
		return "", false, err
	}
	if choice != "custom" {
		return choice, true, nil
	}

	customForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Canvas URL").
				Description("e.g., https://canvas.myschool.edu").
				Placeholder("https://canvas.example.edu").
				Validate(func(s string) error {
					_, err := onboard.NormalizeURL(s)
					return err
				}).
				Value(&custom),
		),
	).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if err := customForm.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return "", false, nil
		}
		return "", false, err
	}
	u, err := onboard.NormalizeURL(custom)
	return u, err == nil, err
}

// promptToken asks for the token with echo off. ok is false when aborted.
func promptToken() (token string, ok bool, err error) {
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
			return "", false, nil
		}
		return "", false, err
	}
	return token, true, nil
}
