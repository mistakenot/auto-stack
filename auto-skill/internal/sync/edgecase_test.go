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
