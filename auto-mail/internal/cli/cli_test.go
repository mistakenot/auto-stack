package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-mail/internal/cli"
)

// runCLI drives the command tree in-process under whatever $HOME the caller has
// set, returning stdout, stderr, and the process exit code. Nothing here opens
// a network connection or needs a daemon (D-11).
func runCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cwd, _ := os.Getwd()
	root := cli.NewRootCmd(app.New(&outBuf, &errBuf, cwd))
	root.SetArgs(args)
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)

	err := root.ExecuteContext(context.Background())
	code = 0
	if err != nil {
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
			if exitErr.Err != nil {
				errBuf.WriteString(exitErr.Err.Error())
			}
		} else {
			code = 1
			errBuf.WriteString(err.Error())
		}
	}
	return outBuf.String(), errBuf.String(), code
}

// TestInitCreatesAlphaStore: `init` creates ~/.auto/mail/alpha-store.db — the
// alpha marker is in the filename, not only in the docs (G10 / D-2).
func TestInitCreatesAlphaStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stdout, stderr, code := runCLI(t, "init")
	if code != 0 {
		t.Fatalf("init exit %d, stderr: %s", code, stderr)
	}

	var payload struct {
		Store   string `json:"store"`
		Created bool   `json:"created"`
		Alpha   bool   `json:"alpha"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("init stdout is not JSON: %v\n%s", err, stdout)
	}
	want := filepath.Join(home, ".auto", "mail", "alpha-store.db")
	if payload.Store != want {
		t.Errorf("store = %q, want %q", payload.Store, want)
	}
	if !payload.Created {
		t.Errorf("created = false on a fresh HOME, want true")
	}
	if !payload.Alpha {
		t.Errorf("alpha = false, want true")
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("store file not on disk after init: %v", err)
	}

	// Re-running init is safe and reports the store as already present.
	stdout, stderr, code = runCLI(t, "init")
	if code != 0 {
		t.Fatalf("second init exit %d, stderr: %s", code, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("second init stdout is not JSON: %v\n%s", err, stdout)
	}
	if payload.Created {
		t.Errorf("created = true on an existing store, want false")
	}
}

// TestListEmptyReturnsJSONArray: with no mail, `list` prints `[]` on stdout and
// nothing else — pure JSON, diagnostics on stderr (project CLI convention).
func TestListEmptyReturnsJSONArray(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, stderr, code := runCLI(t, "init"); code != 0 {
		t.Fatalf("init exit %d, stderr: %s", code, stderr)
	}

	stdout, stderr, code := runCLI(t, "list")
	if code != 0 {
		t.Fatalf("list exit %d, stderr: %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("list stdout = %q, want %q", stdout, "[]")
	}
	var deliveries []map[string]any
	if err := json.Unmarshal([]byte(stdout), &deliveries); err != nil {
		t.Fatalf("list stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(deliveries) != 0 {
		t.Errorf("list returned %d deliveries on a fresh store, want 0", len(deliveries))
	}
	if stderr != "" {
		t.Errorf("list wrote to stderr: %q", stderr)
	}
}

// TestListWithoutInitStillWorks: mail never requires a separate setup step —
// `list` opens (creating) the store itself, so an agent's first call succeeds.
func TestListWithoutInitStillWorks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	stdout, stderr, code := runCLI(t, "list")
	if code != 0 {
		t.Fatalf("list exit %d, stderr: %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("list stdout = %q, want %q", stdout, "[]")
	}
}

// TestDocsStatesTheAlphaContract: an agent must be able to discover the surface
// it is asked to use, and the alpha marker must be discoverable there (G10).
func TestDocsStatesTheAlphaContract(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	stdout, _, code := runCLI(t, "docs")
	if code != 0 {
		t.Fatalf("docs exit %d", code)
	}
	for _, want := range []string{"auto mail", "alpha-store.db", "## init", "## list"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("docs output missing %q", want)
		}
	}
}
