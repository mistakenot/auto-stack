package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

const (
	reflectDirName   = "reflect"
	settingsFileName = "settings.json"
)

// ValidationError is an alias for the shared validation error type.
type ValidationError = sharedconfig.ValidationError

// ValidationErrorsError is an alias for the shared validation errors wrapper.
type ValidationErrorsError = sharedconfig.ValidationErrorsError

type SharedSettings struct {
	Host string `json:"host"`
}

type ReflectSettings struct {
	DefaultOutput string `json:"default_output"`
}

func SharedSettingsPath() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, settingsFileName), nil
}

func ReflectDir() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, reflectDirName), nil
}

func ReflectSettingsPath() (string, error) {
	reflectDir, err := ReflectDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(reflectDir, settingsFileName), nil
}

func DefaultSharedSettings() SharedSettings {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown"
	}
	return SharedSettings{Host: hostname}
}

func DefaultReflectSettings() ReflectSettings {
	return ReflectSettings{DefaultOutput: "json"}
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

func ValidateReflectSettings(path string, cfg ReflectSettings) []ValidationError {
	output := strings.ToLower(strings.TrimSpace(cfg.DefaultOutput))
	if output == "json" || output == "text" {
		return nil
	}
	return []ValidationError{{
		Code:    "invalid_enum",
		Path:    path,
		Field:   "default_output",
		Message: "default_output must be one of: json|text",
		Value:   cfg.DefaultOutput,
	}}
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

func LoadReflectSettings(path string) (ReflectSettings, error) {
	var cfg ReflectSettings
	if err := sharedconfig.DecodeJSONFileStrict(path, &cfg); err != nil {
		return ReflectSettings{}, err
	}
	if errs := ValidateReflectSettings(path, cfg); len(errs) > 0 {
		return ReflectSettings{}, &ValidationErrorsError{Path: path, Errors: errs}
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

func EnsureReflectSettings() (string, ReflectSettings, bool, error) {
	path, err := ReflectSettingsPath()
	if err != nil {
		return "", ReflectSettings{}, false, err
	}
	reflectDir, err := ReflectDir()
	if err != nil {
		return "", ReflectSettings{}, false, err
	}
	if err := os.MkdirAll(reflectDir, 0o755); err != nil {
		return "", ReflectSettings{}, false, fmt.Errorf("create %s: %w", reflectDir, err)
	}
	if _, err := os.Stat(path); err == nil {
		cfg, err := LoadReflectSettings(path)
		return path, cfg, false, err
	} else if !os.IsNotExist(err) {
		return "", ReflectSettings{}, false, fmt.Errorf("stat %s: %w", path, err)
	}

	cfg := DefaultReflectSettings()
	if err := sharedconfig.WriteJSONFile(path, cfg); err != nil {
		return "", ReflectSettings{}, false, err
	}
	return path, cfg, true, nil
}
