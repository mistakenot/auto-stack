package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mistakenot/auto-graph/internal/codegraph"
	"github.com/mistakenot/auto-graph/internal/graph"
)

func TestAstGrepNotFound(t *testing.T) {
	// Set PATH to an empty directory so ast-grep cannot be found.
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir)

	// Create a project dir with tsconfig.json so language detection passes.
	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "tsconfig.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := newCodeGraphCmd()
	err := runCodeGraph(cmd, projDir, "json", "typescript")
	if err == nil {
		t.Fatal("expected error when ast-grep is not found, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "ast-grep not found") {
		t.Errorf("error message should mention 'ast-grep not found', got: %s", errMsg)
	}
	// Check for remediation hint.
	if !strings.Contains(errMsg, "npm") && !strings.Contains(errMsg, "brew") {
		t.Errorf("error message should contain remediation hint (npm or brew), got: %s", errMsg)
	}
}

func TestAstGrepNotCheckedForGo(t *testing.T) {
	// Set PATH to an empty directory so ast-grep cannot be found.
	tmpDir := t.TempDir()
	t.Setenv("PATH", tmpDir)

	// Create a Go project dir with go.mod and a .go file.
	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "main.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := newCodeGraphCmd()
	cmd.SetOut(&bytes.Buffer{})
	err := runCodeGraph(cmd, projDir, "json", "go")
	// Should NOT fail with ast-grep error — Go doesn't need it.
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "ast-grep") {
			t.Errorf("Go scanning should not require ast-grep, but got: %s", errMsg)
		}
	}
}

func TestLanguageAutoDetection(t *testing.T) {
	// Create a temp dir with tsconfig.json.
	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "tsconfig.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	lang, err := codegraph.DetectLanguage(projDir)
	if err != nil {
		t.Fatalf("DetectLanguage failed: %v", err)
	}
	if lang != "typescript" {
		t.Errorf("expected language %q, got %q", "typescript", lang)
	}
}

func TestLanguageAutoDetectionGo(t *testing.T) {
	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	lang, err := codegraph.DetectLanguage(projDir)
	if err != nil {
		t.Fatalf("codegraph.DetectLanguage failed: %v", err)
	}
	if lang != "go" {
		t.Errorf("expected language %q, got %q", "go", lang)
	}
}

func TestLanguageAutoDetectionAmbiguous(t *testing.T) {
	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "tsconfig.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := codegraph.DetectLanguage(projDir)
	if err == nil {
		t.Fatal("expected error when both config files found, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "ambiguous") {
		t.Errorf("error message should mention ambiguity, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "--lang=go") || !strings.Contains(errMsg, "--lang=typescript") {
		t.Errorf("error message should contain both lang hints, got: %s", errMsg)
	}
}

func TestLanguageAutoDetectionNoConfig(t *testing.T) {
	// Create a temp dir without any config files.
	projDir := t.TempDir()

	_, err := codegraph.DetectLanguage(projDir)
	if err == nil {
		t.Fatal("expected error when no config file found, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "could not detect project language") {
		t.Errorf("error message should mention detection failure, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "--lang") {
		t.Errorf("error message should contain remediation hint (--lang), got: %s", errMsg)
	}
}

func TestLanguageOverride(t *testing.T) {
	// Create a temp dir WITHOUT tsconfig.json.
	projDir := t.TempDir()

	// When --lang is specified, codegraph.DetectLanguage should not be called.
	// We verify this by checking that runCodeGraph with lang="typescript"
	// on a dir without tsconfig.json does NOT fail with a language detection error.
	// Instead it should get past detection and succeed or fail for other reasons
	// (like scanning an empty directory).

	// We need ast-grep installed for this test.
	if _, lookErr := findInPath("ast-grep"); lookErr != nil {
		t.Skip("ast-grep not installed, skipping language override test")
	}

	// Create a proper cobra command to pass to runCodeGraph.
	cmd := newCodeGraphCmd()
	cmd.SetOut(&bytes.Buffer{}) // suppress output during test
	cmd.SetArgs([]string{projDir})

	err := runCodeGraph(cmd, projDir, "json", "typescript")
	// The error should NOT be about language detection.
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "could not detect project language") {
			t.Errorf("--lang override should bypass language detection, but got: %s", errMsg)
		}
		// Other errors (e.g. no files found, scanning errors) are acceptable.
	}
}

func findInPath(name string) (string, error) {
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}

// fixtureDir returns the absolute path to a Go test fixture directory.
func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine test file path")
	}
	dir := filepath.Join(filepath.Dir(filename), "..", "..", "testdata", "fixtures", name)
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("failed to resolve fixture path: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("fixture directory does not exist: %s", abs)
	}
	return abs
}

// runGoGraphFixture runs the code graph pipeline on a fixture and returns the parsed graph.
func runGoGraphFixture(t *testing.T, fixtureName string) *graph.Graph {
	t.Helper()
	dir := fixtureDir(t, fixtureName)

	cmd := newCodeGraphCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := runCodeGraph(cmd, dir, "json", "go")
	if err != nil {
		t.Fatalf("runCodeGraph(%s) failed: %v", fixtureName, err)
	}

	var g graph.Graph
	if err := json.Unmarshal(buf.Bytes(), &g); err != nil {
		t.Fatalf("failed to parse JSON output for %s: %v\nraw output: %s", fixtureName, err, buf.String())
	}
	return &g
}

func TestGoFixtureBasicImports(t *testing.T) {
	g := runGoGraphFixture(t, "go-basic-imports")

	// Expected nodes: main.go, util/helper.go, util/format.go, service/service.go
	if len(g.Nodes) != 4 {
		t.Errorf("expected 4 nodes, got %d", len(g.Nodes))
		for _, n := range g.Nodes {
			t.Logf("  node: %s", n.Path)
		}
	}

	// All nodes should have language "go".
	for _, n := range g.Nodes {
		if n.Language != "go" {
			t.Errorf("node %s has language %q, expected \"go\"", n.ID, n.Language)
		}
	}

	// Expected edges:
	// main.go -> util/helper.go (imports util package)
	// main.go -> util/format.go (imports util package)
	// util/helper.go -> service/service.go (imports service package)
	// util/format.go -> service/service.go (imports service package)
	expectedEdges := map[string]bool{
		"main.go->util/helper.go":          false,
		"main.go->util/format.go":          false,
		"util/helper.go->service/service.go": false,
		"util/format.go->service/service.go": false,
	}

	for _, e := range g.Edges {
		key := e.Source + "->" + e.Target
		if _, ok := expectedEdges[key]; ok {
			expectedEdges[key] = true
		} else {
			t.Errorf("unexpected edge: %s -> %s", e.Source, e.Target)
		}
		// All edge import_kind should be "static" in this fixture.
		if e.Attrs["import_kind"] != "static" {
			t.Errorf("edge %s -> %s has import_kind %q, expected \"static\"", e.Source, e.Target, e.Attrs["import_kind"])
		}
	}

	for key, found := range expectedEdges {
		if !found {
			t.Errorf("missing expected edge: %s", key)
		}
	}

	if len(g.Edges) != 4 {
		t.Errorf("expected 4 edges, got %d", len(g.Edges))
	}
}

func TestGoFixtureAllImportStyles(t *testing.T) {
	g := runGoGraphFixture(t, "go-all-import-styles")

	// Expected nodes: main.go, pkg/target.go
	if len(g.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(g.Nodes))
		for _, n := range g.Nodes {
			t.Logf("  node: %s", n.Path)
		}
	}

	for _, n := range g.Nodes {
		if n.Language != "go" {
			t.Errorf("node %s has language %q, expected \"go\"", n.ID, n.Language)
		}
	}

	// main.go imports pkg with 4 different styles: static, blank, dot, aliased.
	// All resolve to pkg/target.go. Due to edge deduplication, there should be
	// exactly 1 edge (source+target is the same for all 4 imports).
	// Actually, looking at the dedup logic in buildGraph, it deduplicates on
	// edgeKey{source, target} — so main.go -> pkg/target.go appears once.
	if len(g.Edges) != 1 {
		t.Errorf("expected 1 edge (deduplicated), got %d", len(g.Edges))
		for _, e := range g.Edges {
			t.Logf("  edge: %s -> %s (kind=%s)", e.Source, e.Target, e.Attrs["import_kind"])
		}
	}

	if len(g.Edges) > 0 {
		e := g.Edges[0]
		if e.Source != "main.go" || e.Target != "pkg/target.go" {
			t.Errorf("expected edge main.go -> pkg/target.go, got %s -> %s", e.Source, e.Target)
		}
		// The first import encountered determines the import_kind for the deduplicated edge.
		// The first import in the file is the static one.
		if e.Attrs["import_kind"] != "static" {
			t.Errorf("expected first-seen import_kind to be \"static\", got %q", e.Attrs["import_kind"])
		}
	}
}

func TestGoFixtureModuleResolution(t *testing.T) {
	g := runGoGraphFixture(t, "go-module-resolution")

	// Expected nodes: cmd/main.go, internal/util/util.go
	if len(g.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(g.Nodes))
		for _, n := range g.Nodes {
			t.Logf("  node: %s", n.Path)
		}
	}

	for _, n := range g.Nodes {
		if n.Language != "go" {
			t.Errorf("node %s has language %q, expected \"go\"", n.ID, n.Language)
		}
	}

	// cmd/main.go imports github.com/example/project/internal/util
	// which should resolve to internal/util/util.go
	if len(g.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(g.Edges))
		for _, e := range g.Edges {
			t.Logf("  edge: %s -> %s", e.Source, e.Target)
		}
	}

	if len(g.Edges) > 0 {
		e := g.Edges[0]
		if e.Source != "cmd/main.go" {
			t.Errorf("expected source cmd/main.go, got %s", e.Source)
		}
		if e.Target != "internal/util/util.go" {
			t.Errorf("expected target internal/util/util.go, got %s", e.Target)
		}
		if e.Attrs["import_kind"] != "static" {
			t.Errorf("expected import_kind \"static\", got %q", e.Attrs["import_kind"])
		}
	}
}

func TestGoFixtureExternalImports(t *testing.T) {
	g := runGoGraphFixture(t, "go-external-imports")

	// Expected: 1 node (main.go), no edges (all imports are external).
	if len(g.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(g.Nodes))
		for _, n := range g.Nodes {
			t.Logf("  node: %s", n.Path)
		}
	}

	if len(g.Nodes) > 0 && g.Nodes[0].Language != "go" {
		t.Errorf("node has language %q, expected \"go\"", g.Nodes[0].Language)
	}

	if len(g.Edges) != 0 {
		t.Errorf("expected 0 edges (all external), got %d", len(g.Edges))
		for _, e := range g.Edges {
			t.Logf("  edge: %s -> %s (raw=%s)", e.Source, e.Target, e.Attrs["raw"])
		}
	}
}

func TestGoFixtureCircular(t *testing.T) {
	g := runGoGraphFixture(t, "go-circular")

	// Expected nodes: alpha/alpha.go, beta/beta.go
	if len(g.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(g.Nodes))
		for _, n := range g.Nodes {
			t.Logf("  node: %s", n.Path)
		}
	}

	for _, n := range g.Nodes {
		if n.Language != "go" {
			t.Errorf("node %s has language %q, expected \"go\"", n.ID, n.Language)
		}
	}

	// Expected edges (circular):
	// alpha/alpha.go -> beta/beta.go
	// beta/beta.go -> alpha/alpha.go
	expectedEdges := map[string]bool{
		"alpha/alpha.go->beta/beta.go":  false,
		"beta/beta.go->alpha/alpha.go":  false,
	}

	for _, e := range g.Edges {
		key := e.Source + "->" + e.Target
		if _, ok := expectedEdges[key]; ok {
			expectedEdges[key] = true
		} else {
			t.Errorf("unexpected edge: %s -> %s", e.Source, e.Target)
		}
		if e.Attrs["import_kind"] != "static" {
			t.Errorf("edge %s -> %s has import_kind %q, expected \"static\"", e.Source, e.Target, e.Attrs["import_kind"])
		}
	}

	for key, found := range expectedEdges {
		if !found {
			t.Errorf("missing expected edge: %s", key)
		}
	}

	if len(g.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(g.Edges))
	}
}
