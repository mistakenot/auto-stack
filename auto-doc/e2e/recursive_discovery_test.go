package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/frontmatter"
)

func TestE2ERecursiveDiscoveryAcrossCommands(t *testing.T) {
	workspace := t.TempDir()
	writeDoc(t, workspace, "docs/root.md", "Root", "Root docs", "# Root\n\nrootuniquetoken")
	writeDoc(t, workspace, "auto-doc/docs/subdoc.md", "Subdoc", "Subproject docs", "# Subdoc\n\nautodocuniquetoken")
	writeDoc(t, workspace, "auto-etl-2/docs/etl2.md", "ETL2", "ETL2 docs", "# ETL2\n\netl2uniquetoken")
	initGitRepo(t, workspace)

	treeOut, stderr, exit := runCLI(t, workspace, "tree")
	if exit != 0 {
		t.Fatalf("autodoc tree failed: exit=%d stderr=%s", exit, stderr)
	}
	for _, needle := range []string{"root.md", "subdoc.md", "etl2.md"} {
		if !strings.Contains(treeOut, needle) {
			t.Fatalf("tree output missing %s:\n%s", needle, treeOut)
		}
	}

	staleOut, stderr, exit := runCLI(t, workspace, "stale")
	if exit == 0 {
		t.Fatalf("autodoc stale should report stale docs for empty hashes; output:\n%s", staleOut)
	}
	if stderr != "" && !strings.Contains(stderr, "stale") {
		t.Fatalf("unexpected stale stderr: %s", stderr)
	}
	for _, needle := range []string{"root.md", "subdoc.md", "etl2.md"} {
		if !strings.Contains(staleOut, needle) {
			t.Fatalf("stale output missing %s:\n%s", needle, staleOut)
		}
	}

	fixOut, stderr, exit := runCLI(t, workspace, "fix")
	if exit != 0 {
		t.Fatalf("autodoc fix failed: exit=%d stderr=%s", exit, stderr)
	}
	for _, needle := range []string{"`docs/root.md`", "`auto-doc/docs/subdoc.md`", "`auto-etl-2/docs/etl2.md`"} {
		if !strings.Contains(fixOut, needle) {
			t.Fatalf("fix output missing %s:\n%s", needle, fixOut)
		}
	}

	_, stderr, exit = runCLI(t, workspace, "search", "reindex")
	if exit != 0 {
		t.Fatalf("autodoc search reindex failed: exit=%d stderr=%s", exit, stderr)
	}

	searchOut, stderr, exit := runCLI(t, workspace, "search", "keyword", "etl2uniquetoken")
	if exit != 0 {
		t.Fatalf("autodoc search keyword failed: exit=%d stderr=%s", exit, stderr)
	}
	if !strings.Contains(searchOut, "auto-etl-2/docs/etl2.md") {
		t.Fatalf("search results missing nested path:\n%s", searchOut)
	}
}

func TestE2EGitDiscoveryIncludesUntrackedAndExcludesIgnored(t *testing.T) {
	workspace := t.TempDir()
	writeDoc(t, workspace, "docs/tracked.md", "Tracked", "Tracked docs", "# Tracked")
	if err := os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("ignored/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, workspace)

	writeDoc(t, workspace, "packages/payments/docs/untracked.md", "Untracked", "Untracked docs", "# Untracked")
	writeDoc(t, workspace, "ignored/docs/hidden.md", "Hidden", "Hidden docs", "# Hidden")

	out, stderr, exit := runCLI(t, workspace, "tree")
	if exit != 0 {
		t.Fatalf("autodoc tree failed: exit=%d stderr=%s", exit, stderr)
	}
	if !strings.Contains(out, "untracked.md") {
		t.Fatalf("tree output missing untracked docs file:\n%s", out)
	}
	if strings.Contains(out, "hidden.md") {
		t.Fatalf("tree output should exclude git-ignored docs file:\n%s", out)
	}
}

func TestE2ENonGitDiscoveryFallback(t *testing.T) {
	workspace := t.TempDir()
	writeDoc(t, workspace, "services/identity/docs/identity.md", "Identity", "Identity docs", "# Identity")

	out, stderr, exit := runCLI(t, workspace, "tree")
	if exit != 0 {
		t.Fatalf("autodoc tree failed in non-git workspace: exit=%d stderr=%s", exit, stderr)
	}
	if !strings.Contains(out, "identity.md") {
		t.Fatalf("tree output missing fallback-discovered doc:\n%s", out)
	}
}

func TestE2EAgentsNearestOwnerRouting(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("# Root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "services", "payments"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "services", "payments", "CLAUDE.md"), []byte("# Payments\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, workspace, "services/payments/docs/payments.md", "Payments", "Payments docs", "# Payments")
	writeDoc(t, workspace, "services/identity/docs/identity.md", "Identity", "Identity docs", "# Identity")

	_, stderr, exit := runCLI(t, workspace, "agents")
	if exit != 0 {
		t.Fatalf("autodoc agents failed: exit=%d stderr=%s", exit, stderr)
	}

	paymentsData, err := os.ReadFile(filepath.Join(workspace, "services", "payments", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootData, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}

	payments := string(paymentsData)
	root := string(rootData)
	if !strings.Contains(payments, "services/payments/docs/payments.md") {
		t.Fatalf("payments owner missing payments doc:\n%s", payments)
	}
	if strings.Contains(payments, "services/identity/docs/identity.md") {
		t.Fatalf("payments owner should not duplicate identity doc:\n%s", payments)
	}
	if !strings.Contains(root, "services/identity/docs/identity.md") {
		t.Fatalf("root owner missing identity doc:\n%s", root)
	}
	if strings.Contains(root, "services/payments/docs/payments.md") {
		t.Fatalf("root owner should not duplicate payments doc:\n%s", root)
	}
}

func TestE2EAgentsSameLevelFilesAndRootFallback(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "services", "billing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "services", "billing", "AGENTS.md"), []byte("# Billing AGENTS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "services", "billing", "CLAUDE.md"), []byte("# Billing CLAUDE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, workspace, "services/billing/docs/billing.md", "Billing", "Billing docs", "# Billing")
	writeDoc(t, workspace, "services/audit/docs/audit.md", "Audit", "Audit docs", "# Audit")

	_, stderr, exit := runCLI(t, workspace, "agents")
	if exit != 0 {
		t.Fatalf("autodoc agents failed: exit=%d stderr=%s", exit, stderr)
	}

	billingAgents, err := os.ReadFile(filepath.Join(workspace, "services", "billing", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	billingClaude, err := os.ReadFile(filepath.Join(workspace, "services", "billing", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootAgents, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(billingAgents), "services/billing/docs/billing.md") {
		t.Fatalf("billing AGENTS missing local doc link:\n%s", billingAgents)
	}
	if !strings.Contains(string(billingClaude), "services/billing/docs/billing.md") {
		t.Fatalf("billing CLAUDE missing local doc link:\n%s", billingClaude)
	}
	if !strings.Contains(string(rootAgents), "services/audit/docs/audit.md") {
		t.Fatalf("root fallback owner missing audit doc:\n%s", rootAgents)
	}
}

func TestE2ESearchReindexRemovesDeletedDocs(t *testing.T) {
	workspace := t.TempDir()
	writeDoc(t, workspace, "docs/alpha.md", "Alpha", "Alpha docs", "# Alpha\n\nalphatoken")
	betaPath := writeDoc(t, workspace, "docs/beta.md", "Beta", "Beta docs", "# Beta\n\nbetatoken")
	initGitRepo(t, workspace)

	_, stderr, exit := runCLI(t, workspace, "search", "reindex")
	if exit != 0 {
		t.Fatalf("initial reindex failed: exit=%d stderr=%s", exit, stderr)
	}

	if err := os.Remove(betaPath); err != nil {
		t.Fatalf("remove beta doc: %v", err)
	}
	_, stderr, exit = runCLI(t, workspace, "search", "reindex")
	if exit != 0 {
		t.Fatalf("second reindex failed: exit=%d stderr=%s", exit, stderr)
	}

	out, stderr, exit := runCLI(t, workspace, "search", "keyword", "betatoken")
	if exit != 0 {
		t.Fatalf("search keyword failed: exit=%d stderr=%s", exit, stderr)
	}
	if strings.Contains(out, "docs/beta.md") {
		t.Fatalf("deleted doc still appears in search results:\n%s", out)
	}
}

func writeDoc(t *testing.T, root, relPath, title, summary, body string) string {
	t.Helper()
	absPath := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", relPath, err)
	}
	doc := frontmatter.Doc{
		Title:   title,
		Summary: summary,
		Hash:    "",
		Body:    "\n" + body + "\n",
	}
	if err := os.WriteFile(absPath, []byte(frontmatter.Serialize(&doc)), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
	return absPath
}
