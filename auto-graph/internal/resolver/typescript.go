package resolver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// tsconfig represents the subset of tsconfig.json we need for resolution.
type tsconfig struct {
	CompilerOptions struct {
		BaseURL string              `json:"baseUrl"`
		Paths   map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
}

// pathMapping is a parsed tsconfig path alias with prefix/suffix split on *.
type pathMapping struct {
	prefix  string   // pattern text before the wildcard (e.g. "@/")
	targets []string // replacement prefixes (text before * in each target)
}

// probeExtensions is the ordered list of extensions to try when resolving
// a path that does not have one. Index variants are tried after file variants.
var probeExtensions = []string{
	"",          // exact path
	".ts",       // TypeScript
	".tsx",      // TypeScript JSX
	".js",       // JavaScript
	".jsx",      // JavaScript JSX
	"/index.ts", // directory with index
	"/index.tsx",
	"/index.js",
	"/index.jsx",
}

// importKind classifies the type of an import specifier.
type importKind int

const (
	importRelative importKind = iota
	importAlias
	importBare
)

// TypeScriptResolver resolves TypeScript/JavaScript import paths using
// tsconfig.json path aliases and file extension probing.
type TypeScriptResolver struct {
	// mappings are the parsed tsconfig path aliases, longest-prefix-first.
	mappings []pathMapping
	// baseURL is the directory that non-relative alias targets resolve against.
	// Empty string means project root.
	baseURL string
	// loaded tracks whether tsconfig was successfully parsed.
	loaded bool
}

// NewTypeScriptResolver creates a resolver that reads tsconfig.json from the
// given project root. If tsconfig.json is missing or unparseable, the resolver
// still works — it just skips alias substitution.
func NewTypeScriptResolver(projectRoot string) *TypeScriptResolver {
	r := &TypeScriptResolver{}
	r.loadTSConfig(projectRoot)
	return r
}

// loadTSConfig reads and parses tsconfig.json from the project root.
func (r *TypeScriptResolver) loadTSConfig(projectRoot string) {
	data, err := os.ReadFile(filepath.Join(projectRoot, "tsconfig.json"))
	if err != nil {
		return
	}

	var cfg tsconfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}

	r.baseURL = cfg.CompilerOptions.BaseURL
	r.loaded = true

	// Parse path mappings. Each key like "@/*" maps to targets like ["src/*"].
	// We strip the trailing "/*" (or "*") to get prefix strings for matching.
	for pattern, targets := range cfg.CompilerOptions.Paths {
		pm := pathMapping{}

		// Strip the wildcard from the pattern.
		if idx := strings.Index(pattern, "*"); idx >= 0 {
			pm.prefix = pattern[:idx]
		} else {
			// Exact match (no wildcard) — use the full pattern as prefix.
			pm.prefix = pattern
		}

		// Strip the wildcard from each target.
		for _, t := range targets {
			if idx := strings.Index(t, "*"); idx >= 0 {
				pm.targets = append(pm.targets, t[:idx])
			} else {
				pm.targets = append(pm.targets, t)
			}
		}

		if len(pm.targets) > 0 {
			r.mappings = append(r.mappings, pm)
		}
	}

	// Sort mappings longest-prefix-first so "@utils/" matches before "@/".
	sortMappingsByPrefixLength(r.mappings)
}

// sortMappingsByPrefixLength sorts mappings in-place, longest prefix first.
// Uses insertion sort since the list is typically very small (< 10 items).
func sortMappingsByPrefixLength(m []pathMapping) {
	for i := 1; i < len(m); i++ {
		for j := i; j > 0 && len(m[j].prefix) > len(m[j-1].prefix); j-- {
			m[j], m[j-1] = m[j-1], m[j]
		}
	}
}

// Resolve maps a raw import path to a resolved file path relative to the
// project root, or marks it as external.
func (r *TypeScriptResolver) Resolve(importPath, sourceFile, projectRoot string) (ResolveResult, error) {
	kind := r.classifyImport(importPath)

	switch kind {
	case importBare:
		return ResolveResult{IsExternal: true}, nil

	case importAlias:
		resolved := r.resolveAlias(importPath, projectRoot)
		if resolved != "" {
			return ResolveResult{ResolvedPath: resolved}, nil
		}
		// Alias didn't resolve to a file — treat as unresolved.
		return ResolveResult{}, nil

	case importRelative:
		resolved := r.resolveRelative(importPath, sourceFile, projectRoot)
		if resolved != "" {
			return ResolveResult{ResolvedPath: resolved}, nil
		}
		return ResolveResult{}, nil
	}

	return ResolveResult{}, nil
}

// classifyImport determines whether an import is relative, an alias, or bare.
func (r *TypeScriptResolver) classifyImport(importPath string) importKind {
	if strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../") {
		return importRelative
	}

	// Check if it matches any tsconfig path alias.
	for _, m := range r.mappings {
		if strings.HasPrefix(importPath, m.prefix) {
			return importAlias
		}
	}

	return importBare
}

// resolveAlias substitutes tsconfig path aliases and probes for a file.
func (r *TypeScriptResolver) resolveAlias(importPath, projectRoot string) string {
	for _, m := range r.mappings {
		if !strings.HasPrefix(importPath, m.prefix) {
			continue
		}

		// The portion after the alias prefix (the wildcard capture).
		rest := importPath[len(m.prefix):]

		for _, target := range m.targets {
			// Build the substituted path: target prefix + wildcard rest.
			candidate := target + rest

			// Resolve relative to baseUrl (which itself is relative to project root).
			var baseDir string
			if r.baseURL != "" {
				baseDir = filepath.Join(projectRoot, r.baseURL)
			} else {
				baseDir = projectRoot
			}

			absCandidate := filepath.Join(baseDir, candidate)
			resolved := probeFile(absCandidate)
			if resolved != "" {
				rel, err := filepath.Rel(projectRoot, resolved)
				if err != nil {
					continue
				}
				return filepath.ToSlash(rel)
			}
		}
	}
	return ""
}

// resolveRelative resolves a relative import against the source file's directory.
func (r *TypeScriptResolver) resolveRelative(importPath, sourceFile, projectRoot string) string {
	sourceDir := filepath.Dir(sourceFile)
	absCandidate := filepath.Join(sourceDir, importPath)
	resolved := probeFile(absCandidate)
	if resolved != "" {
		rel, err := filepath.Rel(projectRoot, resolved)
		if err != nil {
			return ""
		}
		return filepath.ToSlash(rel)
	}
	return ""
}

// probeFile tries each extension variant and returns the first path that exists.
func probeFile(basePath string) string {
	for _, ext := range probeExtensions {
		candidate := basePath + ext
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
