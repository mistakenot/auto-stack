package workspace

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mistakenot/auto-plan/internal/graph"
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

func writeGraph(t *testing.T, root, folder, doc string) {
	t.Helper()
	mkdirs(t, root, PlansDir+"/"+folder)
	if err := os.WriteFile(filepath.Join(root, PlansDir, folder, GraphFile), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func planJSON(kind, epic string, nodes string) string {
	e := ""
	if epic != "" {
		e = `, "epic": "` + epic + `"`
	}
	return `{"version": 1, "nodes": [{"id": "plan", "type": "plan", "status": "active", "fields": {"name": "x", "kind": "` + kind +
		`", "lifecycle": "requirements", "created": "2026-01-01"` + e + `}}` + nodes + `], "edges": []}`
}

// TestPlanSet: qualified references resolve against docs/plans/NNN-*/graph.json,
// loading only the plans they reach, and cross-plan edges are checked for a
// missing plan, a missing node and the edge's allowed target types (AC-5).
func TestPlanSet(t *testing.T) {
	root := gitRepo(t)
	writeGraph(t, root, "001-epic", planJSON("epic", "", `,
	  {"id": "r-8hw3", "type": "rail", "status": "active", "rank": "a0", "fields": {"title": "no network"}},
	  {"id": "g-k7q2", "type": "goal", "status": "active", "rank": "a0", "fields": {"title": "a goal"}},
	  {"id": "c-aaaa", "type": "child", "status": "active", "rank": "a0", "fields": {"plan": "002"}},
	  {"id": "c-bbbb", "type": "child", "status": "active", "rank": "a1", "fields": {"plan": "009"}}`))
	writeGraph(t, root, "002-child", planJSON("task", "001", ""))
	writeGraph(t, root, "003-stray", planJSON("task", "001", ""))
	writeGraph(t, root, "004-task", planJSON("task", "", ""))
	writeGraph(t, root, "005-broken", "{")
	w, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := w.PlanSet()
	if err != nil {
		t.Fatal(err)
	}
	if got := s.IDs(); len(got) != 5 || got[0] != "001" || !s.Has("004") || s.Has("009") {
		t.Fatalf("IDs = %v", got)
	}

	n, err := s.Lookup("001:r-8hw3")
	if err != nil || n.Type != "rail" || n.StringField("title") != "no network" {
		t.Fatalf("Lookup = %+v, %v", n, err)
	}
	if len(s.cache) != 1 {
		t.Fatalf("only the referenced plan is loaded, got %d", len(s.cache))
	}
	if _, err := s.Lookup("009:r-8hw3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing plan: %v", err)
	}
	if _, err := s.Lookup("001:r-zzzz"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("missing node: %v", err)
	}
	if _, err := s.Lookup("005:plan"); err == nil {
		t.Fatal("malformed plan must fail")
	}

	check := func(typ, to, want string) {
		t.Helper()
		errs := s.CheckEdge(graph.Edge{From: "plan", Type: typ, To: to})
		got := ""
		if len(errs) > 0 {
			got = errs[0].Code
		}
		if got != want {
			t.Errorf("CheckEdge %s %s = %+v, want %q", typ, to, errs, want)
		}
	}
	check("honors", "001:r-8hw3", "")
	check("honors", "009:r-8hw3", graph.CodeDanglingRef)
	check("honors", "001:r-zzzz", graph.CodeDanglingRef)
	check("honors", "005:r-zzzz", graph.CodeDanglingRef)
	check("honors", "001:g-k7q2", graph.CodeWrongEndpoint)
	check("honors", "r-8hw3", "")     // plan-local: Validate's job
	check("proves", "001:g-k7q2", "") // not cross-plan: Validate's job

	if err := s.CheckEpic("001"); err != nil {
		t.Fatalf("CheckEpic 001: %v", err)
	}
	if err := s.CheckEpic("004"); err == nil || !strings.Contains(err.Error(), "not an epic") {
		t.Fatalf("CheckEpic task: %v", err)
	}
	if err := s.CheckEpic("009"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CheckEpic missing: %v", err)
	}
	// The family: listed children that exist, plus plans that declare the epic.
	if got := s.Family("001"); !slices.Equal(got, []string{"002", "003"}) {
		t.Fatalf("Family = %v", got)
	}
}
