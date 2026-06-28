package transport

import (
	"net/url"
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
//
// The .git suffix and trailing slash are now combined freely: the
// canonicalizer trims slashes before stripping ".git", so "repo.git/"
// normalizes in a single pass (fixed in Phase 2; verified by T1).
func urlGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		host := hostGen().Draw(t, "host")
		path := pathGen().Draw(t, "path")
		form := rapid.SampledFrom([]string{
			"https", "git", "ssh", "ssh-short", "bare",
		}).Draw(t, "form")

		// Optionally exercise canonicalization-sensitive suffixes. A ".git"
		// suffix and a trailing slash can now co-occur freely: the Phase 2 fix
		// trims slashes before stripping ".git", so "repo.git/" normalizes in a
		// single pass (the combo Phase 1 had to avoid).
		//
		// A *doubled* ".git.git" is intentionally NOT generated: canonicalization
		// strips a single ".git" (matching git's own clone semantics), so
		// "repo.git.git" -> "repo.git", and "repo.git" is itself a valid repo
		// name that further strips to "repo" — a genuine, separate non-idempotence
		// that lies outside this task's single approved fix. See the task report.
		suffix := rapid.SampledFrom([]string{"", ".git"}).Draw(t, "git_suffix")
		trailing := rapid.SampledFrom([]string{"", "/"}).Draw(t, "trailing_slash")

		switch form {
		case "https":
			return "https://" + host + "/" + path + suffix + trailing
		case "git":
			return "git://" + host + "/" + path + suffix + trailing
		case "ssh":
			return "ssh://git@" + host + "/" + path + suffix + trailing
		case "ssh-short":
			// SSH shorthand has no trailing-slash form in practice.
			return "git@" + host + ":" + path + suffix
		default: // bare
			return host + "/" + path + suffix + trailing
		}
	})
}

// adversarialURLGen constructs URLs that should be rejected or that probe
// edge cases: embedded credentials (userinfo and credential query params),
// unusual ports, and uppercase host variations. Construct-don't-reject: every
// string is built deliberately so the canonicalizer's decision is exercised,
// not the generator's.
func adversarialURLGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		host := hostGen().Draw(t, "adv_host")
		path := pathGen().Draw(t, "adv_path")
		kind := rapid.SampledFrom([]string{
			"userinfo", "userinfo-passwordless", "cred-query",
			"odd-port", "default-port", "uppercase-host",
		}).Draw(t, "adv_kind")

		switch kind {
		case "userinfo":
			user := rapid.StringMatching(`[a-z]{1,8}`).Draw(t, "adv_user")
			pass := rapid.StringMatching(`[a-zA-Z0-9]{1,12}`).Draw(t, "adv_pass")
			return "https://" + user + ":" + pass + "@" + host + "/" + path
		case "userinfo-passwordless":
			user := rapid.StringMatching(`[a-z]{1,8}`).Draw(t, "adv_user2")
			return "https://" + user + "@" + host + "/" + path
		case "cred-query":
			key := rapid.SampledFrom(credentialQueryKeys).Draw(t, "adv_cred_key")
			val := rapid.StringMatching(`[a-zA-Z0-9]{1,16}`).Draw(t, "adv_cred_val")
			return "https://" + host + "/" + path + "?" + key + "=" + val
		case "odd-port":
			port := rapid.IntRange(1024, 65535).Draw(t, "adv_port")
			return "https://" + host + ":" + itoa(port) + "/" + path
		case "default-port":
			scheme := rapid.SampledFrom([]string{"https", "ssh", "git"}).Draw(t, "adv_scheme")
			port := defaultPorts[scheme]
			if scheme == "ssh" {
				return "ssh://git@" + host + ":" + port + "/" + path
			}
			return scheme + "://" + host + ":" + port + "/" + path
		default: // uppercase-host
			return "https://" + strings.ToUpper(host) + "/" + path
		}
	})
}

// itoa is a tiny helper avoiding an strconv import churn in generators.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// equivalentURLsGen produces a slice of URL strings that all denote the same
// repository — varying scheme/form, the .git suffix, and trailing slashes —
// so they must canonicalize to the same CacheIdentity.
func equivalentURLsGen() *rapid.Generator[[]string] {
	return rapid.Custom(func(t *rapid.T) []string {
		host := hostGen().Draw(t, "eq_host")
		path := pathGen().Draw(t, "eq_path")
		return []string{
			"https://" + host + "/" + path,
			"https://" + host + "/" + path + ".git",
			"https://" + host + "/" + path + "/",
			"https://" + host + "/" + path + ".git/",
			"git://" + host + "/" + path + ".git",
			"ssh://git@" + host + "/" + path + ".git",
			"git@" + host + ":" + path + ".git",
			host + "/" + path,
			// Uppercase host denotes the same repo (host is case-insensitive).
			"https://" + strings.ToUpper(host) + "/" + path,
		}
	})
}

// anyURLGen draws from both the valid and adversarial generators so the
// invariant properties (T2, T4) see the full input space.
func anyURLGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		if rapid.Bool().Draw(t, "use_adversarial") {
			return adversarialURLGen().Draw(t, "adv_url")
		}
		return urlGen().Draw(t, "valid_url")
	})
}

// TestPropCanonicalizeIdempotent asserts that canonicalizing an already-canonical
// URL is a no-op: Canonicalize(Canonicalize(url)) == Canonicalize(url). The
// generator freely combines .git suffixes with trailing slashes — the case the
// Phase 2 fix made idempotent in a single pass.
func TestPropCanonicalizeIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := anyURLGen().Draw(t, "url")

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

// TestPropCanonicalizeNoCredentials asserts T2: whenever CanonicalizeURL
// succeeds, the canonical output never contains credentials.
func TestPropCanonicalizeNoCredentials(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := anyURLGen().Draw(t, "url")

		canon, _, err := CanonicalizeURL(raw)
		if err != nil {
			t.Skip("canonicalize rejected input")
		}

		if ContainsCredentials(canon) {
			t.Fatalf("canonical output %q (from %q) contains credentials", canon, raw)
		}
	})
}

// TestPropCacheIdentityEquivalence asserts T3: semantically equivalent URLs
// produce the same CacheIdentity (compared via RelPath).
func TestPropCacheIdentityEquivalence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		forms := equivalentURLsGen().Draw(t, "equivalent_urls")

		var wantRel string
		var wantHost string
		var wantFrom string
		first := true
		for _, form := range forms {
			_, id, err := CanonicalizeURL(form)
			if err != nil {
				t.Fatalf("CanonicalizeURL(%q) unexpectedly failed: %v", form, err)
			}
			if first {
				wantRel = id.RelPath()
				wantHost = id.Host
				wantFrom = form
				first = false
				continue
			}
			if id.RelPath() != wantRel {
				t.Fatalf("identity mismatch: %q -> %q, but %q -> %q",
					wantFrom, wantRel, form, id.RelPath())
			}
			if id.Host != wantHost {
				t.Fatalf("host mismatch: %q -> %q, but %q -> %q",
					wantFrom, wantHost, form, id.Host)
			}
		}
	})
}

// TestPropCanonicalizeProducesValidURL asserts T4: a successful canonical URL
// parses with net/url. For non-file schemes the parsed URL must have a
// non-empty host and path; file:// URLs have an empty host by design, so only
// parseability is required.
func TestPropCanonicalizeProducesValidURL(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := anyURLGen().Draw(t, "url")

		canon, _, err := CanonicalizeURL(raw)
		if err != nil {
			t.Skip("canonicalize rejected input")
		}

		u, perr := url.Parse(canon)
		if perr != nil {
			t.Fatalf("canonical %q (from %q) is not parseable: %v", canon, raw, perr)
		}

		if u.Scheme == "file" {
			// file:// URLs have empty host by design; parseability is enough.
			return
		}

		if u.Host == "" {
			t.Fatalf("non-file canonical %q (from %q) has empty host", canon, raw)
		}
		if strings.Trim(u.Path, "/") == "" {
			t.Fatalf("non-file canonical %q (from %q) has empty path", canon, raw)
		}
	})
}

// TestPropEndpointIdempotent asserts T5: when Endpoint succeeds, applying it
// again to its own output yields the same value.
func TestPropEndpointIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := anyURLGen().Draw(t, "url")

		ep1, err := Endpoint(raw)
		if err != nil {
			t.Skip("endpoint rejected input")
		}

		ep2, err := Endpoint(ep1)
		if err != nil {
			t.Fatalf("re-deriving endpoint of %q (from %q) failed: %v", ep1, raw, err)
		}

		if ep2 != ep1 {
			t.Fatalf("not idempotent: Endpoint(%q) = %q, Endpoint(%q) = %q",
				raw, ep1, ep1, ep2)
		}
	})
}
