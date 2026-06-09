package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	stateDirName  = ".auto/reflect"
	playbookFile  = "playbook.json"
	eventsDirName = "events"
	dirPermission = 0o755
)

func EnsureStateDir(repoRoot string) (string, error) {
	if repoRoot == "" {
		return "", errors.New("repo root is required")
	}
	dir := filepath.Join(repoRoot, stateDirName)
	if err := os.MkdirAll(dir, dirPermission); err != nil {
		return "", fmt.Errorf("create state directory: %w", err)
	}
	return dir, nil
}

func PlaybookPath(repoRoot string) string {
	return filepath.Join(repoRoot, stateDirName, playbookFile)
}

// EventsDir returns the directory holding the append-only event shards.
func EventsDir(repoRoot string) string {
	return filepath.Join(repoRoot, stateDirName, eventsDirName)
}

func DisplayPath(cwd, absolute string) string {
	if cwd == "" || absolute == "" {
		return filepath.ToSlash(absolute)
	}
	rel, err := filepath.Rel(cwd, absolute)
	if err != nil {
		return filepath.ToSlash(absolute)
	}
	return filepath.ToSlash(rel)
}
