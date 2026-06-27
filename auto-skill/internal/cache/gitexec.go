package cache

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// runGit executes a git command in the given directory with the given
// environment variables. It always sets GIT_TERMINAL_PROMPT=0 to prevent
// interactive credential prompts.
func runGit(dir string, extraEnv []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Env = append(cmd.Env, extraEnv...)
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

// runGitOffline executes a git command with GIT_NO_LAZY_FETCH=1 to prevent
// any network fetches.
func runGitOffline(dir string, args ...string) (string, error) {
	return runGit(dir, []string{"GIT_NO_LAZY_FETCH=1"}, args...)
}
