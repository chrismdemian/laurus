package assignments

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdNext returns the "next" command showing the next upcoming assignment.
func NewCmdNext(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Show your next upcoming assignment",
		Long:  "Display the single next assignment due across all your courses.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nextRun(f)
		},
	}
	return cmd
}

func nextRun(f *cmdutil.Factory) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()
	now := time.Now()

	// Fast path: upcoming_events (single non-paginated API call)
	a, courseName, err := nextFromUpcomingEvents(ctx, client, now)
	if err != nil {
		return err
	}

	// Fallback: iterate courses if fast path found nothing
	if a == nil {
		a, courseName, err = nextFromCourses(ctx, client, now)
		if err != nil {
			return err
		}
	}

	if a == nil {
		_, _ = fmt.Fprintln(ios.Out, "No upcoming assignments.")
		return nil
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, a)
	}

	// Single-line output: COURSE  Name    due in Xh
	palette := cmdutil.NewPalette(ios)
	_, style := assignmentStatus(*a, now, palette)

	due := "no due date"
	if a.DueAt != nil {
		due = cmdutil.RelativeTime(*a.DueAt)
	}

	line := fmt.Sprintf("%s  %s    %s",
		palette.Header.Render(courseName),
		style.Render(a.Name),
		palette.Muted.Render(due),
	)
	_, _ = fmt.Fprintln(ios.Out, line)
	return nil
}

// nextFromUpcomingEvents uses the fast /upcoming_events endpoint.
func nextFromUpcomingEvents(ctx context.Context, client *canvas.Client, now time.Time) (*canvas.Assignment, string, error) {
	events, err := canvas.ListUpcomingEvents(ctx, client)
	if err != nil {
		return nil, "", fmt.Errorf("fetching upcoming events: %w", err)
	}

	// Filter to assignment-type events with a future due date
	var best *canvas.Assignment
	var bestCourseID int64
	for i := range events {
		ev := &events[i]
		if ev.Assignment == nil {
			continue
		}
		a := ev.Assignment
		if a.DueAt == nil || !a.DueAt.After(now) {
			continue
		}
		if best == nil || a.DueAt.Before(*best.DueAt) {
			best = a
			bestCourseID = a.CourseID
		}
	}

	if best == nil {
		return nil, "", nil
	}

	// Resolve course name from course map (single paginated call, but still
	// much cheaper than the fallback path of iterating all courses' assignments)
	courseName := ""
	if bestCourseID != 0 {
		courseMap, err := buildCourseMap(ctx, client)
		if err == nil {
			courseName = courseMap[bestCourseID]
		}
	}
	if courseName == "" {
		courseName = "???"
	}

	return best, courseName, nil
}

// nextFromCourses is the fallback that iterates all courses.
func nextFromCourses(ctx context.Context, client *canvas.Client, now time.Time) (*canvas.Assignment, string, error) {
	var courses []canvas.Course
	for c, err := range canvas.ListCourses(ctx, client, canvas.CourseListOptions{
		EnrollmentState: "active",
	}) {
		if err != nil {
			return nil, "", fmt.Errorf("listing courses: %w", err)
		}
		courses = append(courses, c)
	}

	var best *canvas.Assignment
	var bestCourse string
	for _, course := range courses {
		for a, err := range canvas.ListAssignments(ctx, client, course.ID, canvas.ListAssignmentsOptions{
			Bucket:  "upcoming",
			OrderBy: "due_at",
			Include: []string{"submission"},
		}) {
			if err != nil {
				return nil, "", fmt.Errorf("listing assignments for %s: %w", course.CourseCode, err)
			}
			if a.DueAt == nil || !a.DueAt.After(now) {
				continue
			}
			if isSubmitted(a) {
				continue
			}
			if best == nil || a.DueAt.Before(*best.DueAt) {
				aCopy := a
				best = &aCopy
				bestCourse = course.CourseCode
			}
		}
	}

	return best, bestCourse, nil
}
