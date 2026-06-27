package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
)

// decodeJSONArray decodes a JSON array of objects from stdout.
func decodeJSONArray(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &out); err != nil {
		t.Fatalf("decode JSON array: %v\nraw:\n%s", err, raw)
	}
	return out
}

func findRow(rows []map[string]any, name string) map[string]any {
	for _, r := range rows {
		if r["name"] == name {
			return r
		}
	}
	return nil
}

// seedAndSync authors two skills and renders them via the real sync engine
// (hermetic — authored skills need no network), producing a manifest and rendered
// target trees so the inspect triad reads real state.
func seedAndSync(t *testing.T, root string) {
	t.Helper()
	writeSyncYAML(t, root, &skill.SkillsYAML{Targets: []string{"claude", "agents"}})
	writeFile(t, filepath.Join(root, "skills", "alpha", "SKILL.md"),
		validSkill("alpha", "Use when authoring alpha.", "## Workflow\n\n1. Step.\n"))
	writeFile(t, filepath.Join(root, "skills", "bravo", "SKILL.md"),
		validSkill("bravo", "Use when authoring bravo.", "## Workflow\n\n1. Step.\n"))
	stdout, stderr, code := runCLI(t, "--root", root, "sync")
	if code != 0 {
		t.Fatalf("sync failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

func TestListJSONDefaultShape(t *testing.T) {
	root := t.TempDir()
	seedAndSync(t, root)

	stdout, stderr, code := runCLI(t, "--root", root, "list")
	if code != 0 {
		t.Fatalf("list failed: code=%d stderr=%s", code, stderr)
	}
	rows := decodeJSONArray(t, stdout)
	alpha := findRow(rows, "alpha")
	if alpha == nil {
		t.Fatalf("alpha not in list: %s", stdout)
	}
	if alpha["origin"] != "local" {
		t.Errorf("alpha origin = %v, want local", alpha["origin"])
	}
	if _, ok := alpha["stale"]; !ok {
		t.Error("alpha row missing stale key")
	}
	// Freshly synced → stale false.
	if stale, ok := alpha["stale"].(bool); !ok || stale {
		t.Errorf("alpha stale = %v, want false after sync", alpha["stale"])
	}
}

func TestListStaleAfterEdit(t *testing.T) {
	root := t.TempDir()
	seedAndSync(t, root)

	// Mutate a rendered tree so its digest no longer matches the manifest.
	rendered := filepath.Join(root, ".claude", "skills", "alpha", "SKILL.md")
	data, err := os.ReadFile(rendered)
	if err != nil {
		t.Fatalf("read rendered: %v", err)
	}
	if err := os.WriteFile(rendered, append(data, []byte("\n<!-- drift -->\n")...), 0o644); err != nil {
		t.Fatalf("write rendered: %v", err)
	}

	stdout, _, code := runCLI(t, "--root", root, "list")
	if code != 0 {
		t.Fatalf("list failed: code=%d", code)
	}
	alpha := findRow(decodeJSONArray(t, stdout), "alpha")
	if stale, ok := alpha["stale"].(bool); !ok || !stale {
		t.Errorf("alpha stale = %v, want true after edit", alpha["stale"])
	}
}

func TestListFlagConflictFailsFast(t *testing.T) {
	root := t.TempDir()
	seedAndSync(t, root)

	stdout, stderr, code := runCLI(t, "--root", root, "list", "--local", "--vendored")
	if code == 0 {
		t.Fatalf("expected non-zero exit for --local --vendored; stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "local") || !strings.Contains(stderr, "vendored") {
		t.Errorf("expected a flag-conflict error mentioning both flags; stderr=%s", stderr)
	}
}

func TestListVendoredOrigin(t *testing.T) {
	root := t.TempDir()
	seedAndSync(t, root)
	// Hand-add a vendored lock entry (post-sync, so sync never fetches it).
	writeSyncLock(t, root, map[string]skill.LockEntry{
		"remote-deploy": {
			Source:      "github.com/acme/skills",
			URL:         "https://github.com/acme/skills",
			VersionSpec: "latest",
			Ref:         "main",
			Commit:      "abc123",
			State:       "ready",
		},
	})

	stdout, _, code := runCLI(t, "--root", root, "list", "--vendored")
	if code != 0 {
		t.Fatalf("list --vendored failed: code=%d", code)
	}
	rows := decodeJSONArray(t, stdout)
	if findRow(rows, "remote-deploy") == nil {
		t.Fatalf("vendored skill not listed: %s", stdout)
	}
	for _, r := range rows {
		if r["origin"] != "vendored" {
			t.Errorf("--vendored returned a non-vendored row: %v", r)
		}
	}
}

func TestDescribeVendoredAndUnknown(t *testing.T) {
	root := t.TempDir()
	seedAndSync(t, root)
	writeSyncLock(t, root, map[string]skill.LockEntry{
		"remote-deploy": {Source: "github.com/acme/skills", URL: "https://github.com/acme/skills", Ref: "main", Commit: "abc123", VersionSpec: "latest"},
	})

	stdout, _, code := runCLI(t, "--root", root, "describe", "remote-deploy")
	if code != 0 {
		t.Fatalf("describe failed: code=%d", code)
	}
	prov := decodeJSONMap(t, stdout)
	if prov["origin"] != "vendored" || prov["source"] != "github.com/acme/skills" || prov["commit"] != "abc123" {
		t.Errorf("unexpected provenance: %v", prov)
	}

	// Unknown name → non-zero exit, empty/parseable stdout.
	stdout, stderr, code := runCLI(t, "--root", root, "describe", "ghost")
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown skill")
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout should be empty on error, got: %s", stdout)
	}
	if !strings.Contains(stderr, "auto skill list") {
		t.Errorf("error should carry a remediation hint; stderr=%s", stderr)
	}
}

func TestDescribeAuthoredOrigin(t *testing.T) {
	root := t.TempDir()
	seedAndSync(t, root)
	stdout, _, code := runCLI(t, "--root", root, "describe", "alpha")
	if code != 0 {
		t.Fatalf("describe alpha failed: code=%d", code)
	}
	prov := decodeJSONMap(t, stdout)
	if prov["origin"] != "local" {
		t.Errorf("origin = %v, want local", prov["origin"])
	}
	if _, hasSource := prov["source"]; hasSource {
		t.Errorf("authored skill should have no source: %v", prov)
	}
}

func TestGetRenderedAndTextAndMissing(t *testing.T) {
	root := t.TempDir()
	seedAndSync(t, root)

	// JSON default wraps the content.
	stdout, _, code := runCLI(t, "--root", root, "get", "alpha")
	if code != 0 {
		t.Fatalf("get failed: code=%d", code)
	}
	payload := decodeJSONMap(t, stdout)
	if payload["name"] != "alpha" {
		t.Errorf("name = %v", payload["name"])
	}
	content, _ := payload["content"].(string)
	if !strings.Contains(content, "name: alpha") {
		t.Errorf("content missing frontmatter: %q", content)
	}

	// --format text emits raw markdown only.
	stdout, _, code = runCLI(t, "--root", root, "get", "alpha", "--format", "text")
	if code != 0 {
		t.Fatalf("get text failed: code=%d", code)
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout), "---") {
		t.Errorf("text output should be raw markdown, got: %q", stdout)
	}
	if strings.Contains(stdout, "\"content\"") {
		t.Error("text output should not be JSON-wrapped")
	}

	// Missing skill → hard error.
	_, _, code = runCLI(t, "--root", root, "get", "ghost")
	if code == 0 {
		t.Fatal("expected non-zero exit for missing skill")
	}
}

func TestGetSpecificTarget(t *testing.T) {
	root := t.TempDir()
	seedAndSync(t, root)
	stdout, _, code := runCLI(t, "--root", root, "get", "alpha", "--target", "claude")
	if code != 0 {
		t.Fatalf("get --target claude failed: code=%d", code)
	}
	payload := decodeJSONMap(t, stdout)
	if payload["target"] != "claude" {
		t.Errorf("target = %v, want claude", payload["target"])
	}
}

func TestSourceAndTargetListE2E(t *testing.T) {
	root := t.TempDir()
	seedAndSync(t, root)
	writeSyncLock(t, root, map[string]skill.LockEntry{
		"remote-deploy": {Source: "github.com/acme/skills", URL: "https://github.com/acme/skills", Ref: "main", Commit: "abc123"},
	})

	stdout, _, code := runCLI(t, "--root", root, "source", "list")
	if code != 0 {
		t.Fatalf("source list failed: code=%d", code)
	}
	sources := decodeJSONArray(t, stdout)
	if findRow(sources, "") != nil { // ID should not be empty
		t.Error("source with empty id")
	}
	if len(sources) != 1 || sources[0]["id"] != "github.com/acme/skills" {
		t.Errorf("unexpected sources: %s", stdout)
	}

	stdout, _, code = runCLI(t, "--root", root, "source", "describe", "github.com/acme/skills")
	if code != 0 {
		t.Fatalf("source describe failed: code=%d", code)
	}
	src := decodeJSONMap(t, stdout)
	if src["commit"] != "abc123" {
		t.Errorf("source commit = %v", src["commit"])
	}

	stdout, _, code = runCLI(t, "--root", root, "target", "list")
	if code != 0 {
		t.Fatalf("target list failed: code=%d", code)
	}
	targets := decodeJSONArray(t, stdout)
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2: %s", len(targets), stdout)
	}
	for _, tg := range targets {
		mc, ok := tg["managed_count"].(float64)
		if !ok || mc < 2 {
			t.Errorf("target %v managed_count = %v, want >= 2 (alpha, bravo)", tg["name"], tg["managed_count"])
		}
	}
}

func TestLsIsRemoved(t *testing.T) {
	root := t.TempDir()
	stdout, stderr, code := runCLI(t, "--root", root, "ls")
	if code == 0 {
		t.Fatalf("expected non-zero exit for removed ls command; stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "unknown command") || !strings.Contains(stderr, "ls") {
		t.Errorf("expected unknown-command error for ls; stderr=%s", stderr)
	}
}

func TestSkillUpdateIsSkillsVerbNotSelfUpdate(t *testing.T) {
	root := t.TempDir()
	seedAndSync(t, root)

	stdout, stderr, code := runCLI(t, "--root", root, "update")
	if code != 0 {
		t.Fatalf("skill update failed: code=%d stderr=%s", code, stderr)
	}
	res := decodeJSONMap(t, stdout)
	// The skills update engine returns a plan/changed/check shape, NOT the binary
	// self-update result.
	if _, ok := res["plan"]; !ok {
		t.Errorf("skill update should return the skills-update engine shape (plan/changed); got: %s", stdout)
	}
	if _, ok := res["check"]; !ok {
		t.Errorf("skill update missing 'check' field; got: %s", stdout)
	}
}

func TestQuickstartAndDocsDropLs(t *testing.T) {
	for _, sub := range []string{"quickstart", "docs"} {
		stdout, _, code := runCLI(t, sub)
		if code != 0 {
			t.Fatalf("%s failed: code=%d", sub, code)
		}
		if strings.Contains(stdout, "auto skill ls") || strings.Contains(stdout, "`ls`") {
			t.Errorf("%s still references ls:\n%s", sub, stdout)
		}
		if !strings.Contains(stdout, "list") {
			t.Errorf("%s should reference the list command:\n%s", sub, stdout)
		}
	}
	// docs should point self-update at the root `auto update`.
	stdout, _, _ := runCLI(t, "docs")
	if !strings.Contains(stdout, "auto update") {
		t.Errorf("docs should point self-update at `auto update`:\n%s", stdout)
	}
}
