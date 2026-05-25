//go:build e2e

package e2e

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "regenerate golden files")

// graph mirrors the Graph struct from internal/graph for JSON decoding.
type graph struct {
	Root  string `json:"root"`
	Nodes []node `json:"nodes"`
	Edges []edge `json:"edges"`
}

type node struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Path     string            `json:"path"`
	Language string            `json:"language,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
}

type edge struct {
	Source string            `json:"source"`
	Target string            `json:"target"`
	Kind   string            `json:"kind"`
	Attrs  map[string]string `json:"attrs,omitempty"`
}

// binaryPath builds the autograph binary into a temp dir and returns its path.
func buildBinary(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	bin := filepath.Join(tmpDir, "autograph")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	// Build from the module root (one level up from e2e/).
	moduleRoot := filepath.Join(testdataDir(), "..", "..")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/autograph/")
	cmd.Dir = moduleRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build autograph binary: %v\n%s", err, out)
	}

	return bin
}

// testdataDir returns the absolute path to the e2e/testdata directory.
func testdataDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata")
}

// goldenDir returns the path to the golden file directory.
func goldenDir() string {
	return filepath.Join(testdataDir(), "golden")
}

// sampleProjectDir returns the path to the local sample project fixture.
func sampleProjectDir() string {
	return filepath.Join(testdataDir(), "sample-project")
}

// goSampleProjectDir returns the path to the local Go sample project fixture.
func goSampleProjectDir() string {
	return filepath.Join(testdataDir(), "go-sample-project")
}

// runAutograph runs the autograph binary with the given args and returns stdout.
func runAutograph(t *testing.T, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("autograph %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestSampleProjectJSON(t *testing.T) {
	bin := buildBinary(t)
	dir := sampleProjectDir()

	output := runAutograph(t, bin, "code", "graph", dir)

	var g graph
	if err := json.Unmarshal([]byte(output), &g); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nraw output:\n%s", err, output)
	}

	// Basic structural assertions.
	if len(g.Nodes) == 0 {
		t.Fatal("expected non-empty nodes array")
	}
	if len(g.Edges) == 0 {
		t.Fatal("expected non-empty edges array")
	}

	// Build node ID set.
	nodeIDs := make(map[string]bool)
	for _, n := range g.Nodes {
		nodeIDs[n.ID] = true
	}

	// All edge sources and targets must reference existing nodes.
	for _, e := range g.Edges {
		if !nodeIDs[e.Source] {
			t.Errorf("edge source %q not found in nodes", e.Source)
		}
		if !nodeIDs[e.Target] {
			t.Errorf("edge target %q not found in nodes", e.Target)
		}
	}

	// No duplicate edges.
	type edgeKey struct{ source, target string }
	edgeSeen := make(map[edgeKey]bool)
	for _, e := range g.Edges {
		key := edgeKey{e.Source, e.Target}
		if edgeSeen[key] {
			t.Errorf("duplicate edge: %s -> %s", e.Source, e.Target)
		}
		edgeSeen[key] = true
	}

	// Verify specific expected counts from the sample project.
	if len(g.Nodes) < 10 {
		t.Errorf("expected at least 10 nodes, got %d", len(g.Nodes))
	}
	if len(g.Edges) < 8 {
		t.Errorf("expected at least 8 edges, got %d", len(g.Edges))
	}

	// Verify diverse import kinds are represented.
	importKinds := make(map[string]bool)
	for _, e := range g.Edges {
		if k, ok := e.Attrs["import_kind"]; ok {
			importKinds[k] = true
		}
	}
	for _, expected := range []string{"static", "dynamic", "type", "side-effect"} {
		if !importKinds[expected] {
			t.Errorf("expected import kind %q in edges, not found", expected)
		}
	}

	// Golden file comparison.
	goldenPath := filepath.Join(goldenDir(), "sample-project.json")
	normalized := normalizeGraphJSON(t, g)

	if *update {
		if err := os.MkdirAll(goldenDir(), 0o755); err != nil {
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(normalized), 0o644); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}
	if string(golden) != normalized {
		t.Errorf("output does not match golden file %s\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, normalized, string(golden))
	}
}

func TestSampleProjectDOT(t *testing.T) {
	bin := buildBinary(t)
	dir := sampleProjectDir()

	output := runAutograph(t, bin, "code", "graph", dir, "--format=dot")

	if len(output) == 0 {
		t.Fatal("expected non-empty DOT output")
	}

	// DOT output must start with "digraph" and end with "}".
	trimmed := strings.TrimSpace(output)
	if !strings.HasPrefix(trimmed, "digraph") {
		t.Errorf("DOT output should start with 'digraph', got: %s", trimmed[:min(50, len(trimmed))])
	}
	if !strings.HasSuffix(trimmed, "}") {
		t.Errorf("DOT output should end with '}', got: ...%s", trimmed[max(0, len(trimmed)-50):])
	}

	// Must contain at least one edge arrow.
	if !strings.Contains(output, "->") {
		t.Error("DOT output should contain at least one '->' edge")
	}

	// Golden file comparison (sort edge lines for determinism).
	goldenPath := filepath.Join(goldenDir(), "sample-project.dot")
	normalized := normalizeDOT(output)
	if *update {
		if err := os.MkdirAll(goldenDir(), 0o755); err != nil {
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(normalized), 0o644); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}
	if string(golden) != normalized {
		t.Errorf("DOT output does not match golden file %s\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, normalized, string(golden))
	}
}

func TestSampleProjectMermaid(t *testing.T) {
	bin := buildBinary(t)
	dir := sampleProjectDir()

	output := runAutograph(t, bin, "code", "graph", dir, "--format=mermaid")

	if len(output) == 0 {
		t.Fatal("expected non-empty Mermaid output")
	}

	// Mermaid output must start with "graph LR".
	trimmed := strings.TrimSpace(output)
	if !strings.HasPrefix(trimmed, "graph LR") {
		t.Errorf("Mermaid output should start with 'graph LR', got: %s", trimmed[:min(50, len(trimmed))])
	}

	// Must contain at least one edge arrow.
	if !strings.Contains(output, "-->") {
		t.Error("Mermaid output should contain at least one '-->' edge")
	}

	// Golden file comparison (sort edge lines for determinism).
	goldenPath := filepath.Join(goldenDir(), "sample-project.mermaid")
	normalized := normalizeMermaid(output)
	if *update {
		if err := os.MkdirAll(goldenDir(), 0o755); err != nil {
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(normalized), 0o644); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}
	if string(golden) != normalized {
		t.Errorf("Mermaid output does not match golden file %s\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, normalized, string(golden))
	}
}

// normalizeGraphJSON produces a deterministic JSON representation of the graph
// with the root field replaced by a placeholder (since it contains absolute paths).
func normalizeGraphJSON(t *testing.T, g graph) string {
	t.Helper()

	// Replace the absolute root path with a placeholder.
	g.Root = "<project-root>"

	// Sort nodes by ID for deterministic output.
	sort.Slice(g.Nodes, func(i, j int) bool {
		return g.Nodes[i].ID < g.Nodes[j].ID
	})

	// Sort edges by (source, target) for deterministic output.
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].Source != g.Edges[j].Source {
			return g.Edges[i].Source < g.Edges[j].Source
		}
		return g.Edges[i].Target < g.Edges[j].Target
	})

	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatalf("marshaling normalized graph: %v", err)
	}

	return string(data) + "\n"
}

// normalizeDOT sorts the edge lines in DOT output for deterministic comparison.
// Keeps the header ("digraph imports {", "    rankdir=LR;") and footer ("}") in place.
func normalizeDOT(output string) string {
	lines := strings.Split(output, "\n")
	var header []string
	var edges []string
	var footer []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, "->") {
			edges = append(edges, line)
		} else if trimmed == "}" {
			footer = append(footer, line)
		} else if trimmed != "" {
			header = append(header, line)
		}
	}

	sort.Strings(edges)
	var result []string
	result = append(result, header...)
	result = append(result, edges...)
	result = append(result, footer...)
	return strings.Join(result, "\n") + "\n"
}

// normalizeMermaid sorts the edge lines in Mermaid output for deterministic comparison.
// Keeps the header ("graph LR") in place.
func normalizeMermaid(output string) string {
	lines := strings.Split(output, "\n")
	var header []string
	var edges []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, "-->") {
			edges = append(edges, line)
		} else if trimmed != "" {
			header = append(header, line)
		}
	}

	sort.Strings(edges)
	var result []string
	result = append(result, header...)
	result = append(result, edges...)
	return strings.Join(result, "\n") + "\n"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// fileExistsE2E checks if a file exists (used by e2e tests).
func fileExistsE2E(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// cloneRepo clones a git repository to a temp directory and checks out a specific commit.
func cloneRepo(t *testing.T, url, commit string) string {
	t.Helper()
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")

	cmd := exec.Command("git", "clone", url, repoDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git clone failed: %v\n%s", err, out)
	}

	cmd = exec.Command("git", "-C", repoDir, "checkout", commit)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git checkout %s failed: %v\n%s", commit, err, out)
	}

	return repoDir
}

// TestGoSampleProjectJSON runs the Go sample project through autograph and
// verifies JSON output structure and golden file comparison.
func TestGoSampleProjectJSON(t *testing.T) {
	bin := buildBinary(t)
	dir := goSampleProjectDir()

	output := runAutograph(t, bin, "code", "graph", dir)

	var g graph
	if err := json.Unmarshal([]byte(output), &g); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nraw output:\n%s", err, output)
	}

	// All nodes must have language "go".
	for _, n := range g.Nodes {
		if n.Language != "go" {
			t.Errorf("node %q has language %q, want \"go\"", n.ID, n.Language)
		}
	}

	// Verify at least 10 nodes (the project has 12 files).
	if len(g.Nodes) < 10 {
		t.Errorf("expected at least 10 nodes, got %d", len(g.Nodes))
	}

	// Verify we have edges.
	if len(g.Edges) == 0 {
		t.Fatal("expected non-empty edges array")
	}

	// Verify edge referential integrity.
	nodeIDs := make(map[string]bool)
	for _, n := range g.Nodes {
		nodeIDs[n.ID] = true
	}
	for _, e := range g.Edges {
		if !nodeIDs[e.Source] {
			t.Errorf("edge source %q not found in nodes", e.Source)
		}
		if !nodeIDs[e.Target] {
			t.Errorf("edge target %q not found in nodes", e.Target)
		}
	}

	// Verify diverse import kinds.
	importKinds := make(map[string]bool)
	for _, e := range g.Edges {
		if k, ok := e.Attrs["import_kind"]; ok {
			importKinds[k] = true
		}
	}
	for _, expected := range []string{"static", "blank", "dot", "aliased"} {
		if !importKinds[expected] {
			t.Errorf("expected import kind %q in edges, not found", expected)
		}
	}

	// Golden file comparison.
	goldenPath := filepath.Join(goldenDir(), "go-sample-project.json")
	normalized := normalizeGraphJSON(t, g)

	if *update {
		if err := os.MkdirAll(goldenDir(), 0o755); err != nil {
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(normalized), 0o644); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}
	if string(golden) != normalized {
		t.Errorf("output does not match golden file %s\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, normalized, string(golden))
	}
}

// TestGoSampleProjectFormats runs the Go sample project through all three output
// formats and verifies structural properties.
func TestGoSampleProjectFormats(t *testing.T) {
	bin := buildBinary(t)
	dir := goSampleProjectDir()

	t.Run("json", func(t *testing.T) {
		output := runAutograph(t, bin, "code", "graph", dir, "--format=json")
		trimmed := strings.TrimSpace(output)
		if !strings.HasPrefix(trimmed, "{") {
			t.Errorf("JSON output should start with '{', got: %s", trimmed[:min(80, len(trimmed))])
		}

		var g graph
		if err := json.Unmarshal([]byte(output), &g); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if len(g.Nodes) < 10 {
			t.Errorf("expected at least 10 nodes, got %d", len(g.Nodes))
		}
	})

	t.Run("dot", func(t *testing.T) {
		output := runAutograph(t, bin, "code", "graph", dir, "--format=dot")
		trimmed := strings.TrimSpace(output)
		if !strings.HasPrefix(trimmed, "digraph") {
			t.Errorf("DOT output should start with 'digraph', got: %s", trimmed[:min(50, len(trimmed))])
		}
		if !strings.HasSuffix(trimmed, "}") {
			t.Errorf("DOT output should end with '}', got: ...%s", trimmed[max(0, len(trimmed)-50):])
		}
		if !strings.Contains(output, "->") {
			t.Error("DOT output should contain at least one '->' edge")
		}
	})

	t.Run("mermaid", func(t *testing.T) {
		output := runAutograph(t, bin, "code", "graph", dir, "--format=mermaid")
		trimmed := strings.TrimSpace(output)
		if !strings.HasPrefix(trimmed, "graph LR") {
			t.Errorf("Mermaid output should start with 'graph LR', got: %s", trimmed[:min(50, len(trimmed))])
		}
		if !strings.Contains(output, "-->") {
			t.Error("Mermaid output should contain at least one '-->' edge")
		}
	})
}

// TestPublicGoRepo clones a pinned Go repo and verifies autograph produces a valid graph.
func TestPublicGoRepo(t *testing.T) {
	// Load repo config from repos.json.
	reposPath := filepath.Join(testdataDir(), "..", "repos.json")
	data, err := os.ReadFile(reposPath)
	if err != nil {
		t.Fatalf("reading repos.json: %v", err)
	}

	type repoEntry struct {
		Name   string `json:"name"`
		URL    string `json:"url"`
		Commit string `json:"commit"`
		Subdir string `json:"subdir"`
	}
	var repos []repoEntry
	if err := json.Unmarshal(data, &repos); err != nil {
		t.Fatalf("parsing repos.json: %v", err)
	}

	// Find the cobra entry.
	var cobraRepo *repoEntry
	for i := range repos {
		if repos[i].Name == "cobra" {
			cobraRepo = &repos[i]
			break
		}
	}
	if cobraRepo == nil {
		t.Skip("cobra entry not found in repos.json")
	}

	bin := buildBinary(t)
	repoDir := cloneRepo(t, cobraRepo.URL, cobraRepo.Commit)

	targetDir := repoDir
	if cobraRepo.Subdir != "" {
		targetDir = filepath.Join(repoDir, cobraRepo.Subdir)
	}

	// Time only the autograph run, not the clone.
	start := time.Now()
	output := runAutograph(t, bin, "code", "graph", targetDir, "--lang=go")
	elapsed := time.Since(start)

	// Performance assertion: should complete in under 3 seconds (AC-9).
	if elapsed > 3*time.Second {
		t.Errorf("autograph took %v, want < 3s", elapsed)
	}

	var g graph
	if err := json.Unmarshal([]byte(output), &g); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nfirst 500 chars:\n%s", err, output[:min(500, len(output))])
	}

	// Structural assertions.
	if len(g.Nodes) == 0 {
		t.Fatal("expected non-empty nodes array")
	}
	t.Logf("cobra: %d nodes, %d edges in %v", len(g.Nodes), len(g.Edges), elapsed)

	// All nodes should have language "go".
	for _, n := range g.Nodes {
		if n.Language != "go" {
			t.Errorf("node %q has language %q, want \"go\"", n.ID, n.Language)
			break
		}
	}

	// Edges referential integrity.
	nodeIDs := make(map[string]bool)
	for _, n := range g.Nodes {
		nodeIDs[n.ID] = true
	}
	for _, e := range g.Edges {
		if !nodeIDs[e.Source] {
			t.Errorf("edge source %q not found in nodes", e.Source)
		}
		if !nodeIDs[e.Target] {
			t.Errorf("edge target %q not found in nodes", e.Target)
		}
	}
}

// TestEdgeReferentialIntegrity verifies structural invariants on the graph
// across all fixture directories.
func TestEdgeReferentialIntegrity(t *testing.T) {
	bin := buildBinary(t)

	// Run against every fixture directory that has a tsconfig.json.
	fixturesDir := filepath.Join(testdataDir(), "..", "..", "testdata", "fixtures")
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatalf("reading fixtures dir: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		fixtureDir := filepath.Join(fixturesDir, entry.Name())
		hasTSConfig := fileExistsE2E(filepath.Join(fixtureDir, "tsconfig.json"))
		hasGoMod := fileExistsE2E(filepath.Join(fixtureDir, "go.mod"))
		if !hasTSConfig && !hasGoMod {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			output := runAutograph(t, bin, "code", "graph", fixtureDir)

			var g graph
			if err := json.Unmarshal([]byte(output), &g); err != nil {
				t.Fatalf("failed to parse JSON: %v", err)
			}

			nodeIDs := make(map[string]bool)
			for _, n := range g.Nodes {
				nodeIDs[n.ID] = true
			}

			for _, e := range g.Edges {
				if !nodeIDs[e.Source] {
					t.Errorf("edge source %q not in nodes", e.Source)
				}
				if !nodeIDs[e.Target] {
					t.Errorf("edge target %q not in nodes", e.Target)
				}
			}

			// No duplicate edges.
			type edgeKey struct{ source, target string }
			edgeSeen := make(map[edgeKey]bool)
			for _, e := range g.Edges {
				key := edgeKey{e.Source, e.Target}
				if edgeSeen[key] {
					t.Errorf("duplicate edge: %s -> %s", e.Source, e.Target)
				}
				edgeSeen[key] = true
			}

			t.Logf("%s: %d nodes, %d edges", entry.Name(), len(g.Nodes), len(g.Edges))
		})
	}
}

// TestVersion verifies the binary runs and prints a version.
func TestVersion(t *testing.T) {
	bin := buildBinary(t)
	output := runAutograph(t, bin, "--version")
	if !strings.Contains(output, "autograph version") {
		t.Errorf("expected version output, got: %s", output)
	}
}

// TestInvalidDirectory verifies the binary returns an error for a non-existent dir.
func TestInvalidDirectory(t *testing.T) {
	bin := buildBinary(t)
	cmd := exec.Command(bin, "code", "graph", "/nonexistent/path")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
	if !strings.Contains(string(out), "not a directory") {
		t.Errorf("expected 'not a directory' in error, got: %s", out)
	}
}

// TestNoTSConfig verifies the binary returns a helpful error for a directory
// without tsconfig.json or go.mod.
func TestNoTSConfig(t *testing.T) {
	bin := buildBinary(t)
	tmpDir := t.TempDir()
	cmd := exec.Command(bin, "code", "graph", tmpDir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected error for directory without tsconfig.json or go.mod")
	}
	output := string(out)
	if !strings.Contains(output, "go.mod") || !strings.Contains(output, "tsconfig.json") {
		t.Errorf("expected both go.mod and tsconfig.json in error message, got: %s", output)
	}
}

// TestMultipleFormats verifies all three output formats produce non-empty output
// from the same input.
func TestMultipleFormats(t *testing.T) {
	bin := buildBinary(t)
	dir := sampleProjectDir()

	formats := []struct {
		name   string
		flag   string
		prefix string
	}{
		{"json", "json", `{`},
		{"dot", "dot", "digraph"},
		{"mermaid", "mermaid", "graph LR"},
	}

	for _, f := range formats {
		t.Run(f.name, func(t *testing.T) {
			output := runAutograph(t, bin, "code", "graph", dir, fmt.Sprintf("--format=%s", f.flag))
			trimmed := strings.TrimSpace(output)
			if len(trimmed) == 0 {
				t.Fatalf("empty output for format %s", f.name)
			}
			if !strings.HasPrefix(trimmed, f.prefix) {
				t.Errorf("expected output to start with %q, got: %s", f.prefix, trimmed[:min(80, len(trimmed))])
			}
		})
	}
}
