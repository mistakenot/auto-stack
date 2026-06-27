package adopt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
)

// newRepo creates a git-initialized temp root and returns it plus an Env that
// redirects all .auto/ state into that root.
func newRepo(t *testing.T) (string, skill.Env) {
	t.Helper()
	root := t.TempDir()
	gitInit(t, root)
	return root, skill.Env{Root: root, RootOverride: true}
}

func gitInit(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// writeForeign drops a foreign skill dir <root>/.<style>/skills/<name>/SKILL.md
// and returns the exact bytes written.
func writeForeign(t *testing.T, root, style, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, "."+style, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: Use when testing adopt.\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return content
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// assertNoStageDirs fails if any leftover .adopt-stage-/.adopt-trash- dirs
// remain under ./skills.
func assertNoStageDirs(t *testing.T, env skill.Env) {
	t.Helper()
	entries, err := os.ReadDir(env.SkillsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".adopt-stage-") || strings.HasPrefix(e.Name(), ".adopt-trash-") {
			t.Fatalf("leftover staging dir %q under ./skills", e.Name())
		}
	}
}

func TestAdoptSingle(t *testing.T) {
	root, env := newRepo(t)
	want := writeForeign(t, root, "claude", "new-plan", "BODY")

	res, err := Adopt(env, []string{"new-plan"}, Options{})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Adopted) != 1 || res.Adopted[0].Name != "new-plan" || res.Adopted[0].From != "claude" {
		t.Fatalf("unexpected adopted: %+v", res.Adopted)
	}

	dst := filepath.Join(env.SkillsDir(), "new-plan", "SKILL.md")
	if !exists(dst) {
		t.Fatalf("destination %s not created", dst)
	}
	// adopt does NOT re-render: bytes are unchanged.
	if got := readFile(t, dst); got != want {
		t.Fatalf("adopt re-rendered content:\n got: %q\nwant: %q", got, want)
	}
	// Source removed.
	if exists(filepath.Join(root, ".claude", "skills", "new-plan")) {
		t.Fatalf("source dir was not removed")
	}
	// git-tracked: the new path is staged.
	status := git(t, root, "status", "--porcelain")
	if !strings.Contains(status, "skills/new-plan/SKILL.md") {
		t.Fatalf("expected skills/new-plan staged; status:\n%s", status)
	}
	assertNoStageDirs(t, env)
}

func TestAdoptListMode(t *testing.T) {
	root, env := newRepo(t)
	writeForeign(t, root, "claude", "new-plan", "A")
	writeForeign(t, root, "agents", "other", "B")

	res, err := Adopt(env, nil, Options{})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(res.Adopted) != 0 {
		t.Fatalf("list mode adopted something: %+v", res.Adopted)
	}
	names := map[string]bool{}
	for _, c := range res.Candidates {
		names[c.Name] = true
	}
	if !names["new-plan"] || !names["other"] {
		t.Fatalf("candidates missing expected names: %+v", res.Candidates)
	}
	// No filesystem change.
	if exists(filepath.Join(env.SkillsDir(), "new-plan")) {
		t.Fatalf("list mode created ./skills/new-plan")
	}
}

func TestAdoptAll(t *testing.T) {
	root, env := newRepo(t)
	writeForeign(t, root, "claude", "alpha", "A")
	writeForeign(t, root, "agents", "beta", "B")

	res, err := Adopt(env, nil, Options{All: true})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Adopted) != 2 {
		t.Fatalf("expected 2 adopted, got %+v", res.Adopted)
	}
	for _, name := range []string{"alpha", "beta"} {
		if !exists(filepath.Join(env.SkillsDir(), name, "SKILL.md")) {
			t.Fatalf("%s not adopted", name)
		}
	}
}

func TestAdoptExistingTargetRefusalThenForce(t *testing.T) {
	root, env := newRepo(t)
	writeForeign(t, root, "claude", "new-plan", "FOREIGN")
	// Pre-create ./skills/new-plan with distinct content.
	authored := filepath.Join(env.SkillsDir(), "new-plan")
	if err := os.MkdirAll(authored, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "---\nname: new-plan\ndescription: Use when testing.\n---\n\nEXISTING\n"
	if err := os.WriteFile(filepath.Join(authored, "SKILL.md"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	// Without --force: refused, existing content untouched.
	res, err := Adopt(env, []string{"new-plan"}, Options{})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "--force") {
		t.Fatalf("expected --force refusal, got %v", res.Errors)
	}
	if got := readFile(t, filepath.Join(authored, "SKILL.md")); got != existing {
		t.Fatalf("refused adopt modified existing content: %q", got)
	}
	assertNoStageDirs(t, env)

	// With --force: overwritten.
	res, err = Adopt(env, []string{"new-plan"}, Options{Force: true})
	if err != nil {
		t.Fatalf("Adopt force: %v", err)
	}
	if len(res.Errors) != 0 || len(res.Adopted) != 1 {
		t.Fatalf("force adopt failed: errs=%v adopted=%+v", res.Errors, res.Adopted)
	}
	if !strings.Contains(readFile(t, filepath.Join(authored, "SKILL.md")), "FOREIGN") {
		t.Fatalf("force adopt did not overwrite with foreign content")
	}
	assertNoStageDirs(t, env)
}

func TestAdoptDivergentRequiresFrom(t *testing.T) {
	root, env := newRepo(t)
	writeForeign(t, root, "claude", "new-plan", "CLAUDE-BODY")
	writeForeign(t, root, "agents", "new-plan", "AGENTS-BODY")

	// No --from: hard error.
	res, err := Adopt(env, []string{"new-plan"}, Options{})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(res.Adopted) != 0 {
		t.Fatalf("divergent adopt should not pick one: %+v", res.Adopted)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "--from") {
		t.Fatalf("expected --from hard error, got %v", res.Errors)
	}
	// Failed adopt leaves no half-written destination.
	if exists(filepath.Join(env.SkillsDir(), "new-plan")) {
		t.Fatalf("divergent error left a half-written ./skills/new-plan")
	}
	assertNoStageDirs(t, env)

	// --from claude: picks claude's copy.
	res, err = Adopt(env, []string{"new-plan"}, Options{From: "claude"})
	if err != nil {
		t.Fatalf("Adopt from: %v", err)
	}
	if len(res.Errors) != 0 || len(res.Adopted) != 1 || res.Adopted[0].From != "claude" {
		t.Fatalf("--from claude failed: errs=%v adopted=%+v", res.Errors, res.Adopted)
	}
	if !strings.Contains(readFile(t, filepath.Join(env.SkillsDir(), "new-plan", "SKILL.md")), "CLAUDE-BODY") {
		t.Fatalf("--from claude adopted the wrong copy")
	}
}

func TestAdoptIdenticalCopiesAdoptOne(t *testing.T) {
	root, env := newRepo(t)
	// Identical content across two targets.
	writeForeign(t, root, "claude", "shared", "SAME-BODY")
	writeForeign(t, root, "agents", "shared", "SAME-BODY")

	res, err := Adopt(env, []string{"shared"}, Options{})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("identical copies should not error: %v", res.Errors)
	}
	if len(res.Adopted) != 1 {
		t.Fatalf("expected exactly one adoption, got %+v", res.Adopted)
	}
	if !exists(filepath.Join(env.SkillsDir(), "shared", "SKILL.md")) {
		t.Fatalf("shared not adopted")
	}
}

func TestAdoptCommittedSource(t *testing.T) {
	root, env := newRepo(t)
	writeForeign(t, root, "claude", "tracked", "TRACKED-BODY")
	// Commit the source so the source is git-tracked.
	git(t, root, "add", ".claude/skills/tracked/SKILL.md")
	git(t, root, "commit", "-m", "seed tracked source")

	res, err := Adopt(env, []string{"tracked"}, Options{})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(res.Errors) != 0 || len(res.Adopted) != 1 {
		t.Fatalf("committed-source adopt failed: errs=%v adopted=%+v", res.Errors, res.Adopted)
	}
	dst := filepath.Join(env.SkillsDir(), "tracked", "SKILL.md")
	if !exists(dst) {
		t.Fatalf("destination not created")
	}
	// git recorded the add at the new path.
	status := git(t, root, "status", "--porcelain")
	if !strings.Contains(status, "skills/tracked/SKILL.md") {
		t.Fatalf("expected staged add at new path; status:\n%s", status)
	}
}

func TestAdoptUnknownName(t *testing.T) {
	_, env := newRepo(t)
	res, err := Adopt(env, []string{"does-not-exist"}, Options{})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "no adoptable") {
		t.Fatalf("expected no-adoptable error, got %v", res.Errors)
	}
}
