package config

import (
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

// EnsureGlobalConfig loads or creates the shared project registry, delegating to
// the shared package — which also performs the one-time migration from the
// legacy ~/.auto/watch/settings.json. Centralizing it there means `auto init`
// and `auto watch init` migrate identically, in any order.
func EnsureGlobalConfig() (string, model.GlobalConfig, bool, error) {
	return sharedconfig.EnsureProjects()
}

// EnsureHostFile loads or creates ~/.auto/host.json.
// Delegates to the shared config package.
func EnsureHostFile() (string, sharedconfig.HostConfig, bool, error) {
	return sharedconfig.EnsureHost()
}
