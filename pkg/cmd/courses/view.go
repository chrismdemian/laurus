package courses

import (
	"context"
	"fmt"
	"strings"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/charmbracelet/glamour"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// ViewCourse displays detailed information about a course.
// Exported so it can be called from the top-level "course" alias.
func ViewCourse(f *cmdutil.Factory, query string, syllabus bool) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()

	course, err := canvas.FindCourse(context.Background(), client, query)
	if err != nil {
		return fmt.Errorf("finding course %q: %w", query, err)
	}

	// Re-fetch with full includes
	course, err = canvas.GetCourse(context.Background(), client, course.ID,
		[]string{"syllabus_body", "teachers", "total_students", "enrollments", "total_scores"})
	if err != nil {
		return fmt.Errorf("fetching course details: %w", err)
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, course)
	}

	if syllabus {
		return renderSyllabus(f, course)
	}

	return renderCourseDetail(f, course)
}

func renderSyllabus(f *cmdutil.Factory, course canvas.Course) error {
	ios := f.IOStreams()

	if course.SyllabusBody == nil || strings.TrimSpace(*course.SyllabusBody) == "" {
		_, _ = fmt.Fprintln(ios.Out, "No syllabus available.")
		return nil
	}

	_ = ios.StartPager()
	defer ios.StopPager()

	// Canvas returns raw HTML — convert to Markdown first, then render for terminal
	md, err := htmltomd.ConvertString(*course.SyllabusBody)
	if err != nil {
		// Fall back to raw HTML if conversion fails
		_, _ = fmt.Fprintln(ios.Out, *course.SyllabusBody)
		return nil
	}

	rendered, err := glamour.Render(md, "auto")
	if err != nil {
		// Fall back to plain markdown if glamour fails
		_, _ = fmt.Fprintln(ios.Out, md)
		return nil
	}

	_, _ = fmt.Fprint(ios.Out, rendered)
	return nil
}

func renderCourseDetail(f *cmdutil.Factory, course canvas.Course) error {
	ios := f.IOStreams()
	palette := cmdutil.NewPalette(ios)

	_ = ios.StartPager()
	defer ios.StopPager()

	// Course name
	_, _ = fmt.Fprintln(ios.Out, palette.Header.Render(course.Name))
	if course.CourseCode != "" {
		printField(ios, palette, "Code", course.CourseCode)
	}
	printField(ios, palette, "Status", course.WorkflowState)

	// Filter out teachers with empty names (Canvas sometimes returns empty entries)
	var teacherNames []string
	for _, t := range course.Teachers {
		name := strings.TrimSpace(t.Name)
		if name != "" {
			teacherNames = append(teacherNames, name)
		}
	}
	if len(teacherNames) > 0 {
		printField(ios, palette, "Teachers", strings.Join(teacherNames, ", "))
	}

	if course.TotalStudents != nil {
		printField(ios, palette, "Students", fmt.Sprintf("%d", *course.TotalStudents))
	}

	if course.TimeZone != "" {
		printField(ios, palette, "Timezone", course.TimeZone)
	}

	if course.StartAt != nil {
		printField(ios, palette, "Started", course.StartAt.Format("Jan 2, 2006"))
	}
	if course.EndAt != nil {
		printField(ios, palette, "Ends", course.EndAt.Format("Jan 2, 2006"))
	}

	// Grade from enrollment
	if e := findStudentEnrollment(course.Enrollments); e != nil && e.Grades != nil {
		_, _ = fmt.Fprintln(ios.Out)
		g := e.Grades
		if g.CurrentScore != nil {
			grade := fmt.Sprintf("%.1f%%", *g.CurrentScore)
			if g.CurrentGrade != nil {
				grade += fmt.Sprintf(" (%s)", *g.CurrentGrade)
			}
			printField(ios, palette, "Current Grade", grade)
		}
		if g.FinalScore != nil {
			grade := fmt.Sprintf("%.1f%%", *g.FinalScore)
			if g.FinalGrade != nil {
				grade += fmt.Sprintf(" (%s)", *g.FinalGrade)
			}
			printField(ios, palette, "Final Grade", grade)
		}
	}

	if course.HTMLURL != "" {
		_, _ = fmt.Fprintln(ios.Out)
		printField(ios, palette, "URL", course.HTMLURL)
	}

	return nil
}

func printField(ios *iostreams.IOStreams, palette *cmdutil.Palette, label, value string) {
	_, _ = fmt.Fprintf(ios.Out, "  %s  %s\n",
		palette.Muted.Render(fmt.Sprintf("%-14s", label)),
		value,
	)
}
