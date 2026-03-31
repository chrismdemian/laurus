package canvas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Get performs a GET request and unmarshals the JSON response into T.
func Get[T any](ctx context.Context, c *Client, path string, params url.Values) (T, error) {
	var zero T

	fullPath := path
	if len(params) > 0 {
		fullPath = path + "?" + params.Encode()
	}

	resp, err := c.do(ctx, "GET", fullPath, nil)
	if err != nil {
		return zero, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, fmt.Errorf("reading response: %w", err)
	}

	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return zero, fmt.Errorf("parsing response: %w", err)
	}
	return result, nil
}

// GetWithHeaders performs a GET request, unmarshals JSON into T, and also returns response headers.
func GetWithHeaders[T any](ctx context.Context, c *Client, path string, params url.Values) (T, http.Header, error) {
	var zero T

	fullPath := path
	if len(params) > 0 {
		fullPath = path + "?" + params.Encode()
	}

	resp, err := c.do(ctx, "GET", fullPath, nil)
	if err != nil {
		return zero, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, nil, fmt.Errorf("reading response: %w", err)
	}

	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return zero, nil, fmt.Errorf("parsing response: %w", err)
	}
	return result, resp.Header, nil
}

// Post performs a POST request with a JSON body and unmarshals the response into T.
func Post[T any](ctx context.Context, c *Client, path string, reqBody any) (T, error) {
	var zero T

	data, err := json.Marshal(reqBody)
	if err != nil {
		return zero, fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := c.do(ctx, "POST", path, bytes.NewReader(data))
	if err != nil {
		return zero, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, fmt.Errorf("reading response: %w", err)
	}

	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return zero, fmt.Errorf("parsing response: %w", err)
	}
	return result, nil
}

// Put performs a PUT request with a JSON body and unmarshals the response into T.
func Put[T any](ctx context.Context, c *Client, path string, reqBody any) (T, error) {
	var zero T

	data, err := json.Marshal(reqBody)
	if err != nil {
		return zero, fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := c.do(ctx, "PUT", path, bytes.NewReader(data))
	if err != nil {
		return zero, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, fmt.Errorf("reading response: %w", err)
	}

	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return zero, fmt.Errorf("parsing response: %w", err)
	}
	return result, nil
}

// Delete performs a DELETE request. It does not parse the response body.
func Delete(ctx context.Context, c *Client, path string) error {
	resp, err := c.do(ctx, "DELETE", path, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
