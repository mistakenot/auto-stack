package scanner

import (
	"os"
	"os/exec"
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

func requireAstGrep(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ast-grep"); err != nil {
		t.Skip("ast-grep not installed")
	}
}

func TestBasicImports(t *testing.T) {
	requireAstGrep(t)
	dir := fixtureDir(t, "basic-imports")

	sc := NewTypeScriptScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Expected imports:
	// src/index.ts -> ./utils
	// src/index.ts -> ./components/App
	// src/utils.ts -> ./helpers
	// src/components/App.tsx -> ../utils
	expected := map[string][]string{
		"src/index.ts":           {"./utils", "./components/App"},
		"src/utils.ts":           {"./helpers"},
		"src/components/App.tsx": {"../utils"},
	}

	// Build a map from results for checking.
	got := make(map[string]map[string]bool)
	for _, m := range matches {
		rel, err := filepath.Rel(dir, m.SourceFile)
		if err != nil {
			t.Fatalf("could not make relative path: %v", err)
		}
		rel = filepath.ToSlash(rel)
		if got[rel] == nil {
			got[rel] = make(map[string]bool)
		}
		got[rel][m.ImportPath] = true
	}

	for file, imports := range expected {
		for _, imp := range imports {
			if !got[file][imp] {
				t.Errorf("expected %s to import %q, but it was not found. Got imports for %s: %v", file, imp, file, got[file])
			}
		}
	}
}

func TestAllImportStyles(t *testing.T) {
	requireAstGrep(t)
	dir := fixtureDir(t, "all-import-styles")

	sc := NewTypeScriptScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// We expect these kinds to be present for main.ts:
	// static: ./static_target
	// dynamic: ./dynamic_target
	// require: ./require_target
	// side-effect: ./side_effect
	// type: ./type_target
	// And for reexport_source.ts:
	// reexport: ./reexport_target
	type importEntry struct {
		file       string
		importPath string
		kind       string
	}
	expectedEntries := []importEntry{
		{file: "main.ts", importPath: "./static_target", kind: "static"},
		{file: "main.ts", importPath: "./dynamic_target", kind: "dynamic"},
		{file: "main.ts", importPath: "./require_target", kind: "require"},
		{file: "main.ts", importPath: "./side_effect", kind: "side-effect"},
		{file: "main.ts", importPath: "./type_target", kind: "type"},
		{file: "reexport_source.ts", importPath: "./reexport_target", kind: "reexport"},
	}

	// Build lookup.
	type matchKey struct {
		file       string
		importPath string
	}
	gotKinds := make(map[matchKey]string)
	for _, m := range matches {
		rel, err := filepath.Rel(dir, m.SourceFile)
		if err != nil {
			t.Fatalf("could not make relative path: %v", err)
		}
		rel = filepath.ToSlash(rel)
		gotKinds[matchKey{file: rel, importPath: m.ImportPath}] = m.Kind
	}

	for _, e := range expectedEntries {
		key := matchKey{file: e.file, importPath: e.importPath}
		gotKind, ok := gotKinds[key]
		if !ok {
			t.Errorf("expected %s to import %q (kind=%s), but import not found", e.file, e.importPath, e.kind)
			continue
		}
		if gotKind != e.kind {
			t.Errorf("import %s -> %q: expected kind %q, got %q", e.file, e.importPath, e.kind, gotKind)
		}
	}
}

func TestSideEffectImport(t *testing.T) {
	requireAstGrep(t)
	dir := fixtureDir(t, "all-import-styles")

	sc := NewTypeScriptScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	found := false
	for _, m := range matches {
		if m.ImportPath == "./side_effect" {
			if m.Kind != "side-effect" {
				t.Errorf("side-effect import has kind %q, expected %q", m.Kind, "side-effect")
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("side-effect import ./side_effect not found in scan results")
	}
}

func TestTypeImport(t *testing.T) {
	requireAstGrep(t)
	dir := fixtureDir(t, "all-import-styles")

	sc := NewTypeScriptScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	found := false
	for _, m := range matches {
		if m.ImportPath == "./type_target" {
			if m.Kind != "type" {
				t.Errorf("type import has kind %q, expected %q", m.Kind, "type")
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("type import ./type_target not found in scan results")
	}
}

func TestMultipleKindsSamePath(t *testing.T) {
	requireAstGrep(t)
	dir := fixtureDir(t, "merged-imports")

	sc := NewTypeScriptScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// consumer.ts imports ./shared with both static and type kinds.
	// With the updated dedupe key (file, importPath, kind), both should be returned.
	type matchKey struct {
		file       string
		importPath string
		kind       string
	}
	got := make(map[matchKey]bool)
	for _, m := range matches {
		rel, err := filepath.Rel(dir, m.SourceFile)
		if err != nil {
			t.Fatalf("could not make relative path: %v", err)
		}
		rel = filepath.ToSlash(rel)
		got[matchKey{file: rel, importPath: m.ImportPath, kind: m.Kind}] = true
	}

	staticKey := matchKey{file: "consumer.ts", importPath: "./shared", kind: "static"}
	typeKey := matchKey{file: "consumer.ts", importPath: "./shared", kind: "type"}

	if !got[staticKey] {
		t.Errorf("expected static import of ./shared from consumer.ts, got matches: %v", got)
	}
	if !got[typeKey] {
		t.Errorf("expected type import of ./shared from consumer.ts, got matches: %v", got)
	}
}

func TestReexport(t *testing.T) {
	requireAstGrep(t)
	dir := fixtureDir(t, "all-import-styles")

	sc := NewTypeScriptScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	found := false
	for _, m := range matches {
		rel, err := filepath.Rel(dir, m.SourceFile)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == "reexport_source.ts" && m.ImportPath == "./reexport_target" {
			if m.Kind != "reexport" {
				t.Errorf("re-export has kind %q, expected %q", m.Kind, "reexport")
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("re-export ./reexport_target not found in scan results from reexport_source.ts")
	}
}

func TestReexportVariants(t *testing.T) {
	requireAstGrep(t)
	dir := fixtureDir(t, "alias-reexports")

	sc := NewTypeScriptScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Build lookup keyed by (file, importPath) -> kind.
	type matchKey struct {
		file       string
		importPath string
	}
	gotKinds := make(map[matchKey]string)
	for _, m := range matches {
		rel, err := filepath.Rel(dir, m.SourceFile)
		if err != nil {
			t.Fatalf("could not make relative path: %v", err)
		}
		rel = filepath.ToSlash(rel)
		gotKinds[matchKey{file: rel, importPath: m.ImportPath}] = m.Kind
	}

	type importEntry struct {
		file       string
		importPath string
		kind       string
	}
	expectedEntries := []importEntry{
		// Re-exports from the barrel file
		{file: "src/client/my-feature/index.ts", importPath: "./Widget", kind: "reexport"},
		{file: "src/client/my-feature/index.ts", importPath: "./widget.utils", kind: "reexport"},
		// Imports from dashboard.tsx
		{file: "src/routes/dashboard.tsx", importPath: "@/utils/format", kind: "static"},
		{file: "src/routes/dashboard.tsx", importPath: "../components/Header", kind: "static"},
		{file: "src/routes/dashboard.tsx", importPath: "@/services/heavy-service", kind: "dynamic"},
		{file: "src/routes/dashboard.tsx", importPath: "@/does-not-exist", kind: "dynamic"},
	}

	for _, e := range expectedEntries {
		key := matchKey{file: e.file, importPath: e.importPath}
		gotKind, ok := gotKinds[key]
		if !ok {
			t.Errorf("expected %s to import %q (kind=%s), but import not found", e.file, e.importPath, e.kind)
			continue
		}
		if gotKind != e.kind {
			t.Errorf("import %s -> %q: expected kind %q, got %q", e.file, e.importPath, e.kind, gotKind)
		}
	}
}

func TestReexportSingleQuotes(t *testing.T) {
	requireAstGrep(t)
	dir := fixtureDir(t, "single-quote-reexports")

	sc := NewTypeScriptScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	type importEntry struct {
		file       string
		importPath string
		kind       string
	}
	expectedEntries := []importEntry{
		{file: "index.ts", importPath: "./Widget", kind: "reexport"},
		{file: "index.ts", importPath: "./types", kind: "reexport"},
		{file: "index.ts", importPath: "./widget-utils", kind: "reexport"},
	}

	type matchKey struct {
		file       string
		importPath string
	}
	gotKinds := make(map[matchKey]string)
	for _, m := range matches {
		rel, err := filepath.Rel(dir, m.SourceFile)
		if err != nil {
			t.Fatalf("could not make relative path: %v", err)
		}
		rel = filepath.ToSlash(rel)
		gotKinds[matchKey{file: rel, importPath: m.ImportPath}] = m.Kind
	}

	for _, e := range expectedEntries {
		key := matchKey{file: e.file, importPath: e.importPath}
		gotKind, ok := gotKinds[key]
		if !ok {
			t.Errorf("expected %s to import %q (kind=%s), but import not found", e.file, e.importPath, e.kind)
			continue
		}
		if gotKind != e.kind {
			t.Errorf("import %s -> %q: expected kind %q, got %q", e.file, e.importPath, e.kind, gotKind)
		}
	}
}

func TestSideEffectSingleQuotes(t *testing.T) {
	requireAstGrep(t)

	dir := t.TempDir()

	// Write a minimal tsconfig.json.
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a file with a single-quoted side-effect import.
	if err := os.WriteFile(filepath.Join(dir, "app.ts"), []byte("import './side-effect'\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write the target so ast-grep has something to scan.
	if err := os.WriteFile(filepath.Join(dir, "side-effect.ts"), []byte("console.log('init')\n"), 0644); err != nil {
		t.Fatal(err)
	}

	sc := NewTypeScriptScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	found := false
	for _, m := range matches {
		if m.ImportPath == "./side-effect" && m.Kind == "side-effect" {
			found = true
			break
		}
	}
	if !found {
		t.Error("single-quoted side-effect import './side-effect' not found in scan results")
		for _, m := range matches {
			t.Logf("  match: file=%s importPath=%s kind=%s", m.SourceFile, m.ImportPath, m.Kind)
		}
	}
}
