// Package inspect is the read-only inspection layer for auto-skill. It joins the
// four already-existing project state sources — authored ./skills (skill.List),
// vendored dependency identity (.auto/skills/lock.json), derived render state
// (.auto/skills/manifest.json), and the on-disk rendered target trees — into the
// resource triad (list/describe/get) and the source/target sub-resources.
//
// inspect introduces NO write path and performs NO network fetch: every value it
// returns is read off disk. The stale flag is computed OFFLINE by re-hashing the
// on-disk rendered tree with the exact canonicalization render uses for
// skill_version (render.CanonicalTreeFile + render.ComputeSkillVersion) and
// comparing it to the manifest's expected version — a missing tree reports stale,
// never a silent fetch.
package inspect

// Origin classifies where a skill comes from.
const (
	OriginLocal    = "local"    // authored under ./skills
	OriginVendored = "vendored" // sourced from a remote repo, recorded in the lock
)

// Filter scopes a list run. Local and Vendored are mutually exclusive; the CLI
// boundary rejects the conflict before calling Inspect.
type Filter struct {
	Local    bool
	Vendored bool
}

// SkillView is one row of `list`: ids + metadata + the offline stale flag. It is
// the cheap rung of the resource triad — long fields are truncated at the CLI
// boundary, which prints the exact command to recover the full version.
type SkillView struct {
	Name         string `json:"name"`
	Origin       string `json:"origin"`
	Description  string `json:"description"`
	Path         string `json:"path"`
	SkillVersion string `json:"skill_version,omitempty"`
	// Stale is true when the on-disk rendered tree digest differs from the
	// manifest's expected skill_version (or the tree is absent), false when it
	// matches, and null (nil) when it cannot be known — no manifest entry, e.g. a
	// project that only authored local skills and never ran sync.
	Stale *bool `json:"stale"`
	// Shadowed is set on a local row whose name also appears in the lock: the
	// authored skill wins and the vendored entry is hidden.
	Shadowed bool `json:"shadowed,omitempty"`
}

// Provenance is the `describe` payload: identity + provenance for one skill.
// Source/URL/Ref/Commit/VersionSpec come from the lock (empty for authored-only);
// SkillVersion and Replacements come from the manifest.
type Provenance struct {
	Name         string            `json:"name"`
	Origin       string            `json:"origin"`
	Description  string            `json:"description,omitempty"`
	Path         string            `json:"path,omitempty"`
	Source       string            `json:"source,omitempty"`
	URL          string            `json:"url,omitempty"`
	Ref          string            `json:"ref,omitempty"`
	Commit       string            `json:"commit,omitempty"`
	VersionSpec  string            `json:"version_spec,omitempty"`
	SkillVersion string            `json:"skill_version,omitempty"`
	Replacements map[string]string `json:"replacements,omitempty"`
}

// Source is one upstream dependency, deduped by repo, with the skills it provides.
type Source struct {
	ID     string   `json:"id"`
	URL    string   `json:"url,omitempty"`
	Ref    string   `json:"ref,omitempty"`
	Commit string   `json:"commit,omitempty"`
	Skills []string `json:"skills"`
}

// Target is one configured output target: its style name, resolved on-disk skills
// directory, and the count of skills the manifest records it as managing.
type Target struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	ManagedCount int    `json:"managed_count"`
}
