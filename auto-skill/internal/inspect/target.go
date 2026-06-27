package inspect

import (
	"github.com/mistakenot/auto-skill/internal/skill"
)

// TargetList returns the configured output targets (default claude, agents when
// skills.yaml declares none), each with its resolved on-disk skills directory and
// the count of skills the manifest records it as managing. The managed count is 0
// when no manifest exists yet.
func TargetList(env skill.Env) ([]Target, error) {
	cfg, err := loadProjectConfig(env)
	if err != nil {
		return nil, err
	}
	manifest, err := loadManifest(env)
	if err != nil {
		return nil, err
	}

	targets := resolveTargets(env, cfg)
	out := make([]Target, 0, len(targets))
	for _, t := range targets {
		count := 0
		if manifest != nil {
			if mt, ok := manifest.Targets[t.Name]; ok {
				count = len(mt.ManagedSkills)
			}
		}
		out = append(out, Target{Name: t.Name, Path: t.Dir, ManagedCount: count})
	}
	return out, nil
}
