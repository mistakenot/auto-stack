package feedback

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureSpan(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "config", "user.email", "test@example.com")

	file := filepath.Join(repo, "docs", "a.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "seed")

	span, err := CaptureSpan(repo, "docs/a.md", 1, 2, false)
	if err != nil {
		t.Fatalf("capture span: %v", err)
	}
	if span.StartByte != 0 {
		t.Fatalf("start byte = %d", span.StartByte)
	}
	if span.ContentSnippet != "one\ntwo\n" {
		t.Fatalf("snippet = %q", span.ContentSnippet)
	}
	if span.SnippetSHA256 == "" {
		t.Fatal("expected snippet hash")
	}
	if span.ObservedBlobSHA == "" {
		t.Fatal("expected observed blob sha")
	}
	if span.CaptureSource != "head" {
		t.Fatalf("capture source = %q, want head", span.CaptureSource)
	}

	span2, err := CaptureSpan(repo, "docs/a.md", 1, 2, false)
	if err != nil {
		t.Fatalf("capture span second time: %v", err)
	}
	if span.SnippetSHA256 != span2.SnippetSHA256 {
		t.Fatalf("snippet sha should be deterministic: %q != %q", span.SnippetSHA256, span2.SnippetSHA256)
	}
}

func TestCaptureSpanDirtyFileUsesWorkingTreeSource(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "config", "user.email", "test@example.com")

	file := filepath.Join(repo, "docs", "dirty.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "seed")

	if err := os.WriteFile(file, []byte("changed line\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	span, err := CaptureSpan(repo, "docs/dirty.md", 1, 1, true)
	if err != nil {
		t.Fatalf("capture span: %v", err)
	}
	if span.CaptureSource != "working_tree" {
		t.Fatalf("capture source = %q, want working_tree", span.CaptureSource)
	}
	if !span.WorktreeDirty {
		t.Fatal("expected worktree_dirty=true")
	}
	if span.HeadBlobSHA == nil || *span.HeadBlobSHA == "" {
		t.Fatal("expected head_blob_sha for dirty tracked file")
	}
	if span.ObservedBlobSHA == *span.HeadBlobSHA {
		t.Fatal("expected observed blob sha to differ from head blob sha when file is dirty")
	}
}

func TestCaptureSpanHandlesNoTrailingNewlineAndEmptyFileError(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "config", "user.email", "test@example.com")

	file := filepath.Join(repo, "docs", "notrailing.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("alpha\nbeta"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "seed")

	span, err := CaptureSpan(repo, "docs/notrailing.md", 2, 2, false)
	if err != nil {
		t.Fatalf("capture span: %v", err)
	}
	if span.ContentSnippet != "beta" {
		t.Fatalf("unexpected snippet: %q", span.ContentSnippet)
	}

	empty := filepath.Join(repo, "docs", "empty.md")
	if err := os.WriteFile(empty, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = CaptureSpan(repo, "docs/empty.md", 1, 1, false)
	if err == nil {
		t.Fatal("expected error for empty file span")
	}
	if !strings.Contains(err.Error(), "exceeds file length") {
		t.Fatalf("expected file length error, got: %v", err)
	}
}

func runGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s failed: %v\nstderr:\n%s", strings.Join(args, " "), err, stderr.String())
	}
}
