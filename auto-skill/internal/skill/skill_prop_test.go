package skill

import (
	"encoding/json"
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

// skillNameGen constructs names matching skillNameRE: ^[a-z0-9]+(?:-[a-z0-9]+)*$.
func skillNameGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z0-9]+(-[a-z0-9]+)*`)
}

// commitHexGen constructs lowercase hex commit hashes of 7-40 chars (commitHexRE).
func commitHexGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[0-9a-f]{7,40}`)
}

// versionSpecGen constructs valid version specs across the accepted forms.
func versionSpecGen() *rapid.Generator[string] {
	name := rapid.StringMatching(`[A-Za-z0-9._/-]{1,20}`)
	return rapid.Custom(func(t *rapid.T) string {
		switch rapid.SampledFrom([]string{"latest", "branch", "tag", "commit", "bare"}).Draw(t, "spec_form") {
		case "latest":
			return "latest"
		case "branch":
			return "branch:" + name.Draw(t, "branch")
		case "tag":
			return "tag:" + name.Draw(t, "tag")
		case "commit":
			return "commit:" + commitHexGen().Draw(t, "commit_spec")
		default:
			return name.Draw(t, "bare")
		}
	})
}

// lockEntryGen builds a LockEntry from valid component generators.
func lockEntryGen() *rapid.Generator[LockEntry] {
	host := rapid.StringMatching(`[a-z0-9]+\.(com|org|io)`)
	pathSeg := rapid.StringMatching(`[a-z0-9]+(-[a-z0-9]+)*`)
	return rapid.Custom(func(t *rapid.T) LockEntry {
		h := host.Draw(t, "host")
		owner := pathSeg.Draw(t, "owner")
		repo := pathSeg.Draw(t, "repo")
		source := h + "/" + owner + "/" + repo
		return LockEntry{
			Source:      source,
			URL:         "https://" + source,
			VersionSpec: versionSpecGen().Draw(t, "version_spec"),
			Ref:         pathSeg.Draw(t, "ref"),
			Commit:      commitHexGen().Draw(t, "commit"),
			Subpath:     "skills/" + pathSeg.Draw(t, "subpath"),
			Private:     rapid.Bool().Draw(t, "private"),
			Local:       rapid.Bool().Draw(t, "local"),
			State:       rapid.SampledFrom([]string{"resolved", "unresolved"}).Draw(t, "state"),
		}
	})
}

// lockGen builds a Lock with a map of uniquely-named skill entries.
func lockGen() *rapid.Generator[Lock] {
	return rapid.Custom(func(t *rapid.T) Lock {
		names := rapid.SliceOfNDistinct(skillNameGen(), 0, 5, func(s string) string { return s }).Draw(t, "names")
		skills := make(map[string]LockEntry, len(names))
		for _, n := range names {
			skills[n] = lockEntryGen().Draw(t, "entry")
		}
		return Lock{
			Version: rapid.IntRange(0, 10).Draw(t, "version"),
			Skills:  skills,
		}
	})
}

// TestPropLockRoundTrip asserts that marshaling a Lock to JSON and parsing it
// back via ParseLock yields an equal Lock — the (un)marshal pair is lossless.
func TestPropLockRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := lockGen().Draw(t, "lock")

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal lock: %v", err)
		}

		parsed, err := ParseLock(data)
		if err != nil {
			t.Fatalf("ParseLock(%s): %v", data, err)
		}

		// Normalize: an empty Skills map and a nil map are equivalent for our
		// purposes, since json.Marshal of a nil map yields "null"/"{}" depending
		// on tags. Compare via reflect.DeepEqual after the round trip.
		if !reflect.DeepEqual(*parsed, original) {
			t.Fatalf("round trip mismatch:\n  original = %#v\n  parsed   = %#v\n  json     = %s",
				original, *parsed, data)
		}
	})
}
