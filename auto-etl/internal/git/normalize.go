package git

import (
	"strings"
)

// StripCredentials removes userinfo (user:password@) from a URL.
func StripCredentials(raw string) string {
	raw = strings.TrimSpace(raw)
	idx := strings.Index(raw, "://")
	if idx == -1 {
		return raw
	}
	scheme := raw[:idx+3]
	rest := raw[idx+3:]
	if atIdx := strings.Index(rest, "@"); atIdx != -1 {
		slashIdx := strings.Index(rest, "/")
		if slashIdx == -1 || atIdx < slashIdx {
			rest = rest[atIdx+1:]
		}
	}
	return scheme + rest
}
