package github

import (
	"testing"
)

func TestParseGitHubRemote_HTTPS(t *testing.T) {
	owner, repo := parseGitHubRemote("https://github.com/owner/repo.git")
	if owner != "owner" || repo != "repo" {
		t.Errorf("got %s/%s, want owner/repo", owner, repo)
	}
}

func TestParseGitHubRemote_HTTPSNoGit(t *testing.T) {
	owner, repo := parseGitHubRemote("https://github.com/owner/repo")
	if owner != "owner" || repo != "repo" {
		t.Errorf("got %s/%s, want owner/repo", owner, repo)
	}
}

func TestParseGitHubRemote_SSH(t *testing.T) {
	owner, repo := parseGitHubRemote("git@github.com:owner/repo.git")
	if owner != "owner" || repo != "repo" {
		t.Errorf("got %s/%s, want owner/repo", owner, repo)
	}
}

func TestParseGitHubRemote_SSHNoGit(t *testing.T) {
	owner, repo := parseGitHubRemote("git@github.com:owner/repo")
	if owner != "owner" || repo != "repo" {
		t.Errorf("got %s/%s, want owner/repo", owner, repo)
	}
}

func TestParseGitHubRemote_NonGitHub(t *testing.T) {
	tests := []string{
		"https://gitlab.com/owner/repo.git",
		"git@bitbucket.org:owner/repo.git",
		"https://example.com/owner/repo",
		"",
	}
	for _, remote := range tests {
		owner, repo := parseGitHubRemote(remote)
		if owner != "" || repo != "" {
			t.Errorf("parseGitHubRemote(%q) = %s/%s, want empty", remote, owner, repo)
		}
	}
}

func TestDiscoverRepos_DeduplicateAcrossWorkspaces(t *testing.T) {
	remotes := map[string]string{
		"/home/user/project1": "https://github.com/owner/repo.git",
		"/home/user/project2": "git@github.com:owner/repo.git",
	}
	repos := DiscoverRepos(remotes)
	if len(repos) != 1 {
		t.Errorf("got %d repos, want 1 (deduped)", len(repos))
	}
	if repos[0].OwnerRepo() != "owner/repo" {
		t.Errorf("got %s, want owner/repo", repos[0].OwnerRepo())
	}
}

func TestDiscoverRepos_MixedRemotes(t *testing.T) {
	remotes := map[string]string{
		"/a": "https://github.com/owner/repo.git",
		"/b": "https://gitlab.com/other/project.git",
		"/c": "git@github.com:owner2/repo2.git",
	}
	repos := DiscoverRepos(remotes)
	if len(repos) != 2 {
		t.Errorf("got %d repos, want 2 (github only)", len(repos))
	}
}

func TestDiscoverRepos_TrailingGitNormalization(t *testing.T) {
	remotes := map[string]string{
		"/a": "https://github.com/owner/repo.git",
		"/b": "https://github.com/owner/repo",
	}
	repos := DiscoverRepos(remotes)
	if len(repos) != 1 {
		t.Errorf("got %d repos, want 1 (normalized .git)", len(repos))
	}
}

func TestDiscoverRepos_EmptyCache(t *testing.T) {
	repos := DiscoverRepos(map[string]string{})
	if len(repos) != 0 {
		t.Errorf("got %d repos, want 0", len(repos))
	}
}
