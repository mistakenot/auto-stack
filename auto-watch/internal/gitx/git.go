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
		return "", nil //nolint:nilerr // intentionally swallow "no origin" error
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
		return "", nil //nolint:nilerr // intentionally swallow "no origin" error
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
