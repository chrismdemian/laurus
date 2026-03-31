package canvas

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// SmartSearchResult represents a result from Canvas Smart Search API (BETA).
type SmartSearchResult struct {
	ContentID   int64   `json:"content_id"`
	ContentType string  `json:"content_type"`
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	HTMLURL     string  `json:"html_url"`
	Distance    float64 `json:"distance"`
}

// SmartSearch performs an AI-powered semantic search within a course.
// This is a BETA Canvas API and may not be available on all instances.
// Returns ErrNotFound or ErrForbidden if the instance doesn't support it.
func SmartSearch(ctx context.Context, c *Client, courseID int64, query string) ([]SmartSearchResult, error) {
	path := fmt.Sprintf("/api/v1/courses/%d/smartsearch", courseID)

	params := url.Values{}
	params.Set("q", query)

	return Get[[]SmartSearchResult](ctx, c, path, params)
}

// SearchResult is a unified result from searching across Canvas resource types.
type SearchResult struct {
	Type    string `json:"type"`
	ID      int64  `json:"id,omitempty"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url,omitempty"`
}

// SearchCourse searches across assignments, pages, and discussions in a course.
// This is the REST fallback used when Smart Search is unavailable.
func SearchCourse(ctx context.Context, c *Client, courseID int64, query string) ([]SearchResult, error) {
	var (
		mu      sync.Mutex
		results []SearchResult
		lastErr error
		wg      sync.WaitGroup
	)

	// Search assignments.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for a, err := range ListAssignments(ctx, c, courseID, ListAssignmentsOptions{
			SearchTerm: query,
		}) {
			if err != nil {
				mu.Lock()
				lastErr = err
				mu.Unlock()
				return
			}
			mu.Lock()
			results = append(results, SearchResult{
				Type:    "assignment",
				ID:      a.ID,
				Title:   a.Name,
				HTMLURL: a.HTMLURL,
			})
			mu.Unlock()
		}
	}()

	// Search pages (may 404 if Pages tab is disabled).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for p, err := range ListPages(ctx, c, courseID, ListPagesOptions{
			SearchTerm: query,
		}) {
			if err != nil {
				// Pages tab disabled is expected on some courses.
				if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrForbidden) {
					mu.Lock()
					lastErr = err
					mu.Unlock()
				}
				return
			}
			mu.Lock()
			results = append(results, SearchResult{
				Type:    "page",
				ID:      p.PageID,
				Title:   p.Title,
				HTMLURL: p.HTMLURL,
			})
			mu.Unlock()
		}
	}()

	// Search discussions.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for d, err := range ListDiscussionTopics(ctx, c, courseID, ListDiscussionTopicsOptions{
			SearchTerm: query,
		}) {
			if err != nil {
				mu.Lock()
				lastErr = err
				mu.Unlock()
				return
			}
			mu.Lock()
			results = append(results, SearchResult{
				Type:    "discussion",
				ID:      d.ID,
				Title:   d.Title,
				HTMLURL: d.HTMLURL,
			})
			mu.Unlock()
		}
	}()

	wg.Wait()

	if len(results) == 0 && lastErr != nil {
		return nil, lastErr
	}

	return results, nil
}

// SearchCourseWithSmartFallback tries Smart Search first, falls back to REST search.
func SearchCourseWithSmartFallback(ctx context.Context, c *Client, courseID int64, query string) ([]SearchResult, error) {
	smartResults, err := SmartSearch(ctx, c, courseID, query)
	if err == nil && len(smartResults) > 0 {
		results := make([]SearchResult, len(smartResults))
		for i, sr := range smartResults {
			results[i] = SearchResult{
				Type:    sr.ContentType,
				ID:      sr.ContentID,
				Title:   sr.Title,
				HTMLURL: sr.HTMLURL,
			}
		}
		return results, nil
	}

	// Smart Search unavailable or returned nothing — fall back to REST.
	if err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrForbidden) &&
		!strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "403") {
		// Unexpected error — still try REST fallback but log intent.
		_ = err
	}

	return SearchCourse(ctx, c, courseID, query)
}
