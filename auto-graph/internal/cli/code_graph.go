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
	var strict bool

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
(e.g. tsconfig.json for TypeScript). Use --lang to override detection.

By default the graph is emitted even when some imports could not be resolved
(unresolved aliases, computed dynamic imports); those are reported as warnings
on stderr. Pass --strict to exit non-zero (code 3) when any such coverage gap
exists, so incomplete graphs cannot pass silently in automation.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeGraph(cmd, args[0], formatFlag, langFlag, noDocs, strict)
		},
	}

	cmd.Flags().StringVar(&formatFlag, "format", defaultFormat, "output format: json, dot, mermaid")
	cmd.Flags().StringVar(&langFlag, "lang", "", "language override (auto-detected from config files if omitted)")
	cmd.Flags().BoolVar(&noDocs, "no-docs", false, "exclude documentation links from graph")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit non-zero (code 3) if any import could not be resolved into the graph")

	return cmd
}

func runCodeGraph(cmd *cobra.Command, dir, formatFlag, langFlag string, noDocs, strict bool) error {
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

	// Report imports that produced no edge (coverage gaps) as warnings.
	printDiagnostics(cmd.ErrOrStderr(), diags)

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

	// In --strict mode a coverage gap is a failure: the valid partial graph has
	// already been written to stdout, but we exit non-zero so callers cannot
	// mistake an incomplete graph for a complete one.
	if strict && len(diags) > 0 {
		return &ExitError{Code: 3, Err: strictError(diags)}
	}

	return nil
}
