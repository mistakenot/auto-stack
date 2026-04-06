package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-watch/internal/model"
)

type HostInfo struct {
	HostID   string `json:"hostId"`
	Hostname string `json:"hostname,omitempty"`
}

func LoadGlobalConfig(path string) (model.GlobalConfig, error) {
	var cfg model.GlobalConfig
	if err := decodeJSONFile(path, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Projects == nil {
		cfg.Projects = []model.ProjectRef{}
	}
	return cfg, nil
}

func SaveGlobalConfig(path string, cfg model.GlobalConfig) error {
	if cfg.Projects == nil {
		cfg.Projects = []model.ProjectRef{}
	}
	return writeJSONFile(path, cfg)
}

func EnsureGlobalConfig() (string, model.GlobalConfig, bool, error) {
	path, err := SettingsPath()
	if err != nil {
		return "", model.GlobalConfig{}, false, err
	}
	if err := EnsureGlobalDirs(); err != nil {
		return "", model.GlobalConfig{}, false, err
	}
	if _, err := os.Stat(path); err == nil {
		cfg, err := LoadGlobalConfig(path)
		return path, cfg, false, err
	} else if !os.IsNotExist(err) {
		return "", model.GlobalConfig{}, false, err
	}
	cfg := model.GlobalConfig{Projects: []model.ProjectRef{}}
	if err := SaveGlobalConfig(path, cfg); err != nil {
		return "", model.GlobalConfig{}, false, err
	}
	return path, cfg, true, nil
}

func EnsureHostFile() (string, HostInfo, bool, error) {
	path, err := HostPath()
	if err != nil {
		return "", HostInfo{}, false, err
	}
	if err := EnsureGlobalDirs(); err != nil {
		return "", HostInfo{}, false, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return "", HostInfo{}, false, fmt.Errorf("resolve hostname: %w", err)
	}
	info := HostInfo{HostID: hostname}
	if _, err := os.Stat(path); err == nil {
		var existing HostInfo
		if err := decodeJSONFile(path, &existing); err == nil {
			if existing.HostID != "" {
				return path, existing, false, nil
			}
		}
	}
	if err := writeJSONFile(path, info); err != nil {
		return "", HostInfo{}, false, err
	}
	return path, info, true, nil
}

func decodeJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
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
