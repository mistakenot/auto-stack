package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-graph/internal/codegraph"
	"github.com/mistakenot/auto-graph/internal/config"
	"github.com/mistakenot/auto-graph/internal/doclink"
	"github.com/mistakenot/auto-graph/internal/format"
	"github.com/spf13/cobra"
)

func newCodeGraphCmd() *cobra.Command {
	var formatFlag string
	var langFlag string
	var noDocs bool

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
			return runCodeGraph(cmd, args[0], formatFlag, langFlag, noDocs)
		},
	}

	cmd.Flags().StringVar(&formatFlag, "format", defaultFormat, "output format: json, dot, mermaid")
	cmd.Flags().StringVar(&langFlag, "lang", "", "language override (auto-detected from config files if omitted)")
	cmd.Flags().BoolVar(&noDocs, "no-docs", false, "exclude documentation links from graph")

	return cmd
}

func runCodeGraph(cmd *cobra.Command, dir, formatFlag, langFlag string, noDocs bool) error {
	// Resolve the project directory to an absolute path.
	projectRoot, err := filepath.Abs(dir)
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("resolving directory: %w", err)}
	}

	info, err := os.Stat(projectRoot)
	if err != nil || !info.IsDir() {
		return &ExitError{Code: 1, Err: fmt.Errorf("not a directory: %s", dir)}
	}

	// Detect language.
	lang := langFlag
	if lang == "" {
		detected, err := codegraph.DetectLanguage(projectRoot)
		if err != nil {
			return &ExitError{Code: 1, Err: err}
		}
		lang = detected
	}

	// Build the graph.
	g, diags, err := codegraph.Build(projectRoot, lang, cmd.ErrOrStderr())
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	// Print diagnostics for unresolved alias imports.
	errW := cmd.ErrOrStderr()
	for _, d := range diags {
		fmt.Fprintf(errW, "warning: %s:%d: unresolved alias import %q (check compilerOptions.paths and baseUrl in tsconfig.json)\n", d.Source, d.Line, d.Raw)
	}

	// Enrich graph with documentation links unless --no-docs is set.
	if !noDocs {
		links, err := doclink.Scan(projectRoot, cmd.ErrOrStderr())
		if err != nil {
			return &ExitError{Code: 1, Err: fmt.Errorf("scanning doc links: %w", err)}
		}
		doclink.Enrich(g, links)
	}

	// Output in requested format.
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
