package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/frontmatter"
	"github.com/datadyne-io/autodoc/internal/linkscan"
)

func TestE2ETwoWayFreshnessLifecycle(t *testing.T) {
	workspace := t.TempDir()
	copyFixtureTree(t, filepath.Join("testdata", "two_way_freshness"), workspace)
	initGitRepo(t, workspace)

	_, stderr, exit := runCLI(t, workspace, "init")
	if exit != 0 {
		t.Fatalf("autodoc init failed: exit=%d stderr=%s", exit, stderr)
	}

	_, _, exit = runCLI(t, workspace, "fix")
	// fix assigns missing IDs but reports doc issues (stale hash), so non-zero is expected
	_ = exit

	docPath := filepath.Join(workspace, "docs", "caching.md")
	doc := readDoc(t, docPath)
	if len(doc.Id) != 8 {
		t.Fatalf("expected generated doc id, got %q", doc.Id)
	}

	_, stderr, exit = runCLI(t, workspace, "fixed", "docs/caching.md")
	if exit != 0 {
		t.Fatalf("autodoc fixed failed: exit=%d stderr=%s", exit, stderr)
	}
	doc = readDoc(t, docPath)

	codePath := filepath.Join(workspace, "pkg", "cache", "lru.go")
	rewriteAutodocTag(t, codePath, doc.Id, "00000000", "00000000")

	out, _, exit := runCLI(t, workspace, "fix")
	if exit == 0 {
		t.Fatalf("expected non-zero exit for both mismatch, got exit=%d", exit)
	}
	if !strings.Contains(out, "LINK STALE: both source and doc changed since last sync") {
		t.Fatalf("expected both-mismatch output, got:\n%s", out)
	}
	docHash, scopeHash := extractCurrentHashes(t, out)
	rewriteAutodocTag(t, codePath, doc.Id, docHash, scopeHash)

	out, stderr, exit = runCLI(t, workspace, "fix")
	if exit != 0 {
		t.Fatalf("autodoc fix (clean) failed: exit=%d stderr=%s", exit, stderr)
	}
	if !strings.Contains(out, "No fixes needed") {
		t.Fatalf("expected clean fix output, got:\n%s", out)
	}

	rewriteText(t, codePath, "value := 1", "value := 2")
	out, _, exit = runCLI(t, workspace, "fix")
	if exit == 0 {
		t.Fatalf("expected non-zero exit for scope mismatch, got exit=%d", exit)
	}
	if !strings.Contains(out, "LINK STALE: source changed, doc may need updating") {
		t.Fatalf("expected scope-mismatch output, got:\n%s", out)
	}
	docHash, scopeHash = extractCurrentHashes(t, out)
	rewriteAutodocTag(t, codePath, doc.Id, docHash, scopeHash)

	out, stderr, exit = runCLI(t, workspace, "fix")
	if exit != 0 {
		t.Fatalf("autodoc fix (clean after scope update) failed: exit=%d stderr=%s", exit, stderr)
	}
	if !strings.Contains(out, "No fixes needed") {
		t.Fatalf("expected clean fix output after scope update, got:\n%s", out)
	}

	rewriteText(t, docPath, `summary: "Caching behavior"`, `summary: "Caching behavior and invalidation"`)
	_, stderr, exit = runCLI(t, workspace, "fixed", "docs/caching.md")
	if exit != 0 {
		t.Fatalf("autodoc fixed after doc edit failed: exit=%d stderr=%s", exit, stderr)
	}

	out, _, exit = runCLI(t, workspace, "fix")
	if exit == 0 {
		t.Fatalf("expected non-zero exit for doc mismatch, got exit=%d", exit)
	}
	if !strings.Contains(out, "LINK STALE: doc updated, source tag needs refresh") {
		t.Fatalf("expected doc-mismatch output, got:\n%s", out)
	}

	if err := os.Remove(docPath); err != nil {
		t.Fatalf("remove doc: %v", err)
	}
	out, _, exit = runCLI(t, workspace, "fix")
	if exit == 0 {
		t.Fatalf("expected non-zero exit for orphaned tag, got exit=%d", exit)
	}
	if !strings.Contains(out, "LINK ORPHANED: doc not found") {
		t.Fatalf("expected orphaned output, got:\n%s", out)
	}
}

func TestE2EOneDocReferencedByManyFiles(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "pkg", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "pkg", "b"), 0o755); err != nil {
		t.Fatal(err)
	}

	doc := frontmatter.Doc{Id: "deadbeef", Title: "Shared", Summary: "Shared doc", ReadWhen: "when using shared resources", Hash: "", Body: "\n# Shared\n"}
	if err := os.WriteFile(filepath.Join(workspace, "docs", "shared.md"), []byte(frontmatter.Serialize(&doc)), 0o644); err != nil {
		t.Fatal(err)
	}

	fileA := filepath.Join(workspace, "pkg", "a", "one.go")
	fileB := filepath.Join(workspace, "pkg", "b", "two.go")
	if err := os.WriteFile(fileA, []byte("package a\n\nfunc one() {\n    // [autodoc(deadbeef@00000000, 00000000)]\n    _ = 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("package b\n\nfunc two() {\n    // [autodoc(deadbeef@00000000, 00000000)]\n    _ = 2\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, exit := runCLI(t, workspace, "fixed", "docs/shared.md")
	if exit != 0 {
		t.Fatalf("autodoc fixed failed: exit=%d stderr=%s", exit, stderr)
	}
	fixedDoc := readDoc(t, filepath.Join(workspace, "docs", "shared.md"))
	scopeA, err := linkscan.ComputeScopeHash(fileA, 4)
	if err != nil {
		t.Fatalf("scopeA: %v", err)
	}
	scopeB, err := linkscan.ComputeScopeHash(fileB, 4)
	if err != nil {
		t.Fatalf("scopeB: %v", err)
	}
	rewriteAutodocTag(t, fileA, fixedDoc.Id, "00000000", scopeA)
	rewriteAutodocTag(t, fileB, fixedDoc.Id, "00000000", scopeB)

	initGitRepo(t, workspace)
	out, _, exit := runCLI(t, workspace, "fix")
	if exit == 0 {
		t.Fatalf("expected non-zero exit for link issues, got exit=%d", exit)
	}

	if strings.Count(out, "LINK STALE: doc updated, source tag needs refresh") != 2 {
		t.Fatalf("expected two doc-mismatch blocks, got:\n%s", out)
	}
	if !strings.Contains(out, "pkg/a/one.go") || !strings.Contains(out, "pkg/b/two.go") {
		t.Fatalf("expected both file paths in output, got:\n%s", out)
	}
}

func TestE2EMultipleTagsOneFileIsolatedScopes(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}

	doc := frontmatter.Doc{Id: "deadbeef", Title: "Cache", Summary: "Cache doc", ReadWhen: "when modifying cache logic", Hash: "", Body: "\n# Cache\n"}
	if err := os.WriteFile(filepath.Join(workspace, "docs", "cache.md"), []byte(frontmatter.Serialize(&doc)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, exit := runCLI(t, workspace, "fixed", "docs/cache.md")
	if exit != 0 {
		t.Fatalf("autodoc fixed failed: exit=%d stderr=%s", exit, stderr)
	}
	fixedDoc := readDoc(t, filepath.Join(workspace, "docs", "cache.md"))

	codePath := filepath.Join(workspace, "pkg", "multi.go")
	code := strings.TrimLeft(`
package pkg

func first() {
    // [autodoc(deadbeef@DOC_HASH, 00000000)]
    a := 1
    _ = a
}

func second() {
    // [autodoc(deadbeef@DOC_HASH, 11111111)]
    b := 2
    _ = b
}
`, "\n")
	if err := os.WriteFile(codePath, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	rewriteText(t, codePath, "DOC_HASH", fixedDoc.Hash)
	rewriteText(t, codePath, "DOC_HASH", fixedDoc.Hash)

	scope1, err := linkscan.ComputeScopeHash(codePath, 4)
	if err != nil {
		t.Fatalf("scope1: %v", err)
	}
	scope2, err := linkscan.ComputeScopeHash(codePath, 10)
	if err != nil {
		t.Fatalf("scope2: %v", err)
	}
	rewriteText(t, codePath, "00000000", scope1)
	rewriteText(t, codePath, "11111111", scope2)

	initGitRepo(t, workspace)
	out, stderr, exit := runCLI(t, workspace, "fix")
	if exit != 0 {
		t.Fatalf("autodoc fix (clean) failed: exit=%d stderr=%s", exit, stderr)
	}
	if !strings.Contains(out, "No fixes needed") {
		t.Fatalf("expected clean output, got:\n%s", out)
	}

	rewriteText(t, codePath, "a := 1", "a := 99")
	out, _, exit = runCLI(t, workspace, "fix")
	if exit == 0 {
		t.Fatalf("expected non-zero exit for scope edit, got exit=%d", exit)
	}
	if strings.Count(out, "LINK STALE: source changed, doc may need updating") != 1 {
		t.Fatalf("expected one scope-mismatch block, got:\n%s", out)
	}
}
