package transport

import (
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mistakenot/auto-shared/git"
)

// CacheIdentity is the canonical repo identity derived from a URL.
// Host is lowercased; Path is the full ordered path components.
type CacheIdentity struct {
	Host string
	Path []string
}

// RelPath returns the slash-joined relative path for filesystem layout.
func (id CacheIdentity) RelPath() string {
	parts := append([]string{id.Host}, id.Path...)
	return strings.Join(parts, "/")
}

// HashSuffix returns a short disambiguation suffix derived from the
// canonical URL via git.ComputeRepoID. Used when two distinct remotes
// would otherwise collide at one cache path.
func (id CacheIdentity) HashSuffix(canonicalURL string) string {
	full := git.ComputeRepoID(canonicalURL)
	if len(full) > 8 {
		return full[:8]
	}
	return full
}

// TransportError represents a transport validation failure with a code.
type TransportError struct {
	Code    string
	Message string
	Value   string
}

func (e *TransportError) Error() string {
	return e.Message
}

const (
	CodeUnsupportedTransport = "unsupported_transport"
	CodeCredentialsInURL     = "credentials_in_url"
)

var allowedSchemes = map[string]bool{
	"https": true,
	"ssh":   true,
	"git":   true,
	"file":  true,
}

var credentialQueryKeys = []string{
	"access_token",
	"private_token",
	"token",
	"x-access-token",
}

var defaultPorts = map[string]string{
	"https": "443",
	"ssh":   "22",
	"git":   "9418",
}

// ContainsCredentials detects userinfo or token query params in a URL.
// This is the shared helper that lock validation delegates to.
func ContainsCredentials(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}

	// Check for git@host:path SSH form — no credentials possible.
	if strings.HasPrefix(raw, "git@") && !strings.Contains(raw, "://") {
		return false
	}

	// For ssh:// URLs, git@host is conventional, not a credential.
	if after, ok := strings.CutPrefix(raw, "ssh://git@"); ok {
		return containsCredentialsParsed("ssh://" + after)
	}

	return containsCredentialsParsed(raw)
}

func containsCredentialsParsed(raw string) bool {
	// Ensure we have a scheme for url.Parse to handle correctly.
	toParse := raw
	if !strings.Contains(toParse, "://") {
		toParse = "https://" + toParse
	}

	u, err := url.Parse(toParse)
	if err != nil {
		return false
	}

	if u.User != nil {
		// git@ alone is not a credential for ssh scheme.
		if u.User.Username() == "git" && (u.Scheme == "ssh" || u.Scheme == "git") {
			_, hasPassword := u.User.Password()
			if !hasPassword {
				return false
			}
		}
		return true
	}

	q := u.Query()
	return slices.ContainsFunc(credentialQueryKeys, func(key string) bool {
		return q.Has(key)
	})
}

// CanonicalizeURL parses and validates a URL, rejecting credentials and
// disallowed schemes. Returns a sanitized canonical URL and the cache identity.
func CanonicalizeURL(raw string) (canonical string, id CacheIdentity, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", CacheIdentity{}, &TransportError{
			Code:    CodeUnsupportedTransport,
			Message: "empty URL",
		}
	}

	// Reject leading dash (could be interpreted as git flag).
	if strings.HasPrefix(raw, "-") {
		return "", CacheIdentity{}, &TransportError{
			Code:    CodeUnsupportedTransport,
			Message: "URL must not start with '-'; this could be interpreted as a git flag",
			Value:   raw,
		}
	}

	// Reject remote-helper forms (ext::, fd::, etc.).
	if idx := strings.Index(raw, "::"); idx > 0 {
		prefix := raw[:idx]
		if !strings.Contains(prefix, "/") && !strings.Contains(prefix, "\\") {
			return "", CacheIdentity{}, &TransportError{
				Code:    CodeUnsupportedTransport,
				Message: fmt.Sprintf("remote-helper transport %q is not allowed; use https, ssh, git, or file", prefix),
				Value:   raw,
			}
		}
	}

	// Detect the scheme.
	scheme, host, pathStr := parseURLParts(raw)

	// Validate scheme.
	if !allowedSchemes[scheme] {
		return "", CacheIdentity{}, &TransportError{
			Code:    CodeUnsupportedTransport,
			Message: fmt.Sprintf("transport scheme %q is not allowed; use https, ssh, git, or file", scheme),
			Value:   raw,
		}
	}

	// Check credentials after scheme detection but before canonicalization.
	if ContainsCredentials(raw) {
		return "", CacheIdentity{}, &TransportError{
			Code:    CodeCredentialsInURL,
			Message: "URL must not embed credentials; use git's credential helper instead",
			Value:   raw,
		}
	}

	// For file:// scheme, handle separately.
	if scheme == "file" {
		absPath := pathStr
		if !filepath.IsAbs(absPath) {
			absPath = "/" + absPath
		}
		absPath = filepath.Clean(absPath)
		canonical = "file://" + absPath
		id = CacheIdentity{
			Host: "_local",
			Path: splitPath(strings.TrimPrefix(absPath, "/")),
		}
		return canonical, id, nil
	}

	// Lowercase host and strip default port.
	host = strings.ToLower(host)
	if colonIdx := strings.LastIndex(host, ":"); colonIdx > 0 {
		portPart := host[colonIdx+1:]
		hostPart := host[:colonIdx]
		if defaultPort, ok := defaultPorts[scheme]; ok && portPart == defaultPort {
			host = hostPart
		}
	}

	// Clean the path: strip .git suffix, leading/trailing slashes.
	pathStr = strings.TrimSuffix(pathStr, ".git")
	pathStr = strings.Trim(pathStr, "/")

	if pathStr == "" {
		return "", CacheIdentity{}, &TransportError{
			Code:    CodeUnsupportedTransport,
			Message: "URL has no repository path",
			Value:   raw,
		}
	}

	pathParts := splitPath(pathStr)

	id = CacheIdentity{
		Host: host,
		Path: pathParts,
	}

	canonical = "https://" + host + "/" + pathStr

	return canonical, id, nil
}

// Endpoint returns the canonical trust identity: scheme://host:port
// (with default-port normalization) or a canonical absolute path for
// file:/local sources.
func Endpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", &TransportError{
			Code:    CodeUnsupportedTransport,
			Message: "empty endpoint",
		}
	}

	if strings.HasPrefix(raw, "-") {
		return "", &TransportError{
			Code:    CodeUnsupportedTransport,
			Message: "endpoint must not start with '-'",
			Value:   raw,
		}
	}

	// Reject remote-helper forms.
	if idx := strings.Index(raw, "::"); idx > 0 {
		prefix := raw[:idx]
		if !strings.Contains(prefix, "/") && !strings.Contains(prefix, "\\") {
			return "", &TransportError{
				Code:    CodeUnsupportedTransport,
				Message: fmt.Sprintf("remote-helper transport %q is not allowed", prefix),
				Value:   raw,
			}
		}
	}

	// Check for absolute local path.
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw), nil
	}

	// Check for credentials.
	if ContainsCredentials(raw) {
		return "", &TransportError{
			Code:    CodeCredentialsInURL,
			Message: "endpoint must not embed credentials",
			Value:   raw,
		}
	}

	scheme, host, _ := parseURLParts(raw)

	if !allowedSchemes[scheme] {
		return "", &TransportError{
			Code:    CodeUnsupportedTransport,
			Message: fmt.Sprintf("transport scheme %q is not allowed", scheme),
			Value:   raw,
		}
	}

	if scheme == "file" {
		// For file:// endpoints, return the canonical path.
		return filepath.Clean("/" + host), nil
	}

	host = strings.ToLower(host)

	// Normalize port: add default port if missing, keep explicit port.
	if colonIdx := strings.LastIndex(host, ":"); colonIdx > 0 {
		portPart := host[colonIdx+1:]
		hostPart := host[:colonIdx]
		if defaultPort, ok := defaultPorts[scheme]; ok && portPart == defaultPort {
			host = hostPart
		}
	}

	// Add default port if none specified.
	if !strings.Contains(host, ":") {
		if defaultPort, ok := defaultPorts[scheme]; ok {
			host = host + ":" + defaultPort
		}
	}

	return scheme + "://" + host, nil
}

// parseURLParts extracts scheme, host, and path from a URL string,
// handling SSH shorthand (git@host:path) and bare host/path forms.
func parseURLParts(raw string) (scheme, host, path string) {
	// ssh://git@host/path or ssh://host/path
	if rest, ok := strings.CutPrefix(raw, "ssh://"); ok {
		rest = strings.TrimPrefix(rest, "git@")
		host, path = splitHostPath(rest)
		return "ssh", host, path
	}

	// git@host:owner/repo (SSH shorthand)
	if rest, ok := strings.CutPrefix(raw, "git@"); ok {
		if colonIdx := strings.Index(rest, ":"); colonIdx > 0 {
			host = rest[:colonIdx]
			path = rest[colonIdx+1:]
			return "ssh", host, path
		}
	}

	// scheme://host/path
	if idx := strings.Index(raw, "://"); idx > 0 {
		scheme = strings.ToLower(raw[:idx])
		rest := raw[idx+3:]
		// Strip git@ prefix for any scheme.
		rest = strings.TrimPrefix(rest, "git@")
		host, path = splitHostPath(rest)
		return scheme, host, path
	}

	// Bare form: github.com/owner/repo → infer HTTPS.
	host, path = splitHostPath(raw)
	return "https", host, path
}

// splitHostPath splits "host[:port]/path" into host and path.
func splitHostPath(s string) (host, path string) {
	host, path, ok := strings.Cut(s, "/")
	if !ok {
		return s, ""
	}
	return host, path
}

// splitPath splits a slash-separated path into components, dropping empty parts.
func splitPath(s string) []string {
	parts := strings.Split(s, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
