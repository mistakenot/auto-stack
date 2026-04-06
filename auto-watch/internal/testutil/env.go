package testutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistakenot/auto-watch/internal/app"
	"github.com/mistakenot/auto-watch/internal/cli"
)

type Env struct {
	t             *testing.T
	Home          string
	BinDir        string
	SessionPrefix string
}

func NewEnv(t *testing.T) *Env {
	t.Helper()
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &Env{
		t:             t,
		Home:          home,
		BinDir:        binDir,
		SessionPrefix: fmt.Sprintf("autowatch-test-%d", time.Now().UnixNano()),
	}
}

func (e *Env) NewRepo(name string) string {
	e.t.Helper()
	repoRoot := filepath.Join(e.t.TempDir(), name)
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		e.t.Fatalf("create repo root: %v", err)
	}
	e.WriteFile(repoRoot, "README.md", "# test\n")
	e.runGit(repoRoot, "init", "-b", "main")
	e.runGit(repoRoot, "config", "user.email", "autowatch-tests@example.com")
	e.runGit(repoRoot, "config", "user.name", "Autowatch Tests")
	e.runGit(repoRoot, "add", ".")
	e.runGit(repoRoot, "commit", "-m", "initial")
	return repoRoot
}

func (e *Env) AddRemote(repoRoot, remote string) {
	e.t.Helper()
	e.runGit(repoRoot, "remote", "add", "origin", remote)
}

func (e *Env) CommitFile(repoRoot, relPath, content, message string) {
	e.t.Helper()
	e.WriteFile(repoRoot, relPath, content)
	e.runGit(repoRoot, "add", relPath)
	e.runGit(repoRoot, "commit", "-m", message)
}

func (e *Env) WriteFile(root, relPath, content string) string {
	e.t.Helper()
	path := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatalf("mkdir for %s: %v", relPath, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		e.t.Fatalf("write %s: %v", relPath, err)
	}
	return path
}

func (e *Env) WriteExecutable(name, script string) string {
	e.t.Helper()
	path := filepath.Join(e.BinDir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		e.t.Fatalf("write executable %s: %v", name, err)
	}
	return path
}

func (e *Env) RunCLI(cwd string, args ...string) (stdout string, stderr string, code int) {
	e.t.Helper()
	var out bytes.Buffer
	var errOut bytes.Buffer
	application := app.New(&out, &errOut)
	application.CWD = cwd
	rootCmd := cli.NewRootCmd(application)
	rootCmd.SetArgs(args)
	err := rootCmd.ExecuteContext(context.Background())
	if err != nil {
		code = 1
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
			if exitErr.Err != nil && exitErr.Err.Error() != "" {
				fmt.Fprintln(&errOut, exitErr.Err.Error())
			}
		} else {
			fmt.Fprintln(&errOut, err.Error())
		}
	}
	return out.String(), errOut.String(), code
}

func (e *Env) runGit(repoRoot string, args ...string) {
	e.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		e.t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
