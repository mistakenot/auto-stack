package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

const (
	searchDirName    = "search"
	etlDirName       = "etl"
	outputDirName    = "output"
	settingsFileName = "settings.json"
	DefaultIndexName = "default"
)

var indexNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

// ValidationError is an alias for the shared validation error type.
type ValidationError = sharedconfig.ValidationError

// ValidationErrorsError is an alias for the shared validation errors wrapper.
type ValidationErrorsError = sharedconfig.ValidationErrorsError

type SharedSettings struct {
	Host string `json:"host"`
}

type SearchSettings struct {
	DefaultIndex string `json:"default_index"`
	DefaultInput string `json:"default_input"`
}

func SharedSettingsPath() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, settingsFileName), nil
}

func SearchDir() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, searchDirName), nil
}

func SearchSettingsPath() (string, error) {
	searchDir, err := SearchDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(searchDir, settingsFileName), nil
}

func DefaultInputPath() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, etlDirName, outputDirName), nil
}

func IndexPath(name string) (string, error) {
	searchDir, err := SearchDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(searchDir, name+".sqlite"), nil
}

func DefaultSharedSettings() SharedSettings {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown"
	}
	return SharedSettings{Host: hostname}
}

func DefaultSearchSettings() (SearchSettings, error) {
	defaultInput, err := DefaultInputPath()
	if err != nil {
		return SearchSettings{}, err
	}
	return SearchSettings{
		DefaultIndex: DefaultIndexName,
		DefaultInput: defaultInput,
	}, nil
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

func ValidateSearchSettings(path string, cfg SearchSettings) []ValidationError {
	var errs []ValidationError
	if strings.TrimSpace(cfg.DefaultIndex) == "" {
		errs = append(errs, ValidationError{
			Code:    "required",
			Path:    path,
			Field:   "default_index",
			Message: "default_index is required and must be a non-empty string",
			Value:   cfg.DefaultIndex,
		})
	} else if !indexNamePattern.MatchString(cfg.DefaultIndex) {
		errs = append(errs, ValidationError{
			Code:    "format",
			Path:    path,
			Field:   "default_index",
			Message: "default_index must match ^[a-z0-9]+(?:[._-][a-z0-9]+)*$",
			Value:   cfg.DefaultIndex,
		})
	}
	if strings.TrimSpace(cfg.DefaultInput) == "" {
		errs = append(errs, ValidationError{
			Code:    "required",
			Path:    path,
			Field:   "default_input",
			Message: "default_input is required and must be a non-empty string",
			Value:   cfg.DefaultInput,
		})
	}
	return errs
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

func LoadSearchSettings(path string) (SearchSettings, error) {
	var cfg SearchSettings
	if err := sharedconfig.DecodeJSONFileStrict(path, &cfg); err != nil {
		return SearchSettings{}, err
	}
	if errs := ValidateSearchSettings(path, cfg); len(errs) > 0 {
		return SearchSettings{}, &ValidationErrorsError{Path: path, Errors: errs}
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

func EnsureSearchSettings() (string, SearchSettings, bool, error) {
	path, err := SearchSettingsPath()
	if err != nil {
		return "", SearchSettings{}, false, err
	}
	searchDir, err := SearchDir()
	if err != nil {
		return "", SearchSettings{}, false, err
	}
	if err := os.MkdirAll(searchDir, 0o755); err != nil {
		return "", SearchSettings{}, false, fmt.Errorf("create %s: %w", searchDir, err)
	}
	if _, err := os.Stat(path); err == nil {
		cfg, err := LoadSearchSettings(path)
		return path, cfg, false, err
	} else if !os.IsNotExist(err) {
		return "", SearchSettings{}, false, fmt.Errorf("stat %s: %w", path, err)
	}

	cfg, err := DefaultSearchSettings()
	if err != nil {
		return "", SearchSettings{}, false, err
	}
	if err := sharedconfig.WriteJSONFile(path, cfg); err != nil {
		return "", SearchSettings{}, false, err
	}
	return path, cfg, true, nil
}
