package inspect

import (
	"fmt"
	"sort"

	"github.com/mistakenot/auto-skill/internal/skill"
)

// SourceList returns the upstream dependencies recorded in the lock, deduped by
// repo (the lock entry's source), each with the sorted set of skills it provides.
// An un-initialised or authored-only project returns an empty slice (no error):
// list/read commands stay useful with no inputs.
func SourceList(env skill.Env) ([]Source, error) {
	lock, err := loadLock(env)
	if err != nil {
		return nil, err
	}
	if lock == nil {
		return []Source{}, nil
	}

	bySource := map[string]*Source{}
	for name, entry := range lock.Skills {
		id := sourceID(entry)
		if id == "" {
			continue
		}
		s, ok := bySource[id]
		if !ok {
			s = &Source{ID: id, URL: entry.URL, Ref: entry.Ref, Commit: entry.Commit}
			bySource[id] = s
		}
		s.Skills = append(s.Skills, name)
	}

	out := make([]Source, 0, len(bySource))
	for _, s := range bySource {
		sort.Strings(s.Skills)
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// SourceDescribe returns one source by id (its url/ref/commit + the skills it
// provides). An unknown id is a hard error with a remediation hint.
func SourceDescribe(env skill.Env, id string) (Source, error) {
	sources, err := SourceList(env)
	if err != nil {
		return Source{}, err
	}
	for _, s := range sources {
		if s.ID == id {
			return s, nil
		}
	}
	return Source{}, fmt.Errorf("unknown source %q: run auto skill source list to see available sources", id)
}

// sourceID is the dedupe key for a lock entry: its source repo, falling back to
// the raw URL when the source is unset.
func sourceID(entry skill.LockEntry) string {
	if entry.Source != "" {
		return entry.Source
	}
	return entry.URL
}
