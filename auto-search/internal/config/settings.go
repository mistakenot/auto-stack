package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	autoDirName      = ".auto"
	searchDirName    = "search"
	etlDirName       = "etl"
	outputDirName    = "output"
	settingsFileName = "settings.json"
	DefaultIndexName = "default"
)

var indexNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

type ValidationError struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   any    `json:"value,omitempty"`
}

type ValidationErrorsError struct {
	Path   string
	Errors []ValidationError
}

func (e *ValidationErrorsError) Error() string {
	if e == nil || len(e.Errors) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("invalid settings")
	if e.Path != "" {
		builder.WriteString(" in ")
		builder.WriteString(e.Path)
	}
	builder.WriteString(": ")
	builder.WriteString(e.Errors[0].Message)
	return builder.String()
}

type SharedSettings struct {
	Host string `json:"host"`
}

type SearchSettings struct {
	DefaultIndex string `json:"default_index"`
	DefaultInput string `json:"default_input"`
}

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

func AutoDir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, autoDirName), nil
}

func SharedSettingsPath() (string, error) {
	autoDir, err := AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, settingsFileName), nil
}

func SearchDir() (string, error) {
	autoDir, err := AutoDir()
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
	autoDir, err := AutoDir()
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
	if err := decodeJSONFile(path, &cfg); err != nil {
		return SharedSettings{}, err
	}
	if errs := ValidateSharedSettings(path, cfg); len(errs) > 0 {
		return SharedSettings{}, &ValidationErrorsError{Path: path, Errors: errs}
	}
	return cfg, nil
}

func LoadSearchSettings(path string) (SearchSettings, error) {
	var cfg SearchSettings
	if err := decodeJSONFile(path, &cfg); err != nil {
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
	autoDir, err := AutoDir()
	if err != nil {
		return "", SharedSettings{}, false, err
	}
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		return "", SharedSettings{}, false, fmt.Errorf("create %s: %w", autoDir, err)
	}
	if _, err := os.Stat(path); err == nil {
		cfg, err := LoadSharedSettings(path)
		return path, cfg, false, err
	} else if !os.IsNotExist(err) {
		return "", SharedSettings{}, false, fmt.Errorf("stat %s: %w", path, err)
	}

	cfg := DefaultSharedSettings()
	if err := writeJSONFile(path, cfg); err != nil {
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
	if err := writeJSONFile(path, cfg); err != nil {
		return "", SearchSettings{}, false, err
	}
	return path, cfg, true, nil
}

func decodeJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent for %s: %w", path, err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
