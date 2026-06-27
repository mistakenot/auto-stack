package cmd

import (
	"fmt"
	"os"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

// loadRegistryQuietly returns the project registry from ~/.auto/projects.json,
// or an empty one if it is absent or unreadable — never an error. It is strictly
// read-only: it deliberately does NOT call EnsureProjects, so the ETL never
// mutates (creates/migrates) the registry just by running discovery.
func loadRegistryQuietly() sharedconfig.ProjectsConfig {
	path, err := sharedconfig.ProjectsConfigPath()
	if err != nil {
		return sharedconfig.ProjectsConfig{}
	}
	if _, err := os.Stat(path); err != nil {
		return sharedconfig.ProjectsConfig{}
	}
	cfg, err := sharedconfig.LoadProjects(path)
	if err != nil {
		return sharedconfig.ProjectsConfig{}
	}
	return cfg
}

// gateStats records how a remotes map fared against the registry gate.
type gateStats struct {
	total   int
	kept    int
	skipped int
}

// filterRemotesByRegistry returns a new map containing only the workspace→remote
// entries that belong to a registered project, matched path-first then
// remote-fallback (FindProjectByPath, then FindProjectByRemote). The input map is
// never mutated. When the registry is empty, the result is empty (strict: the
// registry is the sole authority for repo discovery).
func filterRemotesByRegistry(remotes map[string]string, cfg sharedconfig.ProjectsConfig) (map[string]string, gateStats) {
	kept := make(map[string]string)
	stats := gateStats{total: len(remotes)}
	for path, remote := range remotes {
		if cfg.FindProjectByPath(path) != nil || cfg.FindProjectByRemote(remote) != nil {
			kept[path] = remote
			stats.kept++
		} else {
			stats.skipped++
		}
	}
	return kept, stats
}

// gateSummary renders a one-line, stderr-friendly summary of the gate result.
// When the registry is empty or nothing was kept, it appends a remediation hint
// pointing the user at `auto init --project`.
func gateSummary(stats gateStats, registryEmpty bool) string {
	msg := fmt.Sprintf("registry gate: kept %d, skipped %d of %d discovered repo(s)",
		stats.kept, stats.skipped, stats.total)
	if registryEmpty || stats.kept == 0 {
		msg += " — run 'auto init --project' in each repo you want indexed"
	}
	return msg
}
