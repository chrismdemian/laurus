package assignments

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/render"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdAssignment returns the top-level singular "assignment" alias for viewing details.
func NewCmdAssignment(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assignment <course> <name>",
		Short: "View details of a specific assignment",
		Long:  "Show full details including description, rubric, grade, and comments.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return viewRun(f, args[0], args[1])
		},
	}
	return cmd
}

func viewRun(f *cmdutil.Factory, courseQuery, assignmentQuery string) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	// Resolve course
	course, err := canvas.FindCourse(ctx, client, courseQuery)
	if err != nil {
		return fmt.Errorf("finding course %q: %w", courseQuery, err)
	}

	// Resolve assignment
	assignment, err := canvas.FindAssignment(ctx, client, course.ID, assignmentQuery)
	if err != nil {
		return fmt.Errorf("finding assignment %q: %w", assignmentQuery, err)
	}

	// Re-fetch with full includes
	assignment, err = canvas.GetAssignment(ctx, client, course.ID, assignment.ID,
		[]string{"submission", "all_dates", "score_statistics"})
	if err != nil {
		return fmt.Errorf("fetching assignment details: %w", err)
	}

	// Fetch submission with comments and rubric
	var subPtr *canvas.Submission
	sub, err := canvas.GetSubmission(ctx, client, course.ID, assignment.ID,
		[]string{"submission_comments", "rubric_assessment"})
	if err != nil && !errors.Is(err, canvas.ErrNotFound) {
		return fmt.Errorf("fetching submission: %w", err)
	}
	if err == nil {
		subPtr = &sub
	}

	if ios.IsJSON {
		data := struct {
			Assignment canvas.Assignment  `json:"assignment"`
			Submission *canvas.Submission `json:"submission,omitempty"`
		}{
			Assignment: assignment,
			Submission: subPtr,
		}
		return cmdutil.RenderJSON(ios, data)
	}

	return renderAssignmentDetail(f, course, assignment, subPtr)
}

func renderAssignmentDetail(f *cmdutil.Factory, course canvas.Course, a canvas.Assignment, sub *canvas.Submission) error {
	ios := f.IOStreams()
	palette := cmdutil.NewPalette(ios)
	now := time.Now()

	_ = ios.StartPager()
	defer ios.StopPager()

	// Header
	_, _ = fmt.Fprintln(ios.Out, palette.Header.Render(a.Name))

	// Basic fields
	printField(ios, palette, "Course", course.CourseCode)
	printField(ios, palette, "Due", cmdutil.FormatDueDate(a.DueAt, nil))

	if a.PointsPossible != nil {
		printField(ios, palette, "Points", fmt.Sprintf("%.0f", *a.PointsPossible))
	}

	if len(a.SubmissionTypes) > 0 {
		printField(ios, palette, "Submission", strings.Join(a.SubmissionTypes, ", "))
	}

	// Status
	status, _ := assignmentStatus(a, now, palette)
	printField(ios, palette, "Status", status)

	// Grade (from submission)
	if sub != nil && sub.Score != nil {
		grade := fmt.Sprintf("%.1f", *sub.Score)
		if a.PointsPossible != nil && *a.PointsPossible > 0 {
			pct := *sub.Score / *a.PointsPossible * 100
			grade += fmt.Sprintf("/%.0f (%.1f%%)", *a.PointsPossible, pct)
		}
		if sub.Grade != nil && *sub.Grade != fmt.Sprintf("%.1f", *sub.Score) {
			grade += fmt.Sprintf(" [%s]", *sub.Grade)
		}
		printField(ios, palette, "Grade", grade)
	}

	if a.HTMLURL != "" {
		printField(ios, palette, "URL", a.HTMLURL)
	}

	// Description
	if a.Description != nil && strings.TrimSpace(*a.Description) != "" {
		_, _ = fmt.Fprintln(ios.Out)
		_, _ = fmt.Fprintln(ios.Out, palette.Muted.Render("  --- Description ---"))
		rendered, err := render.CanvasHTML(*a.Description, ios.TerminalWidth()-4)
		if err != nil {
			_, _ = fmt.Fprintln(ios.Out, *a.Description)
		} else {
			_, _ = fmt.Fprint(ios.Out, rendered)
		}
	}

	// Rubric
	if len(a.Rubric) > 0 {
		_, _ = fmt.Fprintln(ios.Out)
		_, _ = fmt.Fprintln(ios.Out, palette.Muted.Render("  --- Rubric ---"))
		for _, criterion := range a.Rubric {
			_, _ = fmt.Fprintf(ios.Out, "  %s (%.0f pts)\n",
				palette.Header.Render(criterion.Description), criterion.Points)
			if criterion.LongDescription != "" {
				_, _ = fmt.Fprintf(ios.Out, "    %s\n", criterion.LongDescription)
			}
			// Show rubric assessment if available
			if sub != nil {
				if assessment, ok := sub.RubricAssessment[criterion.ID]; ok {
					if assessment.Points != nil {
						_, _ = fmt.Fprintf(ios.Out, "    Score: %.1f/%.0f\n", *assessment.Points, criterion.Points)
					}
					if assessment.Comments != nil && *assessment.Comments != "" {
						_, _ = fmt.Fprintf(ios.Out, "    Comment: %s\n", *assessment.Comments)
					}
				}
			}
		}
	}

	// Submission comments
	if sub != nil && len(sub.SubmissionComments) > 0 {
		_, _ = fmt.Fprintln(ios.Out)
		_, _ = fmt.Fprintln(ios.Out, palette.Muted.Render("  --- Comments ---"))
		for _, c := range sub.SubmissionComments {
			_, _ = fmt.Fprintf(ios.Out, "  %s (%s):  %s\n",
				palette.Header.Render(c.Author),
				c.CreatedAt.Format("Jan 2"),
				c.Comment,
			)
		}
	}

	return nil
}
