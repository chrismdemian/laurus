// Package watch implements the watch command (notification daemon).
package watch

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdWatch returns the watch command.
func NewCmdWatch(f *cmdutil.Factory) *cobra.Command {
	var once bool

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Poll Canvas and send desktop notifications for new activity",
		Long: `Run a background polling loop that checks for new announcements,
grade changes, and upcoming deadlines, sending OS notifications.

Use --once for a single poll cycle (useful with cron/Task Scheduler).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return watchRun(f, once)
		},
	}

	cmd.Flags().BoolVar(&once, "once", false, "Run a single poll cycle and exit")

	return cmd
}

// Polling intervals.
const (
	announcementInterval = 30 * time.Minute
	gradeInterval        = 15 * time.Minute
	deadlineInterval     = 2 * time.Hour
)

// Deadline lead times for notifications.
var deadlineLeads = []time.Duration{24 * time.Hour, 6 * time.Hour, 1 * time.Hour}

func watchRun(f *cmdutil.Factory, once bool) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	db, err := f.Cache()
	if err != nil {
		return fmt.Errorf("opening cache: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		_, _ = fmt.Fprintln(ios.ErrOut, "\nShutting down...")
		cancel()
	}()

	_, _ = fmt.Fprintln(ios.Out, "Watching for Canvas activity... (Ctrl+C to stop)")

	errW := ios.ErrOut

	// Run first poll immediately.
	pollAll(ctx, client, db, errW)

	if once {
		return nil
	}

	announcementTicker := time.NewTicker(announcementInterval)
	gradeTicker := time.NewTicker(gradeInterval)
	deadlineTicker := time.NewTicker(deadlineInterval)
	cleanupTicker := time.NewTicker(24 * time.Hour)
	defer announcementTicker.Stop()
	defer gradeTicker.Stop()
	defer deadlineTicker.Stop()
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-announcementTicker.C:
			pollAnnouncements(ctx, client, db, errW)
		case <-gradeTicker.C:
			pollGrades(ctx, client, db, errW)
		case <-deadlineTicker.C:
			pollDeadlines(ctx, client, db, errW)
		case <-cleanupTicker.C:
			_ = db.CleanNotifications(7 * 24 * time.Hour)
		}
	}
}

func pollAll(ctx context.Context, client *canvas.Client, db *cache.DB, errW io.Writer) {
	pollAnnouncements(ctx, client, db, errW)
	pollGrades(ctx, client, db, errW)
	pollDeadlines(ctx, client, db, errW)
}

func pollAnnouncements(ctx context.Context, client *canvas.Client, db *cache.DB, errW io.Writer) {
	var contextCodes []string
	for c, err := range canvas.ListCourses(ctx, client, canvas.CourseListOptions{
		EnrollmentState: "active",
	}) {
		if err != nil {
			_, _ = fmt.Fprintf(errW, "watch: listing courses: %v\n", err)
			return
		}
		contextCodes = append(contextCodes, fmt.Sprintf("course_%d", c.ID))
	}

	if len(contextCodes) == 0 {
		return
	}

	for a, err := range canvas.ListAnnouncements(ctx, client, canvas.ListAnnouncementsOptions{
		ContextCodes: contextCodes,
		StartDate:    time.Now().Add(-24 * time.Hour).Format("2006-01-02"),
	}) {
		if err != nil {
			_, _ = fmt.Fprintf(errW, "watch: listing announcements: %v\n", err)
			return
		}
		key := fmt.Sprintf("announcement-%d", a.ID)
		if db.HasNotified(key) {
			continue
		}
		_ = beeep.Notify("Laurus — New Announcement", a.Title, "")
		_ = db.MarkNotified(key)
	}
}

func pollGrades(ctx context.Context, client *canvas.Client, db *cache.DB, errW io.Writer) {
	cutoff := time.Now().Add(-gradeInterval * 2)

	for c, err := range canvas.ListCourses(ctx, client, canvas.CourseListOptions{
		EnrollmentState: "active",
	}) {
		if err != nil {
			_, _ = fmt.Fprintf(errW, "watch: listing courses for grades: %v\n", err)
			return
		}
		for sub, err := range canvas.ListSubmissions(ctx, client, c.ID, canvas.ListSubmissionsOptions{
			WorkflowState: "graded",
		}) {
			if err != nil {
				_, _ = fmt.Fprintf(errW, "watch: listing submissions for %s: %v\n", c.CourseCode, err)
				break
			}
			if sub.Grade == nil || sub.PostedAt == nil {
				continue
			}
			// Skip old grades to reduce notification spam on first run.
			if sub.PostedAt.Before(cutoff) {
				continue
			}
			key := fmt.Sprintf("grade-%d-%s", sub.ID, *sub.Grade)
			if db.HasNotified(key) {
				continue
			}
			_ = beeep.Notify(
				"Laurus — Grade Posted",
				fmt.Sprintf("%s: Assignment graded (%s)", c.CourseCode, *sub.Grade),
				"",
			)
			_ = db.MarkNotified(key)
		}
	}
}

func pollDeadlines(ctx context.Context, client *canvas.Client, db *cache.DB, errW io.Writer) {
	events, err := canvas.ListUpcomingEvents(ctx, client)
	if err != nil {
		_, _ = fmt.Fprintf(errW, "watch: listing upcoming events: %v\n", err)
		return
	}

	for _, ev := range events {
		if ev.Assignment == nil || ev.Assignment.DueAt == nil {
			continue
		}
		dueAt := *ev.Assignment.DueAt
		timeUntilDue := time.Until(dueAt)
		if timeUntilDue <= 0 {
			continue
		}

		for _, lead := range deadlineLeads {
			if timeUntilDue > lead {
				continue
			}
			key := fmt.Sprintf("deadline-%d-%s", ev.Assignment.ID, lead)
			if db.HasNotified(key) {
				continue
			}
			_ = beeep.Notify(
				"Laurus — Deadline",
				fmt.Sprintf("%s due in %s", ev.Title, formatDuration(timeUntilDue)),
				"",
			)
			_ = db.MarkNotified(key)
			break // Only notify for the most urgent lead time.
		}
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}
