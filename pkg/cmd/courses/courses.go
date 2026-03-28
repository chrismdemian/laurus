// Package courses implements the courses command group.
package courses

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdCourses returns the courses list command.
func NewCmdCourses(f *cmdutil.Factory) *cobra.Command {
	var opts listOpts

	cmd := &cobra.Command{
		Use:   "courses",
		Short: "List your Canvas courses",
		Long:  "Display enrolled courses with grades and status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listRun(f, opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.All, "all", "a", false, "Include concluded/past courses")
	cmd.Flags().BoolVarP(&opts.Favorites, "favorites", "f", false, "Show only favorited courses")

	return cmd
}

// NewCmdCourse returns the top-level singular "course" alias for viewing a specific course.
func NewCmdCourse(f *cmdutil.Factory) *cobra.Command {
	var syllabus bool

	cmd := &cobra.Command{
		Use:   "course <name>",
		Short: "View a specific course",
		Long:  "Show detailed information about a course. Accepts course code, name, or ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return ViewCourse(f, args[0], syllabus)
		},
	}

	cmd.Flags().BoolVarP(&syllabus, "syllabus", "s", false, "Show only the syllabus")

	return cmd
}

type listOpts struct {
	All       bool
	Favorites bool
}

func listRun(f *cmdutil.Factory, opts listOpts) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()

	listOpts := canvas.CourseListOptions{
		Include:       []string{"enrollments", "total_scores"},
		FavoritesOnly: opts.Favorites,
	}
	if opts.All {
		listOpts.State = []string{"available", "completed"}
	} else {
		listOpts.EnrollmentState = "active"
	}

	var courses []canvas.Course
	for c, err := range canvas.ListCourses(context.Background(), client, listOpts) {
		if err != nil {
			return fmt.Errorf("listing courses: %w", err)
		}
		courses = append(courses, c)
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, courses)
	}

	if len(courses) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No courses found.")
		return nil
	}

	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("COURSE", "CODE", "GRADE", "STATUS")

	for _, c := range courses {
		grade := "-"
		if e := findStudentEnrollment(c.Enrollments); e != nil && e.Grades != nil {
			if e.Grades.CurrentScore != nil {
				if e.Grades.CurrentGrade != nil {
					grade = fmt.Sprintf("%.1f%% (%s)", *e.Grades.CurrentScore, *e.Grades.CurrentGrade)
				} else {
					grade = fmt.Sprintf("%.1f%%", *e.Grades.CurrentScore)
				}
			}
		}

		status := c.WorkflowState
		style := palette.CourseStateStyle(c.WorkflowState)

		tbl.AddStyledRow(
			cmdutil.StyledCell{Value: c.Name, Style: style},
			cmdutil.StyledCell{Value: c.CourseCode, Style: style},
			cmdutil.StyledCell{Value: grade, Style: style},
			cmdutil.StyledCell{Value: status, Style: style},
		)
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return tbl.Render()
}

func findStudentEnrollment(enrollments []canvas.Enrollment) *canvas.Enrollment {
	for i := range enrollments {
		// Canvas returns "StudentEnrollment" from the enrollments endpoint
		// but "student" from the courses endpoint with include[]=enrollments
		t := enrollments[i].Type
		if t == "StudentEnrollment" || t == "student" {
			return &enrollments[i]
		}
	}
	if len(enrollments) > 0 {
		return &enrollments[0]
	}
	return nil
}
