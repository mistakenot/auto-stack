package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2EMarkdownEmbeddedTagsLifecycle(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	referencePath := filepath.Join(workspace, "docs", "reference.md")
	consumerPath := filepath.Join(workspace, "docs", "consumer.md")
	sectionPath := filepath.Join(workspace, "docs", "section-scoped.md")

	if err := os.WriteFile(referencePath, []byte(strings.TrimLeft(`
---
id: "deadbeef"
title: "Reference"
summary: "Reference doc"
read_when: "updating the shared reference"
hash: "00000000"
---

# Reference

Canonical content.
`, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(consumerPath, []byte(strings.TrimLeft(`
---
id: "feedface"
title: "Consumer"
summary: "Consumer doc"
read_when: "updating consumer docs"
hash: "00000000"
---
<!-- [autodoc(deadbeef@00000000, 00000000)] -->

# Consumer

Whole-doc content.
`, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sectionPath, []byte(strings.TrimLeft(`
---
id: "c0ffee00"
title: "Section Scoped"
summary: "Section scoped doc"
read_when: "updating section scoped docs"
hash: "00000000"
---

# Parent

## Target
<!-- [autodoc(deadbeef@00000000, 00000000)] -->

inside target

## Other

outside target
`, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	initGitRepo(t, workspace)

	_, stderr, exit := runCLI(t, workspace, "fixed", "docs/reference.md")
	if exit != 0 {
		t.Fatalf("fixed reference: exit=%d stderr=%s", exit, stderr)
	}
	referenceDoc := readDoc(t, referencePath)
	rewriteMarkdownTag(t, consumerPath, referenceDoc.Id, referenceDoc.Hash, "00000000")
	rewriteMarkdownTag(t, sectionPath, referenceDoc.Id, referenceDoc.Hash, "00000000")

	out, _, exit := runCLI(t, workspace, "fix")
	if exit == 0 {
		t.Fatalf("expected markdown scope mismatch, got clean output:\n%s", out)
	}
	if strings.Count(out, "LINK STALE: source changed, doc may need updating") != 2 {
		t.Fatalf("expected two source mismatches, got:\n%s", out)
	}

	_, stderr, exit = runCLI(t, workspace, "fixed", "docs/consumer.md")
	if exit != 0 {
		t.Fatalf("fixed consumer: exit=%d stderr=%s", exit, stderr)
	}
	_, stderr, exit = runCLI(t, workspace, "fixed", "docs/section-scoped.md")
	if exit != 0 {
		t.Fatalf("fixed section-scoped: exit=%d stderr=%s", exit, stderr)
	}

	out, stderr, exit = runCLI(t, workspace, "fix")
	if exit != 0 {
		t.Fatalf("expected clean markdown fix after fixed rewrites: exit=%d stderr=%s\n%s", exit, stderr, out)
	}

	rewriteText(t, sectionPath, "outside target", "outside target changed")
	_, stderr, exit = runCLI(t, workspace, "fixed", "docs/section-scoped.md")
	if exit != 0 {
		t.Fatalf("fixed section-scoped after outer edit: exit=%d stderr=%s", exit, stderr)
	}
	out, stderr, exit = runCLI(t, workspace, "fix")
	if exit != 0 {
		t.Fatalf("expected clean fix for edit outside tagged section: exit=%d stderr=%s\n%s", exit, stderr, out)
	}

	rewriteText(t, sectionPath, "inside target", "inside target changed")
	out, _, exit = runCLI(t, workspace, "fix")
	if exit == 0 {
		t.Fatalf("expected mismatch for edit inside tagged section, got clean output:\n%s", out)
	}
	if strings.Count(out, "LINK STALE: source changed, doc may need updating") != 1 {
		t.Fatalf("expected one section-scoped mismatch, got:\n%s", out)
	}
}
