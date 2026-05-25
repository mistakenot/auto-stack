package git

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RepoInfo holds discovery info about a git repository.
type RepoInfo struct {
	Path   string
	Remote string
}

// DiscoverRepos finds git repositories from the remotes cache and explicit paths.
// Deduplicates by resolved repo path (git rev-parse --show-toplevel).
// Validates each is a git repo.
func DiscoverRepos(remotes map[string]string, explicitPaths []string) []RepoInfo {
	// Collect candidate paths from remotes cache keys and explicit paths.
	candidates := make(map[string]bool)
	for path := range remotes {
		candidates[path] = true
	}
	for _, path := range explicitPaths {
		candidates[path] = true
	}

	// Resolve each candidate to its git toplevel and deduplicate.
	seen := make(map[string]bool)
	var repos []RepoInfo

	for path := range candidates {
		toplevel, err := gitToplevel(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s is not a git repository, skipping\n", path)
			continue
		}

		if seen[toplevel] {
			continue
		}
		seen[toplevel] = true

		remote := gitOriginRemote(toplevel)
		repos = append(repos, RepoInfo{
			Path:   toplevel,
			Remote: remote,
		})
	}

	return repos
}

// gitToplevel resolves a path to its git repository root.
func gitToplevel(path string) (string, error) {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitOriginRemote returns the origin remote URL for a repo path, or empty string if none.
func gitOriginRemote(path string) string {
	cmd := exec.Command("git", "-C", path, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
