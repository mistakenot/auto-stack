package linkscan

import (
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestScanMarkdownDocsFindsWrappedTagsOnly(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteFile("docs/reference.md", `---
id: "deadbeef"
title: "Reference"
summary: "Reference doc"
hash: "cafebabe"
---

# Reference
`)
	ws.WriteFile("docs/consumer.md", strings.TrimLeft(`
---
id: "feedface"
title: "Consumer"
summary: "Consumer doc"
hash: "00000000"
---
<!-- [autodoc(deadbeef@cafebabe, 00000000)] -->

# Consumer

[autodoc(deadbeef@cafebabe, 00000000)]

<!-- [autodoc(deadbeef@bad, short)] -->
`, "\n"))

	entries, err := doctree.WalkRepo(ws.Dir, "docs")
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	result, err := ScanMarkdownDocs(entries)
	if err != nil {
		t.Fatalf("ScanMarkdownDocs: %v", err)
	}

	if len(result.Tags) != 1 {
		t.Fatalf("len(result.Tags) = %d, want 1", len(result.Tags))
	}
	if result.Tags[0].ScopeKind != ScopeKindMarkdown {
		t.Fatalf("ScopeKind = %v, want markdown", result.Tags[0].ScopeKind)
	}
	if len(result.Malformed) != 1 {
		t.Fatalf("len(result.Malformed) = %d, want 1", len(result.Malformed))
	}
}

func TestComputeMarkdownScopeHashWholeDocAndSection(t *testing.T) {
	content := strings.TrimLeft(`
---
id: "feedface"
title: "Consumer"
summary: "Consumer doc"
hash: "00000000"
---
<!-- [autodoc(deadbeef@cafebabe, 00000000)] -->

# Title

## Section One
<!-- [autodoc(deadbeef@cafebabe, 11111111)] -->
keep me

### Nested
still inside

## Section Two
outside
`, "\n")

	wholeTag := Tag{Line: 7, ScopeKind: ScopeKindMarkdown}
	sectionTag := Tag{Line: 12, ScopeKind: ScopeKindMarkdown}

	wholeHash1, err := ComputeScopeHashFromContentForTag(content, &wholeTag)
	if err != nil {
		t.Fatalf("whole scope hash: %v", err)
	}
	sectionHash1, err := ComputeScopeHashFromContentForTag(content, &sectionTag)
	if err != nil {
		t.Fatalf("section scope hash: %v", err)
	}

	changedOutsideSection := strings.Replace(content, "outside", "outside changed", 1)
	wholeHash2, err := ComputeScopeHashFromContentForTag(changedOutsideSection, &wholeTag)
	if err != nil {
		t.Fatalf("whole scope hash after outer edit: %v", err)
	}
	sectionHash2, err := ComputeScopeHashFromContentForTag(changedOutsideSection, &sectionTag)
	if err != nil {
		t.Fatalf("section scope hash after outer edit: %v", err)
	}

	if wholeHash1 == wholeHash2 {
		t.Fatal("expected whole-doc hash to change when later content changes")
	}
	if sectionHash1 != sectionHash2 {
		t.Fatal("expected section hash to ignore edits outside the anchored section")
	}
}

func TestScanMarkdownDocsIgnoresInlineCodeMentions(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteFile("docs/guide.md", strings.TrimLeft(`
---
id: "feedface"
title: "Guide"
summary: "Guide doc"
hash: "00000000"
---

# Guide

Skills use the `+"`[autodoc(<docId>@<docHash>, <scopeHash>)]`"+` comment syntax. Place it as an HTML comment (`+"`<!-- [autodoc(...)] -->`"+`) above the section.

Typos like `+"`<!-- [autodoc(abc@def) -->`"+` or `+"`<!-- [autodoc(abcdefgh@deadbeef, 123)] -->`"+` emit malformed errors.
`, "\n"))

	entries, err := doctree.WalkRepo(ws.Dir, "docs")
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	result, err := ScanMarkdownDocs(entries)
	if err != nil {
		t.Fatalf("ScanMarkdownDocs: %v", err)
	}
	if len(result.Tags) != 0 {
		t.Fatalf("len(result.Tags) = %d, want 0", len(result.Tags))
	}
	if len(result.Malformed) != 0 {
		t.Fatalf("len(result.Malformed) = %d, want 0 (inline code spans should be treated as prose)", len(result.Malformed))
	}
}

func TestScanMarkdownDocsIgnoresFencedExamples(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteFile("docs/guide.md", strings.TrimLeft(`
---
id: "feedface"
title: "Guide"
summary: "Guide doc"
hash: "00000000"
---

# Guide

`+"\n```md\n<!-- [autodoc(deadbeef@cafebabe, 00000000)] -->\n## Fake heading\n```\n", "\n"))

	entries, err := doctree.WalkRepo(ws.Dir, "docs")
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	result, err := ScanMarkdownDocs(entries)
	if err != nil {
		t.Fatalf("ScanMarkdownDocs: %v", err)
	}
	if len(result.Tags) != 0 {
		t.Fatalf("len(result.Tags) = %d, want 0", len(result.Tags))
	}
}
