package bus

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strings"

	"github.com/mistakenot/auto-shared/config"
)

// DeriveDocChanged inspects an agent.tool.post event and emits doc.changed
// events for each path whose cleaned Rel satisfies docs/**/*.md. Only paths
// in a registered project (looked up via reg) produce derived events;
// unregistered projects derive nothing.
func DeriveDocChanged(ev Event, reg config.ProjectsConfig) []Event {
	if ev.Type != "agent.tool.post" {
		return nil
	}

	// The registry is the authority — derive only when the project is known.
	if ev.Project == "" || reg.FindProjectByID(ev.Project) == nil {
		return nil
	}

	tp, err := DecodeData[ToolPost](ev)
	if err != nil {
		return nil
	}

	var derived []Event
	for _, p := range tp.Paths {
		rel := cleanRel(p.Rel)
		if rel == "" {
			continue
		}
		if !isDocPath(rel) {
			continue
		}

		dc := DocChanged{
			Project:  ev.Project,
			Path:     rel,
			AbsPath:  p.Abs,
			Worktree: ev.Worktree,
			Branch:   ev.Branch,
		}
		derived = append(derived, newDerived(ev, "doc.changed", rel, dc))
	}
	return derived
}

// cleanRel cleans rel, rejects paths that escape the root (contain ".."),
// and returns the cleaned path or "" if invalid.
func cleanRel(rel string) string {
	if rel == "" {
		return ""
	}
	// Use path.Clean (forward slashes) for repo-relative paths.
	cleaned := path.Clean(rel)
	// Reject any path that walks above the root.
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/..") {
		return ""
	}
	// Strip leading "./" or "/" for consistency.
	cleaned = strings.TrimPrefix(cleaned, "./")
	cleaned = strings.TrimPrefix(cleaned, "/")
	return cleaned
}

// isDocPath returns true when rel is a markdown or html file under docs/.
func isDocPath(rel string) bool {
	return strings.HasPrefix(rel, "docs/") && (strings.HasSuffix(rel, ".md") || strings.HasSuffix(rel, ".html"))
}

// newDerived creates a derived event carrying the same provenance as the source
// event but with a new type and data payload. The derived id is deterministic
// (see deterministicID) so that relayed and locally-derived copies of the same
// source event share an id and can be deduped by a consumer.
func newDerived(src Event, typ, rel string, data any) Event {
	ev, _ := NewEvent(typ, "auto/bus/derive", data)
	// Replace the random id minted by NewEvent with a deterministic one keyed
	// on the source event id, derived type, and derived path.
	ev.ID = deterministicID(src.ID, typ, rel)
	// Carry provenance from the source event.
	ev.Host = src.Host
	ev.Project = src.Project
	ev.Session = src.Session
	ev.Remote = src.Remote
	ev.Branch = src.Branch
	ev.Worktree = src.Worktree
	ev.Commit = src.Commit
	return ev
}

// deterministicID derives a stable 16-hex-character event id from the source
// event id, derived type, and derived path. The same (srcID, typ, rel) triple
// always yields the same id, so relayed and locally-derived copies of one event
// collide on id and can be deduped. The 16-hex form matches the random id format
// minted by newID (bus-spec §2.1): sha256 truncated to the first 8 bytes.
func deterministicID(srcID, typ, rel string) string {
	sum := sha256.Sum256([]byte(srcID + ":" + typ + ":" + rel))
	return hex.EncodeToString(sum[:8])
}
