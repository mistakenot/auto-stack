package workspace

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	// Resolve symlinks (macOS /var → /private/var) so paths compare equal to
	// what git reports.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNextIDMissingFolder(t *testing.T) {
	root := gitRepo(t)
	w, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	id, err := w.NextID()
	if err != nil || id != "001" {
		t.Fatalf("NextID = %q, %v; want 001", id, err)
	}
	mkdirs(t, root, PlansDir)
	if id, _ := w.NextID(); id != "001" {
		t.Fatalf("empty folder NextID = %q, want 001", id)
	}
}

func TestNextIDSkipsGapsAndIgnoresNoise(t *testing.T) {
	root := gitRepo(t)
	mkdirs(t, root, PlansDir+"/001-a", PlansDir+"/003-b", PlansDir+"/notes", PlansDir+"/9999-too-long", PlansDir+"/010-Bad_Name")
	if err := os.WriteFile(filepath.Join(root, PlansDir, "005-file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	w, _ := Open(root)
	id, err := w.NextID()
	if err != nil || id != "004" {
		t.Fatalf("NextID = %q, %v; want 004", id, err)
	}
	plans, _ := w.Plans()
	if len(plans) != 2 || plans[0].Folder() != "001-a" || plans[1].Dir != "docs/plans/003-b" {
		t.Fatalf("Plans = %+v", plans)
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"demo", "stage-briefs", "a1-b2"} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Demo", "stage_briefs", "-a", "a-", "a--b", "a b", "001-x/y"} {
		if err := ValidateName(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestResolveFromSubdirectory(t *testing.T) {
	root := gitRepo(t)
	mkdirs(t, root, PlansDir+"/001-a", PlansDir+"/003-b", "src/deep")
	w, err := Open(filepath.Join(root, "src", "deep"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Root != root {
		t.Fatalf("Root = %s, want %s", w.Root, root)
	}

	for arg, want := range map[string]string{
		"003":                                  "003-b",
		"001-a":                                "001-a",
		"../../docs/plans/003-b":               "003-b",
		"../../docs/plans/001-a/":              "001-a",
		"../../docs/plans/003-b/graph.json":    "003-b",
		filepath.Join(root, PlansDir, "001-a"): "001-a",
	} {
		p, err := w.ResolveOne(arg)
		if err != nil {
			t.Errorf("Resolve(%q): %v", arg, err)
			continue
		}
		if p.Folder() != want {
			t.Errorf("Resolve(%q) = %s, want %s", arg, p.Folder(), want)
		}
	}

	all, err := w.Resolve(All)
	if err != nil || len(all) != 2 {
		t.Fatalf("Resolve(all) = %v, %v", all, err)
	}
	if _, err := w.ResolveOne(All); err == nil {
		t.Error("ResolveOne(all) must fail")
	}
	for _, missing := range []string{"002", "001-b", "docs/plans/nope"} {
		if _, err := w.Resolve(missing); !errors.Is(err, ErrNotFound) {
			t.Errorf("Resolve(%q) = %v, want ErrNotFound", missing, err)
		}
	}
}

func TestOpenOutsideRepo(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil {
		t.Skip("temp dir is inside a git repository on this machine")
	}
}
