package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var testBinaryPath string

func TestMain(m *testing.M) {
	binDir, err := os.MkdirTemp("", "autoreflect-e2e-bin-")
	if err != nil {
		panic(err)
	}

	testBinaryPath = filepath.Join(binDir, "autoreflect")
	build := exec.Command("go", "build", "-o", testBinaryPath, "./cmd/autoreflect")
	build.Dir = filepath.Join("..", "..")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("build failed: " + err.Error())
	}

	code := m.Run()
	_ = os.RemoveAll(binDir)
	os.Exit(code)
}

func TestE2EFeedbackAddList(t *testing.T) {
	repo := t.TempDir()
	runCmd(t, repo, "git", "init")
	runCmd(t, repo, "git", "config", "user.name", "E2E Test")
	runCmd(t, repo, "git", "config", "user.email", "e2e@example.com")
	runCmd(t, repo, "git", "remote", "add", "origin", "https://github.com/example/auto-stack.git")

	docPath := filepath.Join(repo, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docPath, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	stdout, stderr, err := runBinary(repo,
		"feedback", "add",
		"--kind", "helpful",
		"--file", "docs/guide.md",
		"--start", "1",
		"--end", "2",
		"--comment", "works",
	)
	if err != nil {
		t.Fatalf("feedback add failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	var add map[string]any
	if err := json.Unmarshal([]byte(stdout), &add); err != nil {
		t.Fatalf("decode add json: %v\nraw:\n%s", err, stdout)
	}

	stdout, stderr, err = runBinary(repo, "feedback", "list", "--kind", "helpful")
	if err != nil {
		t.Fatalf("feedback list failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	var list map[string]any
	if err := json.Unmarshal([]byte(stdout), &list); err != nil {
		t.Fatalf("decode list json: %v\nraw:\n%s", err, stdout)
	}
	events, ok := list["events"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("expected one event, got %#v", list["events"])
	}
	event := events[0].(map[string]any)
	requireFields(t, event, "git_hash", "git_tree_sha", "git_remote", "workspace_name")
	subject := event["subject"].(map[string]any)
	requireFields(t, subject, "head_blob_sha", "observed_blob_sha", "capture_source", "worktree_dirty", "content_snippet")
	if subject["content_snippet"] != "alpha\nbeta\n" {
		t.Fatalf("unexpected captured snippet: %v", subject["content_snippet"])
	}
}

func TestE2EFeedbackDirtyWorktreeAndInvalidSpan(t *testing.T) {
	repo := initE2ERepo(t)
	docPath := filepath.Join(repo, "docs", "guide.md")
	writeE2EFile(t, docPath, "one\ntwo\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	writeE2EFile(t, docPath, "changed\ncontent\n")
	stdout, stderr, err := runBinary(repo,
		"feedback", "add",
		"--kind", "harmful",
		"--file", "docs/guide.md",
		"--start", "1",
		"--comment", "dirty state",
	)
	if err != nil {
		t.Fatalf("feedback add dirty failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	var add map[string]any
	if err := json.Unmarshal([]byte(stdout), &add); err != nil {
		t.Fatalf("decode add json: %v\nraw:\n%s", err, stdout)
	}
	event := add["event"].(map[string]any)
	subject := event["subject"].(map[string]any)
	if subject["capture_source"] != "working_tree" {
		t.Fatalf("expected capture_source=working_tree, got %v", subject["capture_source"])
	}
	if subject["worktree_dirty"] != true {
		t.Fatalf("expected worktree_dirty=true, got %v", subject["worktree_dirty"])
	}

	_, stderr, err = runBinary(repo,
		"feedback", "add",
		"--kind", "helpful",
		"--file", "docs/guide.md",
		"--start", "1",
		"--end", "99",
		"--comment", "bad span",
	)
	if err == nil {
		t.Fatal("expected non-zero for invalid span")
	}
	if !strings.Contains(stderr, "exceeds file length") {
		t.Fatalf("expected span remediation hint, got:\n%s", stderr)
	}
}

func TestE2EFeedbackMissingContext(t *testing.T) {
	repo := initE2ERepo(t)
	writeE2EFile(t, filepath.Join(repo, "README.md"), "seed\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	stdout, stderr, err := runBinary(repo, "feedback", "add", "--kind", "missing", "--comment", "need more docs")
	if err != nil {
		t.Fatalf("feedback add missing failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var add map[string]any
	if err := json.Unmarshal([]byte(stdout), &add); err != nil {
		t.Fatalf("decode add json: %v\nraw:\n%s", err, stdout)
	}
	event := add["event"].(map[string]any)
	subject := event["subject"].(map[string]any)
	if subject["type"] != "missing_context" {
		t.Fatalf("expected missing_context subject, got %v", subject["type"])
	}
}
