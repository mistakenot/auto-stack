package sync

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
)

// TestEdgeCase_AuthoredShadowsVendored: when a name exists as BOTH a vendored
// (locked) and an authored (./skills/<name>/) skill, the authored copy shadows
// the vendored one — its body is what gets rendered into every target — and the
// sync surfaces a "shadows vendored" advisory warning (never an error).
func TestEdgeCase_AuthoredShadowsVendored(t *testing.T) {
	f := newFixture(t)
	commit := f.commitSkill("foo", "vendored body")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"foo": lockEntry(f.url, "foo", "latest", commit),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{
		Skills: map[string]skill.SkillConfig{"foo": {Version: "latest"}},
	})
	realizeCommit(t, env, f.url, commit)

	// Control: a vendored-only sync renders the vendored body with no warning.
	base, err := Run(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("control Run: %v", err)
	}
	noWarnings(t, base.Warnings)
	for _, dir := range targetSkillDirs(resolveTargets(env, nil), "foo") {
		if !strings.Contains(skillBody(t, dir), "vendored body") {
			t.Fatalf("vendored body should render before the authored copy exists at %s", dir)
		}
	}

	// Now an authored foo shadows the vendored foo.
	writeAuthoredSkill(t, env, "foo", "authored body")

	res, err := Run(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("shadow must not error: %v", res.Errors)
	}

	// Behavior: the authored body (not the vendored one) is what renders.
	targets := resolveTargets(env, nil)
	for _, dir := range targetSkillDirs(targets, "foo") {
		body := skillBody(t, dir)
		if !strings.Contains(body, "authored body") {
			t.Errorf("authored body should render at %s, got %q", dir, body)
		}
		if strings.Contains(body, "vendored body") {
			t.Errorf("vendored body must not render at %s, got %q", dir, body)
		}
	}

	// Diagnostic: the shadow is surfaced as an advisory warning.
	containsWarning(t, res.Warnings, "shadows vendored")
}

// TestEdgeCase_RemoveVendoredWithAuthored: removing the VENDORED source of a name
// that also exists as an authored skill drops the lock entry but leaves the
// authored copy intact — it stays desired, so its rendered target copies survive
// and are reported (never pruned).
func TestEdgeCase_RemoveVendoredWithAuthored(t *testing.T) {
	f := newFixture(t)
	commit := f.commitSkill("foo", "vendored body")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"foo": lockEntry(f.url, "foo", "latest", commit),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{
		Skills: map[string]skill.SkillConfig{"foo": {Version: "latest"}},
	})
	realizeCommit(t, env, f.url, commit)
	writeAuthoredSkill(t, env, "foo", "authored body")

	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}

	res, err := Remove(env, "foo", SelVendored)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !contains(res.Removed, "vendored") {
		t.Errorf("Removed should list vendored, got %v", res.Removed)
	}
	if len(res.Errors) != 0 {
		t.Errorf("unexpected errors: %v", res.Errors)
	}

	// The lock entry for the vendored source is gone.
	lock, err := loadLock(env)
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}
	if _, ok := lock.Skills["foo"]; ok {
		t.Error("lock still has the removed vendored foo entry")
	}

	// The authored source survives on disk.
	if !dirExists(filepath.Join(env.SkillsDir(), "foo")) {
		t.Error("authored ./skills/foo must survive removal of the vendored source")
	}

	// The authored copy is still desired, so its target copies survive and are
	// reported (not pruned).
	if len(res.Pruned) != 0 {
		t.Errorf("nothing should be pruned while authored foo remains, got %v", res.Pruned)
	}
	targets := resolveTargets(env, nil)
	for _, dir := range targetSkillDirs(targets, "foo") {
		if !strings.Contains(skillBody(t, dir), "authored body") {
			t.Errorf("authored copy must survive at %s", dir)
		}
	}
	for _, style := range []string{"claude", "agents"} {
		if !contains(res.Reported, style+"/foo") {
			t.Errorf("Reported missing surviving copy %s/foo: %v", style, res.Reported)
		}
	}
}

// TestEdgeCase_RemoveLocalWithVendored (matrix 3): removing the AUTHORED source of
// a name that also exists as a vendored (locked) skill deletes ./skills/<name>/ but
// leaves the lock entry intact — the name stays desired via the vendored source, so
// the reconcile re-renders the (no-longer-shadowed) vendored body into every target.
func TestEdgeCase_RemoveLocalWithVendored(t *testing.T) {
	f := newFixture(t)
	commit := f.commitSkill("foo", "vendored body")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"foo": lockEntry(f.url, "foo", "latest", commit),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{
		Skills: map[string]skill.SkillConfig{"foo": {Version: "latest"}},
	})
	realizeCommit(t, env, f.url, commit)
	writeAuthoredSkill(t, env, "foo", "authored body")

	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	// Seed: the authored copy shadows the vendored one.
	targets := resolveTargets(env, nil)
	for _, dir := range targetSkillDirs(targets, "foo") {
		if !strings.Contains(skillBody(t, dir), "authored body") {
			t.Fatalf("authored body should render before removal at %s", dir)
		}
	}

	res, err := Remove(env, "foo", SelLocal)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !contains(res.Removed, "local") {
		t.Errorf("Removed should list local, got %v", res.Removed)
	}
	if len(res.Errors) != 0 {
		t.Errorf("unexpected errors: %v", res.Errors)
	}

	// Authored source deleted; vendored lock entry survives.
	if dirExists(filepath.Join(env.SkillsDir(), "foo")) {
		t.Error("authored ./skills/foo must be deleted by a SelLocal remove")
	}
	lock, err := loadLock(env)
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}
	if _, ok := lock.Skills["foo"]; !ok {
		t.Error("vendored lock entry foo must survive removal of the authored source")
	}

	// foo stays desired (now via the vendored source), so nothing is pruned and the
	// vendored body renders on the reconcile.
	if len(res.Pruned) != 0 {
		t.Errorf("nothing should be pruned while vendored foo remains, got %v", res.Pruned)
	}
	for _, dir := range targetSkillDirs(targets, "foo") {
		body := skillBody(t, dir)
		if !strings.Contains(body, "vendored body") {
			t.Errorf("vendored body should render after the authored copy is removed at %s, got %q", dir, body)
		}
		if strings.Contains(body, "authored body") {
			t.Errorf("authored body must not survive at %s", dir)
		}
	}
}

// TestEdgeCase_RemoveAmbiguous (matrix 4): a name existing as BOTH authored and
// vendored with no selector is a fail-fast usage error whose message guides the
// user to disambiguate — and it mutates nothing (lock + authored dir both intact).
func TestEdgeCase_RemoveAmbiguous(t *testing.T) {
	env := newEnv(t)
	writeAuthoredSkill(t, env, "foo", "authored body")
	writeLock(t, env, map[string]skill.LockEntry{
		"foo": lockEntry("https://example.com/repo", "foo", "latest",
			"0123456789abcdef0123456789abcdef01234567"),
	})

	res, err := Remove(env, "foo", SelUnset)
	if err == nil {
		t.Fatal("ambiguous remove (both sources, no selector) must return an error")
	}
	if !strings.Contains(err.Error(), "both") || !strings.Contains(err.Error(), "--local") {
		t.Errorf("error should guide the user (mention 'both' and '--local'): %v", err)
	}
	if len(res.Removed) != 0 {
		t.Errorf("no source should be removed on an ambiguous error, got %v", res.Removed)
	}
	// Nothing mutated.
	if !dirExists(filepath.Join(env.SkillsDir(), "foo")) {
		t.Error("./skills/foo must survive an ambiguous-selector error")
	}
	lock, err := loadLock(env)
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}
	if _, ok := lock.Skills["foo"]; !ok {
		t.Error("lock entry foo must survive an ambiguous-selector error")
	}
}

// TestEdgeCase_AddSameNameAsAuthored (matrix 5): adding a vendored skill whose name
// already exists as an authored skill lands the lock entry, but the authored body
// wins (shadows the vendored copy) and the shadow is surfaced as an advisory.
func TestEdgeCase_AddSameNameAsAuthored(t *testing.T) {
	f := newFixture(t)
	commit := f.commitSkill("foo", "vendored body")

	env := newEnv(t)
	approve(t, env, f.url)
	// Authored foo already exists...
	writeAuthoredSkill(t, env, "foo", "authored body")
	// ...and a vendored foo of the same name is added to the lock + skills.yaml.
	writeLock(t, env, map[string]skill.LockEntry{
		"foo": lockEntry(f.url, "foo", "latest", commit),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{
		Skills: map[string]skill.SkillConfig{"foo": {Version: "latest"}},
	})
	realizeCommit(t, env, f.url, commit)

	res, err := Run(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("adding a vendored name matching an authored skill must not error: %v", res.Errors)
	}

	// The vendored lock entry is present (the add landed).
	lock, err := loadLock(env)
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}
	if _, ok := lock.Skills["foo"]; !ok {
		t.Error("vendored lock entry foo should be present after the add")
	}
	// Authored body wins.
	for _, dir := range targetSkillDirs(resolveTargets(env, nil), "foo") {
		body := skillBody(t, dir)
		if !strings.Contains(body, "authored body") {
			t.Errorf("authored body should win at %s, got %q", dir, body)
		}
		if strings.Contains(body, "vendored body") {
			t.Errorf("vendored body must not render at %s", dir)
		}
	}
	// Diagnostic: the shadow is surfaced as an advisory.
	containsWarning(t, res.Warnings, "shadows vendored")
}

// TestEdgeCase_SyncTargetNotInLock (matrix 6): scoping a sync to a skill name that
// is not in the lock is a no-op — no crash, an empty install plan, and no prune of
// the in-lock-but-out-of-scope skills.
func TestEdgeCase_SyncTargetNotInLock(t *testing.T) {
	f := newFixture(t)
	commit := f.commitSkill("bar", "bar body")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"bar": lockEntry(f.url, "bar", "latest", commit),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{
		Skills: map[string]skill.SkillConfig{"bar": {Version: "latest"}},
	})
	realizeCommit(t, env, f.url, commit)

	res, err := Run(env, Options{Targets: []string{"missing"}})
	if err != nil {
		t.Fatalf("scoping to a missing target must not crash: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Errorf("scoping to an unknown skill should be a no-op, got errors: %v", res.Errors)
	}
	if len(res.Installs) != 0 {
		t.Errorf("expected an empty install plan for an unknown --target, got %v", res.Installs)
	}
	if len(res.Pruned) != 0 {
		t.Errorf("a scoped run must not prune out-of-scope skills, got %v", res.Pruned)
	}
}

// TestEdgeCase_SyncCheckWithPendingJournal (matrix 7): an interrupted commit leaves
// a pending journal; `sync --check` (offline, writes nothing) refuses to run the
// writing recovery and instead fails the gate with a recovery-guiding error.
func TestEdgeCase_SyncCheckWithPendingJournal(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})

	// Simulate an interrupted commit: a non-empty journal awaits recovery.
	if err := writeJournal(env, &journal{Version: journalVersion, Root: env.Root}); err != nil {
		t.Fatalf("seed journal: %v", err)
	}
	if !journalPending(env) {
		t.Fatal("seeded journal should be pending")
	}

	res, err := Run(env, Options{Check: true})
	if err != nil {
		t.Fatalf("check Run should not hard-error on a pending journal: %v", err)
	}
	if res.ExitCode() == 0 {
		t.Error("a pending journal must fail the check gate (non-zero exit)")
	}
	if len(res.Errors) == 0 {
		t.Fatal("expected a pending-journal error under --check")
	}
	found := false
	for _, e := range res.Errors {
		if strings.Contains(e, "recover") {
			found = true
		}
	}
	if !found {
		t.Errorf("check error should guide recovery (contain 'recover'), got %v", res.Errors)
	}
}

// TestEdgeCase_SyncEmptyLockNoAuthored (matrix 8): an empty lock with no authored
// ./skills/ is a clean no-op — exit 0, no errors, no warnings, no crash, nothing to
// install (no empty-manifest problem).
func TestEdgeCase_SyncEmptyLockNoAuthored(t *testing.T) {
	env := newEnv(t)
	writeLock(t, env, map[string]skill.LockEntry{})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	// No ./skills/ authored dir at all.

	res, err := Run(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("empty-lock sync must not crash: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Errorf("empty-lock sync should have no errors, got %v", res.Errors)
	}
	noWarnings(t, res.Warnings)
	if res.ExitCode() != 0 {
		t.Errorf("empty-lock sync should exit 0, got %d", res.ExitCode())
	}
	if len(res.Installs) != 0 {
		t.Errorf("nothing to install for an empty lock, got %v", res.Installs)
	}
}

// TestEdgeCase_SyncLockedIntentChanged (matrix 9): the lock pins a commit while
// skills.yaml declares a different (floating) intent with auto_update off. The lock
// is left untouched and the drift is reported as a stale_by_intent entry.
//
// NOTE: Result.Stale is only populated under --check (computeStale runs solely in
// the offline gate; a plain locked `Run` never fills Stale), so the intent drift is
// observed via `sync --check` — where the lock is likewise never advanced.
func TestEdgeCase_SyncLockedIntentChanged(t *testing.T) {
	const pinned = "0123456789abcdef0123456789abcdef01234567"
	env := newEnv(t)
	writeLock(t, env, map[string]skill.LockEntry{
		"foo": lockEntry("https://example.com/repo", "foo", "commit:"+pinned, pinned),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{
		Skills: map[string]skill.SkillConfig{"foo": {Version: "latest"}},
	})

	res, err := Run(env, Options{Check: true})
	if err != nil {
		t.Fatalf("check Run: %v", err)
	}
	// The lock is untouched (a check writes nothing).
	lock, err := loadLock(env)
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}
	if got := lock.Skills["foo"].Commit; got != pinned {
		t.Errorf("lock commit should be untouched, got %q want %q", got, pinned)
	}
	// Diagnostic: the drift is reported as stale_by_intent.
	found := false
	for _, s := range res.Stale {
		if s.Skill == "foo" && s.Reason == "stale_by_intent" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a stale_by_intent entry for foo, got %+v", res.Stale)
	}
}

// TestEdgeCase_AuthoredInvalidName (matrix 10): an authored dir whose name violates
// the skill-name schema (^[a-z0-9]+(-[a-z0-9]+)*$) is skipped with an error that
// names it, rather than being rendered.
func TestEdgeCase_AuthoredInvalidName(t *testing.T) {
	env := newEnv(t)
	writeLock(t, env, map[string]skill.LockEntry{})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	// An authored dir + SKILL.md name that violates the schema (uppercase + '_').
	writeAuthoredSkill(t, env, "Bad_Name", "body")

	res, err := Run(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("Run should not hard-error on an invalid authored name: %v", err)
	}
	// The invalid skill is skipped with an error naming it.
	found := false
	for _, e := range res.Errors {
		if strings.Contains(e, "Bad_Name") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error naming the invalid authored skill, got %v", res.Errors)
	}
	// It is never installed.
	for _, in := range res.Installs {
		if in.Skill == "Bad_Name" {
			t.Errorf("invalid authored skill must not be installed: %+v", in)
		}
	}
}

// TestEdgeCase_RemoveNonExistent (matrix 11): removing a name that exists as neither
// authored nor vendored is a fail-fast error that names the missing skill and
// mutates nothing.
func TestEdgeCase_RemoveNonExistent(t *testing.T) {
	env := newEnv(t)

	res, err := Remove(env, "ghost", SelUnset)
	if err == nil {
		t.Fatal("removing a non-existent skill must return an error")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should name the missing skill 'ghost': %v", err)
	}
	if len(res.Removed) != 0 {
		t.Errorf("nothing should be removed, got %v", res.Removed)
	}
	// Nothing to mutate: the lock stays empty.
	lock, err := loadLock(env)
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}
	if len(lock.Skills) != 0 {
		t.Errorf("lock should remain empty, got %v", lock.Skills)
	}
}

// TestEdgeCase_ForeignDirCollision (matrix 12): a foreign (unmanaged — no manifest,
// no receipt) dir squatting on a desired skill's target path is refused without
// --force: the collision is reported with a remediation hint and the foreign dir is
// left intact (never overwritten).
func TestEdgeCase_ForeignDirCollision(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	// A desired authored skill...
	writeAuthoredSkill(t, env, "foo", "authored body")
	// ...but a foreign dir already squats on foo's path in every target.
	for _, style := range []string{"claude", "agents"} {
		writeForeignDir(t, env, style, "foo", "foreign body")
	}

	res, err := Run(env, Options{Locked: true})
	if err != nil {
		t.Fatalf("Run must not hard-error on a foreign-dir collision: %v", err)
	}
	// The collision is reported, not silently overwritten.
	if len(res.Conflicts) == 0 {
		t.Fatal("expected a foreign-dir conflict, got none")
	}
	sawFoo := false
	for _, c := range res.Conflicts {
		if c.Skill == "foo" {
			sawFoo = true
		}
	}
	if !sawFoo {
		t.Errorf("expected a conflict for foo, got %+v", res.Conflicts)
	}
	// The refusal carries a remediation hint.
	remediated := false
	for _, e := range res.Errors {
		if strings.Contains(e, "force") || strings.Contains(e, "adopt") {
			remediated = true
		}
	}
	if !remediated {
		t.Errorf("conflict error should include a remediation hint (force/adopt), got %v", res.Errors)
	}
	// Without --force the foreign dirs survive untouched.
	for _, dir := range targetSkillDirs(resolveTargets(env, nil), "foo") {
		if !strings.Contains(skillBody(t, dir), "foreign body") {
			t.Errorf("foreign dir at %s must survive without --force", dir)
		}
	}
}
