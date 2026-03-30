package canvas

import (
	"context"
	"fmt"
)

// CreatePlannerNote creates a personal planner note (student-only todo item).
func CreatePlannerNote(ctx context.Context, c *Client, req CreatePlannerNoteRequest) (PlannerNote, error) {
	return Post[PlannerNote](ctx, c, "/api/v1/planner_notes", req)
}

// DeletePlannerNote deletes a planner note by ID.
func DeletePlannerNote(ctx context.Context, c *Client, noteID int64) error {
	path := fmt.Sprintf("/api/v1/planner_notes/%d", noteID)
	return Delete(ctx, c, path)
}

// createOverrideRequest is the JSON body for creating a planner override.
type createOverrideRequest struct {
	PlannerableType string `json:"plannable_type"`
	PlannerableID   int64  `json:"plannable_id"`
	MarkedComplete  bool   `json:"marked_complete"`
	Dismissed       bool   `json:"dismissed"`
}

// CreatePlannerOverride creates an override on a planner item (mark done or dismiss).
// plannerableType is typically "planner_note", "assignment", or "discussion_topic".
func CreatePlannerOverride(ctx context.Context, c *Client, plannerableType string, plannerableID int64, markedComplete, dismissed bool) (PlannerOverride, error) {
	return Post[PlannerOverride](ctx, c, "/api/v1/planner/overrides", createOverrideRequest{
		PlannerableType: plannerableType,
		PlannerableID:   plannerableID,
		MarkedComplete:  markedComplete,
		Dismissed:       dismissed,
	})
}
