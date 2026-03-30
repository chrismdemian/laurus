package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type graphqlRequest struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
	OperationName string         `json:"operationName"`
}

func TestQueryCourseSummariesGraphQL_PaginatesAndKeepsBestEnrollment(t *testing.T) {
	t.Parallel()

	var gqlCalls int
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users/self/profile":
			_, _ = fmt.Fprint(w, `{"id":42,"name":"Test User"}`)
		case "/api/graphql":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("Authorization = %q, want Bearer test-token", got)
			}
			if got := r.Header.Get("User-Agent"); got != "Laurus/test" {
				t.Fatalf("User-Agent = %q, want Laurus/test", got)
			}

			var req graphqlRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if req.OperationName != "UserCourseSummaries" {
				t.Fatalf("operationName = %q, want UserCourseSummaries", req.OperationName)
			}
			if after, ok := req.Variables["after"]; ok && after != nil && gqlCalls == 0 {
				t.Fatalf("first request after = %v, want nil", after)
			}

			gqlCalls++
			if gqlCalls == 1 {
				_, _ = fmt.Fprint(w, `{"data":{"legacyNode":{"enrollmentsConnection":{"nodes":[
					{
						"_id":"5001",
						"type":"TeacherEnrollment",
						"enrollmentState":"deleted",
						"grades":null,
						"course":{"_id":"100","name":"Algorithms","courseCode":"CSC263","state":"available"}
					},
					{
						"_id":"5002",
						"type":"StudentEnrollment",
						"enrollmentState":"active",
						"grades":{"currentScore":87.5,"currentGrade":"A","finalScore":86.1,"finalGrade":"A"},
						"course":{"_id":"100","name":"Algorithms","courseCode":"CSC263","state":"available"}
					}
				],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-2"}}}}}`)
				return
			}

			if after := req.Variables["after"]; after != "cursor-2" {
				t.Fatalf("second request after = %v, want cursor-2", after)
			}

			_, _ = fmt.Fprint(w, `{"data":{"legacyNode":{"enrollmentsConnection":{"nodes":[
				{
					"_id":"6001",
					"type":"StudentEnrollment",
					"enrollmentState":"inactive",
					"grades":{"currentScore":91.2,"currentGrade":"A+","finalScore":91.2,"finalGrade":"A+"},
					"course":{"_id":"200","name":"Databases","courseCode":"CSC343","state":"completed"}
				}
			],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	got, err := QueryCourseSummariesGraphQL(context.Background(), client, GraphQLCourseListOptions{})
	if err != nil {
		t.Fatalf("QueryCourseSummariesGraphQL error: %v", err)
	}
	if gqlCalls != 2 {
		t.Fatalf("graphql calls = %d, want 2", gqlCalls)
	}
	if len(got) != 1 {
		t.Fatalf("got %d courses, want 1 active course", len(got))
	}
	if got[0].ID != 100 {
		t.Fatalf("course ID = %d, want 100", got[0].ID)
	}
	if len(got[0].Enrollments) != 1 {
		t.Fatalf("enrollments = %d, want 1", len(got[0].Enrollments))
	}
	enrollment := got[0].Enrollments[0]
	if enrollment.Type != "StudentEnrollment" {
		t.Fatalf("enrollment.Type = %q, want StudentEnrollment", enrollment.Type)
	}
	if enrollment.Grades == nil || enrollment.Grades.CurrentScore == nil || *enrollment.Grades.CurrentScore != 87.5 {
		t.Fatalf("grades = %+v, want current score 87.5", enrollment.Grades)
	}
}

func TestQueryCourseSummariesGraphQL_AllFiltersWorkflowStates(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users/self/profile":
			_, _ = fmt.Fprint(w, `{"id":42,"name":"Test User"}`)
		case "/api/graphql":
			_, _ = fmt.Fprint(w, `{"data":{"legacyNode":{"enrollmentsConnection":{"nodes":[
				{"_id":"1","type":"StudentEnrollment","enrollmentState":"active","grades":null,"course":{"_id":"100","name":"Algorithms","courseCode":"CSC263","state":"available"}},
				{"_id":"2","type":"StudentEnrollment","enrollmentState":"completed","grades":null,"course":{"_id":"200","name":"Databases","courseCode":"CSC343","state":"completed"}},
				{"_id":"3","type":"StudentEnrollment","enrollmentState":"active","grades":null,"course":{"_id":"300","name":"Draft Course","courseCode":"CSC399","state":"created"}}
			],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	got, err := QueryCourseSummariesGraphQL(context.Background(), client, GraphQLCourseListOptions{All: true})
	if err != nil {
		t.Fatalf("QueryCourseSummariesGraphQL error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d courses, want 2 available/completed courses", len(got))
	}
	if got[0].ID != 100 || got[1].ID != 200 {
		t.Fatalf("course IDs = [%d %d], want [100 200]", got[0].ID, got[1].ID)
	}
}

func TestQueryCourseSummariesGraphQL_FallsBackOnSchemaErrors(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users/self/profile":
			_, _ = fmt.Fprint(w, `{"id":42,"name":"Test User"}`)
		case "/api/graphql":
			_, _ = fmt.Fprint(w, `{"errors":[{"message":"field not found","extensions":{"code":"undefinedField"}}]}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	_, err := QueryCourseSummariesGraphQL(context.Background(), client, GraphQLCourseListOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsGraphQLFallback(err) {
		t.Fatalf("expected fallback error, got %v", err)
	}
	if !errors.Is(err, ErrGraphQLPartialData) {
		t.Fatalf("expected ErrGraphQLPartialData, got %v", err)
	}
}

func TestQueryDashboardAssignmentsGraphQL_MapsAssignments(t *testing.T) {
	t.Parallel()

	due := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	submitted := time.Date(2026, 3, 31, 10, 15, 0, 0, time.UTC).Format(time.RFC3339)
	posted := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users/self/profile":
			_, _ = fmt.Fprint(w, `{"id":42,"name":"Test User"}`)
		case "/api/graphql":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"operationName":"UserDashboardAssignments"`) {
				t.Fatalf("request body missing dashboard operation: %s", string(body))
			}
			_, _ = fmt.Fprintf(w, `{"data":{"legacyNode":{"enrollmentsConnection":{"nodes":[
				{
					"_id":"7001",
					"type":"StudentEnrollment",
					"enrollmentState":"active",
					"course":{
						"_id":"100",
						"name":"Algorithms",
						"courseCode":"CSC263",
						"state":"available",
						"assignmentsConnection":{
							"nodes":[
								{
									"_id":"500",
									"name":"Problem Set 4",
									"dueAt":%s,
									"pointsPossible":100,
									"submissionTypes":["online_upload"],
									"published":true,
									"omitFromFinalGrade":false,
									"htmlUrl":"https://canvas.example/courses/100/assignments/500",
									"submissionsConnection":{
										"nodes":[
											{
												"_id":"900",
												"score":92.5,
												"grade":"A-",
												"state":"graded",
												"submissionStatus":"graded",
												"submittedAt":%s,
												"postedAt":%s,
												"late":false,
												"missing":false,
												"excused":false,
												"attempt":2
											}
										],
										"pageInfo":{"hasNextPage":false,"endCursor":null}
									}
								}
							],
							"pageInfo":{"hasNextPage":false,"endCursor":null}
						}
					}
				}
			],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`, jsonQuote(due), jsonQuote(submitted), jsonQuote(posted))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	got, err := QueryDashboardAssignmentsGraphQL(context.Background(), client)
	if err != nil {
		t.Fatalf("QueryDashboardAssignmentsGraphQL error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d dashboard courses, want 1", len(got))
	}
	if got[0].Course.ID != 100 {
		t.Fatalf("course ID = %d, want 100", got[0].Course.ID)
	}
	if len(got[0].Assignments) != 1 {
		t.Fatalf("assignments = %d, want 1", len(got[0].Assignments))
	}
	assignment := got[0].Assignments[0]
	if assignment.ID != 500 {
		t.Fatalf("assignment ID = %d, want 500", assignment.ID)
	}
	if assignment.Submission == nil {
		t.Fatal("submission is nil, want mapped submission")
	}
	if assignment.Submission.WorkflowState != "graded" {
		t.Fatalf("workflow state = %q, want graded", assignment.Submission.WorkflowState)
	}
	if assignment.Submission.Attempt == nil || *assignment.Submission.Attempt != 2 {
		t.Fatalf("attempt = %v, want 2", assignment.Submission.Attempt)
	}
}

func TestQueryDashboardAssignmentsGraphQL_FallsBackOnTruncation(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users/self/profile":
			_, _ = fmt.Fprint(w, `{"id":42,"name":"Test User"}`)
		case "/api/graphql":
			_, _ = fmt.Fprint(w, `{"data":{"legacyNode":{"enrollmentsConnection":{"nodes":[
				{
					"_id":"7001",
					"type":"StudentEnrollment",
					"enrollmentState":"active",
					"course":{
						"_id":"100",
						"name":"Algorithms",
						"courseCode":"CSC263",
						"state":"available",
						"assignmentsConnection":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}
					}
				}
			],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	_, err := QueryDashboardAssignmentsGraphQL(context.Background(), client)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsGraphQLFallback(err) {
		t.Fatalf("expected fallback error, got %v", err)
	}
	if !errors.Is(err, ErrGraphQLTruncated) {
		t.Fatalf("expected ErrGraphQLTruncated, got %v", err)
	}
}

func TestQueryCourseGradesGraphQL_MapsGroups(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users/self/profile":
			_, _ = fmt.Fprint(w, `{"id":42,"name":"Test User"}`)
		case "/api/graphql":
			_, _ = fmt.Fprint(w, `{"data":{"course":{
				"_id":"100",
				"assignmentGroupsConnection":{
					"nodes":[
						{
							"_id":"10",
							"name":"Problem Sets",
							"groupWeight":25,
							"rules":{"dropLowest":1,"dropHighest":0,"neverDrop":[{"_id":"500"}]},
							"assignmentsConnection":{
								"nodes":[
									{
										"_id":"500",
										"name":"Problem Set 4",
										"pointsPossible":100,
										"published":true,
										"omitFromFinalGrade":false,
										"submissionsConnection":{
											"nodes":[
												{
													"_id":"900",
													"score":92.5,
													"grade":"A-",
													"excused":false,
													"state":"graded",
													"postedAt":"2026-03-31T12:00:00Z",
													"attempt":2
												}
											],
											"pageInfo":{"hasNextPage":false,"endCursor":null}
										}
									}
								],
								"pageInfo":{"hasNextPage":false,"endCursor":null}
							}
						}
					],
					"pageInfo":{"hasNextPage":false,"endCursor":null}
				}
			}}}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	got, err := QueryCourseGradesGraphQL(context.Background(), client, 100)
	if err != nil {
		t.Fatalf("QueryCourseGradesGraphQL error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d groups, want 1", len(got))
	}
	group := got[0]
	if group.ID != 10 {
		t.Fatalf("group ID = %d, want 10", group.ID)
	}
	if group.GroupWeight != 25 {
		t.Fatalf("group weight = %v, want 25", group.GroupWeight)
	}
	if group.Rules.DropLowest != 1 {
		t.Fatalf("drop lowest = %d, want 1", group.Rules.DropLowest)
	}
	if len(group.Rules.NeverDrop) != 1 || group.Rules.NeverDrop[0] != 500 {
		t.Fatalf("never drop = %v, want [500]", group.Rules.NeverDrop)
	}
	if len(group.Assignments) != 1 {
		t.Fatalf("assignments = %d, want 1", len(group.Assignments))
	}
	if group.Assignments[0].Submission == nil || group.Assignments[0].Submission.Score == nil || *group.Assignments[0].Submission.Score != 92.5 {
		t.Fatalf("submission = %+v, want score 92.5", group.Assignments[0].Submission)
	}
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
