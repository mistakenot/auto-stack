package sync

import (
	"github.com/mistakenot/auto-skill/internal/skill"
)

// UpdateResult is the float-then-render engine's decision output. It reports the
// resolved plan and the subset of skills whose target commit moved. This engine
// resolves and decides what *would* change; the actual re-render + write is wired
// in phases 4/5 (and skipped entirely for --check).
type UpdateResult struct {
	Plan    *Plan       `json:"plan"`
	Changed []SkillPlan `json:"changed"`
	Check   bool        `json:"check"`
}

// Update is the stable internal entrypoint for the skills update engine. names
// restricts the run to specific skills (empty = all); check resolves + compares
// upstream without writing anything. Per D-3 the public `auto skill update`
// command name is T6's — this exposes the engine only, with no cobra command.
//
// Phase 5's `sync` auto_update path calls UpdateWith for full Options control.
func Update(env skill.Env, names []string, check bool) (*UpdateResult, error) {
	return UpdateWith(env, Options{Targets: names, Check: check})
}

// UpdateWith is the Options-driven update engine. It reuses phase A's resolution
// with float enabled (latest/branch: advanced to newest upstream; tag: peeled +
// re-resolved on this explicit update, with a force-move warning; a pinned <sha>
// never floats). --check does a git fetch + compares HEAD vs the locked commit
// and writes nothing.
func UpdateWith(env skill.Env, opts Options) (*UpdateResult, error) {
	syaml, err := loadSkillsYAML(env)
	if err != nil {
		return nil, err
	}

	mode := planMode{
		offline:   false, // update always reaches upstream, even under --check
		floatRefs: true,
		floatTags: true,        // explicit update: tags may move (peeled, warned)
		rewrite:   !opts.Check, // --check writes nothing, so never mark rewrites
	}

	plan, err := planRepos(env, opts, syaml, mode)
	if err != nil {
		return nil, err
	}

	result := &UpdateResult{Plan: plan, Check: opts.Check}
	for i := range plan.Skills {
		sp := plan.Skills[i]
		if sp.TargetCommit != "" && sp.TargetCommit != sp.LockedCommit {
			result.Changed = append(result.Changed, sp)
		}
	}
	return result, nil
}
