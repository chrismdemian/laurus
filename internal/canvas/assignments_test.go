package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestListAssignments_Basic(t *testing.T) {
	due := time.Date(2025, 10, 15, 3, 59, 0, 0, time.UTC)
	pts := 100.0
	assignments := []Assignment{
		{ID: 500, Name: "Binary Search Trees", CourseID: 100, DueAt: &due, PointsPossible: &pts},
		{ID: 501, Name: "Hash Tables", CourseID: 100},
	}
	data, _ := json.Marshal(assignments)

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/courses/100/assignments" {
			t.Errorf("path = %q, want /api/v1/courses/100/assignments", r.URL.Path)
		}
		if pp := r.URL.Query().Get("per_page"); pp != "100" {
			t.Errorf("per_page = %q, want 100", pp)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})

	var got []Assignment
	for a, err := range ListAssignments(context.Background(), client, 100, ListAssignmentsOptions{}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = append(got, a)
	}
	if len(got) != 2 {
		t.Fatalf("got %d assignments, want 2", len(got))
	}
	if got[0].Name != "Binary Search Trees" {
		t.Errorf("got[0].Name = %q, want Binary Search Trees", got[0].Name)
	}
}

func TestListAssignments_WithOptions(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("bucket") != "upcoming" {
			t.Errorf("bucket = %q, want upcoming", q.Get("bucket"))
		}
		if q.Get("order_by") != "due_at" {
			t.Errorf("order_by = %q, want due_at", q.Get("order_by"))
		}
		if q.Get("search_term") != "trees" {
			t.Errorf("search_term = %q, want trees", q.Get("search_term"))
		}
		includes := q["include[]"]
		if len(includes) != 1 || includes[0] != "submission" {
			t.Errorf("include[] = %v, want [submission]", includes)
		}
		_, _ = fmt.Fprint(w, `[]`)
	})

	for _, err := range ListAssignments(context.Background(), client, 100, ListAssignmentsOptions{
		Bucket:     "upcoming",
		OrderBy:    "due_at",
		SearchTerm: "trees",
		Include:    []string{"submission"},
	}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestGetAssignment(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/courses/100/assignments/500" {
			t.Errorf("path = %q, want /api/v1/courses/100/assignments/500", r.URL.Path)
		}
		includes := r.URL.Query()["include[]"]
		if len(includes) != 2 {
			t.Errorf("include[] count = %d, want 2", len(includes))
		}
		_, _ = fmt.Fprint(w, `{"id": 500, "name": "BST", "course_id": 100}`)
	})

	a, err := GetAssignment(context.Background(), client, 100, 500, []string{"submission", "all_dates"})
	if err != nil {
		t.Fatalf("GetAssignment error: %v", err)
	}
	if a.ID != 500 {
		t.Errorf("ID = %d, want 500", a.ID)
	}
}

func TestGetAssignment_NotFound(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"message":"not found"}`)
	})

	_, err := GetAssignment(context.Background(), client, 100, 999, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestListSubmissions_Basic(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/courses/100/students/submissions" {
			t.Errorf("path = %q, want /api/v1/courses/100/students/submissions", r.URL.Path)
		}
		studentIDs := r.URL.Query()["student_ids[]"]
		if len(studentIDs) != 1 || studentIDs[0] != "self" {
			t.Errorf("student_ids[] = %v, want [self]", studentIDs)
		}
		_, _ = fmt.Fprint(w, `[{"id": 777, "assignment_id": 500, "workflow_state": "graded"}]`)
	})

	var got []Submission
	for s, err := range ListSubmissions(context.Background(), client, 100, ListSubmissionsOptions{}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = append(got, s)
	}
	if len(got) != 1 {
		t.Fatalf("got %d submissions, want 1", len(got))
	}
	if got[0].WorkflowState != "graded" {
		t.Errorf("WorkflowState = %q, want graded", got[0].WorkflowState)
	}
}

func TestListSubmissions_WithOptions(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assignmentIDs := q["assignment_ids[]"]
		if len(assignmentIDs) != 2 || assignmentIDs[0] != "500" || assignmentIDs[1] != "501" {
			t.Errorf("assignment_ids[] = %v, want [500 501]", assignmentIDs)
		}
		if q.Get("workflow_state") != "graded" {
			t.Errorf("workflow_state = %q, want graded", q.Get("workflow_state"))
		}
		includes := q["include[]"]
		if len(includes) != 1 || includes[0] != "rubric_assessment" {
			t.Errorf("include[] = %v, want [rubric_assessment]", includes)
		}
		_, _ = fmt.Fprint(w, `[]`)
	})

	for _, err := range ListSubmissions(context.Background(), client, 100, ListSubmissionsOptions{
		AssignmentIDs: []int64{500, 501},
		WorkflowState: "graded",
		Include:       []string{"rubric_assessment"},
	}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestGetSubmission(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/courses/100/assignments/500/submissions/self" {
			t.Errorf("path = %q, want .../submissions/self", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"id": 777, "assignment_id": 500, "workflow_state": "submitted"}`)
	})

	s, err := GetSubmission(context.Background(), client, 100, 500, nil)
	if err != nil {
		t.Fatalf("GetSubmission error: %v", err)
	}
	if s.WorkflowState != "submitted" {
		t.Errorf("WorkflowState = %q, want submitted", s.WorkflowState)
	}
}

func TestListUpcomingEvents(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/self/upcoming_events" {
			t.Errorf("path = %q, want /api/v1/users/self/upcoming_events", r.URL.Path)
		}
		// Canvas returns string IDs like "assignment_500" on this endpoint
		_, _ = fmt.Fprint(w, `[
			{"id": "event_1", "title": "Lecture", "type": "event"},
			{"id": "assignment_500", "title": "BST Due", "type": "assignment", "assignment": {"id": 500, "name": "BST"}}
		]`)
	})

	events, err := ListUpcomingEvents(context.Background(), client)
	if err != nil {
		t.Fatalf("ListUpcomingEvents error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Type != "event" {
		t.Errorf("events[0].Type = %q, want event", events[0].Type)
	}
	if events[1].Assignment == nil || events[1].Assignment.ID != 500 {
		t.Errorf("events[1].Assignment = %v, want {ID:500}", events[1].Assignment)
	}
}

func TestListMissingSubmissions(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/self/missing_submissions" {
			t.Errorf("path = %q, want /api/v1/users/self/missing_submissions", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `[{"id": 501, "name": "Hash Tables", "course_id": 100, "missing": true}]`)
	})

	var got []Assignment
	for a, err := range ListMissingSubmissions(context.Background(), client, nil) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = append(got, a)
	}
	if len(got) != 1 {
		t.Fatalf("got %d assignments, want 1", len(got))
	}
	if !got[0].Missing {
		t.Error("Missing = false, want true")
	}
}

func TestListTodoItems(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/self/todo" {
			t.Errorf("path = %q, want /api/v1/users/self/todo", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `[{
			"type": "submitting",
			"assignment": {"id": 500, "name": "BST"},
			"context_type": "Course",
			"context_name": "CSC108",
			"course_id": 100,
			"html_url": "https://q.utoronto.ca/courses/100/assignments/500"
		}]`)
	})

	items, err := ListTodoItems(context.Background(), client)
	if err != nil {
		t.Fatalf("ListTodoItems error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Type != "submitting" {
		t.Errorf("Type = %q, want submitting", items[0].Type)
	}
	if items[0].Assignment == nil || items[0].Assignment.Name != "BST" {
		t.Errorf("Assignment = %v, want {Name:BST}", items[0].Assignment)
	}
}

func TestFindAssignment_ByID(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/courses/100/assignments/500" {
			_, _ = fmt.Fprint(w, `{"id": 500, "name": "BST", "course_id": 100}`)
			return
		}
		t.Errorf("unexpected path: %s", r.URL.Path)
	})

	a, err := FindAssignment(context.Background(), client, 100, "500")
	if err != nil {
		t.Fatalf("FindAssignment error: %v", err)
	}
	if a.ID != 500 {
		t.Errorf("ID = %d, want 500", a.ID)
	}
}

func TestFindAssignment_ByName(t *testing.T) {
	assignments := []Assignment{
		{ID: 500, Name: "Binary Search Trees"},
		{ID: 501, Name: "Hash Tables"},
	}
	data, _ := json.Marshal(assignments)

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/courses/100/assignments" {
			_, _ = w.Write(data)
			return
		}
		// Numeric lookup for "hash" should not be called
		t.Errorf("unexpected path: %s", r.URL.Path)
	})

	a, err := FindAssignment(context.Background(), client, 100, "hash")
	if err != nil {
		t.Fatalf("FindAssignment error: %v", err)
	}
	if a.ID != 501 {
		t.Errorf("ID = %d, want 501", a.ID)
	}
}

func TestFindAssignment_ExactNameFirst(t *testing.T) {
	assignments := []Assignment{
		{ID: 500, Name: "Assignment 1"},
		{ID: 501, Name: "Assignment 10"},
	}
	data, _ := json.Marshal(assignments)

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	})

	a, err := FindAssignment(context.Background(), client, 100, "Assignment 1")
	if err != nil {
		t.Fatalf("FindAssignment error: %v", err)
	}
	if a.ID != 500 {
		t.Errorf("ID = %d, want 500 (exact match)", a.ID)
	}
}

func TestFindAssignment_NotFound(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `[]`)
	})

	_, err := FindAssignment(context.Background(), client, 100, "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestAssignment_Unmarshal_WithSubmission(t *testing.T) {
	raw := `{
		"id": 500,
		"name": "BST",
		"course_id": 100,
		"missing": false,
		"submission": {
			"id": 777,
			"assignment_id": 500,
			"user_id": 42,
			"score": 85.5,
			"grade": "85.5",
			"workflow_state": "graded",
			"late": false,
			"missing": false,
			"excused": false
		}
	}`

	var a Assignment
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if a.Submission == nil {
		t.Fatal("Submission is nil, want non-nil")
	}
	if a.Submission.ID != 777 {
		t.Errorf("Submission.ID = %d, want 777", a.Submission.ID)
	}
	if a.Submission.Score == nil || *a.Submission.Score != 85.5 {
		t.Errorf("Submission.Score = %v, want 85.5", a.Submission.Score)
	}
}

func TestAssignment_Unmarshal_NullSubmission(t *testing.T) {
	raw := `{
		"id": 501,
		"name": "Hash Tables",
		"course_id": 100,
		"submission": null,
		"missing": true
	}`

	var a Assignment
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if a.Submission != nil {
		t.Errorf("Submission = %v, want nil", a.Submission)
	}
	if !a.Missing {
		t.Error("Missing = false, want true")
	}
}

func TestUpcomingEvent_Unmarshal(t *testing.T) {
	raw := `{
		"title": "BST Due",
		"type": "assignment",
		"start_at": "2025-10-15T03:59:00Z",
		"assignment": {
			"id": 500,
			"name": "Binary Search Trees",
			"course_id": 100
		}
	}`

	var ev UpcomingEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if ev.Type != "assignment" {
		t.Errorf("Type = %q, want assignment", ev.Type)
	}
	if ev.Assignment == nil || ev.Assignment.ID != 500 {
		t.Errorf("Assignment = %v, want {ID:500}", ev.Assignment)
	}
}

func TestUpcomingEvent_Unmarshal_StringID(t *testing.T) {
	// Canvas returns string IDs like "assignment_500" on upcoming_events
	raw := `{
		"id": "assignment_500",
		"title": "BST Due",
		"type": "assignment",
		"assignment": {"id": 500, "name": "BST"}
	}`

	var ev UpcomingEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("Unmarshal error: %v (should handle string IDs)", err)
	}
	if ev.Assignment == nil || ev.Assignment.ID != 500 {
		t.Errorf("Assignment = %v, want {ID:500}", ev.Assignment)
	}
}

func TestTodoItem_Unmarshal(t *testing.T) {
	raw := `{
		"type": "submitting",
		"assignment": {
			"id": 500,
			"name": "BST",
			"due_at": "2025-10-15T03:59:00Z"
		},
		"context_type": "Course",
		"context_name": "CSC108",
		"course_id": 100,
		"html_url": "https://q.utoronto.ca/courses/100/assignments/500"
	}`

	var item TodoItem
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if item.Type != "submitting" {
		t.Errorf("Type = %q, want submitting", item.Type)
	}
	if item.Assignment == nil {
		t.Fatal("Assignment is nil")
	}
	if item.Assignment.DueAt == nil {
		t.Error("Assignment.DueAt is nil, want non-nil")
	}
	if item.CourseID == nil || *item.CourseID != 100 {
		t.Errorf("CourseID = %v, want 100", item.CourseID)
	}
}
