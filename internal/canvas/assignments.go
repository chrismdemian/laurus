package canvas

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"strconv"
	"strings"
)

// ListAssignmentsOptions controls filtering for ListAssignments.
type ListAssignmentsOptions struct {
	// Bucket filters by assignment state: "upcoming", "overdue", "past", "undated".
	Bucket string

	// OrderBy controls sort order: "due_at", "name", "position".
	OrderBy string

	// SearchTerm filters assignments by name (server-side).
	SearchTerm string

	// Include specifies additional data: "submission", "all_dates", "score_statistics".
	Include []string
}

// ListSubmissionsOptions controls filtering for ListSubmissions.
type ListSubmissionsOptions struct {
	// AssignmentIDs limits to specific assignments.
	AssignmentIDs []int64

	// WorkflowState filters by state: "submitted", "unsubmitted", "graded", "pending_review".
	WorkflowState string

	// Include specifies additional data: "submission_comments", "rubric_assessment", "assignment".
	Include []string
}

// ListAssignments returns an iterator over assignments for a course.
func ListAssignments(ctx context.Context, c *Client, courseID int64, opts ListAssignmentsOptions) iter.Seq2[Assignment, error] {
	path := fmt.Sprintf("/api/v1/courses/%d/assignments", courseID)

	params := url.Values{}
	if opts.Bucket != "" {
		params.Set("bucket", opts.Bucket)
	}
	if opts.OrderBy != "" {
		params.Set("order_by", opts.OrderBy)
	}
	if opts.SearchTerm != "" {
		params.Set("search_term", opts.SearchTerm)
	}
	for _, inc := range opts.Include {
		params.Add("include[]", inc)
	}

	return Paginate[Assignment](ctx, c, path, params)
}

// GetAssignment retrieves a single assignment by ID with optional includes.
func GetAssignment(ctx context.Context, c *Client, courseID, assignmentID int64, include []string) (Assignment, error) {
	path := fmt.Sprintf("/api/v1/courses/%d/assignments/%d", courseID, assignmentID)

	params := url.Values{}
	for _, inc := range include {
		params.Add("include[]", inc)
	}

	return Get[Assignment](ctx, c, path, params)
}

// ListSubmissions returns an iterator over the current user's submissions for a course.
// Always requests student_ids[]=self to scope to the authenticated user.
func ListSubmissions(ctx context.Context, c *Client, courseID int64, opts ListSubmissionsOptions) iter.Seq2[Submission, error] {
	path := fmt.Sprintf("/api/v1/courses/%d/students/submissions", courseID)

	params := url.Values{}
	params.Add("student_ids[]", "self")
	for _, id := range opts.AssignmentIDs {
		params.Add("assignment_ids[]", strconv.FormatInt(id, 10))
	}
	if opts.WorkflowState != "" {
		params.Set("workflow_state", opts.WorkflowState)
	}
	for _, inc := range opts.Include {
		params.Add("include[]", inc)
	}

	return Paginate[Submission](ctx, c, path, params)
}

// GetSubmission retrieves the current user's submission for a specific assignment.
func GetSubmission(ctx context.Context, c *Client, courseID, assignmentID int64, include []string) (Submission, error) {
	path := fmt.Sprintf("/api/v1/courses/%d/assignments/%d/submissions/self", courseID, assignmentID)

	params := url.Values{}
	for _, inc := range include {
		params.Add("include[]", inc)
	}

	return Get[Submission](ctx, c, path, params)
}

// ListUpcomingEvents returns upcoming events and assignments for the current user.
// This endpoint is NOT paginated.
func ListUpcomingEvents(ctx context.Context, c *Client) ([]CalendarEvent, error) {
	return Get[[]CalendarEvent](ctx, c, "/api/v1/users/self/upcoming_events", nil)
}

// ListMissingSubmissions returns an iterator over assignments with missing submissions.
func ListMissingSubmissions(ctx context.Context, c *Client, include []string) iter.Seq2[Assignment, error] {
	path := "/api/v1/users/self/missing_submissions"

	params := url.Values{}
	for _, inc := range include {
		params.Add("include[]", inc)
	}

	return Paginate[Assignment](ctx, c, path, params)
}

// ListTodoItems returns the current user's todo items (assignments to submit/grade).
// This endpoint is NOT paginated.
func ListTodoItems(ctx context.Context, c *Client) ([]TodoItem, error) {
	return Get[[]TodoItem](ctx, c, "/api/v1/users/self/todo", nil)
}

// FindAssignment resolves a fuzzy query to a single assignment within a course.
// Priority: numeric ID direct lookup > exact name > substring name.
// All string matching is case-insensitive.
func FindAssignment(ctx context.Context, c *Client, courseID int64, query string) (Assignment, error) {
	// Try numeric ID first
	if id, err := strconv.ParseInt(query, 10, 64); err == nil {
		a, err := GetAssignment(ctx, c, courseID, id, nil)
		if err == nil {
			return a, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Assignment{}, fmt.Errorf("looking up assignment %d: %w", id, err)
		}
	}

	// Collect all assignments for the course
	var assignments []Assignment
	for a, err := range ListAssignments(ctx, c, courseID, ListAssignmentsOptions{}) {
		if err != nil {
			return Assignment{}, fmt.Errorf("listing assignments for search: %w", err)
		}
		assignments = append(assignments, a)
	}

	q := strings.ToLower(query)

	// Exact name match
	for _, a := range assignments {
		if strings.EqualFold(a.Name, query) {
			return a, nil
		}
	}

	// Substring match on name
	for _, a := range assignments {
		if strings.Contains(strings.ToLower(a.Name), q) {
			return a, nil
		}
	}

	return Assignment{}, fmt.Errorf("no assignment matching %q: %w", query, ErrNotFound)
}
