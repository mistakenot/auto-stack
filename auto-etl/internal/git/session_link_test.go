package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-etl/internal/model"
	"github.com/parquet-go/parquet-go"
)

// writeTestParquet writes messageRow records to a parquet file for testing.
func writeTestParquet(t *testing.T, path string, rows []messageRow) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := parquet.NewGenericWriter[messageRow](f)
	if _, err := w.Write(rows); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLinkSessionIDs_CommitCommand(t *testing.T) {
	// AC-2: commit-creating bash command with matching short SHA
	dir := t.TempDir()
	msgDir := filepath.Join(dir, "messages")
	writeTestParquet(t, filepath.Join(msgDir, "test.parquet"), []messageRow{
		{
			SessionID:   "session-abc",
			BashCommand: "git commit -m 'test'",
			Content:     "[main abcdef1] test commit\n 1 file changed",
			GitRemote:   "github.com/example/repo",
		},
	})

	commits := []model.Commit{
		{
			ID:     "repoid-abcdef1234567890abcdef1234567890abcdef12",
			RepoID: "repoid",
		},
	}

	err := LinkSessionIDs(commits, msgDir, "github.com/example/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commits[0].SessionID != "session-abc" {
		t.Errorf("expected session-abc, got %q", commits[0].SessionID)
	}
}

func TestLinkSessionIDs_GitLogIgnored(t *testing.T) {
	// AC-3: git log command should NOT match
	dir := t.TempDir()
	msgDir := filepath.Join(dir, "messages")
	writeTestParquet(t, filepath.Join(msgDir, "test.parquet"), []messageRow{
		{
			SessionID:   "session-xyz",
			BashCommand: "git log --oneline",
			Content:     "[main abcdef1] some message",
			GitRemote:   "github.com/example/repo",
		},
	})

	commits := []model.Commit{
		{
			ID:     "repoid-abcdef1234567890abcdef1234567890abcdef12",
			RepoID: "repoid",
		},
	}

	err := LinkSessionIDs(commits, msgDir, "github.com/example/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commits[0].SessionID != "" {
		t.Errorf("expected empty session ID for git log match, got %q", commits[0].SessionID)
	}
}

func TestLinkSessionIDs_CatIgnored(t *testing.T) {
	// AC-3: cat command should NOT match
	dir := t.TempDir()
	msgDir := filepath.Join(dir, "messages")
	writeTestParquet(t, filepath.Join(msgDir, "test.parquet"), []messageRow{
		{
			SessionID:   "session-xyz",
			BashCommand: "cat output.txt",
			Content:     "[main abcdef1] some text",
			GitRemote:   "github.com/example/repo",
		},
	})

	commits := []model.Commit{
		{
			ID:     "repoid-abcdef1234567890abcdef1234567890abcdef12",
			RepoID: "repoid",
		},
	}

	err := LinkSessionIDs(commits, msgDir, "github.com/example/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commits[0].SessionID != "" {
		t.Errorf("expected empty session ID for cat match, got %q", commits[0].SessionID)
	}
}

func TestLinkSessionIDs_TrailerTakesPrecedence(t *testing.T) {
	// AC-4: existing SessionID from trailer should not be overwritten
	dir := t.TempDir()
	msgDir := filepath.Join(dir, "messages")
	writeTestParquet(t, filepath.Join(msgDir, "test.parquet"), []messageRow{
		{
			SessionID:   "session-fallback",
			BashCommand: "git commit -m 'test'",
			Content:     "[main abcdef1] test",
			GitRemote:   "github.com/example/repo",
		},
	})

	commits := []model.Commit{
		{
			ID:        "repoid-abcdef1234567890abcdef1234567890abcdef12",
			RepoID:    "repoid",
			SessionID: "session-trailer",
		},
	}

	err := LinkSessionIDs(commits, msgDir, "github.com/example/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commits[0].SessionID != "session-trailer" {
		t.Errorf("trailer should take precedence, got %q", commits[0].SessionID)
	}
}

func TestLinkSessionIDs_NoMatch(t *testing.T) {
	// AC-5: no matching messages -> empty session ID
	dir := t.TempDir()
	msgDir := filepath.Join(dir, "messages")
	writeTestParquet(t, filepath.Join(msgDir, "test.parquet"), []messageRow{
		{
			SessionID:   "session-other",
			BashCommand: "git commit -m 'test'",
			Content:     "[main 9999999] unrelated commit",
			GitRemote:   "github.com/example/repo",
		},
	})

	commits := []model.Commit{
		{
			ID:     "repoid-abcdef1234567890abcdef1234567890abcdef12",
			RepoID: "repoid",
		},
	}

	err := LinkSessionIDs(commits, msgDir, "github.com/example/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commits[0].SessionID != "" {
		t.Errorf("expected empty session ID for no match, got %q", commits[0].SessionID)
	}
}

func TestLinkSessionIDs_NoMessagesDir(t *testing.T) {
	// Missing messages directory should not error
	commits := []model.Commit{
		{
			ID:     "repoid-abcdef1234567890abcdef1234567890abcdef12",
			RepoID: "repoid",
		},
	}

	err := LinkSessionIDs(commits, "/nonexistent/path/messages", "github.com/example/repo")
	if err != nil {
		t.Fatalf("unexpected error for missing dir: %v", err)
	}
	if commits[0].SessionID != "" {
		t.Errorf("expected empty session ID, got %q", commits[0].SessionID)
	}
}

func TestLinkSessionIDs_DifferentRepo(t *testing.T) {
	// Messages from different repo should not match
	dir := t.TempDir()
	msgDir := filepath.Join(dir, "messages")
	writeTestParquet(t, filepath.Join(msgDir, "test.parquet"), []messageRow{
		{
			SessionID:   "session-other-repo",
			BashCommand: "git commit -m 'test'",
			Content:     "[main abcdef1] test commit",
			GitRemote:   "github.com/other/repo",
		},
	})

	commits := []model.Commit{
		{
			ID:     "repoid-abcdef1234567890abcdef1234567890abcdef12",
			RepoID: "repoid",
		},
	}

	err := LinkSessionIDs(commits, msgDir, "github.com/example/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commits[0].SessionID != "" {
		t.Errorf("expected empty session ID for different repo, got %q", commits[0].SessionID)
	}
}

func TestLinkSessionIDs_PrefixMatch(t *testing.T) {
	// 7-char captured SHA should match commit with full 40-char SHA
	dir := t.TempDir()
	msgDir := filepath.Join(dir, "messages")
	writeTestParquet(t, filepath.Join(msgDir, "test.parquet"), []messageRow{
		{
			SessionID:   "session-prefix",
			BashCommand: "git commit -m 'test'",
			Content:     "[main abcdef1] test commit",
			GitRemote:   "github.com/example/repo",
		},
	})

	commits := []model.Commit{
		{
			ID:     "repoid-abcdef1999999999999999999999999999999999",
			RepoID: "repoid",
		},
	}

	err := LinkSessionIDs(commits, msgDir, "github.com/example/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commits[0].SessionID != "session-prefix" {
		t.Errorf("expected session-prefix for prefix match, got %q", commits[0].SessionID)
	}
}

func TestLinkSessionIDs_AmbiguousPrefix(t *testing.T) {
	// Two different sessions matching the same commit prefix -> no match (AC-5)
	dir := t.TempDir()
	msgDir := filepath.Join(dir, "messages")
	writeTestParquet(t, filepath.Join(msgDir, "test.parquet"), []messageRow{
		{
			SessionID:   "session-1",
			BashCommand: "git commit -m 'first'",
			Content:     "[main abcdef1] first commit",
			GitRemote:   "github.com/example/repo",
		},
		{
			SessionID:   "session-2",
			BashCommand: "git commit -m 'second'",
			Content:     "[main abcdef12345] second commit",
			GitRemote:   "github.com/example/repo",
		},
	})

	commits := []model.Commit{
		{
			ID:     "repoid-abcdef1234567890abcdef1234567890abcdef12",
			RepoID: "repoid",
		},
	}

	err := LinkSessionIDs(commits, msgDir, "github.com/example/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both "abcdef1" and "abcdef12345" are prefixes of the full SHA,
	// but they map to different sessions, so this is ambiguous → skip.
	if commits[0].SessionID != "" {
		t.Errorf("expected empty session ID for ambiguous prefix, got %q", commits[0].SessionID)
	}
}

func TestLinkSessionIDs_AllHaveTrailer(t *testing.T) {
	// When all commits already have SessionID, skip entirely (early return)
	commits := []model.Commit{
		{
			ID:        "repoid-abcdef1234567890abcdef1234567890abcdef12",
			RepoID:    "repoid",
			SessionID: "already-set",
		},
	}

	// Pass a nonexistent dir — should not matter since we short-circuit
	err := LinkSessionIDs(commits, "/nonexistent/path/messages", "github.com/example/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commits[0].SessionID != "already-set" {
		t.Errorf("expected already-set, got %q", commits[0].SessionID)
	}
}

func TestLinkSessionIDs_MergeCommand(t *testing.T) {
	// AC-3: git merge should be considered a commit-creating command
	dir := t.TempDir()
	msgDir := filepath.Join(dir, "messages")
	writeTestParquet(t, filepath.Join(msgDir, "test.parquet"), []messageRow{
		{
			SessionID:   "session-merge",
			BashCommand: "git merge feature-branch",
			Content:     "[main abcdef1] Merge branch 'feature-branch'",
			GitRemote:   "github.com/example/repo",
		},
	})

	commits := []model.Commit{
		{
			ID:     "repoid-abcdef1234567890abcdef1234567890abcdef12",
			RepoID: "repoid",
		},
	}

	err := LinkSessionIDs(commits, msgDir, "github.com/example/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commits[0].SessionID != "session-merge" {
		t.Errorf("expected session-merge, got %q", commits[0].SessionID)
	}
}

func TestLinkSessionIDs_CherryPickCommand(t *testing.T) {
	// AC-3: git cherry-pick should be considered a commit-creating command
	dir := t.TempDir()
	msgDir := filepath.Join(dir, "messages")
	writeTestParquet(t, filepath.Join(msgDir, "test.parquet"), []messageRow{
		{
			SessionID:   "session-cherry",
			BashCommand: "git cherry-pick abc123",
			Content:     "[main abcdef1] cherry-picked commit",
			GitRemote:   "github.com/example/repo",
		},
	})

	commits := []model.Commit{
		{
			ID:     "repoid-abcdef1234567890abcdef1234567890abcdef12",
			RepoID: "repoid",
		},
	}

	err := LinkSessionIDs(commits, msgDir, "github.com/example/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commits[0].SessionID != "session-cherry" {
		t.Errorf("expected session-cherry, got %q", commits[0].SessionID)
	}
}
