package scenariofixture

import (
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-search/internal/etlscan"
)

// TestLoadScenario_RoundTrip writes the 2-commit insufficient_history scenario
// to parquet and reads it back through the slim etlscan readers, confirming the
// expanded row counts and a derived field (commit_id = "<repo_id>-<sha>").
func TestLoadScenario_RoundTrip(t *testing.T) {
	root := LoadScenario(t, "insufficient_history")

	commits, err := etlscan.ReadCommitsSlim(filepath.Join(root, "commits", "commits.parquet"))
	if err != nil {
		t.Fatalf("ReadCommitsSlim: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits: got %d, want 2", len(commits))
	}

	files, err := etlscan.ReadCommitFilesSlim(filepath.Join(root, "commit_files", "commit_files.parquet"))
	if err != nil {
		t.Fatalf("ReadCommitFilesSlim: %v", err)
	}
	// 2 commits × 2 files each = 4 commit_files rows.
	if len(files) != 4 {
		t.Fatalf("commit_files: got %d, want 4", len(files))
	}

	repos, err := etlscan.ReadGitRepos(filepath.Join(root, "git_repositories", "git_repositories.parquet"))
	if err != nil {
		t.Fatalf("ReadGitRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].RepoID != "fixture-repo" {
		t.Fatalf("git_repositories: got %+v, want one row with repo_id=fixture-repo", repos)
	}

	// commit_id carries the "<repo_id>-<sha>" prefix the cochange joins expect.
	for _, f := range files {
		if f.RepoID != "fixture-repo" {
			t.Errorf("commit_file repo_id = %q, want fixture-repo", f.RepoID)
		}
		if got := f.CommitID; got != "fixture-repo-aaaa0001" && got != "fixture-repo-aaaa0002" {
			t.Errorf("commit_file commit_id = %q, want fixture-repo-<sha>", got)
		}
	}
}
