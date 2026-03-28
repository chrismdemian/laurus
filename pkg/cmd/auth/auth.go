// Package auth implements the auth command group (login, status, logout).
package auth

import (
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdAuth returns the parent auth command with login/status/logout subcommands.
func NewCmdAuth(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth <command>",
		Short: "Manage Canvas authentication",
		Long:  "Log in, check status, or log out of your Canvas LMS instance.",
	}

	cmd.AddCommand(NewCmdLogin(f))
	cmd.AddCommand(NewCmdStatus(f))
	cmd.AddCommand(NewCmdLogout(f))

	return cmd
}
