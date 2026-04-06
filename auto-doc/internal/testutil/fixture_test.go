package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceSetupAndTeardown(t *testing.T) {
	var workspaceDir string

	t.Run("workspace lifecycle", func(t *testing.T) {
		ws := NewWorkspace(t)
		workspaceDir = ws.Dir

		// Workspace dir exists
		info, err := os.Stat(ws.Dir)
		if err != nil {
			t.Fatalf("workspace dir does not exist: %v", err)
		}
		if !info.IsDir() {
			t.Fatal("workspace path is not a directory")
		}

		// WriteDoc creates the docs dir and file with frontmatter
		docPath := ws.WriteDoc("guides/setup.md", "Setup Guide", "How to set up the project", "# Setup\n\nDo the thing.")
		data, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatalf("failed to read doc: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, `title: "Setup Guide"`) {
			t.Error("doc missing title in frontmatter")
		}
		if !strings.Contains(content, `summary: "How to set up the project"`) {
			t.Error("doc missing summary in frontmatter")
		}
		if !strings.Contains(content, `hash: ""`) {
			t.Error("doc missing hash in frontmatter")
		}
		if !strings.Contains(content, "# Setup") {
			t.Error("doc missing body content")
		}

		// WriteConfig creates settings.json
		cfgPath := ws.WriteConfig("docs")
		data, err = os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("failed to read config: %v", err)
		}
		if !strings.Contains(string(data), `"docsDir"`) {
			t.Error("config missing docsDir key")
		}
		// Verify config filename is settings.json
		if filepath.Base(cfgPath) != "settings.json" {
			t.Errorf("config filename = %q, want settings.json", filepath.Base(cfgPath))
		}

		// Path helper resolves correctly
		expected := filepath.Join(ws.Dir, "some", "path.txt")
		if got := ws.Path("some/path.txt"); got != expected {
			t.Errorf("Path() = %s, want %s", got, expected)
		}

		ws.WriteDocWithId("id.md", "deadbeef", "Doc With ID", "Summary", "12345678", "# Doc")
		withIDData, err := os.ReadFile(ws.Path("docs/id.md"))
		if err != nil {
			t.Fatalf("failed to read id doc: %v", err)
		}
		if !strings.Contains(string(withIDData), `id: "deadbeef"`) {
			t.Fatal("WriteDocWithId missing id field")
		}

		srcPath := ws.WriteSourceFile("pkg/app/main.go", "package app\n")
		if _, err := os.Stat(srcPath); err != nil {
			t.Fatalf("WriteSourceFile did not create file: %v", err)
		}

		ws.InitGitRepo()
		headCmd := exec.Command("git", "rev-parse", "HEAD")
		headCmd.Dir = ws.Dir
		if out, err := headCmd.CombinedOutput(); err != nil {
			t.Fatalf("expected git repo with initial commit: %v\n%s", err, out)
		}
	})

	// After the subtest completes, t.Cleanup should have removed the dir
	if _, err := os.Stat(workspaceDir); !os.IsNotExist(err) {
		t.Errorf("workspace dir %s was not cleaned up", workspaceDir)
	}
}
