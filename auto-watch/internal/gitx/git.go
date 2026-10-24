package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func FindRepoRoot(cwd string) (string, error) {
	return runGit(cwd, "rev-parse", "--show-toplevel")
}

func OriginRemote(repoRoot string) (string, error) {
	out, err := runGitAllowFailure(repoRoot, "remote", "get-url", "origin")
	if err != nil {
		return "", nil //nolint:nilerr // intentionally swallow "no origin" error
	}
	return strings.TrimSpace(out), nil
}

func DefaultBranch(repoRoot string) (string, error) {
	if ref, err := runGitAllowFailure(repoRoot, "symbolic-ref", "refs/remotes/origin/HEAD"); err == nil {
		ref = strings.TrimSpace(ref)
		if idx := strings.LastIndex(ref, "/"); idx >= 0 && idx+1 < len(ref) {
			return ref[idx+1:], nil
		}
	}
	for _, branch := range []string{"main", "master"} {
		if _, err := runGitAllowFailure(repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			return branch, nil
		}
	}
	current, err := CurrentBranch(repoRoot)
	if err != nil {
		return "", err
	}
	if current == "" {
		return "", errors.New("could not determine default branch")
	}
	return current, nil
}

func CurrentBranch(repoRoot string) (string, error) {
	return runGit(repoRoot, "branch", "--show-current")
}

func BranchHeadSHA(repoRoot, branch string) (string, error) {
	return runGit(repoRoot, "rev-parse", branch)
}

// IsDirty reports whether the working tree at worktree has uncommitted changes
// (staged, unstaged or untracked), via `git status --porcelain`. A git error is
// returned to the caller, which fails closed (keeps the worktree) on error. Used
// by the Clean dirty-worktree guard (phase 4).
func IsDirty(worktree string) (bool, error) {
	out, err := runGit(worktree, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func AddWorktree(repoRoot, worktreePath, branch string) error {
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return fmt.Errorf("create worktree parent: %w", err)
	}
	_, err := runGitWithOutput(repoRoot, "worktree", "add", "--detach", worktreePath, branch)
	return err
}

func RemoveWorktree(repoRoot, worktreePath string) error {
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return nil
	}
	_, err := runGitWithOutput(repoRoot, "worktree", "remove", "--force", worktreePath)
	return err
}

func runGit(repoRoot string, args ...string) (string, error) {
	out, err := runGitWithOutput(repoRoot, args...)
	if err != nil {
		// Return the real git error (stderr included) rather than swallowing it:
		// callers (FindRepoRoot, CurrentBranch, BranchHeadSHA, IsDirty) depend on
		// a failure being visible so they can surface a remediation or contain it.
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func runGitAllowFailure(repoRoot string, args ...string) (string, error) {
	out, err := runGitWithOutput(repoRoot, args...)
	return strings.TrimSpace(out), err
}

func runGitWithOutput(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}
