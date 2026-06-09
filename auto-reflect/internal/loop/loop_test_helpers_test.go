package loop

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initLoopRepo creates a temp git repo with a seed commit and points HOME at a
// temp dir so EnsureHost writes there. It returns the repo root.
func initLoopRepo(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := t.TempDir()
	gitCmd(t, repo, "init")
	gitCmd(t, repo, "config", "user.name", "Loop Test")
	gitCmd(t, repo, "config", "user.email", "loop@example.com")
	gitCmd(t, repo, "remote", "add", "origin", "git@github.com:example/auto-stack.git")
	writeRepoFile(t, repo, "README.md", "seed\n")
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-m", "seed")
	return repo
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v failed: %v\nstderr:\n%s", args, err, stderr.String())
	}
}

func writeRepoFile(t *testing.T, repo, name, content string) {
	t.Helper()
	path := filepath.Join(repo, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
