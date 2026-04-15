package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	autoDirName  = ".auto"
	hostFileName = "host.json"
)

// HomeDir returns the user's home directory, preferring $HOME over os.UserHomeDir.
func HomeDir() (string, error) {
	if home := os.Getenv("HOME"); strings.TrimSpace(home) != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

// AutoDir returns the path to ~/.auto.
func AutoDir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, autoDirName), nil
}

// HostConfigPath returns the path to ~/.auto/host.json.
func HostConfigPath() (string, error) {
	autoDir, err := AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, hostFileName), nil
}

// EnsureAutoDir creates ~/.auto if it doesn't exist.
func EnsureAutoDir() error {
	autoDir, err := AutoDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(autoDir, 0o755)
}
