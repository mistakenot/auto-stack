package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupGitHooks_FreshInstall(t *testing.T) {
	gitDir := t.TempDir()

	if err := SetupGitHooks(gitDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hookPath := filepath.Join(gitDir, "hooks", "prepare-commit-msg")
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("hook file not found: %v", err)
	}

	if info.Mode().Perm() != 0755 {
		t.Errorf("expected 0755 permissions, got %o", info.Mode().Perm())
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook: %v", err)
	}
	if string(content) != string(hookScript) {
		t.Error("hook content does not match embedded script")
	}
}

func TestSetupGitHooks_ExistingHook(t *testing.T) {
	gitDir := t.TempDir()
	hooksDir := filepath.Join(gitDir, "hooks")
	os.MkdirAll(hooksDir, 0755)
	os.WriteFile(filepath.Join(hooksDir, "prepare-commit-msg"), []byte("#!/bin/bash\necho existing"), 0755)

	err := SetupGitHooks(gitDir)
	if err == nil {
		t.Fatal("expected error for existing hook")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "not modified") {
		t.Errorf("error should mention 'not modified', got: %s", err.Error())
	}

	content, _ := os.ReadFile(filepath.Join(hooksDir, "prepare-commit-msg"))
	if string(content) != "#!/bin/bash\necho existing" {
		t.Error("existing hook was modified")
	}
}

func TestSetupGitHooks_NoHooksSubdir(t *testing.T) {
	gitDir := t.TempDir()

	if err := SetupGitHooks(gitDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hookPath := filepath.Join(gitDir, "hooks", "prepare-commit-msg")
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("hook file not found after creation: %v", err)
	}
}
