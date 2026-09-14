package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/chrismdemian/laurus/internal/canvas"
)

// newHandlerServer builds a Server whose Canvas client points at handler. The
// cache is left unconfigured, so every read goes to that handler.
func newHandlerServer(t *testing.T, handler http.HandlerFunc) *Server {
	t.Helper()
	httpSrv := httptest.NewServer(handler)
	t.Cleanup(httpSrv.Close)

	return &Server{
		newClient: func() (*canvas.Client, error) {
			return canvas.NewClient(httpSrv.URL, "test-token", "test"), nil
		},
	}
}

// resultEnvelope decodes a successful tool result into its envelope.
func resultEnvelope(t *testing.T, res *mcplib.CallToolResult) (envelope, map[string]any) {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool returned an error result: %s", resultText(t, res))
	}
	var env envelope
	if err := json.Unmarshal([]byte(resultText(t, res)), &env); err != nil {
		t.Fatalf("decoding envelope: %v", err)
	}
	data, _ := env.Data.(map[string]any)
	return env, data
}

func resultText(t *testing.T, res *mcplib.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	txt, ok := res.Content[0].(mcplib.TextContent)
	if !ok {
		t.Fatalf("tool result content is %T, want text", res.Content[0])
	}
	return txt.Text
}

const testCourseJSON = `{"id":468353,"name":"ECE355","course_code":"ECE355","default_view":"wiki"}`

func TestHandleGetFrontPage_OK(t *testing.T) {
	s := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/courses/468353/front_page":
			_, _ = fmt.Fprint(w, `{"page_id":7,"url":"home","title":"Course Home","body":"<p>Read the <a href=\"/courses/468353/files/1\">syllabus</a></p>"}`)
		case strings.HasPrefix(r.URL.Path, "/api/v1/courses/468353"):
			_, _ = fmt.Fprint(w, testCourseJSON)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	res, err := s.handleGetFrontPage(context.Background(), mcplib.CallToolRequest{}, getFrontPageArgs{Course: "468353"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env, data := resultEnvelope(t, res)
	if env.Source != sourceLive {
		t.Errorf("source = %q, want live", env.Source)
	}
	if data["title"] != "Course Home" {
		t.Errorf("title = %v, want Course Home", data["title"])
	}
	body, _ := data["body"].(string)
	if !strings.Contains(body, "syllabus") {
		t.Errorf("body = %q, want the page text", body)
	}
	if strings.Contains(body, "<p>") {
		t.Errorf("body = %q, want markdown rather than HTML", body)
	}
}

func TestHandleGetFrontPage_NotFoundIsAnError(t *testing.T) {
	s := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/courses/468696/front_page" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"errors":[{"message":"No front page has been set"}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":468696,"name":"ECE421","default_view":"modules"}`)
	})

	res, err := s.handleGetFrontPage(context.Background(), mcplib.CallToolRequest{}, getFrontPageArgs{Course: "468696"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("want an error result for a course with no front page, got %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "Not found") {
		t.Errorf("message = %q, want it to report not found", resultText(t, res))
	}
}

func TestHandleGetCourse_IncludesFrontPageAndDefaultView(t *testing.T) {
	s := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/courses/468353/front_page" {
			_, _ = fmt.Fprint(w, `{"page_id":7,"title":"Course Home","body":"<p>Reading list</p>"}`)
			return
		}
		_, _ = fmt.Fprint(w, testCourseJSON)
	})

	res, err := s.handleGetCourse(context.Background(), mcplib.CallToolRequest{}, getCourseArgs{Course: "468353"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env, data := resultEnvelope(t, res)
	if env.Note != "" {
		t.Errorf("note = %q, want none when the front page was read", env.Note)
	}
	if data["default_view"] != "wiki" {
		t.Errorf("default_view = %v, want wiki", data["default_view"])
	}
	fp, ok := data["front_page"].(map[string]any)
	if !ok {
		t.Fatalf("front_page = %v, want an object", data["front_page"])
	}
	if fp["title"] != "Course Home" {
		t.Errorf("front_page.title = %v, want Course Home", fp["title"])
	}
}

// A course with no front page is the ordinary case for a modules course: no
// front page in the data, and no note, because nothing went wrong.
func TestHandleGetCourse_NoFrontPage(t *testing.T) {
	s := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/courses/468696/front_page" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"errors":[{"message":"No front page has been set"}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":468696,"name":"ECE421","default_view":"modules"}`)
	})

	res, err := s.handleGetCourse(context.Background(), mcplib.CallToolRequest{}, getCourseArgs{Course: "468696"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env, data := resultEnvelope(t, res)
	if env.Note != "" {
		t.Errorf("note = %q, want none for a course that simply has no front page", env.Note)
	}
	if _, present := data["front_page"]; present {
		t.Errorf("front_page = %v, want it omitted", data["front_page"])
	}
	if data["default_view"] != "modules" {
		t.Errorf("default_view = %v, want modules", data["default_view"])
	}
}

// A front page that cannot be read must not fail get_course: the rest of the
// course is still the answer, and the envelope carries the reason.
func TestHandleGetCourse_FrontPageErrorDegrades(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"forbidden", http.StatusForbidden, `{"status":"unauthorized","errors":[{"message":"user not authorized to perform that action"}]}`},
		// A 200 whose body is not a page: an error with no sentinel at all,
		// which the old code turned into a failed get_course.
		{"unparseable body", http.StatusOK, `<html>gateway</html>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/courses/468353/front_page" {
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprint(w, tc.body)
					return
				}
				_, _ = fmt.Fprint(w, testCourseJSON)
			})

			res, err := s.handleGetCourse(context.Background(), mcplib.CallToolRequest{}, getCourseArgs{Course: "468353"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			env, data := resultEnvelope(t, res)
			if data["name"] != "ECE355" {
				t.Errorf("name = %v, want the course to still be returned", data["name"])
			}
			if _, present := data["front_page"]; present {
				t.Errorf("front_page = %v, want it omitted when it could not be read", data["front_page"])
			}
			if !strings.Contains(env.Note, "front page could not be read") {
				t.Errorf("note = %q, want it to explain the missing front page", env.Note)
			}
		})
	}
}

func TestHandleGetFile_BareIDNeedsNoCourse(t *testing.T) {
	var paths []string
	s := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/v1/files/44730205" {
			_, _ = fmt.Fprint(w, `{"id":44730205,"display_name":"syllabus.pdf","size":98348,"content-type":"application/pdf","url":"https://cdn.example/f?verifier=abc"}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"errors":[{"message":"user not authorized to perform that action"}]}`)
	})

	res, err := s.handleGetFile(context.Background(), mcplib.CallToolRequest{}, getFileArgs{File: "44730205"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, data := resultEnvelope(t, res)
	if data["name"] != "syllabus.pdf" {
		t.Errorf("name = %v, want syllabus.pdf", data["name"])
	}
	for _, p := range paths {
		if strings.Contains(p, "/courses/") {
			t.Errorf("requested %q; the ID path must not touch the course endpoints", p)
		}
	}
}

func TestHandleGetFile_URLFormNeedsNoCourse(t *testing.T) {
	s := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/files/44730205" {
			_, _ = fmt.Fprint(w, `{"id":44730205,"display_name":"syllabus.pdf"}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	})

	res, err := s.handleGetFile(context.Background(), mcplib.CallToolRequest{},
		getFileArgs{File: "https://q.utoronto.ca/courses/468353/files/44730205?wrap=1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, data := resultEnvelope(t, res)
	if data["id"] != float64(44730205) {
		t.Errorf("id = %v, want 44730205", data["id"])
	}
}

func TestHandleGetFile_NameWithoutCourseIsRefused(t *testing.T) {
	s := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request should be made; got %s", r.URL.Path)
	})

	res, err := s.handleGetFile(context.Background(), mcplib.CallToolRequest{}, getFileArgs{File: "syllabus.pdf"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("want an error result, got %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "course is required") {
		t.Errorf("message = %q, want it to ask for a course", resultText(t, res))
	}
}

// A hidden Files tab must produce the advice to pass an ID, not a bare 403.
func TestHandleGetFile_HiddenFilesTabExplainsTheIDRoute(t *testing.T) {
	s := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/files") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `{"status":"unauthorized","errors":[{"message":"user not authorized to perform that action"}]}`)
			return
		}
		_, _ = fmt.Fprint(w, testCourseJSON)
	})

	res, err := s.handleGetFile(context.Background(), mcplib.CallToolRequest{}, getFileArgs{Course: "468353", File: "syllabus.pdf"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("want an error result, got %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "file ID") {
		t.Errorf("message = %q, want it to suggest the file ID", resultText(t, res))
	}
}
