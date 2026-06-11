package config

import (
	"os"

	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-watch/internal/model"
)

// ProjectsPath returns the canonical host-level project registry path
// (~/.auto/projects.json), shared across all auto tools.
func ProjectsPath() (string, error) {
	return sharedconfig.ProjectsConfigPath()
}

func LoadGlobalConfig(path string) (model.GlobalConfig, error) {
	return sharedconfig.LoadProjects(path)
}

func SaveGlobalConfig(path string, cfg model.GlobalConfig) error {
	return sharedconfig.SaveProjects(path, cfg)
}

func ValidateGlobalConfig(cfg model.GlobalConfig) []model.ValidationError {
	return sharedconfig.ValidateProjects(cfg)
}

func UpsertProjectRef(cfg *model.GlobalConfig, project model.ProjectRef) {
	sharedconfig.UpsertProject(cfg, project)
}

// EnsureGlobalConfig loads or creates the shared project registry. On first
// creation it seeds the registry from the legacy ~/.auto/watch/settings.json,
// so projects registered before the registry moved are not lost.
func EnsureGlobalConfig() (string, model.GlobalConfig, bool, error) {
	path, err := ProjectsPath()
	if err != nil {
		return "", model.GlobalConfig{}, false, err
	}
	if err := sharedconfig.EnsureAutoDir(); err != nil {
		return "", model.GlobalConfig{}, false, err
	}
	if _, err := os.Stat(path); err == nil {
		cfg, err := LoadGlobalConfig(path)
		return path, cfg, false, err
	} else if !os.IsNotExist(err) {
		return "", model.GlobalConfig{}, false, err
	}
	cfg := model.GlobalConfig{Projects: []model.ProjectRef{}}
	legacyPath, migrated, ok, err := migrateLegacyProjects()
	if err != nil {
		return "", model.GlobalConfig{}, false, err
	} else if ok {
		cfg = migrated
	}
	if err := SaveGlobalConfig(path, cfg); err != nil {
		return "", model.GlobalConfig{}, false, err
	}
	// Retire the legacy file so an older binary still on PATH can't keep writing
	// to it and silently diverge from the canonical registry.
	if ok {
		_ = os.Rename(legacyPath, legacyPath+".migrated")
	}
	return path, cfg, true, nil
}

// migrateLegacyProjects reads the pre-registry ~/.auto/watch/settings.json, if
// present and non-empty, returning its path and projects so callers can seed
// the shared registry and then retire the legacy file. A missing or empty
// legacy file is not an error (ok=false).
func migrateLegacyProjects() (legacyPath string, cfg model.GlobalConfig, ok bool, err error) {
	legacyPath, err = SettingsPath()
	if err != nil {
		return "", model.GlobalConfig{}, false, err
	}
	if _, err := os.Stat(legacyPath); err != nil {
		if os.IsNotExist(err) {
			return legacyPath, model.GlobalConfig{}, false, nil
		}
		return legacyPath, model.GlobalConfig{}, false, err
	}
	cfg, err = sharedconfig.LoadProjects(legacyPath)
	if err != nil {
		return legacyPath, model.GlobalConfig{}, false, err
	}
	if len(cfg.Projects) == 0 {
		return legacyPath, model.GlobalConfig{}, false, nil
	}
	return legacyPath, cfg, true, nil
}

// EnsureHostFile loads or creates ~/.auto/host.json.
// Delegates to the shared config package.
func EnsureHostFile() (string, sharedconfig.HostConfig, bool, error) {
	return sharedconfig.EnsureHost()
}
