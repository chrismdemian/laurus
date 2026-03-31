package mcp

import (
	"context"
	"sort"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/canvas"
)

func (s *Server) registerAssignmentTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_assignments",
			mcplib.WithDescription("List assignments with due dates and submission status. If no course is specified, lists assignments across all courses."),
			mcplib.WithString("course",
				mcplib.Description("Course name, code, or ID to filter by (optional)"),
			),
			mcplib.WithString("status",
				mcplib.Description("Filter by status"),
				mcplib.Enum("upcoming", "overdue", "past", "undated"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleListAssignments),
	)

	srv.AddTool(
		mcplib.NewTool("get_next_assignment",
			mcplib.WithDescription("Get the single next upcoming assignment due across all courses."),
		),
		mcplib.NewTypedToolHandler(s.handleGetNextAssignment),
	)

	srv.AddTool(
		mcplib.NewTool("list_overdue",
			mcplib.WithDescription("List all overdue and missing assignments across all courses."),
		),
		mcplib.NewTypedToolHandler(s.handleListOverdue),
	)

	srv.AddTool(
		mcplib.NewTool("get_assignment",
			mcplib.WithDescription("Get full details for a specific assignment including description and submission status."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
			mcplib.WithString("assignment",
				mcplib.Required(),
				mcplib.Description("Assignment name or ID"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleGetAssignment),
	)
}

type assignmentSummary struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	CourseName     string     `json:"course_name"`
	CourseID       int64      `json:"course_id"`
	DueAt          *time.Time `json:"due_at,omitempty"`
	PointsPossible *float64   `json:"points_possible,omitempty"`
	Score          *float64   `json:"score,omitempty"`
	Grade          *string    `json:"grade,omitempty"`
	Status         string     `json:"status"`
	HTMLURL        string     `json:"html_url"`
}

func assignmentStatus(a canvas.Assignment) string {
	sub := a.Submission
	if sub != nil && sub.Excused {
		return "excused"
	}
	if a.Missing || (sub != nil && sub.Missing) {
		return "missing"
	}
	if sub != nil && sub.Score != nil {
		return "graded"
	}
	if sub != nil && (sub.SubmittedAt != nil || sub.WorkflowState == "graded" || sub.Grade != nil) {
		return "submitted"
	}
	if a.DueAt != nil && a.DueAt.Before(time.Now()) {
		return "overdue"
	}
	return "upcoming"
}

func toAssignmentSummary(a canvas.Assignment, courseName string, courseID int64) assignmentSummary {
	as := assignmentSummary{
		ID:             a.ID,
		Name:           a.Name,
		CourseName:     courseName,
		CourseID:       courseID,
		DueAt:          a.DueAt,
		PointsPossible: a.PointsPossible,
		Status:         assignmentStatus(a),
		HTMLURL:        a.HTMLURL,
	}
	if a.Submission != nil {
		as.Score = a.Submission.Score
		as.Grade = a.Submission.Grade
	}
	return as
}

type listAssignmentsArgs struct {
	Course string `json:"course"`
	Status string `json:"status"`
}

func (s *Server) handleListAssignments(ctx context.Context, _ mcplib.CallToolRequest, args listAssignmentsArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	var results []assignmentSummary

	if args.Course != "" {
		// Single course path
		course, err := canvas.FindCourse(ctx, client, args.Course)
		if err != nil {
			return toolError(err)
		}
		opts := canvas.ListAssignmentsOptions{
			Include: []string{"submission"},
			Bucket:  args.Status,
		}
		assignments, err := collectIter(canvas.ListAssignments(ctx, client, course.ID, opts))
		if err != nil {
			return toolError(err)
		}
		for _, a := range assignments {
			results = append(results, toAssignmentSummary(a, course.CourseCode, course.ID))
		}
	} else {
		// All courses — try GraphQL first
		dashboardCourses, gqlErr := canvas.QueryDashboardAssignmentsGraphQL(ctx, client)
		if gqlErr == nil {
			for _, dc := range dashboardCourses {
				for _, a := range dc.Assignments {
					results = append(results, toAssignmentSummary(a, dc.Course.CourseCode, dc.Course.ID))
				}
			}
		} else {
			if !canvas.IsGraphQLFallback(gqlErr) {
				return toolError(gqlErr)
			}
			// REST fallback
			courses, err := collectIter(canvas.ListCourses(ctx, client, canvas.CourseListOptions{
				EnrollmentState: "active",
			}))
			if err != nil {
				return toolError(err)
			}
			for _, c := range courses {
				assignments, err := collectIter(canvas.ListAssignments(ctx, client, c.ID, canvas.ListAssignmentsOptions{
					Include: []string{"submission"},
					Bucket:  args.Status,
				}))
				if err != nil {
					continue
				}
				for _, a := range assignments {
					results = append(results, toAssignmentSummary(a, c.CourseCode, c.ID))
				}
			}
		}

		// Apply status filter if GraphQL path was used (it doesn't support bucket)
		if args.Status != "" && gqlErr == nil {
			filtered := results[:0]
			for _, r := range results {
				if matchesStatus(r.Status, args.Status) {
					filtered = append(filtered, r)
				}
			}
			results = filtered
		}
	}

	// Sort by due date
	sort.Slice(results, func(i, j int) bool {
		if results[i].DueAt == nil {
			return false
		}
		if results[j].DueAt == nil {
			return true
		}
		return results[i].DueAt.Before(*results[j].DueAt)
	})

	return jsonResult(results)
}

func matchesStatus(actual, filter string) bool {
	switch filter {
	case "upcoming":
		return actual == "upcoming"
	case "overdue":
		return actual == "overdue" || actual == "missing"
	case "past":
		return actual == "graded" || actual == "submitted"
	case "undated":
		return true // can't filter locally, return all
	}
	return true
}

type getNextAssignmentArgs struct{}

func (s *Server) handleGetNextAssignment(ctx context.Context, _ mcplib.CallToolRequest, _ getNextAssignmentArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	events, err := canvas.ListUpcomingEvents(ctx, client)
	if err != nil {
		return toolError(err)
	}

	for _, e := range events {
		if e.Assignment != nil {
			a := e.Assignment
			result := struct {
				Name           string     `json:"name"`
				CourseName     string     `json:"course_name,omitempty"`
				DueAt          *time.Time `json:"due_at,omitempty"`
				PointsPossible *float64   `json:"points_possible,omitempty"`
				HTMLURL        string     `json:"html_url"`
			}{
				Name:           a.Name,
				DueAt:          a.DueAt,
				PointsPossible: a.PointsPossible,
				HTMLURL:        e.HTMLURL,
			}
			return jsonResult(result)
		}
	}

	return mcplib.NewToolResultText("No upcoming assignments found."), nil
}

type listOverdueArgs struct{}

func (s *Server) handleListOverdue(ctx context.Context, _ mcplib.CallToolRequest, _ listOverdueArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	assignments, err := collectIter(canvas.ListMissingSubmissions(ctx, client, []string{"course"}))
	if err != nil {
		return toolError(err)
	}

	type overdue struct {
		ID             int64      `json:"id"`
		Name           string     `json:"name"`
		CourseID       int64      `json:"course_id"`
		DueAt          *time.Time `json:"due_at,omitempty"`
		PointsPossible *float64   `json:"points_possible,omitempty"`
		HTMLURL        string     `json:"html_url"`
	}

	results := make([]overdue, 0, len(assignments))
	for _, a := range assignments {
		results = append(results, overdue{
			ID:             a.ID,
			Name:           a.Name,
			CourseID:       a.CourseID,
			DueAt:          a.DueAt,
			PointsPossible: a.PointsPossible,
			HTMLURL:        a.HTMLURL,
		})
	}

	if len(results) == 0 {
		return mcplib.NewToolResultText("No overdue or missing assignments."), nil
	}

	return jsonResult(results)
}

type getAssignmentArgs struct {
	Course     string `json:"course"`
	Assignment string `json:"assignment"`
}

func (s *Server) handleGetAssignment(ctx context.Context, _ mcplib.CallToolRequest, args getAssignmentArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}

	assignment, err := canvas.FindAssignment(ctx, client, course.ID, args.Assignment)
	if err != nil {
		return toolError(err)
	}

	full, err := canvas.GetAssignment(ctx, client, course.ID, assignment.ID, []string{"submission"})
	if err != nil {
		return toolError(err)
	}

	type assignmentDetail struct {
		ID              int64      `json:"id"`
		Name            string     `json:"name"`
		CourseName      string     `json:"course_name"`
		DueAt           *time.Time `json:"due_at,omitempty"`
		LockAt          *time.Time `json:"lock_at,omitempty"`
		PointsPossible  *float64   `json:"points_possible,omitempty"`
		SubmissionTypes []string   `json:"submission_types"`
		Description     string     `json:"description,omitempty"`
		Status          string     `json:"status"`
		Score           *float64   `json:"score,omitempty"`
		Grade           *string    `json:"grade,omitempty"`
		HTMLURL         string     `json:"html_url"`
	}

	detail := assignmentDetail{
		ID:              full.ID,
		Name:            full.Name,
		CourseName:      course.CourseCode,
		DueAt:           full.DueAt,
		LockAt:          full.LockAt,
		PointsPossible:  full.PointsPossible,
		SubmissionTypes: full.SubmissionTypes,
		Status:          assignmentStatus(full),
		HTMLURL:         full.HTMLURL,
	}
	if full.Description != nil && *full.Description != "" {
		detail.Description = htmlToMarkdown(*full.Description)
	}
	if full.Submission != nil {
		detail.Score = full.Submission.Score
		detail.Grade = full.Submission.Grade
	}

	return jsonResult(detail)
}
