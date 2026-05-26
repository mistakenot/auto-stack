package resolver

import (
	"encoding/json"
	"os"
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

func TestAliasResolveWithDotSlashTarget(t *testing.T) {
	dir := fixtureDir(t, "alias-reexports")

	r := NewTypeScriptResolver(dir)

	// The alias-reexports fixture has "@/*": ["./src/*"] with baseUrl: "."
	// Resolve @/utils/format -> src/utils/format.ts
	sourceFile := filepath.Join(dir, "src", "index.ts")
	result, err := r.Resolve("@/utils/format", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.IsExternal {
		t.Fatal("expected internal import, got external")
	}
	if result.ResolvedPath != "src/utils/format.ts" {
		t.Errorf("expected resolved path %q, got %q", "src/utils/format.ts", result.ResolvedPath)
	}
	if !result.MatchedAlias {
		t.Error("expected MatchedAlias to be true")
	}
}

// writeTSConfig is a test helper that writes a tsconfig.json with given compilerOptions.
func writeTSConfig(t *testing.T, dir string, baseURL string, paths map[string][]string) {
	t.Helper()
	cfg := struct {
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl,omitempty"`
			Paths   map[string][]string `json:"paths,omitempty"`
		} `json:"compilerOptions"`
	}{}
	cfg.CompilerOptions.BaseURL = baseURL
	cfg.CompilerOptions.Paths = paths
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

// writeFile is a test helper that creates a file with given content, making parent dirs.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestExactAliasMapping(t *testing.T) {
	dir := t.TempDir()

	// Set up tsconfig with exact mapping (no wildcard).
	writeTSConfig(t, dir, ".", map[string][]string{
		"@config": {"./src/config.ts"},
	})
	writeFile(t, filepath.Join(dir, "src", "config.ts"), "export const cfg = {};")

	r := NewTypeScriptResolver(dir)
	sourceFile := filepath.Join(dir, "src", "index.ts")

	// Exact match: "@config" should resolve.
	result, err := r.Resolve("@config", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.ResolvedPath != "src/config.ts" {
		t.Errorf("expected resolved path %q, got %q", "src/config.ts", result.ResolvedPath)
	}
	if !result.MatchedAlias {
		t.Error("expected MatchedAlias to be true for exact alias match")
	}

	// "@config/extra" should NOT match the exact alias — it should be external.
	result2, err := r.Resolve("@config/extra", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if !result2.IsExternal {
		t.Error("expected @config/extra to be classified as external (no wildcard match)")
	}
	if result2.MatchedAlias {
		t.Error("expected MatchedAlias to be false for @config/extra")
	}
}

func TestUnresolvedAliasMetadata(t *testing.T) {
	dir := fixtureDir(t, "alias-reexports")

	r := NewTypeScriptResolver(dir)
	sourceFile := filepath.Join(dir, "src", "index.ts")

	// @/does-not-exist matches the alias pattern but doesn't resolve to a file.
	result, err := r.Resolve("@/does-not-exist", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.ResolvedPath != "" {
		t.Errorf("expected empty resolved path, got %q", result.ResolvedPath)
	}
	if result.IsExternal {
		t.Error("expected IsExternal to be false for unresolved alias")
	}
	if !result.MatchedAlias {
		t.Error("expected MatchedAlias to be true for unresolved alias")
	}
}

func TestZeroLengthWildcardCapture(t *testing.T) {
	dir := fixtureDir(t, "alias-reexports")

	r := NewTypeScriptResolver(dir)
	sourceFile := filepath.Join(dir, "src", "index.ts")

	// "@/" is the alias prefix with zero-length capture — should not match.
	result, err := r.Resolve("@/", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if !result.IsExternal {
		t.Error("expected @/ (zero-length capture) to fall through to bare/external")
	}
	if result.MatchedAlias {
		t.Error("expected MatchedAlias to be false for zero-length wildcard capture")
	}
}

func TestBaseUrlProbing(t *testing.T) {
	dir := t.TempDir()

	// Set up tsconfig with baseUrl but no paths.
	writeTSConfig(t, dir, ".", nil)
	writeFile(t, filepath.Join(dir, "src", "utils.ts"), "export function util() {}")

	r := NewTypeScriptResolver(dir)
	sourceFile := filepath.Join(dir, "app.ts")

	// Import "src/utils" should probe baseUrl/src/utils and find src/utils.ts.
	result, err := r.Resolve("src/utils", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.IsExternal {
		t.Fatal("expected baseUrl probe to resolve, got external")
	}
	if result.ResolvedPath != "src/utils.ts" {
		t.Errorf("expected resolved path %q, got %q", "src/utils.ts", result.ResolvedPath)
	}
}
