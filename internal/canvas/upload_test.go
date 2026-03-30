package canvas

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightUpload(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/courses/100/assignments/200/submissions/self/files" {
			t.Errorf("path = %q", r.URL.Path)
		}

		var body preflightRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Name != "solution.py" {
			t.Errorf("name = %q, want solution.py", body.Name)
		}
		if body.Size != 1024 {
			t.Errorf("size = %d, want 1024", body.Size)
		}

		resp := FileUploadPreflight{
			UploadURL:    "https://s3.example.com/upload",
			UploadParams: map[string]string{"key": "abc123", "Policy": "xyz"},
			FileParam:    "file",
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	pf, err := PreflightUpload(context.Background(), client,
		"/api/v1/courses/100/assignments/200/submissions/self/files",
		"solution.py", 1024, "text/x-python")
	if err != nil {
		t.Fatalf("PreflightUpload error: %v", err)
	}
	if pf.UploadURL != "https://s3.example.com/upload" {
		t.Errorf("UploadURL = %q", pf.UploadURL)
	}
	if pf.UploadParams["key"] != "abc123" {
		t.Errorf("UploadParams[key] = %q", pf.UploadParams["key"])
	}
}

func TestUploadFileBytes_S3Redirect(t *testing.T) {
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}

		// Verify no auth header sent to S3
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization header sent to S3: %q", auth)
		}

		// Verify multipart content
		ct := r.Header.Get("Content-Type")
		mediaType, params, err := mime.ParseMediaType(ct)
		if err != nil {
			t.Fatalf("parse content type: %v", err)
		}
		if mediaType != "multipart/form-data" {
			t.Errorf("content type = %q, want multipart/form-data", mediaType)
		}

		reader := multipart.NewReader(r.Body, params["boundary"])

		// First field should be an upload param
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("reading first part: %v", err)
		}
		if part.FormName() != "key" {
			t.Errorf("first field = %q, want key", part.FormName())
		}
		val, _ := io.ReadAll(part)
		if string(val) != "abc123" {
			t.Errorf("key value = %q, want abc123", string(val))
		}

		// Last field should be the file
		var lastPart *multipart.Part
		for {
			p, err := reader.NextPart()
			if err != nil {
				break
			}
			lastPart = p
		}
		if lastPart == nil || lastPart.FormName() != "file" {
			name := ""
			if lastPart != nil {
				name = lastPart.FormName()
			}
			t.Errorf("last field = %q, want file", name)
		}

		// Return 303 with Location (S3 pattern)
		w.Header().Set("Location", "https://canvas.example.com/api/v1/files/999/create_success?uuid=XYZ")
		w.WriteHeader(http.StatusSeeOther)
	}))
	defer uploadServer.Close()

	preflight := FileUploadPreflight{
		UploadURL:    uploadServer.URL,
		UploadParams: map[string]string{"key": "abc123"},
		FileParam:    "file",
	}

	result, err := UploadFileBytes(context.Background(), preflight, strings.NewReader("print('hello')"), "solution.py")
	if err != nil {
		t.Fatalf("UploadFileBytes error: %v", err)
	}
	if result.Location != "https://canvas.example.com/api/v1/files/999/create_success?uuid=XYZ" {
		t.Errorf("location = %q", result.Location)
	}
	if result.InlineFile != nil {
		t.Error("expected no inline file for S3 redirect")
	}
}

func TestConfirmUpload(t *testing.T) {
	confirmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %q, want GET", r.Method)
		}

		file := File{
			ID:          999,
			DisplayName: "solution.py",
			Size:        1024,
			ContentType: "text/x-python",
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(file)
	}))
	defer confirmServer.Close()

	// Create a client that points to our confirm server
	client := NewClient(confirmServer.URL, "test-token", "test")

	file, err := ConfirmUpload(context.Background(), client, confirmServer.URL+"/api/v1/files/999/create_success")
	if err != nil {
		t.Fatalf("ConfirmUpload error: %v", err)
	}
	if file.ID != 999 {
		t.Errorf("file.ID = %d, want 999", file.ID)
	}
	if file.DisplayName != "solution.py" {
		t.Errorf("file.DisplayName = %q", file.DisplayName)
	}
}

func TestUploadFile_FullFlow(t *testing.T) {
	// Create a temp file to upload
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "solution.py")
	if err := os.WriteFile(filePath, []byte("print('hello world')"), 0644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	// We need the Canvas server URL for the Location header, but it's not known
	// until the server starts. Use a variable to capture it.
	var canvasURL string

	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Step 2: Upload bytes — redirect to Canvas confirmation endpoint
		w.Header().Set("Location", canvasURL+"/api/v1/files/999/create_success?uuid=XYZ")
		w.WriteHeader(http.StatusSeeOther)
	}))
	defer uploadServer.Close()

	client, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.Contains(r.URL.Path, "submissions/self/files"):
			// Step 1: Preflight
			resp := FileUploadPreflight{
				UploadURL:    uploadServer.URL,
				UploadParams: map[string]string{"key": "abc123"},
				FileParam:    "file",
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == "GET" && strings.Contains(r.URL.Path, "create_success"):
			// Step 3: Confirm
			file := File{ID: 999, DisplayName: "solution.py", Size: 20}
			_ = json.NewEncoder(w).Encode(file)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	canvasURL = srv.URL

	file, err := UploadFile(context.Background(), client,
		"/api/v1/courses/100/assignments/200/submissions/self/files", filePath)
	if err != nil {
		t.Fatalf("UploadFile error: %v", err)
	}
	if file.ID != 999 {
		t.Errorf("file.ID = %d, want 999", file.ID)
	}
}

func TestUploadFile_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "empty.txt")
	if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make any API calls for empty file")
	})

	_, err := UploadFile(context.Background(), client, "/api/v1/courses/100/assignments/200/submissions/self/files", filePath)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if !strings.Contains(err.Error(), "empty (0 bytes)") {
		t.Errorf("error = %q, want 'empty (0 bytes)'", err.Error())
	}
}

func TestUploadFileBytes_InlineConfirm(t *testing.T) {
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Backend confirms inline with a 201 and file JSON body (no Location header)
		file := File{ID: 777, DisplayName: "essay.pdf", Size: 2048}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(file)
	}))
	defer uploadServer.Close()

	preflight := FileUploadPreflight{
		UploadURL:    uploadServer.URL,
		UploadParams: map[string]string{"token": "xyz"},
		FileParam:    "file",
	}

	result, err := UploadFileBytes(context.Background(), preflight, strings.NewReader("essay content"), "essay.pdf")
	if err != nil {
		t.Fatalf("UploadFileBytes error: %v", err)
	}
	if result.InlineFile == nil {
		t.Fatal("expected inline file, got nil")
	}
	if result.InlineFile.ID != 777 {
		t.Errorf("file.ID = %d, want 777", result.InlineFile.ID)
	}
	if result.Location != "" {
		t.Errorf("expected empty location for inline confirm, got %q", result.Location)
	}
}

func TestValidateConfirmURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		locURL  string
		wantErr bool
	}{
		{"same host", "https://q.utoronto.ca", "https://q.utoronto.ca/api/v1/files/999/create_success", false},
		{"different host", "https://q.utoronto.ca", "https://evil.com/steal-token", true},
		{"case insensitive", "https://Q.UTORONTO.CA", "https://q.utoronto.ca/api/v1/files/1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfirmURL(tt.baseURL, tt.locURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfirmURL(%q, %q) error = %v, wantErr = %v", tt.baseURL, tt.locURL, err, tt.wantErr)
			}
		})
	}
}

func TestDetectContentType_CaseInsensitive(t *testing.T) {
	if got := detectContentType("REPORT.PDF"); got != "application/pdf" {
		t.Errorf("detectContentType(REPORT.PDF) = %q, want application/pdf", got)
	}
}

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"solution.py", "text/x-python"},
		{"report.pdf", "application/pdf"},
		{"data.csv", "text/csv"},
		{"code.java", "text/x-java-source"},
		{"main.go", "text/x-go"},
		{"image.png", "image/png"},
		{"unknown.xyz", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := detectContentType(tt.filename)
			if got != tt.want {
				t.Errorf("detectContentType(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}
