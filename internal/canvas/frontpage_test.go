package canvas

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	}

	for _, tt := range tests {
		gotID, gotOK := ParseFileRef(tt.query)
		if gotID != tt.wantID || gotOK != tt.wantOK {
			t.Errorf("ParseFileRef(%q) = (%d, %v), want (%d, %v)", tt.query, gotID, gotOK, tt.wantID, tt.wantOK)
		}
	}
}
