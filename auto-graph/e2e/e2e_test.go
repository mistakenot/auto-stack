//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("autograph %v failed: %v\nstdout: %s\nstderr: %s", args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
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
	for _, expected := range []string{"static", "dynamic", "type_only", "side_effect"} {
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

// runAutographWithStderr runs the autograph binary with the given args and
// returns stdout and stderr separately. Does not fail on non-zero exit.
func runAutographWithStderr(t *testing.T, bin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// TestSingleQuoteJSONCProject verifies that single-quoted imports/reexports
// and JSONC tsconfig (trailing commas + comments) produce the correct graph.
func TestSingleQuoteJSONCProject(t *testing.T) {
	bin := buildBinary(t)
	dir := filepath.Join(testdataDir(), "single-quote-jsonc-project")

	output := runAutograph(t, bin, "code", "graph", dir)

	var g graph
	if err := json.Unmarshal([]byte(output), &g); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nraw output:\n%s", err, output)
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

	// Expect exactly 4 edges:
	// 1. src/routes/dashboard.tsx -> src/utils/format.ts (static, alias)
	// 2. src/routes/dashboard.tsx -> src/components/Header.tsx (static, relative)
	// 3. src/feature/index.ts -> src/feature/Widget.tsx (reexport — merged named + type)
	// 4. src/feature/index.ts -> src/feature/widget-utils.ts (reexport)
	if len(g.Edges) != 4 {
		t.Errorf("expected 4 edges, got %d", len(g.Edges))
		for _, e := range g.Edges {
			t.Logf("  edge: %s -> %s (kind=%s, import_kind=%s)", e.Source, e.Target, e.Kind, e.Attrs["import_kind"])
		}
	}

	// Verify diverse import kinds are represented.
	importKinds := make(map[string]bool)
	for _, e := range g.Edges {
		if k, ok := e.Attrs["import_kind"]; ok {
			importKinds[k] = true
		}
	}
	for _, expected := range []string{"static", "reexport"} {
		if !importKinds[expected] {
			t.Errorf("expected import kind %q in edges, not found", expected)
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

	// Golden file comparison.
	goldenPath := filepath.Join(goldenDir(), "single-quote-jsonc-project.json")
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

// TestMalformedTSConfigStderr verifies that a malformed tsconfig.json
// produces a warning on stderr while still outputting a valid graph on stdout.
func TestMalformedTSConfigStderr(t *testing.T) {
	bin := buildBinary(t)

	// Create a temp dir with a malformed tsconfig and one .ts file.
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte("{{{"), 0o644); err != nil {
		t.Fatalf("writing tsconfig.json: %v", err)
	}
	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("creating src dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "app.ts"), []byte("export function main() {}\n"), 0o644); err != nil {
		t.Fatalf("writing app.ts: %v", err)
	}

	stdout, stderr, _ := runAutographWithStderr(t, bin, "code", "graph", tmpDir, "--lang=typescript")

	// Stderr should contain a warning about the tsconfig.
	stderrLower := strings.ToLower(stderr)
	if !strings.Contains(stderrLower, "warning") {
		t.Errorf("expected stderr to contain 'warning', got: %s", stderr)
	}
	if !strings.Contains(stderrLower, "tsconfig") {
		t.Errorf("expected stderr to contain 'tsconfig', got: %s", stderr)
	}

	// Stdout should still contain valid JSON graph output.
	var g graph
	if err := json.Unmarshal([]byte(stdout), &g); err != nil {
		t.Fatalf("expected valid JSON graph on stdout despite malformed tsconfig: %v\nstdout: %s", err, stdout)
	}

	// Graph should still have nodes (the .ts file was discovered).
	if len(g.Nodes) == 0 {
		t.Error("expected at least one node in graph output")
	}
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
	for _, expected := range []string{"static", "side_effect", "dot", "aliased"} {
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

// doclinksProjectDir returns the path to the doclinks project fixture.
func doclinksProjectDir() string {
	return filepath.Join(testdataDir(), "doclinks-project")
}

// prepareDoclinksGitRepo copies the doclinks fixture to a temp dir and
// initializes a git repo so that git ls-files works for doclink scanning.
func prepareDoclinksGitRepo(t *testing.T) string {
	t.Helper()
	src := doclinksProjectDir()
	dst := filepath.Join(t.TempDir(), "doclinks-project")

	// Copy the fixture tree.
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copying doclinks fixture: %v", err)
	}

	// Initialize git repo so git ls-files works for doclink scanning.
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "test"},
		{"add", "-A"},
		{"commit", "-m", "init", "--allow-empty"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dst}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	return dst
}

// TestDocLinksProjectJSON verifies that doc nodes and doc_link edges appear
// in the JSON output when autodoc tags reference doc files with matching IDs.
func TestDocLinksProjectJSON(t *testing.T) {
	bin := buildBinary(t)
	dir := prepareDoclinksGitRepo(t)

	output := runAutograph(t, bin, "code", "graph", dir)

	var g graph
	if err := json.Unmarshal([]byte(output), &g); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nraw output:\n%s", err, output)
	}

	// Build node maps.
	nodeIDs := make(map[string]bool)
	nodesByKind := make(map[string][]node)
	for _, n := range g.Nodes {
		nodeIDs[n.ID] = true
		nodesByKind[n.Kind] = append(nodesByKind[n.Kind], n)
	}

	// Assert doc nodes exist.
	docNodes := nodesByKind["doc"]
	if len(docNodes) != 2 {
		t.Errorf("expected 2 doc nodes, got %d", len(docNodes))
		for _, n := range g.Nodes {
			t.Logf("  node: id=%s kind=%s path=%s", n.ID, n.Kind, n.Path)
		}
	}

	// Assert file nodes exist (app.ts, helper.ts, utils.ts).
	fileNodes := nodesByKind["file"]
	if len(fileNodes) < 3 {
		t.Errorf("expected at least 3 file nodes, got %d", len(fileNodes))
	}

	// Assert doc_link edges exist.
	var docLinkEdges []edge
	var importEdges []edge
	for _, e := range g.Edges {
		switch e.Kind {
		case "doc_link":
			docLinkEdges = append(docLinkEdges, e)
		case "import":
			importEdges = append(importEdges, e)
		}
	}

	if len(docLinkEdges) != 2 {
		t.Errorf("expected 2 doc_link edges, got %d", len(docLinkEdges))
		for _, e := range g.Edges {
			t.Logf("  edge: %s -> %s kind=%s", e.Source, e.Target, e.Kind)
		}
	}

	// Assert import edges still present.
	if len(importEdges) < 2 {
		t.Errorf("expected at least 2 import edges, got %d", len(importEdges))
	}

	// Referential integrity: all edge sources and targets must be in nodes.
	for _, e := range g.Edges {
		if !nodeIDs[e.Source] {
			t.Errorf("edge source %q not found in nodes", e.Source)
		}
		if !nodeIDs[e.Target] {
			t.Errorf("edge target %q not found in nodes", e.Target)
		}
	}

	// Verify specific doc node attributes.
	for _, n := range docNodes {
		if n.Attrs == nil || n.Attrs["title"] == "" {
			t.Errorf("doc node %q missing title attr", n.ID)
		}
	}
}

// TestDocLinksProjectNoDocs verifies that --no-docs excludes doc nodes and doc_link edges.
func TestDocLinksProjectNoDocs(t *testing.T) {
	bin := buildBinary(t)
	dir := prepareDoclinksGitRepo(t)

	output := runAutograph(t, bin, "code", "graph", dir, "--no-docs")

	var g graph
	if err := json.Unmarshal([]byte(output), &g); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nraw output:\n%s", err, output)
	}

	// All nodes must be "file" kind.
	for _, n := range g.Nodes {
		if n.Kind != "file" {
			t.Errorf("with --no-docs, node %q has kind %q, want \"file\"", n.ID, n.Kind)
		}
	}

	// All edges must be "import" kind.
	for _, e := range g.Edges {
		if e.Kind != "import" {
			t.Errorf("with --no-docs, edge %s->%s has kind %q, want \"import\"", e.Source, e.Target, e.Kind)
		}
	}
}

// TestDocLinksProjectDOT verifies doc nodes use [shape=note] and doc edges use [style=dashed] in DOT output.
func TestDocLinksProjectDOT(t *testing.T) {
	bin := buildBinary(t)
	dir := prepareDoclinksGitRepo(t)

	output := runAutograph(t, bin, "code", "graph", dir, "--format=dot")

	if len(output) == 0 {
		t.Fatal("expected non-empty DOT output")
	}

	// DOT output must contain [shape=note] for doc nodes.
	if !strings.Contains(output, "[shape=note]") {
		t.Error("DOT output should contain '[shape=note]' for doc nodes")
	}

	// DOT output must contain [style=dashed] for doc_link edges.
	if !strings.Contains(output, "[style=dashed]") {
		t.Error("DOT output should contain '[style=dashed]' for doc_link edges")
	}

	// Verify structural DOT properties.
	trimmed := strings.TrimSpace(output)
	if !strings.HasPrefix(trimmed, "digraph") {
		t.Errorf("DOT output should start with 'digraph', got: %s", trimmed[:min(50, len(trimmed))])
	}
	if !strings.HasSuffix(trimmed, "}") {
		t.Errorf("DOT output should end with '}'")
	}
}

// TestDocLinksProjectMermaid verifies doc nodes use hexagon syntax and doc edges use dashed arrows in Mermaid output.
func TestDocLinksProjectMermaid(t *testing.T) {
	bin := buildBinary(t)
	dir := prepareDoclinksGitRepo(t)

	output := runAutograph(t, bin, "code", "graph", dir, "--format=mermaid")

	if len(output) == 0 {
		t.Fatal("expected non-empty Mermaid output")
	}

	trimmed := strings.TrimSpace(output)
	if !strings.HasPrefix(trimmed, "graph LR") {
		t.Errorf("Mermaid output should start with 'graph LR', got: %s", trimmed[:min(50, len(trimmed))])
	}

	// Mermaid doc nodes use hexagon syntax: {{path}}
	if !strings.Contains(output, "{{") {
		t.Error("Mermaid output should contain '{{' for doc node hexagon syntax")
	}

	// Mermaid doc edges use dashed arrows: -.->
	if !strings.Contains(output, "-.->") {
		t.Error("Mermaid output should contain '-.->' for doc_link dashed edges")
	}

	// Regular import edges should also be present.
	if !strings.Contains(output, "-->") {
		t.Error("Mermaid output should contain '-->' for regular import edges")
	}
}

// TestDocLinksContextPack verifies that doc files appear in context pack output
// when autodoc tags link seed files to docs.
func TestDocLinksContextPack(t *testing.T) {
	bin := buildBinary(t)
	dir := prepareDoclinksGitRepo(t)

	output := runAutograph(t, bin, "code", "context", dir,
		"--file", "src/app.ts",
		"--token-limit", "50000",
		"--format", "json")

	// Parse as generic JSON to check structure.
	var pack map[string]interface{}
	if err := json.Unmarshal([]byte(output), &pack); err != nil {
		t.Fatalf("failed to parse context pack JSON: %v\nraw output:\n%s", err, output)
	}

	// Check that files array contains a doc entry.
	filesRaw, ok := pack["files"].([]interface{})
	if !ok {
		t.Fatalf("expected 'files' array in context pack output")
	}

	var docFiles []string
	var allRoles []string
	for _, fRaw := range filesRaw {
		f, ok := fRaw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := f["role"].(string)
		path, _ := f["path"].(string)
		allRoles = append(allRoles, role)
		if role == "doc" {
			docFiles = append(docFiles, path)
		}
	}

	if len(docFiles) == 0 {
		t.Errorf("expected at least one doc file in context pack, got roles: %v", allRoles)
	}

	// The doc linked from the seed (app.ts -> guide.md) should be present.
	foundGuide := false
	for _, p := range docFiles {
		if strings.Contains(p, "guide.md") {
			foundGuide = true
		}
	}
	if !foundGuide {
		t.Errorf("expected docs/guide.md in context pack doc files, got: %v", docFiles)
	}
}

// TestDocLinksContextPackNoDocs verifies that --no-docs excludes doc files from context pack.
func TestDocLinksContextPackNoDocs(t *testing.T) {
	bin := buildBinary(t)
	dir := prepareDoclinksGitRepo(t)

	output := runAutograph(t, bin, "code", "context", dir,
		"--file", "src/app.ts",
		"--token-limit", "50000",
		"--format", "json",
		"--no-docs")

	var pack map[string]interface{}
	if err := json.Unmarshal([]byte(output), &pack); err != nil {
		t.Fatalf("failed to parse context pack JSON: %v\nraw output:\n%s", err, output)
	}

	filesRaw, ok := pack["files"].([]interface{})
	if !ok {
		t.Fatalf("expected 'files' array in context pack output")
	}

	for _, fRaw := range filesRaw {
		f, ok := fRaw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := f["role"].(string)
		if role == "doc" {
			path, _ := f["path"].(string)
			t.Errorf("with --no-docs, found doc file in context pack: %s", path)
		}
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
