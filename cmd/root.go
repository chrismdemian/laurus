package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "laurus",
	Short: "Canvas LMS from your terminal",
	Long:  "Laurus — courses, assignments, grades, and files without opening a browser.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("laurus %s (%s) built %s\n", version, commit, date)
		},
	})

	rootCmd.PersistentFlags().Bool("json", false, "Output as JSON")
	rootCmd.PersistentFlags().Bool("cached", false, "Read from local cache (offline mode)")
	rootCmd.PersistentFlags().Bool("no-color", false, "Disable color output")
}
