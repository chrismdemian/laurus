package grade

import (
	"testing"

	"github.com/shopspring/decimal"
)

// Helper to create a decimal pointer.
func dp(v string) *decimal.Decimal {
	d := decimal.RequireFromString(v)
	return &d
}

// Helper to create a decimal.
func d(v string) decimal.Decimal {
	return decimal.RequireFromString(v)
}

func TestPointsModeBasic(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "Homework", Weight: d("30"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("90"), PointsPossible: d("100")},
				{AssignmentID: 2, Score: dp("80"), PointsPossible: d("100")},
			},
		},
		{
			ID: 2, Name: "Exams", Weight: d("70"),
			Submissions: []SubmissionInput{
				{AssignmentID: 3, Score: dp("75"), PointsPossible: d("100")},
			},
		},
	}

	result := Calculate(groups, false, nil)
	// Points mode: (90+80+75) / (100+100+100) * 100 = 81.67
	assertDecimalEqual(t, "CurrentScore", d("81.67"), result.CurrentScore)
	assertDecimalEqual(t, "FinalScore", d("81.67"), result.FinalScore)
}

func TestWeightedModeBasic(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "Homework", Weight: d("30"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("90"), PointsPossible: d("100")},
				{AssignmentID: 2, Score: dp("80"), PointsPossible: d("100")},
			},
		},
		{
			ID: 2, Name: "Exams", Weight: d("70"),
			Submissions: []SubmissionInput{
				{AssignmentID: 3, Score: dp("75"), PointsPossible: d("100")},
			},
		},
	}

	result := Calculate(groups, true, nil)
	// Homework: (90+80)/(100+100) = 85% * 30 = 25.5
	// Exams: 75/100 = 75% * 70 = 52.5
	// Total: (25.5+52.5)/100 * 100 = 78.00
	assertDecimalEqual(t, "CurrentScore", d("78.00"), result.CurrentScore)
}

func TestEdgeCase1_WeightsDontSumTo100(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "Homework", Weight: d("40"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("70"), PointsPossible: d("100")},
			},
		},
		{
			ID: 2, Name: "Exams", Weight: d("40"),
			Submissions: []SubmissionInput{
				{AssignmentID: 2, Score: dp("70"), PointsPossible: d("100")},
			},
		},
	}

	result := Calculate(groups, true, nil)
	// fullWeight = 80, weightedSum = 0.7*40 + 0.7*40 = 56
	// scaled = 56/80 * 100 = 70.00
	assertDecimalEqual(t, "CurrentScore", d("70.00"), result.CurrentScore)
}

func TestEdgeCase2_ZeroPointsGroupWeighted(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "Homework", Weight: d("50"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("80"), PointsPossible: d("100")},
			},
		},
		{
			ID: 2, Name: "Extra", Weight: d("50"),
			Submissions: []SubmissionInput{}, // no submissions at all
		},
	}

	result := Calculate(groups, true, nil)
	// Extra group has 0 possible → excluded. fullWeight = 50.
	// weighted = 0.8 * 50 = 40. 40/50 * 100 = 80.00
	assertDecimalEqual(t, "CurrentScore", d("80.00"), result.CurrentScore)
}

func TestEdgeCase3_DifferentDropsForCurrentVsFinal(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("100"),
			DropLowest: 1,
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("50"), PointsPossible: d("100")},
				{AssignmentID: 2, Score: dp("90"), PointsPossible: d("100")},
				{AssignmentID: 3, PointsPossible: d("100")}, // ungraded (Score=nil)
			},
		},
	}

	result := Calculate(groups, true, nil)

	// Current: subs 1,2 (graded). Drop lowest → drop 1 (50). Keep 2 (90). Score = 90.
	assertDecimalEqual(t, "CurrentScore", d("90.00"), result.CurrentScore)

	// Final: subs 1,2,3 (ungraded=0). Drop lowest → drop 3 (0). Keep 1,2. Score = (50+90)/200*100 = 70.
	assertDecimalEqual(t, "FinalScore", d("70.00"), result.FinalScore)

	// Different assignments dropped in each track.
	if len(result.Groups[0].DroppedCurrent) != 1 || result.Groups[0].DroppedCurrent[0] != 1 {
		t.Errorf("DroppedCurrent: want [1], got %v", result.Groups[0].DroppedCurrent)
	}
	if len(result.Groups[0].DroppedFinal) != 1 || result.Groups[0].DroppedFinal[0] != 3 {
		t.Errorf("DroppedFinal: want [3], got %v", result.Groups[0].DroppedFinal)
	}
}

func TestEdgeCase4_TieBreakingByAssignmentID(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("100"),
			DropLowest: 1,
			Submissions: []SubmissionInput{
				{AssignmentID: 10, Score: dp("80"), PointsPossible: d("100")},
				{AssignmentID: 20, Score: dp("80"), PointsPossible: d("100")},
				{AssignmentID: 30, Score: dp("80"), PointsPossible: d("100")},
			},
		},
	}

	result := Calculate(groups, true, nil)

	// All tied at 80/100. Drop lowest should drop highest assignment ID (30).
	if len(result.Groups[0].DroppedCurrent) != 1 || result.Groups[0].DroppedCurrent[0] != 30 {
		t.Errorf("DroppedCurrent: want [30], got %v", result.Groups[0].DroppedCurrent)
	}
}

func TestEdgeCase5_PendingReview(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "Quizzes", Weight: d("100"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("90"), PointsPossible: d("100")},
				{AssignmentID: 2, Score: dp("70"), PointsPossible: d("100"), PendingReview: true},
			},
		},
	}

	result := Calculate(groups, true, nil)

	// Current: only graded+non-pending → just assignment 1. Score = 90.
	assertDecimalEqual(t, "CurrentScore", d("90.00"), result.CurrentScore)

	// Final: pending treated as 0. (90+0)/(100+100)*100 = 45.
	assertDecimalEqual(t, "FinalScore", d("45.00"), result.FinalScore)
}

func TestEdgeCase6_UnpostedGrades(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("100"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("90"), PointsPossible: d("100")},
				{AssignmentID: 2, Score: dp("50"), PointsPossible: d("100"), Unposted: true},
			},
		},
	}

	result := Calculate(groups, true, nil)

	// Unposted excluded entirely. Only assignment 1.
	assertDecimalEqual(t, "CurrentScore", d("90.00"), result.CurrentScore)
	assertDecimalEqual(t, "FinalScore", d("90.00"), result.FinalScore)
}

func TestEdgeCase7_DuplicateSubmissions(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("100"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("90"), PointsPossible: d("100")},
				{AssignmentID: 1, Score: dp("50"), PointsPossible: d("100")}, // duplicate — discarded
				{AssignmentID: 2, Score: dp("80"), PointsPossible: d("100")},
			},
		},
	}

	result := Calculate(groups, true, nil)

	// Deduplicated: assignment 1 (90) + assignment 2 (80). Score = 170/200*100 = 85.
	assertDecimalEqual(t, "CurrentScore", d("85.00"), result.CurrentScore)
}

func TestEdgeCase8_DecimalPrecision(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("100"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("1"), PointsPossible: d("3")},
				{AssignmentID: 2, Score: dp("1"), PointsPossible: d("3")},
				{AssignmentID: 3, Score: dp("1"), PointsPossible: d("3")},
			},
		},
	}

	result := Calculate(groups, true, nil)

	// 3/9 * 100 = 33.33... → rounded to 33.33
	assertDecimalEqual(t, "CurrentScore", d("33.33"), result.CurrentScore)
}

func TestEdgeCase9_ExcusedAssignments(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("100"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("90"), PointsPossible: d("100")},
				{AssignmentID: 2, Score: dp("0"), PointsPossible: d("100"), Excused: true},
			},
		},
	}

	result := Calculate(groups, true, nil)

	// Excused removed entirely. Only assignment 1. Score = 90.
	assertDecimalEqual(t, "CurrentScore", d("90.00"), result.CurrentScore)
	assertDecimalEqual(t, "FinalScore", d("90.00"), result.FinalScore)
}

func TestEdgeCase10_ExtraCreditScoreExceedsPossible(t *testing.T) {
	// Extra credit: a submission scores above points_possible.
	// Canvas allows this and does not cap the percentage.
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("50"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("110"), PointsPossible: d("100")}, // 110% on this assignment
			},
		},
		{
			ID: 2, Name: "Exams", Weight: d("50"),
			Submissions: []SubmissionInput{
				{AssignmentID: 2, Score: dp("80"), PointsPossible: d("100")},
			},
		},
	}

	result := Calculate(groups, true, nil)

	// HW: 110/100 = 110% * 50 = 55. Exams: 80/100 = 80% * 50 = 40.
	// fullWeight = 100. Total = 95/100 * 100 = 95.00
	assertDecimalEqual(t, "CurrentScore", d("95.00"), result.CurrentScore)
}

func TestEdgeCase11_OmitFromFinalGrade(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("100"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("90"), PointsPossible: d("100")},
				{AssignmentID: 2, Score: dp("50"), PointsPossible: d("100"), OmitFromFinal: true},
			},
		},
	}

	result := Calculate(groups, true, nil)

	// Omitted removed entirely. Only assignment 1.
	assertDecimalEqual(t, "CurrentScore", d("90.00"), result.CurrentScore)
}

func TestEdgeCase12_WhatIf(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("100"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("90"), PointsPossible: d("100")},
				{AssignmentID: 2, PointsPossible: d("100")}, // ungraded
			},
		},
	}

	whatIfs := map[int64]decimal.Decimal{
		2: d("85"),
	}

	result := CalculateWhatIf(groups, true, nil, whatIfs)

	// With what-if: (90+85)/(100+100)*100 = 87.50
	assertDecimalEqual(t, "CurrentScore", d("87.50"), result.CurrentScore)
}

func TestGradingScheme(t *testing.T) {
	scheme := []SchemeEntry{
		{Name: "A", Value: d("0.94")},
		{Name: "A-", Value: d("0.90")},
		{Name: "B+", Value: d("0.87")},
		{Name: "B", Value: d("0.84")},
		{Name: "B-", Value: d("0.80")},
		{Name: "C+", Value: d("0.77")},
		{Name: "C", Value: d("0.74")},
		{Name: "C-", Value: d("0.70")},
		{Name: "D", Value: d("0.60")},
		{Name: "F", Value: d("0.00")},
	}

	groups := []GroupInput{
		{
			ID: 1, Name: "All", Weight: d("100"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("91"), PointsPossible: d("100")},
			},
		},
	}

	result := Calculate(groups, true, scheme)

	assertDecimalEqual(t, "CurrentScore", d("91.00"), result.CurrentScore)
	if result.CurrentGrade == nil || *result.CurrentGrade != "A-" {
		t.Errorf("CurrentGrade: want A-, got %v", result.CurrentGrade)
	}
}

func TestDropLowestWithNeverDrop(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("100"),
			DropLowest: 1,
			NeverDrop:  []int64{1}, // assignment 1 cannot be dropped
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("50"), PointsPossible: d("100")}, // lowest but protected
				{AssignmentID: 2, Score: dp("70"), PointsPossible: d("100")},
				{AssignmentID: 3, Score: dp("90"), PointsPossible: d("100")},
			},
		},
	}

	result := Calculate(groups, true, nil)

	// Assignment 1 is never-drop. Next lowest is 2 (70). Drop it.
	// Keep: 1 (50) + 3 (90) = 140/200 = 70%.
	assertDecimalEqual(t, "CurrentScore", d("70.00"), result.CurrentScore)

	if len(result.Groups[0].DroppedCurrent) != 1 || result.Groups[0].DroppedCurrent[0] != 2 {
		t.Errorf("DroppedCurrent: want [2], got %v", result.Groups[0].DroppedCurrent)
	}
}

func TestDropHighest(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("100"),
			DropHighest: 1,
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("60"), PointsPossible: d("100")},
				{AssignmentID: 2, Score: dp("80"), PointsPossible: d("100")},
				{AssignmentID: 3, Score: dp("100"), PointsPossible: d("100")},
			},
		},
	}

	result := Calculate(groups, true, nil)

	// Drop highest (100). Keep 60 + 80 = 140/200 = 70%.
	assertDecimalEqual(t, "CurrentScore", d("70.00"), result.CurrentScore)

	if len(result.Groups[0].DroppedCurrent) != 1 || result.Groups[0].DroppedCurrent[0] != 3 {
		t.Errorf("DroppedCurrent: want [3], got %v", result.Groups[0].DroppedCurrent)
	}
}

func TestDropHighestTieBreak(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("100"),
			DropHighest: 1,
			Submissions: []SubmissionInput{
				{AssignmentID: 10, Score: dp("90"), PointsPossible: d("100")},
				{AssignmentID: 20, Score: dp("90"), PointsPossible: d("100")},
				{AssignmentID: 30, Score: dp("90"), PointsPossible: d("100")},
			},
		},
	}

	result := Calculate(groups, true, nil)

	// All tied. Drop highest should drop lowest assignment ID (10).
	if len(result.Groups[0].DroppedCurrent) != 1 || result.Groups[0].DroppedCurrent[0] != 10 {
		t.Errorf("DroppedCurrent: want [10], got %v", result.Groups[0].DroppedCurrent)
	}
}

func TestEmptyGroups(t *testing.T) {
	result := Calculate(nil, true, nil)
	assertDecimalEqual(t, "CurrentScore", d("0"), result.CurrentScore)
	assertDecimalEqual(t, "FinalScore", d("0"), result.FinalScore)
}

func TestAllExcused(t *testing.T) {
	groups := []GroupInput{
		{
			ID: 1, Name: "HW", Weight: d("100"),
			Submissions: []SubmissionInput{
				{AssignmentID: 1, Score: dp("90"), PointsPossible: d("100"), Excused: true},
			},
		},
	}

	result := Calculate(groups, true, nil)
	// All excused → 0 possible → 0.
	assertDecimalEqual(t, "CurrentScore", d("0"), result.CurrentScore)
}

func assertDecimalEqual(t *testing.T, label string, want, got decimal.Decimal) {
	t.Helper()
	if !want.Equal(got) {
		t.Errorf("%s: want %s, got %s", label, want.String(), got.String())
	}
}
