package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mistakenot/auto-graph/internal/config"
	"github.com/mistakenot/auto-graph/internal/format"
	"github.com/mistakenot/auto-graph/internal/graph"
	"github.com/mistakenot/auto-graph/internal/resolver"
	"github.com/mistakenot/auto-graph/internal/scanner"
	"github.com/spf13/cobra"
)

func newCodeGraphCmd() *cobra.Command {
	var formatFlag string
	var langFlag string

	defaultFormat := "json"
	if path, err := config.GraphSettingsPath(); err == nil {
		if cfg, err := config.LoadGraphSettings(path); err == nil {
			defaultFormat = cfg.DefaultOutput
		}
	}

	cmd := &cobra.Command{
		Use:   "graph <dir>",
		Short: "Build a file-level import graph for a project directory",
		Long: `Build a file-level import graph by scanning source files for import
statements and resolving them to actual files. Outputs the graph in
JSON (default), Graphviz DOT, or Mermaid format.

The language is auto-detected from config files in the target directory
(e.g. tsconfig.json for TypeScript). Use --lang to override detection.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeGraph(cmd, args[0], formatFlag, langFlag)
		},
	}

	cmd.Flags().StringVar(&formatFlag, "format", defaultFormat, "output format: json, dot, mermaid")
	cmd.Flags().StringVar(&langFlag, "lang", "", "language override (auto-detected from config files if omitted)")

	return cmd
}

func runCodeGraph(cmd *cobra.Command, dir, formatFlag, langFlag string) error {
	// Resolve the project directory to an absolute path.
	projectRoot, err := filepath.Abs(dir)
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("resolving directory: %w", err)}
	}

	info, err := os.Stat(projectRoot)
	if err != nil || !info.IsDir() {
		return &ExitError{Code: 1, Err: fmt.Errorf("not a directory: %s", dir)}
	}

	// Step 1: Detect language.
	lang := langFlag
	if lang == "" {
		detected, err := detectLanguage(projectRoot)
		if err != nil {
			return &ExitError{Code: 1, Err: err}
		}
		lang = detected
	}

	// Step 2: Create language-specific scanner and resolver.
	var sc scanner.Scanner
	var res resolver.Resolver

	switch lang {
	case "typescript":
		if _, err := exec.LookPath("ast-grep"); err != nil {
			return &ExitError{
				Code: 1,
				Err:  fmt.Errorf("ast-grep not found: install with npm i -g @ast-grep/cli or brew install ast-grep"),
			}
		}
		sc = scanner.NewTypeScriptScanner()
		res = resolver.NewTypeScriptResolver(projectRoot)
	case "go":
		sc = scanner.NewGoScanner()
		var goErr error
		res, goErr = resolver.NewGoResolver(projectRoot)
		if goErr != nil {
			return &ExitError{Code: 1, Err: fmt.Errorf("initializing Go resolver: %w", goErr)}
		}
	default:
		return &ExitError{
			Code: 1,
			Err:  fmt.Errorf("unsupported language %q; supported: typescript, go", lang),
		}
	}

	// Step 3: Walk the filesystem for source files to discover all nodes.
	filePaths, err := discoverFiles(projectRoot, lang)
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("discovering files: %w", err)}
	}

	// Step 4: Run the scanner to get import matches.
	matches, err := sc.Scan(projectRoot)
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("scanning imports: %w", err)}
	}

	// Step 5: Build the graph.
	g := buildGraph(projectRoot, filePaths, matches, res, lang)

	// Step 6: Output in requested format.
	w := cmd.OutOrStdout()
	switch formatFlag {
	case "json":
		if err := format.WriteJSON(w, g); err != nil {
			return &ExitError{Code: 1, Err: fmt.Errorf("writing JSON: %w", err)}
		}
	case "dot":
		if err := format.WriteDOT(w, g); err != nil {
			return &ExitError{Code: 1, Err: fmt.Errorf("writing DOT: %w", err)}
		}
	case "mermaid":
		if err := format.WriteMermaid(w, g); err != nil {
			return &ExitError{Code: 1, Err: fmt.Errorf("writing Mermaid: %w", err)}
		}
	default:
		return &ExitError{
			Code: 1,
			Err:  fmt.Errorf("unknown format %q; supported formats: json, dot, mermaid", formatFlag),
		}
	}

	return nil
}

// detectLanguage auto-detects the project language from config files present
// in the project directory.
func detectLanguage(projectRoot string) (string, error) {
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

// discoverFiles walks the project directory and returns relative paths for all
// source files matching the given language.
func discoverFiles(projectRoot, lang string) ([]string, error) {
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

		// Skip hidden directories and language-specific dependency dirs.
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
func buildGraph(projectRoot string, filePaths []string, matches []scanner.ImportMatch, res resolver.Resolver, lang string) *graph.Graph {
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

	// Create edges from resolved imports.
	type edgeKey struct {
		source string
		target string
	}
	edgeSeen := make(map[edgeKey]bool)

	for _, m := range matches {
		result, err := res.Resolve(m.ImportPath, m.SourceFile, projectRoot)
		if err != nil {
			continue
		}

		// Skip external (node_modules / stdlib / third-party) and unresolved imports.
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

		// Determine target files: direct file match or package directory expansion.
		var targetFiles []string
		if nodeSet[targetRel] {
			targetFiles = []string{targetRel}
		} else if files, ok := dirToFiles[targetRel]; ok {
			targetFiles = files
		}

		for _, targetFile := range targetFiles {
			// Skip self-imports.
			if sourceRel == targetFile {
				continue
			}

			// Deduplicate edges.
			key := edgeKey{source: sourceRel, target: targetFile}
			if edgeSeen[key] {
				continue
			}
			edgeSeen[key] = true

			g.Edges = append(g.Edges, graph.Edge{
				Source: sourceRel,
				Target: targetFile,
				Kind:   graph.EdgeImport,
				Attrs: map[string]string{
					"import_kind": m.Kind,
					"raw":         m.ImportPath,
				},
			})
		}
	}

	return g
}
