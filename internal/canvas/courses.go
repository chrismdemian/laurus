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

// CourseListOptions controls filtering for ListCourses.
type CourseListOptions struct {
	// EnrollmentState filters by enrollment state (e.g., "active", "completed").
	// Empty string means Canvas default (active + invited).
	EnrollmentState string

	// State filters by course workflow state (e.g., "available", "completed").
	State []string

	// Include specifies additional data to include (e.g., "enrollments", "syllabus_body").
	Include []string

	// FavoritesOnly returns only favorited courses.
	FavoritesOnly bool
}

// ListCourses returns an iterator over the authenticated user's courses.
// If opts.FavoritesOnly is true, it uses the favorites endpoint.
func ListCourses(ctx context.Context, c *Client, opts CourseListOptions) iter.Seq2[Course, error] {
	path := "/api/v1/courses"
	if opts.FavoritesOnly {
		path = "/api/v1/users/self/favorites/courses"
	}

	params := url.Values{}
	if opts.EnrollmentState != "" {
		params.Set("enrollment_state", opts.EnrollmentState)
	}
	for _, s := range opts.State {
		params.Add("state[]", s)
	}
	for _, inc := range opts.Include {
		params.Add("include[]", inc)
	}

	return Paginate[Course](ctx, c, path, params)
}

// GetCourse retrieves a single course by ID with optional includes.
func GetCourse(ctx context.Context, c *Client, courseID int64, include []string) (Course, error) {
	path := fmt.Sprintf("/api/v1/courses/%d", courseID)

	params := url.Values{}
	for _, inc := range include {
		params.Add("include[]", inc)
	}

	return Get[Course](ctx, c, path, params)
}

// ListEnrollments returns an iterator over enrollments for a course.
func ListEnrollments(ctx context.Context, c *Client, courseID int64) iter.Seq2[Enrollment, error] {
	path := fmt.Sprintf("/api/v1/courses/%d/enrollments", courseID)
	return Paginate[Enrollment](ctx, c, path, nil)
}

// GetUserProfile retrieves the authenticated user's profile.
func GetUserProfile(ctx context.Context, c *Client) (User, error) {
	return Get[User](ctx, c, "/api/v1/users/self/profile", nil)
}

// FindCourse resolves a fuzzy query to a single course.
// Priority: numeric ID direct lookup > exact course_code > substring course_code > substring name.
// All string matching is case-insensitive.
func FindCourse(ctx context.Context, c *Client, query string) (Course, error) {
	// Try numeric ID first
	if id, err := strconv.ParseInt(query, 10, 64); err == nil {
		course, err := GetCourse(ctx, c, id, nil)
		if err == nil {
			return course, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Course{}, fmt.Errorf("looking up course %d: %w", id, err)
		}
	}

	// Collect all active courses
	var courses []Course
	for course, err := range ListCourses(ctx, c, CourseListOptions{}) {
		if err != nil {
			return Course{}, fmt.Errorf("listing courses for search: %w", err)
		}
		courses = append(courses, course)
	}

	q := strings.ToLower(query)

	// Exact course_code match
	for _, course := range courses {
		if strings.EqualFold(course.CourseCode, query) {
			return course, nil
		}
	}

	// Substring match on course_code
	for _, course := range courses {
		if strings.Contains(strings.ToLower(course.CourseCode), q) {
			return course, nil
		}
	}

	// Substring match on name
	for _, course := range courses {
		if strings.Contains(strings.ToLower(course.Name), q) {
			return course, nil
		}
	}

	return Course{}, fmt.Errorf("no course matching %q: %w", query, ErrNotFound)
}
