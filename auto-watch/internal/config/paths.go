package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	autoDirName      = ".auto"
	watchDirName     = "watch"
	settingsFileName = "settings.json"
	hostFileName     = "host.json"
	projectFileName  = "project.json"
	lockFileName     = "daemon.lock"
	pidFileName      = "daemon.pid.json"
	dbFileName       = "logs.sqlite"
)

func HomeDir() (string, error) {
	if home := os.Getenv("HOME"); home != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

func AutoDir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, autoDirName), nil
}

func WatchDir() (string, error) {
	autoDir, err := AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, watchDirName), nil
}

func SettingsPath() (string, error) {
	dir, err := WatchDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, settingsFileName), nil
}

func HostPath() (string, error) {
	dir, err := AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, hostFileName), nil
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
	return filepath.Join(repoRoot, autoDirName, watchDirName)
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
	autoDir, err := AutoDir()
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
