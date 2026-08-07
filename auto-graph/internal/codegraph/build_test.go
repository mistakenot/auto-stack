package codegraph

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func requireAstGrep(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ast-grep"); err != nil {
		t.Skip("ast-grep not installed")
	}
}

func TestDetectLanguage(t *testing.T) {
	t.Run("detects typescript from tsconfig.json", func(t *testing.T) {
		dir := fixtureDir(t, "basic-imports")
		lang, err := DetectLanguage(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lang != "typescript" {
			t.Errorf("expected typescript, got %q", lang)
		}
	})

	t.Run("returns error when no config found", func(t *testing.T) {
		dir := t.TempDir()
		_, err := DetectLanguage(dir)
		if err == nil {
			t.Fatal("expected error for directory without tsconfig.json")
		}
		if !strings.Contains(err.Error(), "could not detect project language") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestDiscoverFiles(t *testing.T) {
	t.Run("discovers typescript files", func(t *testing.T) {
		dir := fixtureDir(t, "basic-imports")
		files, err := DiscoverFiles(dir, "typescript")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) == 0 {
			t.Fatal("expected files to be discovered")
		}
		// All paths should use forward slashes and be relative.
		for _, f := range files {
			if filepath.IsAbs(f) {
				t.Errorf("expected relative path, got %q", f)
			}
			if strings.Contains(f, "\\") {
				t.Errorf("expected forward slashes, got %q", f)
			}
		}
	})

	t.Run("skips node_modules and hidden dirs", func(t *testing.T) {
		// Create a temp dir with node_modules and a hidden dir.
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
		os.WriteFile(filepath.Join(dir, "node_modules", "lib.ts"), []byte("export {}"), 0o644)
		os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755)
		os.WriteFile(filepath.Join(dir, ".hidden", "secret.ts"), []byte("export {}"), 0o644)
		os.WriteFile(filepath.Join(dir, "app.ts"), []byte("export {}"), 0o644)

		files, err := DiscoverFiles(dir, "typescript")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 1 {
			t.Fatalf("expected 1 file, got %d: %v", len(files), files)
		}
		if files[0] != "app.ts" {
			t.Errorf("expected app.ts, got %q", files[0])
		}
	})

	t.Run("results are sorted", func(t *testing.T) {
		dir := fixtureDir(t, "basic-imports")
		files, err := DiscoverFiles(dir, "typescript")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for i := 1; i < len(files); i++ {
			if files[i] < files[i-1] {
				t.Errorf("files not sorted: %q comes after %q", files[i], files[i-1])
			}
		}
	})

	t.Run("unsupported language returns error", func(t *testing.T) {
		_, err := DiscoverFiles(t.TempDir(), "python")
		if err == nil {
			t.Fatal("expected error for unsupported language")
		}
	})
}

func TestBuildGraphParity(t *testing.T) {
	requireAstGrep(t)
	dir := fixtureDir(t, "basic-imports")

	g, _, err := Build(dir, "typescript", io.Discard)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// basic-imports has: src/index.ts, src/utils.ts, src/helpers.ts, src/components/App.tsx
	if len(g.Nodes) == 0 {
		t.Fatal("expected nodes in graph")
	}

	// Check that nodes have expected fields.
	for _, n := range g.Nodes {
		if n.Kind != "file" {
			t.Errorf("expected node kind 'file', got %q", n.Kind)
		}
		if n.Language != "typescript" {
			t.Errorf("expected language 'typescript', got %q", n.Language)
		}
		if n.Path == "" {
			t.Error("expected non-empty path")
		}
	}

	// Verify edges have the merged attrs.
	if len(g.Edges) == 0 {
		t.Fatal("expected edges in graph")
	}

	for _, e := range g.Edges {
		if e.Attrs["import_kind"] == "" {
			t.Errorf("edge %s->%s missing import_kind attr", e.Source, e.Target)
		}
		if e.Attrs["import_kinds"] == "" {
			t.Errorf("edge %s->%s missing import_kinds attr", e.Source, e.Target)
		}
		if e.Attrs["raw"] == "" {
			t.Errorf("edge %s->%s missing raw attr", e.Source, e.Target)
		}
		if e.Attrs["raws"] == "" {
			t.Errorf("edge %s->%s missing raws attr", e.Source, e.Target)
		}
	}
}

// writeFile writes content to dir/rel, creating parent directories.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBuildResolvesAliasWithGlobInclude is a regression test for the tsconfig
// comma-stripping bug: a multi-element string include array like
// ["**/*.ts", "**/*.tsx"] used to corrupt the JSON, drop every path alias, and
// silently produce a graph with no aliased edges. The alias edge must resolve.
func TestBuildResolvesAliasWithGlobInclude(t *testing.T) {
	requireAstGrep(t)
	dir := t.TempDir()
	writeFile(t, dir, "tsconfig.json", `{
  "compilerOptions": { "baseUrl": ".", "paths": { "@/*": ["./src/*"] } },
  "include": ["**/*.ts", "**/*.tsx"]
}`)
	writeFile(t, dir, "src/util.ts", "export const x = 1;\n")
	writeFile(t, dir, "src/index.ts", "import { x } from \"@/util\";\nexport const y = x;\n")

	g, diags, err := Build(dir, "typescript", io.Discard)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	for _, d := range diags {
		t.Errorf("unexpected unresolved-alias diagnostic: %s:%d %q", d.Source, d.Line, d.Raw)
	}
	found := false
	for _, e := range g.Edges {
		if strings.Contains(e.Source, "index") && strings.Contains(e.Target, "util") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an alias-resolved edge index->util; edges: %+v", g.Edges)
	}
}

// TestBuildFailsOnUnparseableTSConfig asserts the hard-fail contract: a
// tsconfig.json that is present but genuinely unparseable must fail Build with
// no output, rather than silently dropping alias edges.
func TestBuildFailsOnUnparseableTSConfig(t *testing.T) {
	requireAstGrep(t)
	dir := t.TempDir()
	// Bare `.` is not a valid JSON value; stripJSONC cannot rescue this.
	writeFile(t, dir, "tsconfig.json", `{ "compilerOptions": { "baseUrl": . } }`)
	writeFile(t, dir, "src/index.ts", "export const y = 1;\n")

	g, _, err := Build(dir, "typescript", io.Discard)
	if err == nil {
		t.Fatal("expected Build to fail on unparseable tsconfig.json")
	}
	if g != nil {
		t.Errorf("expected no graph output on failure, got %d nodes", len(g.Nodes))
	}
	if !strings.Contains(err.Error(), "tsconfig.json") {
		t.Errorf("error should name tsconfig.json, got: %v", err)
	}
}

// TestBuildDiagnosesComputedDynamicImport verifies that a dynamic import with a
// computed (non-literal) specifier is surfaced as a coverage-gap diagnostic
// rather than dropped silently, while a sibling static import still resolves.
func TestBuildDiagnosesComputedDynamicImport(t *testing.T) {
	requireAstGrep(t)
	dir := t.TempDir()
	writeFile(t, dir, "tsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["./src/*"]}},"include":["**/*.ts"]}`)
	writeFile(t, dir, "src/dep.ts", "export const x = 1;\n")
	writeFile(t, dir, "src/main.ts", "const name = String(1);\nconst a = import(name);\nimport { x } from \"@/dep\";\n")

	g, diags, err := Build(dir, "typescript", io.Discard)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	var dyn *Diagnostic
	for i := range diags {
		if diags[i].Kind == DiagUnresolvedDynamic {
			dyn = &diags[i]
		}
	}
	if dyn == nil {
		t.Fatalf("expected an unresolved_dynamic diagnostic, got %+v", diags)
	}
	if !strings.Contains(dyn.Raw, "import(") {
		t.Errorf("diagnostic Raw should show the computed expression, got %q", dyn.Raw)
	}
	if !strings.Contains(dyn.Source, "main.ts") {
		t.Errorf("diagnostic Source should point at main.ts, got %q", dyn.Source)
	}

	// The computed dynamic import must not produce an edge; the static @/dep one must.
	for _, e := range g.Edges {
		if e.Attrs["import_kind"] == "dynamic" {
			t.Errorf("computed dynamic import should not produce an edge, got %+v", e)
		}
	}
	staticFound := false
	for _, e := range g.Edges {
		if strings.Contains(e.Source, "main") && strings.Contains(e.Target, "dep") {
			staticFound = true
		}
	}
	if !staticFound {
		t.Errorf("expected the static @/dep edge to resolve; edges: %+v", g.Edges)
	}
}

func TestBuildMergedMetadata(t *testing.T) {
	requireAstGrep(t)
	dir := fixtureDir(t, "merged-imports")

	g, _, err := Build(dir, "typescript", io.Discard)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// consumer.ts imports shared.ts with both static and type kinds.
	// The scanner should now return both, and build should merge them.
	var found bool
	for _, e := range g.Edges {
		if e.Source == "consumer.ts" && e.Target == "shared.ts" {
			found = true

			// Check import_kinds contains both static and type_only (canonicalized from "type").
			kinds := strings.Split(e.Attrs["import_kinds"], ",")
			hasStatic := false
			hasTypeOnly := false
			for _, k := range kinds {
				if k == "static" {
					hasStatic = true
				}
				if k == "type_only" {
					hasTypeOnly = true
				}
			}
			if !hasStatic {
				t.Errorf("expected import_kinds to contain 'static', got %q", e.Attrs["import_kinds"])
			}
			if !hasTypeOnly {
				t.Errorf("expected import_kinds to contain 'type_only', got %q", e.Attrs["import_kinds"])
			}

			// Primary import_kind should be the first encountered (static).
			if e.Attrs["import_kind"] != "static" {
				t.Errorf("expected primary import_kind 'static', got %q", e.Attrs["import_kind"])
			}

			break
		}
	}

	if !found {
		t.Error("expected edge consumer.ts -> shared.ts not found")
		t.Logf("edges: %+v", g.Edges)
	}
}
