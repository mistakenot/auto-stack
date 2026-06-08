// Package rootcmd is the public seam the umbrella `auto` binary uses to mount
// autodoc as `auto doc`. auto-doc writes to os.Stdout directly, so the writer
// arguments are accepted for umbrella uniformity but unused.
package rootcmd

import (
	"io"

	"github.com/datadyne-io/autodoc/internal/cli"
	"github.com/spf13/cobra"
)

// New returns the autodoc command tree. The stdout/stderr writers are accepted
// for a uniform signature across tools but are ignored here (autodoc writes to
// os.Stdout / os.Stderr directly, identical to production).
func New(stdout, stderr io.Writer) *cobra.Command {
	return cli.NewRootCmd()
}
