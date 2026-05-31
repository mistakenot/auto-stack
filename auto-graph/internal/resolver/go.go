package resolver

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GoResolver resolves Go import paths using the module path from go.mod.
// Same-module imports are resolved to relative package directories; stdlib
// and third-party imports are classified as external.
type GoResolver struct {
	modulePath string
}

// NewGoResolver creates a resolver by reading the module path from go.mod
// in the given project root. Returns an error if go.mod is missing or does
// not contain a module directive.
func NewGoResolver(projectRoot string) (*GoResolver, error) {
	modPath := filepath.Join(projectRoot, "go.mod")
	f, err := os.Open(modPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open go.mod: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			modulePath := strings.TrimSpace(rest)
			if idx := strings.Index(modulePath, "//"); idx >= 0 {
				modulePath = strings.TrimSpace(modulePath[:idx])
			}
			if len(modulePath) > 0 && modulePath[0] == '"' {
				if unquoted, err := strconv.Unquote(modulePath); err == nil {
					modulePath = unquoted
				}
			}
			if modulePath == "" {
				continue
			}
			return &GoResolver{modulePath: modulePath}, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading go.mod: %w", err)
	}

	return nil, fmt.Errorf("no module directive found in %s", modPath)
}

// Resolve maps a Go import path to a resolved package directory relative to
// the project root. Same-module imports resolve to the relative directory;
// stdlib and external imports are marked as external.
func (r *GoResolver) Resolve(importPath, sourceFile, projectRoot string) (ResolveResult, error) {
	// Same-module check: exact match or prefix with "/" boundary.
	if importPath == r.modulePath {
		return ResolveResult{ResolvedPath: "."}, nil
	}
	if relPkgDir, ok := strings.CutPrefix(importPath, r.modulePath+"/"); ok {
		return ResolveResult{ResolvedPath: relPkgDir}, nil
	}

	// Everything else (stdlib or external) is external.
	// Stdlib detection: first path segment has no dot (e.g. "fmt", "net/http").
	// External: first segment has a dot (e.g. "github.com/other/pkg").
	// Both are treated as external for graph purposes.
	return ResolveResult{IsExternal: true}, nil
}
