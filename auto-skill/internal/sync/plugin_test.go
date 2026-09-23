package sync

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/plugin"
	"github.com/mistakenot/auto-skill/internal/skill"
)

const pluginRoot = "plugins/pw"

func pluginManifest(name string) string {
	return `{"$schema":"` + plugin.SchemaURL + `","name":"` + name + `"}`
}

// commitPluginSkill writes plugins/pw/skills/<name>/SKILL.md (+ manifest) and commits.
func (f *fixture) commitPluginSkill(name, body string) string {
	f.t.Helper()
	f.writeFile(pluginRoot+"/plugin.json", pluginManifest("pw"))
	f.writeFile(pluginRoot+"/skills/"+name+"/SKILL.md", "---\nname: "+name+"\ndescription: Use when testing.\n---\n\n"+body+"\n")
	f.git("add", "-A")
	f.git("commit", "-m", "plugin "+name+" "+body)
	return f.head()
}

func (f *fixture) writeFile(rel, content string) {
	f.t.Helper()
	full := filepath.Join(f.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) removePath(rel string) {
	f.t.Helper()
	if err := os.RemoveAll(filepath.Join(f.dir, filepath.FromSlash(rel))); err != nil {
		f.t.Fatal(err)
	}
}

func pluginEntry(url, spec, commit string) skill.LockEntry {
	return skill.LockEntry{Source: url, URL: url, VersionSpec: spec, Ref: commit, Commit: commit, Subpath: pluginRoot, State: "resolved"}
}

func memberEntry(url, name, spec, commit string) skill.LockEntry {
	e := lockEntry(url, name, spec, commit)
	e.Subpath = pluginRoot + "/skills/" + name
	e.Plugin = "pw"
	return e
}

// writePluginLock writes a lock with plugin pw at commit and the given members.
func writePluginLock(t *testing.T, env skill.Env, url, spec, commit string, members ...string) {
	t.Helper()
	if err := os.MkdirAll(env.SkillsConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := &skill.Lock{Version: 1, Skills: map[string]skill.LockEntry{}, Plugins: map[string]skill.LockEntry{"pw": pluginEntry(url, spec, commit)}}
	for _, m := range members {
		lock.Skills[m] = memberEntry(url, m, spec, commit)
	}
	data, err := skill.EncodeJSON(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.LockPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPluginUpdateTracksMembership: floating a plugin moves every member to the
// new commit, inserts skills the manifest gained, and drops (lock + targets)
// skills it lost.
func TestPluginUpdateTracksMembership(t *testing.T) {
	f := newFixture(t)
	f.commitPluginSkill("alpha", "v1")
	old := f.commitPluginSkill("beta", "v1")

	env := newEnv(t)
	approve(t, env, f.url)
	writePluginLock(t, env, f.url, "latest", old, "alpha", "beta")
	writeSkillsYAML(t, env, &skill.SkillsYAML{Plugins: map[string]skill.PluginConfig{"pw": {Version: "latest"}}})
	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	targets := resolveTargets(env, nil)
	for _, dir := range targetSkillDirs(targets, "beta") {
		if !strings.Contains(skillBody(t, dir), "v1") {
			t.Fatalf("beta not rendered at %s", dir)
		}
	}

	// Upstream: alpha bumped, beta dropped, gamma added.
	f.removePath(pluginRoot + "/skills/beta")
	f.commitPluginSkill("alpha", "v2")
	newSHA := f.commitPluginSkill("gamma", "v1")

	res, err := Run(env, Options{AutoUpdate: true})
	if err != nil {
		t.Fatalf("update Run: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("errors: %v", res.Errors)
	}
	if !res.LockRewritten {
		t.Fatal("lock must be rewritten")
	}

	lock, err := loadLock(env)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Plugins["pw"].Commit != newSHA {
		t.Errorf("plugin commit = %s, want %s", short(lock.Plugins["pw"].Commit), short(newSHA))
	}
	if got := strings.Join(lock.PluginMembers("pw"), ","); got != "alpha,gamma" {
		t.Errorf("members = %s, want alpha,gamma", got)
	}
	if _, ok := lock.Skills["beta"]; ok {
		t.Error("beta must leave the lock")
	}
	g := lock.Skills["gamma"]
	if g.Commit != newSHA || g.Plugin != "pw" || g.Subpath != pluginRoot+"/skills/gamma" || g.VersionSpec != "latest" {
		t.Errorf("gamma entry = %+v", g)
	}
	if lock.Skills["alpha"].Commit != newSHA {
		t.Errorf("alpha commit = %s, want %s", short(lock.Skills["alpha"].Commit), short(newSHA))
	}
	for _, dir := range targetSkillDirs(targets, "alpha") {
		if !strings.Contains(skillBody(t, dir), "v2") {
			t.Errorf("alpha not re-rendered at %s", dir)
		}
	}
	for _, dir := range targetSkillDirs(targets, "gamma") {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("gamma not rendered at %s", dir)
		}
	}
	for _, dir := range targetSkillDirs(targets, "beta") {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("beta should be pruned at %s", dir)
		}
	}
	// The plan carries exactly the post-update members; the dropped beta is
	// absent (it is pruned by the ownership pass, not planned).
	names := make([]string, 0, len(res.Plan))
	for _, sp := range res.Plan {
		names = append(names, sp.Name)
	}
	if strings.Join(names, ",") != "alpha,gamma" {
		t.Fatalf("plan = %v", names)
	}
}

// TestPluginPlainSyncNeverFloats: an ordinary sync leaves a floating plugin at
// its locked commit and renders the locked members.
func TestPluginPlainSyncNeverFloats(t *testing.T) {
	f := newFixture(t)
	old := f.commitPluginSkill("alpha", "v1")
	f.commitPluginSkill("alpha", "v2")

	env := newEnv(t)
	approve(t, env, f.url)
	writePluginLock(t, env, f.url, "latest", old, "alpha")
	writeSkillsYAML(t, env, &skill.SkillsYAML{Plugins: map[string]skill.PluginConfig{"pw": {Version: "latest"}}})
	before := readLockBytes(t, env)

	res, err := Run(env, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.LockRewritten || !bytes.Equal(readLockBytes(t, env), before) {
		t.Fatal("plain sync must not move the plugin")
	}
	for _, dir := range targetSkillDirs(resolveTargets(env, nil), "alpha") {
		if !strings.Contains(skillBody(t, dir), "v1") {
			t.Fatalf("alpha should render the locked v1 at %s", dir)
		}
	}
}

// TestPluginIntentChangeReported: skills.yaml plugin version differing from the
// lock is reported (not applied) under a plain sync, naming the plugin.
func TestPluginIntentChangeReported(t *testing.T) {
	f := newFixture(t)
	old := f.commitPluginSkill("alpha", "v1")

	env := newEnv(t)
	approve(t, env, f.url)
	writePluginLock(t, env, f.url, "latest", old, "alpha")
	writeSkillsYAML(t, env, &skill.SkillsYAML{Plugins: map[string]skill.PluginConfig{"pw": {Version: "branch:other"}}})

	plan, err := BuildPlan(env, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plugins) != 1 || plan.Plugins[0].Action != ActionIntentChanged {
		t.Fatalf("plugin plan = %+v", plan.Plugins)
	}
	if len(plan.Skills) != 1 || plan.Skills[0].Action != ActionIntentChanged || !strings.Contains(plan.Skills[0].Message, "update pw") {
		t.Fatalf("member plan = %+v", plan.Skills)
	}
}

// TestPluginScopedUpdateByName: `update pw` (or `update alpha`) floats the whole
// plugin, and a plain --target scope stays locked.
func TestPluginScopedUpdateByName(t *testing.T) {
	f := newFixture(t)
	old := f.commitPluginSkill("alpha", "v1")
	f.commitPluginSkill("beta", "v1")
	newSHA := f.commitPluginSkill("alpha", "v2")

	{
		env := newEnv(t)
		approve(t, env, f.url)
		writePluginLock(t, env, f.url, "latest", old, "alpha")
		writeSkillsYAML(t, env, &skill.SkillsYAML{Plugins: map[string]skill.PluginConfig{"pw": {Version: "latest"}}})

		res, err := Run(env, Options{Targets: []string{"pw"}, AutoUpdate: true, Update: true})
		if err != nil {
			t.Fatalf("update pw: %v", err)
		}
		lock, _ := loadLock(env)
		if lock.Plugins["pw"].Commit != newSHA || strings.Join(lock.PluginMembers("pw"), ",") != "alpha,beta" {
			t.Errorf("update pw: plugin=%s members=%v errors=%v", short(lock.Plugins["pw"].Commit), lock.PluginMembers("pw"), res.Errors)
		}
	}

	// Member name expands to the plugin.
	env := newEnv(t)
	approve(t, env, f.url)
	writePluginLock(t, env, f.url, "latest", old, "alpha")
	writeSkillsYAML(t, env, &skill.SkillsYAML{Plugins: map[string]skill.PluginConfig{"pw": {Version: "latest"}}})
	if got := ExpandTargets(env, []string{"alpha"}); strings.Join(got, ",") != "alpha,pw" {
		t.Fatalf("ExpandTargets(alpha) = %v", got)
	}
	if _, err := Run(env, Options{Targets: []string{"alpha"}, AutoUpdate: true, Update: true}); err != nil {
		t.Fatal(err)
	}
	lock, _ := loadLock(env)
	if lock.Plugins["pw"].Commit != newSHA || strings.Join(lock.PluginMembers("pw"), ",") != "alpha,beta" {
		t.Errorf("update alpha: plugin=%s members=%v", short(lock.Plugins["pw"].Commit), lock.PluginMembers("pw"))
	}

	// A plain --target (no Update) stays locked even with AutoUpdate.
	env2 := newEnv(t)
	approve(t, env2, f.url)
	writePluginLock(t, env2, f.url, "latest", old, "alpha")
	writeSkillsYAML(t, env2, &skill.SkillsYAML{Plugins: map[string]skill.PluginConfig{"pw": {Version: "latest"}}})
	realizeCommit(t, env2, f.url, old)
	res, err := Run(env2, Options{Targets: []string{"pw"}, AutoUpdate: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Locked || res.LockRewritten {
		t.Fatalf("--target without Update must stay locked: %+v", res)
	}
}

// TestPluginManifestGoneIsUnavailable: an update whose new commit no longer has
// the manifest (moved/renamed upstream) fails the plugin without touching the
// lock or pruning its members.
func TestPluginManifestGoneIsUnavailable(t *testing.T) {
	f := newFixture(t)
	old := f.commitPluginSkill("alpha", "v1")

	env := newEnv(t)
	approve(t, env, f.url)
	writePluginLock(t, env, f.url, "latest", old, "alpha")
	writeSkillsYAML(t, env, &skill.SkillsYAML{Plugins: map[string]skill.PluginConfig{"pw": {Version: "latest"}}})
	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatal(err)
	}
	before := readLockBytes(t, env)

	f.removePath(pluginRoot + "/plugin.json")
	f.git("add", "-A")
	f.git("commit", "-m", "drop manifest")

	res, err := Run(env, Options{AutoUpdate: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Errors) == 0 || !strings.Contains(strings.Join(res.Errors, "\n"), "re-add") {
		t.Fatalf("expected an unavailable-plugin error with a re-add hint, got %v", res.Errors)
	}
	if res.LockRewritten || !bytes.Equal(readLockBytes(t, env), before) {
		t.Fatal("lock must be untouched")
	}
	for _, dir := range targetSkillDirs(resolveTargets(env, nil), "alpha") {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("alpha must not be pruned on an unavailable plugin: %s", dir)
		}
	}

	// A renamed manifest is likewise refused.
	f.writeFile(pluginRoot+"/plugin.json", pluginManifest("renamed"))
	f.git("add", "-A")
	f.git("commit", "-m", "rename")
	res, err = Run(env, Options{AutoUpdate: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) == 0 || !strings.Contains(strings.Join(res.Errors, "\n"), `declares name "renamed"`) {
		t.Fatalf("expected rename refusal, got %v", res.Errors)
	}
}

// TestRemovePlugin drops the plugin, its members, both skills.yaml entries, and
// prunes every member's target copies.
func TestRemovePlugin(t *testing.T) {
	f := newFixture(t)
	f.commitPluginSkill("alpha", "v1")
	commit := f.commitPluginSkill("beta", "v1")

	env := newEnv(t)
	approve(t, env, f.url)
	writePluginLock(t, env, f.url, "latest", commit, "alpha", "beta")
	writeSkillsYAML(t, env, &skill.SkillsYAML{
		Plugins: map[string]skill.PluginConfig{"pw": {Version: "latest"}},
		Skills:  map[string]skill.SkillConfig{"alpha": {}}, // replacements-only member stub
	})
	if _, err := Run(env, Options{Locked: true}); err != nil {
		t.Fatal(err)
	}

	// A member cannot be removed on its own.
	if _, err := Remove(env, "alpha", SelUnset); err == nil || !strings.Contains(err.Error(), "remove pw --plugin") {
		t.Fatalf("member remove should be refused with a hint, got %v", err)
	}

	res, err := Remove(env, "pw", SelUnset)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !contains(res.Removed, "plugin") || strings.Join(res.Skills, ",") != "alpha,beta" || len(res.Errors) != 0 {
		t.Fatalf("result = %+v", res)
	}
	lock, _ := loadLock(env)
	if len(lock.Plugins) != 0 || len(lock.Skills) != 0 {
		t.Fatalf("lock not emptied: %+v", lock)
	}
	syaml, _ := loadSkillsYAML(env)
	if len(syaml.Plugins) != 0 || len(syaml.Skills) != 0 {
		t.Fatalf("skills.yaml not cleaned: %+v", syaml)
	}
	for _, name := range []string{"alpha", "beta"} {
		for _, style := range []string{"claude", "agents"} {
			dir := filepath.Join(env.Root, "."+style, "skills", name)
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Errorf("%s should be pruned", dir)
			}
			if !contains(res.Pruned, style+"/"+name) {
				t.Errorf("Pruned missing %s/%s: %v", style, name, res.Pruned)
			}
		}
	}
	if _, err := Remove(env, "pw", SelPlugin); err == nil {
		t.Fatal("second remove must report not found")
	}
}

// TestPluginStaleRefAndValidation: the lock validator ties members to plugins,
// and skills.yaml plugin refs without a lock entry are reported stale.
func TestPluginStaleRefAndValidation(t *testing.T) {
	env := newEnv(t)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": memberEntry("https://example.com/r", "alpha", "latest", "0123456789abcdef0123456789abcdef01234567"),
	})
	lock, err := loadLock(env)
	if err != nil {
		t.Fatal(err)
	}
	errs := skill.ValidateLock(lock)
	if len(errs) != 1 || errs[0].Code != skill.CodeUnknownPluginRef {
		t.Fatalf("expected one unknown_plugin_ref, got %v", errs)
	}

	writeSkillsYAML(t, env, &skill.SkillsYAML{Plugins: map[string]skill.PluginConfig{"gone": {Version: "latest"}}})
	diags, err := skill.CheckStaleSkillRefs(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 || diags[0].Code != "stale_plugin_ref" {
		t.Fatalf("diags = %+v", diags)
	}

	if _, err := skill.ParseLock([]byte(`{"version":1,"skills":{},"plugins":{"Bad_Name":{}}}`)); err == nil {
		t.Fatal("invalid plugin key must fail to parse")
	}
}
