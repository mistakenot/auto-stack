package codegraph

import (
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

	g, _, err := Build(dir, "typescript")
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

func TestBuildMergedMetadata(t *testing.T) {
	requireAstGrep(t)
	dir := fixtureDir(t, "merged-imports")

	g, _, err := Build(dir, "typescript")
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
