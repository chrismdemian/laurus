package canvas

import (
	"context"
	"fmt"
)

// submissionWrapper wraps CreateSubmissionRequest in the format Canvas expects:
// {"submission": {"submission_type": "...", ...}}
type submissionWrapper struct {
	Submission CreateSubmissionRequest `json:"submission"`
}

// CreateSubmission submits an assignment for the current user.
// Canvas expects the body wrapped as {"submission": {...}}.
func CreateSubmission(ctx context.Context, c *Client, courseID, assignmentID int64, req CreateSubmissionRequest) (Submission, error) {
	path := fmt.Sprintf("/api/v1/courses/%d/assignments/%d/submissions", courseID, assignmentID)
	return Post[Submission](ctx, c, path, submissionWrapper{Submission: req})
}
