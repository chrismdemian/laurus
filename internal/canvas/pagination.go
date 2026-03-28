package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/url"

	"github.com/peterhellberg/link"
)

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

			// Parse Link header for next page
			currentURL = ""
			links := link.ParseResponse(resp)
			if next, ok := links["next"]; ok && next.URI != "" {
				currentURL = next.URI
			}
		}
	}
}
