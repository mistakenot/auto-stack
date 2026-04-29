package gitutil

import (
	"strings"
	"testing"
)

func TestNormalizeRemote(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		// Standard remotes (no credentials)
		{"https", "https://github.com/org/repo.git", "github.com/org/repo"},
		{"http", "http://github.com/org/repo.git", "github.com/org/repo"},
		{"ssh url", "ssh://git@github.com/org/repo.git", "github.com/org/repo"},
		{"scp-style", "git@github.com:org/repo.git", "github.com/org/repo"},
		{"scp-style no .git", "git@github.com:org/repo", "github.com/org/repo"},
		{"https no .git", "https://github.com/org/repo", "github.com/org/repo"},
		{"host case normalized", "https://GitHub.COM/Org/Repo.git", "github.com/Org/Repo"},

		// Credential-bearing remotes (must strip credentials)
		{"https token", "https://x-access-token:ghp_abc123@github.com/org/repo.git", "github.com/org/repo"},
		{"https basic auth", "https://user:password@github.com/org/repo.git", "github.com/org/repo"},
		{"https oauth", "https://oauth2:gho_xxxx@github.com/org/repo.git", "github.com/org/repo"},
		{"ssh user:pass", "ssh://deploy:secret@github.com/org/repo.git", "github.com/org/repo"},
		{"scp-style deploy user", "deploy@github.com:org/repo.git", "github.com/org/repo"},

		// Edge cases
		{"local fallback", "/tmp/bare-repo", "/tmp/bare-repo"},
		{"whitespace trimmed", "  https://github.com/org/repo.git  ", "github.com/org/repo"},
		{"git scheme", "git://github.com/org/repo.git", "github.com/org/repo"},
		{"with port", "https://gitlab.example.com:8443/org/repo.git", "gitlab.example.com:8443/org/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeRemote(tt.raw)
			if got != tt.want {
				t.Errorf("normalizeRemote(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNormalizeRemote_NeverLeaksCredentials(t *testing.T) {
	credentialURLs := []string{
		"https://x-access-token:ghp_abc123def456@github.com/org/repo.git",
		"https://user:s3cret@github.com/org/repo.git",
		"https://oauth2:gho_xxxx@github.com/org/repo.git",
		"ssh://deploy:s3cret@github.com/org/repo.git",
		"deploy:s3cret@github.com:org/repo.git",
	}

	for _, raw := range credentialURLs {
		got := normalizeRemote(raw)
		if strings.Contains(got, "@") {
			t.Errorf("normalizeRemote(%q) = %q — contains '@', possible credential leak", raw, got)
		}
		if strings.Contains(got, "ghp_") || strings.Contains(got, "gho_") {
			t.Errorf("normalizeRemote(%q) = %q — contains token pattern", raw, got)
		}
		if strings.Contains(got, "s3cret") || strings.Contains(got, "password") {
			t.Errorf("normalizeRemote(%q) = %q — contains password", raw, got)
		}
	}
}
