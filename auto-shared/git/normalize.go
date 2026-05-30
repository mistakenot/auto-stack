// Package git provides shared git remote URL normalisation and repository
// identity helpers. It is the single source of truth for deriving a stable
// repo_id from a remote URL, so that auto-etl (which writes repo_id into the
// parquet datasets) and auto-search (which matches against it) cannot drift.
package git

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// NormalizeRemoteURL normalizes a git remote URL to a canonical HTTPS form.
// SSH URLs are converted to HTTPS, trailing .git is stripped, and the
// hostname is lowercased. Returns empty string for empty input.
func NormalizeRemoteURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// ssh://git@host/owner/repo.git → https://host/owner/repo
	if strings.HasPrefix(raw, "ssh://") {
		raw = strings.TrimPrefix(raw, "ssh://")
		raw = strings.TrimPrefix(raw, "git@")
		raw = "https://" + raw
	}

	// git@host:owner/repo.git → https://host/owner/repo
	if strings.HasPrefix(raw, "git@") {
		raw = strings.TrimPrefix(raw, "git@")
		raw = "https://" + strings.Replace(raw, ":", "/", 1)
	}

	// Strip trailing .git
	raw = strings.TrimSuffix(raw, ".git")

	// Strip credentials from HTTPS URLs (e.g. https://user:token@host/path → https://host/path)
	if idx := strings.Index(raw, "://"); idx != -1 {
		scheme := raw[:idx+3]
		rest := raw[idx+3:]
		if atIdx := strings.Index(rest, "@"); atIdx != -1 {
			slashIdx := strings.Index(rest, "/")
			if slashIdx == -1 || atIdx < slashIdx {
				rest = rest[atIdx+1:]
			}
		}
		raw = scheme + rest
	}

	// Lowercase the hostname: split on :// then lowercase the host portion.
	if idx := strings.Index(raw, "://"); idx != -1 {
		scheme := raw[:idx+3]
		rest := raw[idx+3:]
		if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
			host := strings.ToLower(rest[:slashIdx])
			path := rest[slashIdx:]
			raw = scheme + host + path
		} else {
			raw = scheme + strings.ToLower(rest)
		}
	}

	return raw
}

// ComputeRepoID returns a stable 16-character hex identifier derived from
// the SHA-256 hash of the normalized remote URL.
func ComputeRepoID(normalizedRemote string) string {
	h := sha256.Sum256([]byte(normalizedRemote))
	return fmt.Sprintf("%x", h)[:16]
}

// ComputeRepoIDFromPath returns a stable 16-character hex identifier derived
// from the SHA-256 hash of the absolute path. Used as a fallback when no
// remote exists.
func ComputeRepoIDFromPath(absPath string) string {
	h := sha256.Sum256([]byte(absPath))
	return fmt.Sprintf("%x", h)[:16]
}
