package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// RepoRoot returns the absolute root of the git working tree containing dir.
// It returns an error when dir is not inside a git repository, so callers can
// distinguish "not a repo" from a repo with no remote.
func RepoRoot(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(out)
	if root == "" {
		return "", fmt.Errorf("not a git repository: %s", dir)
	}
	return root, nil
}

// Provenance returns the worktree root, current branch, and HEAD commit for dir
// in a single git subprocess. It tolerates unborn HEAD and non-repo directories
// by returning empty strings (not errors), mirroring RepoRoot's error tolerance.
func Provenance(dir string) (root, branch, commit string, err error) {
	out, err := runGit(dir, "rev-parse", "--show-toplevel", "--abbrev-ref", "HEAD", "HEAD")
	if err != nil {
		// Not a repo or unborn HEAD — degrade gracefully.
		return "", "", "", nil //nolint:nilerr
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) >= 1 {
		root = strings.TrimSpace(lines[0])
	}
	if len(lines) >= 2 {
		branch = strings.TrimSpace(lines[1])
	}
	if len(lines) >= 3 {
		commit = strings.TrimSpace(lines[2])
	}
	return root, branch, commit, nil
}

// OriginRemote returns the `origin` remote URL for the repo at dir, or an empty
// string (with nil error) when no origin remote is configured.
func OriginRemote(dir string) (string, error) {
	out, err := runGit(dir, "remote", "get-url", "origin")
	if err != nil {
		return "", nil //nolint:nilerr // a missing origin is not an error here
	}
	return strings.TrimSpace(out), nil
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}
