package codegraph

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mistakenot/auto-graph/internal/graph"
	"github.com/mistakenot/auto-graph/internal/resolver"
	"github.com/mistakenot/auto-graph/internal/scanner"
)

type Diagnostic struct {
	Source string
	Line   int
	Raw    string
}

// Build constructs a file-level import graph for the given project root and
// language. It discovers files, scans imports, resolves import paths, and
// returns the resulting graph with merged import metadata. Diagnostics are
// returned for imports that matched an alias but could not be resolved.
func Build(projectRoot, lang string, warn io.Writer) (*graph.Graph, []Diagnostic, error) {
	var sc scanner.Scanner
	var res resolver.Resolver

	switch lang {
	case "typescript":
		if _, err := exec.LookPath("ast-grep"); err != nil {
			return nil, nil, fmt.Errorf("ast-grep not found: install with npm i -g @ast-grep/cli or brew install ast-grep")
		}
		sc = scanner.NewTypeScriptScanner()
		res = resolver.NewTypeScriptResolver(projectRoot, warn)
	case "go":
		sc = scanner.NewGoScanner()
		var goErr error
		res, goErr = resolver.NewGoResolver(projectRoot)
		if goErr != nil {
			return nil, nil, fmt.Errorf("initializing Go resolver: %w", goErr)
		}
	default:
		return nil, nil, fmt.Errorf("unsupported language %q; supported: typescript, go", lang)
	}

	filePaths, err := DiscoverFiles(projectRoot, lang)
	if err != nil {
		return nil, nil, fmt.Errorf("discovering files: %w", err)
	}

	matches, err := sc.Scan(projectRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("scanning imports: %w", err)
	}

	g, diags := buildGraph(projectRoot, filePaths, matches, res, lang)
	return g, diags, nil
}

// DetectLanguage auto-detects the project language from config files present
// in the project directory.
func DetectLanguage(projectRoot string) (string, error) {
	hasGoMod := fileExists(filepath.Join(projectRoot, "go.mod"))
	hasTSConfig := fileExists(filepath.Join(projectRoot, "tsconfig.json"))

	if hasGoMod && hasTSConfig {
		return "", fmt.Errorf("ambiguous: both go.mod and tsconfig.json found in %s; use --lang=go or --lang=typescript", projectRoot)
	}
	if hasGoMod {
		return "go", nil
	}
	if hasTSConfig {
		return "typescript", nil
	}

	return "", fmt.Errorf(
		"could not detect project language: no go.mod or tsconfig.json found in %s; use --lang to specify explicitly",
		projectRoot,
	)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
	case "go":
		extensions = map[string]bool{
			".go": true,
		}
	default:
		return nil, fmt.Errorf("no file extensions defined for language %q", lang)
	}

	var paths []string

	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			if lang == "go" && (name == "testdata" || strings.HasPrefix(name, "_")) {
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
func buildGraph(projectRoot string, filePaths []string, matches []scanner.ImportMatch, res resolver.Resolver, lang string) (*graph.Graph, []Diagnostic) {
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
			Language: lang,
		})
	}

	// Build directory-to-files index for package-level resolution (Go).
	dirToFiles := make(map[string][]string)
	for _, p := range filePaths {
		dir := filepath.ToSlash(filepath.Dir(p))
		dirToFiles[dir] = append(dirToFiles[dir], p)
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
	var diags []Diagnostic

	for _, m := range matches {
		result, err := res.Resolve(m.ImportPath, m.SourceFile, projectRoot)
		if err != nil {
			continue
		}

		if result.MatchedAlias && result.ResolvedPath == "" {
			sourceRel, relErr := filepath.Rel(projectRoot, m.SourceFile)
			if relErr == nil {
				sourceRel = filepath.ToSlash(sourceRel)
			} else {
				sourceRel = m.SourceFile
			}
			diags = append(diags, Diagnostic{Source: sourceRel, Line: m.Line, Raw: m.ImportPath})
			continue
		}

		if result.IsExternal || result.ResolvedPath == "" {
			continue
		}

		sourceRel, err := filepath.Rel(projectRoot, m.SourceFile)
		if err != nil {
			continue
		}
		sourceRel = filepath.ToSlash(sourceRel)

		if !nodeSet[sourceRel] {
			continue
		}

		targetRel := result.ResolvedPath

		// Determine target files: direct file match or package directory expansion.
		var targetFiles []string
		if nodeSet[targetRel] {
			targetFiles = []string{targetRel}
		} else if files, ok := dirToFiles[targetRel]; ok {
			targetFiles = files
		}

		for _, targetFile := range targetFiles {
			if sourceRel == targetFile {
				continue
			}

			key := edgeKey{source: sourceRel, target: targetFile}
			data, exists := edgeMap[key]
			if !exists {
				data = &edgeData{}
				edgeMap[key] = data
				edgeOrder = append(edgeOrder, key)
			}

			canonKind := canonicalizeKind(m.Kind)
			if !containsString(data.kinds, canonKind) {
				data.kinds = append(data.kinds, canonKind)
			}

			if !containsString(data.raws, m.ImportPath) {
				data.raws = append(data.raws, m.ImportPath)
			}
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

	return g, diags
}

// canonicalizeKind normalizes scanner-emitted import kind strings to the
// canonical vocabulary expected by contextpack: type_only, side_effect,
// dynamic, reexport, static.
func canonicalizeKind(kind string) string {
	switch kind {
	case "type":
		return "type_only"
	case "side-effect", "blank":
		return "side_effect"
	default:
		return kind
	}
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
