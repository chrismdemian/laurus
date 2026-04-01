// Package grades implements the grades command group.
package grades

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// NewCmdGrades returns the grades list command.
func NewCmdGrades(f *cmdutil.Factory) *cobra.Command {
	var opts listOpts

	cmd := &cobra.Command{
		Use:   "grades",
		Short: "Show grades for all courses",
		Long:  "Display current and final grades across all enrolled courses.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listRun(f, opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.All, "all", "a", false, "Include concluded/past courses")
	cmd.Flags().BoolVar(&opts.GPA, "gpa", false, "Compute GPA across all courses")

	return cmd
}

type listOpts struct {
	All bool
	GPA bool
}

func listRun(f *cmdutil.Factory, opts listOpts) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	listOpts := canvas.CourseListOptions{
		Include: []string{"enrollments", "total_scores"},
	}
	if opts.All {
		listOpts.State = []string{"available", "completed"}
	} else {
		listOpts.EnrollmentState = "active"
	}

	var courses []canvas.Course
	for c, err := range canvas.ListCourses(ctx, client, listOpts) {
		if err != nil {
			return fmt.Errorf("listing courses: %w", err)
		}
		courses = append(courses, c)
	}

	// Opportunistic cache write.
	if db, err := f.Cache(); err == nil {
		items := make([]cache.CacheItem, len(courses))
		for i, x := range courses {
			items[i] = cache.CacheItem{ID: x.ID, CourseID: 0, Data: x}
		}
		_ = db.UpsertMany(cache.ResourceCourses, items)
		_ = db.SetSyncMeta(cache.ResourceCourses, 0, len(items), "success")
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, courses)
	}

	if len(courses) == 0 {
		_, _ = fmt.Fprintln(ios.Out, "No courses found.")
		return nil
	}

	if opts.GPA {
		return renderGPA(ios, courses)
	}

	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("COURSE", "CODE", "CURRENT", "FINAL", "LETTER")

	for _, c := range courses {
		e := findStudentEnrollment(c.Enrollments)
		current := "-"
		final := "-"
		letter := "-"
		style := palette.Neutral

		if e != nil {
			cs, cl := enrollmentGrade(e)
			if cs != nil {
				current = fmt.Sprintf("%.1f%%", *cs)
				style = gradeStyle(palette, *cs)
			}
			fs, _ := enrollmentFinalGrade(e)
			if fs != nil {
				final = fmt.Sprintf("%.1f%%", *fs)
			}
			if cl != nil {
				letter = *cl
			}
		}

		tbl.AddStyledRow(
			cmdutil.StyledCell{Value: c.Name, Style: style},
			cmdutil.StyledCell{Value: c.CourseCode, Style: style},
			cmdutil.StyledCell{Value: current, Style: style},
			cmdutil.StyledCell{Value: final, Style: style},
			cmdutil.StyledCell{Value: letter, Style: style},
		)
	}

	_ = ios.StartPager()
	defer ios.StopPager()
	return tbl.Render()
}

// gpaScale maps common letter grades to GPA points (standard 4.0 scale).
var gpaScale = map[string]float64{
	"A+": 4.0, "A": 4.0, "A-": 3.7,
	"B+": 3.3, "B": 3.0, "B-": 2.7,
	"C+": 2.3, "C": 2.0, "C-": 1.7,
	"D+": 1.3, "D": 1.0, "D-": 0.7,
	"F": 0.0,
}

func renderGPA(ios *iostreams.IOStreams, courses []canvas.Course) error {
	palette := cmdutil.NewPalette(ios)
	tbl := cmdutil.NewTable(ios)
	tbl.AddHeader("COURSE", "LETTER", "GPA POINTS")

	totalPoints := 0.0
	count := 0

	for _, c := range courses {
		e := findStudentEnrollment(c.Enrollments)
		if e == nil {
			continue
		}
		_, cl := enrollmentGrade(e)
		if cl == nil {
			continue
		}
		pts, ok := gpaScale[*cl]
		if !ok {
			continue
		}
		count++
		totalPoints += pts

		style := gradeStyle(palette, pts/4.0*100)
		tbl.AddStyledRow(
			cmdutil.StyledCell{Value: c.Name, Style: style},
			cmdutil.StyledCell{Value: *cl, Style: style},
			cmdutil.StyledCell{Value: fmt.Sprintf("%.1f", pts), Style: style},
		)
	}

	_ = ios.StartPager()
	defer ios.StopPager()

	if err := tbl.Render(); err != nil {
		return err
	}

	if count > 0 {
		gpa := totalPoints / float64(count)
		_, _ = fmt.Fprintf(ios.Out, "\n  %s  %.2f / 4.00\n",
			palette.Header.Render("GPA"),
			gpa,
		)
	} else {
		_, _ = fmt.Fprintln(ios.Out, "No letter grades available for GPA calculation.")
	}

	return nil
}

func gradeStyle(palette *cmdutil.Palette, score float64) lipgloss.Style {
	switch {
	case score >= 80:
		return palette.Graded
	case score >= 60:
		return palette.DueToday
	default:
		return palette.Overdue
	}
}

func findStudentEnrollment(enrollments []canvas.Enrollment) *canvas.Enrollment {
	for i := range enrollments {
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

func enrollmentGrade(e *canvas.Enrollment) (score *float64, letter *string) {
	if e.ComputedCurrentScore != nil {
		return e.ComputedCurrentScore, e.ComputedCurrentGrade
	}
	if e.Grades != nil && e.Grades.CurrentScore != nil {
		return e.Grades.CurrentScore, e.Grades.CurrentGrade
	}
	return nil, nil
}

func enrollmentFinalGrade(e *canvas.Enrollment) (score *float64, letter *string) {
	if e.ComputedFinalScore != nil {
		return e.ComputedFinalScore, e.ComputedFinalGrade
	}
	if e.Grades != nil && e.Grades.FinalScore != nil {
		return e.Grades.FinalScore, e.Grades.FinalGrade
	}
	return nil, nil
}

func printField(ios *iostreams.IOStreams, palette *cmdutil.Palette, label, value string) {
	_, _ = fmt.Fprintf(ios.Out, "  %s  %s\n",
		palette.Muted.Render(fmt.Sprintf("%-14s", label)),
		value,
	)
}
