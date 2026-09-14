package courses

import (
	"context"
	"errors"
	"fmt"
	"strings"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/charmbracelet/glamour"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/internal/render"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// ViewCourse displays detailed information about a course.
// Exported so it can be called from the top-level "course" alias.
func ViewCourse(f *cmdutil.Factory, query string, syllabus, home bool) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	course, err := canvas.FindCourse(ctx, client, query)
	if err != nil {
		return fmt.Errorf("finding course %q: %w", query, err)
	}

	// Re-fetch with full includes
	course, err = canvas.GetCourse(ctx, client, course.ID,
		[]string{"syllabus_body", "teachers", "total_students", "enrollments", "total_scores"})
	if err != nil {
		return fmt.Errorf("fetching course details: %w", err)
	}

	// Courses whose default view is the wiki keep their key information on the
	// front page. --home asks for the front page and nothing else, so there a
	// failure is the command's failure; every other view treats an unreadable
	// front page as one it simply cannot show.
	if home && !ios.IsJSON {
		page, err := canvas.GetFrontPage(ctx, client, course.ID)
		if err != nil {
			if errors.Is(err, canvas.ErrNotFound) {
				return renderFrontPageOnly(f, course, nil)
			}
			return fmt.Errorf("fetching front page: %w", err)
		}
		return renderFrontPageOnly(f, course, &page)
	}

	frontPage := fetchFrontPage(ctx, client, course.ID, ios)

	if ios.IsJSON {
		out := courseJSON{Course: course}
		if frontPage != nil {
			body := ""
			if frontPage.Body != nil {
				body = *frontPage.Body
			}
			out.FrontPage = &frontPageJSON{Title: frontPage.Title, Body: body}
		}
		return cmdutil.RenderJSON(ios, out)
	}

	if syllabus {
		return renderSyllabus(f, course, frontPage)
	}

	return renderCourseDetail(f, course, frontPage)
}

// courseJSON is the --json shape: every course field, promoted from the
// embedded struct, plus the front page when the course has one.
type courseJSON struct {
	canvas.Course
	FrontPage *frontPageJSON `json:"front_page"`
}

type frontPageJSON struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// fetchFrontPage returns the course front page, or nil when there is none to
// show. The front page is a bonus in every view except --home, so no failure
// here fails the command: a course that has none stays silent, and anything
// else (a refusal, a rate limit, a timeout) leaves a note on stderr.
func fetchFrontPage(ctx context.Context, client *canvas.Client, courseID int64, ios *iostreams.IOStreams) *canvas.Page {
	page, err := canvas.GetFrontPage(ctx, client, courseID)
	if err != nil {
		if !errors.Is(err, canvas.ErrNotFound) {
			_, _ = fmt.Fprintf(ios.ErrOut, "Could not read the course front page: %v\n", err)
		}
		return nil
	}
	return &page
}

// frontPageBody returns the front page HTML if there is any to show.
func frontPageBody(page *canvas.Page) string {
	if page == nil || page.Body == nil || strings.TrimSpace(*page.Body) == "" {
		return ""
	}
	return *page.Body
}

// renderFrontPage writes the front page body through the Canvas HTML renderer,
// which keeps link targets visible so file links stay usable.
func renderFrontPage(f *cmdutil.Factory, body string) {
	ios := f.IOStreams()
	rendered, err := render.CanvasHTML(body, ios.TerminalWidth()-4)
	if err != nil {
		_, _ = fmt.Fprintln(ios.Out, body)
		return
	}
	_, _ = fmt.Fprint(ios.Out, rendered)
}

// renderFrontPageOnly backs `course --home`.
func renderFrontPageOnly(f *cmdutil.Factory, course canvas.Course, page *canvas.Page) error {
	ios := f.IOStreams()
	palette := cmdutil.NewPalette(ios)

	body := frontPageBody(page)
	if body == "" {
		view := course.DefaultView
		if view == "" {
			view = "unknown"
		}
		if page != nil {
			_, _ = fmt.Fprintln(ios.Out, "Front page is empty.")
			return nil
		}
		_, _ = fmt.Fprintf(ios.Out, "No front page (default view: %s).\n", view)
		return nil
	}

	_ = ios.StartPager()
	defer ios.StopPager()

	if page.Title != "" {
		_, _ = fmt.Fprintln(ios.Out, palette.Header.Render(page.Title))
	}
	renderFrontPage(f, body)
	return nil
}

func renderSyllabus(f *cmdutil.Factory, course canvas.Course, page *canvas.Page) error {
	ios := f.IOStreams()

	if course.SyllabusBody == nil || strings.TrimSpace(*course.SyllabusBody) == "" {
		// A wiki course keeps its syllabus on the front page instead.
		if body := frontPageBody(page); body != "" && course.DefaultView == "wiki" {
			_, _ = fmt.Fprintln(ios.ErrOut, "No syllabus page; showing the course front page instead.")
			_ = ios.StartPager()
			defer ios.StopPager()
			renderFrontPage(f, body)
			return nil
		}
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

func renderCourseDetail(f *cmdutil.Factory, course canvas.Course, page *canvas.Page) error {
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
	if course.DefaultView != "" {
		printField(ios, palette, "Default view", course.DefaultView)
	}

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
	if e := findStudentEnrollment(course.Enrollments); e != nil {
		score, letter := enrollmentGrade(e)
		if score != nil {
			_, _ = fmt.Fprintln(ios.Out)
			grade := fmt.Sprintf("%.1f%%", *score)
			if letter != nil {
				grade += fmt.Sprintf(" (%s)", *letter)
			}
			printField(ios, palette, "Current Grade", grade)
		}
	}

	if course.HTMLURL != "" {
		_, _ = fmt.Fprintln(ios.Out)
		printField(ios, palette, "URL", course.HTMLURL)
	}

	if body := frontPageBody(page); body != "" {
		_, _ = fmt.Fprintln(ios.Out)
		title := page.Title
		if title == "" {
			title = "Front page"
		}
		_, _ = fmt.Fprintln(ios.Out, palette.Header.Render(title))
		renderFrontPage(f, body)
	}

	return nil
}

func printField(ios *iostreams.IOStreams, palette *cmdutil.Palette, label, value string) {
	_, _ = fmt.Fprintf(ios.Out, "  %s  %s\n",
		palette.Muted.Render(fmt.Sprintf("%-14s", label)),
		value,
	)
}
