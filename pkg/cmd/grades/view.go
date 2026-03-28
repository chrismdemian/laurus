package grades

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
	"github.com/chrismdemian/laurus/pkg/grade"
)

var hundred = decimal.NewFromInt(100)

// NewCmdGrade returns the singular "grade" command for per-course breakdown.
func NewCmdGrade(f *cmdutil.Factory) *cobra.Command {
	var opts viewOpts

	cmd := &cobra.Command{
		Use:   "grade <course>",
		Short: "View detailed grades for a course",
		Long:  "Show per-assignment grade breakdown with group weighting and drop rules.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return viewRun(f, args[0], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Detailed, "detailed", false, "Show rubric scores and submission comments")
	cmd.Flags().BoolVar(&opts.Statistics, "statistics", false, "Show class average/median per assignment")
	cmd.Flags().StringVar(&opts.WhatIf, "what-if", "", "Simulate scores: \"85\" applies to first ungraded, or \"id:score,...\"")

	return cmd
}

type viewOpts struct {
	Detailed   bool
	Statistics bool
	WhatIf     string
}

func viewRun(f *cmdutil.Factory, query string, opts viewOpts) error {
	client, err := f.Client()
	if err != nil {
		return err
	}
	ios := f.IOStreams()
	ctx := context.Background()

	// Find and re-fetch course with grade-relevant fields.
	course, err := canvas.FindCourse(ctx, client, query)
	if err != nil {
		return fmt.Errorf("finding course %q: %w", query, err)
	}
	course, err = canvas.GetCourse(ctx, client, course.ID,
		[]string{"enrollments", "total_scores"})
	if err != nil {
		return fmt.Errorf("fetching course: %w", err)
	}

	// Fetch assignment groups with assignments and submissions in one call.
	include := []string{"assignments", "submission"}
	if opts.Statistics {
		include = append(include, "score_statistics")
	}

	var groups []canvas.AssignmentGroup
	for g, err := range canvas.ListAssignmentGroups(ctx, client, course.ID, include) {
		if err != nil {
			return fmt.Errorf("fetching assignment groups: %w", err)
		}
		groups = append(groups, g)
	}

	// Fetch grading standard if the course has one.
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
		// Non-fatal: if grading standard fetch fails, proceed without letter grades.
	}

	// Convert Canvas types → grade calculator inputs.
	gradeInputs := convertToGradeInputs(groups)
	weighted := course.ApplyAssignmentGroupWeights

	// Calculate grades (with or without what-if).
	var result grade.CourseGrade
	var baseline *grade.CourseGrade
	if opts.WhatIf != "" {
		whatIfs, err := parseWhatIf(opts.WhatIf, groups)
		if err != nil {
			return err
		}
		// Warn if any what-if IDs don't match real assignments.
		allIDs := map[int64]bool{}
		for _, g := range groups {
			for _, a := range g.Assignments {
				allIDs[a.ID] = true
			}
		}
		for id := range whatIfs {
			if !allIDs[id] {
				_, _ = fmt.Fprintf(ios.ErrOut, "warning: assignment ID %d not found in this course\n", id)
			}
		}
		base := grade.Calculate(gradeInputs, weighted, scheme)
		baseline = &base
		result = grade.CalculateWhatIf(gradeInputs, weighted, scheme, whatIfs)
	} else {
		result = grade.Calculate(gradeInputs, weighted, scheme)
	}

	if ios.IsJSON {
		return cmdutil.RenderJSON(ios, result)
	}

	return renderGradeDetail(ios, course, groups, result, baseline, opts)
}

func renderGradeDetail(ios *iostreams.IOStreams, course canvas.Course, apiGroups []canvas.AssignmentGroup, result grade.CourseGrade, baseline *grade.CourseGrade, opts viewOpts) error {
	palette := cmdutil.NewPalette(ios)

	_ = ios.StartPager()
	defer ios.StopPager()

	// Course header with overall grades.
	_, _ = fmt.Fprintln(ios.Out, palette.Header.Render(course.Name))

	currentStr := result.CurrentScore.String() + "%"
	if result.CurrentGrade != nil {
		currentStr += fmt.Sprintf(" (%s)", *result.CurrentGrade)
	}
	finalStr := result.FinalScore.String() + "%"
	if result.FinalGrade != nil {
		finalStr += fmt.Sprintf(" (%s)", *result.FinalGrade)
	}

	// Show before → after for what-if mode.
	if baseline != nil {
		baseCurrentStr := baseline.CurrentScore.String() + "%"
		baseFinalStr := baseline.FinalScore.String() + "%"
		printField(ios, palette, "Current", baseCurrentStr+" -> "+currentStr)
		printField(ios, palette, "Final", baseFinalStr+" -> "+finalStr)
	} else {
		printField(ios, palette, "Current", currentStr)
		printField(ios, palette, "Final", finalStr)
	}

	// Per-group breakdown.
	for _, gr := range result.Groups {
		_, _ = fmt.Fprintln(ios.Out)

		// Group header: name + weight + group percentage.
		groupHeader := gr.Name
		if !gr.Weight.IsZero() {
			groupHeader += fmt.Sprintf(" (%s%%)", gr.Weight.String())
		}
		if !gr.CurrentPossible.IsZero() {
			groupPct := gr.CurrentScore.Div(gr.CurrentPossible).Mul(decimal.NewFromInt(100)).Round(1)
			groupHeader += fmt.Sprintf("  %s%%", groupPct.String())
		}
		_, _ = fmt.Fprintln(ios.Out, palette.Header.Render(groupHeader))

		// Find the matching API group for extra details.
		var apiGroup *canvas.AssignmentGroup
		for i := range apiGroups {
			if apiGroups[i].ID == gr.ID {
				apiGroup = &apiGroups[i]
				break
			}
		}

		// Assignment table.
		tbl := cmdutil.NewTable(ios)
		headers := []string{"ASSIGNMENT", "SCORE", "PCT"}
		if opts.Statistics {
			headers = append(headers, "AVG")
		}
		headers = append(headers, "STATUS")
		tbl.AddHeader(headers...)

		for _, sr := range gr.Submissions {
			name := assignmentName(apiGroup, sr.AssignmentID)
			pctStr := "-"
			var scoreStr, status string
			var style lipgloss.Style

			switch {
			case sr.Excused:
				scoreStr = "EX"
				status = "excused"
				style = palette.Muted
			case sr.Score != nil:
				scoreStr = fmt.Sprintf("%s/%s", sr.Score.String(), sr.PointsPossible.String())
				if !sr.PointsPossible.IsZero() {
					pct := sr.Score.Div(sr.PointsPossible).Mul(decimal.NewFromInt(100)).Round(1)
					pctStr = pct.String() + "%"
				}
				style = palette.Graded
			default:
				scoreStr = fmt.Sprintf("-/%s", sr.PointsPossible.String())
				status = "ungraded"
				style = palette.Muted
			}

			if sr.DroppedCurrent {
				status = "dropped"
				style = palette.Concluded
			}

			row := []cmdutil.StyledCell{
				{Value: name, Style: style},
				{Value: scoreStr, Style: style},
				{Value: pctStr, Style: style},
			}

			if opts.Statistics {
				avgStr := "-"
				if apiGroup != nil {
					if a := findAssignment(apiGroup, sr.AssignmentID); a != nil {
						if a.ScoreStatistics != nil && a.ScoreStatistics.Mean != nil {
							avgStr = fmt.Sprintf("%.1f", *a.ScoreStatistics.Mean)
						}
					}
				}
				row = append(row, cmdutil.StyledCell{Value: avgStr, Style: style})
			}

			row = append(row, cmdutil.StyledCell{Value: status, Style: style})
			tbl.AddStyledRow(row...)

			// Detailed mode: rubric + comments.
			if opts.Detailed && apiGroup != nil {
				if a := findAssignment(apiGroup, sr.AssignmentID); a != nil && a.Submission != nil {
					renderSubmissionDetails(ios, palette, a)
				}
			}
		}

		if err := tbl.Render(); err != nil {
			return err
		}
	}

	return nil
}

func renderSubmissionDetails(ios *iostreams.IOStreams, palette *cmdutil.Palette, a *canvas.Assignment) {
	sub := a.Submission

	// Rubric assessment.
	if len(sub.RubricAssessment) > 0 && len(a.Rubric) > 0 {
		for _, criterion := range a.Rubric {
			if assessment, ok := sub.RubricAssessment[criterion.ID]; ok {
				pts := "-"
				if assessment.Points != nil {
					pts = fmt.Sprintf("%.1f", *assessment.Points)
				}
				_, _ = fmt.Fprintf(ios.Out, "    %s %s/%s\n",
					palette.Muted.Render(criterion.Description+":"),
					pts,
					fmt.Sprintf("%.1f", criterion.Points),
				)
			}
		}
	}

	// Comments.
	for _, c := range sub.SubmissionComments {
		_, _ = fmt.Fprintf(ios.Out, "    %s %s\n",
			palette.Muted.Render(c.Author+":"),
			c.Comment,
		)
	}
}

// =============================================================================
// Canvas → grade calculator conversion
// =============================================================================

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
				// Student tokens only see posted scores — Canvas omits unposted
				// grades from the API response entirely. No need to filter here.
				si.Unposted = false
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

// =============================================================================
// What-if parsing
// =============================================================================

// parseWhatIf handles formats like "85" (percentage, applied to first ungraded)
// or "123:85,456:90" (raw points per assignment ID).
func parseWhatIf(input string, groups []canvas.AssignmentGroup) (map[int64]decimal.Decimal, error) {
	whatIfs := make(map[int64]decimal.Decimal)

	// Simple case: just a number → interpret as percentage, apply to first ungraded.
	if pct, err := decimal.NewFromString(input); err == nil && !strings.Contains(input, ":") {
		for _, g := range groups {
			for _, a := range g.Assignments {
				if !a.Published {
					continue
				}
				if a.PointsPossible == nil || *a.PointsPossible == 0 {
					continue
				}
				if a.Submission == nil || a.Submission.Score == nil {
					// Convert percentage to raw points: 85% on a 10-point assignment = 8.5
					pts := pct.Div(hundred).Mul(decimal.NewFromFloat(*a.PointsPossible))
					whatIfs[a.ID] = pts
					return whatIfs, nil
				}
			}
		}
		return nil, fmt.Errorf("no ungraded assignments found for what-if simulation")
	}

	// Assignment-specific: "id:score,id:score"
	for _, pair := range strings.Split(input, ",") {
		parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid what-if format %q; use \"score\" or \"id:score,id:score\"", pair)
		}
		id, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid assignment ID %q in what-if", parts[0])
		}
		score, err := decimal.NewFromString(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("invalid score %q in what-if", parts[1])
		}
		whatIfs[id] = score
	}

	return whatIfs, nil
}

// =============================================================================
// Helpers
// =============================================================================

func assignmentName(g *canvas.AssignmentGroup, assignmentID int64) string {
	if g != nil {
		for _, a := range g.Assignments {
			if a.ID == assignmentID {
				return a.Name
			}
		}
	}
	return fmt.Sprintf("Assignment %d", assignmentID)
}

func findAssignment(g *canvas.AssignmentGroup, assignmentID int64) *canvas.Assignment {
	for i := range g.Assignments {
		if g.Assignments[i].ID == assignmentID {
			return &g.Assignments[i]
		}
	}
	return nil
}
