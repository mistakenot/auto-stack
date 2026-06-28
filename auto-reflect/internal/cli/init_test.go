package cli_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitWritesStateGitignore covers AC-8/AC-9 (F9): init writes a
// .auto/reflect/.gitignore that ignores the disposable playbook.json while
// leaving the canonical events/ log tracked.
func TestInitWritesStateGitignore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	if _, stderr, code := runCLIAt(t, repo, "init", "--project"); code != 0 {
		t.Fatalf("init --project failed: code=%d\nstderr:\n%s", code, stderr)
	}

	gitignorePath := filepath.Join(repo, ".auto", "reflect", ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("expected .gitignore at %s: %v", gitignorePath, err)
	}
	got := string(data)
	if got != "playbook.json\n" {
		t.Fatalf("unexpected .gitignore content: %q", got)
	}
	if strings.Contains(got, "events") {
		t.Fatalf(".gitignore must not ignore the canonical events/ log: %q", got)
	}

	// git agrees: the cache is ignored, the canonical log is not.
	if !gitIgnored(t, repo, ".auto/reflect/playbook.json") {
		t.Fatalf("playbook.json should be git-ignored")
	}
	if gitIgnored(t, repo, ".auto/reflect/events/host-2026-01-01-deadbeef.jsonl") {
		t.Fatalf("events/ shards must NOT be git-ignored")
	}
}

// TestInitGitignoreIdempotent covers AC-8: a second init neither overwrites nor
// duplicates the .gitignore.
func TestInitGitignoreIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	if _, stderr, code := runCLIAt(t, repo, "init", "--project"); code != 0 {
		t.Fatalf("first init failed: code=%d\nstderr:\n%s", code, stderr)
	}

	gitignorePath := filepath.Join(repo, ".auto", "reflect", ".gitignore")
	// Mutate the file so we can prove the second init does not clobber it.
	sentinel := "playbook.json\n# user-added line\n"
	if err := os.WriteFile(gitignorePath, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("seed sentinel .gitignore: %v", err)
	}

	if _, stderr, code := runCLIAt(t, repo, "init", "--project"); code != 0 {
		t.Fatalf("second init failed: code=%d\nstderr:\n%s", code, stderr)
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore after second init: %v", err)
	}
	if string(data) != sentinel {
		t.Fatalf("second init overwrote the existing .gitignore: got %q want %q", string(data), sentinel)
	}
}

// gitIgnored reports whether git would ignore the given repo-relative path.
// `git check-ignore <path>` exits 0 when ignored, 1 when not.
func gitIgnored(t *testing.T, repo, relPath string) bool {
	t.Helper()
	cmd := exec.Command("git", "check-ignore", "-q", relPath)
	cmd.Dir = repo
	err := cmd.Run()
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false
	}
	t.Fatalf("git check-ignore %s: %v", relPath, err)
	return false
}
