package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

const (
	uiDirName        = "ui"
	settingsFileName = "settings.json"
	defaultPort      = 8080
)

// ValidationError is an alias for the shared validation error type.
type ValidationError = sharedconfig.ValidationError

// ValidationErrorsError is an alias for the shared validation errors wrapper.
type ValidationErrorsError = sharedconfig.ValidationErrorsError

type SharedSettings struct {
	Host string `json:"host"`
}

// Settings holds autoui tool configuration stored at ~/.auto/ui/settings.json.
type Settings struct {
	Port int `json:"port"`
}

func SharedSettingsPath() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, settingsFileName), nil
}

func UIDir() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, uiDirName), nil
}

func UISettingsPath() (string, error) {
	uiDir, err := UIDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(uiDir, settingsFileName), nil
}

func DefaultSharedSettings() SharedSettings {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown"
	}
	return SharedSettings{Host: hostname}
}

func DefaultUISettings() Settings {
	return Settings{
		Port: defaultPort,
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

// Validate checks that the UI settings are well-formed.
func ValidateUISettings(path string, cfg Settings) []ValidationError {
	return validate(path, cfg)
}

func validate(path string, cfg Settings) []ValidationError {
	if cfg.Port < 1 || cfg.Port > 65535 {
		return []ValidationError{{
			Code:    "range",
			Path:    path,
			Field:   "port",
			Message: "port must be between 1 and 65535",
			Value:   cfg.Port,
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

func LoadUISettings(path string) (Settings, error) {
	var cfg Settings
	if err := sharedconfig.DecodeJSONFileStrict(path, &cfg); err != nil {
		return Settings{}, err
	}
	if errs := validate(path, cfg); len(errs) > 0 {
		return Settings{}, &ValidationErrorsError{Path: path, Errors: errs}
	}
	return cfg, nil
}

func SaveUISettings(path string, cfg Settings) error {
	if errs := validate(path, cfg); len(errs) > 0 {
		return &ValidationErrorsError{Path: path, Errors: errs}
	}
	return sharedconfig.WriteJSONFile(path, cfg)
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

func EnsureUISettings() (string, Settings, bool, error) {
	path, err := UISettingsPath()
	if err != nil {
		return "", Settings{}, false, err
	}
	uiDir, err := UIDir()
	if err != nil {
		return "", Settings{}, false, err
	}
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		return "", Settings{}, false, fmt.Errorf("create %s: %w", uiDir, err)
	}
	if _, err := os.Stat(path); err == nil {
		cfg, err := LoadUISettings(path)
		return path, cfg, false, err
	} else if !os.IsNotExist(err) {
		return "", Settings{}, false, fmt.Errorf("stat %s: %w", path, err)
	}

	cfg := DefaultUISettings()
	if err := sharedconfig.WriteJSONFile(path, cfg); err != nil {
		return "", Settings{}, false, err
	}
	return path, cfg, true, nil
}
