package cli_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mistakenot/auto-plan/internal/workspace"
)

var scaffoldPaths = []string{".auto/plan/plans/AGENTS.md", ".auto/plan/plans/CLAUDE.md"}

type scaffoldOut struct {
	Scaffolded *[]string `json:"scaffolded"`
}

// assertScaffold checks AGENTS.md holds the scaffold text and CLAUDE.md is a
// relative symlink whose target is exactly AGENTS.md.
func assertScaffold(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".auto", "plan", "plans")
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil || string(data) != workspace.AgentsText {
		t.Fatalf("AGENTS.md = %q, %v", data, err)
	}
	target, err := os.Readlink(filepath.Join(dir, "CLAUDE.md"))
	if err != nil || target != "AGENTS.md" {
		t.Fatalf("CLAUDE.md symlink target = %q, %v", target, err)
	}
}

func TestInitScaffoldsAgentsAndClaudeSymlink(t *testing.T) {
	root := repo(t)
	first := decode[scaffoldOut](t, mustRun(t, root, "init"))
	if first.Scaffolded == nil || !slices.Equal(*first.Scaffolded, scaffoldPaths) {
		t.Fatalf("first init scaffolded = %v, want %v", first.Scaffolded, scaffoldPaths)
	}
	assertScaffold(t, root)

	second := mustRun(t, root, "init")
	if strings.Contains(second, "scaffolded") {
		t.Fatalf("second init must create nothing, got %s", second)
	}
	assertScaffold(t, root)
}

func TestInitTextListsScaffoldedFiles(t *testing.T) {
	root := repo(t)
	got := mustRun(t, root, "init", "--text")
	want := "created .auto/plan/plans\ncreated .auto/plan/plans/AGENTS.md\ncreated .auto/plan/plans/CLAUDE.md\n"
	if got != want {
		t.Fatalf("init --text = %q, want %q", got, want)
	}
	if got := mustRun(t, root, "init", "--text"); got != ".auto/plan/plans already exists\n" {
		t.Fatalf("second init --text = %q", got)
	}
}

func TestInitPreservesEditedAgents(t *testing.T) {
	root := repo(t)
	mustRun(t, root, "init")
	agents := filepath.Join(root, ".auto", "plan", "plans", "AGENTS.md")
	const edited = "# my own notes\n"
	if err := os.WriteFile(agents, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	// Remove the symlink too: init recreates CLAUDE.md but never AGENTS.md.
	if err := os.Remove(filepath.Join(root, ".auto", "plan", "plans", "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	out := decode[scaffoldOut](t, mustRun(t, root, "init"))
	if out.Scaffolded == nil || !slices.Equal(*out.Scaffolded, []string{".auto/plan/plans/CLAUDE.md"}) {
		t.Fatalf("scaffolded = %v", out.Scaffolded)
	}
	if data, _ := os.ReadFile(agents); string(data) != edited {
		t.Fatalf("AGENTS.md was overwritten: %q", data)
	}
}

func TestInitLeavesRegularClaudeAlone(t *testing.T) {
	root := repo(t)
	dir := filepath.Join(root, ".auto", "plan", "plans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const mine = "# hand-written\n"
	claude := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claude, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, root, "init")
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, stderr)
	}
	out := decode[scaffoldOut](t, stdout)
	if out.Scaffolded == nil || !slices.Equal(*out.Scaffolded, []string{".auto/plan/plans/AGENTS.md"}) {
		t.Fatalf("scaffolded = %v", out.Scaffolded)
	}
	if strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, ".auto/plan/plans/CLAUDE.md exists and is not a symlink to AGENTS.md; left as is") {
		t.Fatalf("stderr = %q, want one note", stderr)
	}
	if data, _ := os.ReadFile(claude); string(data) != mine {
		t.Fatalf("CLAUDE.md was changed: %q", data)
	}

	// A symlink elsewhere is left alone too.
	if err := os.Remove(claude); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../README.md", claude); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, root, "init")
	if code != 0 || strings.Contains(stdout, "scaffolded") || !strings.Contains(stderr, "left as is") {
		t.Fatalf("exit %d stdout %s stderr %q", code, stdout, stderr)
	}
	if target, _ := os.Readlink(claude); target != "../../README.md" {
		t.Fatalf("CLAUDE.md symlink changed to %q", target)
	}
}

func TestNewWithoutInitScaffolds(t *testing.T) {
	root := repo(t)
	out := decode[scaffoldOut](t, mustRun(t, root, "new", "demo", "--kind", "task"))
	if out.Scaffolded == nil || !slices.Equal(*out.Scaffolded, scaffoldPaths) {
		t.Fatalf("new scaffolded = %v", out.Scaffolded)
	}
	assertScaffold(t, root)
	if second := mustRun(t, root, "new", "other", "--kind", "task"); strings.Contains(second, "scaffolded") {
		t.Fatalf("second new must create nothing, got %s", second)
	}
}

func TestPlanScanIgnoresScaffold(t *testing.T) {
	root := repo(t)
	mustRun(t, root, "init")
	mustRun(t, root, "new", "demo", "--kind", "task")
	out := decode[struct {
		Plans []map[string]any `json:"plans"`
	}](t, mustRun(t, root, "list"))
	if len(out.Plans) != 1 || out.Plans[0]["id"] != p1 {
		t.Fatalf("list = %v, want only plan %s", out.Plans, p1)
	}
	mustRun(t, root, "lint", "all")
	// The next number follows the one plan, unaffected by the files.
	if got := decode[map[string]any](t, mustRun(t, root, "new", "next", "--kind", "task")); got["number"] != "002" {
		t.Fatalf("next new = %v", got)
	}
}
