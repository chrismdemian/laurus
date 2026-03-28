package auth

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/auth"
	"github.com/chrismdemian/laurus/internal/config"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdLogout returns the auth logout command.
func NewCmdLogout(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out of Canvas LMS",
		RunE: func(cmd *cobra.Command, args []string) error {
			return logoutRun(f)
		},
	}
}

func logoutRun(f *cmdutil.Factory) error {
	cfg, err := f.Config()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.CanvasURL == "" {
		fmt.Println("Not logged in.")
		return nil
	}

	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Log out of %s?", cfg.CanvasURL)).
				Value(&confirmed),
		),
	).WithAccessible(os.Getenv("ACCESSIBLE") != "")

	if err := form.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return nil
		}
		return err
	}

	if !confirmed {
		fmt.Println("Cancelled.")
		return nil
	}

	if err := auth.Delete(cfg.CanvasURL); err != nil {
		return fmt.Errorf("removing token: %w", err)
	}

	cfg.CanvasURL = ""
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println("Logged out successfully.")
	return nil
}
