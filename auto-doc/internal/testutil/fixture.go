// [autodoc(e8d3cf9c@34e92e15, 26543ed4)]
package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Workspace is a temporary directory that simulates a repository with docs.
// Each test gets its own isolated workspace that is cleaned up automatically.
type Workspace struct {
	Dir string // root of the temp workspace
	t   *testing.T
}

// NewWorkspace creates a temp directory and returns a Workspace.
// Cleanup is registered via t.Cleanup so callers don't need to tear down manually.
func NewWorkspace(t *testing.T) *Workspace {
	t.Helper()
	dir, err := os.MkdirTemp("", "autodoc-test-*")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return &Workspace{Dir: dir, t: t}
}

// WriteDoc creates a markdown doc file under the workspace's docs directory
// with standard frontmatter. The relPath is relative to the docs dir
// (e.g. "getting-started.md" becomes <workspace>/docs/getting-started.md).
func (w *Workspace) WriteDoc(relPath, title, summary, body string) string {
	w.t.Helper()
	full := filepath.Join(w.Dir, "docs", relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		w.t.Fatalf("mkdir for doc %s: %v", relPath, err)
	}
	content := fmt.Sprintf("---\ntitle: %q\nsummary: %q\nhash: \"\"\n---\n\n%s\n", title, summary, body)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		w.t.Fatalf("write doc %s: %v", relPath, err)
	}
	return full
}

// WriteDocWithHash creates a markdown doc file with a specific hash value.
func (w *Workspace) WriteDocWithHash(relPath, title, summary, hash, body string) string {
	w.t.Helper()
	full := filepath.Join(w.Dir, "docs", relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		w.t.Fatalf("mkdir for doc %s: %v", relPath, err)
	}
	content := fmt.Sprintf("---\ntitle: %q\nsummary: %q\nhash: %q\n---\n\n%s\n", title, summary, hash, body)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		w.t.Fatalf("write doc %s: %v", relPath, err)
	}
	return full
}

// WriteDocWithId creates a markdown doc file with a specific id and hash values.
func (w *Workspace) WriteDocWithId(relPath, id, title, summary, hash, body string) string {
	w.t.Helper()
	full := filepath.Join(w.Dir, "docs", relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		w.t.Fatalf("mkdir for doc %s: %v", relPath, err)
	}
	content := fmt.Sprintf("---\nid: %q\ntitle: %q\nsummary: %q\nhash: %q\n---\n\n%s\n", id, title, summary, hash, body)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		w.t.Fatalf("write doc %s: %v", relPath, err)
	}
	return full
}

// WriteDocWithReadWhen creates a markdown doc file with a read_when field.
func (w *Workspace) WriteDocWithReadWhen(relPath, title, summary, readWhen, body string) string {
	w.t.Helper()
	full := filepath.Join(w.Dir, "docs", relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		w.t.Fatalf("mkdir for doc %s: %v", relPath, err)
	}
	content := fmt.Sprintf("---\ntitle: %q\nsummary: %q\nread_when: %q\nhash: \"\"\n---\n\n%s\n", title, summary, readWhen, body)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		w.t.Fatalf("write doc %s: %v", relPath, err)
	}
	return full
}

// WriteConfig writes a settings.json config file at .auto/doc/settings.json in the workspace.
func (w *Workspace) WriteConfig(docsDir string) string {
	w.t.Helper()
	configDir := filepath.Join(w.Dir, ".auto", "doc")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		w.t.Fatalf("mkdir .auto/doc: %v", err)
	}
	cfgPath := filepath.Join(configDir, "settings.json")
	content := fmt.Sprintf(`{"docsDir": %q}`, docsDir)
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		w.t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

// WriteFile writes an arbitrary file relative to the workspace root.
func (w *Workspace) WriteFile(relPath, content string) string {
	w.t.Helper()
	full := filepath.Join(w.Dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		w.t.Fatalf("mkdir for %s: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		w.t.Fatalf("write %s: %v", relPath, err)
	}
	return full
}

// WriteSourceFile writes a source file relative to the workspace root.
func (w *Workspace) WriteSourceFile(relPath, content string) string {
	w.t.Helper()
	return w.WriteFile(relPath, content)
}

// InitGitRepo initializes a git repo in the workspace and creates an initial commit.
func (w *Workspace) InitGitRepo() {
	w.t.Helper()
	w.runGit("init")
	w.runGit("config", "user.email", "autodoc-tests@example.com")
	w.runGit("config", "user.name", "Autodoc Tests")
	w.runGit("add", ".")
	w.runGit("commit", "--allow-empty", "-m", "initial")
}

// GitAddAll stages all current files in the workspace so they are visible to git ls-files.
func (w *Workspace) GitAddAll() {
	w.t.Helper()
	w.runGit("add", ".")
}

// Path returns the absolute path for a path relative to the workspace root.
func (w *Workspace) Path(relPath string) string {
	return filepath.Join(w.Dir, relPath)
}

func (w *Workspace) runGit(args ...string) {
	w.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = w.Dir
	if out, err := cmd.CombinedOutput(); err != nil {
		w.t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
