package mcp

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/syncer"
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
			freshArg(),
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
			freshArg(),
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
	Fresh  bool   `json:"fresh"`
}

// bucketMatches applies Canvas's bucket filter names to a cached assignment
// the way the server would (approximately; the server also considers
// lock dates for "past").
func bucketMatches(a canvas.Assignment, bucket string) bool {
	st := assignmentStatus(a)
	now := time.Now()
	switch strings.ToLower(strings.TrimSpace(bucket)) {
	case "":
		return true
	case "upcoming", "future":
		return st == "upcoming"
	case "overdue":
		return st == "overdue" || st == "missing"
	case "unsubmitted":
		return st == "upcoming" || st == "overdue" || st == "missing"
	case "past":
		return a.DueAt != nil && a.DueAt.Before(now)
	case "undated":
		return a.DueAt == nil
	case "ungraded":
		return st == "submitted"
	default:
		return st == strings.ToLower(bucket)
	}
}

// list_assignments is cache-first on the 5-minute tier (submissions carry
// scores). One course reads that course's cached assignments; no course
// reads every active course's.
func (s *Server) handleListAssignments(ctx context.Context, _ mcplib.CallToolRequest, args listAssignmentsArgs) (*mcplib.CallToolResult, error) {
	var courses []canvas.Course
	if args.Course != "" {
		course, err := s.findCourse(ctx, args.Course)
		if err != nil {
			return toolError(err)
		}
		courses = []canvas.Course{course}
	} else {
		all, _, err := s.cachedCourses(ctx, tierIdentity, false)
		if err != nil {
			return toolError(err)
		}
		courses = syncer.ActiveCourses(all)
	}

	var results []assignmentSummary
	env := envelope{Source: sourceCache}
	var oldest time.Time
	var notes []string
	for _, c := range courses {
		var assignments []canvas.Assignment
		cenv, err := s.read(ctx, readSpec{rt: cache.ResourceAssignments, courseID: c.ID, ttl: tierGrade, fresh: args.Fresh}, &assignments, func() error {
			client, err := s.getClient()
			if err != nil {
				return err
			}
			got, err := collectIter(canvas.ListAssignments(ctx, client, c.ID, canvas.ListAssignmentsOptions{Include: []string{"submission"}}))
			assignments = got
			return err
		})
		if err != nil {
			if len(courses) == 1 {
				return toolError(err)
			}
			notes = append(notes, fmt.Sprintf("%s: %v", c.CourseCode, err))
			continue
		}
		if cenv.Stale {
			env.Stale = true
		}
		if cenv.SyncError != "" {
			notes = append(notes, fmt.Sprintf("%s: %s", c.CourseCode, cenv.SyncError))
		}
		if cenv.Source == sourceLive {
			env.Source = sourceLive
		}
		if oldest.IsZero() || cenv.AsOf.Before(oldest) {
			oldest = cenv.AsOf
		}
		for _, a := range assignments {
			if bucketMatches(a, args.Status) {
				results = append(results, toAssignmentSummary(a, c.CourseCode, c.ID))
			}
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

	env.AsOf = oldest
	if oldest.IsZero() {
		env.AsOf = time.Now().UTC()
	}
	if len(notes) > 0 {
		env.SyncError = strings.Join(notes, "; ")
	}
	env.Data = results
	return jsonResult(env)
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

	return liveEmpty("No upcoming assignments found.")
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
		return liveEmpty("No overdue or missing assignments.")
	}

	return liveResult(results)
}

type getAssignmentArgs struct {
	Course     string `json:"course"`
	Assignment string `json:"assignment"`
	Fresh      bool   `json:"fresh"`
}

// get_assignment serves the cached assignment (5-minute tier; the sync
// includes the submission). A name that does not match the cache falls
// back to the live resolver.
func (s *Server) handleGetAssignment(ctx context.Context, _ mcplib.CallToolRequest, args getAssignmentArgs) (*mcplib.CallToolResult, error) {
	course, err := s.findCourse(ctx, args.Course)
	if err != nil {
		return toolError(err)
	}

	var assignments []canvas.Assignment
	env, err := s.read(ctx, readSpec{rt: cache.ResourceAssignments, courseID: course.ID, ttl: tierGrade, fresh: args.Fresh}, &assignments, func() error {
		client, err := s.getClient()
		if err != nil {
			return err
		}
		got, err := collectIter(canvas.ListAssignments(ctx, client, course.ID, canvas.ListAssignmentsOptions{Include: []string{"submission"}}))
		assignments = got
		return err
	})
	if err != nil {
		return toolError(err)
	}

	full, ok := matchAssignment(assignments, args.Assignment)
	if !ok {
		client, err := s.getClient()
		if err != nil {
			return toolError(err)
		}
		found, err := canvas.FindAssignment(ctx, client, course.ID, args.Assignment)
		if err != nil {
			return toolError(err)
		}
		full, err = canvas.GetAssignment(ctx, client, course.ID, found.ID, []string{"submission"})
		if err != nil {
			return toolError(err)
		}
		env = envelope{AsOf: time.Now().UTC(), Source: sourceLive}
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

	env.Data = detail
	return jsonResult(env)
}

// matchAssignment mirrors canvas.FindAssignment's precedence over a cached
// list: numeric ID, exact name, then name substring (case-insensitive).
func matchAssignment(assignments []canvas.Assignment, query string) (canvas.Assignment, bool) {
	if id, err := strconv.ParseInt(strings.TrimSpace(query), 10, 64); err == nil {
		for _, a := range assignments {
			if a.ID == id {
				return a, true
			}
		}
	}
	q := strings.ToLower(strings.TrimSpace(query))
	for _, a := range assignments {
		if strings.EqualFold(a.Name, query) {
			return a, true
		}
	}
	for _, a := range assignments {
		if strings.Contains(strings.ToLower(a.Name), q) {
			return a, true
		}
	}
	return canvas.Assignment{}, false
}
