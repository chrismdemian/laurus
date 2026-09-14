package canvas

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetFrontPage_OK(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/courses/468353/front_page" {
			t.Errorf("path = %q, want /api/v1/courses/468353/front_page", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"page_id":42,"url":"home","title":"Course Home","body":"<p>Read <a href=\"/courses/468353/files/1\">syllabus</a></p>","front_page":true,"published":true}`)
	})

	page, err := GetFrontPage(context.Background(), client, 468353)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.Title != "Course Home" {
		t.Errorf("Title = %q, want Course Home", page.Title)
	}
	if page.Body == nil || *page.Body == "" {
		t.Fatal("Body = empty, want front page HTML")
	}
	if !page.FrontPage {
		t.Error("FrontPage = false, want true")
	}
}

func TestGetFrontPage_NotFound(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"errors":[{"message":"The specified resource does not exist."}]}`)
	})

	_, err := GetFrontPage(context.Background(), client, 468696)
	if err == nil {
		t.Fatal("expected an error for a course with no front page")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestCourse_DefaultView(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"id":468353,"name":"ECE355","default_view":"wiki"}`)
	})

	course, err := GetCourse(context.Background(), client, 468353, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if course.DefaultView != "wiki" {
		t.Errorf("DefaultView = %q, want wiki", course.DefaultView)
	}
}

func TestGetFile_ByID_OK(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/files/44730205" {
			t.Errorf("path = %q, want the user-scoped /api/v1/files/44730205", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"id":44730205,"display_name":"syllabus.pdf","size":12345,"url":"https://cdn.example/files/44730205?verifier=abc"}`)
	})

	file, err := GetFile(context.Background(), client, 44730205)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if file.DisplayName != "syllabus.pdf" {
		t.Errorf("DisplayName = %q, want syllabus.pdf", file.DisplayName)
	}
	if file.Size != 12345 {
		t.Errorf("Size = %d, want 12345", file.Size)
	}
	if file.URL == "" {
		t.Error("URL = empty, want the download URL")
	}
}

func TestGetFile_ByID_Forbidden(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"status":"unauthorized","errors":[{"message":"user not authorized to perform that action"}]}`)
	})

	_, err := GetFile(context.Background(), client, 38134074)
	if err == nil {
		t.Fatal("expected an error for a file the user cannot read")
	}
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("error = %v, want ErrForbidden", err)
	}
}

func TestParseFileRef(t *testing.T) {
	tests := []struct {
		query  string
		wantID int64
		wantOK bool
	}{
		{"44730205", 44730205, true},
		{" 44730205 ", 44730205, true},
		{"12345", 12345, true},
		{"https://q.utoronto.ca/courses/468353/files/44730205", 44730205, true},
		{"/courses/468353/files/44730205", 44730205, true},
		{"/courses/468353/files/44730205/download?download_frd=1", 44730205, true},
		{"https://q.utoronto.ca/courses/468353/files/44730205?wrap=1", 44730205, true},
		{"/courses/468353/files/44730205/", 44730205, true},
		{"/files/44730205", 44730205, true},
		{"syllabus.pdf", 0, false},
		{"files", 0, false},
		{"123abc", 0, false},
		{"/courses/468353", 0, false},
		{"lecture 3 files", 0, false},
		{"", 0, false},
		{"0", 0, false},
		{"-5", 0, false},
		{"+5", 0, false},
		{"+44730205", 0, false},
		{"-44730205", 0, false},
		// Short numbers are file names far more often than file IDs: Canvas
		// file IDs run to seven or eight digits.
		{"2024", 0, false},
		{"1", 0, false},
		{"0044730205", 44730205, true},
		// A short ID is still honoured inside a real Canvas link.
		{"/courses/468353/files/42", 42, true},
	}

	for _, tt := range tests {
		gotID, gotOK := ParseFileRef(tt.query)
		if gotID != tt.wantID || gotOK != tt.wantOK {
			t.Errorf("ParseFileRef(%q) = (%d, %v), want (%d, %v)", tt.query, gotID, gotOK, tt.wantID, tt.wantOK)
		}
	}
}

func TestDownloadFromURL_OK(t *testing.T) {
	payload := []byte("%PDF-1.5 pretend pdf bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	n, err := DownloadFromURL(context.Background(), srv.URL, int64(len(payload)), &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != int64(len(payload)) {
		t.Errorf("n = %d, want %d", n, len(payload))
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Errorf("body = %q, want %q", buf.Bytes(), payload)
	}
}

func TestDownloadFromURL_SizeMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("short"))
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	_, err := DownloadFromURL(context.Background(), srv.URL, 98348, &buf)
	if err == nil {
		t.Fatal("expected an error when the download is not the size Canvas reported")
	}
	if !strings.Contains(err.Error(), "expected 98348") {
		t.Errorf("error = %v, want it to name the expected size", err)
	}
}

func TestDownloadFromURL_RejectsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>Please log in</body></html>"))
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	_, err := DownloadFromURL(context.Background(), srv.URL, 0, &buf)
	if err == nil {
		t.Fatal("expected an error when the server answers with an HTML page")
	}
	if !strings.Contains(err.Error(), "HTML page") {
		t.Errorf("error = %v, want it to say the response was an HTML page", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes of the HTML page, want none", buf.Len())
	}
}

func TestDownloadFromURL_SizeUncheckedWhenZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("whatever length"))
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	if _, err := DownloadFromURL(context.Background(), srv.URL, 0, &buf); err != nil {
		t.Fatalf("unexpected error with no expected size: %v", err)
	}
}

func TestDownloadFromURL_ErrorNeverLeaksTheVerifier(t *testing.T) {
	const verifier = "b3da8595-ef90-4cdd-9afc-9b67361b3a07"

	// A server that is already closed, so the transport fails and Go quotes
	// the whole URL back in its error.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL + "/files/44730205/download?download_frd=1&verifier=" + verifier
	dead.Close()

	var buf bytes.Buffer
	_, err := DownloadFromURL(context.Background(), deadURL, 0, &buf)
	if err == nil {
		t.Fatal("expected a transport error from a closed server")
	}
	if strings.Contains(err.Error(), verifier) {
		t.Errorf("error leaks the verifier token: %v", err)
	}
	if strings.Contains(err.Error(), "verifier=") && !strings.Contains(err.Error(), "verifier=REDACTED") {
		t.Errorf("error carries an unredacted verifier parameter: %v", err)
	}
	// The redaction must not swallow the diagnosis.
	if !strings.Contains(err.Error(), "downloading file") {
		t.Errorf("error = %v, want it to still say what failed", err)
	}
}

func TestRedactURLs(t *testing.T) {
	const raw = "https://q.utoronto.ca/files/44730205/download?download_frd=1&verifier=secret-token"

	tests := []struct {
		name string
		msg  string
	}{
		{"quoted whole url", `Get "` + raw + `": dial tcp: connection refused`},
		{"url with no quotes", "Get " + raw + ": EOF"},
		{"verifier alone", "failed for verifier=secret-token"},
		{"verifier mid-query", "url ?a=1&verifier=secret-token&b=2 failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactURLs(tt.msg, raw)
			if strings.Contains(got, "secret-token") {
				t.Errorf("redactURLs(%q) = %q, still carries the token", tt.msg, got)
			}
		})
	}

	// A message with nothing sensitive must come back unchanged.
	plain := "downloading file: HTTP 404"
	if got := redactURLs(plain, raw); got != plain {
		t.Errorf("redactURLs(%q) = %q, want it unchanged", plain, got)
	}
}
