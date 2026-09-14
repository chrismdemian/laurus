package canvas

import (
	"context"
	"fmt"
	"io"
	"iter"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ListFilesOptions controls filtering for ListFiles.
type ListFilesOptions struct {
	// Sort controls ordering: "name", "size", "created_at", "updated_at".
	Sort string

	// Order controls direction: "asc" or "desc".
	Order string

	// SearchTerm filters files by name.
	SearchTerm string
}

// ListFiles returns an iterator over files for a course.
func ListFiles(ctx context.Context, c *Client, courseID int64, opts ListFilesOptions) iter.Seq2[File, error] {
	path := fmt.Sprintf("/api/v1/courses/%d/files", courseID)

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

	return Paginate[File](ctx, c, path, params)
}

// ListFolders returns an iterator over all folders for a course (flat list at all depths).
func ListFolders(ctx context.Context, c *Client, courseID int64) iter.Seq2[Folder, error] {
	path := fmt.Sprintf("/api/v1/courses/%d/folders", courseID)
	return Paginate[Folder](ctx, c, path, nil)
}

// GetFile retrieves a single file by its ID.
// This works even when the course Files tab is restricted (403),
// because individual file access through module items is permitted.
func GetFile(ctx context.Context, c *Client, fileID int64) (File, error) {
	path := fmt.Sprintf("/api/v1/files/%d", fileID)
	return Get[File](ctx, c, path, nil)
}

// publicURLResponse wraps the Canvas response for file public URLs.
type publicURLResponse struct {
	PublicURL string `json:"public_url"`
}

// GetFilePublicURL returns a pre-signed download URL for a file.
// The URL does not require authentication and expires after ~1 hour.
func GetFilePublicURL(ctx context.Context, c *Client, fileID int64) (string, error) {
	path := fmt.Sprintf("/api/v1/files/%d/public_url", fileID)
	resp, err := Get[publicURLResponse](ctx, c, path, nil)
	if err != nil {
		return "", fmt.Errorf("getting public URL for file %d: %w", fileID, err)
	}
	return resp.PublicURL, nil
}

// DownloadFile downloads a file by its ID to the given writer.
// Uses GetFilePublicURL to obtain a pre-signed URL, then downloads with a plain
// HTTP client (no auth headers sent to external S3/CDN hosts).
func DownloadFile(ctx context.Context, c *Client, fileID int64, dest io.Writer) (int64, error) {
	publicURL, err := GetFilePublicURL(ctx, c, fileID)
	if err != nil {
		return 0, fmt.Errorf("getting download URL: %w", err)
	}
	return DownloadFromURL(ctx, publicURL, 0, dest)
}

// DownloadFromURL downloads a pre-authorized Canvas file URL to dest using a
// plain HTTP client. Canvas file URLs carry their own verifier token, so no
// auth headers are sent to the external S3/CDN host.
//
// expectedSize, when positive, is the size Canvas reported for the file: a
// download that does not match it is reported as an error rather than written
// off as a success. An HTML response is rejected outright, because a login or
// error page answers with 200 and would otherwise land on disk as the file.
func DownloadFromURL(ctx context.Context, rawURL string, expectedSize int64, dest io.Writer) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return 0, fmt.Errorf("creating download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("downloading file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("downloading file: HTTP %d", resp.StatusCode)
	}

	// A Canvas login or error page answers 200 with HTML. No Canvas file
	// download is served as text/html, so treat one as a failed download.
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		if mediaType, _, mErr := mime.ParseMediaType(ct); mErr == nil && mediaType == "text/html" {
			return 0, fmt.Errorf("downloading file: server returned an HTML page, not the file (content-type %s)", ct)
		}
	}

	n, err := io.Copy(dest, resp.Body)
	if err != nil {
		return n, fmt.Errorf("writing file: %w", err)
	}

	if expectedSize > 0 && n != expectedSize {
		return n, fmt.Errorf("downloading file: got %d bytes, expected %d", n, expectedSize)
	}
	return n, nil
}

// FindFile resolves a fuzzy query to a single file within a course.
// Priority: exact display name match > substring match > first server result.
func FindFile(ctx context.Context, c *Client, courseID int64, query string) (File, error) {
	// Try server-side search first for efficiency
	var files []File
	for f, err := range ListFiles(ctx, c, courseID, ListFilesOptions{SearchTerm: query}) {
		if err != nil {
			return File{}, fmt.Errorf("searching files: %w", err)
		}
		files = append(files, f)
	}

	if len(files) == 0 {
		return File{}, fmt.Errorf("no file matching %q: %w", query, ErrNotFound)
	}

	q := strings.ToLower(query)

	// Exact display name match
	for _, f := range files {
		if strings.EqualFold(f.DisplayName, query) {
			return f, nil
		}
	}

	// Substring match on display name
	for _, f := range files {
		if strings.Contains(strings.ToLower(f.DisplayName), q) {
			return f, nil
		}
	}

	// Server-side search returned results but no exact/substring match — return first result
	return files[0], nil
}

// fileRefPattern matches the /files/<id> segment of a Canvas file link.
var fileRefPattern = regexp.MustCompile(`/files/(\d+)(?:/|\?|#|$)`)

// ParseFileRef extracts a Canvas file ID from a bare numeric string or from a
// Canvas file URL/path containing a "/files/<id>" segment. It reports false
// when the query is a file name rather than a reference.
func ParseFileRef(query string) (int64, bool) {
	q := strings.TrimSpace(query)
	if q == "" {
		return 0, false
	}

	if id, err := strconv.ParseInt(q, 10, 64); err == nil && id > 0 {
		return id, true
	}

	if m := fileRefPattern.FindStringSubmatch(q); m != nil {
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err == nil && id > 0 {
			return id, true
		}
	}
	return 0, false
}
