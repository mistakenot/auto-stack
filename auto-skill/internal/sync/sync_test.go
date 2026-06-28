package sync

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
)

func readLockBytes(t *testing.T, env skill.Env) []byte {
	t.Helper()
	data, err := os.ReadFile(env.LockPath())
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	return data
}

func lockCommit(t *testing.T, env skill.Env, name string) string {
	t.Helper()
	lock, err := loadLock(env)
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}
	return lock.Skills[name].Commit
}

// TestRunAutoUpdateFloats: auto_update:true floats `latest` to the newest HEAD,
// rewrites the lock to the new commit, and writes the manifest.
func TestRunAutoUpdateFloats(t *testing.T) {
	f := newFixture(t)
	old := f.commitSkill("alpha", "v1")
	newSHA := f.commitSkill("alpha", "v2")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "latest", old),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{AutoUpdate: true})

	res, err := Run(env, Options{AutoUpdate: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode() != 0 {
		t.Fatalf("exit %d, errors=%v", res.ExitCode(), res.Errors)
	}
	if !res.LockRewritten {
		t.Error("expected the lock to be rewritten on a float")
	}
	if got := lockCommit(t, env, "alpha"); got != newSHA {
		t.Errorf("lock commit = %s, want floated %s", short(got), short(newSHA))
	}
	if !res.ManifestWritten {
		t.Error("expected the manifest to be written")
	}
}

// TestRunAutoUpdateOffLocked: auto_update:false reproduces the locked commit and
// never advances the lock even though upstream moved.
func TestRunAutoUpdateOffLocked(t *testing.T) {
	f := newFixture(t)
	old := f.commitSkill("alpha", "v1")
	f.commitSkill("alpha", "v2") // upstream moves, but auto_update is off

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "latest", old),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{AutoUpdate: false})
	realizeCommit(t, env, f.url, old)
	lockBefore := readLockBytes(t, env)

	res, err := Run(env, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.LockRewritten {
		t.Error("auto_update:false must not rewrite the lock")
	}
	if got := lockCommit(t, env, "alpha"); got != old {
		t.Errorf("lock commit = %s, want pinned %s", short(got), short(old))
	}
	if !bytes.Equal(readLockBytes(t, env), lockBefore) {
		t.Error("lock.json bytes changed under auto_update:false")
	}
}

// TestRunLockedPrecedenceOverAutoUpdate: --locked beats auto_update:true — the
// lock stays pinned even though the spec floats and upstream moved.
func TestRunLockedPrecedenceOverAutoUpdate(t *testing.T) {
	f := newFixture(t)
	old := f.commitSkill("alpha", "v1")
	f.commitSkill("alpha", "v2")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "latest", old),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{AutoUpdate: true})
	realizeCommit(t, env, f.url, old)
	lockBefore := readLockBytes(t, env)

	res, err := Run(env, Options{Locked: true, AutoUpdate: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.LockRewritten {
		t.Error("--locked must override auto_update and not rewrite the lock")
	}
	if !bytes.Equal(readLockBytes(t, env), lockBefore) {
		t.Error("--locked left lock.json non-identical")
	}
}

// TestRunTargetImpliesLocked: a scoped --target run implies --locked, so the
// project-wide lock is not advanced even with auto_update:true and upstream
// moved.
func TestRunTargetImpliesLocked(t *testing.T) {
	f := newFixture(t)
	old := f.commitSkill("alpha", "v1")
	f.commitSkill("alpha", "v2")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "latest", old),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{AutoUpdate: true})
	realizeCommit(t, env, f.url, old)
	lockBefore := readLockBytes(t, env)

	res, err := Run(env, Options{Targets: []string{"alpha"}, AutoUpdate: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Locked {
		t.Error("--target must imply --locked in the result")
	}
	if res.LockRewritten {
		t.Error("--target run must not advance the lock")
	}
	if got := lockCommit(t, env, "alpha"); got != old {
		t.Errorf("lock commit = %s, want pinned %s", short(got), short(old))
	}
	if !bytes.Equal(readLockBytes(t, env), lockBefore) {
		t.Error("--target run changed lock.json bytes")
	}
}

// TestRunLockedEditRewritesManifestNotLock: a locked sync after an authored edit
// rewrites manifest.json (the rendered output changed) and leaves lock.json
// byte-identical (the "lock unchanged" contract).
func TestRunLockedEditRewritesManifestNotLock(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "alpha", "v1 body")

	// Seed an empty lock so we can prove it is byte-stable across the edit.
	writeLock(t, env, map[string]skill.LockEntry{})
	lockBefore := readLockBytes(t, env)

	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	manifest1, err := os.ReadFile(env.ManifestPath())
	if err != nil {
		t.Fatalf("manifest after first run: %v", err)
	}

	// Edit the authored skill: rendered output (skill_version) changes.
	writeAuthoredSkill(t, env, "alpha", "v2 body edited")
	res, err := Run(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if !res.ManifestWritten {
		t.Error("locked sync after an edit should write the manifest")
	}
	manifest2, err := os.ReadFile(env.ManifestPath())
	if err != nil {
		t.Fatalf("manifest after second run: %v", err)
	}
	if bytes.Equal(manifest1, manifest2) {
		t.Error("manifest.json should change after the authored edit")
	}
	if res.LockRewritten {
		t.Error("locked sync must not rewrite the lock")
	}
	if !bytes.Equal(readLockBytes(t, env), lockBefore) {
		t.Error("lock.json must be byte-identical across a locked sync")
	}

	// The installed tree reflects the edit.
	target := filepath.Join(env.Root, ".claude", "skills", "alpha", "SKILL.md")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read installed: %v", err)
	}
	if !strings.Contains(string(data), "v2 body edited") {
		t.Errorf("installed tree not updated: %q", string(data))
	}
}

// TestRunBudgetWarnsExitsZero: an oversized SKILL.md warns but never fails the
// run (advisory budget; lint is the gate).
func TestRunBudgetWarnsExitsZero(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	big := strings.Repeat("word ", 5000) // ~6k tokens > 4000 advisory budget
	writeAuthoredSkill(t, env, "alpha", big)

	res, err := Run(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode() != 0 {
		t.Fatalf("budget overflow must exit zero, got %d (errors=%v)", res.ExitCode(), res.Errors)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("budget overflow must not produce errors: %v", res.Errors)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "advisory budget") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an advisory budget warning, got %v", res.Warnings)
	}
}

// TestRunCheckStaleExitsNonZero: --check is an offline dry-run that writes
// nothing and exits non-zero when a target is stale-by-render.
func TestRunCheckStaleExitsNonZero(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "alpha", "v1 body")

	// No prior install → the target is absent → stale-by-render.
	res, err := Run(env, Options{Check: true})
	if err != nil {
		t.Fatalf("Run --check: %v", err)
	}
	if res.Mode != "check" {
		t.Errorf("mode = %q, want check", res.Mode)
	}
	if len(res.Stale) == 0 {
		t.Fatal("expected a stale target under --check")
	}
	if res.ExitCode() == 0 {
		t.Error("--check with a stale target must exit non-zero")
	}
	// --check writes nothing.
	if _, err := os.Stat(env.ManifestPath()); !os.IsNotExist(err) {
		t.Errorf("--check must not write the manifest (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(env.Root, ".claude", "skills", "alpha")); !os.IsNotExist(err) {
		t.Error("--check must not write the target tree")
	}

	// After a real sync, --check is clean and exits zero.
	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	res, err = Run(env, Options{Check: true})
	if err != nil {
		t.Fatalf("Run --check (post-sync): %v", err)
	}
	if len(res.Stale) != 0 {
		t.Errorf("expected no stale targets after a sync, got %v", res.Stale)
	}
	if res.ExitCode() != 0 {
		t.Errorf("clean --check should exit zero, got %d", res.ExitCode())
	}
}

// TestRunRecoversPendingJournalAtStartup: a pending journal from an interrupted
// commit is recovered at the next Run's startup.
// TestRunScopedTargetLeavesOtherRendersIntact: a scoped `--target X` run renders
// only the named skill and must NEVER delete the rendered copies of the other
// managed skills. Regression for the bug where the prune pass's "desired" set
// collapsed to the scoped subset (proc.Staged), so every non-targeted managed
// skill classified as an orphan and a scoped `sync --target X` reaped all of
// their renders ("Removed N orphaned target(s)").
func TestRunScopedTargetLeavesOtherRendersIntact(t *testing.T) {
	f := newFixture(t)
	f.commitSkill("alpha", "v1")
	head := f.commitSkill("beta", "v1") // cumulative tree: both skills at head

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "latest", head),
		"beta":  lockEntry(f.url, "beta", "latest", head),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})

	// Full sync renders both vendored skills into every target dir.
	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("full sync: %v", err)
	}
	betaClaude := filepath.Join(env.Root, ".claude", "skills", "beta")
	betaAgents := filepath.Join(env.Root, ".agents", "skills", "beta")
	for _, d := range []string{betaClaude, betaAgents} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("precondition: beta render missing after full sync at %s: %v", d, err)
		}
	}

	// Scoped sync of alpha alone must not prune or delete beta's renders.
	res, err := Run(env, Options{Targets: []string{"alpha"}})
	if err != nil {
		t.Fatalf("scoped sync: %v", err)
	}
	if len(res.Pruned) != 0 {
		t.Errorf("scoped `--target alpha` pruned %v, want nothing pruned", res.Pruned)
	}
	for _, d := range []string{betaClaude, betaAgents} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("scoped `--target alpha` deleted non-targeted beta render at %s: %v", d, err)
		}
	}
}

func TestRunRecoversPendingJournalAtStartup(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "alpha", "v1 body")

	in, _ := prepareCommit(t, env, true)
	if _, err := commit(in, faultBeforeManifest); err == nil {
		t.Fatal("expected fault")
	}
	if !journalPending(env) {
		t.Fatal("expected a pending journal")
	}

	res, err := Run(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Recovered {
		t.Error("expected Run to report recovering the pending journal")
	}
	if journalPending(env) {
		t.Error("journal should be cleared after the recovering Run")
	}
}
