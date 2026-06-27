package source

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-skill/internal/transport"
)

// Source is the canonical descriptor for any skill source input.
type Source struct {
	Host     string   // lowercased hostname, e.g. "github.com"
	RepoPath []string // full ordered path components, e.g. ["acme","platform","skills"]
	Ref      string   // git ref from deep-link split or --version (may be empty)
	Subpath  string   // subpath within repo from deep-link (may be empty)
	Local    bool     // true for ./relative, /absolute, or file: sources
	URL      string   // canonical URL (from transport.CanonicalizeURL for remote; raw path for local)
}

// ParseOptions controls ParseSource behavior.
type ParseOptions struct {
	Version     string      // explicit --version overrides deep-link ref
	RefResolver RefResolver // resolver for deep-link splitting (nil = no deep-link resolution)
}

// ParseSource normalises every accepted source surface form into one
// canonical Source descriptor. It delegates remote URL validation to the
// transport package and handles local paths, bare owner/repo shorthand,
// and deep-link splitting.
func ParseSource(input string, opts ParseOptions) (Source, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Source{}, &transport.TransportError{
			Code:    transport.CodeUnsupportedTransport,
			Message: "empty source",
		}
	}

	// ── Local paths ──────────────────────────────────────────────────
	if isLocalPath(input) {
		return parseLocal(input), nil
	}

	// ── Remote sources ───────────────────────────────────────────────

	// Bare owner/repo: if the first segment before "/" has no dot, assume github.com.
	remotePath := input
	if isBareOwnerRepo(input) {
		remotePath = "github.com/" + input
	}

	canonical, id, err := transport.CanonicalizeURL(remotePath)
	if err != nil {
		return Source{}, err
	}

	src := Source{
		Host:     id.Host,
		RepoPath: id.Path,
		URL:      canonical,
	}

	// ── Deep-link detection ──────────────────────────────────────────
	// Look for GitHub /tree/ or GitLab /-/tree/ patterns in the
	// *original* input URL path (before transport stripped them).
	treeSegs, repoPrefix := extractTreeSegs(remotePath)
	if len(treeSegs) > 0 {
		// Re-canonicalize just the repo prefix to get correct URL/identity.
		repoCanonical, repoID, err := transport.CanonicalizeURL(repoPrefix)
		if err != nil {
			return Source{}, err
		}
		src.Host = repoID.Host
		src.RepoPath = repoID.Path
		src.URL = repoCanonical

		if opts.RefResolver != nil {
			ref, subpath, err := SplitDeepLink(treeSegs, opts.RefResolver)
			if err != nil {
				return Source{}, err
			}
			src.Ref = ref
			src.Subpath = subpath
		}
	}

	// ── Version override ─────────────────────────────────────────────
	if opts.Version != "" {
		src.Ref = opts.Version
	}

	return src, nil
}

// isLocalPath returns true for ./relative, ../relative, /absolute, or file: scheme.
func isLocalPath(input string) bool {
	if strings.HasPrefix(input, "./") || strings.HasPrefix(input, "../") || input == "." || input == ".." {
		return true
	}
	if strings.HasPrefix(input, "/") {
		return true
	}
	if strings.HasPrefix(strings.ToLower(input), "file:") {
		return true
	}
	return false
}

// parseLocal builds a Source for a local filesystem path.
func parseLocal(input string) Source {
	path := input
	if strings.HasPrefix(strings.ToLower(input), "file:") {
		// Strip file:// or file:/// scheme.
		u, err := url.Parse(input)
		if err == nil {
			path = filepath.Clean(u.Path)
		} else {
			// Fallback: strip scheme prefix manually.
			path = strings.TrimPrefix(input, "file://")
			path = filepath.Clean(path)
		}
	}
	return Source{
		Local: true,
		URL:   path,
	}
}

// isBareOwnerRepo returns true for "owner/repo" style inputs where the
// first segment contains no dot (so it's not a hostname).
func isBareOwnerRepo(input string) bool {
	// Must not be a URL scheme, SSH shorthand, or flag.
	if strings.Contains(input, "://") {
		return false
	}
	if strings.HasPrefix(input, "git@") {
		return false
	}
	if strings.HasPrefix(input, "-") {
		return false
	}

	firstSeg, _, hasSep := strings.Cut(input, "/")
	if !hasSep {
		return false // Single segment, not owner/repo.
	}

	// If first segment has a dot, treat it as a hostname.
	return !strings.Contains(firstSeg, ".")
}

// extractTreeSegs looks for GitHub /tree/ or GitLab /-/tree/ patterns
// in a URL path. Returns the segments after /tree/ and the repo URL prefix.
// Returns nil segs if no tree pattern is found.
func extractTreeSegs(raw string) (segs []string, repoPrefix string) {
	// Normalise: extract the path portion from the URL.
	pathStr := raw
	scheme := ""

	// Handle git@host:path SSH shorthand.
	if strings.HasPrefix(raw, "git@") && !strings.Contains(raw, "://") {
		colonIdx := strings.Index(raw, ":")
		if colonIdx > 0 {
			scheme = raw[:colonIdx+1]
			pathStr = raw[colonIdx+1:]
		}
	} else if idx := strings.Index(raw, "://"); idx > 0 {
		// scheme://host/path
		rest := raw[idx+3:]
		slashIdx := strings.Index(rest, "/")
		if slashIdx < 0 {
			return nil, raw
		}
		scheme = raw[:idx+3+slashIdx]
		pathStr = rest[slashIdx:]
	} else {
		// bare host/path: first segment is host
		slashIdx := strings.Index(raw, "/")
		if slashIdx < 0 {
			return nil, raw
		}
		scheme = raw[:slashIdx]
		pathStr = raw[slashIdx:]
	}

	// Check for GitLab /-/tree/ first (more specific).
	if before, after, ok := strings.Cut(pathStr, "/-/tree/"); ok {
		repoPrefix = scheme + before
		after = strings.Trim(after, "/")
		if after == "" {
			return nil, raw
		}
		return strings.Split(after, "/"), repoPrefix
	}

	// Check for GitHub /tree/.
	if before, after, ok := strings.Cut(pathStr, "/tree/"); ok {
		repoPrefix = scheme + before
		after = strings.Trim(after, "/")
		if after == "" {
			return nil, raw
		}
		return strings.Split(after, "/"), repoPrefix
	}

	return nil, raw
}
