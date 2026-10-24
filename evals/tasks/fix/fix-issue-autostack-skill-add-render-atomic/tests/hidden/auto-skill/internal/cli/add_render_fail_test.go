package cli_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAddRenderFailIsAtomic is the regression guard for the reported bug:
//
//	`auto skill add remotion-dev/skills` failed to install (a `{{` in a SKILL.md
//	body is outside render's accepted template grammar), but the lock files
//	(lock.json + skills.yaml) STILL got updated even though the install failed.
//
// The `add` pipeline used to write lock.json + skills.yaml BEFORE rendering the
// skill, so a render failure left the lock files mutated for a skill that never
// installed. The fix validates that every selected skill renders BEFORE any
// config is written, so a `{{`-broken skill fails the add atomically.
//
// This test drives the real CLI against a local git repo whose SKILL.md carries
// a GitHub Actions `${{ … }}` expression (exactly the kind of text a real skills
// repo carries) and asserts BOTH halves: the install fails, AND the lock files
// are left byte-for-byte unchanged.
func TestAddRenderFailIsAtomic(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	if _, stderr, code := runCLI(t, "--root", root, "init", "--project", "-y"); code != 0 {
		t.Fatalf("init --project failed: code=%d stderr=%s", code, stderr)
	}

	lockPath := filepath.Join(root, ".auto", "skills", "lock.json")
	yamlPath := filepath.Join(root, ".auto", "skills", "skills.yaml")

	// Snapshot the empty-but-initialized lock files before the add.
	lockBefore := readFileString(t, lockPath)
	yamlBefore := readFileString(t, yamlPath)

	// A local git repo whose SKILL.md body carries a `${{ … }}` GitHub Actions
	// expression. `{{ toJSON(github.event) }}` parses as a function call, which
	// render rejects (only `{{ .var }}` field access + the literal-brace escape
	// are allowed) — reproducing the remotion-dev/skills failure mode.
	srcRepo := makeLocalSkillRepo(t, "remotion", `---
name: remotion
description: "Use when rendering videos with Remotion. Prefer for programmatic video generation in CI."
---

## Usage

Render in CI with the props passed through from the workflow event:

    - uses: actions/checkout@v4
    - run: npx remotion render --props='${{ toJSON(github.event.inputs) }}'
`)

	stdout, stderr, code := runCLI(t, "--root", root, "add", srcRepo, "--trust-requested")

	// ── Half 1: the install FAILS on the `{{` template construct ─────────────
	if code == 0 {
		t.Fatalf("expected `add` to fail on the `{{` template construct, but it exited 0\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "cannot be rendered") || !strings.Contains(stderr, "template_rejected") {
		t.Fatalf("expected a render-rejection error on stderr, got:\n%s", stderr)
	}
	t.Logf("install failed as expected (code=%d):\n%s", code, strings.TrimSpace(stderr))

	// ── Half 2: the lock files are left BYTE-FOR-BYTE unchanged ──────────────
	lockAfter := readFileString(t, lockPath)
	yamlAfter := readFileString(t, yamlPath)

	if lockAfter != lockBefore {
		t.Fatalf("lock.json was mutated by a FAILED add (add is not atomic)\nbefore:\n%s\nafter:\n%s", lockBefore, lockAfter)
	}
	if yamlAfter != yamlBefore {
		t.Fatalf("skills.yaml was mutated by a FAILED add (add is not atomic)\nbefore:\n%s\nafter:\n%s", yamlBefore, yamlAfter)
	}

	// Belt-and-suspenders: no `remotion` entry leaked into either file.
	var lock struct {
		Skills map[string]json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal([]byte(lockAfter), &lock); err != nil {
		t.Fatalf("parse lock.json: %v\n%s", err, lockAfter)
	}
	if _, ok := lock.Skills["remotion"]; ok {
		t.Fatalf("a `remotion` entry leaked into lock.json after the failed add: %v", keysOf(lock.Skills))
	}
	if strings.Contains(yamlAfter, "remotion") {
		t.Fatalf("a `remotion` entry leaked into skills.yaml after the failed add:\n%s", yamlAfter)
	}

	t.Logf("atomic: install failed (exit %d) and lock.json + skills.yaml are unchanged", code)
}

// TestAddValidSkillWithLiteralBraceStillInstalls guards the atomic-render fix
// against over-rejection: a skill whose body legitimately contains braces via the
// `{{ "{{" }}` literal-brace escape (the one accepted way to emit `{{` verbatim)
// must still add and render cleanly, writing its lock + skills.yaml entry.
func TestAddValidSkillWithLiteralBraceStillInstalls(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	if _, stderr, code := runCLI(t, "--root", root, "init", "--project", "-y"); code != 0 {
		t.Fatalf("init --project failed: code=%d stderr=%s", code, stderr)
	}

	srcRepo := makeLocalSkillRepo(t, "braces", `---
name: braces
description: "Use when documenting template syntax. Prefer for skills that show literal braces."
---

## Usage

To emit a literal double brace in docs, write {{ "{{" }} in the template.
`)

	stdout, stderr, code := runCLI(t, "--root", root, "add", srcRepo, "--trust-requested")
	if code != 0 {
		t.Fatalf("expected a valid skill to add cleanly, got code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	lockPath := filepath.Join(root, ".auto", "skills", "lock.json")
	var lock struct {
		Skills map[string]json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal([]byte(readFileString(t, lockPath)), &lock); err != nil {
		t.Fatalf("parse lock.json: %v", err)
	}
	if _, ok := lock.Skills["braces"]; !ok {
		t.Fatalf("expected `braces` in lock.json after a successful add, got keys: %v", keysOf(lock.Skills))
	}
}

// makeLocalSkillRepo creates a throwaway local git repo containing a single
// skill at skills/<name>/SKILL.md, committed, and returns the repo dir (a valid
// `auto skill add` source).
func makeLocalSkillRepo(t *testing.T, name, skillMD string) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	git("init")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "test")

	writeFile(t, filepath.Join(dir, "skills", name, "SKILL.md"), skillMD)

	git("add", "-A")
	git("commit", "-m", "initial")
	return dir
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
