package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestAgentsCreatesFileWhenNoneExist(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("test.md", "Test", "A test", "# Test")

	err := Agents(ws.Dir, "docs", []string{"AGENTS.md", "CLAUDE.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should create AGENTS.md (first in list)
	data, err := os.ReadFile(ws.Path("AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, markerStart) {
		t.Error("missing start marker")
	}
	if !strings.Contains(content, markerEnd) {
		t.Error("missing end marker")
	}
	if !strings.Contains(content, "test.md") {
		t.Error("missing doc entry")
	}
}

func TestAgentsAppendsToExistingFile(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("test.md", "Test", "A test", "# Test")
	ws.WriteFile("CLAUDE.md", "# My Agent File\n\nExisting content.\n")

	err := Agents(ws.Dir, "docs", []string{"AGENTS.md", "CLAUDE.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(ws.Path("CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "Existing content.") {
		t.Error("lost existing content")
	}
	if !strings.Contains(content, markerStart) {
		t.Error("missing markers")
	}
}

func TestAgentsReplacesExistingMarkers(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("test.md", "Test", "A test", "# Test")

	existing := "# Agent\n\n" + markerStart + "\nold content\n" + markerEnd + "\n\nMore stuff.\n"
	ws.WriteFile("AGENTS.md", existing)

	err := Agents(ws.Dir, "docs", []string{"AGENTS.md", "CLAUDE.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(ws.Path("AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if strings.Contains(content, "old content") {
		t.Error("old content not replaced")
	}
	if !strings.Contains(content, "test.md") {
		t.Error("missing new tree content")
	}
	if !strings.Contains(content, "More stuff.") {
		t.Error("lost content after markers")
	}
}

func TestAgentsIncludesSearchExamples(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("test.md", "Test", "A test", "# Test")

	err := Agents(ws.Dir, "docs", []string{"AGENTS.md", "CLAUDE.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(ws.Path("AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "autodoc search keyword") {
		t.Error("missing search keyword examples")
	}
	if !strings.Contains(content, "autodoc search reindex") {
		t.Error("missing reindex instruction")
	}
}

func TestAgentsWorksWithCLAUDEmd(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("test.md", "Test", "A test", "# Test")
	ws.WriteFile("CLAUDE.md", "# Claude\n")

	err := Agents(ws.Dir, "docs", []string{"AGENTS.md", "CLAUDE.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(ws.Path("CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), markerStart) {
		t.Error("CLAUDE.md not updated")
	}
}

func TestAgentsRoutesDocsToNearestAncestorOwner(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteFile("AGENTS.md", "# Root Agent\n")
	ws.WriteFile("services/payments/CLAUDE.md", "# Payments Agent\n")
	writeRepoDocFile(t, ws, "services/payments/docs/payments.md", "Payments", "Payments docs")
	writeRepoDocFile(t, ws, "services/identity/docs/identity.md", "Identity", "Identity docs")

	err := Agents(ws.Dir, "docs", []string{"AGENTS.md", "CLAUDE.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	paymentsData, err := os.ReadFile(ws.Path("services/payments/CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootData, err := os.ReadFile(ws.Path("AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}

	payments := string(paymentsData)
	root := string(rootData)
	if !strings.Contains(payments, "services/payments/docs/payments.md") {
		t.Fatalf("payments owner missing its local doc:\n%s", payments)
	}
	if strings.Contains(payments, "services/identity/docs/identity.md") {
		t.Fatalf("payments owner should not contain identity doc:\n%s", payments)
	}
	if !strings.Contains(root, "services/identity/docs/identity.md") {
		t.Fatalf("root owner missing identity doc:\n%s", root)
	}
	if strings.Contains(root, "services/payments/docs/payments.md") {
		t.Fatalf("root owner should not duplicate payments doc:\n%s", root)
	}
}

func TestAgentsUpdatesAllAgentFilesAtSameLevel(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteFile("services/payments/AGENTS.md", "# Payments AGENTS\n")
	ws.WriteFile("services/payments/CLAUDE.md", "# Payments CLAUDE\n")
	writeRepoDocFile(t, ws, "services/payments/docs/payments.md", "Payments", "Payments docs")

	err := Agents(ws.Dir, "docs", []string{"AGENTS.md", "CLAUDE.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	agentsData, err := os.ReadFile(ws.Path("services/payments/AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	claudeData, err := os.ReadFile(ws.Path("services/payments/CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}

	agents := string(agentsData)
	claude := string(claudeData)
	if !strings.Contains(agents, "services/payments/docs/payments.md") {
		t.Fatalf("AGENTS.md missing local doc link:\n%s", agents)
	}
	if !strings.Contains(claude, "services/payments/docs/payments.md") {
		t.Fatalf("CLAUDE.md missing local doc link:\n%s", claude)
	}
}

func TestAgentsCreatesRootFallbackOwnerForNestedDocs(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	writeRepoDocFile(t, ws, "services/payments/docs/payments.md", "Payments", "Payments docs")

	err := Agents(ws.Dir, "docs", []string{"AGENTS.md", "CLAUDE.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(ws.Path("AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "services/payments/docs/payments.md") {
		t.Fatalf("fallback owner missing nested doc link:\n%s", content)
	}
}

func TestAgentsIncludesReadWhenInIndex(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDocWithReadWhen("test.md", "Test", "A test", "running tests", "# Test")

	err := Agents(ws.Dir, "docs", []string{"CLAUDE.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(ws.Path("CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, ". Read when: running tests") {
		t.Fatalf("expected read_when in agents output:\n%s", content)
	}
}

func TestAgentsSymlinkedRootFilesBothReceiveIndex(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	writeRepoDocFile(t, ws, "docs/root.md", "Root", "Root docs")

	ws.WriteFile("CLAUDE.md", "# Claude\n")
	if err := os.Symlink("CLAUDE.md", ws.Path("AGENTS.md")); err != nil {
		t.Fatalf("create symlink AGENTS.md -> CLAUDE.md: %v", err)
	}

	err := Agents(ws.Dir, "docs", []string{"AGENTS.md", "CLAUDE.md"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	agentsData, err := os.ReadFile(ws.Path("AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	claudeData, err := os.ReadFile(ws.Path("CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(agentsData), "docs/root.md") {
		t.Fatalf("AGENTS.md missing root doc link:\n%s", agentsData)
	}
	if !strings.Contains(string(claudeData), "docs/root.md") {
		t.Fatalf("CLAUDE.md missing root doc link:\n%s", claudeData)
	}
}

func writeRepoDocFile(t *testing.T, ws *testutil.Workspace, relPath, title, summary string) {
	t.Helper()
	ws.WriteFile(relPath, `---
title: "`+title+`"
summary: "`+summary+`"
hash: ""
---

# `+title+`
`)
}
