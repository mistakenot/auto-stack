package contextpack

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mistakenot/auto-graph/internal/graph"
)

// setupBuilderFixture creates a temp directory with files and a synthetic graph.
func setupBuilderFixture(t *testing.T, files map[string]string, edges []graph.Edge) (string, *graph.Graph) {
	t.Helper()
	dir := t.TempDir()

	var nodes []graph.Node
	for path, content := range files {
		abs := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, graph.Node{
			ID:       path,
			Kind:     graph.NodeFile,
			Path:     path,
			Language: "typescript",
		})
	}

	// Sort nodes for determinism.
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Path < nodes[j].Path
	})

	g := &graph.Graph{
		Root:  dir,
		Nodes: nodes,
		Edges: edges,
	}

	return dir, g
}

func TestBuild_SeedsOnly(t *testing.T) {
	files := map[string]string{
		"src/App.tsx": "export default function App() { return <div />; }",
	}
	dir, g := setupBuilderFixture(t, files, nil)

	pack, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/App.tsx"},
		TokenLimit:  10000,
		Graph:       g,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pack.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(pack.Files))
	}
	if pack.Files[0].Path != "src/App.tsx" {
		t.Errorf("expected src/App.tsx, got %s", pack.Files[0].Path)
	}
	if pack.Files[0].Role != "seed" {
		t.Errorf("expected role seed, got %s", pack.Files[0].Role)
	}
	if pack.SeedFiles[0] != "src/App.tsx" {
		t.Errorf("expected seed file src/App.tsx, got %s", pack.SeedFiles[0])
	}
}

func TestBuild_DirectDependencies(t *testing.T) {
	files := map[string]string{
		"src/App.tsx":          "import { useAuth } from './hooks/useAuth';",
		"src/hooks/useAuth.ts": "export function useAuth() {}",
		"src/utils/helper.ts":  "export function helper() {}",
	}
	edges := []graph.Edge{
		{
			Source: "src/App.tsx",
			Target: "src/hooks/useAuth.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "static", "import_kinds": "static"},
		},
		{
			Source: "src/App.tsx",
			Target: "src/utils/helper.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "static", "import_kinds": "static"},
		},
	}
	dir, g := setupBuilderFixture(t, files, edges)

	pack, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/App.tsx"},
		TokenLimit:  10000,
		Graph:       g,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Seed + 2 runtime dependencies = 3 files.
	if len(pack.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(pack.Files))
	}

	// First file should be the seed.
	if pack.Files[0].Role != "seed" {
		t.Errorf("expected first file to be seed, got role %s", pack.Files[0].Role)
	}

	// Other files should be dependencies.
	for _, f := range pack.Files[1:] {
		if f.Role != "dependency" {
			t.Errorf("expected role dependency for %s, got %s", f.Path, f.Role)
		}
	}
}

func TestBuild_DirectDependents(t *testing.T) {
	files := map[string]string{
		"src/hooks/useAuth.ts":      "export function useAuth() {}",
		"src/App.tsx":               "import { useAuth } from './hooks/useAuth';",
		"src/components/Profile.tsx": "import { useAuth } from '../hooks/useAuth';",
	}
	edges := []graph.Edge{
		{
			Source: "src/App.tsx",
			Target: "src/hooks/useAuth.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "static", "import_kinds": "static"},
		},
		{
			Source: "src/components/Profile.tsx",
			Target: "src/hooks/useAuth.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "static", "import_kinds": "static"},
		},
	}
	dir, g := setupBuilderFixture(t, files, edges)

	pack, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/hooks/useAuth.ts"},
		TokenLimit:  10000,
		Graph:       g,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Seed + 2 runtime dependents = 3 files.
	if len(pack.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(pack.Files))
	}

	// Check that dependents are included.
	var dependents []string
	for _, f := range pack.Files {
		if f.Role == "dependent" {
			dependents = append(dependents, f.Path)
		}
	}
	sort.Strings(dependents)
	if len(dependents) != 2 {
		t.Fatalf("expected 2 dependents, got %d: %v", len(dependents), dependents)
	}
}

func TestBuild_TypeOnlyOrdering(t *testing.T) {
	files := map[string]string{
		"src/App.tsx":        "import type { User } from './types'; import { api } from './api';",
		"src/types.ts":       "export type User = { name: string };",
		"src/api.ts":         "export function api() {}",
		"src/typeConsumer.ts": "import type { User } from './types';",
	}
	edges := []graph.Edge{
		{
			Source: "src/App.tsx",
			Target: "src/types.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "type_only", "import_kinds": "type_only"},
		},
		{
			Source: "src/App.tsx",
			Target: "src/api.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "static", "import_kinds": "static"},
		},
		{
			Source: "src/typeConsumer.ts",
			Target: "src/types.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "type_only", "import_kinds": "type_only"},
		},
	}
	dir, g := setupBuilderFixture(t, files, edges)

	pack, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/App.tsx"},
		TokenLimit:  10000,
		Graph:       g,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Runtime dep (api.ts) should appear before type-only dep (types.ts).
	apiIdx := -1
	typesIdx := -1
	for i, f := range pack.Files {
		if f.Path == "src/api.ts" {
			apiIdx = i
		}
		if f.Path == "src/types.ts" {
			typesIdx = i
		}
	}
	if apiIdx < 0 || typesIdx < 0 {
		t.Fatal("expected both api.ts and types.ts to be included")
	}
	if apiIdx > typesIdx {
		t.Errorf("expected runtime dep api.ts (idx %d) before type-only dep types.ts (idx %d)", apiIdx, typesIdx)
	}
}

func TestBuild_MergedImportKinds(t *testing.T) {
	files := map[string]string{
		"src/App.tsx":  "import { api } from './api'; import type { ApiResult } from './api';",
		"src/api.ts":   "export function api() {} export type ApiResult = {};",
	}
	edges := []graph.Edge{
		{
			Source: "src/App.tsx",
			Target: "src/api.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "static", "import_kinds": "static,type_only"},
		},
	}
	dir, g := setupBuilderFixture(t, files, edges)

	pack, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/App.tsx"},
		TokenLimit:  10000,
		Graph:       g,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// api.ts should be included as a dependency (runtime takes precedence).
	found := false
	for _, f := range pack.Files {
		if f.Path == "src/api.ts" {
			found = true
			if f.Role != "dependency" {
				t.Errorf("expected role dependency, got %s", f.Role)
			}
		}
	}
	if !found {
		t.Error("expected api.ts to be included")
	}

	// Relationship should show merged import kinds.
	for _, r := range pack.Relationships {
		if r.Source == "src/App.tsx" && r.Target == "src/api.ts" {
			if len(r.ImportKinds) != 2 {
				t.Errorf("expected 2 import kinds, got %d: %v", len(r.ImportKinds), r.ImportKinds)
			}
		}
	}
}

func TestRiskFlags_SideEffect(t *testing.T) {
	files := map[string]string{
		"src/App.tsx":    "import './polyfills';",
		"src/polyfills.ts": "// side effect",
	}
	edges := []graph.Edge{
		{
			Source: "src/App.tsx",
			Target: "src/polyfills.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "side_effect", "import_kinds": "side_effect"},
		},
	}
	_, g := setupBuilderFixture(t, files, edges)

	fwd, rev := buildAdjacencyMaps(g)
	riskFlags := computeAllRiskFlags(g, fwd, rev)

	// Both source and target should have side_effect_import flag
	// because the relationship "touches" both files.
	appFlags := riskFlags["src/App.tsx"]
	if !containsFlag(appFlags, "side_effect_import") {
		t.Errorf("expected side_effect_import flag on src/App.tsx, got %v", appFlags)
	}
	polyfillFlags := riskFlags["src/polyfills.ts"]
	if !containsFlag(polyfillFlags, "side_effect_import") {
		t.Errorf("expected side_effect_import flag on src/polyfills.ts, got %v", polyfillFlags)
	}
}

func TestRiskFlags_DynamicImport(t *testing.T) {
	files := map[string]string{
		"src/App.tsx":   "const mod = await import('./lazy');",
		"src/lazy.ts":   "export default function lazy() {}",
	}
	edges := []graph.Edge{
		{
			Source: "src/App.tsx",
			Target: "src/lazy.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "dynamic", "import_kinds": "dynamic"},
		},
	}
	_, g := setupBuilderFixture(t, files, edges)

	fwd, rev := buildAdjacencyMaps(g)
	riskFlags := computeAllRiskFlags(g, fwd, rev)

	appFlags := riskFlags["src/App.tsx"]
	if !containsFlag(appFlags, "dynamic_import") {
		t.Errorf("expected dynamic_import flag on src/App.tsx, got %v", appFlags)
	}
}

func TestRiskFlags_Reexport(t *testing.T) {
	files := map[string]string{
		"src/index.ts":  "export { foo } from './foo';",
		"src/foo.ts":    "export function foo() {}",
	}
	edges := []graph.Edge{
		{
			Source: "src/index.ts",
			Target: "src/foo.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "reexport", "import_kinds": "reexport"},
		},
	}
	_, g := setupBuilderFixture(t, files, edges)

	fwd, rev := buildAdjacencyMaps(g)
	riskFlags := computeAllRiskFlags(g, fwd, rev)

	indexFlags := riskFlags["src/index.ts"]
	if !containsFlag(indexFlags, "reexport") {
		t.Errorf("expected reexport flag on src/index.ts, got %v", indexFlags)
	}
	if !containsFlag(indexFlags, "entrypoint_like") {
		t.Errorf("expected entrypoint_like flag on src/index.ts, got %v", indexFlags)
	}
}

func TestRiskFlags_Cycle(t *testing.T) {
	files := map[string]string{
		"src/a.ts": "import { b } from './b';",
		"src/b.ts": "import { a } from './a';",
		"src/c.ts": "// no cycle",
	}
	edges := []graph.Edge{
		{
			Source: "src/a.ts",
			Target: "src/b.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "static", "import_kinds": "static"},
		},
		{
			Source: "src/b.ts",
			Target: "src/a.ts",
			Kind:   graph.EdgeImport,
			Attrs:  map[string]string{"import_kind": "static", "import_kinds": "static"},
		},
	}
	_, g := setupBuilderFixture(t, files, edges)

	fwd, rev := buildAdjacencyMaps(g)
	riskFlags := computeAllRiskFlags(g, fwd, rev)

	aFlags := riskFlags["src/a.ts"]
	if !containsFlag(aFlags, "cycle") {
		t.Errorf("expected cycle flag on src/a.ts, got %v", aFlags)
	}
	bFlags := riskFlags["src/b.ts"]
	if !containsFlag(bFlags, "cycle") {
		t.Errorf("expected cycle flag on src/b.ts, got %v", bFlags)
	}
	cFlags := riskFlags["src/c.ts"]
	if containsFlag(cFlags, "cycle") {
		t.Errorf("did not expect cycle flag on src/c.ts, got %v", cFlags)
	}
}

func TestRiskFlags_HighFanIn(t *testing.T) {
	files := map[string]string{
		"src/shared.ts": "export const shared = 1;",
		"src/a.ts":      "import { shared } from './shared';",
		"src/b.ts":      "import { shared } from './shared';",
		"src/c.ts":      "import { shared } from './shared';",
	}
	edges := []graph.Edge{
		{Source: "src/a.ts", Target: "src/shared.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/b.ts", Target: "src/shared.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/c.ts", Target: "src/shared.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
	}
	_, g := setupBuilderFixture(t, files, edges)

	fwd, rev := buildAdjacencyMaps(g)
	riskFlags := computeAllRiskFlags(g, fwd, rev)

	sharedFlags := riskFlags["src/shared.ts"]
	if !containsFlag(sharedFlags, "high_fan_in") {
		t.Errorf("expected high_fan_in flag on src/shared.ts, got %v", sharedFlags)
	}
}

func TestRiskFlags_HighFanOut(t *testing.T) {
	files := map[string]string{
		"src/hub.ts": "import * from many places",
		"src/a.ts":   "export const a = 1;",
		"src/b.ts":   "export const b = 1;",
		"src/c.ts":   "export const c = 1;",
		"src/d.ts":   "export const d = 1;",
		"src/e.ts":   "export const e = 1;",
	}
	edges := []graph.Edge{
		{Source: "src/hub.ts", Target: "src/a.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/hub.ts", Target: "src/b.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/hub.ts", Target: "src/c.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/hub.ts", Target: "src/d.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/hub.ts", Target: "src/e.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
	}
	_, g := setupBuilderFixture(t, files, edges)

	fwd, rev := buildAdjacencyMaps(g)
	riskFlags := computeAllRiskFlags(g, fwd, rev)

	hubFlags := riskFlags["src/hub.ts"]
	if !containsFlag(hubFlags, "high_fan_out") {
		t.Errorf("expected high_fan_out flag on src/hub.ts, got %v", hubFlags)
	}
}

func TestRiskFlags_EntrypointLike(t *testing.T) {
	files := map[string]string{
		"src/index.ts":          "export * from './App';",
		"src/main.tsx":          "ReactDOM.render(<App />);",
		"src/app.ts":            "export const app = {};",
		"src/pages/home.tsx":    "export default function Home() {}",
		"src/routes/api.ts":     "export const routes = [];",
		"src/app/layout.tsx":    "export default function Layout() {}",
		"src/utils/helper.ts":   "export function helper() {}",
	}
	_, g := setupBuilderFixture(t, files, nil)

	fwd, rev := buildAdjacencyMaps(g)
	riskFlags := computeAllRiskFlags(g, fwd, rev)

	entrypoints := []string{
		"src/index.ts",
		"src/main.tsx",
		"src/app.ts",
		"src/pages/home.tsx",
		"src/routes/api.ts",
		"src/app/layout.tsx",
	}
	for _, ep := range entrypoints {
		flags := riskFlags[ep]
		if !containsFlag(flags, "entrypoint_like") {
			t.Errorf("expected entrypoint_like flag on %s, got %v", ep, flags)
		}
	}

	helperFlags := riskFlags["src/utils/helper.ts"]
	if containsFlag(helperFlags, "entrypoint_like") {
		t.Errorf("did not expect entrypoint_like flag on src/utils/helper.ts, got %v", helperFlags)
	}
}

func TestRiskFlags_TestLike(t *testing.T) {
	files := map[string]string{
		"src/App.test.tsx":             "describe('App', () => {});",
		"src/hooks/useAuth.spec.ts":    "describe('useAuth', () => {});",
		"src/__tests__/integration.ts": "test('it works', () => {});",
		"src/test/helpers.ts":          "export function testHelper() {}",
		"src/tests/setup.ts":           "beforeAll(() => {});",
		"src/App.tsx":                  "export default function App() {}",
	}
	_, g := setupBuilderFixture(t, files, nil)

	fwd, rev := buildAdjacencyMaps(g)
	riskFlags := computeAllRiskFlags(g, fwd, rev)

	testFiles := []string{
		"src/App.test.tsx",
		"src/hooks/useAuth.spec.ts",
		"src/__tests__/integration.ts",
		"src/test/helpers.ts",
		"src/tests/setup.ts",
	}
	for _, tf := range testFiles {
		flags := riskFlags[tf]
		if !containsFlag(flags, "test_like") {
			t.Errorf("expected test_like flag on %s, got %v", tf, flags)
		}
	}

	appFlags := riskFlags["src/App.tsx"]
	if containsFlag(appFlags, "test_like") {
		t.Errorf("did not expect test_like flag on src/App.tsx, got %v", appFlags)
	}
}

func TestRiskFlags_LexicographicOrder(t *testing.T) {
	// A file with multiple flags should have them in lexicographic order.
	files := map[string]string{
		"src/index.ts":  "import './polyfills'; import { a } from './a';",
		"src/polyfills.ts": "// polyfills",
		"src/a.ts":      "import { index } from './index';",
		"src/b.ts":      "import { index } from './index';",
		"src/c.ts":      "import { index } from './index';",
	}
	edges := []graph.Edge{
		{Source: "src/index.ts", Target: "src/polyfills.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "side_effect", "import_kinds": "side_effect"}},
		{Source: "src/index.ts", Target: "src/a.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/a.ts", Target: "src/index.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/b.ts", Target: "src/index.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/c.ts", Target: "src/index.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
	}
	_, g := setupBuilderFixture(t, files, edges)

	fwd, rev := buildAdjacencyMaps(g)
	riskFlags := computeAllRiskFlags(g, fwd, rev)

	indexFlags := riskFlags["src/index.ts"]
	// Should have: cycle, entrypoint_like, high_fan_in, side_effect_import
	if len(indexFlags) < 2 {
		t.Fatalf("expected multiple flags on src/index.ts, got %v", indexFlags)
	}

	// Verify lexicographic order.
	for i := 1; i < len(indexFlags); i++ {
		if indexFlags[i-1] > indexFlags[i] {
			t.Errorf("flags not in lexicographic order: %v", indexFlags)
			break
		}
	}
}

func TestBuild_CycleMembers(t *testing.T) {
	files := map[string]string{
		"src/a.ts": "import { b } from './b'; export function a() {}",
		"src/b.ts": "import { a } from './a'; export function b() {}",
		"src/c.ts": "import { a } from './a'; export function c() {}",
	}
	edges := []graph.Edge{
		{Source: "src/a.ts", Target: "src/b.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/b.ts", Target: "src/a.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/c.ts", Target: "src/a.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
	}
	dir, g := setupBuilderFixture(t, files, edges)

	pack, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/a.ts"},
		TokenLimit:  10000,
		Graph:       g,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// b.ts should be included as it's a cycle neighbor of the seed.
	var foundB bool
	for _, f := range pack.Files {
		if f.Path == "src/b.ts" {
			foundB = true
			if !containsFlag(f.Flags, "cycle") {
				t.Errorf("expected cycle flag on src/b.ts, got flags %v", f.Flags)
			}
		}
	}
	if !foundB {
		t.Error("expected src/b.ts to be included as cycle neighbor")
	}
}

func TestBuild_OmittedCandidates(t *testing.T) {
	// Create files where seed takes most of the budget.
	largeContent := strings.Repeat("x", 400) // 100 tokens
	smallContent := "y"                        // 1 token

	files := map[string]string{
		"src/seed.ts": largeContent,
		"src/dep1.ts": smallContent,
		"src/dep2.ts": strings.Repeat("z", 800), // 200 tokens
	}
	edges := []graph.Edge{
		{Source: "src/seed.ts", Target: "src/dep1.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/seed.ts", Target: "src/dep2.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
	}
	dir, g := setupBuilderFixture(t, files, edges)

	// Use a tight estimator that just sums content tokens.
	tightEstimator := func(p *Pack) int {
		total := 0
		for _, f := range p.Files {
			total += EstimateTokens(f.Content)
		}
		return total
	}

	pack, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/seed.ts"},
		TokenLimit:  120, // Enough for seed (100) + dep1 (1) but not dep2 (200).
		Graph:       g,
		Estimator:   tightEstimator,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// dep2 should be omitted.
	if len(pack.OmittedCandidates) != 1 {
		t.Fatalf("expected 1 omitted candidate, got %d", len(pack.OmittedCandidates))
	}
	if pack.OmittedCandidates[0].Path != "src/dep2.ts" {
		t.Errorf("expected omitted candidate src/dep2.ts, got %s", pack.OmittedCandidates[0].Path)
	}
	if pack.OmittedCandidates[0].EstimatedTokens != 200 {
		t.Errorf("expected omitted tokens 200, got %d", pack.OmittedCandidates[0].EstimatedTokens)
	}
}

func TestBuild_SeedBudgetExceeded(t *testing.T) {
	largeContent := strings.Repeat("x", 4000) // 1000 tokens
	files := map[string]string{
		"src/big.ts": largeContent,
	}
	dir, g := setupBuilderFixture(t, files, nil)

	tightEstimator := func(p *Pack) int {
		total := 0
		for _, f := range p.Files {
			total += EstimateTokens(f.Content)
		}
		return total
	}

	_, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/big.ts"},
		TokenLimit:  500, // Less than the seed's 1000 tokens.
		Graph:       g,
		Estimator:   tightEstimator,
	})
	if err == nil {
		t.Fatal("expected SeedBudgetExceededError")
	}

	seedErr, ok := err.(*SeedBudgetExceededError)
	if !ok {
		t.Fatalf("expected *SeedBudgetExceededError, got %T: %v", err, err)
	}
	if seedErr.TokenLimit != 500 {
		t.Errorf("expected token limit 500, got %d", seedErr.TokenLimit)
	}
	if seedErr.MinimumBudget < 1000 {
		t.Errorf("expected minimum budget >= 1000, got %d", seedErr.MinimumBudget)
	}
}

func TestBuild_DeterministicSorting(t *testing.T) {
	files := map[string]string{
		"src/App.tsx":      "import many things",
		"src/z-module.ts":  "export function z() {}",
		"src/a-module.ts":  "export function a() {}",
		"src/m-module.ts":  "export function m() {}",
	}
	edges := []graph.Edge{
		{Source: "src/App.tsx", Target: "src/z-module.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/App.tsx", Target: "src/a-module.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/App.tsx", Target: "src/m-module.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
	}
	dir, g := setupBuilderFixture(t, files, edges)

	// Run build multiple times and verify same order.
	var lastOrder []string
	for i := 0; i < 5; i++ {
		pack, err := Build(BuildOptions{
			ProjectRoot: dir,
			Seeds:       []string{"src/App.tsx"},
			TokenLimit:  10000,
			Graph:       g,
		})
		if err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}

		var order []string
		for _, f := range pack.Files {
			order = append(order, f.Path)
		}

		if lastOrder != nil {
			for j := range order {
				if order[j] != lastOrder[j] {
					t.Errorf("run %d: non-deterministic ordering at index %d: %v vs %v", i, j, order, lastOrder)
					break
				}
			}
		}
		lastOrder = order
	}

	// Verify alphabetical ordering within same priority (non-seed deps).
	if len(lastOrder) >= 4 {
		deps := lastOrder[1:] // skip seed
		for i := 1; i < len(deps); i++ {
			if deps[i-1] > deps[i] {
				t.Errorf("expected alphabetical ordering within same priority: %v", deps)
				break
			}
		}
	}
}

func TestGuidance_DependentsMayBreak(t *testing.T) {
	files := map[string]string{
		"src/shared.ts":     "export const shared = 1;",
		"src/consumer1.ts":  "import { shared } from './shared';",
		"src/consumer2.ts":  "import { shared } from './shared';",
	}
	edges := []graph.Edge{
		{Source: "src/consumer1.ts", Target: "src/shared.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/consumer2.ts", Target: "src/shared.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
	}
	dir, g := setupBuilderFixture(t, files, edges)

	pack, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/shared.ts"},
		TokenLimit:  10000,
		Graph:       g,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Guidance should mention dependents that may break.
	foundGuidance := false
	for _, w := range pack.Guidance.Watch {
		if strings.Contains(w, "Changing src/shared.ts may affect") {
			foundGuidance = true
		}
	}
	if !foundGuidance {
		t.Errorf("expected guidance about dependents that may break, got: %v", pack.Guidance.Watch)
	}
}

func TestGuidance_CycleWarning(t *testing.T) {
	files := map[string]string{
		"src/a.ts": "import { b } from './b';",
		"src/b.ts": "import { a } from './a';",
	}
	edges := []graph.Edge{
		{Source: "src/a.ts", Target: "src/b.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/b.ts", Target: "src/a.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
	}
	dir, g := setupBuilderFixture(t, files, edges)

	pack, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/a.ts"},
		TokenLimit:  10000,
		Graph:       g,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Guidance should warn about cycles.
	foundCycle := false
	for _, w := range pack.Guidance.Watch {
		if strings.Contains(w, "Cycle detected") {
			foundCycle = true
		}
	}
	if !foundCycle {
		t.Errorf("expected cycle guidance, got: %v", pack.Guidance.Watch)
	}
}

func TestGuidance_SideEffectWarning(t *testing.T) {
	files := map[string]string{
		"src/App.tsx":      "import './polyfills';",
		"src/polyfills.ts": "// side effects",
	}
	edges := []graph.Edge{
		{Source: "src/App.tsx", Target: "src/polyfills.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "side_effect", "import_kinds": "side_effect"}},
	}
	dir, g := setupBuilderFixture(t, files, edges)

	pack, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/App.tsx"},
		TokenLimit:  10000,
		Graph:       g,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Guidance should warn about side-effect imports.
	foundSideEffect := false
	for _, w := range pack.Guidance.Watch {
		if strings.Contains(w, "side-effect import") {
			foundSideEffect = true
		}
	}
	if !foundSideEffect {
		t.Errorf("expected side-effect import guidance, got: %v", pack.Guidance.Watch)
	}
}

func TestGuidance_OmittedSuggestion(t *testing.T) {
	files := map[string]string{
		"src/seed.ts": strings.Repeat("x", 400),
		"src/dep.ts":  strings.Repeat("y", 4000),
	}
	edges := []graph.Edge{
		{Source: "src/seed.ts", Target: "src/dep.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
	}
	dir, g := setupBuilderFixture(t, files, edges)

	tightEstimator := func(p *Pack) int {
		total := 0
		for _, f := range p.Files {
			total += EstimateTokens(f.Content)
		}
		return total
	}

	pack, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/seed.ts"},
		TokenLimit:  110, // Enough for seed but not dep.
		Graph:       g,
		Estimator:   tightEstimator,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Guidance should suggest omitted files.
	foundOmitted := false
	for _, w := range pack.Guidance.Watch {
		if strings.Contains(w, "Omitted files worth fetching") {
			foundOmitted = true
		}
	}
	if !foundOmitted {
		t.Errorf("expected omitted files guidance, got: %v", pack.Guidance.Watch)
	}
}

func TestCandidate_PriorityOrder(t *testing.T) {
	// Verify the priority ordering of candidates matches the spec.
	files := map[string]string{
		"src/seed.ts":           "import './dep'; import type { T } from './typeDep';",
		"src/dep.ts":            "export function dep() {}",
		"src/typeDep.ts":        "export type T = string;",
		"src/dependent.ts":      "import { seed } from './seed';",
		"src/typeDependent.ts":  "import type { Seed } from './seed';",
	}
	edges := []graph.Edge{
		{Source: "src/seed.ts", Target: "src/dep.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/seed.ts", Target: "src/typeDep.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "type_only", "import_kinds": "type_only"}},
		{Source: "src/dependent.ts", Target: "src/seed.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "import_kinds": "static"}},
		{Source: "src/typeDependent.ts", Target: "src/seed.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "type_only", "import_kinds": "type_only"}},
	}
	dir, g := setupBuilderFixture(t, files, edges)

	pack, err := Build(BuildOptions{
		ProjectRoot: dir,
		Seeds:       []string{"src/seed.ts"},
		TokenLimit:  10000,
		Graph:       g,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected order: seed (0), dep (10), dependent (20), typeDep (30), typeDependent (30)
	if len(pack.Files) != 5 {
		t.Fatalf("expected 5 files, got %d", len(pack.Files))
	}

	expectedOrder := []struct {
		path string
		role string
	}{
		{"src/seed.ts", "seed"},
		{"src/dep.ts", "dependency"},
		{"src/dependent.ts", "dependent"},
		{"src/typeDep.ts", "type_dependency"},
		{"src/typeDependent.ts", "type_dependent"},
	}

	for i, expected := range expectedOrder {
		if i >= len(pack.Files) {
			break
		}
		if pack.Files[i].Path != expected.path {
			t.Errorf("position %d: expected %s, got %s", i, expected.path, pack.Files[i].Path)
		}
		if pack.Files[i].Role != expected.role {
			t.Errorf("position %d (%s): expected role %s, got %s", i, pack.Files[i].Path, expected.role, pack.Files[i].Role)
		}
	}
}

// containsFlag checks if a slice contains a given flag string.
func containsFlag(flags []string, flag string) bool {
	for _, f := range flags {
		if f == flag {
			return true
		}
	}
	return false
}
