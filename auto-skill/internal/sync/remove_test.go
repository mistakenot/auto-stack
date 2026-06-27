package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
)

// TestRemoveLocal: removing an authored skill deletes ./skills/<name>/ and the
// reconcile prunes its rendered target copies (receipts + manifest no longer
// list it), since it has left the desired set.
func TestRemoveLocal(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "keep", "body keep")
	writeAuthoredSkill(t, env, "foo", "v1 body")
	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	targets := resolveTargets(env, nil)
	for _, dir := range targetSkillDirs(targets, "foo") {
		if !strings.Contains(skillBody(t, dir), "v1 body") {
			t.Fatalf("foo not installed at %s", dir)
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

	// Authored source gone.
	if dirExists(filepath.Join(env.SkillsDir(), "foo")) {
		t.Error("./skills/foo should be deleted")
	}
	for _, style := range []string{"claude", "agents"} {
		dir := filepath.Join(env.Root, "."+style, "skills", "foo")
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("target copy %s should be pruned (err=%v)", dir, err)
		}
		if !contains(res.Pruned, style+"/foo") {
			t.Errorf("Pruned missing %s/foo: %v", style, res.Pruned)
		}
		if receiptHas(t, env, style, "foo") {
			t.Errorf("receipts still list pruned %s/foo", style)
		}
	}
	if manifestManages(t, env, "foo") {
		t.Error("manifest still manages the removed foo")
	}
	// keep is untouched.
	for _, dir := range targetSkillDirs(targets, "keep") {
		if !strings.Contains(skillBody(t, dir), "body keep") {
			t.Errorf("keep must survive removal of foo at %s", dir)
		}
	}
}

// TestRemoveVendored: removing a vendored skill drops the lock + skills.yaml
// entry and the reconcile prunes its rendered target copies.
func TestRemoveVendored(t *testing.T) {
	f := newFixture(t)
	commit := f.commitSkill("vend", "v1")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"vend": lockEntry(f.url, "vend", "latest", commit),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{
		Skills: map[string]skill.SkillConfig{"vend": {Version: "latest"}},
	})
	realizeCommit(t, env, f.url, commit)

	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	targets := resolveTargets(env, nil)
	for _, dir := range targetSkillDirs(targets, "vend") {
		if !strings.Contains(skillBody(t, dir), "v1") {
			t.Fatalf("vend not installed at %s", dir)
		}
	}

	res, err := Remove(env, "vend", SelVendored)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !contains(res.Removed, "vendored") {
		t.Errorf("Removed should list vendored, got %v", res.Removed)
	}
	if len(res.Errors) != 0 {
		t.Errorf("unexpected errors: %v", res.Errors)
	}

	// Lock entry gone.
	lock, err := loadLock(env)
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}
	if _, ok := lock.Skills["vend"]; ok {
		t.Error("lock still has the removed vend entry")
	}
	// skills.yaml entry gone.
	syaml, err := loadSkillsYAML(env)
	if err != nil {
		t.Fatalf("loadSkillsYAML: %v", err)
	}
	if _, ok := syaml.Skills["vend"]; ok {
		t.Error("skills.yaml still has the removed vend entry")
	}
	// Targets pruned.
	for _, style := range []string{"claude", "agents"} {
		dir := filepath.Join(env.Root, "."+style, "skills", "vend")
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("target copy %s should be pruned (err=%v)", dir, err)
		}
		if !contains(res.Pruned, style+"/vend") {
			t.Errorf("Pruned missing %s/vend: %v", style, res.Pruned)
		}
	}
}

// TestRemoveAmbiguous: a name existing as BOTH local and vendored with no
// selector is a fail-fast usage error that mutates nothing.
func TestRemoveAmbiguous(t *testing.T) {
	env := newEnv(t)
	writeAuthoredSkill(t, env, "bar", "authored body")
	writeLock(t, env, map[string]skill.LockEntry{
		"bar": lockEntry("https://example.com/repo", "bar", "latest",
			"0123456789abcdef0123456789abcdef01234567"),
	})

	res, err := Remove(env, "bar", SelUnset)
	if err == nil {
		t.Fatal("ambiguous remove must return an error")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Errorf("error should mention the both-exist ambiguity: %v", err)
	}
	if len(res.Removed) != 0 {
		t.Errorf("no source should be removed on error, got %v", res.Removed)
	}
	// No mutation.
	if !dirExists(filepath.Join(env.SkillsDir(), "bar")) {
		t.Error("./skills/bar must survive an ambiguous-selector error")
	}
	lock, err := loadLock(env)
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}
	if _, ok := lock.Skills["bar"]; !ok {
		t.Error("lock entry bar must survive an ambiguous-selector error")
	}
}

// TestRemoveNotFound: removing a name that exists as neither local nor vendored
// is an error.
func TestRemoveNotFound(t *testing.T) {
	env := newEnv(t)
	if _, err := Remove(env, "ghost", SelUnset); err == nil {
		t.Fatal("removing a non-existent skill must return an error")
	}
}

// TestRemoveReportsModifiedCopy: a target copy of the removed skill that was
// locally modified (so its on-disk digest no longer matches the receipt) is
// REPORTED, not deleted — proving the receipt-gated prune authority is reused
// (G-no-foreign-delete).
func TestRemoveReportsModifiedCopy(t *testing.T) {
	env := newEnv(t)
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	writeAuthoredSkill(t, env, "foo", "v1 body")
	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	targets := resolveTargets(env, nil)

	// Edit every rendered copy so its digest drifts from the receipt.
	for _, dir := range targetSkillDirs(targets, "foo") {
		edited := "---\nname: foo\ndescription: Use when testing.\n---\n\nLOCALLY EDITED\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(edited), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	res, err := Remove(env, "foo", SelLocal)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !contains(res.Removed, "local") {
		t.Errorf("Removed should list local, got %v", res.Removed)
	}
	if len(res.Pruned) != 0 {
		t.Errorf("modified copies must not be pruned, got %v", res.Pruned)
	}
	for _, style := range []string{"claude", "agents"} {
		dir := filepath.Join(env.Root, "."+style, "skills", "foo")
		if !strings.Contains(skillBody(t, dir), "LOCALLY EDITED") {
			t.Errorf("modified copy at %s must survive (reported, not deleted)", dir)
		}
		if !contains(res.Reported, style+"/foo") {
			t.Errorf("Reported missing %s/foo: %v", style, res.Reported)
		}
	}
}
