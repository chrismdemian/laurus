// Package status implements the status command for shell prompt integration.
package status

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdStatus returns the status command.
func NewCmdStatus(f *cmdutil.Factory) *cobra.Command {
	var short bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Quick summary for shell prompts (cache-only, <10ms)",
		Long:  "Show a brief status: items due this week, overdue, and unread announcements.\nReads from local cache only — run 'laurus sync' to populate.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return statusRun(f, short)
		},
	}

	cmd.Flags().BoolVar(&short, "short", false, "Minimal output: due/overdue/unread (e.g., 3/1/2)")

	return cmd
}

type statusCounts struct {
	DueThisWeek int `json:"due_this_week"`
	Overdue     int `json:"overdue"`
	Unread      int `json:"unread"`
}

func statusRun(f *cmdutil.Factory, short bool) error {
	ios := f.IOStreams()

	db, err := f.Cache()
	if err != nil {
		_, _ = fmt.Fprintln(ios.ErrOut, "No cache available. Run 'laurus sync' to populate.")
		return nil
	}

	// Read cached assignments.
	var assignments []canvas.Assignment
	if err := db.List(cache.ResourceAssignments, 0, &assignments); err != nil {
		_, _ = fmt.Fprintln(ios.ErrOut, "No cached data. Run 'laurus sync' to populate.")
		return nil
	}

	now := time.Now()
	weekEnd := endOfWeek(now)

	var counts statusCounts
	for _, a := range assignments {
		if a.DueAt == nil {
			continue
		}
		// Skip already submitted assignments.
		if a.Submission != nil && a.Submission.SubmittedAt != nil {
			continue
		}
		if a.Submission != nil && (a.Submission.WorkflowState == "graded" || a.Submission.Grade != nil) {
			continue
		}

		if a.DueAt.Before(now) {
			counts.Overdue++
		} else if a.DueAt.Before(weekEnd) {
			counts.DueThisWeek++
		}
	}

	// Read cached announcements for unread count.
	var announcements []canvas.Announcement
	if err := db.List(cache.ResourceAnnouncements, 0, &announcements); err == nil {
		for _, a := range announcements {
			if a.ReadState == "unread" {
				counts.Unread++
			}
		}
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, counts)
	}

	if short {
		_, _ = fmt.Fprintf(ios.Out, "%d/%d/%d\n", counts.DueThisWeek, counts.Overdue, counts.Unread)
		return nil
	}

	parts := []string{}
	if counts.DueThisWeek > 0 {
		parts = append(parts, fmt.Sprintf("%d due this week", counts.DueThisWeek))
	}
	if counts.Overdue > 0 {
		parts = append(parts, fmt.Sprintf("%d overdue", counts.Overdue))
	}
	if counts.Unread > 0 {
		parts = append(parts, fmt.Sprintf("%d unread", counts.Unread))
	}

	if len(parts) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "All clear.")
	} else {
		for i, p := range parts {
			if i > 0 {
				_, _ = fmt.Fprint(ios.Out, " | ")
			}
			_, _ = fmt.Fprint(ios.Out, p)
		}
		_, _ = fmt.Fprintln(ios.Out)
	}

	return nil
}

// endOfWeek returns end of Sunday for the current week.
func endOfWeek(now time.Time) time.Time {
	weekday := now.Weekday()
	daysUntilSunday := (7 - int(weekday)) % 7
	sunday := now.AddDate(0, 0, daysUntilSunday)
	return time.Date(sunday.Year(), sunday.Month(), sunday.Day(), 23, 59, 59, 0, now.Location())
}
