package cache

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestExtractMissingSubpathReturnsSentinel(t *testing.T) {
	requireGit(t)
	fixtureRepo, headSHA := createExtractFixture(t, map[string]string{
		"skills/hello/SKILL.md": "# Hello Skill\n",
	})

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)

	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "missing"}}
	repo, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := repo.Realize(headSHA); err != nil {
		t.Fatalf("Realize: %v", err)
	}

	dest := t.TempDir()
	err = repo.Extract(headSHA, "skills/renamed", dest)
	if err == nil {
		t.Fatal("expected error for missing subpath")
	}
	if !errors.Is(err, ErrSubpathNotFound) {
		t.Fatalf("error = %v, want errors.Is ErrSubpathNotFound", err)
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

// createDeadlockFixture builds a repo whose archive contains a symlink that
// sorts BEFORE a blob larger than the 64KB OS pipe buffer. git emits the
// symlink first (validation rejects it) while the big blob is still unwritten,
// reproducing the pipe-buffer deadlock if Extract calls Wait without draining
// git's stdout.
func createDeadlockFixture(t *testing.T, bigName string, bigSize int) (repoPath, headSHA string) {
	t.Helper()
	requireGit(t)

	dir := t.TempDir()
	repoPath = filepath.Join(dir, "fixture.git")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init")
	// A symlink whose name sorts before the big blob.
	if err := os.Symlink("target-does-not-matter", filepath.Join(repoPath, "aaa-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, bigName), []byte(strings.Repeat("x", bigSize)), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "symlink + big blob")

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return repoPath, strings.TrimSpace(string(out))
}

// TestExtractDrainsStdoutOnRejection is a regression test for a pipe-buffer
// deadlock: when validation rejects an entry mid-stream, Extract must still
// drain git's remaining stdout before Wait, or git blocks writing into the full
// 64KB pipe and Extract hangs forever. The big blob (256KB) guarantees there is
// far more than 64KB of undrained output after the rejected symlink entry.
func TestExtractDrainsStdoutOnRejection(t *testing.T) {
	requireGit(t)
	fixtureRepo, headSHA := createDeadlockFixture(t, "zzz-big.bin", 256*1024)

	cacheDir := t.TempDir()
	c := NewCache(cacheDir)
	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", "deadlock"}}
	repo, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := repo.Realize(headSHA); err != nil {
		t.Fatalf("Realize: %v", err)
	}

	dest := t.TempDir()
	done := make(chan error, 1)
	go func() { done <- repo.Extract(headSHA, "", dest) }()

	select {
	case err := <-done:
		// The fix returns the validation rejection promptly instead of hanging.
		var ee *ExtractError
		if !errors.As(err, &ee) || ee.Code != CodeSymlinkEntry {
			t.Fatalf("Extract error = %v, want a %s ExtractError", err, CodeSymlinkEntry)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Extract deadlocked: did not return within 15s for a rejected entry followed by a >64KB undrained archive")
	}
}

// createScopeFixture builds a repo that mixes real skill dirs with a symlinked
// skill dir (as monorepos like mistakenot/skills do) and a non-skill file, to
// exercise ListSkillDirs/ExtractPaths scoping.
func createScopeFixture(t *testing.T) (repoPath, headSHA string) {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	repoPath = filepath.Join(dir, "fixture.git")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		full := filepath.Join(repoPath, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init")
	write("skills/alpha/SKILL.md", "# Alpha\n")
	write("pkg/beta/SKILL.md", "# Beta\n")
	write("notes.txt", "not a skill\n")
	// A symlinked skill dir, mirroring how these repos expose skills under
	// .claude/skills — the safe extractor rejects symlinks, so a whole-repo
	// archive would fail here.
	if err := os.MkdirAll(filepath.Join(repoPath, ".claude/skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../skills/alpha", filepath.Join(repoPath, ".claude/skills/alpha")); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "skills + symlink + non-skill")
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return repoPath, strings.TrimSpace(string(out))
}

func openScopeRepo(t *testing.T, fixtureRepo, headSHA, idLeaf string) *Repo {
	t.Helper()
	c := NewCache(t.TempDir())
	id := transport.CacheIdentity{Host: "example.com", Path: []string{"test", idLeaf}}
	repo, err := c.Open(id, "file://"+fixtureRepo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := repo.Realize(headSHA); err != nil {
		t.Fatalf("Realize: %v", err)
	}
	return repo
}

func TestListSkillDirsSkipsSymlinkedTrees(t *testing.T) {
	fixtureRepo, headSHA := createScopeFixture(t)
	repo := openScopeRepo(t, fixtureRepo, headSHA, "lsdirs")

	dirs, err := repo.ListSkillDirs(headSHA)
	if err != nil {
		t.Fatalf("ListSkillDirs: %v", err)
	}
	got := map[string]bool{}
	for _, d := range dirs {
		got[d] = true
	}
	for _, want := range []string{"skills/alpha", "pkg/beta"} {
		if !got[want] {
			t.Errorf("ListSkillDirs missing %q; got %v", want, dirs)
		}
	}
	// The symlinked .claude/skills/alpha is not a real tree, so its SKILL.md is
	// never listed and the directory must not appear.
	if got[".claude/skills/alpha"] || got[".claude/skills"] {
		t.Errorf("ListSkillDirs leaked a symlinked tree: %v", dirs)
	}
	if got[""] {
		t.Errorf("ListSkillDirs reported the repo root: %v", dirs)
	}
}

func TestExtractPathsScopesAndAvoidsSymlinks(t *testing.T) {
	fixtureRepo, headSHA := createScopeFixture(t)
	repo := openScopeRepo(t, fixtureRepo, headSHA, "extractpaths")

	// Sanity: a whole-repo extract fails on the unrelated symlink — this is what
	// scoping avoids.
	if err := repo.Extract(headSHA, "", t.TempDir()); err == nil {
		t.Fatal("whole-repo Extract unexpectedly succeeded despite a symlink in the tree")
	}

	dest := t.TempDir()
	if err := repo.ExtractPaths(headSHA, []string{"skills/alpha", "pkg/beta"}, dest); err != nil {
		t.Fatalf("ExtractPaths: %v", err)
	}
	// Selected skill trees are present, with full repo-relative paths preserved.
	for _, rel := range []string{"skills/alpha/SKILL.md", "pkg/beta/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Errorf("expected %q extracted: %v", rel, err)
		}
	}
	// Unselected paths (the symlink and the non-skill file) are not extracted.
	if _, err := os.Stat(filepath.Join(dest, ".claude")); !os.IsNotExist(err) {
		t.Errorf(".claude should not be extracted (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "notes.txt")); !os.IsNotExist(err) {
		t.Errorf("notes.txt should not be extracted (err=%v)", err)
	}
}
