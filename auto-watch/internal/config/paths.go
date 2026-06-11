package config

import (
	"fmt"
	"os"
	"path/filepath"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

const (
	watchDirName    = "watch"
	projectFileName = "project.json"
	lockFileName    = "daemon.lock"
	pidFileName     = "daemon.pid.json"
	dbFileName      = "logs.sqlite"
)

// AutoDir returns ~/.auto, delegating to the shared config package.
func AutoDir() (string, error) {
	return sharedconfig.AutoDir()
}

func WatchDir() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, watchDirName), nil
}

func HostPath() (string, error) {
	return sharedconfig.HostConfigPath()
}

func DBPath() (string, error) {
	dir, err := WatchDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, dbFileName), nil
}

func RunsDir() (string, error) {
	dir, err := WatchDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "runs"), nil
}

func LockPath() (string, error) {
	dir, err := WatchDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, lockFileName), nil
}

func PIDPath() (string, error) {
	dir, err := WatchDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, pidFileName), nil
}

func ProjectDir(repoRoot string) string {
	return filepath.Join(repoRoot, ".auto", watchDirName)
}

func ProjectConfigPath(repoRoot string) string {
	return filepath.Join(ProjectDir(repoRoot), projectFileName)
}

func ProjectGitIgnorePath(repoRoot string) string {
	return filepath.Join(ProjectDir(repoRoot), ".gitignore")
}

func WorktreesDir(repoRoot string) string {
	return filepath.Join(ProjectDir(repoRoot), "worktrees")
}

func EnsureGlobalDirs() error {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return err
	}
	watchDir, err := WatchDir()
	if err != nil {
		return err
	}
	runsDir, err := RunsDir()
	if err != nil {
		return err
	}
	for _, path := range []string{autoDir, watchDir, runsDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
	}
	return nil
}

func EnsureProjectDir(repoRoot string) error {
	return os.MkdirAll(ProjectDir(repoRoot), 0o755)
}
