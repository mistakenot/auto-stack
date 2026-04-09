package github

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// RepoRef holds an owner/repo pair extracted from a git remote URL.
type RepoRef struct {
	Owner     string
	Repo      string
	GitRemote string // original remote URL for linkage
}

// OwnerRepo returns "owner/repo".
func (r RepoRef) OwnerRepo() string {
	return r.Owner + "/" + r.Repo
}

var (
	// Match HTTPS: https://github.com/owner/repo.git or https://github.com/owner/repo
	httpsRe = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?$`)
	// Match SSH: git@github.com:owner/repo.git or git@github.com:owner/repo
	sshRe = regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`)
)

// DiscoverRepos extracts unique GitHub owner/repo pairs from an ETL remotes cache.
// Non-GitHub remotes are silently ignored.
func DiscoverRepos(remotes map[string]string) []RepoRef {
	seen := make(map[string]bool)
	var result []RepoRef

	for _, remote := range remotes {
		remote = strings.TrimSpace(remote)
		if remote == "" {
			continue
		}

		owner, repo := parseGitHubRemote(remote)
		if owner == "" {
			continue
		}

		key := owner + "/" + repo
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, RepoRef{Owner: owner, Repo: repo, GitRemote: remote})
	}

	if len(result) == 0 {
		fmt.Fprintf(os.Stderr, "warning: no GitHub remotes found in settings cache\n")
	}

	return result
}

// parseGitHubRemote extracts owner/repo from a GitHub HTTPS or SSH URL.
// Returns ("", "") for non-GitHub URLs.
func parseGitHubRemote(remote string) (owner, repo string) {
	if m := httpsRe.FindStringSubmatch(remote); m != nil {
		return m[1], m[2]
	}
	if m := sshRe.FindStringSubmatch(remote); m != nil {
		return m[1], m[2]
	}
	return "", ""
}
