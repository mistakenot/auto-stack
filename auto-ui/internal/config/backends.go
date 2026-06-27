package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

const backendsFileName = "backends.json"

// Backend is a single autowatch backend the UI proxies to. URI is the dial
// address (unix:// or tcp://); HostID is the authoritative host id learned from
// the backend's daemon.status on connect.
type Backend struct {
	URI    string `json:"uri"`
	Name   string `json:"name,omitempty"`
	HostID string `json:"hostId,omitempty"`
}

// BackendsConfig is the on-disk shape of ~/.auto/ui/backends.json.
type BackendsConfig struct {
	Backends []Backend `json:"backends"`
}

// BackendsPath returns the path to the UI backends config file.
func BackendsPath() (string, error) {
	uiDir, err := UIDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(uiDir, backendsFileName), nil
}

// LoadBackends reads the backends config from path. A missing file is treated
// as an empty config (no backends registered yet).
func LoadBackends(path string) (BackendsConfig, error) {
	var cfg BackendsConfig
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return BackendsConfig{}, nil
		}
		return BackendsConfig{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if err := sharedconfig.DecodeJSONFileStrict(path, &cfg); err != nil {
		return BackendsConfig{}, err
	}
	if errs := validateBackends(path, cfg); len(errs) > 0 {
		return BackendsConfig{}, &ValidationErrorsError{Path: path, Errors: errs}
	}
	return cfg, nil
}

// SaveBackends validates cfg then atomically writes it to path, creating the
// parent directory if needed.
func SaveBackends(path string, cfg BackendsConfig) error {
	if errs := validateBackends(path, cfg); len(errs) > 0 {
		return &ValidationErrorsError{Path: path, Errors: errs}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent for %s: %w", path, err)
	}
	return sharedconfig.WriteJSONFileAtomic(path, cfg)
}

// validateBackends checks the backends config: each URI must be a well-formed
// unix:// or tcp:// address, and both URIs and non-empty host ids must be unique.
func validateBackends(path string, cfg BackendsConfig) []ValidationError {
	var errs []ValidationError
	seenURI := make(map[string]bool, len(cfg.Backends))
	seenHost := make(map[string]bool, len(cfg.Backends))
	for i, b := range cfg.Backends {
		if verr := validateBackendURI(path, i, b.URI); verr != nil {
			errs = append(errs, *verr)
		} else if seenURI[b.URI] {
			errs = append(errs, ValidationError{
				Code:    "duplicate",
				Path:    path,
				Field:   fmt.Sprintf("backends[%d].uri", i),
				Message: fmt.Sprintf("duplicate backend uri %q", b.URI),
				Value:   b.URI,
			})
		} else {
			seenURI[b.URI] = true
		}

		if b.HostID != "" {
			if seenHost[b.HostID] {
				errs = append(errs, ValidationError{
					Code:    "duplicate",
					Path:    path,
					Field:   fmt.Sprintf("backends[%d].hostId", i),
					Message: fmt.Sprintf("duplicate backend hostId %q", b.HostID),
					Value:   b.HostID,
				})
			} else {
				seenHost[b.HostID] = true
			}
		}
	}
	return errs
}

// validateBackendURI returns a ValidationError if uri is not a well-formed
// unix:// or tcp:// transport address (matching transport.Dial's accepted
// schemes), or nil if it is valid.
func validateBackendURI(path string, i int, uri string) *ValidationError {
	field := fmt.Sprintf("backends[%d].uri", i)
	if strings.TrimSpace(uri) == "" {
		return &ValidationError{
			Code:    "required",
			Path:    path,
			Field:   field,
			Message: "backend uri is required",
			Value:   uri,
		}
	}
	scheme, addr, ok := strings.Cut(uri, "://")
	if !ok {
		return &ValidationError{
			Code:    "format",
			Path:    path,
			Field:   field,
			Message: fmt.Sprintf("invalid backend uri %q; expected scheme://address (e.g. unix:///tmp/sock or tcp://127.0.0.1:8080)", uri),
			Value:   uri,
		}
	}
	if scheme != "unix" && scheme != "tcp" {
		return &ValidationError{
			Code:    "format",
			Path:    path,
			Field:   field,
			Message: fmt.Sprintf("unsupported backend uri scheme %q in %q; use unix:// or tcp://", scheme, uri),
			Value:   uri,
		}
	}
	if addr == "" {
		return &ValidationError{
			Code:    "format",
			Path:    path,
			Field:   field,
			Message: fmt.Sprintf("empty address in backend uri %q", uri),
			Value:   uri,
		}
	}
	return nil
}
