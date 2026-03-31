package mcp

import (
	"context"
	"fmt"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/shopspring/decimal"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/pkg/grade"
)

func (s *Server) registerGradeTools(srv *server.MCPServer) {
	srv.AddTool(
		mcplib.NewTool("list_grades",
			mcplib.WithDescription("List current grades across all enrolled courses."),
			mcplib.WithBoolean("include_completed",
				mcplib.Description("Include completed/past courses"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleListGrades),
	)

	srv.AddTool(
		mcplib.NewTool("get_grades",
			mcplib.WithDescription("Get detailed per-assignment grade breakdown for a specific course, including group weights and drop rules."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
		),
		mcplib.NewTypedToolHandler(s.handleGetGrades),
	)

	srv.AddTool(
		mcplib.NewTool("calculate_what_if",
			mcplib.WithDescription("Calculate hypothetical grade scenarios. Provide assignment IDs and hypothetical scores to see how they would affect the course grade."),
			mcplib.WithString("course",
				mcplib.Required(),
				mcplib.Description("Course name, code, or ID"),
			),
			mcplib.WithArray("scenarios",
				mcplib.Required(),
				mcplib.Description("Array of {assignment_id, score} objects representing hypothetical scores"),
				mcplib.Items(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"assignment_id": map[string]any{"type": "integer", "description": "Assignment ID"},
						"score":         map[string]any{"type": "number", "description": "Hypothetical score (raw points)"},
					},
					"required": []string{"assignment_id", "score"},
				}),
			),
		),
		mcplib.NewTypedToolHandler(s.handleCalculateWhatIf),
	)
}

type listGradesArgs struct {
	IncludeCompleted bool `json:"include_completed"`
}

func (s *Server) handleListGrades(ctx context.Context, _ mcplib.CallToolRequest, args listGradesArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	gqlOpts := canvas.GraphQLCourseListOptions{All: args.IncludeCompleted}
	courses, gqlErr := canvas.QueryCourseSummariesGraphQL(ctx, client, gqlOpts)
	if canvas.IsGraphQLFallback(gqlErr) {
		opts := canvas.CourseListOptions{
			Include: []string{"enrollments", "total_scores"},
		}
		if !args.IncludeCompleted {
			opts.EnrollmentState = "active"
		}
		courses, err = collectIter(canvas.ListCourses(ctx, client, opts))
		if err != nil {
			return toolError(err)
		}
	} else if gqlErr != nil {
		return toolError(gqlErr)
	}

	type courseGrade struct {
		ID           int64    `json:"id"`
		Name         string   `json:"name"`
		CourseCode   string   `json:"course_code"`
		CurrentScore *float64 `json:"current_score,omitempty"`
		CurrentGrade *string  `json:"current_grade,omitempty"`
		FinalScore   *float64 `json:"final_score,omitempty"`
		FinalGrade   *string  `json:"final_grade,omitempty"`
	}

	results := make([]courseGrade, 0, len(courses))
	for _, c := range courses {
		cg := courseGrade{
			ID:         c.ID,
			Name:       c.Name,
			CourseCode: c.CourseCode,
		}
		for _, e := range c.Enrollments {
			if e.Grades != nil {
				cg.CurrentScore = e.Grades.CurrentScore
				cg.CurrentGrade = e.Grades.CurrentGrade
				cg.FinalScore = e.Grades.FinalScore
				cg.FinalGrade = e.Grades.FinalGrade
			}
			if e.ComputedCurrentScore != nil {
				cg.CurrentScore = e.ComputedCurrentScore
				cg.CurrentGrade = e.ComputedCurrentGrade
				cg.FinalScore = e.ComputedFinalScore
				cg.FinalGrade = e.ComputedFinalGrade
			}
			break
		}
		results = append(results, cg)
	}

	return jsonResult(results)
}

type getGradesArgs struct {
	Course string `json:"course"`
}

func (s *Server) handleGetGrades(ctx context.Context, _ mcplib.CallToolRequest, args getGradesArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}
	full, err := canvas.GetCourse(ctx, client, course.ID, []string{"enrollments", "total_scores"})
	if err != nil {
		return toolError(err)
	}

	groups, scheme, err := fetchGradeData(ctx, client, full)
	if err != nil {
		return toolError(err)
	}

	inputs := convertToGradeInputs(groups)
	result := grade.Calculate(inputs, full.ApplyAssignmentGroupWeights, scheme)

	return jsonResult(formatGradeResult(full, groups, result))
}

type calculateWhatIfArgs struct {
	Course    string `json:"course"`
	Scenarios []struct {
		AssignmentID int64   `json:"assignment_id"`
		Score        float64 `json:"score"`
	} `json:"scenarios"`
}

func (s *Server) handleCalculateWhatIf(ctx context.Context, _ mcplib.CallToolRequest, args calculateWhatIfArgs) (*mcplib.CallToolResult, error) {
	client, err := s.getClient()
	if err != nil {
		return toolError(err)
	}

	course, err := canvas.FindCourse(ctx, client, args.Course)
	if err != nil {
		return toolError(err)
	}
	full, err := canvas.GetCourse(ctx, client, course.ID, []string{"enrollments", "total_scores"})
	if err != nil {
		return toolError(err)
	}

	groups, scheme, err := fetchGradeData(ctx, client, full)
	if err != nil {
		return toolError(err)
	}

	whatIfs := make(map[int64]decimal.Decimal, len(args.Scenarios))
	for _, sc := range args.Scenarios {
		whatIfs[sc.AssignmentID] = decimal.NewFromFloat(sc.Score)
	}

	inputs := convertToGradeInputs(groups)
	weighted := full.ApplyAssignmentGroupWeights
	baseline := grade.Calculate(inputs, weighted, scheme)
	result := grade.CalculateWhatIf(inputs, weighted, scheme, whatIfs)

	type whatIfResult struct {
		Before gradeOutput `json:"before"`
		After  gradeOutput `json:"after"`
	}

	return jsonResult(whatIfResult{
		Before: formatGradeResult(full, groups, baseline),
		After:  formatGradeResult(full, groups, result),
	})
}

// fetchGradeData gets assignment groups and grading scheme for a course.
func fetchGradeData(ctx context.Context, client *canvas.Client, course canvas.Course) ([]canvas.AssignmentGroup, []grade.SchemeEntry, error) {
	// Try GraphQL first
	groups, gqlErr := canvas.QueryCourseGradesGraphQL(ctx, client, course.ID)
	if canvas.IsGraphQLFallback(gqlErr) {
		var err error
		groups, err = collectIter(canvas.ListAssignmentGroups(ctx, client, course.ID, []string{"assignments", "submission"}))
		if err != nil {
			return nil, nil, err
		}
	} else if gqlErr != nil {
		return nil, nil, gqlErr
	}

	var scheme []grade.SchemeEntry
	if course.GradingStandardID != nil {
		gs, err := canvas.GetGradingStandard(ctx, client, course.ID, *course.GradingStandardID)
		if err == nil {
			for _, entry := range gs.GradingScheme {
				scheme = append(scheme, grade.SchemeEntry{
					Name:  entry.Name,
					Value: decimal.NewFromFloat(entry.Value),
				})
			}
		}
	}

	return groups, scheme, nil
}

// convertToGradeInputs converts Canvas assignment groups to grade calculator inputs.
func convertToGradeInputs(groups []canvas.AssignmentGroup) []grade.GroupInput {
	inputs := make([]grade.GroupInput, len(groups))
	for i, g := range groups {
		inputs[i] = grade.GroupInput{
			ID:          g.ID,
			Name:        g.Name,
			Weight:      decimal.NewFromFloat(g.GroupWeight),
			DropLowest:  g.Rules.DropLowest,
			DropHighest: g.Rules.DropHighest,
			NeverDrop:   g.Rules.NeverDrop,
		}
		for _, a := range g.Assignments {
			if !a.Published {
				continue
			}
			si := grade.SubmissionInput{
				AssignmentID:   a.ID,
				PointsPossible: decimalFromPtr(a.PointsPossible),
				OmitFromFinal:  a.OmitFromFinalGrade,
			}
			if a.Submission != nil {
				sub := a.Submission
				si.Excused = sub.Excused
				si.PendingReview = sub.WorkflowState == "pending_review"
				if sub.Score != nil {
					d := decimal.NewFromFloat(*sub.Score)
					si.Score = &d
				}
			}
			inputs[i].Submissions = append(inputs[i].Submissions, si)
		}
	}
	return inputs
}

func decimalFromPtr(f *float64) decimal.Decimal {
	if f == nil {
		return decimal.Zero
	}
	return decimal.NewFromFloat(*f)
}

type gradeOutput struct {
	CourseName   string       `json:"course_name"`
	CourseCode   string       `json:"course_code"`
	CurrentScore string       `json:"current_score"`
	FinalScore   string       `json:"final_score"`
	CurrentGrade *string      `json:"current_grade,omitempty"`
	FinalGrade   *string      `json:"final_grade,omitempty"`
	Groups       []groupEntry `json:"groups"`
}

type groupEntry struct {
	Name         string            `json:"name"`
	Weight       string            `json:"weight,omitempty"`
	CurrentScore string            `json:"current_score"`
	Assignments  []assignmentEntry `json:"assignments"`
}

type assignmentEntry struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Score          *float64 `json:"score,omitempty"`
	PointsPossible float64  `json:"points_possible"`
	Status         string   `json:"status"`
	Dropped        bool     `json:"dropped,omitempty"`
}

func formatGradeResult(course canvas.Course, groups []canvas.AssignmentGroup, result grade.CourseGrade) gradeOutput {
	// Build assignment name lookup from canvas data
	nameMap := make(map[int64]string)
	for _, g := range groups {
		for _, a := range g.Assignments {
			nameMap[a.ID] = a.Name
		}
	}

	out := gradeOutput{
		CourseName:   course.Name,
		CourseCode:   course.CourseCode,
		CurrentScore: result.CurrentScore.StringFixed(2) + "%",
		FinalScore:   result.FinalScore.StringFixed(2) + "%",
		CurrentGrade: result.CurrentGrade,
		FinalGrade:   result.FinalGrade,
	}

	for _, g := range result.Groups {
		ge := groupEntry{
			Name:         g.Name,
			CurrentScore: fmt.Sprintf("%s/%s", g.CurrentScore.StringFixed(2), g.CurrentPossible.StringFixed(2)),
		}
		if !g.Weight.IsZero() {
			ge.Weight = g.Weight.StringFixed(1) + "%"
		}

		droppedSet := make(map[int64]bool)
		for _, id := range g.DroppedCurrent {
			droppedSet[id] = true
		}

		for _, sr := range g.Submissions {
			ae := assignmentEntry{
				ID:             sr.AssignmentID,
				Name:           nameMap[sr.AssignmentID],
				PointsPossible: sr.PointsPossible.InexactFloat64(),
				Dropped:        droppedSet[sr.AssignmentID],
			}
			if sr.Score != nil {
				f := sr.Score.InexactFloat64()
				ae.Score = &f
				ae.Status = "graded"
			} else if sr.Excused {
				ae.Status = "excused"
			} else {
				ae.Status = "ungraded"
			}
			ge.Assignments = append(ge.Assignments, ae)
		}
		out.Groups = append(out.Groups, ge)
	}

	return out
}
