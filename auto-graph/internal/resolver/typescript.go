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
	prefix      string          // pattern text before the wildcard (e.g. "@/")
	suffix      string          // pattern text after the wildcard (e.g. ""), empty for most cases
	hasWildcard bool            // true if pattern contains *, false for exact mappings
	targets     []targetMapping // replacement templates
}

// targetMapping is a parsed target replacement template.
type targetMapping struct {
	prefix string // target text before * (e.g. "./src/")
	suffix string // target text after * (e.g. "")
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

	// Parse path mappings. Each key like "@/*" maps to targets like ["./src/*"].
	// We split on the wildcard to get prefix/suffix for matching and substitution.
	for pattern, targets := range cfg.CompilerOptions.Paths {
		pm := pathMapping{}

		if idx := strings.Index(pattern, "*"); idx >= 0 {
			pm.prefix = pattern[:idx]
			pm.suffix = pattern[idx+1:]
			pm.hasWildcard = true
		} else {
			// Exact match (no wildcard) — use the full pattern as prefix.
			pm.prefix = pattern
			pm.suffix = ""
			pm.hasWildcard = false
		}

		for _, t := range targets {
			var tm targetMapping
			if idx := strings.Index(t, "*"); idx >= 0 {
				tm.prefix = t[:idx]
				tm.suffix = t[idx+1:]
			} else {
				tm.prefix = t
				tm.suffix = ""
			}
			pm.targets = append(pm.targets, tm)
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
	case importAlias:
		resolved := r.resolveAlias(importPath, projectRoot)
		if resolved != "" {
			return ResolveResult{ResolvedPath: resolved, MatchedAlias: true}, nil
		}
		// Alias matched but didn't resolve to a file — signal via MatchedAlias.
		return ResolveResult{MatchedAlias: true}, nil

	case importRelative:
		resolved := r.resolveRelative(importPath, sourceFile, projectRoot)
		if resolved != "" {
			return ResolveResult{ResolvedPath: resolved}, nil
		}
		return ResolveResult{}, nil

	case importBare:
		// Before classifying as external, probe baseUrl/importPath.
		if r.loaded && r.baseURL != "" {
			var baseDir string
			baseDir = filepath.Join(projectRoot, r.baseURL)
			absCandidate := filepath.Join(baseDir, importPath)
			resolved := probeFile(absCandidate)
			if resolved != "" {
				rel, err := filepath.Rel(projectRoot, resolved)
				if err == nil {
					return ResolveResult{ResolvedPath: filepath.ToSlash(rel)}, nil
				}
			}
		}
		return ResolveResult{IsExternal: true}, nil
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
		if matchesAlias(importPath, m) {
			return importAlias
		}
	}

	return importBare
}

// matchesAlias checks whether an import path matches a pathMapping.
// For wildcard patterns: checks prefix AND suffix match with content between them.
// For exact patterns: checks exact equality.
func matchesAlias(importPath string, m pathMapping) bool {
	if !m.hasWildcard {
		return importPath == m.prefix
	}
	// Wildcard: must start with prefix, end with suffix, and have content between.
	if !strings.HasPrefix(importPath, m.prefix) {
		return false
	}
	if !strings.HasSuffix(importPath, m.suffix) {
		return false
	}
	// Wildcard must capture one or more characters (TypeScript semantics).
	return len(importPath) > len(m.prefix)+len(m.suffix)
}

// wildcardCapture extracts the text matched by the wildcard in a pattern.
func wildcardCapture(importPath string, m pathMapping) string {
	return importPath[len(m.prefix) : len(importPath)-len(m.suffix)]
}

// resolveAlias substitutes tsconfig path aliases and probes for a file.
func (r *TypeScriptResolver) resolveAlias(importPath, projectRoot string) string {
	for _, m := range r.mappings {
		if !matchesAlias(importPath, m) {
			continue
		}

		// Extract the wildcard capture (or empty for exact mappings).
		var captured string
		if m.hasWildcard {
			captured = wildcardCapture(importPath, m)
		}

		for _, target := range m.targets {
			// Build the substituted path.
			var candidate string
			if m.hasWildcard {
				candidate = target.prefix + captured + target.suffix
			} else {
				candidate = target.prefix + target.suffix
			}

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
