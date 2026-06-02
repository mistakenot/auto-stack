package cochange

import (
	"math"
	"sort"
)

// Scoring thresholds (AC-3). MinCoCommits drops candidates with too few raw
// co-changes; MinCommitsA gates whether A has enough history to score at all.
const (
	MinCoCommits = 3
	MinCommitsA  = 5
)

// ScoredCandidate is a candidate B with its derived scalar scores computed from
// the raw weighted aggregates. It is the intermediate the orchestrator turns
// into a RelatedFile.
type ScoredCandidate struct {
	Candidate      Candidate
	Score          float64
	ConfidenceAtoB float64
	ConfidenceBtoA float64
	Lift           float64
}

// safeDiv returns num/den, or 0 when den is non-positive (guards divide-by-zero
// and the degenerate empty-weight case so scores never become NaN/Inf).
func safeDiv(num, den float64) float64 {
	if den <= 0 {
		return 0
	}
	return num / den
}

// scoreCandidate computes the derived scalar scores for one candidate given A's
// weighted total Wa and the global weighted total Wn (solution.md authoritative
// formula):
//
//	confidence_a_to_b = Wab / Wa
//	confidence_b_to_a = Wab / Wb
//	lift              = (Wab * Wn) / (Wa * Wb)
//	score             = confidence_a_to_b * log1p(lift)
//
// Large commits are no longer dropped by a binary cutoff. They are damped
// continuously by the inverse-fan-out weight applied at load time
// (filesWeight = 1 / log1p(max(1, files_changed))), so a coupling observed in a
// big commit still contributes a small, non-zero amount rather than vanishing.
func scoreCandidate(c *Candidate, wa, wn float64) ScoredCandidate {
	confAtoB := safeDiv(c.Wab, wa)
	confBtoA := safeDiv(c.Wab, c.Wb)
	lift := safeDiv(c.Wab*wn, wa*c.Wb)
	score := confAtoB * math.Log1p(lift)
	return ScoredCandidate{
		Candidate:      *c,
		Score:          score,
		ConfidenceAtoB: confAtoB,
		ConfidenceBtoA: confBtoA,
		Lift:           lift,
	}
}

// ScoreAndRank applies the AC-3 co_commits filter, scores each surviving
// candidate, sorts by score descending (stable tiebreak by path ascending), and
// applies the limit (0 = no cap). It does NOT enforce the AC-3c commits(A) < 5
// gate — that is the orchestrator's responsibility because it changes the whole
// payload shape (metadata-only + warning). ScoreAndRank assumes A has enough
// history.
//
// limit < 0 is treated as no cap.
func ScoreAndRank(res *AggregateResult, limit int) []ScoredCandidate {
	if res == nil {
		return nil
	}
	scored := make([]ScoredCandidate, 0, len(res.Candidates))
	for i := range res.Candidates {
		c := &res.Candidates[i]
		if c.CoCommits < MinCoCommits {
			continue
		}
		scored = append(scored, scoreCandidate(c, res.Wa, res.Wn))
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].Candidate.Path < scored[j].Candidate.Path
	})

	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
}

// InsufficientHistory reports whether A has too few commits to score (AC-3c).
// The orchestrator returns a metadata-only payload with a warning in this case.
// It is only meaningful when A appears in history at all (CommitsA > 0).
func InsufficientHistory(res *AggregateResult) bool {
	return res != nil && res.CommitsA > 0 && res.CommitsA < MinCommitsA
}
