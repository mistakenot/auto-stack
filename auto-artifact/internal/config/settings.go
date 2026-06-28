// Package config loads and validates auto-artifact's settings file
// (~/.auto/artifact/settings.json), which holds the S3 bucket coordinates and
// upload credentials.
package config

import (
	"path/filepath"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

const (
	artifactDirName  = "artifact"
	settingsFileName = "settings.json"
	uploadsFileName  = "uploads.jsonl"
)

// ValidationError is an alias for the shared validation error type.
type ValidationError = sharedconfig.ValidationError

// ValidationErrorsError is an alias for the shared validation errors wrapper.
type ValidationErrorsError = sharedconfig.ValidationErrorsError

// Settings is the on-disk auto-artifact configuration.
type Settings struct {
	Endpoint         string `json:"endpoint"`
	Bucket           string `json:"bucket"`
	Region           string `json:"region"`
	AccessKeyID      string `json:"access_key_id"`
	SecretAccessKey  string `json:"secret_access_key"`
	DefaultRetention string `json:"default_retention"`
}

// ArtifactDir returns the path to ~/.auto/artifact.
func ArtifactDir() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, artifactDirName), nil
}

// SettingsPath returns the path to ~/.auto/artifact/settings.json.
func SettingsPath() (string, error) {
	dir, err := ArtifactDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, settingsFileName), nil
}

// UploadsLogPath returns the path to ~/.auto/artifact/uploads.jsonl.
func UploadsLogPath() (string, error) {
	dir, err := ArtifactDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, uploadsFileName), nil
}

// Load reads and strictly decodes the settings file (unknown fields rejected).
func Load(path string) (Settings, error) {
	var cfg Settings
	if err := sharedconfig.DecodeJSONFileStrict(path, &cfg); err != nil {
		return Settings{}, err
	}
	return cfg, nil
}
