package source

import "strings"

// RefResolver checks if a ref prefix resolves to a real git ref.
type RefResolver interface {
	// ResolveRef returns true if the given ref string resolves to a real ref.
	ResolveRef(ref string) bool
}

// DeepLinkError indicates that no candidate ref prefix resolved and the
// caller should use explicit --version + --path flags instead.
type DeepLinkError struct {
	Message string
}

func (e *DeepLinkError) Error() string { return e.Message }

// SplitDeepLink splits a GitHub/GitLab deep-link path-after-tree into
// (ref, subpath) using the longest-resolving-ref algorithm.
// segs are the path segments after "/tree/" or "/-/tree/".
// Returns error if no candidate prefix resolves (requires --version + --path).
func SplitDeepLink(segs []string, resolver RefResolver) (ref string, subpath string, err error) {
	if len(segs) == 0 {
		return "", "", &DeepLinkError{
			Message: "deep-link has no path segments after /tree/; use --version and --path explicitly",
		}
	}

	// Try every candidate ref from longest to shortest.
	for n := len(segs); n >= 1; n-- {
		candidate := strings.Join(segs[:n], "/")
		if resolver.ResolveRef(candidate) {
			sub := ""
			if n < len(segs) {
				sub = strings.Join(segs[n:], "/")
			}
			return candidate, sub, nil
		}
	}

	return "", "", &DeepLinkError{
		Message: "no ref prefix resolved from deep-link path; use --version and --path explicitly",
	}
}
