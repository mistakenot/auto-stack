package cli

import (
	"fmt"
	"io"

	"github.com/mistakenot/auto-graph/internal/codegraph"
)

// diagMessage renders a single coverage-gap diagnostic as a human-readable
// warning line, tailored to the diagnostic kind.
func diagMessage(d codegraph.Diagnostic) string {
	switch d.Kind {
	case codegraph.DiagUnresolvedAlias:
		return fmt.Sprintf("%s:%d: unresolved alias import %q (check compilerOptions.paths and baseUrl in tsconfig.json)", d.Source, d.Line, d.Raw)
	case codegraph.DiagUnresolvedDynamic:
		return fmt.Sprintf("%s:%d: unresolved dynamic import %q (computed specifier — not statically analyzable)", d.Source, d.Line, d.Raw)
	default:
		return fmt.Sprintf("%s:%d: unresolved import %q", d.Source, d.Line, d.Raw)
	}
}

// printDiagnostics writes each coverage-gap diagnostic to w as a warning line.
func printDiagnostics(w io.Writer, diags []codegraph.Diagnostic) {
	for _, d := range diags {
		fmt.Fprintf(w, "warning: %s\n", diagMessage(d))
	}
}

// strictError summarizes coverage gaps into the error returned when --strict is
// set. The per-import detail has already been written as warnings.
func strictError(diags []codegraph.Diagnostic) error {
	n := len(diags)
	noun := "import"
	if n != 1 {
		noun = "imports"
	}
	return fmt.Errorf("strict: %d %s could not be resolved into the graph (see warnings above)", n, noun)
}
