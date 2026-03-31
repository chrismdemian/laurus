package canvas

import (
	"context"
	"fmt"
	"net/url"
)

// AppointmentGroup represents a Canvas appointment group (office hours).
type AppointmentGroup struct {
	ID                            int64           `json:"id"`
	Title                         string          `json:"title"`
	Description                   *string         `json:"description"`
	LocationName                  *string         `json:"location_name"`
	ParticipantCount              int             `json:"participant_count"`
	MaxAppointmentsPerParticipant *int            `json:"max_appointments_per_participant"`
	ContextCodes                  []string        `json:"context_codes"`
	WorkflowState                 string          `json:"workflow_state"`
	HTMLURL                       string          `json:"html_url"`
	Appointments                  []CalendarEvent `json:"appointments"`
}

// ListAppointmentGroups returns reservable appointment groups for the current user.
func ListAppointmentGroups(ctx context.Context, c *Client) ([]AppointmentGroup, error) {
	params := url.Values{}
	params.Set("scope", "reservable")
	params.Add("include[]", "appointments")
	params.Add("include[]", "participant_count")
	params.Add("include[]", "reserved_times")

	return Get[[]AppointmentGroup](ctx, c, "/api/v1/appointment_groups", params)
}

// ReserveAppointmentSlot reserves a time slot for the current user.
func ReserveAppointmentSlot(ctx context.Context, c *Client, slotID int64) (CalendarEvent, error) {
	path := fmt.Sprintf("/api/v1/calendar_events/%d/reservations", slotID)
	return Post[CalendarEvent](ctx, c, path, nil)
}
