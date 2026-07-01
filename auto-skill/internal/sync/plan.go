package sync

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mistakenot/auto-skill/internal/cache"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/trace"
	"github.com/mistakenot/auto-skill/internal/transport"
	"github.com/mistakenot/auto-skill/internal/trust"
)

// hexShaRE matches a bare commit SHA (7-40 hex chars) used to detect a pinned
// <sha> version spec that must never float.
var hexShaRE = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// BuildPlan computes phase A for a plain `sync`: read lock + skills.yaml, dedupe
// by repo, skip cache-satisfied commits offline, perform locked materialization
// or (when auto_update) float re-resolution, and reconcile intent drift.
func BuildPlan(env skill.Env, opts Options) (*Plan, error) {
	done := trace.Spanf(opts.Trace, "sync load skills.yaml")
	syaml, err := loadSkillsYAML(env)
	if err != nil {
		done("error=%v", err)
		return nil, err
	}
	done("skills=%d auto_update=%t", len(syaml.Skills), syaml.AutoUpdate)
	autoUpdate := opts.AutoUpdate || syaml.AutoUpdate
	mode := planMode{
		offline:   opts.Check,
		floatRefs: autoUpdate && !opts.Locked && !opts.NoUpdate && !opts.Check,
		floatTags: false,
		rewrite:   autoUpdate && !opts.Locked && !opts.NoUpdate && !opts.Check,
	}
	trace.Logf(opts.Trace, "sync plan mode offline=%t float_refs=%t float_tags=%t rewrite=%t", mode.offline, mode.floatRefs, mode.floatTags, mode.rewrite)
	return planRepos(env, opts, syaml, mode)
}

// planRepos is the shared phase-A planner driven by both Plan (sync) and Update.
func planRepos(env skill.Env, opts Options, syaml *skill.SkillsYAML, mode planMode) (*Plan, error) {
	lock, err := loadLock(env)
	if err != nil {
		return nil, err
	}

	scope := normalizeNames(opts.Targets)
	trace.Logf(opts.Trace, "sync plan loaded lock entries=%d scoped=%t", len(lock.Skills), len(scope) > 0)
	groups, order, planErrs := groupByRepo(lock, scope)
	trace.Logf(opts.Trace, "sync plan grouped repos=%d grouping_errors=%d", len(order), len(planErrs))

	c := cache.NewCache(env.UpstreamCacheDir()).WithTrace(opts.Trace)
	store := trust.NewStore(env.TrustPath())
	gate := &trust.Gate{Store: store}
	gio := trust.GateIO{IsTTY: opts.IsTTY, TrustRequested: opts.TrustRequested}

	plan := &Plan{Errors: planErrs}

	for _, key := range order {
		g := groups[key]
		planRepoGroup(c, gate, gio, syaml, mode, g, plan, opts.Trace)
	}

	sort.Slice(plan.Skills, func(i, j int) bool { return plan.Skills[i].Name < plan.Skills[j].Name })
	sort.Slice(plan.Repos, func(i, j int) bool { return plan.Repos[i].Key < plan.Repos[j].Key })
	return plan, nil
}

// repoGroup bundles all in-scope skills sharing one canonical repo URL.
type repoGroup struct {
	key       string
	url       string
	canonical string
	cacheID   transport.CacheIdentity
	endpoint  string
	skills    []groupedSkill
}

type groupedSkill struct {
	name  string
	entry skill.LockEntry
}

// planRepoGroup resolves one distinct repo and appends its per-skill decisions.
func planRepoGroup(c *cache.Cache, gate *trust.Gate, gio trust.GateIO, syaml *skill.SkillsYAML, mode planMode, g *repoGroup, plan *Plan, tr *trace.Logger) {
	doneRepo := trace.Spanf(tr, "sync plan repo %s skills=%d offline=%t", g.key, len(g.skills), mode.offline)
	defer func() {
		doneRepo("plan_skills=%d repos_to_fetch=%d errors=%d", len(plan.Skills), len(plan.Repos), len(plan.Errors))
	}()
	repoExists := pathExists(repoCachePath(c, g.cacheID))
	trace.Logf(tr, "sync plan repo %s cache_exists=%t", g.key, repoExists)

	// Offline mode (sync --check): never clone or fetch. Only verify already
	// cached commits; anything missing reports an incomplete cache.
	if mode.offline {
		var repo *cache.Repo
		if repoExists {
			if r, err := c.Open(g.cacheID, g.url); err == nil {
				repo = r
			}
		}
		for i := range g.skills {
			sp := planOfflineSkill(repo, syaml, g.skills[i])
			if sp.Err != nil {
				plan.Errors = append(plan.Errors, sp.Err)
			}
			plan.Skills = append(plan.Skills, sp)
			traceSkillPlan(tr, sp)
		}
		return
	}

	// Online mode: gate trust before any clone/fetch, then open (clone-on-miss).
	done := trace.Spanf(tr, "sync authorize repo %s", g.endpoint)
	if err := gate.Authorize(g.endpoint, syaml.TrustedHosts, gio); err != nil {
		done("error=%v", err)
		appendRepoError(plan, g, err)
		return
	}
	done("")
	repo, err := c.Open(g.cacheID, g.url)
	if err != nil {
		appendRepoError(plan, g, fmt.Errorf("open cache for %s: %w", g.url, err))
		return
	}

	target := RepoTarget{Key: g.key, URL: g.url, CanonicalURL: g.canonical, CacheID: g.cacheID, Endpoint: g.endpoint}
	needFetch := false
	for i := range g.skills {
		sp := planOnlineSkill(repo, syaml, mode, g, g.skills[i])
		if sp.Err != nil {
			plan.Errors = append(plan.Errors, sp.Err)
		}
		if sp.Action == ActionMaterialize && !sp.Cached {
			needFetch = true
			target.Commits = appendUnique(target.Commits, sp.TargetCommit)
		}
		plan.Skills = append(plan.Skills, sp)
		traceSkillPlan(tr, sp)
	}
	if needFetch {
		plan.Repos = append(plan.Repos, target)
		trace.Logf(tr, "sync plan repo %s fetch_commits=%d", g.key, len(target.Commits))
	}
}

func traceSkillPlan(tr *trace.Logger, sp SkillPlan) {
	trace.Logf(tr, "sync plan skill=%s action=%s cached=%t rewrite=%t target=%s message=%s",
		sp.Name, sp.Action, sp.Cached, sp.LockRewrite, short(sp.TargetCommit), sp.Message)
}

// planOnlineSkill decides a single skill with the cache repo opened.
func planOnlineSkill(repo *cache.Repo, syaml *skill.SkillsYAML, mode planMode, g *repoGroup, s groupedSkill) SkillPlan {
	lockSpec := s.entry.VersionSpec
	intent := declaredVersion(syaml, s.name)
	if intent == "" {
		intent = lockSpec
	}
	intentChanged := intent != lockSpec

	sp := SkillPlan{
		Name:         s.name,
		Repo:         g.key,
		URL:          g.url,
		Subpath:      s.entry.Subpath,
		VersionSpec:  lockSpec,
		LockSpec:     lockSpec,
		LockedCommit: s.entry.Commit,
		TargetCommit: s.entry.Commit,
	}

	// Intent reconciliation: skills.yaml version differs from the lock.
	if intentChanged {
		if !mode.floatRefs {
			sp.Action = ActionIntentChanged
			sp.Message = fmt.Sprintf("intent changed (%s → %s) — run: auto skill update %s", lockSpec, intent, s.name)
			return sp
		}
		// auto_update on: act on the new intent and rewrite the lock entry.
		sp.VersionSpec = intent
		return resolveAndDecide(repo, mode, sp, intent, true)
	}

	switch classifySpec(lockSpec) {
	case kindFloat:
		if mode.floatRefs {
			return resolveAndDecide(repo, mode, sp, lockSpec, false)
		}
	case kindTag:
		if mode.floatTags {
			return resolveAndDecide(repo, mode, sp, lockSpec, false)
		}
	case kindCommit:
		// A pinned <sha>/commit: never floats.
	}

	// Locked materialization: keep the pinned commit, fetch on a cache miss.
	return decidePinned(repo, sp)
}

// resolveAndDecide re-resolves spec to newest upstream and records the outcome.
// forced marks an intent-driven rewrite (the spec itself changed).
func resolveAndDecide(repo *cache.Repo, mode planMode, sp SkillPlan, spec string, forced bool) SkillPlan {
	newSha, err := resolveLatest(repo, refForSpec(spec))
	if err != nil {
		sp.Action = ActionUnavailable
		sp.Err = fmt.Errorf("re-resolve %s for %s: %w", spec, sp.Name, err)
		sp.Message = sp.Err.Error()
		return sp
	}
	sp.TargetCommit = newSha
	if classifySpec(spec) == kindTag && newSha != sp.LockedCommit && sp.LockedCommit != "" {
		sp.Warning = fmt.Sprintf("tag %q moved %s → %s (force-update)", strings.TrimPrefix(spec, "tag:"), short(sp.LockedCommit), short(newSha))
	}
	if newSha != sp.LockedCommit || forced {
		if mode.rewrite {
			sp.LockRewrite = true
		}
		sp.Action = ActionResolve
	} else {
		sp.Action = ActionUpToDate
	}
	present, _ := repo.CommitPresent(newSha)
	sp.Cached = present
	if !present {
		// Newly resolved object may need full materialization in phase B.
		sp.Action = ActionMaterialize
	}
	return sp
}

// decidePinned keeps the locked commit and checks it offline; a miss schedules
// locked materialization in phase B.
func decidePinned(repo *cache.Repo, sp SkillPlan) SkillPlan {
	present, _ := repo.CommitPresent(sp.LockedCommit)
	if present {
		sp.Action = ActionUpToDate
		sp.Cached = true
		return sp
	}
	sp.Action = ActionMaterialize
	sp.Cached = false
	return sp
}

// planOfflineSkill decides a skill under sync --check (no network at all).
func planOfflineSkill(repo *cache.Repo, syaml *skill.SkillsYAML, s groupedSkill) SkillPlan {
	lockSpec := s.entry.VersionSpec
	intent := declaredVersion(syaml, s.name)
	if intent == "" {
		intent = lockSpec
	}
	sp := SkillPlan{
		Name:         s.name,
		Repo:         "",
		URL:          s.entry.URL,
		Subpath:      s.entry.Subpath,
		VersionSpec:  lockSpec,
		LockSpec:     lockSpec,
		LockedCommit: s.entry.Commit,
		TargetCommit: s.entry.Commit,
	}
	if intent != lockSpec {
		sp.Action = ActionIntentChanged
		sp.Message = fmt.Sprintf("intent changed (%s → %s) — run: auto skill update %s", lockSpec, intent, s.name)
		return sp
	}
	if repo != nil {
		if present, _ := repo.CommitPresent(s.entry.Commit); present {
			sp.Action = ActionUpToDate
			sp.Cached = true
			return sp
		}
	}
	sp.Action = ActionMaterialize
	sp.Cached = false
	sp.Err = fmt.Errorf("incomplete cache for %s (commit %s); run: auto skill sync", s.name, short(s.entry.Commit))
	sp.Message = sp.Err.Error()
	return sp
}

// ── spec classification ─────────────────────────────────────────────────

type specKind int

const (
	kindFloat  specKind = iota // latest, branch:<name>
	kindTag                    // tag:<name>, or a bare non-hex ref
	kindCommit                 // commit:<hex>, or a bare <sha>
)

func classifySpec(spec string) specKind {
	s := strings.TrimSpace(spec)
	switch {
	case s == "" || s == "latest":
		return kindFloat
	case strings.HasPrefix(s, "branch:"):
		return kindFloat
	case strings.HasPrefix(s, "tag:"):
		return kindTag
	case strings.HasPrefix(s, "commit:"):
		return kindCommit
	default:
		if hexShaRE.MatchString(s) {
			return kindCommit
		}
		return kindTag
	}
}

// refForSpec maps a floating/tag spec to the git ref to fetch + resolve.
func refForSpec(spec string) string {
	s := strings.TrimSpace(spec)
	switch {
	case s == "" || s == "latest":
		return "HEAD"
	case strings.HasPrefix(s, "branch:"):
		return strings.TrimPrefix(s, "branch:")
	case strings.HasPrefix(s, "tag:"):
		return strings.TrimPrefix(s, "tag:")
	default:
		return s
	}
}

// resolveLatest fetches the newest objects for ref and returns its commit SHA.
// It refreshes FETCH_HEAD via the cache's Realize (git fetch origin <ref>) so a
// stale cache still advances to upstream, then peels to a commit.
func resolveLatest(repo *cache.Repo, ref string) (string, error) {
	if err := repo.Realize(ref); err != nil {
		return "", err
	}
	if sha, err := repo.ResolveRef("FETCH_HEAD^{commit}"); err == nil {
		return sha, nil
	}
	return repo.ResolveRef("FETCH_HEAD")
}

// ── loaders / helpers ───────────────────────────────────────────────────

func loadLock(env skill.Env) (*skill.Lock, error) {
	data, err := os.ReadFile(env.LockPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &skill.Lock{Version: 1, Skills: map[string]skill.LockEntry{}}, nil
		}
		return nil, fmt.Errorf("read lock: %w", err)
	}
	lock, err := skill.ParseLock(data)
	if err != nil {
		return nil, fmt.Errorf("parse lock: %w", err)
	}
	if lock.Skills == nil {
		lock.Skills = map[string]skill.LockEntry{}
	}
	return lock, nil
}

func loadSkillsYAML(env skill.Env) (*skill.SkillsYAML, error) {
	data, err := os.ReadFile(env.SkillsYAMLPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &skill.SkillsYAML{Skills: map[string]skill.SkillConfig{}}, nil
		}
		return nil, fmt.Errorf("read skills.yaml: %w", err)
	}
	cfg, err := skill.ParseSkillsYAML(data)
	if err != nil {
		return nil, fmt.Errorf("parse skills.yaml: %w", err)
	}
	if cfg.Skills == nil {
		cfg.Skills = make(map[string]skill.SkillConfig)
	}
	return cfg, nil
}

// declaredVersion returns the skills.yaml-declared version for a skill, falling
// back to the shared default (empty means "no declared intent").
func declaredVersion(syaml *skill.SkillsYAML, name string) string {
	if syaml == nil {
		return ""
	}
	if sc, ok := syaml.Skills[name]; ok && strings.TrimSpace(sc.Version) != "" {
		return strings.TrimSpace(sc.Version)
	}
	return strings.TrimSpace(syaml.Shared.Version)
}

// groupByRepo buckets in-scope lock entries by canonical repo URL.
func groupByRepo(lock *skill.Lock, scope map[string]bool) (map[string]*repoGroup, []string, []error) {
	groups := map[string]*repoGroup{}
	var order []string
	var errs []error

	names := make([]string, 0, len(lock.Skills))
	for name := range lock.Skills {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if len(scope) > 0 && !scope[strings.ToLower(name)] {
			continue
		}
		entry := lock.Skills[name]
		canonical, cacheID, err := transport.CanonicalizeURL(entry.URL)
		if err != nil {
			errs = append(errs, fmt.Errorf("canonicalize url for %s (%s): %w", name, entry.URL, err))
			continue
		}
		ep, err := transport.Endpoint(entry.URL)
		if err != nil {
			errs = append(errs, fmt.Errorf("endpoint for %s (%s): %w", name, entry.URL, err))
			continue
		}
		g, ok := groups[canonical]
		if !ok {
			g = &repoGroup{key: canonical, url: entry.URL, canonical: canonical, cacheID: cacheID, endpoint: ep}
			groups[canonical] = g
			order = append(order, canonical)
		}
		g.skills = append(g.skills, groupedSkill{name: name, entry: entry})
	}
	sort.Strings(order)
	return groups, order, errs
}

func appendRepoError(plan *Plan, g *repoGroup, err error) {
	plan.Errors = append(plan.Errors, err)
	for i := range g.skills {
		s := &g.skills[i]
		plan.Skills = append(plan.Skills, SkillPlan{
			Name:         s.name,
			Repo:         g.key,
			URL:          g.url,
			Subpath:      s.entry.Subpath,
			VersionSpec:  s.entry.VersionSpec,
			LockSpec:     s.entry.VersionSpec,
			LockedCommit: s.entry.Commit,
			TargetCommit: s.entry.Commit,
			Action:       ActionError,
			Err:          err,
			Message:      err.Error(),
		})
	}
}

func normalizeNames(targets []string) map[string]bool {
	if len(targets) == 0 {
		return nil
	}
	out := make(map[string]bool, len(targets))
	for _, t := range targets {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			out[t] = true
		}
	}
	return out
}

func appendUnique(items []string, v string) []string {
	if slices.Contains(items, v) {
		return items
	}
	return append(items, v)
}

func repoCachePath(c *cache.Cache, id transport.CacheIdentity) string {
	p, err := c.RepoPath(id)
	if err != nil {
		return ""
	}
	return p
}

func pathExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
