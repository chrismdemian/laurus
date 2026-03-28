package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestListCourses_Basic(t *testing.T) {
	courses := []Course{
		{ID: 1, Name: "Intro to CS", CourseCode: "CSC108"},
		{ID: 2, Name: "Data Structures", CourseCode: "CSC148"},
	}
	data, _ := json.Marshal(courses)

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/courses" {
			t.Errorf("path = %q, want /api/v1/courses", r.URL.Path)
		}
		if pp := r.URL.Query().Get("per_page"); pp != "100" {
			t.Errorf("per_page = %q, want 100", pp)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})

	var got []Course
	for c, err := range ListCourses(context.Background(), client, CourseListOptions{}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = append(got, c)
	}
	if len(got) != 2 {
		t.Fatalf("got %d courses, want 2", len(got))
	}
	if got[0].CourseCode != "CSC108" {
		t.Errorf("got[0].CourseCode = %q, want CSC108", got[0].CourseCode)
	}
}

func TestListCourses_Favorites(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/users/self/favorites/courses") {
			t.Errorf("path = %q, want favorites endpoint", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `[]`)
	})

	for _, err := range ListCourses(context.Background(), client, CourseListOptions{FavoritesOnly: true}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestListCourses_WithOptions(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if es := q.Get("enrollment_state"); es != "active" {
			t.Errorf("enrollment_state = %q, want active", es)
		}
		states := q["state[]"]
		if len(states) != 2 || states[0] != "available" || states[1] != "completed" {
			t.Errorf("state[] = %v, want [available completed]", states)
		}
		includes := q["include[]"]
		if len(includes) != 1 || includes[0] != "enrollments" {
			t.Errorf("include[] = %v, want [enrollments]", includes)
		}
		_, _ = fmt.Fprint(w, `[]`)
	})

	opts := CourseListOptions{
		EnrollmentState: "active",
		State:           []string{"available", "completed"},
		Include:         []string{"enrollments"},
	}
	for _, err := range ListCourses(context.Background(), client, opts) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestGetCourse(t *testing.T) {
	course := Course{ID: 42, Name: "Algorithms", CourseCode: "CSC263"}
	data, _ := json.Marshal(course)

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/courses/42" {
			t.Errorf("path = %q, want /api/v1/courses/42", r.URL.Path)
		}
		includes := r.URL.Query()["include[]"]
		if len(includes) != 2 {
			t.Errorf("include[] = %v, want [syllabus_body teachers]", includes)
		}
		_, _ = w.Write(data)
	})

	got, err := GetCourse(context.Background(), client, 42, []string{"syllabus_body", "teachers"})
	if err != nil {
		t.Fatalf("GetCourse error: %v", err)
	}
	if got.CourseCode != "CSC263" {
		t.Errorf("CourseCode = %q, want CSC263", got.CourseCode)
	}
}

func TestGetCourse_NotFound(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"message": "The specified resource does not exist"}`)
	})

	_, err := GetCourse(context.Background(), client, 9999, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestListEnrollments(t *testing.T) {
	enrollments := []Enrollment{
		{
			ID:              1,
			UserID:          42,
			CourseID:        100,
			Type:            "StudentEnrollment",
			EnrollmentState: "active",
			Grades: &EnrollmentGrades{
				CurrentScore: ptrFloat(87.5),
				CurrentGrade: ptrString("A"),
			},
		},
	}
	data, _ := json.Marshal(enrollments)

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/courses/100/enrollments" {
			t.Errorf("path = %q, want /api/v1/courses/100/enrollments", r.URL.Path)
		}
		_, _ = w.Write(data)
	})

	var got []Enrollment
	for e, err := range ListEnrollments(context.Background(), client, 100) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = append(got, e)
	}
	if len(got) != 1 {
		t.Fatalf("got %d enrollments, want 1", len(got))
	}
	if got[0].Grades == nil || *got[0].Grades.CurrentScore != 87.5 {
		t.Errorf("CurrentScore = %v, want 87.5", got[0].Grades.CurrentScore)
	}
}

func TestGetUserProfile(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/self/profile" {
			t.Errorf("path = %q, want /api/v1/users/self/profile", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"id": 42, "name": "Test User", "time_zone": "America/Toronto"}`)
	})

	user, err := GetUserProfile(context.Background(), client)
	if err != nil {
		t.Fatalf("GetUserProfile error: %v", err)
	}
	if user.Name != "Test User" {
		t.Errorf("Name = %q, want Test User", user.Name)
	}
	if user.TimeZone != "America/Toronto" {
		t.Errorf("TimeZone = %q, want America/Toronto", user.TimeZone)
	}
}

func TestFindCourse_ByID(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/courses/42" {
			_, _ = fmt.Fprint(w, `{"id": 42, "name": "Found", "course_code": "CSC42"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"message": "not found"}`)
	})

	got, err := FindCourse(context.Background(), client, "42")
	if err != nil {
		t.Fatalf("FindCourse error: %v", err)
	}
	if got.ID != 42 {
		t.Errorf("ID = %d, want 42", got.ID)
	}
}

func TestFindCourse_ExactCode(t *testing.T) {
	courses := []Course{
		{ID: 1, Name: "Intro to CS", CourseCode: "CSC108"},
		{ID: 2, Name: "Data Structures", CourseCode: "CSC148"},
	}
	data, _ := json.Marshal(courses)

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// ID lookup will fail (not a number), goes to list
		if strings.Contains(r.URL.Path, "/api/v1/courses") && !strings.Contains(r.URL.Path, "/api/v1/courses/") {
			_, _ = w.Write(data)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"message": "not found"}`)
	})

	got, err := FindCourse(context.Background(), client, "CSC108")
	if err != nil {
		t.Fatalf("FindCourse error: %v", err)
	}
	if got.ID != 1 {
		t.Errorf("ID = %d, want 1", got.ID)
	}
}

func TestFindCourse_CaseInsensitive(t *testing.T) {
	courses := []Course{
		{ID: 1, Name: "Intro to CS", CourseCode: "CSC108"},
	}
	data, _ := json.Marshal(courses)

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	})

	got, err := FindCourse(context.Background(), client, "csc108")
	if err != nil {
		t.Fatalf("FindCourse error: %v", err)
	}
	if got.ID != 1 {
		t.Errorf("ID = %d, want 1", got.ID)
	}
}

func TestFindCourse_Partial(t *testing.T) {
	courses := []Course{
		{ID: 1, Name: "Intro to CS", CourseCode: "CSC108"},
		{ID: 2, Name: "Data Structures", CourseCode: "CSC148"},
	}
	data, _ := json.Marshal(courses)

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// "108" parses as numeric, so GetCourse(108) will be tried first — return 404
		if r.URL.Path == "/api/v1/courses/108" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"message": "not found"}`)
			return
		}
		_, _ = w.Write(data)
	})

	got, err := FindCourse(context.Background(), client, "108")
	if err != nil {
		t.Fatalf("FindCourse error: %v", err)
	}
	if got.ID != 1 {
		t.Errorf("ID = %d, want 1 (CSC108)", got.ID)
	}
}

func TestFindCourse_ByName(t *testing.T) {
	courses := []Course{
		{ID: 1, Name: "Intro to Computer Science", CourseCode: "CSC108"},
	}
	data, _ := json.Marshal(courses)

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	})

	got, err := FindCourse(context.Background(), client, "computer science")
	if err != nil {
		t.Fatalf("FindCourse error: %v", err)
	}
	if got.ID != 1 {
		t.Errorf("ID = %d, want 1", got.ID)
	}
}

func TestFindCourse_NotFound(t *testing.T) {
	courses := []Course{
		{ID: 1, Name: "Intro to CS", CourseCode: "CSC108"},
	}
	data, _ := json.Marshal(courses)

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	})

	_, err := FindCourse(context.Background(), client, "NONEXISTENT999")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// Test helpers

func ptrFloat(f float64) *float64 { return &f }
func ptrString(s string) *string  { return &s }
