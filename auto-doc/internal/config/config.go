package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

const DefaultConfigFile = "settings.json"
const DefaultConfigDir = ".auto/doc"
const DefaultDocsDir = "docs"
const GlobalAutoDir = ".auto"
const GlobalDocDir = ".auto/doc"
const HostConfigFile = "host.json"

var DefaultAgentFiles = []string{"AGENTS.md", "CLAUDE.md"}

const DefaultParallelism = 4

type Config struct {
	DocsDir              string   `json:"docsDir"`
	AgentFiles           []string `json:"agentFiles"`
	Parallelism          int      `json:"parallelism"`
	Ignores              []string `json:"ignores"`
	ExcludeTagsFromIndex []string `json:"excludeTagsFromIndex"`
}

// GlobalConfig holds fields that make sense at the machine level.
type GlobalConfig struct {
	Ignores []string `json:"ignores"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		DocsDir:     DefaultDocsDir,
		AgentFiles:  DefaultAgentFiles,
		Parallelism: DefaultParallelism,
	}

	if path == "" {
		path = DefaultConfigFile
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Apply defaults for zero values
	if cfg.DocsDir == "" {
		cfg.DocsDir = DefaultDocsDir
	}
	if len(cfg.AgentFiles) == 0 {
		cfg.AgentFiles = DefaultAgentFiles
	}
	if cfg.Parallelism == 0 {
		cfg.Parallelism = DefaultParallelism
	}

	return cfg, nil
}

// LoadGlobal loads the global config from ~/.auto/doc/settings.json.
func LoadGlobal() (*GlobalConfig, error) {
	home, err := sharedconfig.HomeDir()
	if err != nil {
		return &GlobalConfig{}, err
	}
	path := filepath.Join(home, GlobalDocDir, DefaultConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &GlobalConfig{}, nil
		}
		return nil, err
	}
	var cfg GlobalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadWithGlobal loads project config and merges global ignores.
func LoadWithGlobal(projectPath string) (*Config, error) {
	cfg, err := Load(projectPath)
	if err != nil {
		return nil, err
	}
	global, err := LoadGlobal()
	if err != nil {
		return nil, err
	}
	cfg.Ignores = unionStrings(cfg.Ignores, global.Ignores)
	return cfg, nil
}

// LoadHost loads host identification from the given path.
// Delegates to the shared config package.
func LoadHost(path string) (*sharedconfig.HostConfig, error) {
	return sharedconfig.LoadHost(path)
}

// unionStrings returns the union of two string slices, preserving order,
// with items from a first then items from b not already in a.
func unionStrings(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]bool, len(a))
	for _, s := range a {
		seen[s] = true
	}
	result := make([]string, len(a))
	copy(result, a)
	for _, s := range b {
		if !seen[s] {
			result = append(result, s)
			seen[s] = true
		}
	}
	return result
}

// GlobalConfigPath returns the path to ~/.auto/doc/settings.json.
func GlobalConfigPath() (string, error) {
	home, err := sharedconfig.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, GlobalDocDir, DefaultConfigFile), nil
}

// HostConfigPath returns the path to ~/.auto/host.json.
func HostConfigPath() (string, error) {
	return sharedconfig.HostConfigPath()
}
