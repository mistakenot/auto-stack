package cochange

import (
	"fmt"
	"math"
	"testing"

	"github.com/mistakenot/auto-search/internal/etlscan"
)

const dayMs = int64(24 * 60 * 60 * 1000)

func approx(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// findCandidate returns the candidate for the given canonical path, or nil.
func findCandidate(res *AggregateResult, path string) *Candidate {
	for i := range res.Candidates {
		if res.Candidates[i].Path == path {
			return &res.Candidates[i]
		}
	}
	return nil
}

func scored(res *AggregateResult, path string, limit int) *ScoredCandidate {
	ranked := ScoreAndRank(res, limit)
	for i := range ranked {
		if ranked[i].Candidate.Path == path {
			out := ranked[i]
			return &out
		}
	}
	return nil
}

// AC-2: primary score formula. Six commits all touching A and B at a fixed
// cohort size, decay off. With every commit identical, confidence_a_to_b == 1,
// confidence_b_to_a == 1, lift == Wn/Wa == 1, so score == 1*log1p(1).
func TestScore_FormulaExactNoDecay(t *testing.T) {
	var commits []etlscan.CommitSlim
	var files []etlscan.CommitFileSlim
	for _, sha := range []string{"a1", "a2", "a3", "a4", "a5", "a6"} {
		commits = append(commits, commit(sha, 1000, 2))
		files = append(files, touch(sha, "A.go"), touch(sha, "B.go"))
	}
	db := loadSynthetic(t, commits, files, nil)
	res, err := Aggregate(db, "A.go")
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	s := scored(res, "B.go", 0)
	if s == nil {
		t.Fatal("B.go missing from scored results")
	}
	if !approx(s.ConfidenceAtoB, 1) {
		t.Errorf("confidence_a_to_b = %v, want 1", s.ConfidenceAtoB)
	}
	if !approx(s.ConfidenceBtoA, 1) {
		t.Errorf("confidence_b_to_a = %v, want 1", s.ConfidenceBtoA)
	}
	if !approx(s.Lift, 1) {
		t.Errorf("lift = %v, want 1", s.Lift)
	}
	want := 1.0 * math.Log1p(1.0)
	if !approx(s.Score, want) {
		t.Errorf("score = %v, want %v", s.Score, want)
	}
}

// AC-18: candidate B has commits with no A involvement, so confidence_b_to_a < 1
// and Wb > Wab.
func TestScore_ConfidenceBtoALessThanOne(t *testing.T) {
	var commits []etlscan.CommitSlim
	var files []etlscan.CommitFileSlim
	// 5 commits touch both A and B.
	for _, sha := range []string{"c1", "c2", "c3", "c4", "c5"} {
		commits = append(commits, commit(sha, 1000, 2))
		files = append(files, touch(sha, "A.go"), touch(sha, "B.go"))
	}
	// 5 more commits touch only B (independent of A).
	for _, sha := range []string{"b1", "b2", "b3", "b4", "b5"} {
		commits = append(commits, commit(sha, 1000, 1))
		files = append(files, touch(sha, "B.go"))
	}
	db := loadSynthetic(t, commits, files, nil)
	res, err := Aggregate(db, "A.go")
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	cand := findCandidate(res, "B.go")
	if cand == nil {
		t.Fatal("B.go missing")
	}
	if !(cand.Wb > cand.Wab) {
		t.Errorf("expected Wb (%v) > Wab (%v)", cand.Wb, cand.Wab)
	}
	s := scored(res, "B.go", 0)
	if !(s.ConfidenceBtoA < 1) {
		t.Errorf("expected confidence_b_to_a < 1, got %v", s.ConfidenceBtoA)
	}
	if !approx(s.ConfidenceAtoB, 1) {
		t.Errorf("expected confidence_a_to_b == 1 (every A commit touches B), got %v", s.ConfidenceAtoB)
	}
}

// AC-3a: candidates with raw co_commits < 3 are dropped.
func TestScore_CoCommitsThreshold(t *testing.T) {
	var commits []etlscan.CommitSlim
	var files []etlscan.CommitFileSlim
	// A touched in 6 commits (so A has sufficient history).
	for i, sha := range []string{"a1", "a2", "a3", "a4", "a5", "a6"} {
		commits = append(commits, commit(sha, 1000, 2))
		files = append(files, touch(sha, "A.go"))
		// B co-changes in 3 of them (>= threshold), C in only 2 (< threshold).
		if i < 3 {
			files = append(files, touch(sha, "B.go"))
		} else if i < 5 {
			files = append(files, touch(sha, "C.go"))
		}
	}
	db := loadSynthetic(t, commits, files, nil)
	res, err := Aggregate(db, "A.go")
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	out := ScoreAndRank(res, 0)
	for _, s := range out {
		if s.Candidate.Path == "C.go" {
			t.Errorf("C.go (co_commits=2) should be filtered out")
		}
	}
	if scored(res, "B.go", 0) == nil {
		t.Errorf("B.go (co_commits=3) should survive the threshold")
	}
}

// AC-5: the binary large-commit cutoff is gone. A coupling observed only in a
// large (100-file) commit still contributes to aggregation, but its inverse
// fan-out weight (1 / log1p(files_changed)) makes that contribution small — much
// less than the per-edge weight of a focused 2-file commit. Here Big.go
// co-changes with A only in one 100-file commit; Small.go co-changes in four
// 2-file commits. Big.go must appear with 0 < Wab < the 2-file per-edge weight.
func TestScore_LargeCommitContributesContinuously(t *testing.T) {
	var commits []etlscan.CommitSlim
	var files []etlscan.CommitFileSlim
	// One 100-file commit touching A and Big.go (plus 98 unrelated paths so
	// files_changed is genuinely 100).
	commits = append(commits, commit("big1", 1000, 100))
	files = append(files, touch("big1", "A.go"), touch("big1", "Big.go"))
	for i := range 98 {
		files = append(files, touch("big1", fmt.Sprintf("noise/f%02d.go", i)))
	}
	// Four focused 2-file commits touching A and Small.go.
	for _, sha := range []string{"s1", "s2", "s3", "s4"} {
		commits = append(commits, commit(sha, 1000, 2))
		files = append(files, touch(sha, "A.go"), touch(sha, "Small.go"))
	}

	db := loadSynthetic(t, commits, files, nil)
	res, err := Aggregate(db, "A.go")
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	big := findCandidate(res, "Big.go")
	if big == nil {
		t.Fatal("Big.go co-changed in a 100-file commit and must still appear (no binary cutoff)")
	}
	if !(big.Wab > 0) {
		t.Errorf("Big.go Wab = %v, want > 0 (large commit still contributes)", big.Wab)
	}
	// Per-edge weight of a focused 2-file commit (decay disabled).
	smallEdge := commitWeight(2, 0, 0, true, 90)
	if !(big.Wab < smallEdge) {
		t.Errorf("Big.go Wab = %v, want < per-edge 2-file weight %v (continuous fan-out damping)", big.Wab, smallEdge)
	}
}

// AC-3c: when commits(A) < 5, InsufficientHistory is true; otherwise false.
func TestScore_InsufficientHistory(t *testing.T) {
	build := func(n int) *AggregateResult {
		var commits []etlscan.CommitSlim
		var files []etlscan.CommitFileSlim
		shas := []string{"a1", "a2", "a3", "a4", "a5", "a6"}
		for i := range n {
			commits = append(commits, commit(shas[i], 1000, 2))
			files = append(files, touch(shas[i], "A.go"), touch(shas[i], "B.go"))
		}
		db := loadSynthetic(t, commits, files, nil)
		res, err := Aggregate(db, "A.go")
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		return res
	}
	if !InsufficientHistory(build(4)) {
		t.Error("4 commits should be insufficient history")
	}
	if InsufficientHistory(build(5)) {
		t.Error("5 commits should be sufficient history")
	}
}

// AC-7: decay on vs off changes weighting. An older co-change commit contributes
// less under decay than under no-decay.
func TestScore_DecayOnVsOff(t *testing.T) {
	now := int64(1_700_000_000_000)
	old := now - 180*dayMs // ~2 tau old

	commits := []etlscan.CommitSlim{
		commit("recent", now, 2),
		commit("old", old, 2),
	}
	files := []etlscan.CommitFileSlim{
		touch("recent", "A.go"), touch("recent", "B.go"),
		touch("old", "A.go"), touch("old", "B.go"),
	}

	// No decay: both commits weighted equally.
	dbNo, err := LoadRows(commits, files, nil, LoadParams{RepoID: testRepoID, NoDecay: true})
	if err != nil {
		t.Fatalf("LoadRows no-decay: %v", err)
	}
	defer func() { _ = dbNo.Close() }()
	resNo, err := Aggregate(dbNo, "A.go")
	if err != nil {
		t.Fatalf("Aggregate no-decay: %v", err)
	}

	// Decay on (tau=90): the old commit's weight is reduced.
	dbDecay, err := LoadRows(commits, files, nil, LoadParams{RepoID: testRepoID, TauDays: 90})
	if err != nil {
		t.Fatalf("LoadRows decay: %v", err)
	}
	defer func() { _ = dbDecay.Close() }()
	resDecay, err := Aggregate(dbDecay, "A.go")
	if err != nil {
		t.Fatalf("Aggregate decay: %v", err)
	}

	if !(resDecay.Wa < resNo.Wa) {
		t.Errorf("expected decayed Wa (%v) < undecayed Wa (%v)", resDecay.Wa, resNo.Wa)
	}
}

// AC-7: a smaller --decay-tau decays the old commit harder than a larger tau.
func TestScore_DecayTauEffect(t *testing.T) {
	now := int64(1_700_000_000_000)
	old := now - 90*dayMs

	commits := []etlscan.CommitSlim{
		commit("recent", now, 2),
		commit("old", old, 2),
	}
	files := []etlscan.CommitFileSlim{
		touch("recent", "A.go"), touch("recent", "B.go"),
		touch("old", "A.go"), touch("old", "B.go"),
	}

	tauWa := func(tau float64) float64 {
		db, err := LoadRows(commits, files, nil, LoadParams{RepoID: testRepoID, TauDays: tau})
		if err != nil {
			t.Fatalf("LoadRows tau=%v: %v", tau, err)
		}
		defer func() { _ = db.Close() }()
		res, err := Aggregate(db, "A.go")
		if err != nil {
			t.Fatalf("Aggregate tau=%v: %v", tau, err)
		}
		return res.Wa
	}

	short := tauWa(30) // decays old commit hard
	long := tauWa(365) // decays old commit gently
	if !(short < long) {
		t.Errorf("expected smaller tau Wa (%v) < larger tau Wa (%v)", short, long)
	}
}

// AC-8: results are sorted by score descending and the limit is applied.
func TestScore_SortAndLimit(t *testing.T) {
	var commits []etlscan.CommitSlim
	var files []etlscan.CommitFileSlim
	// A in 6 commits. B co-changes in all 6 (strong), C in 4, D in 3 (weakest).
	for i, sha := range []string{"a1", "a2", "a3", "a4", "a5", "a6"} {
		commits = append(commits, commit(sha, 1000, 4))
		files = append(files, touch(sha, "A.go"), touch(sha, "B.go"))
		if i < 4 {
			files = append(files, touch(sha, "C.go"))
		}
		if i < 3 {
			files = append(files, touch(sha, "D.go"))
		}
	}
	db := loadSynthetic(t, commits, files, nil)
	res, err := Aggregate(db, "A.go")
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	all := ScoreAndRank(res, 0)
	if len(all) != 3 {
		t.Fatalf("expected 3 candidates above threshold, got %d", len(all))
	}
	// Descending score order.
	for i := 1; i < len(all); i++ {
		if all[i-1].Score < all[i].Score {
			t.Errorf("results not sorted desc: %v < %v at %d", all[i-1].Score, all[i].Score, i)
		}
	}
	if all[0].Candidate.Path != "B.go" {
		t.Errorf("strongest candidate should be B.go, got %s", all[0].Candidate.Path)
	}

	// Limit caps the list.
	limited := ScoreAndRank(res, 2)
	if len(limited) != 2 {
		t.Fatalf("limit 2 should return 2, got %d", len(limited))
	}
	if limited[0].Candidate.Path != "B.go" {
		t.Errorf("limited top should be B.go, got %s", limited[0].Candidate.Path)
	}
}

// safeDiv guards divide-by-zero (Wa or Wb == 0).
func TestScore_SafeDivGuards(t *testing.T) {
	if got := safeDiv(1, 0); got != 0 {
		t.Errorf("safeDiv(1,0) = %v, want 0", got)
	}
	if got := safeDiv(0, 0); got != 0 {
		t.Errorf("safeDiv(0,0) = %v, want 0", got)
	}
	c := Candidate{Wab: 1, Wb: 0, CoCommits: 5}
	s := scoreCandidate(&c, 0, 0)
	if math.IsNaN(s.Score) || math.IsInf(s.Score, 0) {
		t.Errorf("score with zero denominators must be finite, got %v", s.Score)
	}
}
