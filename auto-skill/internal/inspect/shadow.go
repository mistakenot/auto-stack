package inspect

import (
	"sort"

	"github.com/mistakenot/auto-skill/internal/skill"
)

// ShadowView is one authored skill whose name also appears in the lock. sync
// renders the authored copy and never the vendored one, so the lock entry is
// dead weight: `add` looked like it succeeded, `update` keeps fetching it, but
// nothing from the upstream source ever lands in a target.
type ShadowView struct {
	Name         string `json:"name"`
	AuthoredPath string `json:"authored_path"`
	Source       string `json:"source,omitempty"` // lock URL of the hidden vendored copy
	Commit       string `json:"commit,omitempty"`
	Plugin       string `json:"plugin,omitempty"`
}

// Shadowed returns every authored ./skills entry that shadows a same-named lock
// entry, sorted by name. Offline: it reads ./skills and lock.json only. A
// malformed authored skill is skipped (skill.List reports it separately) and an
// absent lock yields an empty result.
func Shadowed(env skill.Env) ([]ShadowView, error) {
	authored, _, err := skill.List(env)
	if err != nil {
		return nil, err
	}
	lock, err := loadLock(env)
	if err != nil {
		return nil, err
	}
	out := []ShadowView{}
	if lock == nil {
		return out, nil
	}
	for _, s := range authored {
		entry, ok := lock.Skills[s.Name]
		if !ok {
			continue
		}
		out = append(out, ShadowView{
			Name:         s.Name,
			AuthoredPath: s.Path,
			Source:       entry.URL,
			Commit:       entry.Commit,
			Plugin:       entry.Plugin,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
