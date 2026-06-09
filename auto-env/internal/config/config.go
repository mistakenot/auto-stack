package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultPortBase   = 3000
	DefaultPortStride = 100
	EnvDir            = ".auto/env"
	ConfigFile        = "config.json"
	FilesDir          = "files"
	ManifestFile      = ".generated"
)

type Config struct {
	UpCommand   string    `json:"up_command"`
	DownCommand string    `json:"down_command"`
	PortBase    int       `json:"port_base,omitempty"`
	PortStride  int       `json:"port_stride,omitempty"`
	Delimiters  [2]string `json:"delimiters,omitempty"`
}

func ConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, EnvDir, ConfigFile)
}

func FilesPath(repoRoot string) string {
	return filepath.Join(repoRoot, EnvDir, FilesDir)
}

func ManifestPath(repoRoot string) string {
	return filepath.Join(repoRoot, EnvDir, ManifestFile)
}

func Load(repoRoot string) (*Config, error) {
	path := ConfigPath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("config not found: run auto env init")
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if rawDelims, ok := raw["delimiters"]; ok {
		var arr []string
		if err := json.Unmarshal(rawDelims, &arr); err != nil || len(arr) != 2 {
			return nil, errors.New("delimiters must be a 2-element array: [\"open\", \"close\"]")
		}
		cfg.Delimiters = [2]string{arr[0], arr[1]}
	}

	var missing []string
	if cfg.UpCommand == "" {
		missing = append(missing, "up_command")
	}
	if cfg.DownCommand == "" {
		missing = append(missing, "down_command")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required config fields: %v", missing)
	}

	if cfg.PortBase == 0 {
		cfg.PortBase = DefaultPortBase
	}
	if cfg.PortStride == 0 {
		cfg.PortStride = DefaultPortStride
	}
	if cfg.Delimiters[0] == "" {
		cfg.Delimiters = [2]string{"{{", "}}"}
	}

	return &cfg, nil
}
