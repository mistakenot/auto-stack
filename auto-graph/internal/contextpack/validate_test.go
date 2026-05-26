package contextpack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-graph/internal/graph"
)

func setupValidateFixture(t *testing.T) (string, *graph.Graph) {
	t.Helper()
	dir := t.TempDir()

	// Create fixture files
	files := []string{
		"src/App.tsx",
		"src/hooks/useAuth.ts",
		"src/services/userService.ts",
	}
	for _, f := range files {
		abs := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("// "+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Build a graph with these files as nodes
	g := &graph.Graph{
		Root: dir,
		Nodes: []graph.Node{
			{ID: "src/App.tsx", Kind: graph.NodeFile, Path: "src/App.tsx"},
			{ID: "src/hooks/useAuth.ts", Kind: graph.NodeFile, Path: "src/hooks/useAuth.ts"},
			{ID: "src/services/userService.ts", Kind: graph.NodeFile, Path: "src/services/userService.ts"},
		},
	}

	return dir, g
}

func TestValidate_Whitespace(t *testing.T) {
	dir, g := setupValidateFixture(t)

	seeds := []string{"  src/App.tsx  ", "\tsrc/hooks/useAuth.ts\n"}
	result, err := NormalizeAndValidateSeeds(seeds, dir, g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0] != "src/App.tsx" {
		t.Errorf("expected src/App.tsx, got %s", result[0])
	}
	if result[1] != "src/hooks/useAuth.ts" {
		t.Errorf("expected src/hooks/useAuth.ts, got %s", result[1])
	}
}

func TestValidate_EmptyStringsSkipped(t *testing.T) {
	dir, g := setupValidateFixture(t)

	seeds := []string{"", "  ", "src/App.tsx", ""}
	result, err := NormalizeAndValidateSeeds(seeds, dir, g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0] != "src/App.tsx" {
		t.Errorf("expected src/App.tsx, got %s", result[0])
	}
}

func TestValidate_DuplicateSeeds(t *testing.T) {
	dir, g := setupValidateFixture(t)

	seeds := []string{"src/App.tsx", "src/hooks/useAuth.ts", "src/App.tsx"}
	result, err := NormalizeAndValidateSeeds(seeds, dir, g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results (deduped), got %d", len(result))
	}
	if result[0] != "src/App.tsx" {
		t.Errorf("expected src/App.tsx first, got %s", result[0])
	}
	if result[1] != "src/hooks/useAuth.ts" {
		t.Errorf("expected src/hooks/useAuth.ts second, got %s", result[1])
	}
}

func TestValidate_SafeAbsolutePaths(t *testing.T) {
	dir, g := setupValidateFixture(t)

	absPath := filepath.Join(dir, "src", "App.tsx")
	seeds := []string{absPath}
	result, err := NormalizeAndValidateSeeds(seeds, dir, g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0] != "src/App.tsx" {
		t.Errorf("expected src/App.tsx, got %s", result[0])
	}
}

func TestValidate_MissingFiles(t *testing.T) {
	dir, g := setupValidateFixture(t)

	seeds := []string{"src/missing.ts"}
	_, err := NormalizeAndValidateSeeds(seeds, dir, g)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	valErrs, ok := err.(*ValidationErrors)
	if !ok {
		t.Fatalf("expected *ValidationErrors, got %T", err)
	}
	if len(valErrs.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(valErrs.Errors))
	}
	if valErrs.Errors[0].Code != "missing_file" {
		t.Errorf("expected code missing_file, got %s", valErrs.Errors[0].Code)
	}
}

func TestValidate_OutsideProjectPaths(t *testing.T) {
	dir, g := setupValidateFixture(t)

	seeds := []string{"../outside.ts"}
	_, err := NormalizeAndValidateSeeds(seeds, dir, g)
	if err == nil {
		t.Fatal("expected error for outside-project path")
	}
	valErrs, ok := err.(*ValidationErrors)
	if !ok {
		t.Fatalf("expected *ValidationErrors, got %T", err)
	}
	if len(valErrs.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(valErrs.Errors))
	}
	if valErrs.Errors[0].Code != "outside_project" {
		t.Errorf("expected code outside_project, got %s", valErrs.Errors[0].Code)
	}
}

func TestValidate_AbsolutePathOutsideProject(t *testing.T) {
	dir, g := setupValidateFixture(t)

	seeds := []string{"/tmp/some/other/path.ts"}
	_, err := NormalizeAndValidateSeeds(seeds, dir, g)
	if err == nil {
		t.Fatal("expected error for absolute path outside project")
	}
	valErrs, ok := err.(*ValidationErrors)
	if !ok {
		t.Fatalf("expected *ValidationErrors, got %T", err)
	}
	if valErrs.Errors[0].Code != "outside_project" {
		t.Errorf("expected code outside_project, got %s", valErrs.Errors[0].Code)
	}
}

func TestValidate_NotInGraph(t *testing.T) {
	dir, g := setupValidateFixture(t)

	// Create a file that exists on disk but is not in the graph
	extraFile := filepath.Join(dir, "src", "extra.ts")
	if err := os.WriteFile(extraFile, []byte("// extra"), 0o644); err != nil {
		t.Fatal(err)
	}

	seeds := []string{"src/extra.ts"}
	_, err := NormalizeAndValidateSeeds(seeds, dir, g)
	if err == nil {
		t.Fatal("expected error for file not in graph")
	}
	valErrs, ok := err.(*ValidationErrors)
	if !ok {
		t.Fatalf("expected *ValidationErrors, got %T", err)
	}
	if len(valErrs.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(valErrs.Errors))
	}
	if valErrs.Errors[0].Code != "not_in_graph" {
		t.Errorf("expected code not_in_graph, got %s", valErrs.Errors[0].Code)
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	dir, g := setupValidateFixture(t)

	seeds := []string{"../outside.ts", "src/missing.ts"}
	_, err := NormalizeAndValidateSeeds(seeds, dir, g)
	if err == nil {
		t.Fatal("expected errors")
	}
	valErrs, ok := err.(*ValidationErrors)
	if !ok {
		t.Fatalf("expected *ValidationErrors, got %T", err)
	}
	if len(valErrs.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(valErrs.Errors))
	}
}

func TestValidate_MixedValidAndInvalid(t *testing.T) {
	dir, g := setupValidateFixture(t)

	// Even if some seeds are valid, if any are invalid, return errors
	seeds := []string{"src/App.tsx", "src/missing.ts"}
	_, err := NormalizeAndValidateSeeds(seeds, dir, g)
	if err == nil {
		t.Fatal("expected error when any seed is invalid")
	}
	valErrs, ok := err.(*ValidationErrors)
	if !ok {
		t.Fatalf("expected *ValidationErrors, got %T", err)
	}
	if len(valErrs.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(valErrs.Errors))
	}
	if valErrs.Errors[0].Code != "missing_file" {
		t.Errorf("expected code missing_file, got %s", valErrs.Errors[0].Code)
	}
}
