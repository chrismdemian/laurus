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

// ListPagesOptions controls filtering for ListPages.
type ListPagesOptions struct {
	// Sort controls ordering: "title", "created_at", "updated_at".
	Sort string

	// Order controls direction: "asc" or "desc".
	Order string

	// SearchTerm filters pages by title.
	SearchTerm string

	// Published filters by publish state. Nil = all pages.
	Published *bool
}

// ListPages returns an iterator over wiki pages for a course.
func ListPages(ctx context.Context, c *Client, courseID int64, opts ListPagesOptions) iter.Seq2[Page, error] {
	path := fmt.Sprintf("/api/v1/courses/%d/pages", courseID)

	params := url.Values{}
	if opts.Sort != "" {
		params.Set("sort", opts.Sort)
	}
	if opts.Order != "" {
		params.Set("order", opts.Order)
	}
	if opts.SearchTerm != "" {
		params.Set("search_term", opts.SearchTerm)
	}
	if opts.Published != nil {
		params.Set("published", strconv.FormatBool(*opts.Published))
	}

	return Paginate[Page](ctx, c, path, params)
}

// GetPage retrieves a single wiki page by URL slug or numeric ID.
// The detail endpoint always includes the page body.
func GetPage(ctx context.Context, c *Client, courseID int64, urlOrID string) (Page, error) {
	path := fmt.Sprintf("/api/v1/courses/%d/pages/%s", courseID, url.PathEscape(urlOrID))
	return Get[Page](ctx, c, path, nil)
}

// FindPage resolves a fuzzy query to a single page within a course.
// Priority: numeric ID → URL slug → exact title match → substring title match.
func FindPage(ctx context.Context, c *Client, courseID int64, query string) (Page, error) {
	// Try as slug or numeric ID (Canvas accepts both in the URL path)
	page, err := GetPage(ctx, c, courseID, query)
	if err == nil {
		return page, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Page{}, fmt.Errorf("looking up page %q: %w", query, err)
	}

	// Collect all pages for fuzzy search.
	// ListPages may 404 if the Pages tab is disabled — that's not fatal,
	// it just means we can't do fuzzy search (pages may still exist via modules).
	var pages []Page
	for p, err := range ListPages(ctx, c, courseID, ListPagesOptions{}) {
		if err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) {
				break // Pages tab disabled or restricted — skip fuzzy search
			}
			return Page{}, fmt.Errorf("listing pages for search: %w", err)
		}
		pages = append(pages, p)
	}

	q := strings.ToLower(query)

	// Exact title match
	for _, p := range pages {
		if strings.EqualFold(p.Title, query) {
			return p, nil
		}
	}

	// Substring match
	for _, p := range pages {
		if strings.Contains(strings.ToLower(p.Title), q) {
			return p, nil
		}
	}

	return Page{}, fmt.Errorf("no page matching %q: %w", query, ErrNotFound)
}
