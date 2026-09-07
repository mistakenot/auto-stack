package sync

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/mistakenot/auto-skill/internal/cache"
	"github.com/mistakenot/auto-skill/internal/plugin"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/trace"
)

// PluginPlan is phase A's decision for one installed Agent Plugin. A plugin
// floats as a unit: every member skill shares TargetCommit, and when the commit
// moves the membership is re-derived from the manifest at that commit, so
// Members is the post-update member set (name → repo-relative subpath) and
// Removed lists locked members the new commit no longer ships.
type PluginPlan struct {
	Name         string            `json:"name"`
	Repo         string            `json:"repo"`
	URL          string            `json:"url"`
	Root         string            `json:"root"`
	VersionSpec  string            `json:"version_spec"`
	LockSpec     string            `json:"lock_spec"`
	LockedCommit string            `json:"locked_commit"`
	TargetCommit string            `json:"target_commit"`
	Action       Action            `json:"action"`
	LockRewrite  bool              `json:"lock_rewrite,omitempty"`
	Members      map[string]string `json:"members,omitempty"`
	Added        []string          `json:"added,omitempty"`
	Removed      []string          `json:"removed,omitempty"`
	Message      string            `json:"message,omitempty"`
	Err          error             `json:"-"`
}

// groupedPlugin bundles one locked plugin with its member lock entries.
type groupedPlugin struct {
	name    string
	entry   skill.LockEntry
	members []groupedSkill
}

// declaredPluginVersion returns the skills.yaml-declared version for a plugin,
// falling back to the shared default (empty means "no declared intent").
func declaredPluginVersion(syaml *skill.SkillsYAML, name string) string {
	if syaml == nil {
		return ""
	}
	if pc, ok := syaml.Plugins[name]; ok && strings.TrimSpace(pc.Version) != "" {
		return strings.TrimSpace(pc.Version)
	}
	return strings.TrimSpace(syaml.Shared.Version)
}

// declaredIntent returns the declared version for a grouped skill: a plugin
// member inherits its plugin's intent, a standalone skill uses its own entry.
func declaredIntent(syaml *skill.SkillsYAML, s *groupedSkill) string {
	if s.entry.Plugin != "" {
		return declaredPluginVersion(syaml, s.entry.Plugin)
	}
	return declaredVersion(syaml, s.name)
}

// expandScope widens a name scope so a plugin is always planned whole: naming
// the plugin selects every member, and naming any member selects its plugin and
// every sibling. Names that match nothing are kept (they may be authored).
func expandScope(lock *skill.Lock, scope map[string]bool) map[string]bool {
	if len(scope) == 0 || lock == nil {
		return scope
	}
	wanted := map[string]bool{}
	for name := range lock.Plugins {
		if scope[strings.ToLower(name)] {
			wanted[name] = true
		}
	}
	for name := range lock.Skills {
		if p := lock.Skills[name].Plugin; p != "" && scope[strings.ToLower(name)] {
			wanted[p] = true
		}
	}
	if len(wanted) == 0 {
		return scope
	}
	out := make(map[string]bool, len(scope))
	for k := range scope {
		out[k] = true
	}
	for name := range lock.Skills {
		if p := lock.Skills[name].Plugin; p != "" && wanted[p] {
			out[strings.ToLower(name)] = true
		}
	}
	for p := range wanted {
		out[strings.ToLower(p)] = true
	}
	return out
}

// ExpandTargets widens a --target / update name list so plugins are handled as
// a unit (see expandScope). Best-effort: an unreadable lock leaves the list
// unchanged so the normal pipeline reports the problem.
func ExpandTargets(env skill.Env, targets []string) []string {
	if len(targets) == 0 {
		return targets
	}
	lock, err := loadLock(env)
	if err != nil {
		return targets
	}
	scope := expandScope(lock, normalizeNames(targets))
	out := make([]string, 0, len(scope))
	for name := range scope {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// planPluginOnline decides a plugin (and appends each member's SkillPlan) with
// the cache repo opened. Floating resolves the plugin's spec once, then
// re-derives membership from the manifest at the new commit.
func planPluginOnline(repo *cache.Repo, syaml *skill.SkillsYAML, mode planMode, g *repoGroup, p *groupedPlugin, resolved map[string]resolveOutcome, plan *Plan, tr *trace.Logger) {
	lockSpec := p.entry.VersionSpec
	intent := declaredPluginVersion(syaml, p.name)
	if intent == "" {
		intent = lockSpec
	}
	pp := PluginPlan{
		Name:         p.name,
		Repo:         g.key,
		URL:          g.url,
		Root:         p.entry.Subpath,
		VersionSpec:  lockSpec,
		LockSpec:     lockSpec,
		LockedCommit: p.entry.Commit,
		TargetCommit: p.entry.Commit,
	}

	spec := lockSpec
	float, forced := false, false
	switch {
	case intent != lockSpec:
		if !mode.floatRefs {
			pp.Action = ActionIntentChanged
			pp.Message = fmt.Sprintf("plugin intent changed (%s → %s) — run: auto skill update %s", lockSpec, intent, p.name)
			plan.Plugins = append(plan.Plugins, pp)
			for i := range p.members {
				sp := memberPlan(g, &p.members[i], lockSpec)
				sp.Action = ActionIntentChanged
				sp.Message = pp.Message
				plan.Skills = append(plan.Skills, sp)
				traceSkillPlan(tr, sp)
			}
			return
		}
		float, forced, spec = true, true, intent
	default:
		switch classifySpec(lockSpec) {
		case kindFloat:
			float = mode.floatRefs
		case kindTag:
			float = mode.floatTags
		}
	}

	if !float {
		pp.Action = ActionUpToDate
		for i := range p.members {
			sp := decidePinned(repo, memberPlan(g, &p.members[i], lockSpec))
			if !sp.Cached {
				pp.Action = ActionMaterialize
			}
			plan.Skills = append(plan.Skills, sp)
			traceSkillPlan(tr, sp)
		}
		plan.Plugins = append(plan.Plugins, pp)
		return
	}

	newSha, err := resolveLatestMemo(repo, refForSpec(spec), resolved)
	if err != nil {
		failPlugin(plan, g, p, &pp, fmt.Errorf("re-resolve %s for plugin %s: %w", spec, p.name, err), tr)
		return
	}
	pp.VersionSpec = spec
	pp.TargetCommit = newSha
	if classifySpec(spec) == kindTag && newSha != pp.LockedCommit && pp.LockedCommit != "" {
		trace.Logf(tr, "sync plan plugin=%s tag %q moved %s → %s", p.name, strings.TrimPrefix(spec, "tag:"), short(pp.LockedCommit), short(newSha))
	}

	if newSha == pp.LockedCommit && !forced {
		pp.Action = ActionUpToDate
		for i := range p.members {
			sp := decidePinned(repo, memberPlan(g, &p.members[i], lockSpec))
			plan.Skills = append(plan.Skills, sp)
			traceSkillPlan(tr, sp)
		}
		plan.Plugins = append(plan.Plugins, pp)
		return
	}

	// The commit moved (or the spec changed): re-derive membership at newSha.
	members, err := pluginMembersAt(repo, newSha, p.name, p.entry.Subpath)
	if err != nil {
		failPlugin(plan, g, p, &pp, err, tr)
		return
	}
	pp.Members = members
	pp.LockRewrite = mode.rewrite
	pp.Action = ActionResolve
	present, _ := repo.CommitPresent(newSha)
	if !present {
		pp.Action = ActionMaterialize
	}

	old := make(map[string]*groupedSkill, len(p.members))
	for i := range p.members {
		old[p.members[i].name] = &p.members[i]
	}
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sp := SkillPlan{
			Name:         name,
			Repo:         g.key,
			URL:          g.url,
			Subpath:      members[name],
			VersionSpec:  spec,
			TargetCommit: newSha,
			LockRewrite:  mode.rewrite,
			Cached:       present,
		}
		if prev, ok := old[name]; ok {
			sp.LockSpec = prev.entry.VersionSpec
			sp.LockedCommit = prev.entry.Commit
		} else {
			pp.Added = append(pp.Added, name)
		}
		if present {
			sp.Action = ActionResolve
		} else {
			sp.Action = ActionMaterialize
		}
		plan.Skills = append(plan.Skills, sp)
		traceSkillPlan(tr, sp)
	}
	for i := range p.members {
		if _, still := members[p.members[i].name]; !still {
			pp.Removed = append(pp.Removed, p.members[i].name)
		}
	}
	sort.Strings(pp.Removed)
	if len(pp.Added) > 0 || len(pp.Removed) > 0 {
		pp.Message = fmt.Sprintf("membership changed: +%d −%d", len(pp.Added), len(pp.Removed))
	}
	plan.Plugins = append(plan.Plugins, pp)
}

// planPluginOffline decides a plugin under sync --check (no network).
func planPluginOffline(repo *cache.Repo, syaml *skill.SkillsYAML, g *repoGroup, p *groupedPlugin, plan *Plan, tr *trace.Logger) {
	pp := PluginPlan{
		Name:         p.name,
		Repo:         g.key,
		URL:          g.url,
		Root:         p.entry.Subpath,
		VersionSpec:  p.entry.VersionSpec,
		LockSpec:     p.entry.VersionSpec,
		LockedCommit: p.entry.Commit,
		TargetCommit: p.entry.Commit,
		Action:       ActionUpToDate,
	}
	for i := range p.members {
		sp := planOfflineSkill(repo, syaml, &p.members[i])
		if sp.Err != nil {
			plan.Errors = append(plan.Errors, sp.Err)
		}
		if sp.Action != ActionUpToDate {
			pp.Action = sp.Action
			pp.Message = sp.Message
		}
		plan.Skills = append(plan.Skills, sp)
		traceSkillPlan(tr, sp)
	}
	plan.Plugins = append(plan.Plugins, pp)
}

// memberPlan seeds a member's SkillPlan from its lock entry.
func memberPlan(g *repoGroup, m *groupedSkill, spec string) SkillPlan {
	return SkillPlan{
		Name:         m.name,
		Repo:         g.key,
		URL:          g.url,
		Subpath:      m.entry.Subpath,
		VersionSpec:  spec,
		LockSpec:     m.entry.VersionSpec,
		LockedCommit: m.entry.Commit,
		TargetCommit: m.entry.Commit,
	}
}

// failPlugin records one planning error for the plugin and marks every locked
// member unavailable so nothing is rendered or pruned for it this run.
func failPlugin(plan *Plan, g *repoGroup, p *groupedPlugin, pp *PluginPlan, err error, tr *trace.Logger) {
	pp.Action = ActionUnavailable
	pp.Err = err
	pp.Message = err.Error()
	plan.Errors = append(plan.Errors, err)
	plan.Plugins = append(plan.Plugins, *pp)
	for i := range p.members {
		sp := memberPlan(g, &p.members[i], p.entry.VersionSpec)
		sp.Action = ActionUnavailable
		sp.Err = err
		sp.Message = err.Error()
		plan.Skills = append(plan.Skills, sp)
		traceSkillPlan(tr, sp)
	}
}

// pluginMembersAt re-derives a plugin's membership (name → repo-relative
// subpath) from its manifest and skills/ at commit sha. A missing or invalid
// manifest, or a manifest whose name no longer matches the locked plugin, is an
// unavailable-upstream error with a remediation hint.
func pluginMembersAt(repo *cache.Repo, sha, name, root string) (map[string]string, error) {
	tree := plugin.FuncTree{
		FilesFn:    func() ([]string, error) { return repo.ListFiles(sha) },
		ReadFileFn: func(rel string) ([]byte, error) { return repo.ReadFile(sha, rel) },
	}
	m, skills, _, err := plugin.DiscoverSkillsTree(tree, root)
	if err != nil {
		return nil, fmt.Errorf("plugin %s at commit %s: %w; if it moved upstream, remove it and re-add with the new --plugin path", name, short(sha), err)
	}
	if m.Name != name {
		return nil, fmt.Errorf("plugin %s at commit %s: manifest at %q now declares name %q; remove the plugin and re-add it", name, short(sha), displayPluginRoot(root), m.Name)
	}
	out := make(map[string]string, len(skills))
	for _, s := range skills {
		out[s.Name] = path.Join(root, s.Subpath)
	}
	return out, nil
}

func displayPluginRoot(root string) string {
	if root == "" {
		return "."
	}
	return root
}

// applyPluginPlans rewrites the lock for every plugin plan marked LockRewrite:
// the plugin entry advances to its target commit and the member entries are
// upserted (new members inserted, dropped members deleted).
func applyPluginPlans(lock *skill.Lock, plan *Plan) {
	for i := range plan.Plugins {
		pp := plan.Plugins[i]
		if !pp.LockRewrite {
			continue
		}
		entry, ok := lock.Plugins[pp.Name]
		if !ok {
			continue
		}
		entry.Commit = pp.TargetCommit
		entry.Ref = pp.TargetCommit
		entry.VersionSpec = pp.VersionSpec
		lock.Plugins[pp.Name] = entry
		for _, name := range pp.Removed {
			if e, ok := lock.Skills[name]; ok && e.Plugin == pp.Name {
				delete(lock.Skills, name)
			}
		}
		for name, sub := range pp.Members {
			lock.Skills[name] = skill.LockEntry{
				Source:      entry.Source,
				URL:         entry.URL,
				VersionSpec: pp.VersionSpec,
				Ref:         pp.TargetCommit,
				Commit:      pp.TargetCommit,
				Subpath:     sub,
				Private:     entry.Private,
				Local:       entry.Local,
				State:       "resolved",
				Plugin:      pp.Name,
			}
		}
	}
}
