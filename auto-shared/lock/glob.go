package lock

import (
	"path"
	"strings"
)

// matchGlob reports whether the slash-separated, repo-relative name matches
// pattern. Segments are matched one at a time with path.Match, so `*`, `?` and
// `[...]` work within a segment and never cross a `/`; a segment that is
// exactly `**` matches zero or more whole segments, which is how a group's
// `db/schema/**` covers every file below db/schema at any depth. A `**` that
// is not a whole segment (`foo**`) is treated as a plain `*`.
//
// auto-shared is stdlib-only by design (see rpc/layering_test.go), so this is
// a small matcher rather than a globbing dependency.
func matchGlob(pattern, name string) (bool, error) {
	if _, err := path.Match(strings.ReplaceAll(pattern, "**", "*"), ""); err != nil {
		return false, err
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/")), nil
}

func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// Zero or more segments: try every suffix of segs against the rest
			// of the pattern.
			rest := pat[1:]
			for i := 0; i <= len(segs); i++ {
				if matchSegments(rest, segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		ok, err := path.Match(strings.ReplaceAll(pat[0], "**", "*"), segs[0])
		if err != nil || !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// validGlob reports whether pattern is well-formed: every segment must be a
// valid path.Match pattern (unbalanced `[` is the usual failure).
func validGlob(pattern string) bool {
	_, err := matchGlob(pattern, "")
	return err == nil
}
