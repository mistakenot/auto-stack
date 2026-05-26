package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestGoBasicImport(t *testing.T) {
	// Create a temp dir with a simple Go file.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import "fmt"

func main() { fmt.Println("hello") }
`)

	sc := NewGoScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	m := matches[0]
	if m.ImportPath != "fmt" {
		t.Errorf("expected import path %q, got %q", "fmt", m.ImportPath)
	}
	if m.Kind != "static" {
		t.Errorf("expected kind %q, got %q", "static", m.Kind)
	}
	if m.Line != 3 {
		t.Errorf("expected line 3, got %d", m.Line)
	}
	// SourceFile should be absolute.
	if !filepath.IsAbs(m.SourceFile) {
		t.Errorf("expected absolute SourceFile, got %q", m.SourceFile)
	}
}

func TestGoGroupedImports(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() { fmt.Println(os.Args, strings.Join(nil, "")) }
`)

	sc := NewGoScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}

	paths := make([]string, len(matches))
	for i, m := range matches {
		paths[i] = m.ImportPath
	}
	sort.Strings(paths)

	expected := []string{"fmt", "os", "strings"}
	for i, exp := range expected {
		if paths[i] != exp {
			t.Errorf("expected import %q at index %d, got %q", exp, i, paths[i])
		}
	}
}

func TestGoAllImportStyles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import "fmt"
import _ "net/http/pprof"
import . "math"
import myfmt "go/format"
import (
	"os"
	_ "image/png"
)
`)

	sc := NewGoScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	type expected struct {
		importPath string
		kind       string
	}

	expectedImports := []expected{
		{importPath: "fmt", kind: "static"},
		{importPath: "net/http/pprof", kind: "blank"},
		{importPath: "math", kind: "dot"},
		{importPath: "go/format", kind: "aliased"},
		{importPath: "os", kind: "static"},
		{importPath: "image/png", kind: "blank"},
	}

	if len(matches) != len(expectedImports) {
		t.Fatalf("expected %d matches, got %d", len(expectedImports), len(matches))
	}

	// Build a lookup by import path.
	gotKinds := make(map[string]string)
	for _, m := range matches {
		gotKinds[m.ImportPath] = m.Kind
	}

	for _, e := range expectedImports {
		got, ok := gotKinds[e.importPath]
		if !ok {
			t.Errorf("expected import %q not found", e.importPath)
			continue
		}
		if got != e.kind {
			t.Errorf("import %q: expected kind %q, got %q", e.importPath, e.kind, got)
		}
	}
}

func TestGoSkipDirectories(t *testing.T) {
	dir := t.TempDir()

	// Create files in directories that should be skipped.
	skipDirs := []string{"vendor", "testdata", ".hidden", "_ignored"}
	for _, d := range skipDirs {
		subDir := filepath.Join(dir, d)
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(subDir, "skip.go"), `package skip

import "fmt"

var _ = fmt.Println
`)
	}

	// Create a file in a non-skipped directory.
	keepDir := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(keepDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(keepDir, "keep.go"), `package pkg

import "os"

var _ = os.Args
`)

	sc := NewGoScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Only the file in pkg/ should be found.
	if len(matches) != 1 {
		t.Fatalf("expected 1 match (from pkg/), got %d", len(matches))
	}

	if matches[0].ImportPath != "os" {
		t.Errorf("expected import %q from kept file, got %q", "os", matches[0].ImportPath)
	}
}

func TestGoLineNumbers(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"fmt"
	"os"
)

func main() {}
`)

	sc := NewGoScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	// "fmt" is on line 4, "os" is on line 5.
	lineMap := make(map[string]int)
	for _, m := range matches {
		lineMap[m.ImportPath] = m.Line
	}

	if lineMap["fmt"] != 4 {
		t.Errorf("expected fmt on line 4, got line %d", lineMap["fmt"])
	}
	if lineMap["os"] != 5 {
		t.Errorf("expected os on line 5, got line %d", lineMap["os"])
	}
}

func TestGoEmptyDir(t *testing.T) {
	dir := t.TempDir()

	sc := NewGoScanner()
	matches, err := sc.Scan(dir)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(matches) != 0 {
		t.Errorf("expected 0 matches for empty dir, got %d", len(matches))
	}
}

// writeFile is a test helper that creates a file with the given content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
