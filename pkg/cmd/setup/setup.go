// Package setup implements the first-run onboarding wizard.
package setup

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/onboard"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdSetup returns the setup command.
func NewCmdSetup(f *cmdutil.Factory) *cobra.Command {
	var opts setupOptions

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up Laurus for the first time",
		Long: `Connect Laurus to your Canvas LMS instance.

Run with no flags for the interactive wizard. For scripts and coding agents,
the minimum input is a Canvas API token plus the institution's Canvas URL,
and with both supplied nothing is prompted:

  laurus setup --token <token> --url https://canvas.school.edu --yes
  echo "$TOKEN" | laurus setup --token-stdin --url https://canvas.school.edu --yes
  CANVAS_TOKEN=<token> CANVAS_URL=https://canvas.school.edu laurus setup --yes

--yes suppresses the remaining prompts (the browser offer included); if a
required value is still missing it is an error. Without a terminal on stdin
and stdout, missing values are always an error rather than a hung prompt.`,
		// Errors here are actionable on their own (missing token, rejected
		// token); a usage dump after them only buries the message for agents.
		SilenceUsage: true,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return setupRun(f, opts)
		},
	}

	cmd.Flags().StringVar(&opts.token, "token", "", "Canvas API token (or set CANVAS_TOKEN)")
	cmd.Flags().BoolVar(&opts.tokenStdin, "token-stdin", false, "Read the Canvas API token from stdin, so it never appears in argv")
	cmd.Flags().StringVar(&opts.url, "url", "", "Canvas base URL, e.g. https://q.utoronto.ca (or set CANVAS_URL)")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Never prompt: skip the browser offer and fail if a value is missing")
	cmd.MarkFlagsMutuallyExclusive("token", "token-stdin")

	return cmd
}

type setupOptions struct {
	token      string
	tokenStdin bool
	url        string
	yes        bool
}

func setupRun(f *cmdutil.Factory, opts setupOptions) error {
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
	interactive := onboard.IsInteractive(os.Stdin, os.Stdout) && !opts.yes
	if err := onboard.Guard(resolved, interactive); err != nil {
		return err
	}

	if !resolved.Complete() {
		fmt.Println()
		fmt.Println("  Laurus — Canvas LMS from your terminal")
		fmt.Println("  ───────────────────────────────────────")
		fmt.Println()
	}

	if resolved.URL == "" {
		u, ok, err := promptURL()
		if err != nil || !ok {
			return err
		}
		resolved.URL = u
	}

	if resolved.Token == "" {
		if err := offerBrowser(resolved.URL); err != nil {
			return err
		}
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
		fmt.Fprintln(os.Stderr, "\nMake sure you copied the full token from Canvas > Account > Settings > Approved Integrations.")
		return err
	}

	fmt.Println()
	fmt.Printf("  You're all set, %s!\n", res.Name)
	fmt.Printf("  Connected to %s (%s)\n", res.URL, res.TimeZone)
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Println("    laurus sync          Cache your courses and assignments")
	fmt.Println("    laurus next          See your next deadline")
	fmt.Println("    laurus grades        View all your grades")
	fmt.Println("    laurus doctor        Check everything is working")
	fmt.Println()
	return nil
}

func promptURL() (url string, ok bool, err error) {
	var choice, custom string
	selectForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Which Canvas instance does your school use?").
				Options(
					huh.NewOption("University of Toronto (q.utoronto.ca)", "https://q.utoronto.ca"),
					huh.NewOption("Instructure (canvas.instructure.com)", "https://canvas.instructure.com"),
					huh.NewOption("Other / Custom URL", "custom"),
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

// offerBrowser asks whether to open Canvas's token page. Only reached when a
// token still has to be typed, so never in the flag or env paths.
func offerBrowser(canvasURL string) error {
	var open bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Open Canvas settings in your browser?").
				Description("You'll need to generate an API token under 'Approved Integrations'.").
				Affirmative("Yes, open browser").
				Negative("No, I have a token already").
				Value(&open),
		),
	).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if err := form.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return nil
		}
		return err
	}
	if open {
		_ = browser.OpenURL(canvasURL + "/profile/settings")
		fmt.Println()
		fmt.Println("  In Canvas: Account → Settings → + New Access Token")
		fmt.Println("  Copy the token and paste it below.")
		fmt.Println()
	}
	return nil
}

func promptToken() (token string, ok bool, err error) {
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
			return "", false, nil
		}
		return "", false, err
	}
	return token, true, nil
}
