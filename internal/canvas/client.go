// Package canvas provides the Canvas LMS API client.
package canvas

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/time/rate"
)

// Client is a Canvas LMS API client with built-in rate limiting, retry, and auth.
type Client struct {
	baseURL    string
	httpClient *http.Client
	limiter    *rate.Limiter
	mu         *sync.Mutex
}

// NewClient creates a Canvas API client with the full transport stack:
// retryablehttp -> rate limiter -> auth injector -> base transport.
func NewClient(baseURL, token, version string) *Client {
	// Strip trailing slash from base URL
	baseURL = strings.TrimRight(baseURL, "/")

	limiter := rate.NewLimiter(rate.Limit(10), 10)
	mu := &sync.Mutex{}

	// Build transport stack (innermost first)
	base := http.DefaultTransport

	auth := &authTransport{
		token:   token,
		version: version,
		base:    base,
	}

	rl := &rateLimitTransport{
		limiter: limiter,
		mu:      mu,
		base:    auth,
	}

	// Outermost: retryablehttp for 429/5xx retry
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 4
	retryClient.Logger = nil // suppress stderr logging
	retryClient.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			return true, nil
		}
		return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	}
	retryClient.HTTPClient = &http.Client{Transport: rl}

	return &Client{
		baseURL:    baseURL,
		httpClient: retryClient.StandardClient(),
		limiter:    limiter,
		mu:         mu,
	}
}

// do performs an HTTP request and returns the response body.
// If the response status is >= 400, it returns a parsed error.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, parseErrorResponse(resp.StatusCode, respBody, resp.Header)
	}

	return resp, nil
}

// doRaw performs an HTTP request and returns the raw response.
// If fullURL starts with http:// or https://, it is used as-is (for pagination Link URLs).
// Otherwise, baseURL is prepended.
func (c *Client) doRaw(ctx context.Context, method, fullURL string) (*http.Response, error) {
	url := fullURL
	if !strings.HasPrefix(fullURL, "http://") && !strings.HasPrefix(fullURL, "https://") {
		url = c.baseURL + fullURL
	}

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	return c.httpClient.Do(req)
}

// authTransport injects Bearer token and User-Agent headers.
type authTransport struct {
	token   string
	version string
	base    http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	req.Header.Set("User-Agent", "Laurus/"+t.version)
	return t.base.RoundTrip(req)
}

// rateLimitTransport applies proactive rate limiting and adjusts based on server headers.
type rateLimitTransport struct {
	limiter *rate.Limiter
	mu      *sync.Mutex
	base    http.RoundTripper
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.limiter.Wait(req.Context()); err != nil {
		return nil, err
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	t.adjustRate(resp)
	return resp, nil
}

func (t *rateLimitTransport) adjustRate(resp *http.Response) {
	remaining := resp.Header.Get("X-Rate-Limit-Remaining")
	if remaining == "" {
		return
	}

	n, err := strconv.ParseFloat(remaining, 64)
	if err != nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if n < 50 {
		t.limiter.SetLimit(rate.Limit(2))
		t.limiter.SetBurst(2)
	} else {
		t.limiter.SetLimit(rate.Limit(10))
		t.limiter.SetBurst(10)
	}
}
