package gitx_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-watch/internal/gitx"
)

// initRepo creates a committed git repo in a temp dir and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

// TestFindRepoRootErrorsOutsideRepo (AC-9): runGit no longer swallows failures,
// so FindRepoRoot returns a real error outside a repo (the live error branch
// that lets `auto watch task list` print its remediation).
func TestFindRepoRootErrorsOutsideRepo(t *testing.T) {
	if _, err := gitx.FindRepoRoot(t.TempDir()); err == nil {
		t.Fatal("expected an error for a non-repo dir; runGit must not swallow it")
	}
}

func TestFindRepoRootInsideRepo(t *testing.T) {
	dir := initRepo(t)
	root, err := gitx.FindRepoRoot(dir)
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	wantReal, _ := filepath.EvalSymlinks(dir)
	gotReal, _ := filepath.EvalSymlinks(root)
	if gotReal != wantReal {
		t.Fatalf("root = %q, want %q", gotReal, wantReal)
	}
}

// TestDefaultBranchPropagatesErrorOutsideRepo (AC-9): DefaultBranch propagates
// the CurrentBranch error instead of swallowing it to a nil error.
func TestDefaultBranchPropagatesErrorOutsideRepo(t *testing.T) {
	if _, err := gitx.DefaultBranch(t.TempDir()); err == nil {
		t.Fatal("expected DefaultBranch to propagate the git error outside a repo")
	}
}

func TestDefaultBranchInsideRepo(t *testing.T) {
	branch, err := gitx.DefaultBranch(initRepo(t))
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("branch = %q, want main", branch)
	}
}

// TestBranchHeadSHAErrorsOnMissingBranch (AC-9): BranchHeadSHA surfaces the git
// error (branch does not exist) that the daemon turns into a system_warning.
func TestBranchHeadSHAErrorsOnMissingBranch(t *testing.T) {
	if _, err := gitx.BranchHeadSHA(initRepo(t), "no-such-branch"); err == nil {
		t.Fatal("expected a git error for a missing branch")
	}
}

// TestIsDirty exercises the phase-4 dirty-worktree guard helper.
func TestIsDirty(t *testing.T) {
	dir := initRepo(t)
	dirty, err := gitx.IsDirty(dir)
	if err != nil {
		t.Fatalf("IsDirty on a clean repo: %v", err)
	}
	if dirty {
		t.Fatal("a freshly committed repo must be clean")
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, err = gitx.IsDirty(dir)
	if err != nil {
		t.Fatalf("IsDirty on a dirty repo: %v", err)
	}
	if !dirty {
		t.Fatal("a repo with an untracked file must be dirty")
	}
}

// TestIsDirtyErrorsOutsideRepo: IsDirty returns the git error outside a repo, so
// the Clean guard fails closed (keeps the worktree) on error.
func TestIsDirtyErrorsOutsideRepo(t *testing.T) {
	if _, err := gitx.IsDirty(t.TempDir()); err == nil {
		t.Fatal("IsDirty must return the git error outside a repo")
	}
}

// TestOriginRemoteStillSwallowsMissingOrigin: OriginRemote keeps its deliberate
// swallow (via runGitAllowFailure) — a repo with no origin returns ("", nil).
func TestOriginRemoteStillSwallowsMissingOrigin(t *testing.T) {
	out, err := gitx.OriginRemote(initRepo(t))
	if err != nil {
		t.Fatalf("OriginRemote must swallow a missing origin, got: %v", err)
	}
	if out != "" {
		t.Fatalf("expected empty origin, got %q", out)
	}
}
