package config

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"regexp"
	"strings"
)

// HostConfig holds host identification stored in ~/.auto/host.json.
type HostConfig struct {
	HostID   string `json:"hostId"`
	Hostname string `json:"hostname,omitempty"`
}

var hostIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// ValidateHostID checks that id is a well-formed host identifier.
func ValidateHostID(id string) error {
	if id == "" {
		return errors.New("hostId is required")
	}
	if !hostIDPattern.MatchString(id) {
		return fmt.Errorf("hostId %q must match ^[a-z0-9][a-z0-9._-]*$", id)
	}
	return nil
}

// HostIDQuietly returns the configured host id, never erroring.
// It loads ~/.auto/host.json, falling back to a lowercased os.Hostname(),
// and returns the empty string if even that fails.
func HostIDQuietly() string {
	if path, err := HostConfigPath(); err == nil {
		if cfg, err := LoadHost(path); err == nil && ValidateHostID(cfg.HostID) == nil {
			return cfg.HostID
		}
	}
	if hostname, err := os.Hostname(); err == nil {
		return strings.ToLower(hostname)
	}
	return ""
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

// EnsureHost loads ~/.auto/host.json, creating it with a hostname.username
// default if missing. Returns the path, config, whether the file was created,
// and any error.
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

	// Resolve hostname.
	hostname, err := os.Hostname()
	if err != nil {
		return "", HostConfig{}, false, fmt.Errorf("resolve hostname: %w", err)
	}
	bare := strings.ToLower(hostname)

	// Build hostname.username default, validating before use.
	hostID := bare
	if u, err := user.Current(); err == nil {
		sanitized := strings.ReplaceAll(strings.ToLower(u.Username), " ", "-")
		candidate := bare + "." + sanitized
		if ValidateHostID(candidate) == nil {
			hostID = candidate
		}
	}

	cfg := HostConfig{HostID: hostID, Hostname: hostname}
	if err := WriteJSONFile(path, cfg); err != nil {
		return "", HostConfig{}, false, err
	}
	return path, cfg, true, nil
}
