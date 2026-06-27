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
// entries that belong to a registered project. The input map is never mutated.
// When the registry is empty, the result is empty (strict: the registry is the
// sole authority for repo discovery).
//
// An entry is kept when any of the following holds:
//
//   - Its remote matches a registered project's remote. This is the primary,
//     definitive case: it covers worktrees and genuine subdirectories, which
//     resolve the enclosing repo's origin, so the remote already identifies the
//     project regardless of where the workspace sits on disk.
//   - The user registered exactly this directory (FindProjectByExactPath) —
//     explicit intent, kept whatever its remote.
//   - The workspace is nested under a registered project AND carries no distinct
//     remote of its own (a plain subdirectory or local-only repo). With no
//     foreign origin there is nothing out-of-scope to leak.
//
// Crucially, a workspace nested under a registered project whose origin is a
// *foreign*, unregistered remote — a vendored or experimental clone living
// inside a registered project — is NOT kept. Matching it on path prefix alone
// would reopen the exact data-scope leak this gate exists to close (its PRs and
// git history would be indexed via the cached foreign remote).
func filterRemotesByRegistry(remotes map[string]string, cfg sharedconfig.ProjectsConfig) (map[string]string, gateStats) {
	kept := make(map[string]string)
	stats := gateStats{total: len(remotes)}
	for path, remote := range remotes {
		registered := cfg.FindProjectByRemote(remote) != nil ||
			cfg.FindProjectByExactPath(path) != nil ||
			(remote == "" && cfg.FindProjectByPath(path) != nil)
		if registered {
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
