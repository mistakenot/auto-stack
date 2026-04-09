package github

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	gh "github.com/google/go-github/v72/github"
)

func makeMergedPR(number int, mergedAt, updatedAt string) *gh.PullRequest { //nolint:unparam // test helper
	title := fmt.Sprintf("PR #%d", number)
	state := "closed"
	author := "author"
	base := "main"
	head := "feature"
	sha := "abc123"
	return &gh.PullRequest{
		Number:    &number,
		State:     &state,
		Title:     &title,
		MergedAt:  timestamp(mergedAt),
		UpdatedAt: timestamp(updatedAt),
		User:      &gh.User{Login: &author},
		Base:      &gh.PullRequestBranch{Ref: &base},
		Head:      &gh.PullRequestBranch{Ref: &head, SHA: &sha},
	}
}

func TestFetchAll_BasicSync(t *testing.T) {
	mock := &MockClient{
		PullRequests: []*gh.PullRequest{
			makeMergedPR(1, "2026-04-01T12:00:00Z", "2026-04-01T12:00:00Z"),
		},
	}

	dir := t.TempDir()
	repo := RepoRef{Owner: "owner", Repo: "repo", GitRemote: "https://github.com/owner/repo.git"}
	cfg := FetchConfig{
		HostID:        "test-host",
		SyncStatePath: filepath.Join(dir, "sync-state.json"),
	}

	result, summary, err := FetchAll(context.Background(), mock, []RepoRef{repo}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.PullRequests) != 1 {
		t.Errorf("got %d PRs, want 1", len(result.PullRequests))
	}
	if summary.PRsSynced != 1 {
		t.Errorf("PRsSynced = %d, want 1", summary.PRsSynced)
	}
	if summary.ReposProcessed != 1 {
		t.Errorf("ReposProcessed = %d, want 1", summary.ReposProcessed)
	}

	// Verify sync state was persisted
	state := LoadSyncState(cfg.SyncStatePath)
	r := state.GetRepo("owner/repo")
	if info, ok := r.PRs["owner/repo#1"]; !ok || !info.Synced {
		t.Error("PR 1 should be marked synced in state")
	}
}

func TestFetchAll_SkipsSyncedPRs(t *testing.T) {
	mock := &MockClient{
		PullRequests: []*gh.PullRequest{
			makeMergedPR(1, "2026-04-01T12:00:00Z", "2026-04-01T12:00:00Z"),
		},
	}

	dir := t.TempDir()
	statePath := filepath.Join(dir, "sync-state.json")

	// Pre-populate sync state with PR #1 as synced
	state := newSyncState()
	repo := state.GetRepo("owner/repo")
	repo.MarkSynced("owner/repo#1", nil)
	state.Save(statePath)

	repoRef := RepoRef{Owner: "owner", Repo: "repo", GitRemote: "https://github.com/owner/repo.git"}
	cfg := FetchConfig{
		HostID:        "test-host",
		SyncStatePath: statePath,
	}

	result, summary, err := FetchAll(context.Background(), mock, []RepoRef{repoRef}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.PullRequests) != 0 {
		t.Errorf("got %d PRs, want 0 (should skip synced)", len(result.PullRequests))
	}
	if summary.PRsSynced != 0 {
		t.Errorf("PRsSynced = %d, want 0", summary.PRsSynced)
	}
}

func TestFetchAll_SkipsNonMerged(t *testing.T) {
	mock := &MockClient{
		PullRequests: []*gh.PullRequest{
			makeMergedPR(1, "2026-04-01T12:00:00Z", "2026-04-01T12:00:00Z"),
			// Open PR (no merged_at)
			{
				Number:    p(2),
				State:     p("open"),
				Title:     p("WIP"),
				UpdatedAt: timestamp("2026-04-01T12:00:00Z"),
				User:      &gh.User{Login: p("author")},
				Base:      &gh.PullRequestBranch{},
				Head:      &gh.PullRequestBranch{SHA: p("def456")},
			},
			// Closed without merge
			{
				Number:    p(3),
				State:     p("closed"),
				Title:     p("Abandoned"),
				UpdatedAt: timestamp("2026-04-01T12:00:00Z"),
				User:      &gh.User{Login: p("author")},
				Base:      &gh.PullRequestBranch{},
				Head:      &gh.PullRequestBranch{SHA: p("ghi789")},
			},
		},
	}

	dir := t.TempDir()
	repoRef := RepoRef{Owner: "owner", Repo: "repo", GitRemote: "https://github.com/owner/repo.git"}
	cfg := FetchConfig{
		HostID:        "test-host",
		SyncStatePath: filepath.Join(dir, "sync-state.json"),
	}

	result, _, err := FetchAll(context.Background(), mock, []RepoRef{repoRef}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.PullRequests) != 1 {
		t.Errorf("got %d PRs, want 1 (only merged)", len(result.PullRequests))
	}
}

func TestFetchAll_NonCriticalFailure(t *testing.T) {
	mock := &MockClient{
		PullRequests: []*gh.PullRequest{
			makeMergedPR(1, "2026-04-01T12:00:00Z", "2026-04-01T12:00:00Z"),
		},
		GetDiffErr:       errors.New("diff not available"),
		ListFilesErr:     errors.New("files not available"),
		ListCheckRunsErr: errors.New("no Actions scope"),
	}

	dir := t.TempDir()
	repoRef := RepoRef{Owner: "owner", Repo: "repo", GitRemote: "https://github.com/owner/repo.git"}
	cfg := FetchConfig{
		HostID:        "test-host",
		SyncStatePath: filepath.Join(dir, "sync-state.json"),
	}

	result, summary, err := FetchAll(context.Background(), mock, []RepoRef{repoRef}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// PR should still be synced despite non-critical failures
	if len(result.PullRequests) != 1 {
		t.Errorf("got %d PRs, want 1", len(result.PullRequests))
	}
	if result.PullRequests[0].Diff != "" {
		t.Error("diff should be empty on failure")
	}
	if result.PullRequests[0].ChecksJSON != "[]" {
		t.Errorf("checks should be empty array, got %q", result.PullRequests[0].ChecksJSON)
	}

	// Should have warnings
	if len(summary.Warnings) < 2 {
		t.Errorf("expected at least 2 warnings, got %d", len(summary.Warnings))
	}

	// Verify synced with missing fields
	state := LoadSyncState(cfg.SyncStatePath)
	r := state.GetRepo("owner/repo")
	info := r.PRs["owner/repo#1"]
	if info == nil || !info.Synced {
		t.Error("PR should be marked synced")
	}
	if len(info.MissingFields) == 0 {
		t.Error("should have missing_fields recorded")
	}
}

func TestFetchAll_CriticalFailureMarksUnsync(t *testing.T) {
	mock := &MockClient{
		PullRequests: []*gh.PullRequest{
			makeMergedPR(1, "2026-04-01T12:00:00Z", "2026-04-01T12:00:00Z"),
		},
		ListReviewsErr: errors.New("500 internal server error"),
	}

	dir := t.TempDir()
	repoRef := RepoRef{Owner: "owner", Repo: "repo", GitRemote: "https://github.com/owner/repo.git"}
	cfg := FetchConfig{
		HostID:        "test-host",
		SyncStatePath: filepath.Join(dir, "sync-state.json"),
	}

	result, summary, err := FetchAll(context.Background(), mock, []RepoRef{repoRef}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// PR should NOT be in results (critical failure)
	if len(result.PullRequests) != 0 {
		t.Errorf("got %d PRs, want 0 (critical failure)", len(result.PullRequests))
	}

	// Should have warning
	if len(summary.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(summary.Warnings))
	}

	// Verify marked as failed in sync state
	state := LoadSyncState(cfg.SyncStatePath)
	r := state.GetRepo("owner/repo")
	info := r.PRs["owner/repo#1"]
	if info == nil || info.Synced {
		t.Error("PR should be marked as failed (synced=false)")
	}
}

func TestParsePRNumber(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"owner/repo#123", 123},
		{"owner/repo#1", 1},
		{"org/big-repo#9999", 9999},
	}
	for _, tt := range tests {
		got, err := parsePRNumber(tt.input)
		if err != nil {
			t.Errorf("parsePRNumber(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parsePRNumber(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
