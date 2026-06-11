package bus

import (
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
		derived = append(derived, newDerived(ev, "doc.changed", dc))
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

// isDocPath returns true when rel is a markdown file under docs/.
func isDocPath(rel string) bool {
	return strings.HasPrefix(rel, "docs/") && strings.HasSuffix(rel, ".md")
}

// newDerived creates a derived event carrying the same provenance as the source
// event but with a new type and data payload.
func newDerived(src Event, typ string, data any) Event {
	ev, _ := NewEvent(typ, "auto/bus/derive", data)
	// Carry provenance from the source event.
	ev.Project = src.Project
	ev.Session = src.Session
	ev.Remote = src.Remote
	ev.Branch = src.Branch
	ev.Worktree = src.Worktree
	ev.Commit = src.Commit
	return ev
}
