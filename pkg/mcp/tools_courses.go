package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/syncer"
)

func (s *Server) registerCourseTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_courses",
			mcplib.WithDescription("List enrolled Canvas courses with current grades. Returns course name, code, term, and enrollment grades."),
			mcplib.WithBoolean("include_completed",
				mcplib.Description("Include completed/past courses (default: active only)"),
			),
			freshArg(),
		),
		mcplib.NewTypedToolHandler(s.handleListCourses),
	)

	srv.AddTool(
		mcplib.NewTool("get_course",
			mcplib.WithDescription("Get details for a single Canvas course including syllabus and teachers."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID (e.g. \"CSC108\", \"csc108\", or \"12345\")"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleGetCourse),
	)

	srv.AddTool(
		mcplib.NewTool("get_front_page",
			mcplib.WithDescription("Get a course's front page. Courses whose default_view is \"wiki\" keep their syllabus, schedule and reading list here rather than in the syllabus, and the front page is readable even when the Pages tab is disabled."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleGetFrontPage),
	)
}

type listCoursesArgs struct {
	IncludeCompleted bool `json:"include_completed"`
	Fresh            bool `json:"fresh"`
}

// list_courses carries grades, so it sits on the 5-minute tier even though
// course identity would be fine for a day.
func (s *Server) handleListCourses(ctx context.Context, _ mcplib.CallToolRequest, args listCoursesArgs) (*mcplib.CallToolResult, error) {
	courses, env, err := s.cachedCourses(ctx, tierGrade, args.Fresh)
	if err != nil {
		return toolError(err)
	}
	if !args.IncludeCompleted {
		courses = syncer.ActiveCourses(courses)
	}

	type courseSummary struct {
		ID           int64    `json:"id"`
		Name         string   `json:"name"`
		CourseCode   string   `json:"course_code"`
		CurrentScore *float64 `json:"current_score,omitempty"`
		CurrentGrade *string  `json:"current_grade,omitempty"`
		FinalScore   *float64 `json:"final_score,omitempty"`
		FinalGrade   *string  `json:"final_grade,omitempty"`
	}

	summaries := make([]courseSummary, 0, len(courses))
	for _, c := range courses {
		cs := courseSummary{
			ID:         c.ID,
			Name:       c.Name,
			CourseCode: c.CourseCode,
		}
		if len(c.Enrollments) > 0 {
			e := c.Enrollments[0]
			if e.Grades != nil {
				cs.CurrentScore = e.Grades.CurrentScore
				cs.CurrentGrade = e.Grades.CurrentGrade
				cs.FinalScore = e.Grades.FinalScore
				cs.FinalGrade = e.Grades.FinalGrade
			}
			if e.ComputedCurrentScore != nil {
				cs.CurrentScore = e.ComputedCurrentScore
				cs.CurrentGrade = e.ComputedCurrentGrade
				cs.FinalScore = e.ComputedFinalScore
				cs.FinalGrade = e.ComputedFinalGrade
			}
		}
		summaries = append(summaries, cs)
	}

	env.Data = summaries
	return jsonResult(env)
}

type getCourseArgs struct {
	Course string `json:"course"`
}

func (s *Server) handleGetCourse(ctx context.Context, _ mcplib.CallToolRequest, args getCourseArgs) (*mcplib.CallToolResult, error) {
	course, err := s.findCourse(ctx, args.Course)
	if err != nil {
		return toolError(err)
	}

	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}
	full, err := canvas.GetCourse(ctx, client, course.ID, []string{"syllabus_body", "teachers", "enrollments", "total_scores"})
	if err != nil {
		return toolError(err)
	}

	type courseDetail struct {
		ID           int64          `json:"id"`
		Name         string         `json:"name"`
		CourseCode   string         `json:"course_code"`
		DefaultView  string         `json:"default_view,omitempty"`
		Teachers     []string       `json:"teachers,omitempty"`
		Syllabus     string         `json:"syllabus,omitempty"`
		FrontPage    *frontPageData `json:"front_page,omitempty"`
		CurrentScore *float64       `json:"current_score,omitempty"`
		CurrentGrade *string        `json:"current_grade,omitempty"`
		HTMLURL      string         `json:"html_url"`
	}

	detail := courseDetail{
		ID:          full.ID,
		Name:        full.Name,
		CourseCode:  full.CourseCode,
		DefaultView: full.DefaultView,
		HTMLURL:     full.HTMLURL,
	}

	// A wiki course keeps its key information on the front page, but the rest
	// of get_course does not depend on it: a course with no front page, or a
	// front page this call could not read, degrades to a note on the envelope.
	fp, fpErr := frontPage(ctx, client, full.ID)
	detail.FrontPage = fp
	note := ""
	if fpErr != nil {
		note = "front page could not be read: " + unwrapMessage(fpErr)
	}

	for _, t := range full.Teachers {
		detail.Teachers = append(detail.Teachers, t.Name)
	}
	if full.SyllabusBody != nil && *full.SyllabusBody != "" {
		detail.Syllabus = htmlToMarkdown(*full.SyllabusBody)
	}
	if len(full.Enrollments) > 0 {
		e := full.Enrollments[0]
		if e.Grades != nil {
			detail.CurrentScore = e.Grades.CurrentScore
			detail.CurrentGrade = e.Grades.CurrentGrade
		}
		if e.ComputedCurrentScore != nil {
			detail.CurrentScore = e.ComputedCurrentScore
			detail.CurrentGrade = e.ComputedCurrentGrade
		}
	}

	if note != "" {
		return jsonResult(envelope{AsOf: time.Now().UTC(), Source: sourceLive, Note: note, Data: detail})
	}
	return liveResult(detail)
}

// frontPageData is the front page as served to MCP callers: markdown, not HTML.
type frontPageData struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// frontPage fetches a course front page. A course with no front page yields
// (nil, nil); anything else that went wrong yields the error, for the caller
// to report without failing the tool.
func frontPage(ctx context.Context, client *canvas.Client, courseID int64) (*frontPageData, error) {
	page, err := canvas.GetFrontPage(ctx, client, courseID)
	if err != nil {
		if errors.Is(err, canvas.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	body := ""
	if page.Body != nil && strings.TrimSpace(*page.Body) != "" {
		body = htmlToMarkdown(*page.Body)
	}
	return &frontPageData{Title: page.Title, Body: body}, nil
}

type getFrontPageArgs struct {
	Course string `json:"course"`
}

func (s *Server) handleGetFrontPage(ctx context.Context, _ mcplib.CallToolRequest, args getFrontPageArgs) (*mcplib.CallToolResult, error) {
	course, err := s.findCourse(ctx, args.Course)
	if err != nil {
		return toolError(err)
	}

	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	// Report a missing front page through the error path, the way get_page
	// reports a missing page: the caller asked for this one thing.
	page, err := canvas.GetFrontPage(ctx, client, course.ID)
	if err != nil {
		return toolError(err)
	}

	body := ""
	if page.Body != nil && strings.TrimSpace(*page.Body) != "" {
		body = htmlToMarkdown(*page.Body)
	}

	return liveResult(struct {
		CourseID int64  `json:"course_id"`
		PageID   int64  `json:"page_id"`
		Title    string `json:"title"`
		URL      string `json:"url,omitempty"`
		HTMLURL  string `json:"html_url,omitempty"`
		Body     string `json:"body"`
	}{
		CourseID: course.ID,
		PageID:   page.PageID,
		Title:    page.Title,
		URL:      page.URL,
		HTMLURL:  page.HTMLURL,
		Body:     body,
	})
}
