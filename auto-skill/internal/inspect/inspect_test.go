package inspect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
	"gopkg.in/yaml.v3"
)

// skillMD returns a minimal valid SKILL.md body.
func skillMD(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n## Workflow\n\n1. Do the thing.\n"
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeRenderedSkill writes a SKILL.md into a target dir and returns the tree's
// digest, so a matching manifest entry can be seeded for a non-stale fixture.
func writeRenderedSkill(t *testing.T, root, target, name, content string) string {
	t.Helper()
	dir := filepath.Join(targetDir(root, target), name)
	writeFile(t, filepath.Join(dir, "SKILL.md"), content)
	digest, exists, err := treeDigest(dir)
	if err != nil || !exists {
		t.Fatalf("treeDigest(%s): exists=%v err=%v", dir, exists, err)
	}
	return digest
}

func writeLock(t *testing.T, env skill.Env, entries map[string]skill.LockEntry) {
	t.Helper()
	data, err := skill.EncodeJSON(&skill.Lock{Version: 1, Skills: entries})
	if err != nil {
		t.Fatalf("encode lock: %v", err)
	}
	writeFileBytes(t, env.LockPath(), data)
}

func writeManifest(t *testing.T, env skill.Env, m *skill.Manifest) {
	t.Helper()
	data, err := skill.EncodeJSON(m)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	writeFileBytes(t, env.ManifestPath(), data)
}

func writeYAML(t *testing.T, env skill.Env, cfg *skill.SkillsYAML) {
	t.Helper()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal skills.yaml: %v", err)
	}
	writeFileBytes(t, env.SkillsYAMLPath(), data)
}

func writeFileBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// seedProject builds a project with one authored skill (alpha) and one vendored
// skill (deploy) rendered into both targets with a matching (non-stale) manifest.
func seedProject(t *testing.T) skill.Env {
	t.Helper()
	root := t.TempDir()
	env := skill.Env{Root: root}

	writeYAML(t, env, &skill.SkillsYAML{Targets: []string{"claude", "agents"}})

	// Authored skill alpha.
	writeFile(t, filepath.Join(env.SkillsDir(), "alpha", "SKILL.md"),
		skillMD("alpha", "Use when authoring alpha."))

	// Vendored skill deploy: rendered into both targets, lock + manifest seeded.
	deployMD := skillMD("deploy", "Use when deploying to prod.")
	digClaude := writeRenderedSkill(t, root, "claude", "deploy", deployMD)
	digAgents := writeRenderedSkill(t, root, "agents", "deploy", deployMD)

	writeLock(t, env, map[string]skill.LockEntry{
		"deploy": {
			Source:      "github.com/acme/skills",
			URL:         "https://github.com/acme/skills",
			VersionSpec: "latest",
			Ref:         "main",
			Commit:      "abc123",
			State:       "ready",
		},
	})
	writeManifest(t, env, &skill.Manifest{
		Version: 1,
		Skills: map[string]skill.ManifestSkill{
			"deploy": {SkillVersion: digClaude, Replacements: map[string]string{"REGION": "us-east-1"}},
		},
		Targets: map[string]skill.ManifestTarget{
			"claude": {ManagedSkills: map[string]string{"deploy": digClaude}},
			"agents": {ManagedSkills: map[string]string{"deploy": digAgents}},
		},
	})
	return env
}

func viewByName(views []SkillView, name string) (SkillView, bool) {
	for _, v := range views {
		if v.Name == name {
			return v, true
		}
	}
	return SkillView{}, false
}

func TestInspectJoinsAuthoredAndVendored(t *testing.T) {
	env := seedProject(t)
	views, parseErrors, err := Inspect(env, Filter{})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(parseErrors) != 0 {
		t.Fatalf("unexpected parse errors: %v", parseErrors)
	}
	if len(views) != 2 {
		t.Fatalf("got %d views, want 2: %+v", len(views), views)
	}

	alpha, ok := viewByName(views, "alpha")
	if !ok {
		t.Fatal("missing alpha view")
	}
	if alpha.Origin != OriginLocal {
		t.Errorf("alpha origin = %q, want local", alpha.Origin)
	}
	if alpha.Stale != nil {
		t.Errorf("alpha stale = %v, want nil (no manifest entry)", *alpha.Stale)
	}

	deploy, ok := viewByName(views, "deploy")
	if !ok {
		t.Fatal("missing deploy view")
	}
	if deploy.Origin != OriginVendored {
		t.Errorf("deploy origin = %q, want vendored", deploy.Origin)
	}
	if deploy.Description != "Use when deploying to prod." {
		t.Errorf("deploy description = %q", deploy.Description)
	}
	if deploy.Stale == nil || *deploy.Stale {
		t.Errorf("deploy stale = %v, want false (digest matches)", deploy.Stale)
	}
	if deploy.SkillVersion == "" {
		t.Error("deploy skill_version is empty")
	}
}

func TestInspectStaleWhenDigestMismatch(t *testing.T) {
	env := seedProject(t)
	// Mutate the rendered claude tree so its digest no longer matches the manifest.
	writeFile(t, filepath.Join(targetDir(env.Root, "claude"), "deploy", "SKILL.md"),
		skillMD("deploy", "Use when deploying to prod. EDITED."))

	views, _, err := Inspect(env, Filter{})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	deploy, _ := viewByName(views, "deploy")
	if deploy.Stale == nil || !*deploy.Stale {
		t.Errorf("deploy stale = %v, want true (digest mismatch)", deploy.Stale)
	}
}

func TestInspectStaleWhenTreeMissing(t *testing.T) {
	env := seedProject(t)
	// Remove the rendered tree entirely → stale (not present).
	if err := os.RemoveAll(filepath.Join(targetDir(env.Root, "claude"), "deploy")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	views, _, err := Inspect(env, Filter{})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	deploy, _ := viewByName(views, "deploy")
	if deploy.Stale == nil || !*deploy.Stale {
		t.Errorf("deploy stale = %v, want true (tree absent)", deploy.Stale)
	}
}

func TestInspectNoManifestStaleNull(t *testing.T) {
	root := t.TempDir()
	env := skill.Env{Root: root}
	writeFile(t, filepath.Join(env.SkillsDir(), "alpha", "SKILL.md"),
		skillMD("alpha", "Use when authoring alpha."))

	views, _, err := Inspect(env, Filter{})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("got %d views, want 1", len(views))
	}
	if views[0].Stale != nil {
		t.Errorf("stale = %v, want nil with no manifest", *views[0].Stale)
	}
}

func TestInspectFilters(t *testing.T) {
	env := seedProject(t)

	local, _, err := Inspect(env, Filter{Local: true})
	if err != nil {
		t.Fatalf("Inspect local: %v", err)
	}
	if len(local) != 1 || local[0].Name != "alpha" {
		t.Fatalf("local filter = %+v, want [alpha]", local)
	}

	vendored, _, err := Inspect(env, Filter{Vendored: true})
	if err != nil {
		t.Fatalf("Inspect vendored: %v", err)
	}
	if len(vendored) != 1 || vendored[0].Name != "deploy" {
		t.Fatalf("vendored filter = %+v, want [deploy]", vendored)
	}
}

func TestInspectShadowed(t *testing.T) {
	env := seedProject(t)
	// Author a skill with the same name as the vendored deploy: authored wins.
	writeFile(t, filepath.Join(env.SkillsDir(), "deploy", "SKILL.md"),
		skillMD("deploy", "Use when deploying (authored override)."))

	views, _, err := Inspect(env, Filter{})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	count := 0
	for _, v := range views {
		if v.Name == "deploy" {
			count++
			if v.Origin != OriginLocal {
				t.Errorf("deploy origin = %q, want local (authored shadows vendored)", v.Origin)
			}
			if !v.Shadowed {
				t.Error("deploy should be marked shadowed")
			}
		}
	}
	if count != 1 {
		t.Errorf("got %d deploy rows, want exactly 1 (vendored hidden)", count)
	}
}

func TestInspectPartialSuccess(t *testing.T) {
	env := seedProject(t)
	// A malformed authored skill (missing frontmatter) is reported but does not
	// hide the valid skills.
	writeFile(t, filepath.Join(env.SkillsDir(), "broken", "SKILL.md"), "no frontmatter here\n")

	views, parseErrors, err := Inspect(env, Filter{})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(parseErrors) == 0 {
		t.Error("expected a parse error for the malformed skill")
	}
	if _, ok := viewByName(views, "alpha"); !ok {
		t.Error("valid skill alpha should still be returned")
	}
}

func TestDescribeVendoredProvenance(t *testing.T) {
	env := seedProject(t)
	prov, err := Describe(env, "deploy")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if prov.Origin != OriginVendored {
		t.Errorf("origin = %q, want vendored", prov.Origin)
	}
	if prov.Source != "github.com/acme/skills" || prov.Commit != "abc123" || prov.VersionSpec != "latest" {
		t.Errorf("unexpected provenance: %+v", prov)
	}
	if prov.SkillVersion == "" {
		t.Error("skill_version empty")
	}
	if prov.Replacements["REGION"] != "us-east-1" {
		t.Errorf("replacements = %v", prov.Replacements)
	}
}

func TestDescribeAuthored(t *testing.T) {
	env := seedProject(t)
	prov, err := Describe(env, "alpha")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if prov.Origin != OriginLocal {
		t.Errorf("origin = %q, want local", prov.Origin)
	}
	if prov.Source != "" || prov.Commit != "" {
		t.Errorf("authored skill should have no source/commit: %+v", prov)
	}
}

func TestDescribeUnknown(t *testing.T) {
	env := seedProject(t)
	_, err := Describe(env, "nope")
	if err == nil {
		t.Fatal("expected error for unknown skill")
	}
}

func TestGetFromFirstTarget(t *testing.T) {
	env := seedProject(t)
	data, target, err := Get(env, "deploy", "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if target != "agents" { // sorted order: agents < claude
		t.Errorf("target = %q, want agents (first sorted)", target)
	}
	if len(data) == 0 {
		t.Error("empty SKILL.md")
	}
}

func TestGetSpecificTarget(t *testing.T) {
	env := seedProject(t)
	_, target, err := Get(env, "deploy", "claude")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if target != "claude" {
		t.Errorf("target = %q, want claude", target)
	}
}

func TestGetAuthoredFallback(t *testing.T) {
	env := seedProject(t)
	data, target, err := Get(env, "alpha", "")
	if err != nil {
		t.Fatalf("Get authored: %v", err)
	}
	if target != OriginLocal {
		t.Errorf("target = %q, want local", target)
	}
	if len(data) == 0 {
		t.Error("empty authored SKILL.md")
	}
}

func TestGetMissing(t *testing.T) {
	env := seedProject(t)
	_, _, err := Get(env, "ghost", "")
	if err == nil {
		t.Fatal("expected error for unrendered skill")
	}
}
