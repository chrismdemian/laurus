package courses

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// testFactory wires a Factory to a fake Canvas and buffered streams.
func testFactory(t *testing.T, handler http.HandlerFunc) (*cmdutil.Factory, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ios, _, out, errOut := iostreams.Test()
	f := &cmdutil.Factory{
		Version:   "test",
		IOStreams: func() *iostreams.IOStreams { return ios },
		Client: func() (*canvas.Client, error) {
			return canvas.NewClient(srv.URL, "tok", "test"), nil
		},
	}
	return f, out, errOut
}

const wikiCourse = `{"id":468353,"name":"ECE355","course_code":"ECE355","default_view":"wiki","workflow_state":"available"}`

// courseHandler answers the course endpoint with body, and the front page with
// whatever fp writes.
func courseHandler(body string, fp http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/front_page") {
			fp(w, r)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}
}

// The detail view, --syllabus and --json never depend on the front page, so a
// front page that cannot be read must not fail them.
func TestViewCourse_FrontPageErrorDoesNotFailOtherViews(t *testing.T) {
	failures := map[string]http.HandlerFunc{
		"forbidden": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `{"errors":[{"message":"user not authorized"}]}`)
		},
		"unauthorized": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"errors":[{"message":"no"}]}`)
		},
		"unparseable body": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `<html>gateway</html>`)
		},
	}

	modes := []struct {
		name             string
		syllabus, isJSON bool
	}{
		{name: "detail"},
		{name: "syllabus", syllabus: true},
		{name: "json", isJSON: true},
	}

	for fname, fp := range failures {
		for _, m := range modes {
			t.Run(fname+"/"+m.name, func(t *testing.T) {
				f, out, errOut := testFactory(t, courseHandler(wikiCourse, fp))
				f.IOStreams().IsJSON = m.isJSON

				if err := ViewCourse(f, "468353", m.syllabus, false); err != nil {
					t.Fatalf("ViewCourse failed on an unreadable front page: %v", err)
				}
				if out.Len() == 0 {
					t.Error("no output; the course itself should still have been shown")
				}
				if !strings.Contains(errOut.String(), "Could not read the course front page") {
					t.Errorf("stderr = %q, want a note about the front page", errOut.String())
				}
			})
		}
	}
}

// A course that simply has no front page is not a failure and not worth a note.
func TestViewCourse_NoFrontPageIsSilent(t *testing.T) {
	notFound := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"errors":[{"message":"No front page has been set"}]}`)
	}
	f, out, errOut := testFactory(t, courseHandler(
		`{"id":468696,"name":"ECE421","default_view":"modules","workflow_state":"available"}`, notFound))

	if err := ViewCourse(f, "468696", false, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want nothing for a course with no front page", errOut.String())
	}
	if !strings.Contains(out.String(), "modules") {
		t.Errorf("stdout = %q, want the default view reported", out.String())
	}
}

// --home is the one mode that is nothing but the front page, so a failure
// there is the command's failure.
func TestViewCourse_HomePropagatesFailure(t *testing.T) {
	fp := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"errors":[{"message":"user not authorized"}]}`)
	}
	f, _, _ := testFactory(t, courseHandler(wikiCourse, fp))

	if err := ViewCourse(f, "468353", false, true); err == nil {
		t.Fatal("want an error when --home cannot read the front page")
	}
}

// --home on a course that has no front page is a clean report, not an error.
func TestViewCourse_HomeNoFrontPage(t *testing.T) {
	fp := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"errors":[{"message":"No front page has been set"}]}`)
	}
	f, out, _ := testFactory(t, courseHandler(
		`{"id":468696,"name":"ECE421","default_view":"modules","workflow_state":"available"}`, fp))

	if err := ViewCourse(f, "468696", false, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "No front page (default view: modules).") {
		t.Errorf("stdout = %q, want the no-front-page report", out.String())
	}
}

// A front page that exists but says nothing reads as empty, not as absent.
func TestViewCourse_HomeEmptyBody(t *testing.T) {
	fp := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"page_id":7,"title":"Home Page","body":"   "}`)
	}
	f, out, _ := testFactory(t, courseHandler(wikiCourse, fp))

	if err := ViewCourse(f, "468353", false, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Front page is empty.") {
		t.Errorf("stdout = %q, want the empty-front-page report", out.String())
	}
}

// --syllabus falls back to the front page only on a wiki course.
func TestViewCourse_SyllabusFallback(t *testing.T) {
	fp := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"page_id":7,"title":"Home","body":"<p>Reading list</p>"}`)
	}

	t.Run("wiki course falls back", func(t *testing.T) {
		f, out, errOut := testFactory(t, courseHandler(wikiCourse, fp))
		if err := ViewCourse(f, "468353", true, false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(errOut.String(), "showing the course front page instead") {
			t.Errorf("stderr = %q, want the fallback note", errOut.String())
		}
		if !strings.Contains(out.String(), "Reading list") {
			t.Errorf("stdout = %q, want the front page body", out.String())
		}
	})

	t.Run("modules course does not", func(t *testing.T) {
		modules := `{"id":468696,"name":"ECE421","default_view":"modules","workflow_state":"available"}`
		f, out, _ := testFactory(t, courseHandler(modules, fp))
		if err := ViewCourse(f, "468696", true, false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out.String(), "No syllabus available.") {
			t.Errorf("stdout = %q, want the no-syllabus message", out.String())
		}
	})
}
