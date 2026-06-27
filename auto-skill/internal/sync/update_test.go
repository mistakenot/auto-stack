package sync

import (
	"bytes"
	"os"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
)

func changedCommit(res *UpdateResult, name string) (string, bool) {
	for i := range res.Changed {
		if res.Changed[i].Name == name {
			return res.Changed[i].TargetCommit, true
		}
	}
	return "", false
}

// TestUpdateFloatsLatest: update advances a `latest` spec to the newest HEAD.
func TestUpdateFloatsLatest(t *testing.T) {
	f := newFixture(t)
	old := f.commitSkill("alpha", "v1")
	newSHA := f.commitSkill("alpha", "v2")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "latest", old),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})

	res, err := Update(env, nil, false)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, ok := changedCommit(res, "alpha")
	if !ok {
		t.Fatal("expected alpha to be reported as changed")
	}
	if got != newSHA {
		t.Fatalf("expected float to %s, got %s", short(newSHA), short(got))
	}
}

// TestUpdateTagOnlyOnExplicit: a tag moves only on an explicit update; a plain
// sync leaves the tag's pinned commit alone.
func TestUpdateTagOnlyOnExplicit(t *testing.T) {
	f := newFixture(t)
	old := f.commitSkill("alpha", "v1")
	f.tag("v1") // v1 -> old
	newSHA := f.commitSkill("alpha", "v2")
	f.tag("v1") // force-move v1 -> newSHA

	mkLock := func() map[string]skill.LockEntry {
		return map[string]skill.LockEntry{
			"alpha": lockEntry(f.url, "alpha", "tag:v1", old),
		}
	}

	// Plain sync (auto_update) must NOT move the tag.
	envS := newEnv(t)
	approve(t, envS, f.url)
	writeLock(t, envS, mkLock())
	writeSkillsYAML(t, envS, &skill.SkillsYAML{AutoUpdate: true})
	planS, err := BuildPlan(envS, Options{AutoUpdate: true})
	if err != nil {
		t.Fatalf("BuildPlan sync: %v", err)
	}
	spS, _ := findSkill(planS, "alpha")
	if spS.TargetCommit != old {
		t.Fatalf("sync must not move the tag: expected %s, got %s", short(old), short(spS.TargetCommit))
	}

	// Explicit update re-resolves the tag (peeled) and warns on the force-move.
	envU := newEnv(t)
	approve(t, envU, f.url)
	writeLock(t, envU, mkLock())
	writeSkillsYAML(t, envU, &skill.SkillsYAML{})
	res, err := Update(envU, nil, false)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, ok := changedCommit(res, "alpha")
	if !ok || got != newSHA {
		t.Fatalf("explicit update should move tag to %s, got %s ok=%v", short(newSHA), short(got), ok)
	}
	spU, _ := findSkill(res.Plan, "alpha")
	if spU.Warning == "" {
		t.Fatal("expected a force-move warning on the tag")
	}
}

// TestUpdateShaNeverFloats: a pinned commit: spec never moves, even on update.
func TestUpdateShaNeverFloats(t *testing.T) {
	f := newFixture(t)
	old := f.commitSkill("alpha", "v1")
	f.commitSkill("alpha", "v2")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "commit:"+old, old),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})

	res, err := Update(env, nil, false)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, ok := changedCommit(res, "alpha"); ok {
		t.Fatal("a pinned <sha> must never float")
	}
	sp, _ := findSkill(res.Plan, "alpha")
	if sp.TargetCommit != old {
		t.Fatalf("expected target to stay %s, got %s", short(old), short(sp.TargetCommit))
	}
}

// TestUpdateCheckWritesNothing: --check fetches + compares but writes no files.
func TestUpdateCheckWritesNothing(t *testing.T) {
	f := newFixture(t)
	old := f.commitSkill("alpha", "v1")
	newSHA := f.commitSkill("alpha", "v2")

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "latest", old),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})

	lockBefore := readFile(t, env.LockPath())
	yamlBefore := readFile(t, env.SkillsYAMLPath())

	res, err := Update(env, nil, true)
	if err != nil {
		t.Fatalf("Update --check: %v", err)
	}
	got, ok := changedCommit(res, "alpha")
	if !ok || got != newSHA {
		t.Fatalf("--check should still report drift to %s, got %s ok=%v", short(newSHA), short(got), ok)
	}
	sp, _ := findSkill(res.Plan, "alpha")
	if sp.LockRewrite {
		t.Fatal("--check must not mark lock rewrites")
	}
	if !bytes.Equal(readFile(t, env.LockPath()), lockBefore) {
		t.Fatal("--check rewrote lock.json")
	}
	if !bytes.Equal(readFile(t, env.SkillsYAMLPath()), yamlBefore) {
		t.Fatal("--check rewrote skills.yaml")
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
