package sync

import "fmt"

// RenamedUpstreamError reports that a vendored skill's locked subpath no longer
// resolves in its (floated/resolved) upstream commit — the path was renamed or
// removed upstream. It carries the structured fields so the CLI and `doctor`
// (phase 6) can present a clear remediation; downstream code recovers it with
// errors.As. The stale skill's target orphan is left for the next sync's normal
// prune — this type only reports, it triggers no deletion.
type RenamedUpstreamError struct {
	Name    string // skill name
	Subpath string // locked subpath that no longer resolves
	Commit  string // resolved commit the subpath was looked up in
}

// Error renders the remediation message: re-add the skill under its new name or
// drop the stale vendored entry.
func (e *RenamedUpstreamError) Error() string {
	return fmt.Sprintf(
		"%s not found at its locked path %q — renamed or removed upstream? "+
			"Re-add it under its new name (auto skill add <url>) or drop the stale entry "+
			"(auto skill remove %s --vendored).",
		e.Name, e.Subpath, e.Name,
	)
}
