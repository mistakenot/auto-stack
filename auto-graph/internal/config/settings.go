package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

const (
	graphDirName     = "graph"
	settingsFileName = "settings.json"
)

// ValidationError is an alias for the shared validation error type.
type ValidationError = sharedconfig.ValidationError

// ValidationErrorsError is an alias for the shared validation errors wrapper.
type ValidationErrorsError = sharedconfig.ValidationErrorsError

type SharedSettings struct {
	Host string `json:"host"`
}

type GraphSettings struct {
	DefaultOutput string `json:"default_output"`
}

func SharedSettingsPath() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, settingsFileName), nil
}

func GraphDir() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, graphDirName), nil
}

func GraphSettingsPath() (string, error) {
	graphDir, err := GraphDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(graphDir, settingsFileName), nil
}

func DefaultSharedSettings() SharedSettings {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown"
	}
	return SharedSettings{Host: hostname}
}

func DefaultGraphSettings() GraphSettings {
	return GraphSettings{
		DefaultOutput: "json",
	}
}

func ValidateSharedSettings(path string, cfg SharedSettings) []ValidationError {
	if strings.TrimSpace(cfg.Host) == "" {
		return []ValidationError{{
			Code:    "required",
			Path:    path,
			Field:   "host",
			Message: "host is required and must be a non-empty string",
			Value:   cfg.Host,
		}}
	}
	return nil
}

func ValidateGraphSettings(path string, cfg GraphSettings) []ValidationError {
	validOutputs := map[string]bool{"json": true, "dot": true, "mermaid": true}
	if strings.TrimSpace(cfg.DefaultOutput) == "" {
		return []ValidationError{{
			Code:    "required",
			Path:    path,
			Field:   "default_output",
			Message: "default_output is required and must be a non-empty string",
			Value:   cfg.DefaultOutput,
		}}
	}
	if !validOutputs[cfg.DefaultOutput] {
		return []ValidationError{{
			Code:    "format",
			Path:    path,
			Field:   "default_output",
			Message: "default_output must be one of: json, dot, mermaid",
			Value:   cfg.DefaultOutput,
		}}
	}
	return nil
}

func LoadSharedSettings(path string) (SharedSettings, error) {
	var cfg SharedSettings
	if err := sharedconfig.DecodeJSONFileStrict(path, &cfg); err != nil {
		return SharedSettings{}, err
	}
	if errs := ValidateSharedSettings(path, cfg); len(errs) > 0 {
		return SharedSettings{}, &ValidationErrorsError{Path: path, Errors: errs}
	}
	return cfg, nil
}

func LoadGraphSettings(path string) (GraphSettings, error) {
	var cfg GraphSettings
	if err := sharedconfig.DecodeJSONFileStrict(path, &cfg); err != nil {
		return GraphSettings{}, err
	}
	if errs := ValidateGraphSettings(path, cfg); len(errs) > 0 {
		return GraphSettings{}, &ValidationErrorsError{Path: path, Errors: errs}
	}
	return cfg, nil
}

func EnsureSharedSettings() (string, SharedSettings, bool, error) {
	path, err := SharedSettingsPath()
	if err != nil {
		return "", SharedSettings{}, false, err
	}
	if err := sharedconfig.EnsureAutoDir(); err != nil {
		return "", SharedSettings{}, false, err
	}
	if _, err := os.Stat(path); err == nil {
		cfg, err := LoadSharedSettings(path)
		return path, cfg, false, err
	} else if !os.IsNotExist(err) {
		return "", SharedSettings{}, false, fmt.Errorf("stat %s: %w", path, err)
	}

	cfg := DefaultSharedSettings()
	if err := sharedconfig.WriteJSONFile(path, cfg); err != nil {
		return "", SharedSettings{}, false, err
	}
	return path, cfg, true, nil
}

func EnsureGraphSettings() (string, GraphSettings, bool, error) {
	path, err := GraphSettingsPath()
	if err != nil {
		return "", GraphSettings{}, false, err
	}
	graphDir, err := GraphDir()
	if err != nil {
		return "", GraphSettings{}, false, err
	}
	if err := os.MkdirAll(graphDir, 0o755); err != nil {
		return "", GraphSettings{}, false, fmt.Errorf("create %s: %w", graphDir, err)
	}
	if _, err := os.Stat(path); err == nil {
		cfg, err := LoadGraphSettings(path)
		return path, cfg, false, err
	} else if !os.IsNotExist(err) {
		return "", GraphSettings{}, false, fmt.Errorf("stat %s: %w", path, err)
	}

	cfg := DefaultGraphSettings()
	if err := sharedconfig.WriteJSONFile(path, cfg); err != nil {
		return "", GraphSettings{}, false, err
	}
	return path, cfg, true, nil
}
