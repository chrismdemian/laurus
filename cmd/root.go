package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/auth"
	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/config"
	"github.com/chrismdemian/laurus/internal/iostreams"
	announcementscmd "github.com/chrismdemian/laurus/pkg/cmd/announcements"
	assignmentscmd "github.com/chrismdemian/laurus/pkg/cmd/assignments"
	authcmd "github.com/chrismdemian/laurus/pkg/cmd/auth"
	coursescmd "github.com/chrismdemian/laurus/pkg/cmd/courses"
	discussionscmd "github.com/chrismdemian/laurus/pkg/cmd/discussions"
	filescmd "github.com/chrismdemian/laurus/pkg/cmd/files"
	gradescmd "github.com/chrismdemian/laurus/pkg/cmd/grades"
	inboxcmd "github.com/chrismdemian/laurus/pkg/cmd/inbox"
	modulescmd "github.com/chrismdemian/laurus/pkg/cmd/modules"
	pagescmd "github.com/chrismdemian/laurus/pkg/cmd/pages"
	submitcmd "github.com/chrismdemian/laurus/pkg/cmd/submit"
	synccmd "github.com/chrismdemian/laurus/pkg/cmd/sync"
	todocmd "github.com/chrismdemian/laurus/pkg/cmd/todo"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "laurus",
	Short: "Canvas LMS from your terminal",
	Long:  "Laurus -- courses, assignments, grades, and files without opening a browser.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

// cacheDB is the singleton cache database, closed in OnFinalize.
var cacheDB *cache.DB

func init() {
	rootCmd.PersistentFlags().Bool("json", false, "Output as JSON")
	rootCmd.PersistentFlags().Bool("cached", false, "Read from local cache (offline mode)")
	rootCmd.PersistentFlags().Bool("no-color", false, "Disable color output")
	rootCmd.PersistentFlags().Bool("reset-cache", false, "Clear the local cache before running")

	cobra.OnFinalize(func() {
		if cacheDB != nil {
			_ = cacheDB.Close()
		}
	})

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if reset, _ := cmd.Flags().GetBool("reset-cache"); reset {
			dir, err := config.Dir()
			if err != nil {
				return err
			}
			db, err := cache.Open(filepath.Join(dir, "cache.db"))
			if err != nil {
				return fmt.Errorf("opening cache: %w", err)
			}
			if err := db.Reset(); err != nil {
				_ = db.Close()
				return fmt.Errorf("resetting cache: %w", err)
			}
			cacheDB = db
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Cache cleared.")
		}
		return nil
	}

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("laurus %s (%s) built %s\n", version, commit, date)
		},
	})

	f := &cmdutil.Factory{
		Version: version,
	}

	f.IOStreams = func() *iostreams.IOStreams {
		ios := iostreams.System()
		if noColor, _ := rootCmd.Flags().GetBool("no-color"); noColor {
			ios.ColorEnabled = false
		}
		if jsonMode, _ := rootCmd.Flags().GetBool("json"); jsonMode {
			ios.IsJSON = true
			ios.ColorEnabled = false
		}
		return ios
	}

	f.Config = func() (*config.Config, error) {
		return config.Load()
	}

	f.Auth = func(canvasURL string) (*auth.TokenData, error) {
		return auth.Load(canvasURL)
	}

	f.Client = func() (*canvas.Client, error) {
		cfg, err := f.Config()
		if err != nil {
			return nil, err
		}
		if cfg.CanvasURL == "" {
			return nil, fmt.Errorf("not logged in; run 'laurus auth login' first")
		}
		td, err := f.Auth(cfg.CanvasURL)
		if err != nil {
			return nil, fmt.Errorf("auth failed: %w", err)
		}
		return canvas.NewClient(cfg.CanvasURL, td.Token, f.Version), nil
	}

	f.Cache = func() (*cache.DB, error) {
		if cacheDB != nil {
			return cacheDB, nil
		}
		dir, err := config.Dir()
		if err != nil {
			return nil, err
		}
		db, err := cache.Open(filepath.Join(dir, "cache.db"))
		if err != nil {
			return nil, fmt.Errorf("opening cache: %w", err)
		}
		cacheDB = db
		return db, nil
	}

	rootCmd.AddCommand(announcementscmd.NewCmdAnnouncements(f))
	rootCmd.AddCommand(announcementscmd.NewCmdAnnouncement(f))
	rootCmd.AddCommand(authcmd.NewCmdAuth(f))
	rootCmd.AddCommand(coursescmd.NewCmdCourses(f))
	rootCmd.AddCommand(coursescmd.NewCmdCourse(f))
	rootCmd.AddCommand(discussionscmd.NewCmdDiscussions(f))
	rootCmd.AddCommand(discussionscmd.NewCmdDiscussion(f))
	rootCmd.AddCommand(discussionscmd.NewCmdReply(f))
	rootCmd.AddCommand(assignmentscmd.NewCmdAssignments(f))
	rootCmd.AddCommand(assignmentscmd.NewCmdAssignment(f))
	rootCmd.AddCommand(assignmentscmd.NewCmdNext(f))
	rootCmd.AddCommand(filescmd.NewCmdFiles(f))
	rootCmd.AddCommand(filescmd.NewCmdDownload(f))
	rootCmd.AddCommand(filescmd.NewCmdDownloadAll(f))
	rootCmd.AddCommand(gradescmd.NewCmdGrades(f))
	rootCmd.AddCommand(gradescmd.NewCmdGrade(f))
	rootCmd.AddCommand(inboxcmd.NewCmdInbox(f))
	rootCmd.AddCommand(modulescmd.NewCmdModules(f))
	rootCmd.AddCommand(modulescmd.NewCmdMarkDone(f))
	rootCmd.AddCommand(pagescmd.NewCmdPages(f))
	rootCmd.AddCommand(pagescmd.NewCmdPage(f))
	rootCmd.AddCommand(submitcmd.NewCmdSubmit(f))
	rootCmd.AddCommand(synccmd.NewCmdSync(f))
	rootCmd.AddCommand(todocmd.NewCmdTodo(f))
}
