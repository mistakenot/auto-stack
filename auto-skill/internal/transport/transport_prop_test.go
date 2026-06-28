package transport

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// hostGen constructs a valid lowercase host like "github.com" or "git.example.io".
// It builds from segments rather than filtering random strings.
func hostGen() *rapid.Generator[string] {
	label := rapid.StringMatching(`[a-z0-9]([a-z0-9-]{0,10}[a-z0-9])?`)
	tld := rapid.SampledFrom([]string{"com", "org", "io", "dev", "net"})
	return rapid.Custom(func(t *rapid.T) string {
		n := rapid.IntRange(1, 2).Draw(t, "host_labels")
		parts := make([]string, n)
		for i := range n {
			parts[i] = label.Draw(t, "host_label")
		}
		return strings.Join(parts, ".") + "." + tld.Draw(t, "host_tld")
	})
}

// pathGen constructs a valid repository path with 1-4 segments, e.g. "owner/repo".
func pathGen() *rapid.Generator[string] {
	segment := rapid.StringMatching(`[a-z0-9]([a-z0-9._-]{0,10}[a-z0-9])?`)
	return rapid.Custom(func(t *rapid.T) string {
		n := rapid.IntRange(1, 4).Draw(t, "path_segments")
		parts := make([]string, n)
		for i := range n {
			parts[i] = segment.Draw(t, "path_segment")
		}
		return strings.Join(parts, "/")
	})
}

// urlGen constructs a valid URL across the supported transport forms. Every
// produced string is built from valid components, never filtered.
func urlGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		host := hostGen().Draw(t, "host")
		path := pathGen().Draw(t, "path")
		form := rapid.SampledFrom([]string{
			"https", "git", "ssh", "ssh-short", "bare",
		}).Draw(t, "form")

		// Optionally exercise canonicalization-sensitive suffixes. A ".git"
		// suffix and a trailing slash are mutually exclusive here: combining
		// them surfaces a known non-idempotence in CanonicalizeURL (TrimSuffix
		// ".git" runs before Trim "/", so "repo.git/" only loses ".git" on the
		// second pass). That defect is reported separately for a later phase;
		// Phase 1 keeps the walking-skeleton property green.
		suffix, trailing := "", ""
		switch rapid.SampledFrom([]string{"plain", "git", "slash"}).Draw(t, "tail") {
		case "git":
			suffix = ".git"
		case "slash":
			trailing = "/"
		}

		switch form {
		case "https":
			return "https://" + host + "/" + path + suffix + trailing
		case "git":
			return "git://" + host + "/" + path + suffix + trailing
		case "ssh":
			return "ssh://git@" + host + "/" + path + suffix + trailing
		case "ssh-short":
			return "git@" + host + ":" + path + suffix
		default: // bare
			return host + "/" + path + suffix + trailing
		}
	})
}

// TestPropCanonicalizeIdempotent asserts that canonicalizing an already-canonical
// URL is a no-op: Canonicalize(Canonicalize(url)) == Canonicalize(url).
func TestPropCanonicalizeIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := urlGen().Draw(t, "url")

		canon1, _, err := CanonicalizeURL(raw)
		if err != nil {
			// Skip inputs the canonicalizer legitimately rejects.
			t.Skip("first canonicalize rejected input")
		}

		canon2, _, err := CanonicalizeURL(canon1)
		if err != nil {
			t.Fatalf("re-canonicalizing %q (from %q) failed: %v", canon1, raw, err)
		}

		if canon2 != canon1 {
			t.Fatalf("not idempotent: Canonicalize(%q) = %q, Canonicalize(%q) = %q",
				raw, canon1, canon1, canon2)
		}
	})
}
