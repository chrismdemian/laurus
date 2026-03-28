package canvas

import (
	"context"
	"fmt"
	"iter"
	"net/url"
)

// ListAssignmentGroups returns an iterator over assignment groups for a course.
// Use include values: "assignments", "submission", "score_statistics".
func ListAssignmentGroups(ctx context.Context, c *Client, courseID int64, include []string) iter.Seq2[AssignmentGroup, error] {
	path := fmt.Sprintf("/api/v1/courses/%d/assignment_groups", courseID)

	params := url.Values{}
	for _, inc := range include {
		params.Add("include[]", inc)
	}

	return Paginate[AssignmentGroup](ctx, c, path, params)
}

// GetGradingStandard returns a specific grading standard for a course.
func GetGradingStandard(ctx context.Context, c *Client, courseID, standardID int64) (GradingStandard, error) {
	path := fmt.Sprintf("/api/v1/courses/%d/grading_standards/%d", courseID, standardID)
	return Get[GradingStandard](ctx, c, path, nil)
}

// gradingPeriodsWrapper handles Canvas's envelope: {"grading_periods": [...]}.
type gradingPeriodsWrapper struct {
	GradingPeriods []GradingPeriod `json:"grading_periods"`
}

// ListGradingPeriods returns grading periods for a course.
func ListGradingPeriods(ctx context.Context, c *Client, courseID int64) ([]GradingPeriod, error) {
	path := fmt.Sprintf("/api/v1/courses/%d/grading_periods", courseID)
	wrapper, err := Get[gradingPeriodsWrapper](ctx, c, path, nil)
	if err != nil {
		return nil, fmt.Errorf("listing grading periods: %w", err)
	}
	return wrapper.GradingPeriods, nil
}
