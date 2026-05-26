package codegraph

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mistakenot/auto-graph/internal/graph"
	"github.com/mistakenot/auto-graph/internal/resolver"
	"github.com/mistakenot/auto-graph/internal/scanner"
)

// Build constructs a file-level import graph for the given project root and
// language. It discovers files, scans imports via ast-grep, resolves import
// paths, and returns the resulting graph with merged import metadata.
func Build(projectRoot, lang string) (*graph.Graph, error) {
	// Check ast-grep is installed.
	if _, err := exec.LookPath("ast-grep"); err != nil {
		return nil, fmt.Errorf("ast-grep not found: install with npm i -g @ast-grep/cli or brew install ast-grep")
	}

	if lang != "typescript" {
		return nil, fmt.Errorf("unsupported language %q; currently only typescript is supported", lang)
	}

	// Discover source files.
	filePaths, err := DiscoverFiles(projectRoot, lang)
	if err != nil {
		return nil, fmt.Errorf("discovering files: %w", err)
	}

	// Run the scanner.
	sc := scanner.NewTypeScriptScanner()
	matches, err := sc.Scan(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("scanning imports: %w", err)
	}

	// Create the resolver.
	res := resolver.NewTypeScriptResolver(projectRoot)

	// Build the graph with merged import metadata.
	g := buildGraph(projectRoot, filePaths, matches, res)
	return g, nil
}

// DetectLanguage auto-detects the project language from config files present
// in the project directory.
func DetectLanguage(projectRoot string) (string, error) {
	if _, err := os.Stat(filepath.Join(projectRoot, "tsconfig.json")); err == nil {
		return "typescript", nil
	}

	return "", fmt.Errorf(
		"could not detect project language: no tsconfig.json found in %s; use --lang=typescript to specify explicitly",
		projectRoot,
	)
}

// DiscoverFiles walks the project directory and returns relative paths for all
// source files matching the given language.
func DiscoverFiles(projectRoot, lang string) ([]string, error) {
	var extensions map[string]bool

	switch lang {
	case "typescript":
		extensions = map[string]bool{
			".ts":  true,
			".tsx": true,
		}
	default:
		return nil, fmt.Errorf("no file extensions defined for language %q", lang)
	}

	var paths []string

	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden directories and node_modules.
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		if extensions[ext] {
			rel, err := filepath.Rel(projectRoot, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(rel))
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Strings(paths)
	return paths, nil
}

// buildGraph constructs a Graph from discovered files and resolved imports.
// It merges duplicate (source, target) edges by combining import metadata.
func buildGraph(projectRoot string, filePaths []string, matches []scanner.ImportMatch, res resolver.Resolver) *graph.Graph {
	g := &graph.Graph{
		Root: projectRoot,
	}

	// Create nodes from the file walk.
	nodeSet := make(map[string]bool)
	for _, p := range filePaths {
		nodeSet[p] = true
		g.Nodes = append(g.Nodes, graph.Node{
			ID:       p,
			Kind:     graph.NodeFile,
			Path:     p,
			Language: "typescript",
		})
	}

	// Create edges from resolved imports, merging duplicate (source, target) pairs.
	type edgeKey struct {
		source string
		target string
	}
	type edgeData struct {
		kinds []string
		raws  []string
	}
	edgeMap := make(map[edgeKey]*edgeData)
	var edgeOrder []edgeKey

	for _, m := range matches {
		result, err := res.Resolve(m.ImportPath, m.SourceFile, projectRoot)
		if err != nil {
			continue
		}

		// Skip external (node_modules) and unresolved imports.
		if result.IsExternal || result.ResolvedPath == "" {
			continue
		}

		// Compute relative path for the source file.
		sourceRel, err := filepath.Rel(projectRoot, m.SourceFile)
		if err != nil {
			continue
		}
		sourceRel = filepath.ToSlash(sourceRel)

		targetRel := result.ResolvedPath

		// Skip self-imports.
		if sourceRel == targetRel {
			continue
		}

		// Skip if source or target are not in our discovered node set.
		if !nodeSet[sourceRel] || !nodeSet[targetRel] {
			continue
		}

		key := edgeKey{source: sourceRel, target: targetRel}
		data, exists := edgeMap[key]
		if !exists {
			data = &edgeData{}
			edgeMap[key] = data
			edgeOrder = append(edgeOrder, key)
		}

		// Add kind if not already present.
		if !containsString(data.kinds, m.Kind) {
			data.kinds = append(data.kinds, m.Kind)
		}

		// Add raw import path if not already present.
		if !containsString(data.raws, m.ImportPath) {
			data.raws = append(data.raws, m.ImportPath)
		}
	}

	// Emit edges in stable insertion order with merged attrs.
	for _, key := range edgeOrder {
		data := edgeMap[key]

		// Sort kinds and raws for stable output.
		sortedKinds := make([]string, len(data.kinds))
		copy(sortedKinds, data.kinds)
		sort.Strings(sortedKinds)

		sortedRaws := make([]string, len(data.raws))
		copy(sortedRaws, data.raws)
		sort.Strings(sortedRaws)

		attrs := map[string]string{
			"import_kind":  data.kinds[0], // primary: first encountered
			"import_kinds": strings.Join(sortedKinds, ","),
			"raw":          data.raws[0], // primary: first encountered
			"raws":         strings.Join(sortedRaws, ","),
		}

		g.Edges = append(g.Edges, graph.Edge{
			Source: key.source,
			Target: key.target,
			Kind:   graph.EdgeImport,
			Attrs:  attrs,
		})
	}

	return g
}

// containsString checks if a slice already contains a given string.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
