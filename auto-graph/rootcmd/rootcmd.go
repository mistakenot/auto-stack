// Package rootcmd exposes auto-graph's command tree for mounting under the
// unified `auto` binary.
package rootcmd

import (
	"io"

	"github.com/mistakenot/auto-graph/internal/app"
	"github.com/mistakenot/auto-graph/internal/cli"
	"github.com/spf13/cobra"
)

// New builds the auto-graph command tree (mounted as `auto graph`).
func New(stdout, stderr io.Writer) *cobra.Command {
	return cli.NewRootCmd(app.New(stdout, stderr))
}
