// Package assignments implements the assignments command group.
package assignments

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdAssignments returns the assignments list command.
func NewCmdAssignments(f *cmdutil.Factory) *cobra.Command {
	var opts listOpts

	cmd := &cobra.Command{
		Use:   "assignments",
		Short: "List assignments across your courses",
		Long:  "Display assignments with due dates, points, and submission status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listRun(f, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Course, "course", "c", "", "Filter to a specific course (name, code, or ID)")
	cmd.Flags().StringVar(&opts.Due, "due", "", "Filter by due date: \"today\" (24h) or \"week\" (7d)")
	cmd.Flags().BoolVar(&opts.Overdue, "overdue", false, "Show only overdue assignments")
	cmd.Flags().BoolVar(&opts.Missing, "missing", false, "Show only Canvas-flagged missing assignments")
	cmd.Flags().BoolVar(&opts.Unsubmitted, "unsubmitted", false, "Show only unsubmitted assignments")

	cmd.MarkFlagsMutuallyExclusive("overdue", "due")
	cmd.MarkFlagsMutuallyExclusive("overdue", "missing")
	cmd.MarkFlagsMutuallyExclusive("missing", "due")

	return cmd
}

type listOpts struct {
	Course      string
	Due         string // "today" or "week"
	Overdue     bool
	Missing     bool
	Unsubmitted bool
}

func listRun(f *cmdutil.Factory, opts listOpts) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()
	now := time.Now()

	type assignmentWithCourse struct {
		canvas.Assignment
		CourseName string
	}

	var items []assignmentWithCourse

	if opts.Missing {
		// Use the dedicated missing submissions endpoint
		courseNames, err := buildCourseMap(ctx, client)
		if err != nil {
			return err
		}
		for a, err := range canvas.ListMissingSubmissions(ctx, client, nil) {
			if err != nil {
				return fmt.Errorf("listing missing submissions: %w", err)
			}
			// The missing_submissions endpoint returns bare assignments without
			// embedded submission data, so explicitly mark them as missing.
			a.Missing = true
			items = append(items, assignmentWithCourse{
				Assignment: a,
				CourseName: courseNames[a.CourseID],
			})
		}
	} else if opts.Course != "" {
		// Single course
		course, err := canvas.FindCourse(ctx, client, opts.Course)
		if err != nil {
			return fmt.Errorf("finding course %q: %w", opts.Course, err)
		}
		for a, err := range canvas.ListAssignments(ctx, client, course.ID, canvas.ListAssignmentsOptions{
			Include: []string{"submission"},
			OrderBy: "due_at",
		}) {
			if err != nil {
				return fmt.Errorf("listing assignments: %w", err)
			}
			items = append(items, assignmentWithCourse{
				Assignment: a,
				CourseName: course.CourseCode,
			})
		}
	} else {
		// All active courses
		var courses []canvas.Course
		for c, err := range canvas.ListCourses(ctx, client, canvas.CourseListOptions{
			EnrollmentState: "active",
		}) {
			if err != nil {
				return fmt.Errorf("listing courses: %w", err)
			}
			courses = append(courses, c)
		}

		// Opportunistic cache write for courses.
		if db, err := f.Cache(); err == nil {
			courseItems := make([]cache.CacheItem, len(courses))
			for i, x := range courses {
				courseItems[i] = cache.CacheItem{ID: x.ID, CourseID: 0, Data: x}
			}
			_ = db.UpsertMany(cache.ResourceCourses, courseItems)
			_ = db.SetSyncMeta(cache.ResourceCourses, 0, len(courseItems), "success")
		}

		for _, course := range courses {
			var courseAssignments []canvas.Assignment
			for a, err := range canvas.ListAssignments(ctx, client, course.ID, canvas.ListAssignmentsOptions{
				Include: []string{"submission"},
				OrderBy: "due_at",
			}) {
				if err != nil {
					return fmt.Errorf("listing assignments for %s: %w", course.CourseCode, err)
				}
				courseAssignments = append(courseAssignments, a)
				items = append(items, assignmentWithCourse{
					Assignment: a,
					CourseName: course.CourseCode,
				})
			}

			// Opportunistic cache write for assignments.
			if db, err := f.Cache(); err == nil {
				aItems := make([]cache.CacheItem, len(courseAssignments))
				for i, x := range courseAssignments {
					aItems[i] = cache.CacheItem{ID: x.ID, CourseID: course.ID, Data: x}
				}
				_ = db.UpsertMany(cache.ResourceAssignments, aItems)
				_ = db.SetSyncMeta(cache.ResourceAssignments, course.ID, len(aItems), "success")
			}
		}
	}

	// Sort by due date (nil due dates sort to end)
	sort.Slice(items, func(i, j int) bool {
		di, dj := items[i].DueAt, items[j].DueAt
		if di == nil && dj == nil {
			return items[i].Name < items[j].Name
		}
		if di == nil {
			return false
		}
		if dj == nil {
			return true
		}
		return di.Before(*dj)
	})

	// Apply local filters
	var filtered []assignmentWithCourse
	for _, a := range items {
		if opts.Overdue {
			if a.DueAt == nil || !a.DueAt.Before(now) {
				continue
			}
			if isSubmitted(a.Assignment) {
				continue
			}
		}
		if opts.Unsubmitted {
			if isSubmitted(a.Assignment) {
				continue
			}
		}
		if opts.Due != "" {
			if a.DueAt == nil {
				continue
			}
			switch opts.Due {
			case "today":
				if !a.DueAt.After(now) || a.DueAt.After(now.Add(24*time.Hour)) {
					continue
				}
			case "week":
				if !a.DueAt.After(now) || a.DueAt.After(now.Add(7*24*time.Hour)) {
					continue
				}
			}
		}
		filtered = append(filtered, a)
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, filtered)
	}

	if len(filtered) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No assignments found.")
		return nil
	}

	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("COURSE", "ASSIGNMENT", "DUE", "POINTS", "STATUS")

	for _, a := range filtered {
		status, style := assignmentStatus(a.Assignment, now, palette)

		due := "No due date"
		if a.DueAt != nil {
			due = cmdutil.RelativeTime(*a.DueAt)
		}

		points := "-"
		if a.PointsPossible != nil {
			points = fmt.Sprintf("%.0f", *a.PointsPossible)
		}

		tbl.AddStyledRow(
			cmdutil.StyledCell{Value: a.CourseName, Style: style},
			cmdutil.StyledCell{Value: a.Name, Style: style},
			cmdutil.StyledCell{Value: due, Style: style},
			cmdutil.StyledCell{Value: points, Style: style},
			cmdutil.StyledCell{Value: status, Style: style},
		)
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return tbl.Render()
}

// assignmentStatus determines the display status and color for an assignment.
func assignmentStatus(a canvas.Assignment, now time.Time, palette *cmdutil.Palette) (string, lipgloss.Style) {
	sub := a.Submission

	// 1. Excused
	if sub != nil && sub.Excused {
		return "Excused", palette.Muted
	}
	// 2. Graded
	if sub != nil && sub.Grade != nil && sub.PostedAt != nil {
		return "Graded", palette.Graded
	}
	// 3. Submitted
	if sub != nil && sub.SubmittedAt != nil {
		return "Submitted", palette.Submitted
	}
	// 4. Missing
	if a.Missing || (sub != nil && sub.Missing) {
		return "Missing", palette.Overdue
	}
	// 5. Overdue
	if a.DueAt != nil && a.DueAt.Before(now) {
		return "Overdue", palette.Overdue
	}
	// 6. Due today (within 24h)
	if a.DueAt != nil && a.DueAt.Before(now.Add(24*time.Hour)) {
		return "Due today", palette.DueToday
	}
	// 7. Default
	return "Upcoming", palette.Neutral
}

// isSubmitted returns true if the assignment has been submitted or graded.
// Canvas quizzes taken in-browser may not set SubmittedAt, so we also
// check for a grade or a "graded" workflow state.
func isSubmitted(a canvas.Assignment) bool {
	if a.Submission == nil {
		return false
	}
	s := a.Submission
	return s.SubmittedAt != nil || s.Grade != nil || s.WorkflowState == "graded"
}

// buildCourseMap creates a lookup from courseID to course code.
func buildCourseMap(ctx context.Context, client *canvas.Client) (map[int64]string, error) {
	names := make(map[int64]string)
	for c, err := range canvas.ListCourses(ctx, client, canvas.CourseListOptions{
		EnrollmentState: "active",
	}) {
		if err != nil {
			return nil, fmt.Errorf("listing courses: %w", err)
		}
		names[c.ID] = c.CourseCode
	}
	return names, nil
}

// printField writes a labeled field to output (shared between assignment views).
func printField(ios *iostreams.IOStreams, palette *cmdutil.Palette, label, value string) {
	_, _ = fmt.Fprintf(ios.Out, "  %s  %s\n",
		palette.Muted.Render(fmt.Sprintf("%-14s", label)),
		value,
	)
}
