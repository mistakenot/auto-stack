package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-watch/internal/model"
)

func DefaultProjectConfig(projectID string) model.ProjectConfig {
	return model.ProjectConfig{
		ID:       NormalizeID(projectID),
		Tasks:    map[string]model.TaskDef{},
		Triggers: map[string]model.TriggerDef{},
	}
}

func LoadProjectConfig(repoRoot string) (model.ProjectConfig, error) {
	var cfg model.ProjectConfig
	path := ProjectConfigPath(repoRoot)
	if err := sharedconfig.DecodeJSONFileStrict(path, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Tasks == nil {
		cfg.Tasks = map[string]model.TaskDef{}
	}
	if cfg.Triggers == nil {
		cfg.Triggers = map[string]model.TriggerDef{}
	}
	return cfg, nil
}

func SaveProjectConfig(repoRoot string, cfg model.ProjectConfig) error {
	if cfg.Tasks == nil {
		cfg.Tasks = map[string]model.TaskDef{}
	}
	if cfg.Triggers == nil {
		cfg.Triggers = map[string]model.TriggerDef{}
	}
	return sharedconfig.WriteJSONFile(ProjectConfigPath(repoRoot), cfg)
}

func EnsureProjectConfig(repoRoot, projectID string) (model.ProjectConfig, bool, error) {
	if err := EnsureProjectDir(repoRoot); err != nil {
		return model.ProjectConfig{}, false, err
	}
	path := ProjectConfigPath(repoRoot)
	if _, err := os.Stat(path); err == nil {
		cfg, err := LoadProjectConfig(repoRoot)
		return cfg, false, err
	} else if !os.IsNotExist(err) {
		return model.ProjectConfig{}, false, err
	}
	cfg := DefaultProjectConfig(projectID)
	if err := SaveProjectConfig(repoRoot, cfg); err != nil {
		return model.ProjectConfig{}, false, err
	}
	return cfg, true, nil
}

func EnsureWorktreeIgnore(repoRoot string) (string, bool, error) {
	if err := EnsureProjectDir(repoRoot); err != nil {
		return "", false, err
	}
	path := ProjectGitIgnorePath(repoRoot)
	lines := []string{}
	if file, err := os.Open(path); err == nil {
		defer func() { _ = file.Close() }()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return "", false, err
		}
	} else if !os.IsNotExist(err) {
		return "", false, err
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "worktrees/" {
			return path, false, nil
		}
	}
	lines = append(lines, "worktrees/")
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", false, fmt.Errorf("write %s: %w", path, err)
	}
	return path, true, nil
}

// NormalizeID delegates to the shared registry normalizer so all tools agree.
func NormalizeID(value string) string {
	return sharedconfig.NormalizeID(value)
}

// UpsertProjectRef adds or replaces a project in the registry. It lives in
// global.go, delegating to the shared registry upsert.
