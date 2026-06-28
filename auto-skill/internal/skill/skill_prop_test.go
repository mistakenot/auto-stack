package skill

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
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

// distinctIdentity is the key function for SliceOfNDistinct over strings.
func distinctIdentity(s string) string { return s }

// replacementMapGen builds a ReplacementMap with valid var names (replacementVarRE)
// mapped to scalar string yaml.Node values. The Tag is fixed to "!!str" so that
// values which would otherwise parse as bool/int/null (e.g. "true", "123") are
// quoted consistently on every marshal — keeping the canonical round-trip stable.
func replacementMapGen() *rapid.Generator[ReplacementMap] {
	varName := rapid.StringMatching(`[A-Za-z_][A-Za-z0-9_]*`)
	scalarVal := rapid.StringMatching(`[A-Za-z][A-Za-z0-9 ._-]{0,19}`)
	return rapid.Custom(func(t *rapid.T) ReplacementMap {
		names := rapid.SliceOfNDistinct(varName, 0, 4, distinctIdentity).Draw(t, "rep_names")
		m := make(ReplacementMap, len(names))
		for _, n := range names {
			m[n] = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: scalarVal.Draw(t, "rep_val")}
		}
		return m
	})
}

// sharedConfigGen builds a SharedConfig with a valid version spec and replacements.
func sharedConfigGen() *rapid.Generator[SharedConfig] {
	return rapid.Custom(func(t *rapid.T) SharedConfig {
		return SharedConfig{
			Version:      versionSpecGen().Draw(t, "shared_version"),
			Replacements: replacementMapGen().Draw(t, "shared_reps"),
		}
	})
}

// skillConfigGen builds a SkillConfig with a valid version spec and replacements.
func skillConfigGen() *rapid.Generator[SkillConfig] {
	return rapid.Custom(func(t *rapid.T) SkillConfig {
		return SkillConfig{
			Version:      versionSpecGen().Draw(t, "skill_version"),
			Replacements: replacementMapGen().Draw(t, "skill_reps"),
		}
	})
}

// skillsYAMLGen builds a SkillsYAML in the modern form (named maps, never the
// legacy empty-sequence replacements form) so the canonical round-trip holds.
func skillsYAMLGen() *rapid.Generator[SkillsYAML] {
	target := rapid.StringMatching(`\.[a-z]+`)
	host := rapid.StringMatching(`[a-z0-9]+\.(com|org|io)`)
	return rapid.Custom(func(t *rapid.T) SkillsYAML {
		names := rapid.SliceOfNDistinct(skillNameGen(), 0, 4, distinctIdentity).Draw(t, "skill_names")
		skills := make(map[string]SkillConfig, len(names))
		for _, n := range names {
			skills[n] = skillConfigGen().Draw(t, "skill_cfg")
		}
		return SkillsYAML{
			AutoUpdate:    rapid.Bool().Draw(t, "auto_update"),
			Targets:       rapid.SliceOfNDistinct(target, 0, 4, distinctIdentity).Draw(t, "targets"),
			CommitTargets: rapid.Bool().Draw(t, "commit_targets"),
			TrustedHosts:  rapid.SliceOfNDistinct(host, 0, 4, distinctIdentity).Draw(t, "trusted_hosts"),
			Shared:        sharedConfigGen().Draw(t, "shared"),
			Skills:        skills,
		}
	})
}

// manifestFileRefGen builds a ManifestFileRef with a hex content hash.
func manifestFileRefGen() *rapid.Generator[ManifestFileRef] {
	pathSeg := rapid.StringMatching(`[a-z0-9]+(-[a-z0-9]+)*`)
	heading := rapid.StringMatching(`[A-Za-z ]{0,20}`)
	return rapid.Custom(func(t *rapid.T) ManifestFileRef {
		return ManifestFileRef{
			Path:           pathSeg.Draw(t, "fr_path") + ".md",
			ContentHash:    commitHexGen().Draw(t, "fr_hash"),
			MatchedHeading: heading.Draw(t, "fr_heading"),
		}
	})
}

// manifestSkillGen builds a ManifestSkill with hex hashes and replacement values.
func manifestSkillGen() *rapid.Generator[ManifestSkill] {
	varName := rapid.StringMatching(`[A-Za-z_][A-Za-z0-9_]*`)
	val := rapid.StringMatching(`[A-Za-z0-9 ._-]{0,20}`)
	return rapid.Custom(func(t *rapid.T) ManifestSkill {
		repNames := rapid.SliceOfNDistinct(varName, 0, 3, distinctIdentity).Draw(t, "ms_rep_names")
		reps := make(map[string]string, len(repNames))
		for _, n := range repNames {
			reps[n] = val.Draw(t, "ms_rep_val")
		}
		nRefs := rapid.IntRange(0, 3).Draw(t, "ms_nrefs")
		refs := make([]ManifestFileRef, 0, nRefs)
		for range nRefs {
			refs = append(refs, manifestFileRefGen().Draw(t, "ms_ref"))
		}
		return ManifestSkill{
			TemplateHash:  commitHexGen().Draw(t, "ms_thash"),
			Replacements:  reps,
			FileRefs:      refs,
			SkillVersion:  commitHexGen().Draw(t, "ms_sver"),
			RenderVersion: "r1",
		}
	})
}

// manifestGen builds a Manifest whose targets only reference skills present in
// the skills map (so it is structurally valid as well as round-trippable).
func manifestGen() *rapid.Generator[Manifest] {
	targetName := rapid.StringMatching(`\.[a-z]+`)
	return rapid.Custom(func(t *rapid.T) Manifest {
		skillNames := rapid.SliceOfNDistinct(skillNameGen(), 0, 4, distinctIdentity).Draw(t, "m_skill_names")
		skills := make(map[string]ManifestSkill, len(skillNames))
		for _, n := range skillNames {
			skills[n] = manifestSkillGen().Draw(t, "m_skill")
		}
		targetNames := rapid.SliceOfNDistinct(targetName, 0, 3, distinctIdentity).Draw(t, "m_target_names")
		targets := make(map[string]ManifestTarget, len(targetNames))
		for _, tn := range targetNames {
			managed := make(map[string]string)
			for _, sn := range skillNames {
				if rapid.Bool().Draw(t, "m_managed_include") {
					managed[sn] = commitHexGen().Draw(t, "m_managed_ver")
				}
			}
			targets[tn] = ManifestTarget{ManagedSkills: managed}
		}
		return Manifest{
			Version:       rapid.IntRange(0, 5).Draw(t, "m_version"),
			RenderVersion: "r1",
			Skills:        skills,
			Targets:       targets,
		}
	})
}

// invalidLockGen builds a Lock seeded from a valid lock, then injects exactly one
// deliberately invalid field so ValidateLock must report at least one error.
func invalidLockGen() *rapid.Generator[Lock] {
	return rapid.Custom(func(t *rapid.T) Lock {
		lock := lockGen().Draw(t, "base_lock")
		if lock.Skills == nil {
			lock.Skills = map[string]LockEntry{}
		}
		switch rapid.SampledFrom([]string{"badname", "badstate", "missing", "creds"}).Draw(t, "defect") {
		case "badname":
			// Uppercase-leading key never matches skillNameRE and cannot collide
			// with the lowercase keys lockGen produced.
			badName := rapid.StringMatching(`[A-Z][A-Za-z0-9]*`).Draw(t, "bad_name")
			lock.Skills[badName] = LockEntry{State: "unresolved"}
		case "badstate":
			state := rapid.SampledFrom([]string{"pending", "active", "broken", "stale", "RESOLVED", ""}).Draw(t, "bad_state")
			lock.Skills["bad-state-skill"] = LockEntry{State: state}
		case "missing":
			// Resolved entry missing the required source/url/commit fields.
			lock.Skills["missing-fields-skill"] = LockEntry{State: "resolved"}
		default: // creds
			lock.Skills["creds-skill"] = LockEntry{
				Source: "github.com/owner/repo",
				URL:    "https://user:pass@github.com/owner/repo",
				Commit: "abcdef1",
				State:  "resolved",
			}
		}
		return lock
	})
}

// invalidVersionSpecGen builds malformed version specs across every rejection
// branch of ValidateVersionSpec.
func invalidVersionSpecGen() *rapid.Generator[string] {
	unknownPrefix := rapid.SampledFrom([]string{"foo", "bar", "ref", "rev", "sha", "version", "ver", "head"})
	blank := rapid.SampledFrom([]string{"", "   "})
	return rapid.Custom(func(t *rapid.T) string {
		switch rapid.SampledFrom([]string{"empty", "unknown", "branch_empty", "tag_empty", "commit_nonhex", "commit_short", "commit_long"}).Draw(t, "bad_form") {
		case "empty":
			return blank.Draw(t, "empty_v")
		case "unknown":
			return unknownPrefix.Draw(t, "prefix") + ":" + rapid.StringMatching(`[a-z0-9]{0,8}`).Draw(t, "payload")
		case "branch_empty":
			return "branch:" + blank.Draw(t, "branch_blank")
		case "tag_empty":
			return "tag:" + blank.Draw(t, "tag_blank")
		case "commit_nonhex":
			return "commit:" + rapid.StringMatching(`[g-z]{7,40}`).Draw(t, "nonhex")
		case "commit_short":
			return "commit:" + rapid.StringMatching(`[0-9a-f]{1,6}`).Draw(t, "short")
		default: // commit_long
			return "commit:" + rapid.StringMatching(`[0-9a-f]{41,60}`).Draw(t, "long")
		}
	})
}

// TestPropSkillsYAMLRoundTrip (S2) asserts that marshalling a generated SkillsYAML,
// parsing it back, and marshalling again yields byte-identical YAML — a canonical-
// form comparison that sidesteps yaml.Node metadata differences while still
// catching semantic round-trip losses.
func TestPropSkillsYAMLRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := skillsYAMLGen().Draw(t, "skills_yaml")

		data1, err := yaml.Marshal(original)
		if err != nil {
			t.Fatalf("marshal skills.yaml: %v", err)
		}

		parsed, err := ParseSkillsYAML(data1)
		if err != nil {
			t.Fatalf("ParseSkillsYAML(%s): %v", data1, err)
		}

		data2, err := yaml.Marshal(parsed)
		if err != nil {
			t.Fatalf("re-marshal skills.yaml: %v", err)
		}

		if !bytes.Equal(data1, data2) {
			t.Fatalf("canonical-form mismatch:\n  first  = %s\n  second = %s", data1, data2)
		}
	})
}

// TestPropValidateLockConsistency (S3) asserts a Lock whose every field satisfies
// its constraint validates cleanly, and a Lock with one deliberately invalid
// field reports at least one error.
func TestPropValidateLockConsistency(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			lock := lockGen().Draw(t, "lock")
			if errs := ValidateLock(&lock); len(errs) != 0 {
				t.Fatalf("ValidateLock on all-valid lock returned %d errors: %+v\n  lock = %#v", len(errs), errs, lock)
			}
		})
	})

	t.Run("invalid", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			lock := invalidLockGen().Draw(t, "lock")
			if errs := ValidateLock(&lock); len(errs) == 0 {
				t.Fatalf("ValidateLock on lock with an invalid field returned no errors\n  lock = %#v", lock)
			}
		})
	})
}

// TestPropVersionSpecBoundary (S4) asserts every well-formed spec is accepted and
// every malformed spec is rejected.
func TestPropVersionSpecBoundary(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			spec := versionSpecGen().Draw(t, "spec")
			if ve := ValidateVersionSpec(spec); ve != nil {
				t.Fatalf("ValidateVersionSpec(%q) = %+v, want nil", spec, ve)
			}
		})
	})

	t.Run("invalid", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			spec := invalidVersionSpecGen().Draw(t, "spec")
			if ve := ValidateVersionSpec(spec); ve == nil {
				t.Fatalf("ValidateVersionSpec(%q) = nil, want error", spec)
			}
		})
	})
}

// TestPropValidateLockIdempotent (S5) asserts that validating a valid Lock, then
// re-serializing and re-validating, yields the same (error-free) result.
func TestPropValidateLockIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := lockGen().Draw(t, "lock")

		errs1 := ValidateLock(&original)
		if len(errs1) != 0 {
			t.Fatalf("first ValidateLock returned %d errors: %+v", len(errs1), errs1)
		}

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal lock: %v", err)
		}
		parsed, err := ParseLock(data)
		if err != nil {
			t.Fatalf("ParseLock(%s): %v", data, err)
		}

		errs2 := ValidateLock(parsed)
		if len(errs2) != 0 {
			t.Fatalf("second ValidateLock returned %d errors: %+v", len(errs2), errs2)
		}
	})
}

// TestPropManifestRoundTrip (S6) asserts that marshalling a Manifest to JSON and
// parsing it back yields a deeply-equal Manifest.
func TestPropManifestRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := manifestGen().Draw(t, "manifest")

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal manifest: %v", err)
		}

		parsed, err := ParseManifest(data)
		if err != nil {
			t.Fatalf("ParseManifest(%s): %v", data, err)
		}

		if !reflect.DeepEqual(*parsed, original) {
			t.Fatalf("round trip mismatch:\n  original = %#v\n  parsed   = %#v\n  json     = %s",
				original, *parsed, data)
		}
	})
}
