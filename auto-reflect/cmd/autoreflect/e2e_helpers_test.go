package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runBinary(cwd string, args ...string) (stdout string, stderr string, err error) {
	cmd := exec.Command(testBinaryPath, args...)
	cmd.Dir = cwd
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

func runCmd(t *testing.T, cwd string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = cwd
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s failed: %v\nstderr:\n%s", name, strings.Join(args, " "), err, stderr.String())
	}
}

func initE2ERepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runCmd(t, repo, "git", "init")
	runCmd(t, repo, "git", "config", "user.name", "E2E Test")
	runCmd(t, repo, "git", "config", "user.email", "e2e@example.com")
	runCmd(t, repo, "git", "remote", "add", "origin", "https://github.com/example/auto-stack.git")
	return repo
}

func writeE2EFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func requireFields(t *testing.T, row map[string]any, fields ...string) {
	t.Helper()
	for _, field := range fields {
		value, ok := row[field]
		if !ok {
			t.Fatalf("missing field %q in %#v", field, row)
		}
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) == "" {
				t.Fatalf("field %q should not be empty", field)
			}
		}
	}
}
