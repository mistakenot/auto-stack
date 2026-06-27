package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mistakenot/auto-skill/internal/render"
	"github.com/mistakenot/auto-skill/internal/skill"
)

// writeAuthoredSkill writes an authored skill under <root>/skills/<name>.
func writeAuthoredSkill(t *testing.T, env skill.Env, name, body string) {
	t.Helper()
	dir := filepath.Join(env.Root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: Use when testing.\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runProcess runs the full A→B→C pipeline and returns the phase-C result.
func runProcess(t *testing.T, env skill.Env, opts Options) *ProcessResult {
	t.Helper()
	plan, err := BuildPlan(env, opts)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	fetch, err := Fetch(env, plan, opts)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	res, err := Process(env, plan, fetch, opts)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	return res
}

func findStaged(res *ProcessResult, name string) (*StagedSkill, bool) {
	for _, s := range res.Staged {
		if s.Name == name {
			return s, true
		}
	}
	return nil, false
}

func findInstall(res *ProcessResult, target, name string) (Install, bool) {
	for _, in := range res.Installs {
		if in.Target == target && in.Skill == name {
			return in, true
		}
	}
	return Install{}, false
}

func skillMDData(s *StagedSkill) string {
	for _, f := range s.Files {
		if f.Path == render.SkillMDPath {
			return string(f.Data)
		}
	}
	return ""
}

// installTarget materializes a staged skill into a target dir, simulating a
// prior successful sync (phase 5's write).
func installTarget(t *testing.T, dir string, s *StagedSkill) {
	t.Helper()
	if err := WriteSkillDir(filepath.Join(dir, s.Name), s.Files); err != nil {
		t.Fatalf("WriteSkillDir: %v", err)
	}
}

func writeManifest(t *testing.T, env skill.Env, m *skill.Manifest) {
	t.Helper()
	if err := os.MkdirAll(env.SkillsConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := skill.EncodeJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.ManifestPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestProcessUnionShadow: vendored + authored skills union into every target,
// with authored shadowing vendored on a name clash.
func TestProcessUnionShadow(t *testing.T) {
	f := newFixture(t)
	headA := f.commitSkill("alpha", "vendored alpha")
	headB := f.commitSkill("beta", "vendored beta")
	head := f.head()

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "commit:"+head, head),
		"beta":  lockEntry(f.url, "beta", "commit:"+head, head),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	realizeCommit(t, env, f.url, head)
	_ = headA
	_ = headB

	// Authored alpha (shadows vendored) + authored-only gamma.
	writeAuthoredSkill(t, env, "alpha", "authored alpha")
	writeAuthoredSkill(t, env, "gamma", "authored gamma")

	res := runProcess(t, env, Options{Locked: true})

	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	names := make([]string, 0, len(res.Staged))
	for _, s := range res.Staged {
		names = append(names, s.Name)
	}
	want := []string{"alpha", "beta", "gamma"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("staged names = %v, want %v", names, want)
	}

	alpha, _ := findStaged(res, "alpha")
	if alpha.Source != "authored" {
		t.Errorf("alpha.Source = %q, want authored (authored should shadow vendored)", alpha.Source)
	}
	if !strings.Contains(skillMDData(alpha), "authored alpha") {
		t.Errorf("alpha SKILL.md did not come from the authored copy: %q", skillMDData(alpha))
	}

	beta, _ := findStaged(res, "beta")
	if beta.Source == "authored" {
		t.Errorf("beta.Source = authored, want vendored URL")
	}

	// Two default targets, three skills each = 6 installs.
	if len(res.Installs) != 6 {
		t.Fatalf("installs = %d, want 6 (2 targets x 3 skills)", len(res.Installs))
	}
	// Manifest: every target manages every skill.
	if res.Manifest == nil {
		t.Fatal("nil manifest")
	}
	for _, tg := range res.Targets {
		mt, ok := res.Manifest.Targets[tg.Name]
		if !ok {
			t.Fatalf("manifest missing target %q", tg.Name)
		}
		if len(mt.ManagedSkills) != 3 {
			t.Errorf("target %q manages %d skills, want 3", tg.Name, len(mt.ManagedSkills))
		}
	}
}

// TestProcessOnDiskDigestSkip: a freshly-synced target is skipped on re-run; a
// hand-edit forces a re-render; an mtime-only touch does NOT.
func TestProcessOnDiskDigestSkip(t *testing.T) {
	f := newFixture(t)
	f.commitSkill("alpha", "body a")
	head := f.head()

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "commit:"+head, head),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	realizeCommit(t, env, f.url, head)

	// First process: target absent → write.
	res := runProcess(t, env, Options{Locked: true})
	alpha, ok := findStaged(res, "alpha")
	if !ok {
		t.Fatal("alpha not staged")
	}
	target := res.Targets[0]
	in, _ := findInstall(res, target.Name, "alpha")
	if in.Action != InstallWrite {
		t.Fatalf("first run action = %q, want write", in.Action)
	}

	// Simulate the install, then re-run: on-disk digest matches → skip.
	installTarget(t, target.Dir, alpha)
	res = runProcess(t, env, Options{Locked: true})
	in, _ = findInstall(res, target.Name, "alpha")
	if in.Action != InstallSkip {
		t.Fatalf("re-run action = %q, want skip (on-disk digest matches)", in.Action)
	}
	if in.OnDisk != in.Want {
		t.Errorf("on-disk %q != want %q on a clean target", in.OnDisk, in.Want)
	}

	// mtime-only touch: bytes unchanged → still skip.
	skillFile := filepath.Join(target.Dir, "alpha", "SKILL.md")
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(skillFile, future, future); err != nil {
		t.Fatal(err)
	}
	res = runProcess(t, env, Options{Locked: true})
	in, _ = findInstall(res, target.Name, "alpha")
	if in.Action != InstallSkip {
		t.Fatalf("after mtime touch action = %q, want skip (bytes unchanged)", in.Action)
	}

	// Hand-edit a target file: bytes change → re-render.
	if err := os.WriteFile(skillFile, []byte("---\nname: alpha\n---\n\nhand edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = runProcess(t, env, Options{Locked: true})
	in, _ = findInstall(res, target.Name, "alpha")
	if in.Action != InstallWrite {
		t.Fatalf("after hand-edit action = %q, want write (digest diverged)", in.Action)
	}
}

// TestProcessRenderVersionLazyRerender: a manifest entry recorded below the
// engine's render_version forces a one-time re-render even when the on-disk
// digest matches; advancing the manifest re-enables the skip (AC-6).
func TestProcessRenderVersionLazyRerender(t *testing.T) {
	f := newFixture(t)
	f.commitSkill("alpha", "body a")
	head := f.head()

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "commit:"+head, head),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	realizeCommit(t, env, f.url, head)

	// Render and install so the on-disk digest matches the expected skill_version.
	res := runProcess(t, env, Options{Locked: true})
	alpha, _ := findStaged(res, "alpha")
	target := res.Targets[0]
	installTarget(t, target.Dir, alpha)
	// Install into every target so none is write-by-absence.
	for _, tg := range res.Targets {
		installTarget(t, tg.Dir, alpha)
	}

	// Manifest recorded with render_version below the current constant.
	stale := &skill.Manifest{
		Version:       1,
		RenderVersion: "0",
		Skills: map[string]skill.ManifestSkill{
			"alpha": {
				TemplateHash:  alpha.TemplateHash,
				Replacements:  map[string]string{},
				FileRefs:      []skill.ManifestFileRef{},
				SkillVersion:  alpha.SkillVersion,
				RenderVersion: "0",
			},
		},
		Targets: map[string]skill.ManifestTarget{},
	}
	writeManifest(t, env, stale)

	res = runProcess(t, env, Options{Locked: true})
	sa, _ := findStaged(res, "alpha")
	if !sa.ForcedRender {
		t.Error("expected alpha.ForcedRender = true for a stale render_version")
	}
	in, _ := findInstall(res, target.Name, "alpha")
	if in.Action != InstallWrite {
		t.Fatalf("stale render_version action = %q, want write (forced re-render)", in.Action)
	}

	// Advance the manifest to the current render_version: skip again.
	current := &skill.Manifest{
		Version:       1,
		RenderVersion: res.Manifest.RenderVersion,
		Skills: map[string]skill.ManifestSkill{
			"alpha": res.Manifest.Skills["alpha"],
		},
		Targets: map[string]skill.ManifestTarget{},
	}
	writeManifest(t, env, current)

	res = runProcess(t, env, Options{Locked: true})
	in, _ = findInstall(res, target.Name, "alpha")
	if in.Action != InstallSkip {
		t.Fatalf("advanced render_version action = %q, want skip", in.Action)
	}
}

// TestProcessTokenBudgetAdvisory: an oversized SKILL.md emits an advisory
// warning but never errors and still produces a manifest.
func TestProcessTokenBudgetAdvisory(t *testing.T) {
	f := newFixture(t)
	big := strings.Repeat("word ", 5000) // ~25k chars → ~6k tokens > 4000
	f.commitSkill("alpha", big)
	head := f.head()

	env := newEnv(t)
	approve(t, env, f.url)
	writeLock(t, env, map[string]skill.LockEntry{
		"alpha": lockEntry(f.url, "alpha", "commit:"+head, head),
	})
	writeSkillsYAML(t, env, &skill.SkillsYAML{})
	realizeCommit(t, env, f.url, head)

	res := runProcess(t, env, Options{Locked: true})
	if len(res.Errors) != 0 {
		t.Fatalf("token budget must not error: %v", res.Errors)
	}
	if res.Manifest == nil {
		t.Fatal("manifest should still be built")
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "advisory budget") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an advisory token-budget warning, got %v", res.Warnings)
	}
}
