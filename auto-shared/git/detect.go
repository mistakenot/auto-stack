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
