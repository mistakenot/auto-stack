package sync

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/trace"
)

// lockEntry builds a resolved lock entry for a fixture skill.
func lockEntry(url, name, spec, commit string) skill.LockEntry {
	return skill.LockEntry{
		Source:      url,
		URL:         url,
		VersionSpec: spec,
		Ref:         commit,
		Commit:      commit,
		Subpath:     "skills/" + name,
		State:       "resolved",
	}
}

// TestPlanDedupeByRepo: N skills from one repo collapse to one fetch target
// (one repo, one distinct commit). A locally-cloned file:// repo is fully
// materialized, so we pin both skills at a commit the cache cannot satisfy to
// force — and observe the deduped — phase-B work.
func TestPlanDedupeByRepo(t *testing.T) {
	f := newFixture(t)
	f.commitSkill("alpha", "step a")
	f.commitSkill("beta", "step b")

	gone := "0123456789abcdef0123456789abcdef01234567"
	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "commit:"+gone, gone),
		"beta":  lockEntry(f.url, "beta", "commit:"+gone, gone),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})

	plan, err := BuildPlan(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(plan.Skills))
	}
	if len(plan.Repos) != 1 {
		t.Fatalf("expected 1 deduped repo, got %d", len(plan.Repos))
	}
	if got := len(plan.Repos[0].Commits); got != 1 {
		t.Fatalf("expected 1 distinct commit, got %d", got)
	}
}

// TestPlanCacheSatisfiedSkip: an already-materialized commit needs no fetch.
func TestPlanCacheSatisfiedSkip(t *testing.T) {
	f := newFixture(t)
	head := f.commitSkill("alpha", "step a")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "latest", head),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	realizeCommit(t, env, f.url, head) // pre-fetch objects

	plan, err := BuildPlan(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Repos) != 0 {
		t.Fatalf("expected no fetch work, got %d repos", len(plan.Repos))
	}
	sp, _ := findSkill(plan, "alpha")
	if sp.Action != ActionUpToDate || !sp.Cached {
		t.Fatalf("expected up_to_date+cached, got %s cached=%v", sp.Action, sp.Cached)
	}
}

// TestPlanLockedMaterialization: a locked run keeps the pinned commit and
// schedules a fetch on a cache miss; a floating run re-resolves the ref.
func TestPlanLockedVsFloat(t *testing.T) {
	f := newFixture(t)
	old := f.commitSkill("alpha", "v1")
	newSHA := f.commitSkill("alpha", "v2")
	if old == newSHA {
		t.Fatal("fixture did not advance")
	}

	newLock := func() map[string]skill.LockEntry {
		return map[string]skill.LockEntry{
			"alpha": lockEntry(f.url, "alpha", "latest", old),
		}
	}

	// Locked: target stays at the old commit, no rewrite.
	envL := newEnv(t)
	approve(t, envL, f.url)
	writeLock(t, envL, newLock())
	writeSkillsYAML(t, envL, &skill.SkillsYAML{})
	planL, err := BuildPlan(envL, Options{Locked: true})
	if err != nil {
		t.Fatalf("BuildPlan locked: %v", err)
	}
	spL, _ := findSkill(planL, "alpha")
	if spL.TargetCommit != old {
		t.Fatalf("locked: expected target %s, got %s", short(old), short(spL.TargetCommit))
	}
	if spL.LockRewrite {
		t.Fatal("locked: did not expect lock rewrite")
	}

	// Float (auto_update): re-resolve latest → new HEAD, mark rewrite.
	envF := newEnv(t)
	approve(t, envF, f.url)
	writeLock(t, envF, newLock())
	writeSkillsYAML(t, envF, &skill.SkillsYAML{AutoUpdate: true})
	planF, err := BuildPlan(envF, Options{AutoUpdate: true})
	if err != nil {
		t.Fatalf("BuildPlan float: %v", err)
	}
	spF, _ := findSkill(planF, "alpha")
	if spF.TargetCommit != newSHA {
		t.Fatalf("float: expected target %s, got %s", short(newSHA), short(spF.TargetCommit))
	}
	if !spF.LockRewrite {
		t.Fatal("float: expected lock rewrite")
	}
}

// TestPlanFloatMemoizesResolvePerRef: several skills in one repo sharing a
// floating ref (latest → HEAD) trigger exactly one upstream fetch, not one per
// skill. Guards the memoization that keeps `auto skill update` from scaling
// remote access with skill count (≤1 `git realize HEAD` per repo/ref).
func TestPlanFloatMemoizesResolvePerRef(t *testing.T) {
	f := newFixture(t)
	old := f.commitSkill("alpha", "v1")
	f.commitSkill("beta", "v1")
	newSHA := f.commitSkill("gamma", "v1") // HEAD after all three commits
	if old == newSHA {
		t.Fatal("fixture did not advance")
	}

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "latest", old),
		"beta":  lockEntry(f.url, "beta", "latest", old),
		"gamma": lockEntry(f.url, "gamma", "latest", old),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{AutoUpdate: true})

	var buf bytes.Buffer
	plan, err := BuildPlan(env, Options{AutoUpdate: true, Trace: trace.New(&buf)})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	// All three skills float to the same new HEAD.
	for _, name := range []string{"alpha", "beta", "gamma"} {
		sp, ok := findSkill(plan, name)
		if !ok {
			t.Fatalf("missing skill %s in plan", name)
		}
		if sp.TargetCommit != newSHA {
			t.Fatalf("%s: expected target %s, got %s", name, short(newSHA), short(sp.TargetCommit))
		}
	}

	// The shared HEAD ref is fetched once (memoized), not once per skill.
	if got := strings.Count(buf.String(), "git realize HEAD start"); got != 1 {
		t.Fatalf("expected exactly 1 `git realize HEAD`, got %d\ntrace:\n%s", got, buf.String())
	}
}

// TestPlanPinnedGoneUpstream: a pinned commit absent upstream errors clearly.
func TestPlanPinnedGoneUpstream(t *testing.T) {
	f := newFixture(t)
	f.commitSkill("alpha", "v1")

	gone := "0123456789abcdef0123456789abcdef01234567"
	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "commit:"+gone, gone),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})

	plan, err := BuildPlan(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Repos) != 1 {
		t.Fatalf("expected the pinned commit scheduled for fetch, got %d repos", len(plan.Repos))
	}
	// The unavailability surfaces when phase B tries to materialize it.
	res, err := Fetch(env, plan, Options{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !res.HasErrors() {
		t.Fatal("expected fetch to fail for a missing pinned commit")
	}
	if !strings.Contains(res.Err().Error(), "pinned commit unavailable upstream") {
		t.Fatalf("expected 'pinned commit unavailable upstream', got: %v", res.Err())
	}
}

// TestPlanIntentReconciliation: skills.yaml version differs from the lock.
func TestPlanIntentReconciliation(t *testing.T) {
	f := newFixture(t)
	old := f.commitSkill("alpha", "v1")
	newSHA := f.commitSkill("alpha", "v2")
	f.branch("feature") // feature points at newSHA

	mkLock := func() map[string]skill.LockEntry {
		return map[string]skill.LockEntry{
			"alpha": lockEntry(f.url, "alpha", "latest", old),
		}
	}
	intent := &skill.SkillsYAML{
		Skills: map[string]skill.SkillConfig{
			"alpha": {Version: "branch:feature"},
		},
	}

	// auto_update true → re-resolve the new intent + mark rewrite.
	envT := newEnv(t)
	approve(t, envT, f.url)
	writeLock(t, envT, mkLock())
	intentT := *intent
	intentT.AutoUpdate = true
	writeSkillsYAML(t, envT, &intentT)
	planT, err := BuildPlan(envT, Options{AutoUpdate: true})
	if err != nil {
		t.Fatalf("BuildPlan intent float: %v", err)
	}
	spT, _ := findSkill(planT, "alpha")
	if spT.TargetCommit != newSHA {
		t.Fatalf("intent float: expected target %s, got %s", short(newSHA), short(spT.TargetCommit))
	}
	if !spT.LockRewrite || spT.VersionSpec != "branch:feature" {
		t.Fatalf("intent float: expected rewrite to branch:feature, got rewrite=%v spec=%s", spT.LockRewrite, spT.VersionSpec)
	}

	// auto_update false → report, lock untouched.
	envF := newEnv(t)
	approve(t, envF, f.url)
	writeLock(t, envF, mkLock())
	writeSkillsYAML(t, envF, intent) // AutoUpdate false
	planF, err := BuildPlan(envF, Options{})
	if err != nil {
		t.Fatalf("BuildPlan intent locked: %v", err)
	}
	spF, _ := findSkill(planF, "alpha")
	if spF.Action != ActionIntentChanged {
		t.Fatalf("intent locked: expected intent_changed, got %s", spF.Action)
	}
	if spF.TargetCommit != old || spF.LockRewrite {
		t.Fatalf("intent locked: lock should be untouched, got target=%s rewrite=%v", short(spF.TargetCommit), spF.LockRewrite)
	}
	if !strings.Contains(spF.Message, "run: auto skill update") {
		t.Fatalf("intent locked: expected remediation message, got %q", spF.Message)
	}
}

// TestPlanCheckOffline: sync --check never hits the network; a cached commit is
// up-to-date, a missing one reports an incomplete cache.
func TestPlanCheckOffline(t *testing.T) {
	f := newFixture(t)
	head := f.commitSkill("alpha", "v1")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "latest", head),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	realizeCommit(t, env, f.url, head)
	f.remove() // upstream gone: any network call would fail

	plan, err := BuildPlan(env, Options{Check: true})
	if err != nil {
		t.Fatalf("BuildPlan --check: %v", err)
	}
	if len(plan.Repos) != 0 {
		t.Fatalf("--check must schedule no fetch, got %d", len(plan.Repos))
	}
	sp, _ := findSkill(plan, "alpha")
	if sp.Action != ActionUpToDate {
		t.Fatalf("expected up_to_date offline, got %s", sp.Action)
	}
}

func TestPlanCheckOfflineIncomplete(t *testing.T) {
	f := newFixture(t)
	head := f.commitSkill("alpha", "v1")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "latest", head),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	// No realize: cache is empty/absent → offline check cannot satisfy it.

	plan, err := BuildPlan(env, Options{Check: true})
	if err != nil {
		t.Fatalf("BuildPlan --check: %v", err)
	}
	sp, _ := findSkill(plan, "alpha")
	if sp.Err == nil || !strings.Contains(sp.Err.Error(), "incomplete cache") {
		t.Fatalf("expected incomplete cache error, got %v", sp.Err)
	}
	if !plan.HasErrors() {
		t.Fatal("expected plan to record an error")
	}
}
