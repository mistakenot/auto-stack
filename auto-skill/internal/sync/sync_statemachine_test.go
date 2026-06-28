package sync

// Model-based (stateful property) test for the sync pipeline.
//
// The existing prop suite covers data-model invariants (lock round-trips, render
// determinism, URL canonicalization) — all stateless, single-function. None
// exercise a *sequence* of sync operations, which is where the `--target` prune
// bug lived: a scoped `sync --target X` stages only X, so every other managed
// skill classifies as a (false) orphan and gets reaped. Individually-correct
// phases (plan → classify → prune) composed into a destructive result.
//
// This test drives the REAL sync.Run pipeline against a real temp filesystem
// with a local file:// fixture repo (the same hermetic pattern as the rest of
// the sync/prune suite — see helpers_test.go), generating random sequences of
// nine operations and checking, after every step, that what is on disk matches a
// simple model of what should be there.
//
// It is deliberately BLACK-BOX: it only calls the exported sync.Run + reads the
// filesystem, never the internal planPrune/ScanOwnership. That keeps the file
// compiling identically against both the buggy and the fixed pipeline (the fix
// changes planPrune's signature), so the same test catches the bug before the
// fix and passes after it.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
	"gopkg.in/yaml.v3"
	"pgregory.net/rapid"
)

// syncStateMachine is the rapid.StateMachine implementation. The model tracks,
// per render dir (target style), which skills should currently exist on disk;
// the SUT is the real sync pipeline operating on env's temp filesystem.
type syncStateMachine struct {
	t   *testing.T // outer testing.T — the package helpers require it
	env skill.Env
	fix *fixture

	targets []Target // resolved render dirs (style name → absolute dir)

	lock     map[string]skill.LockEntry // current vendored lock entries
	cfg      *skill.SkillsYAML          // current skills.yaml config
	authored map[string]bool            // authored ./skills/<name> source skills

	// model[style][name] == true ⇒ the rendered dir style/name must exist on disk.
	// A missing/false entry ⇒ it must be absent. Maintained per operation.
	model map[string]map[string]bool

	nextID        int    // counter for unique synthetic skill names
	hasFullSynced bool   // a full sync has established baseline state at least once
	lastOp        string // last operation (for failure messages)
}

// ── construction (the "Init" the rapid.StateMachine interface lacks) ─────────

// newSyncSM builds an isolated state machine instance: a fresh fixture repo with
// 3-5 committed skills, an isolated env (Root=t.TempDir(), RootOverride=true), an
// approved trust entry, and lock + skills.yaml pointing at the fixture. Every
// generated sequence gets its own instance, so sequences never share state.
func newSyncSM(t *testing.T, rt *rapid.T) *syncStateMachine {
	f := newFixture(t)

	// A local file:// repo's history is cumulative, so the final HEAD contains
	// every committed skill's subtree. Point all initial lock entries at it.
	n := rapid.IntRange(3, 5).Draw(rt, "init_count")
	names := make([]string, 0, n)
	var head string
	for i := range n {
		name := fmt.Sprintf("init-%d", i)
		head = f.commitSkill(name, "v1")
		names = append(names, name)
	}

	env := newEnv(t)
	approve(t, env, f.url)

	lock := make(map[string]skill.LockEntry, len(names))
	for _, name := range names {
		lock[name] = lockEntry(f.url, name, "latest", head)
	}
	cfg := &skill.SkillsYAML{Skills: map[string]skill.SkillConfig{}}
	writeLock(t, env, lock)
	writeSkillsYAML(t, env, cfg)

	sm := &syncStateMachine{
		t:        t,
		env:      env,
		fix:      f,
		targets:  resolveTargets(env, cfg), // Targets unset ⇒ default styles, stable for the run
		lock:     lock,
		cfg:      cfg,
		authored: map[string]bool{},
		model:    map[string]map[string]bool{},
	}
	for _, tg := range sm.targets {
		sm.model[tg.Name] = map[string]bool{}
	}

	// Free this sequence's temp trees eagerly after each rapid run (including
	// shrink re-runs) so a failing/shrinking run doesn't accumulate disk. The
	// outer t.TempDir cleanup later is a harmless no-op on the already-removed dirs.
	rt.Cleanup(func() {
		_ = os.RemoveAll(env.Root)
		_ = os.RemoveAll(f.dir)
	})
	return sm
}

// ── core operations (the minimal set that reproduces the --target bug class) ──

// FullSync runs `sync --locked`: every locked + authored skill is rendered into
// every render dir, and every receipt-gated orphan is pruned. The model
// converges to exactly the renderable set in every render dir.
func (sm *syncStateMachine) FullSync(t *rapid.T) {
	res, err := Run(sm.env, Options{Locked: true})
	if err != nil {
		t.Fatalf("FullSync: Run: %v", err)
	}
	if res.ExitCode() != 0 {
		t.Fatalf("FullSync: non-zero exit, errors=%v", res.Errors)
	}
	rs := sm.renderableSet()
	for _, tg := range sm.targets {
		next := make(map[string]bool, len(rs))
		for name := range rs {
			next[name] = true
		}
		sm.model[tg.Name] = next
	}
	sm.hasFullSynced = true
	sm.lastOp = "FullSync"
	// Invariant: after a full sync the manifest lists exactly the rendered set.
	sm.assertManifestMatches(t, rs)
}

// ScopedSync runs `sync --target X` for a random existing skill. The --target
// scope restricts only the VENDORED (lock-driven) skills: discoverAuthored
// always runs, so authored ./skills/** skills are (re)rendered on every sync
// regardless of scope. So a scoped sync (re)renders the targeted skill plus
// every authored skill, and must leave every OTHER vendored skill untouched.
// That last clause is what the --target bug violates — on the buggy pipeline
// every non-targeted vendored skill is pruned, which the next Check() catches.
func (sm *syncStateMachine) ScopedSync(t *rapid.T) {
	names := sm.renderable()
	if len(names) == 0 {
		return
	}
	name := rapid.SampledFrom(names).Draw(t, "scope_name")

	lockBefore, _ := os.ReadFile(sm.env.LockPath())
	res, err := Run(sm.env, Options{Targets: []string{name}})
	if err != nil {
		t.Fatalf("ScopedSync(%s): Run: %v", name, err)
	}
	if res.ExitCode() != 0 {
		t.Fatalf("ScopedSync(%s): non-zero exit, errors=%v", name, res.Errors)
	}
	// Model: the targeted skill and every authored skill are (re)rendered into
	// all render dirs; every other vendored skill is left exactly as it was.
	sm.setRendered(name)
	for a := range sm.authored {
		sm.setRendered(a)
	}
	sm.lastOp = "ScopedSync(" + name + ")"

	// Invariant: a scoped sync never advances the lock — lock.json byte-stable.
	lockAfter, _ := os.ReadFile(sm.env.LockPath())
	if !bytes.Equal(lockBefore, lockAfter) {
		t.Fatalf("ScopedSync(%s) changed lock.json (scoped syncs must not advance the lock)", name)
	}
}

// AddSkill commits a new skill to the fixture and adds a lock entry. The skill is
// now renderable but not yet on disk — the model is unchanged until a sync runs.
func (sm *syncStateMachine) AddSkill(t *rapid.T) {
	sm.nextID++
	name := fmt.Sprintf("vend-%d", sm.nextID)
	head := sm.fix.commitSkill(name, "v1")
	sm.lock[name] = lockEntry(sm.fix.url, name, "latest", head)
	writeLock(sm.t, sm.env, sm.lock)
	sm.lastOp = "AddSkill(" + name + ")"
}

// RemoveSkill drops a random vendored skill from the lock + config. Its rendered
// dir lingers on disk (model unchanged) until the next full sync prunes it.
func (sm *syncStateMachine) RemoveSkill(t *rapid.T) {
	names := sortedKeys(sm.lock)
	if len(names) == 0 {
		return
	}
	name := rapid.SampledFrom(names).Draw(t, "remove_name")
	delete(sm.lock, name)
	delete(sm.cfg.Skills, name)
	writeLock(sm.t, sm.env, sm.lock)
	writeSkillsYAML(sm.t, sm.env, sm.cfg)
	sm.lastOp = "RemoveSkill(" + name + ")"
}

// EditConfig mutates the replacements for a random renderable skill. The skill's
// templates declare no customize vars, so the extra replacement is ignored by the
// renderer (skill_version unchanged) — config changes, the model does not.
func (sm *syncStateMachine) EditConfig(t *rapid.T) {
	names := sm.renderable()
	if len(names) == 0 {
		return
	}
	name := rapid.SampledFrom(names).Draw(t, "edit_name")
	sm.nextID++
	sc := sm.cfg.Skills[name]
	if sc.Replacements == nil {
		sc.Replacements = skill.ReplacementMap{}
	}
	sc.Replacements[fmt.Sprintf("var_%d", sm.nextID)] = yaml.Node{
		Kind: yaml.ScalarNode, Tag: "!!str", Value: "v" + strconv.Itoa(sm.nextID),
	}
	sm.cfg.Skills[name] = sc
	writeSkillsYAML(sm.t, sm.env, sm.cfg)
	sm.lastOp = "EditConfig(" + name + ")"
}

// ── environmental chaos operations (recovery / resilience) ───────────────────

// AddAuthoredSkill writes a SKILL.md into the authored ./skills source dir and
// records it in config. It is now renderable; the next full sync renders it into
// every render dir. The model is unchanged until then (the render dirs are empty
// of it; the authored source dir is not a render dir and is never asserted on).
func (sm *syncStateMachine) AddAuthoredSkill(t *rapid.T) {
	sm.nextID++
	name := fmt.Sprintf("auth-%d", sm.nextID)
	writeAuthoredSkill(sm.t, sm.env, name, "authored "+name)
	sm.authored[name] = true
	sm.cfg.Skills[name] = skill.SkillConfig{}
	writeSkillsYAML(sm.t, sm.env, sm.cfg)
	sm.lastOp = "AddAuthoredSkill(" + name + ")"
}

// RenameSkill removes a vendored skill and commits + locks a new one in its
// place. The old rendered dir lingers as an orphan (model unchanged) until the
// next full sync prunes it and renders the new name.
func (sm *syncStateMachine) RenameSkill(t *rapid.T) {
	names := sortedKeys(sm.lock)
	if len(names) == 0 {
		return
	}
	old := rapid.SampledFrom(names).Draw(t, "rename_old")
	sm.nextID++
	newName := fmt.Sprintf("ren-%d", sm.nextID)
	head := sm.fix.commitSkill(newName, "renamed from "+old)
	delete(sm.lock, old)
	delete(sm.cfg.Skills, old)
	sm.lock[newName] = lockEntry(sm.fix.url, newName, "latest", head)
	writeLock(sm.t, sm.env, sm.lock)
	writeSkillsYAML(sm.t, sm.env, sm.cfg)
	sm.lastOp = "RenameSkill(" + old + "->" + newName + ")"
}

// DeleteRenderDir simulates a user deleting one skill's rendered copy from a
// single render dir. The model drops it for that dir only; a later full sync
// heals it.
func (sm *syncStateMachine) DeleteRenderDir(t *rapid.T) {
	type pick struct {
		tg   Target
		name string
	}
	var present []pick
	for _, tg := range sm.targets {
		for name, ok := range sm.model[tg.Name] {
			if ok {
				present = append(present, pick{tg, name})
			}
		}
	}
	if len(present) == 0 {
		return
	}
	sort.Slice(present, func(i, j int) bool {
		if present[i].tg.Name != present[j].tg.Name {
			return present[i].tg.Name < present[j].tg.Name
		}
		return present[i].name < present[j].name
	})
	p := present[rapid.IntRange(0, len(present)-1).Draw(t, "del_idx")]
	if err := os.RemoveAll(filepath.Join(p.tg.Dir, p.name)); err != nil {
		t.Fatalf("DeleteRenderDir: %v", err)
	}
	delete(sm.model[p.tg.Name], p.name)
	sm.lastOp = "DeleteRenderDir(" + p.tg.Name + "/" + p.name + ")"
}

// DeleteAllRenderDirs simulates a user deleting one skill's rendered copy from
// every render dir. A later full sync heals it back.
func (sm *syncStateMachine) DeleteAllRenderDirs(t *rapid.T) {
	set := map[string]bool{}
	for _, tg := range sm.targets {
		for name, ok := range sm.model[tg.Name] {
			if ok {
				set[name] = true
			}
		}
	}
	if len(set) == 0 {
		return
	}
	name := rapid.SampledFrom(sortedKeysOf(set)).Draw(t, "delall_name")
	for _, tg := range sm.targets {
		if err := os.RemoveAll(filepath.Join(tg.Dir, name)); err != nil {
			t.Fatalf("DeleteAllRenderDirs: %v", err)
		}
		delete(sm.model[tg.Name], name)
	}
	sm.lastOp = "DeleteAllRenderDirs(" + name + ")"
}

// ── invariant check (runs after every operation) ─────────────────────────────

// Check asserts the presence invariant in both directions, per render dir: every
// skill the model marks present must exist on disk, and every dir on disk must be
// marked present in the model. This single bidirectional check subsumes the
// "no collateral damage" invariant — a scoped op that wrongly prunes a
// non-targeted skill leaves the model marking it present while disk has lost it.
func (sm *syncStateMachine) Check(t *rapid.T) {
	for _, tg := range sm.targets {
		for name, present := range sm.model[tg.Name] {
			if present && !sm.diskHas(tg, name) {
				t.Fatalf("presence invariant: %s/%s expected on disk but missing (lastOp=%s)",
					tg.Name, name, sm.lastOp)
			}
		}
		for _, name := range sm.diskSkillDirs(tg) {
			if !sm.model[tg.Name][name] {
				t.Fatalf("presence invariant: %s/%s on disk but absent from model (lastOp=%s)",
					tg.Name, name, sm.lastOp)
			}
		}
	}
}

// ── helpers (unexported ⇒ invisible to rapid's action reflection) ────────────

func (sm *syncStateMachine) renderableSet() map[string]bool {
	out := make(map[string]bool, len(sm.lock)+len(sm.authored))
	for name := range sm.lock {
		out[name] = true
	}
	for name := range sm.authored {
		out[name] = true
	}
	return out
}

func (sm *syncStateMachine) renderable() []string { return sortedKeysOf(sm.renderableSet()) }

func (sm *syncStateMachine) setRendered(name string) {
	for _, tg := range sm.targets {
		sm.model[tg.Name][name] = true
	}
}

func (sm *syncStateMachine) diskHas(tg Target, name string) bool {
	info, err := os.Stat(filepath.Join(tg.Dir, name))
	return err == nil && info.IsDir()
}

func (sm *syncStateMachine) diskSkillDirs(tg Target) []string {
	dents, err := os.ReadDir(tg.Dir)
	if err != nil {
		return nil // absent render dir ⇒ no skills
	}
	var out []string
	for _, de := range dents {
		if de.IsDir() && !isTempDirName(de.Name()) {
			out = append(out, de.Name())
		}
	}
	return out
}

func (sm *syncStateMachine) assertManifestMatches(t *rapid.T, rs map[string]bool) {
	data, err := os.ReadFile(sm.env.ManifestPath())
	if err != nil {
		if len(rs) == 0 {
			return // nothing renderable ⇒ manifest may be absent
		}
		t.Fatalf("manifest missing after full sync: %v", err)
	}
	m, err := skill.ParseManifest(data)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if len(m.Skills) != len(rs) {
		t.Fatalf("manifest skills %v != renderable %v", sortedKeysOf(boolSet(m.Skills)), sortedKeysOf(rs))
	}
	for name := range rs {
		if _, ok := m.Skills[name]; !ok {
			t.Fatalf("manifest missing renderable skill %q", name)
		}
	}
}

func sortedKeys(m map[string]skill.LockEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func boolSet[V any](m map[string]V) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// ── entry points ─────────────────────────────────────────────────────────────

// TestSyncStateMachine generates random sequences of the nine operations against
// the real sync pipeline and checks the invariants after every step. On the
// buggy pipeline a FullSync→ScopedSync sequence shrinks to a minimal
// counterexample (collateral prune); on the fixed pipeline all sequences pass.
//
// Cost: ~100 sequences × ~30 ops, each a real local-git sync (~50s). Tune with
// `-rapid.checks=N` / `-rapid.steps=N`, or run with `-short` (rapid halves the
// step count). TestSyncScopedSyncNoCollateralDamage below pins the minimal
// counterexample as a fast, deterministic regression guard so the bug class is
// covered even when this longer randomized run is dialed down.
func TestSyncStateMachine(t *testing.T) {
	requireGit(t)
	rapid.Check(t, func(rt *rapid.T) {
		sm := newSyncSM(t, rt)
		rt.Repeat(rapid.StateMachineActions(sm))
	})
}

// TestSyncScopedSyncNoCollateralDamage is the deterministic kill test for the
// --target prune bug: it is the minimal FullSync→ScopedSync sequence the state
// machine shrinks to, pinned so it always runs. A scoped `sync --target alpha`
// must re-render alpha without deleting beta/gamma from any render dir.
func TestSyncScopedSyncNoCollateralDamage(t *testing.T) {
	requireGit(t)
	skills := []string{"alpha", "beta", "gamma"}

	f := newFixture(t)
	var head string
	for _, n := range skills {
		head = f.commitSkill(n, "v1")
	}

	env := newEnv(t)
	approve(t, env, f.url)
	lock := map[string]skill.LockEntry{}
	for _, n := range skills {
		lock[n] = lockEntry(f.url, n, "latest", head)
	}
	writeLock(t, env, lock)
	writeSkillsYAML(t, env, &skill.SkillsYAML{Skills: map[string]skill.SkillConfig{}})
	targets := resolveTargets(env, &skill.SkillsYAML{})

	// Full sync renders all three into every render dir.
	if res, err := Run(env, Options{Locked: true}); err != nil || res.ExitCode() != 0 {
		t.Fatalf("full sync: err=%v errors=%v", err, errsOf(res))
	}
	for _, tg := range targets {
		for _, n := range skills {
			if _, err := os.Stat(filepath.Join(tg.Dir, n)); err != nil {
				t.Fatalf("after full sync %s/%s should exist: %v", tg.Name, n, err)
			}
		}
	}

	// Scoped sync of alpha must NOT delete beta or gamma from any render dir.
	if res, err := Run(env, Options{Targets: []string{"alpha"}}); err != nil || res.ExitCode() != 0 {
		t.Fatalf("scoped sync: err=%v errors=%v", err, errsOf(res))
	}
	for _, tg := range targets {
		for _, n := range skills {
			if _, err := os.Stat(filepath.Join(tg.Dir, n)); err != nil {
				t.Errorf("scoped sync --target alpha deleted %s/%s (collateral damage): %v", tg.Name, n, err)
			}
		}
	}
}

func errsOf(res *Result) []string {
	if res == nil {
		return nil
	}
	return res.Errors
}
