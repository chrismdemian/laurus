// Package grade implements Canvas's exact grade calculation algorithm,
// including weighted assignment groups, drop-lowest rules (Kane & Kane
// bisection), extra credit, and excused assignments.
//
// This package is standalone — it has no imports from internal/canvas/.
// Callers convert Canvas API types to the input types defined here.
package grade

import (
	"slices"
	"sort"

	"github.com/shopspring/decimal"
)

// =============================================================================
// Input types
// =============================================================================

// GroupInput represents an assignment group ready for grade calculation.
type GroupInput struct {
	ID          int64
	Name        string
	Weight      decimal.Decimal // 0–100 (only used in weighted mode)
	DropLowest  int
	DropHighest int
	NeverDrop   []int64 // assignment IDs exempt from dropping
	Submissions []SubmissionInput
}

// SubmissionInput represents a single assignment/submission pair.
type SubmissionInput struct {
	AssignmentID   int64
	Score          *decimal.Decimal // nil = ungraded
	PointsPossible decimal.Decimal
	Excused        bool
	PendingReview  bool // workflow_state == "pending_review"
	Unposted       bool // posted_at == nil (invisible to student)
	OmitFromFinal  bool // assignment.omit_from_final_grade
}

// SchemeEntry maps a letter grade to a minimum percentage cutoff (0.0–1.0).
type SchemeEntry struct {
	Name  string
	Value decimal.Decimal // e.g., 0.94 for A, 0.90 for A-
}

// =============================================================================
// Output types
// =============================================================================

// CourseGrade holds the computed grades for a course.
type CourseGrade struct {
	CurrentScore decimal.Decimal // percentage from graded items only
	FinalScore   decimal.Decimal // percentage treating ungraded as 0
	CurrentGrade *string         // letter grade (nil if no scheme)
	FinalGrade   *string
	Groups       []GroupResult
}

// GroupResult holds per-group calculation results.
type GroupResult struct {
	ID              int64
	Name            string
	Weight          decimal.Decimal
	CurrentScore    decimal.Decimal // group percentage (current track)
	FinalScore      decimal.Decimal // group percentage (final track)
	CurrentPossible decimal.Decimal
	FinalPossible   decimal.Decimal
	DroppedCurrent  []int64 // assignment IDs dropped in current calculation
	DroppedFinal    []int64 // may differ from current
	Submissions     []SubmissionResult
}

// SubmissionResult holds per-submission details after calculation.
type SubmissionResult struct {
	AssignmentID   int64
	Score          *decimal.Decimal
	PointsPossible decimal.Decimal
	DroppedCurrent bool
	DroppedFinal   bool
	Excused        bool
}

// =============================================================================
// Public API
// =============================================================================

var (
	zero    = decimal.Zero
	hundred = decimal.NewFromInt(100)
)

// Calculate computes course grades using Canvas's exact algorithm.
//
// If weighted is true, groups are combined by their Weight fields (percent mode).
// If weighted is false, all scores are summed directly (points mode).
// scheme may be nil if no letter grades are needed.
func Calculate(groups []GroupInput, weighted bool, scheme []SchemeEntry) CourseGrade {
	result := CourseGrade{}

	for _, g := range groups {
		gr := computeGroup(g)
		result.Groups = append(result.Groups, gr)
	}

	if weighted {
		result.CurrentScore = weightedTotal(result.Groups, true)
		result.FinalScore = weightedTotal(result.Groups, false)
	} else {
		result.CurrentScore = pointsTotal(result.Groups, true)
		result.FinalScore = pointsTotal(result.Groups, false)
	}

	result.CurrentScore = result.CurrentScore.Round(2)
	result.FinalScore = result.FinalScore.Round(2)

	if len(scheme) > 0 {
		result.CurrentGrade = lookupGrade(result.CurrentScore, scheme)
		result.FinalGrade = lookupGrade(result.FinalScore, scheme)
	}

	return result
}

// CalculateWhatIf runs the calculator with hypothetical scores injected.
// whatIfs maps assignment ID → hypothetical score. Matching submissions
// have their score replaced and excused set to false.
func CalculateWhatIf(groups []GroupInput, weighted bool, scheme []SchemeEntry, whatIfs map[int64]decimal.Decimal) CourseGrade {
	cloned := make([]GroupInput, len(groups))
	for i, g := range groups {
		cloned[i] = GroupInput{
			ID:          g.ID,
			Name:        g.Name,
			Weight:      g.Weight,
			DropLowest:  g.DropLowest,
			DropHighest: g.DropHighest,
			NeverDrop:   g.NeverDrop,
			Submissions: make([]SubmissionInput, len(g.Submissions)),
		}
		for j, s := range g.Submissions {
			cloned[i].Submissions[j] = s
			if score, ok := whatIfs[s.AssignmentID]; ok {
				cloned[i].Submissions[j].Score = &score
				cloned[i].Submissions[j].Excused = false
			}
		}
	}
	return Calculate(cloned, weighted, scheme)
}

// =============================================================================
// Group calculation
// =============================================================================

func computeGroup(g GroupInput) GroupResult {
	// Pre-filter: remove excused, unposted, and omit_from_final submissions.
	var active []SubmissionInput
	seen := map[int64]bool{} // deduplicate by assignment ID (edge case 7)
	for _, s := range g.Submissions {
		if s.Excused || s.Unposted || s.OmitFromFinal {
			continue
		}
		if seen[s.AssignmentID] {
			continue
		}
		seen[s.AssignmentID] = true
		active = append(active, s)
	}

	// Build current-track subs (only graded, non-pending)
	var currentSubs []SubmissionInput
	for _, s := range active {
		if s.Score != nil && !s.PendingReview {
			currentSubs = append(currentSubs, s)
		}
	}

	// Build final-track subs (all; ungraded/pending treated as score=0)
	finalSubs := make([]SubmissionInput, len(active))
	for i, s := range active {
		finalSubs[i] = s
		if s.Score == nil || s.PendingReview {
			z := zero
			finalSubs[i].Score = &z
		}
	}

	// Apply drops independently for each track
	dropRules := dropConfig{
		dropLowest:  g.DropLowest,
		dropHighest: g.DropHighest,
		neverDrop:   g.NeverDrop,
	}
	keptCurrent, droppedCurrent := applyDrops(currentSubs, dropRules)
	keptFinal, droppedFinal := applyDrops(finalSubs, dropRules)

	// Sum scores
	currentScore, currentPossible := sumScores(keptCurrent)
	finalScore, finalPossible := sumScores(keptFinal)

	// Build submission results
	droppedCurrentSet := toSet(droppedCurrent)
	droppedFinalSet := toSet(droppedFinal)

	var subResults []SubmissionResult
	for _, s := range g.Submissions {
		sr := SubmissionResult{
			AssignmentID:   s.AssignmentID,
			Score:          s.Score,
			PointsPossible: s.PointsPossible,
			Excused:        s.Excused,
			DroppedCurrent: droppedCurrentSet[s.AssignmentID],
			DroppedFinal:   droppedFinalSet[s.AssignmentID],
		}
		subResults = append(subResults, sr)
	}

	return GroupResult{
		ID:              g.ID,
		Name:            g.Name,
		Weight:          g.Weight,
		CurrentScore:    currentScore,
		FinalScore:      finalScore,
		CurrentPossible: currentPossible,
		FinalPossible:   finalPossible,
		DroppedCurrent:  droppedCurrent,
		DroppedFinal:    droppedFinal,
		Submissions:     subResults,
	}
}

func sumScores(subs []SubmissionInput) (score, possible decimal.Decimal) {
	score = zero
	possible = zero
	for _, s := range subs {
		if s.Score != nil {
			score = score.Add(*s.Score)
		}
		possible = possible.Add(s.PointsPossible)
	}
	return score, possible
}

// =============================================================================
// Course-level totals
// =============================================================================

// pointsTotal computes the points-mode course percentage.
func pointsTotal(groups []GroupResult, current bool) decimal.Decimal {
	totalScore := zero
	totalPossible := zero

	for _, g := range groups {
		if current {
			totalScore = totalScore.Add(g.CurrentScore)
			totalPossible = totalPossible.Add(g.CurrentPossible)
		} else {
			totalScore = totalScore.Add(g.FinalScore)
			totalPossible = totalPossible.Add(g.FinalPossible)
		}
	}

	if totalPossible.IsZero() {
		return zero
	}
	return totalScore.Div(totalPossible).Mul(hundred)
}

// weightedTotal computes the percent-mode (weighted groups) course percentage.
func weightedTotal(groups []GroupResult, current bool) decimal.Decimal {
	weightedSum := zero
	fullWeight := zero

	for _, g := range groups {
		var score, possible decimal.Decimal
		if current {
			score = g.CurrentScore
			possible = g.CurrentPossible
		} else {
			score = g.FinalScore
			possible = g.FinalPossible
		}

		// Groups with zero possible are excluded entirely (edge case 2).
		if possible.IsZero() {
			continue
		}

		groupPct := score.Div(possible)

		// Weight-0 groups contribute score but not to fullWeight (extra credit).
		if g.Weight.IsZero() {
			weightedSum = weightedSum.Add(groupPct.Mul(g.Weight))
			continue
		}

		weightedSum = weightedSum.Add(groupPct.Mul(g.Weight))
		fullWeight = fullWeight.Add(g.Weight)
	}

	if fullWeight.IsZero() {
		return zero
	}

	// Scale up if weights don't sum to 100 (edge case 1).
	return weightedSum.Div(fullWeight).Mul(hundred)
}

// =============================================================================
// Drop rules — Kane & Kane bisection algorithm
// =============================================================================

type dropConfig struct {
	dropLowest  int
	dropHighest int
	neverDrop   []int64
}

// applyDrops implements the Kane & Kane bisection algorithm for drop rules.
// Returns (kept submissions, dropped assignment IDs).
func applyDrops(subs []SubmissionInput, cfg dropConfig) ([]SubmissionInput, []int64) {
	if len(subs) == 0 || (cfg.dropLowest == 0 && cfg.dropHighest == 0) {
		return subs, nil
	}

	neverDropSet := toSet(cfg.neverDrop)

	// Separate into cannot-drop and droppable.
	var cannotDrop, droppable []SubmissionInput
	for _, s := range subs {
		if neverDropSet[s.AssignmentID] {
			cannotDrop = append(cannotDrop, s)
		} else {
			droppable = append(droppable, s)
		}
	}

	if len(droppable) == 0 {
		return subs, nil
	}

	// Clamp drop counts.
	dropLowest := cfg.dropLowest
	dropHighest := cfg.dropHighest
	if dropLowest > len(droppable)-1 {
		dropLowest = len(droppable) - 1
	}
	if dropLowest+dropHighest >= len(droppable) {
		dropHighest = 0
	}

	keepHighest := len(droppable) - dropLowest
	keepLowest := keepHighest - dropHighest

	// Phase 1: drop lowest (keep the best keepHighest submissions).
	kept := droppable
	if dropLowest > 0 {
		kept = bisectionKeep(kept, keepHighest, false)
	}

	// Phase 2: drop highest (keep the worst keepLowest submissions).
	if dropHighest > 0 {
		kept = bisectionKeep(kept, keepLowest, true)
	}

	// Determine which were dropped.
	keptSet := map[int64]bool{}
	for _, s := range kept {
		keptSet[s.AssignmentID] = true
	}
	var dropped []int64
	for _, s := range droppable {
		if !keptSet[s.AssignmentID] {
			dropped = append(dropped, s.AssignmentID)
		}
	}

	result := append(cannotDrop, kept...)
	return result, dropped
}

// bisectionKeep uses the Kane & Kane algorithm to select the optimal `keep`
// submissions. If reverse is false, it maximizes the group percentage (drop lowest).
// If reverse is true, it minimizes it (drop highest).
func bisectionKeep(subs []SubmissionInput, keep int, reverse bool) []SubmissionInput {
	if keep >= len(subs) {
		return subs
	}
	if keep <= 0 {
		return nil
	}

	// Compute the q values (score/possible for each submission) as bisection bounds.
	// The optimal q is between the min and max of score/possible ratios.
	var qValues []decimal.Decimal
	for _, s := range subs {
		if s.PointsPossible.IsZero() {
			continue
		}
		score := zero
		if s.Score != nil {
			score = *s.Score
		}
		qValues = append(qValues, score.Div(s.PointsPossible))
	}
	if len(qValues) == 0 {
		return subs[:keep]
	}

	sort.Slice(qValues, func(i, j int) bool {
		return qValues[i].LessThan(qValues[j])
	})
	qLow := qValues[0]
	qHigh := qValues[len(qValues)-1]

	// Bisection: find q where big_f(q) ≈ 0.
	// 30 iterations gives precision far beyond 2 decimal places.
	for range 30 {
		qMid := qLow.Add(qHigh).Div(decimal.NewFromInt(2))
		val := bigF(subs, qMid, keep, reverse)
		if val.IsPositive() {
			qLow = qMid
		} else {
			qHigh = qMid
		}
	}

	// Use the final q to select submissions.
	qFinal := qLow.Add(qHigh).Div(decimal.NewFromInt(2))
	return selectByQ(subs, qFinal, keep, reverse)
}

// rated pairs a submission's rate value with its assignment ID for sorting.
type rated struct {
	rate         decimal.Decimal
	assignmentID int64
}

// bigF computes the objective function for bisection.
// For each submission: rate = score - q * possible.
// Sort by rate (desc for drop-lowest, asc for drop-highest), take top `keep`, return sum.
func bigF(subs []SubmissionInput, q decimal.Decimal, keep int, reverse bool) decimal.Decimal {
	rates := make([]rated, len(subs))
	for i, s := range subs {
		score := zero
		if s.Score != nil {
			score = *s.Score
		}
		rates[i] = rated{
			rate:         score.Sub(q.Mul(s.PointsPossible)),
			assignmentID: s.AssignmentID,
		}
	}

	sortRated(rates, reverse)

	sum := zero
	for i := 0; i < keep && i < len(rates); i++ {
		sum = sum.Add(rates[i].rate)
	}
	return sum
}

// selectByQ picks the `keep` submissions using the rated-sort at a given q.
func selectByQ(subs []SubmissionInput, q decimal.Decimal, keep int, reverse bool) []SubmissionInput {
	type indexed struct {
		rate         decimal.Decimal
		assignmentID int64
		idx          int
	}
	items := make([]indexed, len(subs))
	for i, s := range subs {
		score := zero
		if s.Score != nil {
			score = *s.Score
		}
		items[i] = indexed{
			rate:         score.Sub(q.Mul(s.PointsPossible)),
			assignmentID: s.AssignmentID,
			idx:          i,
		}
	}

	// Sort: for drop-lowest (reverse=false), descending by rate.
	// Tie-break: drop-lowest drops highest assignment ID (so keep lowest ID first).
	// For drop-highest (reverse=true), ascending by rate.
	// Tie-break: drop-highest drops lowest assignment ID (so keep highest ID first).
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].rate.Equal(items[j].rate) {
			if reverse {
				return items[i].rate.LessThan(items[j].rate)
			}
			return items[i].rate.GreaterThan(items[j].rate)
		}
		// Tie-break by assignment ID.
		if reverse {
			// drop-highest: drop lowest ID → keep highest ID first
			return items[i].assignmentID > items[j].assignmentID
		}
		// drop-lowest: drop highest ID → keep lowest ID first
		return items[i].assignmentID < items[j].assignmentID
	})

	result := make([]SubmissionInput, keep)
	for i := 0; i < keep; i++ {
		result[i] = subs[items[i].idx]
	}
	return result
}

// sortRated sorts rated submissions for bigF evaluation.
func sortRated(rates []rated, reverse bool) {
	sort.SliceStable(rates, func(i, j int) bool {
		if !rates[i].rate.Equal(rates[j].rate) {
			if reverse {
				return rates[i].rate.LessThan(rates[j].rate)
			}
			return rates[i].rate.GreaterThan(rates[j].rate)
		}
		if reverse {
			return rates[i].assignmentID > rates[j].assignmentID
		}
		return rates[i].assignmentID < rates[j].assignmentID
	})
}

// =============================================================================
// Grading scheme lookup
// =============================================================================

// lookupGrade finds the letter grade for a percentage using a grading scheme.
// The scheme is sorted descending by value. Returns the first entry where
// value <= score/100.
func lookupGrade(score decimal.Decimal, scheme []SchemeEntry) *string {
	pct := score.Div(hundred)
	// Ensure scheme is sorted descending.
	sorted := make([]SchemeEntry, len(scheme))
	copy(sorted, scheme)
	slices.SortFunc(sorted, func(a, b SchemeEntry) int {
		return b.Value.Cmp(a.Value) // descending
	})
	for _, entry := range sorted {
		if pct.GreaterThanOrEqual(entry.Value) {
			name := entry.Name
			return &name
		}
	}
	// Below all thresholds — return the last (lowest) entry.
	if len(sorted) > 0 {
		name := sorted[len(sorted)-1].Name
		return &name
	}
	return nil
}

// =============================================================================
// Helpers
// =============================================================================

func toSet(ids []int64) map[int64]bool {
	m := make(map[int64]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}
