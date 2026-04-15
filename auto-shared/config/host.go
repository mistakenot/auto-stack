package config

import (
	"errors"
	"fmt"
	"os"
)

// HostConfig holds host identification stored in ~/.auto/host.json.
type HostConfig struct {
	HostID   string `json:"hostId"`
	Hostname string `json:"hostname,omitempty"`
}

// LoadHost reads and validates a host config file.
// Returns an error if the file is missing or hostId is empty.
func LoadHost(path string) (*HostConfig, error) {
	var cfg HostConfig
	if err := DecodeJSONFile(path, &cfg); err != nil {
		return nil, err
	}
	if cfg.HostID == "" {
		return nil, errors.New("hostId is required in " + path)
	}
	return &cfg, nil
}

// EnsureHost loads ~/.auto/host.json, creating it with os.Hostname() if missing.
// Returns the path, config, whether the file was created, and any error.
func EnsureHost() (string, HostConfig, bool, error) {
	path, err := HostConfigPath()
	if err != nil {
		return "", HostConfig{}, false, err
	}
	if err := EnsureAutoDir(); err != nil {
		return "", HostConfig{}, false, err
	}

	// Try loading existing file.
	if _, err := os.Stat(path); err == nil {
		var existing HostConfig
		if err := DecodeJSONFile(path, &existing); err == nil && existing.HostID != "" {
			return path, existing, false, nil
		}
	}

	// Create with hostname fallback.
	hostname, err := os.Hostname()
	if err != nil {
		return "", HostConfig{}, false, fmt.Errorf("resolve hostname: %w", err)
	}
	cfg := HostConfig{HostID: hostname, Hostname: hostname}
	if err := WriteJSONFile(path, cfg); err != nil {
		return "", HostConfig{}, false, err
	}
	return path, cfg, true, nil
}
