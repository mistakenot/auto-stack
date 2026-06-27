package cache

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/transport"
)

func createExtractFixture(t *testing.T, files map[string]string) (repoPath string, headSHA string) {
	t.Helper()
	requireGit(t)

	dir := t.TempDir()
	repoPath = filepath.Join(dir, "fixture.git")

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	run("init")

	for path, content := range files {
		fullPath := filepath.Join(repoPath, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run("add", ".")
	run("commit", "-m", "initial")

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	return repoPath, strings.TrimSpace(string(out))
}

func TestExtractCleanTree(t *testing.T) {
	requireGit(t)
	fixtureRepo, headSHA := createExtractFixture(t, map[string]string{
		"skills/hello/SKILL.md":  "# Hello Skill\n",
		"skills/hello/prompt.md": "You are a helper.\n",
	})

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "extract"}}
	repo, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := repo.Realize(headSHA); err != nil {
		t.Fatalf("Realize: %v", err)
	}

	dest := t.TempDir()
	if err := repo.Extract(headSHA, "skills/hello", dest); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if string(content) != "# Hello Skill\n" {
		t.Errorf("SKILL.md content = %q, want %q", string(content), "# Hello Skill\n")
	}

	content2, err := os.ReadFile(filepath.Join(dest, "prompt.md"))
	if err != nil {
		t.Fatalf("read prompt.md: %v", err)
	}
	if string(content2) != "You are a helper.\n" {
		t.Errorf("prompt.md content = %q", string(content2))
	}
}

func TestExtractFullTreeNoSubpath(t *testing.T) {
	requireGit(t)
	fixtureRepo, headSHA := createExtractFixture(t, map[string]string{
		"README.md":    "# Root\n",
		"sub/file.txt": "nested\n",
	})

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "full"}}
	repo, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := repo.Realize(headSHA); err != nil {
		t.Fatalf("Realize: %v", err)
	}

	dest := t.TempDir()
	if err := repo.Extract(headSHA, "", dest); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Error("expected README.md")
	}
	if _, err := os.Stat(filepath.Join(dest, "sub", "file.txt")); err != nil {
		t.Error("expected sub/file.txt")
	}
}

func TestExtractRejectsSymlink(t *testing.T) {
	requireGit(t)

	dir := t.TempDir()
	repoPath := filepath.Join(dir, "fixture.git")

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	run("init")

	if err := os.WriteFile(filepath.Join(repoPath, "real.txt"), []byte("data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(repoPath, "link.txt")); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "with symlink")

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	headSHA := strings.TrimSpace(string(out))

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "symlink"}}
	repo, err := c.Open(id, "file://"+repoPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := repo.Realize(headSHA); err != nil {
		t.Fatalf("Realize: %v", err)
	}

	dest := t.TempDir()
	err = repo.Extract(headSHA, "", dest)
	if err == nil {
		t.Fatal("expected error for symlink")
	}

	var ee *ExtractError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *ExtractError, got %T: %v", err, err)
	}
	if ee.Code != CodeSymlinkEntry {
		t.Errorf("code = %q, want %q", ee.Code, CodeSymlinkEntry)
	}

	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Error("dest should be empty after rejected extract")
	}
}

func TestExtractRejectsPreExistingSymlinkDir(t *testing.T) {
	requireGit(t)
	fixtureRepo, headSHA := createExtractFixture(t, map[string]string{
		"subdir/file.txt": "safe content\n",
	})

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "symlinkdir"}}
	repo, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := repo.Realize(headSHA); err != nil {
		t.Fatalf("Realize: %v", err)
	}

	dest := t.TempDir()
	outside := t.TempDir()
	// Plant a symlink at dest/subdir → outside directory, before extract runs.
	if err := os.Symlink(outside, filepath.Join(dest, "subdir")); err != nil {
		t.Fatalf("plant symlink: %v", err)
	}

	err = repo.Extract(headSHA, "", dest)
	if err == nil {
		t.Fatal("expected error for pre-existing symlink directory")
	}
	var ee *ExtractError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *ExtractError, got %T: %v", err, err)
	}
	if ee.Code != CodeSymlinkEntry {
		t.Errorf("code = %q, want %q", ee.Code, CodeSymlinkEntry)
	}

	// Verify nothing was written to the outside directory.
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Error("symlink escape: files were written outside dest")
	}
}

func TestExtractTooManyFiles(t *testing.T) {
	requireGit(t)

	// Use a temporarily low limit to avoid creating 2001 files in a git repo.
	origLimit := maxExtractFilesForTest
	maxExtractFilesForTest = 5
	defer func() { maxExtractFilesForTest = origLimit }()

	files := make(map[string]string)
	for i := range 6 {
		files[fmt.Sprintf("many/file_%04d.txt", i)] = "x"
	}

	fixtureRepo, headSHA := createExtractFixture(t, files)

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "toomany"}}
	repo, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := repo.Realize(headSHA); err != nil {
		t.Fatalf("Realize: %v", err)
	}

	dest := t.TempDir()
	err = repo.Extract(headSHA, "", dest)
	if err == nil {
		t.Fatal("expected error for too many files")
	}

	var ee *ExtractError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *ExtractError, got %T: %v", err, err)
	}
	if ee.Code != CodeTooManyFiles {
		t.Errorf("code = %q, want %q", ee.Code, CodeTooManyFiles)
	}

	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Error("dest should be empty after rejected extract")
	}
}

func TestExtractFileSizeLimit(t *testing.T) {
	requireGit(t)

	// Use a temporarily low limit to avoid creating an 8+ MiB file.
	origLimit := maxExtractFileForTest
	maxExtractFileForTest = 1024
	defer func() { maxExtractFileForTest = origLimit }()

	bigContent := strings.Repeat("x", 1025)
	fixtureRepo, headSHA := createExtractFixture(t, map[string]string{
		"big.bin": bigContent,
	})

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "bigfile"}}
	repo, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := repo.Realize(headSHA); err != nil {
		t.Fatalf("Realize: %v", err)
	}

	dest := t.TempDir()
	err = repo.Extract(headSHA, "", dest)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}

	var ee *ExtractError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *ExtractError, got %T: %v", err, err)
	}
	if ee.Code != CodeSizeLimitFile {
		t.Errorf("code = %q, want %q", ee.Code, CodeSizeLimitFile)
	}

	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Error("dest should be empty after rejected extract")
	}
}
