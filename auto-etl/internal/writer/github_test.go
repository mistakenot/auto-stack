package writer

import (
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-etl/internal/model"
)

func TestMergePRs_Dedup(t *testing.T) {
	existing := []model.PullRequest{
		{ID: "owner/repo#1", Title: "Old"},
		{ID: "owner/repo#2", Title: "Unchanged"},
	}
	incoming := []model.PullRequest{
		{ID: "owner/repo#1", Title: "Updated"},
		{ID: "owner/repo#3", Title: "New"},
	}

	merged := mergePRs(existing, incoming)
	if len(merged) != 3 {
		t.Fatalf("got %d rows, want 3", len(merged))
	}

	// Check that #1 is updated (new data wins)
	byID := map[string]model.PullRequest{}
	for _, pr := range merged {
		byID[pr.ID] = pr
	}
	if byID["owner/repo#1"].Title != "Updated" {
		t.Errorf("PR #1 Title = %q, want Updated", byID["owner/repo#1"].Title)
	}
	if byID["owner/repo#2"].Title != "Unchanged" {
		t.Errorf("PR #2 Title = %q, want Unchanged", byID["owner/repo#2"].Title)
	}
}

func TestMergePRs_EmptyExisting(t *testing.T) {
	incoming := []model.PullRequest{{ID: "owner/repo#1"}}
	merged := mergePRs(nil, incoming)
	if len(merged) != 1 {
		t.Fatalf("got %d, want 1", len(merged))
	}
}

func TestMergeComments_ReplacesAllForRetried(t *testing.T) {
	existing := []model.PRComment{
		{ID: "owner/repo#1/c/100", PRID: "owner/repo#1", Body: "old comment 1"},
		{ID: "owner/repo#1/c/101", PRID: "owner/repo#1", Body: "old comment 2"},
		{ID: "owner/repo#2/c/200", PRID: "owner/repo#2", Body: "unrelated"},
	}
	incoming := []model.PRComment{
		{ID: "owner/repo#1/c/100", PRID: "owner/repo#1", Body: "new comment 1"},
		{ID: "owner/repo#1/c/102", PRID: "owner/repo#1", Body: "new comment 3"},
	}

	merged := mergeComments(existing, incoming)

	// Should have: 1 unrelated + 2 new = 3
	if len(merged) != 3 {
		t.Fatalf("got %d comments, want 3", len(merged))
	}

	byID := map[string]model.PRComment{}
	for _, c := range merged {
		byID[c.ID] = c
	}

	// Old comment 101 should be gone (all old comments for PR#1 replaced)
	if _, ok := byID["owner/repo#1/c/101"]; ok {
		t.Error("old comment 101 should be removed for retried PR")
	}
	// Unrelated PR#2 comment should be preserved
	if _, ok := byID["owner/repo#2/c/200"]; !ok {
		t.Error("unrelated comment should be preserved")
	}
	// New comment should be present
	if byID["owner/repo#1/c/100"].Body != "new comment 1" {
		t.Errorf("comment 100 body = %q, want 'new comment 1'", byID["owner/repo#1/c/100"].Body)
	}
}

func TestWriteGitHub_Roundtrip(t *testing.T) {
	dir := t.TempDir()

	result := &model.GitHubSyncResult{
		PullRequests: []model.PullRequest{
			{
				ID:    "owner/repo#1",
				Title: "Test PR",
				Year:  2026,
				Month: 4,
			},
		},
		Comments: []model.PRComment{
			{
				ID:          "owner/repo#1/c/100",
				PRID:        "owner/repo#1",
				CommentType: "review",
				Body:        "LGTM",
				Year:        2026,
				Month:       4,
			},
		},
	}

	if err := WriteGitHub(dir, result); err != nil {
		t.Fatal(err)
	}

	// Read back PRs
	prPath := filepath.Join(dir, "pull_requests", "year=2026", "month=04", "pull_requests.parquet")
	prs, err := readExistingParquet[model.PullRequest](prPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || prs[0].Title != "Test PR" {
		t.Errorf("read back: %d PRs, title=%q", len(prs), prs[0].Title)
	}

	// Read back comments
	commentPath := filepath.Join(dir, "pull_request_comments", "year=2026", "month=04", "pull_request_comments.parquet")
	comments, err := readExistingParquet[model.PRComment](commentPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].Body != "LGTM" {
		t.Errorf("read back: %d comments, body=%q", len(comments), comments[0].Body)
	}

	// Write again with an additional PR → should merge
	result2 := &model.GitHubSyncResult{
		PullRequests: []model.PullRequest{
			{
				ID:    "owner/repo#2",
				Title: "Second PR",
				Year:  2026,
				Month: 4,
			},
		},
		Comments: nil,
	}

	if err := WriteGitHub(dir, result2); err != nil {
		t.Fatal(err)
	}

	prs, err = readExistingParquet[model.PullRequest](prPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 {
		t.Errorf("after merge: got %d PRs, want 2", len(prs))
	}
}

func TestWriteGitHub_Idempotent(t *testing.T) {
	dir := t.TempDir()

	result := &model.GitHubSyncResult{
		PullRequests: []model.PullRequest{
			{ID: "owner/repo#1", Title: "Test", Year: 2026, Month: 4},
		},
	}

	// Write twice
	WriteGitHub(dir, result)
	WriteGitHub(dir, result)

	prPath := filepath.Join(dir, "pull_requests", "year=2026", "month=04", "pull_requests.parquet")
	prs, _ := readExistingParquet[model.PullRequest](prPath)
	if len(prs) != 1 {
		t.Errorf("got %d PRs, want 1 (deduped)", len(prs))
	}
}

func TestWriteGitHub_Empty(t *testing.T) {
	dir := t.TempDir()
	err := WriteGitHub(dir, &model.GitHubSyncResult{})
	if err != nil {
		t.Fatal(err)
	}
	// Should not create any files
}
