package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureSnippet_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")

	updated, err := ensureSnippet(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updated {
		t.Fatal("expected updated=true for new file")
	}

	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), autosearchSnippet) {
		t.Fatal("snippet not found in created file")
	}
}

func TestEnsureSnippet_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	os.WriteFile(p, []byte("# My Project\n"), 0o644)

	updated, err := ensureSnippet(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updated {
		t.Fatal("expected updated=true")
	}

	data, _ := os.ReadFile(p)
	content := string(data)
	if !strings.Contains(content, autosearchSnippet) {
		t.Fatal("snippet not found")
	}
	if !strings.HasPrefix(content, "# My Project\n") {
		t.Fatal("original content was lost")
	}
}

func TestEnsureSnippet_Idempotent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	os.WriteFile(p, []byte("# My Project\n\n"+autosearchSnippet+"\n"), 0o644)

	updated, err := ensureSnippet(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated {
		t.Fatal("expected updated=false when snippet already present")
	}
}
