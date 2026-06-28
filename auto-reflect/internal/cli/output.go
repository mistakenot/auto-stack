package cli

import "maps"

// output.go defines the uniform mutation-result envelope. Every command that
// mutates state returns a result map carrying a predictable top-level `id` (and
// `ids` for multi-id mutations) so consumers can thread an entity id out of one
// command and into the next without knowing each command's bespoke nesting.

// mutationResult returns a command result map with the provided fields plus a
// predictable top-level "id". Existing keys in fields are preserved; "id" is
// added (or overwritten) last so callers compose descriptive payloads (e.g.
// {"created": true, "observation": obs}) and let this helper attach the entity
// id for the envelope.
func mutationResult(id string, fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields)+1)
	maps.Copy(out, fields)
	out["id"] = id
	return out
}

// mutationResultIDs returns a command result map for a mutation that touches
// several entities (e.g. consolidate). It injects both a top-level "ids" slice
// and a top-level "id" set to the first id, so single-id consumers keep working
// while multi-id consumers can read the full set. An empty ids slice yields an
// empty-string "id".
//
//nolint:unused // consolidate's multi-id envelope adopts this in a later 052 phase.
func mutationResultIDs(ids []string, fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields)+2)
	maps.Copy(out, fields)
	var first string
	if len(ids) > 0 {
		first = ids[0]
	}
	out["id"] = first
	out["ids"] = ids
	return out
}
