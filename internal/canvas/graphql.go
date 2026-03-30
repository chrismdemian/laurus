package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	gql "github.com/hasura/go-graphql-client"
)

var (
	// ErrGraphQLFallback marks errors where the caller should retry via REST.
	ErrGraphQLFallback = errors.New("canvas: fall back to rest")
	// ErrGraphQLUnavailable indicates the GraphQL endpoint/query cannot be used reliably.
	ErrGraphQLUnavailable = errors.New("canvas: graphql unavailable")
	// ErrGraphQLPartialData indicates GraphQL returned data that is not complete enough to trust.
	ErrGraphQLPartialData = errors.New("canvas: graphql returned partial data")
	// ErrGraphQLTruncated indicates a paginated connection had more data than the query returned.
	ErrGraphQLTruncated = errors.New("canvas: graphql connection truncated")
)

// GraphQLFallbackError wraps a GraphQL-specific issue that should fall back to REST.
type GraphQLFallbackError struct {
	Operation string
	Reason    error
	Cause     error
}

func (e *GraphQLFallbackError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("canvas graphql %s: %v", e.Operation, e.Cause)
	}
	return fmt.Sprintf("canvas graphql %s: %v", e.Operation, e.Reason)
}

func (e *GraphQLFallbackError) Unwrap() error {
	return errors.Join(ErrGraphQLFallback, e.Reason, e.Cause)
}

// IsGraphQLFallback reports whether the caller should retry the operation via REST.
func IsGraphQLFallback(err error) bool {
	return errors.Is(err, ErrGraphQLFallback)
}

// GraphQLCourseListOptions controls filtering for the user-enrollments GraphQL path.
type GraphQLCourseListOptions struct {
	All           bool
	FavoritesOnly bool
}

// DashboardCourse is a course plus the assignments loaded through the GraphQL dashboard query.
type DashboardCourse struct {
	Course      Course
	Assignments []Assignment
}

const (
	userCourseSummariesQuery = `
query UserCourseSummaries($userID: ID!, $after: String, $first: Int!) {
  legacyNode(type: User, _id: $userID) {
    ... on User {
      enrollmentsConnection(first: $first, after: $after) {
        nodes {
          _id
          type
          enrollmentState
          grades {
            currentScore
            currentGrade
            finalScore
            finalGrade
          }
          course {
            _id
            name
            courseCode
            state
          }
        }
        pageInfo {
          hasNextPage
          endCursor
        }
      }
    }
  }
}`

	userDashboardAssignmentsQuery = `
query UserDashboardAssignments($userID: ID!, $after: String, $first: Int!, $assignmentFirst: Int!, $submissionFirst: Int!) {
  legacyNode(type: User, _id: $userID) {
    ... on User {
      enrollmentsConnection(first: $first, after: $after) {
        nodes {
          _id
          type
          enrollmentState
          course {
            _id
            name
            courseCode
            state
            assignmentsConnection(first: $assignmentFirst, filter: { userId: $userID }) {
              nodes {
                _id
                name
                dueAt
                pointsPossible
                submissionTypes
                published
                omitFromFinalGrade
                htmlUrl
                submissionsConnection(first: $submissionFirst, filter: { userId: $userID }) {
                  nodes {
                    _id
                    score
                    grade
                    state
                    submissionStatus
                    submittedAt
                    postedAt
                    late
                    missing
                    excused
                    attempt
                  }
                  pageInfo {
                    hasNextPage
                    endCursor
                  }
                }
              }
              pageInfo {
                hasNextPage
                endCursor
              }
            }
          }
        }
        pageInfo {
          hasNextPage
          endCursor
        }
      }
    }
  }
}`

	courseGradesQuery = `
query CourseGrades($courseID: ID!, $userID: ID!, $groupFirst: Int!, $assignmentFirst: Int!, $submissionFirst: Int!) {
  course(id: $courseID) {
    _id
    assignmentGroupsConnection(first: $groupFirst) {
      nodes {
        _id
        name
        groupWeight
        rules {
          dropLowest
          dropHighest
          neverDrop {
            _id
          }
        }
        assignmentsConnection(first: $assignmentFirst) {
          nodes {
            _id
            name
            pointsPossible
            published
            omitFromFinalGrade
            submissionsConnection(first: $submissionFirst, filter: { userId: $userID }) {
              nodes {
                _id
                score
                grade
                excused
                state
                postedAt
                attempt
              }
              pageInfo {
                hasNextPage
                endCursor
              }
            }
          }
          pageInfo {
            hasNextPage
            endCursor
          }
        }
      }
      pageInfo {
        hasNextPage
        endCursor
      }
    }
  }
}`
)

type gqlPageInfo struct {
	HasNextPage bool    `json:"hasNextPage"`
	EndCursor   *string `json:"endCursor"`
}

type gqlCourseNode struct {
	ID         string `json:"_id"`
	Name       string `json:"name"`
	CourseCode string `json:"courseCode"`
	State      string `json:"state"`
}

type gqlEnrollmentGrades struct {
	CurrentScore *float64 `json:"currentScore"`
	FinalScore   *float64 `json:"finalScore"`
	CurrentGrade *string  `json:"currentGrade"`
	FinalGrade   *string  `json:"finalGrade"`
}

type gqlUserEnrollmentNode struct {
	ID              string               `json:"_id"`
	Type            string               `json:"type"`
	EnrollmentState string               `json:"enrollmentState"`
	Grades          *gqlEnrollmentGrades `json:"grades"`
	Course          *gqlCourseNode       `json:"course"`
}

type gqlUserCoursesResponse struct {
	LegacyNode *struct {
		EnrollmentsConnection struct {
			Nodes    []gqlUserEnrollmentNode `json:"nodes"`
			PageInfo gqlPageInfo             `json:"pageInfo"`
		} `json:"enrollmentsConnection"`
	} `json:"legacyNode"`
}

type gqlDashboardAssignmentNode struct {
	ID                 string                    `json:"_id"`
	Name               string                    `json:"name"`
	DueAt              *time.Time                `json:"dueAt"`
	PointsPossible     *float64                  `json:"pointsPossible"`
	SubmissionTypes    []string                  `json:"submissionTypes"`
	Published          bool                      `json:"published"`
	OmitFromFinalGrade bool                      `json:"omitFromFinalGrade"`
	HTMLURL            string                    `json:"htmlUrl"`
	Submissions        gqlDashboardSubmissionSet `json:"submissionsConnection"`
}

type gqlDashboardSubmissionSet struct {
	Nodes    []gqlSubmissionNode `json:"nodes"`
	PageInfo gqlPageInfo         `json:"pageInfo"`
}

type gqlSubmissionNode struct {
	ID               string     `json:"_id"`
	Score            *float64   `json:"score"`
	Grade            *string    `json:"grade"`
	State            string     `json:"state"`
	SubmissionStatus string     `json:"submissionStatus"`
	SubmittedAt      *time.Time `json:"submittedAt"`
	PostedAt         *time.Time `json:"postedAt"`
	Late             bool       `json:"late"`
	Missing          bool       `json:"missing"`
	Excused          bool       `json:"excused"`
	Attempt          *int       `json:"attempt"`
}

type gqlDashboardCourseNode struct {
	ID          string                        `json:"_id"`
	Name        string                        `json:"name"`
	CourseCode  string                        `json:"courseCode"`
	State       string                        `json:"state"`
	Assignments gqlDashboardAssignmentsResult `json:"assignmentsConnection"`
}

type gqlDashboardAssignmentsResult struct {
	Nodes    []gqlDashboardAssignmentNode `json:"nodes"`
	PageInfo gqlPageInfo                  `json:"pageInfo"`
}

type gqlUserDashboardEnrollmentNode struct {
	ID              string                  `json:"_id"`
	Type            string                  `json:"type"`
	EnrollmentState string                  `json:"enrollmentState"`
	Course          *gqlDashboardCourseNode `json:"course"`
}

type gqlDashboardResponse struct {
	LegacyNode *struct {
		EnrollmentsConnection struct {
			Nodes    []gqlUserDashboardEnrollmentNode `json:"nodes"`
			PageInfo gqlPageInfo                      `json:"pageInfo"`
		} `json:"enrollmentsConnection"`
	} `json:"legacyNode"`
}

type gqlAssignmentGroupRuleRef struct {
	ID string `json:"_id"`
}

type gqlAssignmentGroupRules struct {
	DropLowest  *int                        `json:"dropLowest"`
	DropHighest *int                        `json:"dropHighest"`
	NeverDrop   []gqlAssignmentGroupRuleRef `json:"neverDrop"`
}

type gqlCourseGradeAssignmentNode struct {
	ID                 string                    `json:"_id"`
	Name               string                    `json:"name"`
	PointsPossible     *float64                  `json:"pointsPossible"`
	Published          bool                      `json:"published"`
	OmitFromFinalGrade bool                      `json:"omitFromFinalGrade"`
	Submissions        gqlDashboardSubmissionSet `json:"submissionsConnection"`
}

type gqlCourseGradeAssignmentsResult struct {
	Nodes    []gqlCourseGradeAssignmentNode `json:"nodes"`
	PageInfo gqlPageInfo                    `json:"pageInfo"`
}

type gqlCourseGradeGroupNode struct {
	ID          string                          `json:"_id"`
	Name        string                          `json:"name"`
	GroupWeight *float64                        `json:"groupWeight"`
	Rules       *gqlAssignmentGroupRules        `json:"rules"`
	Assignments gqlCourseGradeAssignmentsResult `json:"assignmentsConnection"`
}

type gqlCourseGradeGroupsResult struct {
	Nodes    []gqlCourseGradeGroupNode `json:"nodes"`
	PageInfo gqlPageInfo               `json:"pageInfo"`
}

type gqlCourseGradesResponse struct {
	Course *struct {
		ID               string                     `json:"_id"`
		AssignmentGroups gqlCourseGradeGroupsResult `json:"assignmentGroupsConnection"`
	} `json:"course"`
}

// QueryCourseSummariesGraphQL loads course summary data from the current user's
// enrollments, mapping it into the existing Course model.
func QueryCourseSummariesGraphQL(ctx context.Context, c *Client, opts GraphQLCourseListOptions) ([]Course, error) {
	if opts.FavoritesOnly {
		return nil, &GraphQLFallbackError{
			Operation: "UserCourseSummaries",
			Reason:    ErrGraphQLUnavailable,
			Cause:     errors.New("favorites filter is not implemented on the graphql path"),
		}
	}

	userID, err := c.currentUserIDValue(ctx)
	if err != nil {
		return nil, err
	}

	acc := make(map[int64]Course)
	var after *string

	for {
		var resp gqlUserCoursesResponse
		headers, err := execGraphQLJSON(ctx, c, "UserCourseSummaries", userCourseSummariesQuery, map[string]any{
			"userID": strconv.FormatInt(userID, 10),
			"after":  after,
			"first":  100,
		}, &resp)
		_ = headers
		if err != nil {
			return nil, err
		}
		if resp.LegacyNode == nil {
			return nil, &GraphQLFallbackError{
				Operation: "UserCourseSummaries",
				Reason:    ErrGraphQLPartialData,
				Cause:     errors.New("graphql user lookup returned no data"),
			}
		}

		for _, node := range resp.LegacyNode.EnrollmentsConnection.Nodes {
			course, enrollment, err := mapGraphQLCourseSummary(node)
			if err != nil {
				return nil, &GraphQLFallbackError{
					Operation: "UserCourseSummaries",
					Reason:    ErrGraphQLPartialData,
					Cause:     err,
				}
			}
			if existing, ok := acc[course.ID]; ok {
				best := pickBetterEnrollment(existing.Enrollments[0], enrollment)
				existing.Enrollments = []Enrollment{best}
				acc[course.ID] = existing
				continue
			}
			course.Enrollments = []Enrollment{enrollment}
			acc[course.ID] = course
		}

		if !resp.LegacyNode.EnrollmentsConnection.PageInfo.HasNextPage {
			break
		}
		if resp.LegacyNode.EnrollmentsConnection.PageInfo.EndCursor == nil || *resp.LegacyNode.EnrollmentsConnection.PageInfo.EndCursor == "" {
			return nil, &GraphQLFallbackError{
				Operation: "UserCourseSummaries",
				Reason:    ErrGraphQLTruncated,
				Cause:     errors.New("graphql enrollments pagination missing end cursor"),
			}
		}
		after = resp.LegacyNode.EnrollmentsConnection.PageInfo.EndCursor
	}

	courses := make([]Course, 0, len(acc))
	for _, course := range acc {
		if !opts.All {
			if len(course.Enrollments) == 0 || course.Enrollments[0].EnrollmentState != "active" {
				continue
			}
		} else {
			switch course.WorkflowState {
			case "available", "completed":
				// Match the REST path's state[]=available&state[]=completed behavior.
			default:
				continue
			}
		}
		courses = append(courses, course)
	}

	sort.Slice(courses, func(i, j int) bool {
		if courses[i].CourseCode != courses[j].CourseCode {
			return courses[i].CourseCode < courses[j].CourseCode
		}
		return courses[i].Name < courses[j].Name
	})

	return courses, nil
}

// QueryDashboardAssignmentsGraphQL loads current-user courses plus per-course
// assignment summaries for the cross-course assignments command.
func QueryDashboardAssignmentsGraphQL(ctx context.Context, c *Client) ([]DashboardCourse, error) {
	userID, err := c.currentUserIDValue(ctx)
	if err != nil {
		return nil, err
	}

	type courseAccum struct {
		Course      Course
		Assignments map[int64]Assignment
	}

	acc := make(map[int64]*courseAccum)
	var after *string

	for {
		var resp gqlDashboardResponse
		_, err := execGraphQLJSON(ctx, c, "UserDashboardAssignments", userDashboardAssignmentsQuery, map[string]any{
			"userID":          strconv.FormatInt(userID, 10),
			"after":           after,
			"first":           100,
			"assignmentFirst": 200,
			"submissionFirst": 2,
		}, &resp)
		if err != nil {
			return nil, err
		}
		if resp.LegacyNode == nil {
			return nil, &GraphQLFallbackError{
				Operation: "UserDashboardAssignments",
				Reason:    ErrGraphQLPartialData,
				Cause:     errors.New("graphql user dashboard returned no data"),
			}
		}

		for _, node := range resp.LegacyNode.EnrollmentsConnection.Nodes {
			if node.Course == nil {
				return nil, &GraphQLFallbackError{
					Operation: "UserDashboardAssignments",
					Reason:    ErrGraphQLPartialData,
					Cause:     errors.New("graphql enrollment missing course"),
				}
			}
			if node.EnrollmentState != "active" {
				continue
			}
			courseID, err := parseGraphQLID(node.Course.ID)
			if err != nil {
				return nil, &GraphQLFallbackError{
					Operation: "UserDashboardAssignments",
					Reason:    ErrGraphQLPartialData,
					Cause:     fmt.Errorf("parsing course id %q: %w", node.Course.ID, err),
				}
			}
			if node.Course.Assignments.PageInfo.HasNextPage {
				return nil, &GraphQLFallbackError{
					Operation: "UserDashboardAssignments",
					Reason:    ErrGraphQLTruncated,
					Cause:     fmt.Errorf("course %d assignments exceeded graphql page size", courseID),
				}
			}

			entry, ok := acc[courseID]
			if !ok {
				entry = &courseAccum{
					Course: Course{
						ID:            courseID,
						Name:          node.Course.Name,
						CourseCode:    node.Course.CourseCode,
						WorkflowState: node.Course.State,
					},
					Assignments: make(map[int64]Assignment),
				}
				acc[courseID] = entry
			}

			for _, a := range node.Course.Assignments.Nodes {
				assignment, err := mapGraphQLAssignment(courseID, a)
				if err != nil {
					return nil, &GraphQLFallbackError{
						Operation: "UserDashboardAssignments",
						Reason:    ErrGraphQLPartialData,
						Cause:     err,
					}
				}
				entry.Assignments[assignment.ID] = assignment
			}
		}

		if !resp.LegacyNode.EnrollmentsConnection.PageInfo.HasNextPage {
			break
		}
		if resp.LegacyNode.EnrollmentsConnection.PageInfo.EndCursor == nil || *resp.LegacyNode.EnrollmentsConnection.PageInfo.EndCursor == "" {
			return nil, &GraphQLFallbackError{
				Operation: "UserDashboardAssignments",
				Reason:    ErrGraphQLTruncated,
				Cause:     errors.New("graphql dashboard pagination missing end cursor"),
			}
		}
		after = resp.LegacyNode.EnrollmentsConnection.PageInfo.EndCursor
	}

	var out []DashboardCourse
	for _, entry := range acc {
		assignments := make([]Assignment, 0, len(entry.Assignments))
		for _, assignment := range entry.Assignments {
			assignments = append(assignments, assignment)
		}
		sort.Slice(assignments, func(i, j int) bool {
			di, dj := assignments[i].DueAt, assignments[j].DueAt
			if di == nil && dj == nil {
				return assignments[i].Name < assignments[j].Name
			}
			if di == nil {
				return false
			}
			if dj == nil {
				return true
			}
			return di.Before(*dj)
		})
		out = append(out, DashboardCourse{
			Course:      entry.Course,
			Assignments: assignments,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Course.CourseCode != out[j].Course.CourseCode {
			return out[i].Course.CourseCode < out[j].Course.CourseCode
		}
		return out[i].Course.Name < out[j].Course.Name
	})

	return out, nil
}

// QueryCourseGradesGraphQL loads assignment groups, assignments, and the
// current user's submission state for a single course.
func QueryCourseGradesGraphQL(ctx context.Context, c *Client, courseID int64) ([]AssignmentGroup, error) {
	userID, err := c.currentUserIDValue(ctx)
	if err != nil {
		return nil, err
	}

	var resp gqlCourseGradesResponse
	_, err = execGraphQLJSON(ctx, c, "CourseGrades", courseGradesQuery, map[string]any{
		"courseID":        strconv.FormatInt(courseID, 10),
		"userID":          strconv.FormatInt(userID, 10),
		"groupFirst":      200,
		"assignmentFirst": 300,
		"submissionFirst": 2,
	}, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Course == nil {
		return nil, &GraphQLFallbackError{
			Operation: "CourseGrades",
			Reason:    ErrGraphQLPartialData,
			Cause:     errors.New("graphql course grades returned no course"),
		}
	}
	if resp.Course.AssignmentGroups.PageInfo.HasNextPage {
		return nil, &GraphQLFallbackError{
			Operation: "CourseGrades",
			Reason:    ErrGraphQLTruncated,
			Cause:     fmt.Errorf("course %d assignment groups exceeded graphql page size", courseID),
		}
	}

	groups := make([]AssignmentGroup, 0, len(resp.Course.AssignmentGroups.Nodes))
	for _, node := range resp.Course.AssignmentGroups.Nodes {
		if node.Assignments.PageInfo.HasNextPage {
			return nil, &GraphQLFallbackError{
				Operation: "CourseGrades",
				Reason:    ErrGraphQLTruncated,
				Cause:     fmt.Errorf("assignment group %q exceeded graphql page size", node.Name),
			}
		}
		group, err := mapGraphQLAssignmentGroup(courseID, node)
		if err != nil {
			return nil, &GraphQLFallbackError{
				Operation: "CourseGrades",
				Reason:    ErrGraphQLPartialData,
				Cause:     err,
			}
		}
		groups = append(groups, group)
	}

	return groups, nil
}

func execGraphQLJSON(ctx context.Context, c *Client, operation, query string, variables map[string]any, dest any) (http.Header, error) {
	headers := http.Header{}
	if c == nil || !c.gqlEnabled || c.gqlClient == nil {
		return headers, &GraphQLFallbackError{
			Operation: operation,
			Reason:    ErrGraphQLUnavailable,
			Cause:     errors.New("graphql is disabled"),
		}
	}
	data, err := c.gqlClient.ExecRaw(ctx, query, variables, gql.OperationName(operation), gql.BindResponseHeaders(&headers))
	if err != nil {
		return headers, classifyGraphQLError(operation, err)
	}
	if len(data) == 0 {
		return headers, &GraphQLFallbackError{
			Operation: operation,
			Reason:    ErrGraphQLPartialData,
			Cause:     errors.New("graphql returned empty data payload"),
		}
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return headers, &GraphQLFallbackError{
			Operation: operation,
			Reason:    ErrGraphQLPartialData,
			Cause:     fmt.Errorf("decoding graphql data: %w", err),
		}
	}
	return headers, nil
}

func classifyGraphQLError(operation string, err error) error {
	var gqlErrs gql.Errors
	if errors.As(err, &gqlErrs) {
		reason := ErrGraphQLUnavailable
		for _, gqlErr := range gqlErrs {
			if code, _ := gqlErr.Extensions["code"].(string); code == "selectionMismatch" || code == "undefinedField" || code == "argumentNotAccepted" || code == "variableMismatch" {
				reason = ErrGraphQLPartialData
				break
			}
		}
		return &GraphQLFallbackError{
			Operation: operation,
			Reason:    reason,
			Cause:     gqlErrs,
		}
	}

	return &GraphQLFallbackError{
		Operation: operation,
		Reason:    ErrGraphQLUnavailable,
		Cause:     err,
	}
}

func mapGraphQLCourseSummary(node gqlUserEnrollmentNode) (Course, Enrollment, error) {
	if node.Course == nil {
		return Course{}, Enrollment{}, errors.New("graphql enrollment missing course")
	}

	courseID, err := parseGraphQLID(node.Course.ID)
	if err != nil {
		return Course{}, Enrollment{}, fmt.Errorf("parsing course id %q: %w", node.Course.ID, err)
	}
	enrollmentID, err := parseGraphQLID(node.ID)
	if err != nil {
		return Course{}, Enrollment{}, fmt.Errorf("parsing enrollment id %q: %w", node.ID, err)
	}

	var grades *EnrollmentGrades
	if node.Grades != nil {
		grades = &EnrollmentGrades{
			CurrentScore: node.Grades.CurrentScore,
			FinalScore:   node.Grades.FinalScore,
			CurrentGrade: node.Grades.CurrentGrade,
			FinalGrade:   node.Grades.FinalGrade,
		}
	}

	return Course{
			ID:            courseID,
			Name:          node.Course.Name,
			CourseCode:    node.Course.CourseCode,
			WorkflowState: node.Course.State,
		}, Enrollment{
			ID:              enrollmentID,
			CourseID:        courseID,
			Type:            node.Type,
			EnrollmentState: node.EnrollmentState,
			Grades:          grades,
		}, nil
}

func mapGraphQLAssignment(courseID int64, node gqlDashboardAssignmentNode) (Assignment, error) {
	assignmentID, err := parseGraphQLID(node.ID)
	if err != nil {
		return Assignment{}, fmt.Errorf("parsing assignment id %q: %w", node.ID, err)
	}
	if node.Submissions.PageInfo.HasNextPage {
		return Assignment{}, fmt.Errorf("assignment %d submissions exceeded graphql page size", assignmentID)
	}
	if len(node.Submissions.Nodes) > 1 {
		return Assignment{}, fmt.Errorf("assignment %d returned multiple submissions for current user", assignmentID)
	}

	var submission *Submission
	if len(node.Submissions.Nodes) == 1 {
		sub, err := mapGraphQLSubmission(courseID, assignmentID, node.Submissions.Nodes[0])
		if err != nil {
			return Assignment{}, err
		}
		submission = &sub
	}

	return Assignment{
		ID:                 assignmentID,
		Name:               node.Name,
		DueAt:              node.DueAt,
		PointsPossible:     node.PointsPossible,
		SubmissionTypes:    node.SubmissionTypes,
		Published:          node.Published,
		OmitFromFinalGrade: node.OmitFromFinalGrade,
		CourseID:           courseID,
		HTMLURL:            node.HTMLURL,
		Submission:         submission,
		Missing:            submission != nil && submission.Missing,
	}, nil
}

func mapGraphQLSubmission(courseID, assignmentID int64, node gqlSubmissionNode) (Submission, error) {
	submissionID, err := parseGraphQLID(node.ID)
	if err != nil {
		return Submission{}, fmt.Errorf("parsing submission id %q: %w", node.ID, err)
	}

	workflowState := node.State
	if workflowState == "" {
		workflowState = node.SubmissionStatus
	}

	return Submission{
		ID:            submissionID,
		AssignmentID:  assignmentID,
		Score:         node.Score,
		Grade:         node.Grade,
		SubmittedAt:   node.SubmittedAt,
		WorkflowState: workflowState,
		Late:          node.Late,
		Missing:       node.Missing,
		Excused:       node.Excused,
		PostedAt:      node.PostedAt,
		Attempt:       node.Attempt,
	}, nil
}

func mapGraphQLAssignmentGroup(courseID int64, node gqlCourseGradeGroupNode) (AssignmentGroup, error) {
	groupID, err := parseGraphQLID(node.ID)
	if err != nil {
		return AssignmentGroup{}, fmt.Errorf("parsing assignment group id %q: %w", node.ID, err)
	}

	group := AssignmentGroup{
		ID:   groupID,
		Name: node.Name,
	}
	if node.GroupWeight != nil {
		group.GroupWeight = *node.GroupWeight
	}
	if node.Rules != nil {
		if node.Rules.DropLowest != nil {
			group.Rules.DropLowest = *node.Rules.DropLowest
		}
		if node.Rules.DropHighest != nil {
			group.Rules.DropHighest = *node.Rules.DropHighest
		}
		for _, ref := range node.Rules.NeverDrop {
			id, err := parseGraphQLID(ref.ID)
			if err != nil {
				return AssignmentGroup{}, fmt.Errorf("parsing neverDrop assignment id %q: %w", ref.ID, err)
			}
			group.Rules.NeverDrop = append(group.Rules.NeverDrop, id)
		}
	}

	for _, assignmentNode := range node.Assignments.Nodes {
		if assignmentNode.Submissions.PageInfo.HasNextPage {
			return AssignmentGroup{}, fmt.Errorf("assignment %q submissions exceeded graphql page size", assignmentNode.Name)
		}
		if len(assignmentNode.Submissions.Nodes) > 1 {
			return AssignmentGroup{}, fmt.Errorf("assignment %q returned multiple submissions for current user", assignmentNode.Name)
		}

		assignmentID, err := parseGraphQLID(assignmentNode.ID)
		if err != nil {
			return AssignmentGroup{}, fmt.Errorf("parsing assignment id %q: %w", assignmentNode.ID, err)
		}

		assignment := Assignment{
			ID:                 assignmentID,
			Name:               assignmentNode.Name,
			PointsPossible:     assignmentNode.PointsPossible,
			Published:          assignmentNode.Published,
			OmitFromFinalGrade: assignmentNode.OmitFromFinalGrade,
			CourseID:           courseID,
			AssignmentGroupID:  groupID,
		}

		if len(assignmentNode.Submissions.Nodes) == 1 {
			submission, err := mapGraphQLSubmission(courseID, assignmentID, assignmentNode.Submissions.Nodes[0])
			if err != nil {
				return AssignmentGroup{}, err
			}
			assignment.Submission = &submission
		}

		group.Assignments = append(group.Assignments, assignment)
	}

	return group, nil
}

func parseGraphQLID(id string) (int64, error) {
	return strconv.ParseInt(id, 10, 64)
}

func pickBetterEnrollment(a, b Enrollment) Enrollment {
	if enrollmentRank(b) > enrollmentRank(a) {
		return b
	}
	return a
}

func enrollmentRank(e Enrollment) int {
	score := 0
	if e.EnrollmentState == "active" {
		score += 100
	}
	if e.Type == "StudentEnrollment" || e.Type == "student" {
		score += 50
	}
	if e.Grades != nil {
		score += 10
		if e.Grades.CurrentScore != nil || e.Grades.CurrentGrade != nil || e.Grades.FinalScore != nil || e.Grades.FinalGrade != nil {
			score += 10
		}
	}
	return score
}
