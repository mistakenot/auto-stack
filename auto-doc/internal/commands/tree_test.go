package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestTreeOutput(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("getting-started.md", "Getting Started", "Setup instructions for new users", "# Getting Started")
	ws.WriteDoc("architecture.md", "Architecture", "Overview of system design and components", "# Architecture")

	entries, err := doctree.Walk(ws.Path("docs"))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	TreeOutput(&buf, entries, "docs")
	output := buf.String()

	if !strings.Contains(output, "docs/") {
		t.Error("missing root dir")
	}
	if !strings.Contains(output, "getting-started.md") {
		t.Error("missing getting-started.md")
	}
	if !strings.Contains(output, `"Getting Started"`) {
		t.Error("missing title")
	}
	if !strings.Contains(output, "Setup instructions for new users") {
		t.Error("missing summary")
	}
}

func TestTreeOutputNested(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("api/auth.md", "Authentication", "How to authenticate API requests", "# Auth")
	ws.WriteDoc("api/endpoints.md", "API Endpoints", "List of available endpoints", "# Endpoints")
	ws.WriteDoc("getting-started.md", "Getting Started", "Setup instructions", "# Setup")

	entries, err := doctree.Walk(ws.Path("docs"))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	TreeOutput(&buf, entries, "docs")
	output := buf.String()

	if !strings.Contains(output, "api/") {
		t.Error("missing api/ directory")
	}
	if !strings.Contains(output, "├──") || !strings.Contains(output, "└──") {
		t.Error("missing box-drawing characters")
	}
}

func TestTreeOutputEmpty(t *testing.T) {
	var buf bytes.Buffer
	TreeOutput(&buf, nil, "docs")
	output := buf.String()

	if !strings.Contains(output, "docs/") {
		t.Error("missing root dir for empty tree")
	}
}

func TestTreeOutputRepoRelativeMultiRootOrdering(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("alpha.md", "Alpha", "Root alpha", "# Alpha")
	ws.WriteFile("auto-etl/docs/alpha.md", `---
title: "ETL Alpha"
summary: "ETL alpha"
hash: ""
---

# ETL Alpha
`)
	ws.WriteFile("auto-etl-2/docs/alpha.md", `---
title: "ETL2 Alpha"
summary: "ETL2 alpha"
hash: ""
---

# ETL2 Alpha
`)

	entries, err := doctree.WalkRepo(ws.Dir, "docs")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	TreeOutput(&buf, entries, ".")
	output := buf.String()

	if !strings.Contains(output, "./") {
		t.Fatalf("missing repo root marker in output:\n%s", output)
	}
	if !strings.Contains(output, "auto-etl/") || !strings.Contains(output, "auto-etl-2/") {
		t.Fatalf("missing expected top-level directories in output:\n%s", output)
	}
	if strings.Count(output, "└── docs/") < 3 {
		t.Fatalf("expected docs directories under each root, got:\n%s", output)
	}

	idxRoot := strings.LastIndex(output, "\n└── docs/")
	idxAutoEtl := strings.Index(output, "auto-etl/")
	idxAutoEtl2 := strings.Index(output, "auto-etl-2/")
	if idxAutoEtl == -1 || idxAutoEtl2 == -1 || idxRoot == -1 {
		t.Fatalf("missing expected tree sections:\n%s", output)
	}
	if !(idxAutoEtl < idxAutoEtl2 && idxAutoEtl2 < idxRoot) {
		t.Fatalf("expected deterministic lexical order auto-etl < auto-etl-2 < docs, got:\n%s", output)
	}
}
