package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EFolderLinkMigration walks the self-migration an agent performs:
// fix suggests collapsing per-file tags, the agent writes one .autodoc with
// placeholder hashes, removes the inline tags, and `fixed` writes real hashes.
func TestE2EFolderLinkMigration(t *testing.T) {
	ws := t.TempDir()
	write := func(rel, content string) string {
		t.Helper()
		p := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("docs/cache.md", "---\nid: \"c0ffee00\"\ntitle: \"Cache Design\"\nsummary: \"How the cache works\"\nread_when: \"changing the cache\"\n---\n# Cache\n")
	write("pkg/cache/lru.go", "// [autodoc(c0ffee00@00000000, 00000000)]\npackage cache\n")
	write("pkg/cache/ttl.go", "// [autodoc(c0ffee00@00000000, 00000000)]\npackage cache\n")
	initGitRepo(t, ws)

	if _, stderr, exit := runCLI(t, ws, "fixed", "docs/cache.md"); exit != 0 {
		t.Fatalf("fixed doc: exit=%d stderr=%s", exit, stderr)
	}

	out, _, _ := runCLI(t, ws, "fix")
	if !strings.Contains(out, "FOLDER LINK: 2 files in pkg/cache/ link doc c0ffee00") {
		t.Fatalf("expected folder link suggestion:\n%s", out)
	}

	// Migrate: one .autodoc, inline tags removed.
	write("pkg/cache/.autodoc", "[autodoc(c0ffee00@00000000, 00000000)]\n")
	write("pkg/cache/lru.go", "package cache\n")
	write("pkg/cache/ttl.go", "package cache\n")

	stdout, stderr, exit := runCLI(t, ws, "--json", "fixed", "pkg/cache/.autodoc")
	if exit != 0 {
		t.Fatalf("fixed .autodoc: exit=%d stderr=%s", exit, stderr)
	}
	var res struct {
		Path    string `json:"path"`
		Updated []struct {
			Line   int    `json:"line"`
			NewTag string `json:"newTag"`
		} `json:"updated"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("fixed --json not parseable: %v\n%s", err, stdout)
	}
	if res.Path != "pkg/cache/.autodoc" || len(res.Updated) != 1 || res.Updated[0].Line != 1 {
		t.Fatalf("fixed result = %+v", res)
	}

	out, stderr, exit = runCLI(t, ws, "fix")
	if exit != 0 || !strings.Contains(out, "No fixes needed") || strings.Contains(out, "FOLDER LINK") {
		t.Fatalf("expected clean fix after migration: exit=%d stderr=%s\n%s", exit, stderr, out)
	}

	// A code edit anywhere in the subtree stales the single line.
	write("pkg/cache/inner/shard.go", "package inner\n")
	out, _, exit = runCLI(t, ws, "fix")
	if exit == 0 || !strings.Contains(out, "location:  pkg/cache/.autodoc:1") {
		t.Fatalf("expected stale folder link after edit: exit=%d\n%s", exit, out)
	}
	if _, stderr, exit := runCLI(t, ws, "fixed", "pkg/cache/.autodoc"); exit != 0 {
		t.Fatalf("refresh: exit=%d stderr=%s", exit, stderr)
	}
	if out, _, exit := runCLI(t, ws, "fix"); exit != 0 {
		t.Fatalf("expected clean after refresh: exit=%d\n%s", exit, out)
	}
}
