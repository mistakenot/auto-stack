package resolver

import (
	"path/filepath"
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

func TestRelativeResolve(t *testing.T) {
	dir := fixtureDir(t, "basic-imports")

	r := NewTypeScriptResolver(dir)

	// Resolve ./utils from src/index.ts -> src/utils.ts
	sourceFile := filepath.Join(dir, "src", "index.ts")
	result, err := r.Resolve("./utils", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.IsExternal {
		t.Fatal("expected internal import, got external")
	}
	if result.ResolvedPath != "src/utils.ts" {
		t.Errorf("expected resolved path %q, got %q", "src/utils.ts", result.ResolvedPath)
	}
}

func TestAliasResolve(t *testing.T) {
	dir := fixtureDir(t, "path-aliases")

	r := NewTypeScriptResolver(dir)

	// Resolve @/components/Button from src/index.ts -> src/components/Button.ts
	sourceFile := filepath.Join(dir, "src", "index.ts")
	result, err := r.Resolve("@/components/Button", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.IsExternal {
		t.Fatal("expected internal import, got external")
	}
	if result.ResolvedPath != "src/components/Button.ts" {
		t.Errorf("expected resolved path %q, got %q", "src/components/Button.ts", result.ResolvedPath)
	}
}

func TestIndexResolve(t *testing.T) {
	dir := fixtureDir(t, "index-resolution")

	r := NewTypeScriptResolver(dir)

	// Resolve ./lib from src/index.ts -> src/lib/index.ts
	sourceFile := filepath.Join(dir, "src", "index.ts")
	result, err := r.Resolve("./lib", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.IsExternal {
		t.Fatal("expected internal import, got external")
	}
	if result.ResolvedPath != "src/lib/index.ts" {
		t.Errorf("expected resolved path %q, got %q", "src/lib/index.ts", result.ResolvedPath)
	}
}

func TestBareSpecifier(t *testing.T) {
	dir := fixtureDir(t, "basic-imports")

	r := NewTypeScriptResolver(dir)

	// Resolve "react" -> should be external
	sourceFile := filepath.Join(dir, "src", "index.ts")
	result, err := r.Resolve("react", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if !result.IsExternal {
		t.Error("expected bare specifier 'react' to be classified as external")
	}
}

func TestExtensionProbing(t *testing.T) {
	dir := fixtureDir(t, "basic-imports")

	r := NewTypeScriptResolver(dir)

	// Resolve ./helpers from src/utils.ts -> src/helpers.ts (probing adds .ts extension)
	sourceFile := filepath.Join(dir, "src", "utils.ts")
	result, err := r.Resolve("./helpers", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.IsExternal {
		t.Fatal("expected internal import, got external")
	}
	if result.ResolvedPath != "src/helpers.ts" {
		t.Errorf("expected resolved path %q, got %q", "src/helpers.ts", result.ResolvedPath)
	}
}

func TestTsxExtensionProbing(t *testing.T) {
	dir := fixtureDir(t, "basic-imports")

	r := NewTypeScriptResolver(dir)

	// Resolve ./components/App from src/index.ts -> src/components/App.tsx
	sourceFile := filepath.Join(dir, "src", "index.ts")
	result, err := r.Resolve("./components/App", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.IsExternal {
		t.Fatal("expected internal import, got external")
	}
	if result.ResolvedPath != "src/components/App.tsx" {
		t.Errorf("expected resolved path %q, got %q", "src/components/App.tsx", result.ResolvedPath)
	}
}
