package mcp

import (
	"context"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/canvas"
)

func (s *Server) registerCourseTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_courses",
			mcplib.WithDescription("List enrolled Canvas courses with current grades. Returns course name, code, term, and enrollment grades."),
			mcplib.WithBoolean("include_completed",
				mcplib.Description("Include completed/past courses (default: active only)"),
			),
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
}

type listCoursesArgs struct {
	IncludeCompleted bool `json:"include_completed"`
}

func (s *Server) handleListCourses(ctx context.Context, _ mcplib.CallToolRequest, args listCoursesArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	gqlOpts := canvas.GraphQLCourseListOptions{All: args.IncludeCompleted}
	courses, gqlErr := canvas.QueryCourseSummariesGraphQL(ctx, client, gqlOpts)
	if canvas.IsGraphQLFallback(gqlErr) {
		opts := canvas.CourseListOptions{
			Include: []string{"enrollments", "total_scores"},
		}
		if !args.IncludeCompleted {
			opts.EnrollmentState = "active"
		}
		courses, err = collectIter(canvas.ListCourses(ctx, client, opts))
		if err != nil {
			return toolError(err)
		}
	} else if gqlErr != nil {
		return toolError(gqlErr)
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

	return jsonResult(summaries)
}

type getCourseArgs struct {
	Course string `json:"course"`
}

func (s *Server) handleGetCourse(ctx context.Context, _ mcplib.CallToolRequest, args getCourseArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}

	full, err := canvas.GetCourse(ctx, client, course.ID, []string{"syllabus_body", "teachers", "enrollments", "total_scores"})
	if err != nil {
		return toolError(err)
	}

	type courseDetail struct {
		ID           int64    `json:"id"`
		Name         string   `json:"name"`
		CourseCode   string   `json:"course_code"`
		Teachers     []string `json:"teachers,omitempty"`
		Syllabus     string   `json:"syllabus,omitempty"`
		CurrentScore *float64 `json:"current_score,omitempty"`
		CurrentGrade *string  `json:"current_grade,omitempty"`
		HTMLURL      string   `json:"html_url"`
	}

	detail := courseDetail{
		ID:         full.ID,
		Name:       full.Name,
		CourseCode: full.CourseCode,
		HTMLURL:    full.HTMLURL,
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

	return jsonResult(detail)
}
