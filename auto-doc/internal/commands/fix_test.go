package commands

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/frontmatter"
	"github.com/datadyne-io/autodoc/internal/linkscan"
	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestFixWritesMissingDocID(t *testing.T) {
	ws := testutil.NewWorkspace(t)

	path := ws.WriteFile("docs/guide.md", `---
title: "Guide"
summary: "How to use the guide"
hash: "7ad7f070"
---

# Guide
`)
	ws.InitGitRepo()

	var buf bytes.Buffer
	if err := Fix(&buf, ws.Dir, "docs", 2, []string{"AGENTS.md"}, nil); err == nil {
		t.Fatal("expected error for doc issues")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := frontmatter.Parse(string(data))
	if len(doc.Id) != 8 {
		t.Fatalf("expected generated 8-char doc id, got %q", doc.Id)
	}
}

func TestFixWritesMissingDocIDInNestedDocsRoot(t *testing.T) {
	ws := testutil.NewWorkspace(t)

	path := ws.WriteFile("auto-etl/docs/guide.md", `---
title: "Guide"
summary: "Nested guide"
hash: "7ad7f070"
---

# Guide
`)
	ws.InitGitRepo()

	var buf bytes.Buffer
	if err := Fix(&buf, ws.Dir, "docs", 2, []string{"AGENTS.md"}, nil); err == nil {
		t.Fatal("expected error for doc issues")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := frontmatter.Parse(string(data))
	if len(doc.Id) != 8 {
		t.Fatalf("expected generated 8-char doc id, got %q", doc.Id)
	}
}

func TestFixReportsScopeHashMismatch(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	docHash := createDocWithID(t, ws, "docs/cache.md", "deadbeef")

	ws.WriteSourceFile("pkg/cache/lru.go", strings.TrimLeft(`
package cache

func read() {
    // [autodoc(deadbeef@DOC_HASH, 00000000)]
    value := 1
    _ = value
}
`, "\n"))
	rewriteFile(t, ws.Path("pkg/cache/lru.go"), "DOC_HASH", docHash)
	ws.InitGitRepo()

	var buf bytes.Buffer
	err := Fix(&buf, ws.Dir, "docs", 2, []string{"AGENTS.md"}, nil)
	if err == nil {
		t.Fatal("expected error for scope hash mismatch")
	}

	out := buf.String()
	if !strings.Contains(out, "LINK STALE: code changed, doc may need updating") {
		t.Fatalf("missing scope mismatch block:\n%s", out)
	}
}

func TestFixReportsDocHashMismatch(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	createDocWithID(t, ws, "docs/cache.md", "deadbeef")

	ws.WriteSourceFile("pkg/cache/lru.go", strings.TrimLeft(`
package cache

func read() {
    // [autodoc(deadbeef@00000000, 00000000)]
    value := 1
    _ = value
}
`, "\n"))
	scopeHash, err := linkscan.ComputeScopeHash(ws.Path("pkg/cache/lru.go"), 4)
	if err != nil {
		t.Fatalf("ComputeScopeHash: %v", err)
	}
	rewriteFile(t, ws.Path("pkg/cache/lru.go"), "00000000)]", scopeHash+")]")
	ws.InitGitRepo()

	var buf bytes.Buffer
	err = Fix(&buf, ws.Dir, "docs", 2, []string{"AGENTS.md"}, nil)
	if err == nil {
		t.Fatal("expected error for doc hash mismatch")
	}

	out := buf.String()
	if !strings.Contains(out, "LINK STALE: doc updated, code tag needs refresh") {
		t.Fatalf("missing doc mismatch block:\n%s", out)
	}
}

func TestFixReportsBothMismatch(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	createDocWithID(t, ws, "docs/cache.md", "deadbeef")

	ws.WriteSourceFile("pkg/cache/lru.go", strings.TrimLeft(`
package cache

func read() {
    // [autodoc(deadbeef@00000000, 11111111)]
    value := 1
    _ = value
}
`, "\n"))
	ws.InitGitRepo()

	var buf bytes.Buffer
	err := Fix(&buf, ws.Dir, "docs", 2, []string{"AGENTS.md"}, nil)
	if err == nil {
		t.Fatal("expected error for both mismatch")
	}

	out := buf.String()
	if !strings.Contains(out, "LINK STALE: both code and doc changed since last sync") {
		t.Fatalf("missing both-mismatch block:\n%s", out)
	}
}

func TestFixReportsOrphanedTag(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	createDocWithID(t, ws, "docs/cache.md", "deadbeef")

	ws.WriteSourceFile("pkg/cache/lru.go", strings.TrimLeft(`
package cache

func read() {
    // [autodoc(cafecafe@00000000, 00000000)]
    value := 1
}
`, "\n"))
	ws.InitGitRepo()

	var buf bytes.Buffer
	err := Fix(&buf, ws.Dir, "docs", 2, []string{"AGENTS.md"}, nil)
	if err == nil {
		t.Fatal("expected error for orphaned tag")
	}

	out := buf.String()
	if !strings.Contains(out, "LINK ORPHANED: doc not found for id cafecafe") {
		t.Fatalf("missing orphaned-tag block:\n%s", out)
	}
}

func TestFixReportsMalformedTagAndReturnsError(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	createDocWithID(t, ws, "docs/cache.md", "deadbeef")

	ws.WriteSourceFile("pkg/cache/lru.go", strings.TrimLeft(`
package cache

func read() {
    // [autodoc(deadbeef@bad, short)]
    value := 1
}
`, "\n"))
	ws.InitGitRepo()

	var buf bytes.Buffer
	err := Fix(&buf, ws.Dir, "docs", 2, []string{"AGENTS.md"}, nil)
	if err == nil {
		t.Fatal("expected malformed-tag error")
	}

	out := buf.String()
	if !strings.Contains(out, "LINK ERROR: malformed autodoc tag") {
		t.Fatalf("missing malformed block:\n%s", out)
	}
}

func TestFixGroupsDocIssues(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("a.md", "a", "Summary A", "# A")
	ws.WriteDoc("b.md", "b", "Summary B", "# B")
	ws.WriteDoc("c.md", "c", "Summary C", "# C")
	ws.InitGitRepo()

	var buf bytes.Buffer
	err := Fix(&buf, ws.Dir, "docs", 2, []string{"AGENTS.md"}, nil)
	if err == nil {
		t.Fatal("expected error for doc issues")
	}

	out := buf.String()
	if !strings.Contains(out, "Group 1 of 2") || !strings.Contains(out, "Group 2 of 2") {
		t.Fatalf("missing grouping output:\n%s", out)
	}
}

func TestFixLinkFreshnessUsesRepoRelativeDocPath(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	createRepoDocWithID(t, ws, "auto-etl/docs/cache.md", "deadbeef")

	ws.WriteSourceFile("pkg/cache/lru.go", strings.TrimLeft(`
package cache

func read() {
    // [autodoc(deadbeef@00000000, 00000000)]
    value := 1
    _ = value
}
`, "\n"))
	scopeHash, err := linkscan.ComputeScopeHash(ws.Path("pkg/cache/lru.go"), 4)
	if err != nil {
		t.Fatalf("ComputeScopeHash: %v", err)
	}
	rewriteFile(t, ws.Path("pkg/cache/lru.go"), "00000000)]", scopeHash+")]")
	ws.InitGitRepo()

	var buf bytes.Buffer
	err = Fix(&buf, ws.Dir, "docs", 2, []string{"AGENTS.md"}, nil)
	if err == nil {
		t.Fatal("expected error for link issues")
	}

	out := buf.String()
	if !strings.Contains(out, "doc:       auto-etl/docs/cache.md (id: deadbeef)") {
		t.Fatalf("expected repo-relative doc path in output:\n%s", out)
	}
}

func TestFixReportsEmptyReadWhen(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("guide.md", "Guide", "How to use the guide", "# Guide")
	ws.InitGitRepo()

	var buf bytes.Buffer
	err := Fix(&buf, ws.Dir, "docs", 2, []string{"AGENTS.md"}, nil)
	if err == nil {
		t.Fatal("expected error for doc issues")
	}

	out := buf.String()
	if !strings.Contains(out, "read_when") {
		t.Fatalf("expected read_when instruction in output:\n%s", out)
	}
}

func TestFixNoIssueWhenReadWhenPresent(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDocWithReadWhen("guide.md", "Guide", "How to use the guide", "when updating the guide", "# Guide")

	guidePath := ws.Path("docs/guide.md")
	if err := Fixed(guidePath, "", ""); err != nil {
		t.Fatalf("Fixed: %v", err)
	}

	ws.InitGitRepo()

	var buf bytes.Buffer
	err := Fix(&buf, ws.Dir, "docs", 2, []string{"AGENTS.md"}, nil)
	if err != nil {
		t.Fatalf("expected no issues, got error: %v\noutput:\n%s", err, buf.String())
	}
}

func createDocWithID(t *testing.T, ws *testutil.Workspace, relPath, id string) string {
	t.Helper()
	path := ws.WriteDocWithId(strings.TrimPrefix(relPath, "docs/"), id, "Cache", "Cache docs", "", "# Cache")
	if err := Fixed(path, "", ""); err != nil {
		t.Fatalf("Fixed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return frontmatter.Parse(string(data)).Hash
}

func createRepoDocWithID(t *testing.T, ws *testutil.Workspace, repoRelPath, id string) string {
	t.Helper()
	path := ws.WriteFile(repoRelPath, frontmatter.Serialize(&frontmatter.Doc{
		Id:      id,
		Title:   "Cache",
		Summary: "Cache docs",
		Hash:    "",
		Body:    "\n# Cache\n",
	}))
	if err := Fixed(path, "", ""); err != nil {
		t.Fatalf("Fixed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return frontmatter.Parse(string(data)).Hash
}

func rewriteFile(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), old, new, 1)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
}
