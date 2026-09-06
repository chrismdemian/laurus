package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/url"
	"strings"

	"github.com/peterhellberg/link"
)

// ErrPaginationTruncated is yielded (after every item already received)
// when Canvas advertises a next page but gives it an empty URL, an
// intermittent Canvas bug. Callers get the partial data and a typed signal
// that the set is incomplete, so nothing downstream treats it as the whole
// set (the cache must not prune against it).
var ErrPaginationTruncated = errors.New("canvas: pagination truncated (next link present but empty)")

// Paginate returns an iterator that yields items from a paginated Canvas API endpoint.
// It follows Link rel="next" headers automatically, requesting per_page=100.
func Paginate[T any](ctx context.Context, c *Client, path string, params url.Values) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		p := url.Values{}
		for k, v := range params {
			p[k] = v
		}
		p.Set("per_page", "100")

		currentURL := path + "?" + p.Encode()

		for currentURL != "" {
			resp, err := c.doRaw(ctx, "GET", currentURL)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}

			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				var zero T
				yield(zero, fmt.Errorf("reading paginated response: %w", err))
				return
			}

			if resp.StatusCode >= 400 {
				var zero T
				yield(zero, parseErrorResponse(resp.StatusCode, body, resp.Header))
				return
			}

			var items []T
			if err := json.Unmarshal(body, &items); err != nil {
				var zero T
				yield(zero, fmt.Errorf("parsing paginated response: %w", err))
				return
			}

			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}

			// Parse Link header for next page. rel="next" absent means last
			// page; rel="next" present but empty is the Canvas bug above.
			// The link parser drops an empty "<>" entry entirely, so the raw
			// header is what proves a next page was advertised.
			currentURL = ""
			links := link.ParseResponse(resp)
			if next, ok := links["next"]; ok && next.URI != "" {
				currentURL = next.URI
			} else if headerAdvertisesNext(resp.Header.Values("Link")) {
				var zero T
				yield(zero, ErrPaginationTruncated)
				return
			}
		}
	}
}

// headerAdvertisesNext reports whether any Link header value carries a
// rel="next" relation, regardless of whether its URL parsed.
func headerAdvertisesNext(values []string) bool {
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if strings.Contains(strings.ToLower(part), `rel="next"`) || strings.Contains(strings.ToLower(part), "rel=next") {
				return true
			}
		}
	}
	return false
}
