package config

import (
	"os"

	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-watch/internal/model"
)

func LoadGlobalConfig(path string) (model.GlobalConfig, error) {
	var cfg model.GlobalConfig
	if err := sharedconfig.DecodeJSONFileStrict(path, &cfg); err != nil {
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
	return sharedconfig.WriteJSONFile(path, cfg)
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

// EnsureHostFile loads or creates ~/.auto/host.json.
// Delegates to the shared config package.
func EnsureHostFile() (string, sharedconfig.HostConfig, bool, error) {
	return sharedconfig.EnsureHost()
}
