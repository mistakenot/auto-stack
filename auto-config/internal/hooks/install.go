package hooks

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed prepare-commit-msg
var hookScript []byte

func SetupGitHooks(gitDir string) error {
	hookPath := filepath.Join(gitDir, "hooks", "prepare-commit-msg")

	if _, err := os.Stat(hookPath); err == nil {
		return fmt.Errorf("prepare-commit-msg hook already exists at %s — not modified (remove it manually to reinstall)", hookPath)
	}

	if err := os.MkdirAll(filepath.Join(gitDir, "hooks"), 0755); err != nil {
		return fmt.Errorf("create hooks directory: %w", err)
	}

	if err := os.WriteFile(hookPath, hookScript, 0755); err != nil {
		return fmt.Errorf("write hook: %w", err)
	}

	return nil
}
