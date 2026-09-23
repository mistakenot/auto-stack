package add

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/plugin"
	"github.com/mistakenot/auto-skill/internal/skill"
)

func pluginManifest(name string) string {
	return `{"$schema":"` + plugin.SchemaURL + `","name":"` + name + `","description":"Bundle ` + name + `"}`
}

// makePluginFixture creates a git repo shaped like an agent-plugins.org
// multi-plugin source: root skills/ copies plus plugins/<name>/{plugin.json,skills/}.
func makePluginFixture(t *testing.T) (repoDir, fileURL string) {
	t.Helper()
	dir, url := makeGitFixture(t, map[string]string{
		"skills/new-task": skillMD("new-task", "Root copy."),
		"plugins/planning-workflow/skills/new-task": skillMD("new-task", "Plan a task."),
		"plugins/planning-workflow/skills/new-plan": skillMD("new-plan", "Plan the plan."),
		"plugins/ideation/skills/ten-ideas":         skillMD("ten-ideas", "Ideate."),
	})
	writeRepoFile(t, dir, "plugins/planning-workflow/plugin.json", pluginManifest("planning-workflow"))
	writeRepoFile(t, dir, "plugins/planning-workflow/mcp.json", `{"mcpServers":{}}`)
	writeRepoFile(t, dir, "plugins/planning-workflow/skills/README.md", "not a skill dir")
	writeRepoFile(t, dir, "plugins/ideation/plugin.json", pluginManifest("ideation"))
	writeRepoFile(t, dir, "plugins/no-manifest/skills/x/SKILL.md", skillMD("x", "Orphan."))
	runGitFixture(t, dir, "add", "-A")
	runGitFixture(t, dir, "commit", "-m", "plugins")
	return dir, url
}

func writeRepoFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPluginAddByPath(t *testing.T) {
	_, fileURL := makePluginFixture(t)
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	res, err := Run(env, Options{Source: fileURL, Plugin: "plugins/planning-workflow", TrustRequested: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Plugin == nil || res.Plugin.Name != "planning-workflow" || res.Plugin.Root != "plugins/planning-workflow" {
		t.Fatalf("plugin = %+v", res.Plugin)
	}
	if got := strings.Join(res.Plugin.Skills, ","); got != "new-plan,new-task" {
		t.Fatalf("plugin skills = %s", got)
	}
	if len(res.Added) != 2 || res.Added[0].Plugin != "planning-workflow" || res.Added[1].Subpath != "plugins/planning-workflow/skills/new-task" {
		t.Fatalf("added = %+v", res.Added)
	}

	lock := readLock(t, env)
	pe, ok := lock.Plugins["planning-workflow"]
	if !ok || pe.Subpath != "plugins/planning-workflow" || pe.State != "resolved" || pe.Commit != res.Plugin.Commit || pe.VersionSpec != "latest" {
		t.Fatalf("lock plugin entry = %+v ok=%t", pe, ok)
	}
	for _, name := range []string{"new-task", "new-plan"} {
		e, ok := lock.Skills[name]
		if !ok || e.Plugin != "planning-workflow" || e.Commit != pe.Commit || !strings.HasPrefix(e.Subpath, "plugins/planning-workflow/skills/") {
			t.Fatalf("lock skill %s = %+v ok=%t", name, e, ok)
		}
	}
	if got := lock.PluginMembers("planning-workflow"); strings.Join(got, ",") != "new-plan,new-task" {
		t.Fatalf("members = %v", got)
	}

	syaml := readSkillsYAML(t, env)
	if pc, ok := syaml.Plugins["planning-workflow"]; !ok || pc.Version != "latest" {
		t.Fatalf("skills.yaml plugins = %+v", syaml.Plugins)
	}
	if _, ok := syaml.Skills["new-task"]; ok {
		t.Fatal("plugin members must not get skills.<name> stubs (the plugin carries the version)")
	}
	if errs := skill.ValidateLock(lock); len(errs) != 0 {
		t.Fatalf("lock invalid: %v", errs)
	}
}

func TestPluginAddByNameAndVersion(t *testing.T) {
	_, fileURL := makePluginFixture(t)
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	res, err := Run(env, Options{Source: fileURL, Plugin: "ideation", Version: "branch:master", TrustRequested: true})
	if err != nil {
		// Some git configs default to "main".
		res, err = Run(env, Options{Source: fileURL, Plugin: "ideation", Version: "branch:main", TrustRequested: true})
	}
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Plugin.Root != "plugins/ideation" || strings.Join(res.Plugin.Skills, ",") != "ten-ideas" {
		t.Fatalf("plugin = %+v", res.Plugin)
	}
	if !strings.HasPrefix(res.Plugin.VersionSpec, "branch:") {
		t.Fatalf("version_spec = %q", res.Plugin.VersionSpec)
	}
	syaml := readSkillsYAML(t, env)
	if syaml.Plugins["ideation"].Version != res.Plugin.VersionSpec {
		t.Fatalf("skills.yaml plugin version = %q, want %q", syaml.Plugins["ideation"].Version, res.Plugin.VersionSpec)
	}
}

func TestPluginList(t *testing.T) {
	_, fileURL := makePluginFixture(t)
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	res, err := Run(env, Options{Source: fileURL, Plugin: "planning-workflow", List: true, TrustRequested: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Listed) != 2 || res.Listed[0].Container != "plugin:planning-workflow" || res.Listed[0].Subpath != "plugins/planning-workflow/skills/new-plan" {
		t.Fatalf("listed = %+v", res.Listed)
	}
	if res.Plugin == nil || res.Plugin.Description != "Bundle planning-workflow" {
		t.Fatalf("plugin = %+v", res.Plugin)
	}
	if _, err := os.Stat(env.LockPath()); !os.IsNotExist(err) {
		t.Fatal("--list must not write the lock")
	}
}

func TestPluginAddErrors(t *testing.T) {
	_, fileURL := makePluginFixture(t)
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	var ae *AddError
	cases := []struct {
		label string
		opts  Options
		code  string // AddError code, or "" for a plugin.ManifestError
		pcode string
	}{
		{"with --skill", Options{Plugin: "ideation", Skills: []string{"x"}}, CodePluginFlags, ""},
		{"with --path", Options{Plugin: "ideation", Paths: []string{"x"}}, CodePluginFlags, ""},
		{"with --as", Options{Plugin: "ideation", As: "y"}, CodePluginFlags, ""},
		{"with --full-depth", Options{Plugin: "ideation", FullDepth: true}, CodePluginFlags, ""},
		{"no manifest", Options{Plugin: "plugins/no-manifest"}, "", plugin.CodeManifestMissing},
		{"unknown name", Options{Plugin: "nope"}, "", plugin.CodeNotFound},
	}
	for _, tc := range cases {
		tc.opts.Source = fileURL
		tc.opts.TrustRequested = true
		_, err := Run(env, tc.opts)
		if err == nil {
			t.Errorf("%s: expected error", tc.label)
			continue
		}
		if tc.code != "" {
			if !asAddError(err, &ae) || ae.Code != tc.code {
				t.Errorf("%s: got %v, want AddError %s", tc.label, err, tc.code)
			}
			continue
		}
		var me *plugin.ManifestError
		if !asManifestError(err, &me) || me.Code != tc.pcode {
			t.Errorf("%s: got %v, want ManifestError %s", tc.label, err, tc.pcode)
		}
	}
	if _, err := os.Stat(env.LockPath()); !os.IsNotExist(err) {
		t.Fatal("a failed plugin add must not write the lock")
	}
}

func TestPluginAddInvalidManifestRejectsWholePlugin(t *testing.T) {
	dir, fileURL := makeGitFixture(t, map[string]string{
		"plugins/bad/skills/fine": skillMD("fine", "Fine skill."),
	})
	writeRepoFile(t, dir, "plugins/bad/plugin.json", `{"$schema":"https://example.com/other.json","name":"bad"}`)
	runGitFixture(t, dir, "add", "-A")
	runGitFixture(t, dir, "commit", "-m", "bad manifest")
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	_, err := Run(env, Options{Source: fileURL, Plugin: "plugins/bad", TrustRequested: true})
	if err == nil || !strings.Contains(err.Error(), "$schema") {
		t.Fatalf("expected a $schema rejection, got %v", err)
	}
	if _, err := os.Stat(env.LockPath()); !os.IsNotExist(err) {
		t.Fatal("nothing must be written for a rejected plugin")
	}
}

func TestPluginAddNoSkills(t *testing.T) {
	dir, fileURL := makeGitFixture(t, map[string]string{
		"skills/other": skillMD("other", "Unrelated."),
	})
	writeRepoFile(t, dir, "plugins/empty/plugin.json", pluginManifest("empty"))
	runGitFixture(t, dir, "add", "-A")
	runGitFixture(t, dir, "commit", "-m", "empty plugin")
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)

	var ae *AddError
	_, err := Run(env, Options{Source: fileURL, Plugin: "empty", TrustRequested: true})
	if !asAddError(err, &ae) || ae.Code != CodePluginNoSkills {
		t.Fatalf("got %v, want %s", err, CodePluginNoSkills)
	}
}

func TestPluginAddCollisions(t *testing.T) {
	_, fileURL := makePluginFixture(t)
	_, otherURL := makeGitFixture(t, map[string]string{
		"skills/new-task": skillMD("new-task", "Someone else's new-task."),
	})
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)
	approveEndpoint(t, env, otherURL)

	// A standalone new-task from another source blocks the plugin.
	if _, err := Run(env, Options{Source: otherURL, Skills: []string{"new-task"}, TrustRequested: true}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var ae *AddError
	_, err := Run(env, Options{Source: fileURL, Plugin: "planning-workflow", TrustRequested: true})
	if !asAddError(err, &ae) || ae.Code != CodeNameCollision {
		t.Fatalf("got %v, want %s", err, CodeNameCollision)
	}
	if _, ok := readLock(t, env).Plugins["planning-workflow"]; ok {
		t.Fatal("collision must not write the plugin")
	}

	// A standalone new-task from the SAME source is taken over by the plugin.
	env2 := makeEnv(t)
	approveEndpoint(t, env2, fileURL)
	if _, err := Run(env2, Options{Source: fileURL, Skills: []string{"new-task"}, TrustRequested: true}); err != nil {
		t.Fatalf("seed same source: %v", err)
	}
	if _, err := Run(env2, Options{Source: fileURL, Plugin: "planning-workflow", TrustRequested: true}); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	e := readLock(t, env2).Skills["new-task"]
	if e.Plugin != "planning-workflow" || e.Subpath != "plugins/planning-workflow/skills/new-task" {
		t.Fatalf("new-task should now be a plugin member: %+v", e)
	}
}

func TestPluginReAddDropsRemovedMembers(t *testing.T) {
	dir, fileURL := makePluginFixture(t)
	env := makeEnv(t)
	approveEndpoint(t, env, fileURL)
	if _, err := Run(env, Options{Source: fileURL, Plugin: "planning-workflow", TrustRequested: true}); err != nil {
		t.Fatalf("first add: %v", err)
	}

	// Upstream drops new-plan and adds new-solution.
	if err := os.RemoveAll(filepath.Join(dir, "plugins/planning-workflow/skills/new-plan")); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, "plugins/planning-workflow/skills/new-solution/SKILL.md", skillMD("new-solution", "Solve."))
	runGitFixture(t, dir, "add", "-A")
	runGitFixture(t, dir, "commit", "-m", "swap members")

	res, err := Run(env, Options{Source: fileURL, Plugin: "planning-workflow", TrustRequested: true})
	if err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if strings.Join(res.Plugin.Skills, ",") != "new-solution,new-task" || strings.Join(res.Plugin.Removed, ",") != "new-plan" {
		t.Fatalf("plugin = %+v", res.Plugin)
	}
	lock := readLock(t, env)
	if _, ok := lock.Skills["new-plan"]; ok {
		t.Fatal("dropped member must leave the lock")
	}
	if lock.Skills["new-solution"].Plugin != "planning-workflow" {
		t.Fatal("new member must be a plugin member")
	}
}

func TestPluginAddLocalGit(t *testing.T) {
	dir, _ := makePluginFixture(t)
	env := makeEnv(t)

	res, err := Run(env, Options{Source: dir, Plugin: "plugins/planning-workflow"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Plugin == nil || !res.Plugin.Local || len(res.Added) != 2 || !res.Added[0].Local {
		t.Fatalf("local plugin add = %+v added=%+v", res.Plugin, res.Added)
	}
	lock := readLock(t, env)
	pe := lock.Plugins["planning-workflow"]
	if !pe.Local || !strings.HasPrefix(pe.URL, "file://") || pe.Subpath != "plugins/planning-workflow" {
		t.Fatalf("plugin entry = %+v", pe)
	}
	if lock.Skills["new-task"].Plugin != "planning-workflow" || !lock.Skills["new-task"].Local {
		t.Fatalf("member = %+v", lock.Skills["new-task"])
	}
}

func TestPluginAddPlainDirRejected(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, "plugin.json", pluginManifest("plain"))
	writeRepoFile(t, dir, "skills/a/SKILL.md", skillMD("a", "A."))
	env := makeEnv(t)

	var ae *AddError
	_, err := Run(env, Options{Source: dir, Plugin: "."})
	if !asAddError(err, &ae) || ae.Code != CodePluginFlags {
		t.Fatalf("got %v, want %s", err, CodePluginFlags)
	}
}

func asManifestError(err error, target **plugin.ManifestError) bool {
	return errors.As(err, target)
}
