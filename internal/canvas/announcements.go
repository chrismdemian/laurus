package canvas

import (
	"context"
	"iter"
	"net/url"
	"time"
)

// ListAnnouncementsOptions controls filtering for ListAnnouncements.
type ListAnnouncementsOptions struct {
	// ContextCodes specifies which courses to include (e.g., ["course_123"]).
	// At least one is required by Canvas.
	ContextCodes []string

	// StartDate is the earliest date to return announcements from (ISO 8601 date).
	// Canvas defaults to 14 days ago if omitted — pass an explicit old date for all announcements.
	StartDate string

	// EndDate is the latest date (ISO 8601 date). Canvas defaults it to
	// 28 days AFTER start_date, so an old StartDate alone is an EMPTY
	// window (measured 2026-09-06: start_date=2000-01-01 returned 0 rows
	// for courses holding 15, 34 and 16 announcements). When StartDate is
	// set and EndDate is empty, ListAnnouncements sends a far-future
	// end_date so the window covers everything.
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
	} else if opts.StartDate != "" {
		params.Set("end_date", defaultAnnouncementsEnd())
	}
	if opts.ActiveOnly {
		params.Set("active_only", "true")
	}

	return Paginate[Announcement](ctx, c, path, params)
}

// defaultAnnouncementsEnd is one year from today: far enough that a
// scheduled (delayed-post) announcement is still inside the window.
func defaultAnnouncementsEnd() string {
	return time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
}
