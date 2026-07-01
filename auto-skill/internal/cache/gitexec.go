package cache

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type execCmd = exec.Cmd

func newGitCmd(dir string, extraEnv []string, args ...string) *execCmd {
	args = append([]string{"-c", "maintenance.auto=false"}, args...)
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd
}

func runGit(dir string, extraEnv []string, args ...string) (string, error) {
	cmd := newGitCmd(dir, extraEnv, args...)
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

func runGitOffline(dir string, args ...string) (string, error) {
	return runGit(dir, []string{"GIT_NO_LAZY_FETCH=1"}, args...)
}
