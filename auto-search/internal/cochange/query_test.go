package cochange

import (
	"testing"

	"github.com/mistakenot/auto-search/internal/etlscan"
)

const testRepoID = "repo1"

// commitID builds the production-format commit id (<repoID>-<sha>).
func commitID(sha string) string { return testRepoID + "-" + sha }

// loadSynthetic loads hand-built rows through the production LoadRows path with
// decay disabled (so weights depend only on cohort size and are deterministic).
func loadSynthetic(t *testing.T, commits []etlscan.CommitSlim, files []etlscan.CommitFileSlim, refs []etlscan.GitRefSlim) *DB {
	t.Helper()
	db, err := LoadRows(commits, files, refs, LoadParams{RepoID: testRepoID, NoDecay: true})
	if err != nil {
		t.Fatalf("LoadRows: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func commit(sha string, date int64, filesChanged int32) etlscan.CommitSlim {
	return etlscan.CommitSlim{
		ID:               commitID(sha),
		ShortID:          sha,
		RepoID:           testRepoID,
		AuthorName:       "Dev",
		AuthorEmail:      "dev@example.com",
		AuthorDate:       date,
		FilesChanged:     filesChanged,
		SessionID:        "sess-" + sha,
		MessageTruncated: "commit " + sha,
	}
}

func touch(sha, path string) etlscan.CommitFileSlim {
	return etlscan.CommitFileSlim{
		CommitID:   commitID(sha),
		RepoID:     testRepoID,
		FilePath:   path,
		ChangeType: "M",
	}
}

func rename(sha, oldPath, newPath string) etlscan.CommitFileSlim {
	return etlscan.CommitFileSlim{
		CommitID:   commitID(sha),
		RepoID:     testRepoID,
		FilePath:   newPath,
		ChangeType: "R",
		OldPath:    oldPath,
	}
}

// (i) A renamed candidate is canonicalised to its current path and appears once.
func TestAggregate_RenamedCandidateCanonicalised(t *testing.T) {
	commits := []etlscan.CommitSlim{
		commit("s1", 1000, 2),
		commit("s2", 2000, 2),
		commit("s3", 3000, 2),
	}
	files := []etlscan.CommitFileSlim{
		touch("s1", "a.go"), touch("s1", "old_b.go"),
		touch("s2", "a.go"), rename("s2", "old_b.go", "new_b.go"),
		touch("s3", "a.go"), touch("s3", "new_b.go"),
	}
	db := loadSynthetic(t, commits, files, nil)

	res, err := Aggregate(db, "a.go")
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if got := len(res.Candidates); got != 1 {
		t.Fatalf("expected exactly 1 candidate, got %d: %+v", got, res.Candidates)
	}
	cand := res.Candidates[0]
	if cand.Path != "new_b.go" {
		t.Errorf("candidate path = %q, want canonical %q", cand.Path, "new_b.go")
	}
	// a.go co-occurs with b's lineage in all 3 commits.
	if cand.CoCommits != 3 {
		t.Errorf("co_commits = %d, want 3", cand.CoCommits)
	}
}

// (ii) Wb counts B's commits with no A involvement, so Wb > Wab.
func TestAggregate_WbCountsIndependentCommits(t *testing.T) {
	commits := []etlscan.CommitSlim{
		commit("m1", 1000, 2),
		commit("m2", 2000, 2),
		commit("m3", 3000, 1),
		commit("m4", 4000, 1),
	}
	files := []etlscan.CommitFileSlim{
		touch("m1", "a.go"), touch("m1", "b.go"),
		touch("m2", "a.go"), touch("m2", "b.go"),
		touch("m3", "b.go"),
		touch("m4", "b.go"),
	}
	db := loadSynthetic(t, commits, files, nil)

	res, err := Aggregate(db, "a.go")
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(res.Candidates))
	}
	cand := res.Candidates[0]
	if cand.Path != "b.go" {
		t.Fatalf("candidate = %q, want b.go", cand.Path)
	}
	if cand.CoCommits != 2 {
		t.Errorf("co_commits = %d, want 2", cand.CoCommits)
	}
	if cand.CommitsB != 4 {
		t.Errorf("commits_b = %d, want 4 (2 co + 2 independent)", cand.CommitsB)
	}
	if !(cand.Wb > cand.Wab) {
		t.Errorf("expected Wb (%v) > Wab (%v) because B has non-A commits", cand.Wb, cand.Wab)
	}
}

// (iii) The ref-tip join returns the seeded default branch (raw-sha join).
func TestRefTips_SeededDefaultBranch(t *testing.T) {
	commits := []etlscan.CommitSlim{
		commit("s1", 1000, 2),
		commit("s2", 2000, 2),
	}
	files := []etlscan.CommitFileSlim{
		touch("s1", "a.go"), touch("s1", "b.go"),
		touch("s2", "a.go"), touch("s2", "b.go"),
	}
	// refs.commit_id is the RAW sha (no <repoID>- prefix).
	refs := []etlscan.GitRefSlim{
		{RepoID: testRepoID, RefName: "main", RefType: "branch", CommitID: "s2", IsDefault: true},
		{RepoID: testRepoID, RefName: "stale", RefType: "branch", CommitID: "deadbeef", IsDefault: false},
	}
	db := loadSynthetic(t, commits, files, refs)

	tips, err := RefTips(db, "a.go")
	if err != nil {
		t.Fatalf("RefTips: %v", err)
	}
	if len(tips) != 1 {
		t.Fatalf("expected 1 ref tip (the default branch tip on a touched commit), got %d: %+v", len(tips), tips)
	}
	if tips[0].RefName != "main" || !tips[0].IsDefault {
		t.Errorf("ref tip = %+v, want {main, default}", tips[0])
	}
}
