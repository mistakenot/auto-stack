package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

// createTempGoMod creates a temporary directory with a go.mod containing
// the given module path. Returns the directory path.
func createTempGoMod(t *testing.T, modulePath string) string {
	t.Helper()
	dir := t.TempDir()
	content := "module " + modulePath + "\n\ngo 1.21\n"
	err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// createTempGoModRaw creates a temporary directory with a go.mod containing
// the given raw content. Returns the directory path.
func createTempGoModRaw(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNewGoResolver(t *testing.T) {
	dir := createTempGoMod(t, "github.com/example/myproject")

	r, err := NewGoResolver(dir)
	if err != nil {
		t.Fatalf("NewGoResolver failed: %v", err)
	}
	if r.modulePath != "github.com/example/myproject" {
		t.Errorf("expected module path %q, got %q", "github.com/example/myproject", r.modulePath)
	}
}

func TestNewGoResolverMissingGoMod(t *testing.T) {
	dir := t.TempDir() // no go.mod

	_, err := NewGoResolver(dir)
	if err == nil {
		t.Fatal("expected error for missing go.mod, got nil")
	}
}

func TestNewGoResolverNoModuleDirective(t *testing.T) {
	dir := t.TempDir()
	// go.mod with no module directive
	err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("go 1.21\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	_, err = NewGoResolver(dir)
	if err == nil {
		t.Fatal("expected error for missing module directive, got nil")
	}
}

func TestGoResolverSameModule(t *testing.T) {
	dir := createTempGoMod(t, "github.com/example/project")

	r, err := NewGoResolver(dir)
	if err != nil {
		t.Fatalf("NewGoResolver failed: %v", err)
	}

	tests := []struct {
		name       string
		importPath string
		wantPath   string
	}{
		{
			name:       "subpackage internal/pkg",
			importPath: "github.com/example/project/internal/pkg",
			wantPath:   "internal/pkg",
		},
		{
			name:       "subpackage cmd/server",
			importPath: "github.com/example/project/cmd/server",
			wantPath:   "cmd/server",
		},
		{
			name:       "deeply nested package",
			importPath: "github.com/example/project/internal/util/strings",
			wantPath:   "internal/util/strings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := r.Resolve(tt.importPath, filepath.Join(dir, "main.go"), dir)
			if err != nil {
				t.Fatalf("Resolve failed: %v", err)
			}
			if result.IsExternal {
				t.Error("expected internal import, got external")
			}
			if result.ResolvedPath != tt.wantPath {
				t.Errorf("expected resolved path %q, got %q", tt.wantPath, result.ResolvedPath)
			}
		})
	}
}

func TestGoResolverRootPackage(t *testing.T) {
	dir := createTempGoMod(t, "github.com/example/project")

	r, err := NewGoResolver(dir)
	if err != nil {
		t.Fatalf("NewGoResolver failed: %v", err)
	}

	// Importing the module root package itself
	result, err := r.Resolve("github.com/example/project", filepath.Join(dir, "cmd/main.go"), dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.IsExternal {
		t.Error("expected internal import, got external")
	}
	if result.ResolvedPath != "." {
		t.Errorf("expected resolved path %q, got %q", ".", result.ResolvedPath)
	}
}

func TestGoResolverPathBoundarySafety(t *testing.T) {
	// "modulex/pkg" must NOT be treated as same-module for "module"
	dir := createTempGoMod(t, "github.com/example/project")

	r, err := NewGoResolver(dir)
	if err != nil {
		t.Fatalf("NewGoResolver failed: %v", err)
	}

	// "github.com/example/projectx/pkg" has the module path as a prefix
	// in string terms, but NOT at a "/" boundary — must be external.
	result, err := r.Resolve("github.com/example/projectx/pkg", filepath.Join(dir, "main.go"), dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if !result.IsExternal {
		t.Error("expected external import for path that shares prefix but not boundary")
	}
}

func TestGoResolverStdlib(t *testing.T) {
	dir := createTempGoMod(t, "github.com/example/project")

	r, err := NewGoResolver(dir)
	if err != nil {
		t.Fatalf("NewGoResolver failed: %v", err)
	}

	stdlibPaths := []string{"fmt", "net/http", "encoding/json", "os", "io/fs"}
	for _, importPath := range stdlibPaths {
		t.Run(importPath, func(t *testing.T) {
			result, err := r.Resolve(importPath, filepath.Join(dir, "main.go"), dir)
			if err != nil {
				t.Fatalf("Resolve failed: %v", err)
			}
			if !result.IsExternal {
				t.Errorf("expected stdlib import %q to be external", importPath)
			}
		})
	}
}

func TestGoResolverExternalPackages(t *testing.T) {
	dir := createTempGoMod(t, "github.com/example/project")

	r, err := NewGoResolver(dir)
	if err != nil {
		t.Fatalf("NewGoResolver failed: %v", err)
	}

	externalPaths := []string{
		"github.com/other/pkg",
		"github.com/spf13/cobra",
		"golang.org/x/tools/go/ast",
		"google.golang.org/grpc",
	}
	for _, importPath := range externalPaths {
		t.Run(importPath, func(t *testing.T) {
			result, err := r.Resolve(importPath, filepath.Join(dir, "main.go"), dir)
			if err != nil {
				t.Fatalf("Resolve failed: %v", err)
			}
			if !result.IsExternal {
				t.Errorf("expected external import %q to be external", importPath)
			}
		})
	}
}

func TestGoResolverModuleWithInlineComment(t *testing.T) {
	dir := createTempGoModRaw(t, "module github.com/example/foo // my project\n\ngo 1.21\n")

	r, err := NewGoResolver(dir)
	if err != nil {
		t.Fatalf("NewGoResolver failed: %v", err)
	}
	if r.modulePath != "github.com/example/foo" {
		t.Errorf("expected module path %q, got %q", "github.com/example/foo", r.modulePath)
	}

	result, err := r.Resolve("github.com/example/foo/internal/bar", filepath.Join(dir, "main.go"), dir)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.IsExternal {
		t.Error("expected internal import, got external")
	}
	if result.ResolvedPath != "internal/bar" {
		t.Errorf("expected resolved path %q, got %q", "internal/bar", result.ResolvedPath)
	}
}

func TestGoResolverModuleWithQuotedPath(t *testing.T) {
	dir := createTempGoModRaw(t, "module \"github.com/example/foo\"\n\ngo 1.21\n")

	r, err := NewGoResolver(dir)
	if err != nil {
		t.Fatalf("NewGoResolver failed: %v", err)
	}
	if r.modulePath != "github.com/example/foo" {
		t.Errorf("expected module path %q, got %q", "github.com/example/foo", r.modulePath)
	}
}
