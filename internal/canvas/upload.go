package canvas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

// preflightRequest is the JSON body sent to Canvas's upload preflight endpoint.
type preflightRequest struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
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
// Returns the Location URL to confirm the upload (step 3).
// Uses a plain HTTP client (no auth headers) because S3/InstFS rejects Canvas tokens.
func UploadFileBytes(ctx context.Context, preflight FileUploadPreflight, r io.Reader, filename string) (string, error) {
	// Build multipart body: echo upload_params first, file field last (S3 requirement)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for key, val := range preflight.UploadParams {
		if err := writer.WriteField(key, val); err != nil {
			return "", fmt.Errorf("writing upload param %q: %w", key, err)
		}
	}

	fileParam := preflight.FileParam
	if fileParam == "" {
		fileParam = "file"
	}

	part, err := writer.CreateFormFile(fileParam, filename)
	if err != nil {
		return "", fmt.Errorf("creating file field: %w", err)
	}
	if _, err := io.Copy(part, r); err != nil {
		return "", fmt.Errorf("writing file data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", preflight.UploadURL, &buf)
	if err != nil {
		return "", fmt.Errorf("creating upload request: %w", err)
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
		return "", fmt.Errorf("uploading file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// S3 returns 301/303 with Location header; InstFS returns 201 with Location
	location := resp.Header.Get("Location")
	if location != "" {
		return location, nil
	}

	// Some backends return the file object directly in the body (201)
	if resp.StatusCode == http.StatusCreated {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("reading upload response: %w", err)
		}
		// Try to extract location from JSON response
		var result struct {
			Location string `json:"location"`
			URL      string `json:"url"`
			ID       int64  `json:"id"`
		}
		if err := json.Unmarshal(body, &result); err == nil {
			if result.Location != "" {
				return result.Location, nil
			}
			if result.URL != "" {
				return result.URL, nil
			}
		}
		// If we got an ID directly, the upload is already confirmed
		if result.ID > 0 {
			return "", nil // no confirmation needed
		}
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload failed: HTTP %d: %s", resp.StatusCode, string(body))
	}

	return "", fmt.Errorf("upload response missing Location header (HTTP %d)", resp.StatusCode)
}

// ConfirmUpload performs step 3 of Canvas's upload flow: GET the confirmation URL
// with auth headers to finalize the upload.
// Returns the confirmed File object with its ID.
func ConfirmUpload(ctx context.Context, c *Client, locationURL string) (File, error) {
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

// UploadFile performs the complete 3-step Canvas upload flow for a local file.
// The preflightPath specifies the context-specific endpoint (e.g., assignment submission files).
// Returns the confirmed File object with its Canvas ID.
func UploadFile(ctx context.Context, c *Client, preflightPath, filePath string) (File, error) {
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

	// Step 2: Upload bytes
	location, err := UploadFileBytes(ctx, preflight, f, filename)
	if err != nil {
		return File{}, fmt.Errorf("uploading bytes: %w", err)
	}

	// Step 3: Confirm (if location URL was returned)
	if location == "" {
		// Some backends confirm inline — re-fetch the file info
		return File{}, fmt.Errorf("upload completed but no confirmation URL returned")
	}

	file, err := ConfirmUpload(ctx, c, location)
	if err != nil {
		return File{}, fmt.Errorf("confirming upload: %w", err)
	}

	return file, nil
}

// detectContentType returns a MIME type based on file extension.
func detectContentType(filename string) string {
	ext := filepath.Ext(filename)
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
	case ".r", ".R":
		return "text/x-r"
	case ".m":
		return "text/x-matlab"
	default:
		return "application/octet-stream"
	}
}
