package canvas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ProgressFunc is called during file upload with the number of bytes written so far
// and the total file size in bytes. Implementations should be non-blocking.
type ProgressFunc func(bytesWritten, totalBytes int64)

// preflightRequest is the JSON body sent to Canvas's upload preflight endpoint.
type preflightRequest struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// uploadResult is returned by UploadFileBytes. Either Location is set (needs confirmation)
// or InlineFile is set (upload was confirmed inline by the backend).
type uploadResult struct {
	Location   string // confirmation URL (step 3 needed)
	InlineFile *File  // file confirmed inline (no step 3 needed)
}

// PreflightUpload initiates the first step of Canvas's 3-step upload flow.
// The path parameter specifies the context-specific preflight endpoint, e.g.:
//   - /api/v1/courses/:id/assignments/:id/submissions/self/files (assignment submission)
//   - /api/v1/users/self/files (user files, for conversation attachments)
func PreflightUpload(ctx context.Context, c *Client, path string, name string, size int64, contentType string) (FileUploadPreflight, error) {
	req := preflightRequest{
		Name:        name,
		Size:        size,
		ContentType: contentType,
	}
	return Post[FileUploadPreflight](ctx, c, path, req)
}

// UploadFileBytes performs step 2 of Canvas's upload flow: POST multipart/form-data
// to the upload URL returned by preflight.
//
// Returns an uploadResult: either a Location URL for step 3 confirmation,
// or an inline-confirmed File when the backend confirms directly (InstFS 201).
// Uses a plain HTTP client (no auth headers) because S3/InstFS rejects Canvas tokens.
func UploadFileBytes(ctx context.Context, preflight FileUploadPreflight, r io.Reader, filename string) (uploadResult, error) {
	// Build multipart body: echo upload_params first, file field last (S3 requirement)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for key, val := range preflight.UploadParams {
		if err := writer.WriteField(key, val); err != nil {
			return uploadResult{}, fmt.Errorf("writing upload param %q: %w", key, err)
		}
	}

	fileParam := preflight.FileParam
	if fileParam == "" {
		fileParam = "file"
	}

	part, err := writer.CreateFormFile(fileParam, filename)
	if err != nil {
		return uploadResult{}, fmt.Errorf("creating file field: %w", err)
	}
	if _, err := io.Copy(part, r); err != nil {
		return uploadResult{}, fmt.Errorf("writing file data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return uploadResult{}, fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", preflight.UploadURL, &buf)
	if err != nil {
		return uploadResult{}, fmt.Errorf("creating upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Use plain HTTP client — no auth headers for S3/InstFS
	// Disable redirect following to capture the Location header
	plainClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := plainClient.Do(req)
	if err != nil {
		return uploadResult{}, fmt.Errorf("uploading file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// S3 returns 301/303 with Location header; InstFS may return 201 with Location
	location := resp.Header.Get("Location")
	if location != "" {
		return uploadResult{Location: location}, nil
	}

	// Some backends return the file object directly in the body (201 with no Location)
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return uploadResult{}, fmt.Errorf("reading upload response: %w", err)
		}
		var file File
		if err := json.Unmarshal(body, &file); err == nil && file.ID > 0 {
			return uploadResult{InlineFile: &file}, nil
		}
		// Try to extract a location/url from JSON
		var result struct {
			Location string `json:"location"`
			URL      string `json:"url"`
		}
		if err := json.Unmarshal(body, &result); err == nil {
			if result.Location != "" {
				return uploadResult{Location: result.Location}, nil
			}
			if result.URL != "" {
				return uploadResult{Location: result.URL}, nil
			}
		}
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return uploadResult{}, fmt.Errorf("upload failed: HTTP %d: %s", resp.StatusCode, string(body))
	}

	return uploadResult{}, fmt.Errorf("upload response missing Location header (HTTP %d)", resp.StatusCode)
}

// ConfirmUpload performs step 3 of Canvas's upload flow: GET the confirmation URL
// with auth headers to finalize the upload.
// Returns the confirmed File object with its ID.
// The confirmation URL must point to the same Canvas instance (baseURL) for security.
func ConfirmUpload(ctx context.Context, c *Client, locationURL string) (File, error) {
	// Validate that the confirmation URL points to the Canvas instance,
	// not an attacker-controlled host that could steal the Bearer token.
	if err := validateConfirmURL(c.baseURL, locationURL); err != nil {
		return File{}, err
	}

	resp, err := c.doRaw(ctx, "GET", locationURL)
	if err != nil {
		return File{}, fmt.Errorf("confirming upload: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return File{}, fmt.Errorf("upload confirmation failed: HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return File{}, fmt.Errorf("reading confirmation response: %w", err)
	}

	var file File
	if err := json.Unmarshal(body, &file); err != nil {
		return File{}, fmt.Errorf("parsing confirmed file: %w", err)
	}
	return file, nil
}

// validateConfirmURL checks that locationURL shares the same host as the Canvas base URL.
func validateConfirmURL(baseURL, locationURL string) error {
	base, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("parsing base URL: %w", err)
	}
	loc, err := url.Parse(locationURL)
	if err != nil {
		return fmt.Errorf("parsing confirmation URL: %w", err)
	}
	if !strings.EqualFold(loc.Host, base.Host) {
		return fmt.Errorf("confirmation URL host %q does not match Canvas host %q", loc.Host, base.Host)
	}
	return nil
}

// UploadFile performs the complete 3-step Canvas upload flow for a local file.
// The preflightPath specifies the context-specific endpoint (e.g., assignment submission files).
// Returns the confirmed File object with its Canvas ID.
func UploadFile(ctx context.Context, c *Client, preflightPath, filePath string) (File, error) {
	return UploadFileWithProgress(ctx, c, preflightPath, filePath, nil)
}

// UploadFileWithProgress performs the complete 3-step Canvas upload flow with optional
// progress reporting. If progress is nil, no progress callbacks are made.
// If step 2 fails with HTTP 403/401 (expired upload token), it retries once with a
// fresh preflight token.
func UploadFileWithProgress(ctx context.Context, c *Client, preflightPath, filePath string, progress ProgressFunc) (File, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return File{}, fmt.Errorf("opening file: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return File{}, fmt.Errorf("stating file: %w", err)
	}
	if info.Size() == 0 {
		return File{}, fmt.Errorf("file %q is empty (0 bytes)", filePath)
	}

	filename := filepath.Base(filePath)
	contentType := detectContentType(filename)

	// Step 1: Preflight
	preflight, err := PreflightUpload(ctx, c, preflightPath, filename, info.Size(), contentType)
	if err != nil {
		return File{}, fmt.Errorf("upload preflight: %w", err)
	}

	// Step 2: Upload bytes (with optional progress wrapping)
	var reader io.Reader = f
	if progress != nil {
		reader = &progressReader{r: f, total: info.Size(), fn: progress}
	}

	result, err := UploadFileBytes(ctx, preflight, reader, filename)
	if err != nil && isUploadTokenExpired(err) {
		// Upload token expired — get fresh preflight and retry once
		if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
			return File{}, fmt.Errorf("cannot retry upload (seek failed): %w", err)
		}
		preflight, err = PreflightUpload(ctx, c, preflightPath, filename, info.Size(), contentType)
		if err != nil {
			return File{}, fmt.Errorf("upload retry preflight: %w", err)
		}
		reader = f
		if progress != nil {
			reader = &progressReader{r: f, total: info.Size(), fn: progress}
		}
		result, err = UploadFileBytes(ctx, preflight, reader, filename)
	}
	if err != nil {
		return File{}, fmt.Errorf("uploading bytes: %w", err)
	}

	// If the backend confirmed inline, no step 3 needed
	if result.InlineFile != nil {
		return *result.InlineFile, nil
	}

	// Step 3: Confirm via Location URL
	file, err := ConfirmUpload(ctx, c, result.Location)
	if err != nil {
		return File{}, fmt.Errorf("confirming upload: %w", err)
	}

	return file, nil
}

// isUploadTokenExpired checks if the error indicates an expired upload token (HTTP 403 or 401).
func isUploadTokenExpired(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 403") || strings.Contains(msg, "HTTP 401")
}

// progressReader wraps an io.Reader and calls fn after each Read with cumulative bytes read.
type progressReader struct {
	r       io.Reader
	total   int64
	written int64
	fn      ProgressFunc
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.written += int64(n)
		pr.fn(pr.written, pr.total)
	}
	return n, err
}

// detectContentType returns a MIME type based on file extension.
func detectContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".py":
		return "text/x-python"
	case ".java":
		return "text/x-java-source"
	case ".c", ".h":
		return "text/x-c"
	case ".cpp", ".cc", ".cxx":
		return "text/x-c++src"
	case ".js":
		return "application/javascript"
	case ".ts":
		return "application/typescript"
	case ".go":
		return "text/x-go"
	case ".rs":
		return "text/x-rust"
	case ".txt":
		return "text/plain"
	case ".md":
		return "text/markdown"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".zip":
		return "application/zip"
	case ".tar":
		return "application/x-tar"
	case ".gz":
		return "application/gzip"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".csv":
		return "text/csv"
	case ".r":
		return "text/x-r"
	case ".m":
		return "text/x-matlab"
	default:
		return "application/octet-stream"
	}
}
