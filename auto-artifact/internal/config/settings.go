// Package config loads and validates auto-artifact's settings file
// (~/.auto/artifact/settings.json), which holds the S3 bucket coordinates and
// upload credentials.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-artifact/internal/artifact"
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

// LoadValidated loads the settings file and validates it, returning a
// ValidationErrorsError if any required field is missing or malformed.
func LoadValidated(path string) (Settings, error) {
	cfg, err := Load(path)
	if err != nil {
		return Settings{}, err
	}
	if errs := Validate(path, cfg); len(errs) > 0 {
		return Settings{}, &ValidationErrorsError{Path: path, Errors: errs}
	}
	return cfg, nil
}

// Validate enforces the required fields (endpoint/bucket/region/credentials)
// and that default_retention, when set, is one of the four tiers. Returns a
// structured error per the shared ValidationError shape mandated by CLAUDE.md.
func Validate(path string, cfg Settings) []ValidationError {
	var errs []ValidationError
	required := []struct {
		field, value string
	}{
		{"endpoint", cfg.Endpoint},
		{"bucket", cfg.Bucket},
		{"region", cfg.Region},
		{"access_key_id", cfg.AccessKeyID},
		{"secret_access_key", cfg.SecretAccessKey},
	}
	for _, r := range required {
		if strings.TrimSpace(r.value) == "" {
			errs = append(errs, ValidationError{
				Code:    "required",
				Path:    path,
				Field:   r.field,
				Message: r.field + " is required and must be a non-empty string",
				Value:   r.value,
			})
		}
	}
	if v := strings.TrimSpace(cfg.Endpoint); v != "" && !strings.HasPrefix(v, "https://") {
		errs = append(errs, ValidationError{
			Code:    "format",
			Path:    path,
			Field:   "endpoint",
			Message: "endpoint must be an https:// URL (the tool never emits http URLs)",
			Value:   cfg.Endpoint,
		})
	}
	if v := strings.TrimSpace(cfg.DefaultRetention); v != "" && !artifact.ValidRetention(v) {
		errs = append(errs, ValidationError{
			Code:    "enum",
			Path:    path,
			Field:   "default_retention",
			Message: "default_retention must be one of " + strings.Join(artifact.RetentionTiers, ", "),
			Value:   cfg.DefaultRetention,
		})
	}
	return errs
}

// WriteSecure writes settings as indented JSON with mode 0600 and ensures the
// parent ~/.auto/artifact directory is 0700. The shared JSON writers force
// 0644, which is unsafe for a file holding secret_access_key — hence this
// dedicated writer. Chmod is applied unconditionally so an existing file/dir
// with looser permissions is tightened.
func WriteSecure(path string, cfg Settings) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}
