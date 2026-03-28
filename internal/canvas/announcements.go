package canvas

import (
	"context"
	"iter"
	"net/url"
)

// ListAnnouncementsOptions controls filtering for ListAnnouncements.
type ListAnnouncementsOptions struct {
	// ContextCodes specifies which courses to include (e.g., ["course_123"]).
	// At least one is required by Canvas.
	ContextCodes []string

	// StartDate is the earliest date to return announcements from (ISO 8601 date).
	// Canvas defaults to 14 days ago if omitted — pass an explicit old date for all announcements.
	StartDate string

	// EndDate is the latest date (ISO 8601 date). Optional.
	EndDate string

	// ActiveOnly returns only active announcements.
	ActiveOnly bool
}

// ListAnnouncements returns an iterator over announcements matching the given filters.
// Canvas requires at least one context_code.
func ListAnnouncements(ctx context.Context, c *Client, opts ListAnnouncementsOptions) iter.Seq2[Announcement, error] {
	path := "/api/v1/announcements"

	params := url.Values{}
	for _, code := range opts.ContextCodes {
		params.Add("context_codes[]", code)
	}
	if opts.StartDate != "" {
		params.Set("start_date", opts.StartDate)
	}
	if opts.EndDate != "" {
		params.Set("end_date", opts.EndDate)
	}
	if opts.ActiveOnly {
		params.Set("active_only", "true")
	}

	return Paginate[Announcement](ctx, c, path, params)
}
