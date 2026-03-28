package canvas

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCourse_Unmarshal(t *testing.T) {
	raw := `{
		"id": 12345,
		"name": "Introduction to Computer Science",
		"course_code": "CSC108",
		"enrollment_term_id": 99,
		"start_at": "2025-09-01T04:00:00Z",
		"end_at": "2025-12-15T05:00:00Z",
		"time_zone": "America/Toronto",
		"workflow_state": "available",
		"uuid": "abc123",
		"html_url": "https://q.utoronto.ca/courses/12345",
		"teachers": [{"id": 1, "name": "Lisa Smith", "short_name": "Lisa"}],
		"total_students": 450,
		"enrollments": [{
			"id": 999,
			"user_id": 42,
			"course_id": 12345,
			"type": "StudentEnrollment",
			"enrollment_state": "active",
			"grades": {
				"current_score": 87.5,
				"final_score": 72.3,
				"current_grade": "A",
				"final_grade": "B"
			}
		}]
	}`

	var c Course
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if c.ID != 12345 {
		t.Errorf("ID = %d, want 12345", c.ID)
	}
	if c.CourseCode != "CSC108" {
		t.Errorf("CourseCode = %q, want CSC108", c.CourseCode)
	}
	if c.StartAt == nil {
		t.Fatal("StartAt is nil, want non-nil")
	}
	wantStart := time.Date(2025, 9, 1, 4, 0, 0, 0, time.UTC)
	if !c.StartAt.Equal(wantStart) {
		t.Errorf("StartAt = %v, want %v", c.StartAt, wantStart)
	}
	if c.SyllabusBody != nil {
		t.Errorf("SyllabusBody = %v, want nil (not included)", c.SyllabusBody)
	}
	if len(c.Teachers) != 1 || c.Teachers[0].Name != "Lisa Smith" {
		t.Errorf("Teachers = %v, want [{Lisa Smith}]", c.Teachers)
	}
	if c.TotalStudents == nil || *c.TotalStudents != 450 {
		t.Errorf("TotalStudents = %v, want 450", c.TotalStudents)
	}
	if len(c.Enrollments) != 1 {
		t.Fatalf("Enrollments length = %d, want 1", len(c.Enrollments))
	}
	e := c.Enrollments[0]
	if e.Type != "StudentEnrollment" {
		t.Errorf("Enrollment.Type = %q, want StudentEnrollment", e.Type)
	}
	if e.Grades == nil {
		t.Fatal("Enrollment.Grades is nil")
	}
	if e.Grades.CurrentScore == nil || *e.Grades.CurrentScore != 87.5 {
		t.Errorf("CurrentScore = %v, want 87.5", e.Grades.CurrentScore)
	}
	if e.Grades.CurrentGrade == nil || *e.Grades.CurrentGrade != "A" {
		t.Errorf("CurrentGrade = %v, want A", e.Grades.CurrentGrade)
	}
}

func TestCourse_Unmarshal_NullFields(t *testing.T) {
	raw := `{
		"id": 1,
		"name": "Test",
		"course_code": "TST",
		"start_at": null,
		"end_at": null,
		"syllabus_body": null,
		"total_students": null,
		"workflow_state": "available",
		"teachers": null,
		"enrollments": null
	}`

	var c Course
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if c.StartAt != nil {
		t.Errorf("StartAt = %v, want nil", c.StartAt)
	}
	if c.EndAt != nil {
		t.Errorf("EndAt = %v, want nil", c.EndAt)
	}
	if c.SyllabusBody != nil {
		t.Errorf("SyllabusBody = %v, want nil", c.SyllabusBody)
	}
	if c.TotalStudents != nil {
		t.Errorf("TotalStudents = %v, want nil", c.TotalStudents)
	}
	if c.Teachers != nil {
		t.Errorf("Teachers = %v, want nil", c.Teachers)
	}
	if c.Enrollments != nil {
		t.Errorf("Enrollments = %v, want nil", c.Enrollments)
	}
}

func TestAssignment_Unmarshal(t *testing.T) {
	raw := `{
		"id": 500,
		"name": "Binary Search Trees",
		"description": "<p>Implement a BST.</p>",
		"due_at": "2025-10-15T03:59:00Z",
		"lock_at": null,
		"unlock_at": null,
		"points_possible": 100.0,
		"grading_type": "points",
		"submission_types": ["online_upload", "online_text_entry"],
		"assignment_group_id": 10,
		"published": true,
		"omit_from_final_grade": false,
		"course_id": 12345,
		"html_url": "https://q.utoronto.ca/courses/12345/assignments/500",
		"position": 3,
		"rubric": [
			{
				"id": "r1",
				"description": "Correctness",
				"long_description": "All tests pass",
				"points": 80.0,
				"ratings": [
					{"id": "r1_5", "description": "Full Marks", "points": 80.0},
					{"id": "r1_0", "description": "No Marks", "points": 0.0}
				]
			}
		]
	}`

	var a Assignment
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if a.ID != 500 {
		t.Errorf("ID = %d, want 500", a.ID)
	}
	if a.Description == nil || *a.Description != "<p>Implement a BST.</p>" {
		t.Errorf("Description = %v, want <p>Implement a BST.</p>", a.Description)
	}
	if a.DueAt == nil {
		t.Fatal("DueAt is nil, want non-nil")
	}
	if a.LockAt != nil {
		t.Errorf("LockAt = %v, want nil", a.LockAt)
	}
	if a.PointsPossible == nil || *a.PointsPossible != 100.0 {
		t.Errorf("PointsPossible = %v, want 100.0", a.PointsPossible)
	}
	if len(a.SubmissionTypes) != 2 {
		t.Errorf("SubmissionTypes length = %d, want 2", len(a.SubmissionTypes))
	}
	if len(a.Rubric) != 1 {
		t.Fatalf("Rubric length = %d, want 1", len(a.Rubric))
	}
	if a.Rubric[0].Points != 80.0 {
		t.Errorf("Rubric[0].Points = %f, want 80.0", a.Rubric[0].Points)
	}
	if len(a.Rubric[0].Ratings) != 2 {
		t.Errorf("Rubric[0].Ratings length = %d, want 2", len(a.Rubric[0].Ratings))
	}
}

func TestSubmission_Unmarshal(t *testing.T) {
	raw := `{
		"id": 777,
		"assignment_id": 500,
		"user_id": 42,
		"score": 85.5,
		"grade": "85.5",
		"submitted_at": "2025-10-14T22:00:00Z",
		"workflow_state": "graded",
		"late": false,
		"missing": false,
		"excused": false,
		"posted_at": "2025-10-20T12:00:00Z",
		"attempt": 2,
		"submission_comments": [
			{
				"id": 1001,
				"author_id": 1,
				"author_name": "Lisa Smith",
				"comment": "Great work!",
				"created_at": "2025-10-20T12:01:00Z"
			}
		],
		"rubric_assessment": {
			"r1": {"points": 75.0, "comments": "Minor bug", "rating_id": "r1_5"}
		}
	}`

	var s Submission
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if s.Score == nil || *s.Score != 85.5 {
		t.Errorf("Score = %v, want 85.5", s.Score)
	}
	if s.WorkflowState != "graded" {
		t.Errorf("WorkflowState = %q, want graded", s.WorkflowState)
	}
	if s.Attempt == nil || *s.Attempt != 2 {
		t.Errorf("Attempt = %v, want 2", s.Attempt)
	}
	if s.PostedAt == nil {
		t.Fatal("PostedAt is nil, want non-nil")
	}
	if len(s.SubmissionComments) != 1 {
		t.Fatalf("SubmissionComments length = %d, want 1", len(s.SubmissionComments))
	}
	if s.SubmissionComments[0].Author != "Lisa Smith" {
		t.Errorf("Comment author = %q, want Lisa Smith", s.SubmissionComments[0].Author)
	}
	ra, ok := s.RubricAssessment["r1"]
	if !ok {
		t.Fatal("RubricAssessment missing key r1")
	}
	if ra.Points == nil || *ra.Points != 75.0 {
		t.Errorf("RubricAssessment[r1].Points = %v, want 75.0", ra.Points)
	}
}

func TestSubmission_Unmarshal_Ungraded(t *testing.T) {
	raw := `{
		"id": 778,
		"assignment_id": 501,
		"user_id": 42,
		"score": null,
		"grade": null,
		"submitted_at": null,
		"workflow_state": "unsubmitted",
		"late": false,
		"missing": true,
		"excused": false,
		"posted_at": null,
		"attempt": null
	}`

	var s Submission
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if s.Score != nil {
		t.Errorf("Score = %v, want nil", s.Score)
	}
	if s.Grade != nil {
		t.Errorf("Grade = %v, want nil", s.Grade)
	}
	if s.SubmittedAt != nil {
		t.Errorf("SubmittedAt = %v, want nil", s.SubmittedAt)
	}
	if s.PostedAt != nil {
		t.Errorf("PostedAt = %v, want nil", s.PostedAt)
	}
	if !s.Missing {
		t.Error("Missing = false, want true")
	}
}

func TestEnrollment_Unmarshal_NoGrades(t *testing.T) {
	raw := `{
		"id": 1,
		"user_id": 42,
		"course_id": 100,
		"type": "StudentEnrollment",
		"enrollment_state": "active"
	}`

	var e Enrollment
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if e.Grades != nil {
		t.Errorf("Grades = %v, want nil (no grades included)", e.Grades)
	}
}

func TestAssignmentGroup_Unmarshal(t *testing.T) {
	raw := `{
		"id": 10,
		"name": "Assignments",
		"group_weight": 40.0,
		"rules": {
			"drop_lowest": 1,
			"drop_highest": 0,
			"never_drop": [500, 501]
		}
	}`

	var g AssignmentGroup
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if g.GroupWeight != 40.0 {
		t.Errorf("GroupWeight = %f, want 40.0", g.GroupWeight)
	}
	if g.Rules.DropLowest != 1 {
		t.Errorf("DropLowest = %d, want 1", g.Rules.DropLowest)
	}
	if len(g.Rules.NeverDrop) != 2 || g.Rules.NeverDrop[0] != 500 {
		t.Errorf("NeverDrop = %v, want [500 501]", g.Rules.NeverDrop)
	}
}
