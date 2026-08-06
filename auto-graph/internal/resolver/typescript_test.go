package resolver

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
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

func TestRelativeResolve(t *testing.T) {
	dir := fixtureDir(t, "basic-imports")

	r := NewTypeScriptResolver(dir, io.Discard)

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

	r := NewTypeScriptResolver(dir, io.Discard)

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

	r := NewTypeScriptResolver(dir, io.Discard)

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

	r := NewTypeScriptResolver(dir, io.Discard)

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

	r := NewTypeScriptResolver(dir, io.Discard)

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

	r := NewTypeScriptResolver(dir, io.Discard)

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

	r := NewTypeScriptResolver(dir, io.Discard)

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

	r := NewTypeScriptResolver(dir, io.Discard)
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

	r := NewTypeScriptResolver(dir, io.Discard)
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

	r := NewTypeScriptResolver(dir, io.Discard)
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

	r := NewTypeScriptResolver(dir, io.Discard)
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

func TestStripJSONC(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "trailing commas stripped",
			input: `{"a": 1, "b": [1, 2,], "c": {"x": 1,},}`,
			want:  `{"a": 1, "b": [1, 2], "c": {"x": 1}}`,
		},
		{
			name:  "line comments stripped",
			input: "{\n  // comment\n  \"a\": 1\n}",
			want:  "{\n  \n  \"a\": 1\n}",
		},
		{
			name:  "both combined",
			input: "{\n  // comment\n  \"a\": [1,],\n}",
			want:  "{\n  \n  \"a\": [1]\n}",
		},
		{
			name:  "valid JSON unchanged",
			input: `{"a": 1, "b": [1, 2]}`,
			want:  `{"a": 1, "b": [1, 2]}`,
		},
		{
			name:  "comment-like text inside strings preserved",
			input: `{"url": "https://example.com"}`,
			want:  `{"url": "https://example.com"}`,
		},
		{
			name:  "comma-brace inside string preserved",
			input: `{"description": "Hello, }"}`,
			want:  `{"description": "Hello, }"}`,
		},
		{
			// Regression: the separator comma before a string element must
			// survive. Previously the trailing-comma remover deleted it,
			// corrupting the array. See auto-graph-tsconfig-bug report.
			name:  "separator comma before string element preserved",
			input: `["a", "b"]`,
			want:  `["a", "b"]`,
		},
		{
			name:  "separator comma before string element no space",
			input: `["a","b"]`,
			want:  `["a","b"]`,
		},
		{
			// The exact real-world include array that broke alias resolution.
			name:  "glob include array preserved",
			input: `{"include": ["**/*.ts", "**/*.tsx"]}`,
			want:  `{"include": ["**/*.ts", "**/*.tsx"]}`,
		},
		{
			name:  "multi-target paths preserved",
			input: `{"paths": {"@/*": ["./src/*", "./gen/*"]}}`,
			want:  `{"paths": {"@/*": ["./src/*", "./gen/*"]}}`,
		},
		{
			name:  "trailing comma in string array still removed",
			input: `["a", "b",]`,
			want:  `["a", "b"]`,
		},
		{
			name:  "block comment stripped",
			input: "{\n  /* modules */\n  \"a\": 1\n}",
			want:  "{\n  \n  \"a\": 1\n}",
		},
		{
			name:  "block comment newlines preserved",
			input: "{\n  /* line one\n     line two */\n  \"a\": 1\n}",
			want:  "{\n  \n\n  \"a\": 1\n}",
		},
		{
			name:  "block-comment delimiters inside glob string preserved",
			input: `["**/*.ts"]`,
			want:  `["**/*.ts"]`,
		},
		{
			// Escaped quote inside a string must not end the string, so the
			// comma and bracket that follow it stay part of the value.
			name:  "escaped quote inside string preserved",
			input: `{"a": "he\"llo, ]"}`,
			want:  `{"a": "he\"llo, ]"}`,
		},
		{
			// Escaped backslashes (Windows-style path aliases) must survive,
			// and the trailing \* must not be read as a comment delimiter.
			name:  "escaped backslash windows path preserved",
			input: `{"paths": {"@/*": ["C:\\src\\*"]}}`,
			want:  `{"paths": {"@/*": ["C:\\src\\*"]}}`,
		},
		{
			name:  "unterminated block comment consumed to end",
			input: `{"a": 1 /* oops`,
			want:  `{"a": 1 `,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(stripJSONC([]byte(tt.input)))
			if got != tt.want {
				t.Errorf("stripJSONC(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestJSONCTrailingCommas(t *testing.T) {
	dir := fixtureDir(t, "jsonc-tsconfig")

	r := NewTypeScriptResolver(dir, io.Discard)

	// Resolve @/utils/format from src/routes/dashboard.tsx -> src/utils/format.ts
	sourceFile := filepath.Join(dir, "src", "routes", "dashboard.tsx")
	result, err := r.Resolve("@/utils/format", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.ResolvedPath != "src/utils/format.ts" {
		t.Errorf("expected resolved path %q, got %q", "src/utils/format.ts", result.ResolvedPath)
	}
	if !result.MatchedAlias {
		t.Error("expected MatchedAlias to be true")
	}
}

func TestJSONCComments(t *testing.T) {
	dir := t.TempDir()

	// Write a tsconfig with only // comments (no trailing commas).
	tsconfig := `{
  // This is a comment
  "compilerOptions": {
    // Base URL for module resolution
    "baseUrl": ".",
    "paths": {
      "@lib/*": ["./lib/*"]
    }
  }
}
`
	writeFile(t, filepath.Join(dir, "tsconfig.json"), tsconfig)
	writeFile(t, filepath.Join(dir, "lib", "helpers.ts"), "export function help() {}")

	r := NewTypeScriptResolver(dir, io.Discard)
	sourceFile := filepath.Join(dir, "app.ts")

	result, err := r.Resolve("@lib/helpers", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.ResolvedPath != "lib/helpers.ts" {
		t.Errorf("expected resolved path %q, got %q", "lib/helpers.ts", result.ResolvedPath)
	}
	if !result.MatchedAlias {
		t.Error("expected MatchedAlias to be true")
	}
}

func TestMalformedTSConfigWarning(t *testing.T) {
	dir := t.TempDir()

	// Write a genuinely malformed tsconfig.
	writeFile(t, filepath.Join(dir, "tsconfig.json"), "{{{")
	writeFile(t, filepath.Join(dir, "src", "app.ts"), "export function app() {}")

	var buf bytes.Buffer
	r := NewTypeScriptResolver(dir, &buf)

	// A present-but-unparseable tsconfig must be recorded as a load error so
	// callers (e.g. graph Build) can fail loudly instead of silently dropping
	// aliases.
	if r.LoadErr() == nil {
		t.Fatal("expected LoadErr for malformed tsconfig")
	}
	if !strings.Contains(r.LoadErr().Error(), "failed to parse:") {
		t.Errorf("unexpected LoadErr message: %v", r.LoadErr())
	}

	// Resolver should still work — no panic, just no alias resolution.
	sourceFile := filepath.Join(dir, "src", "app.ts")
	result, err := r.Resolve("./app", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	// No aliases loaded, so non-relative should be external.
	result2, err := r.Resolve("@/something", sourceFile, dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if !result2.IsExternal {
		t.Error("expected @/something to be external when tsconfig is malformed")
	}
	_ = result // ensure no panic on relative resolve
}
