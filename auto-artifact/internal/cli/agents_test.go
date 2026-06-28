package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-artifact/internal/app"
	"github.com/mistakenot/auto-artifact/internal/cli"
)

// runAgentsIn drives `artifact agents` with cwd pinned to dir.
func runAgentsIn(t *testing.T, dir string) (stdout, stderr string, code int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := cli.NewRootCmd(app.New(&outBuf, &errBuf, dir))
	root.SetArgs([]string{"agents"})
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)

	err := root.ExecuteContext(context.Background())
	if err != nil {
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			return outBuf.String(), errBuf.String() + errString(exitErr.Err), exitErr.Code
		}
		return outBuf.String(), errBuf.String() + err.Error(), 1
	}
	return outBuf.String(), errBuf.String(), 0
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestAgentsInsertsAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# Project\n\nExisting.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runAgentsIn(t, dir); code != 0 {
		t.Fatalf("first run failed: code=%d stderr=%s", code, stderr)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "<!-- BEGIN auto-artifact (managed) -->") ||
		!strings.Contains(string(after), "auto artifact quickstart") ||
		!strings.Contains(string(after), "<!-- END auto-artifact (managed) -->") {
		t.Fatalf("managed block not inserted:\n%s", after)
	}
	if !strings.HasPrefix(string(after), "# Project\n\nExisting.\n") {
		t.Fatalf("existing content not preserved:\n%s", after)
	}

	// Second run must be a no-op (byte-identical).
	if _, _, code := runAgentsIn(t, dir); code != 0 {
		t.Fatalf("second run failed: code=%d", code)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, again) {
		t.Fatalf("second run was not idempotent:\nbefore:\n%s\nafter:\n%s", after, again)
	}
}

func TestAgentsFailsWhenNoTargetFiles(t *testing.T) {
	dir := t.TempDir()
	_, stderr, code := runAgentsIn(t, dir)
	if code == 0 {
		t.Fatalf("expected non-zero exit when no CLAUDE.md/AGENTS.md present")
	}
	if !strings.Contains(stderr, "auto artifact agents") {
		t.Fatalf("error should hint at the command; got: %s", stderr)
	}
}
