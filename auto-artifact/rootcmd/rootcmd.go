// Package rootcmd exposes auto-artifact's command tree for mounting under the
// unified `auto` binary.
package rootcmd

import (
	"io"
	"os"

	"github.com/mistakenot/auto-artifact/internal/app"
	"github.com/mistakenot/auto-artifact/internal/cli"
	"github.com/spf13/cobra"
)

// New builds the auto-artifact command tree (mounted as `auto artifact`).
func New(stdout, stderr io.Writer) *cobra.Command {
	cwd, _ := os.Getwd()
	return cli.NewRootCmd(app.New(stdout, stderr, cwd))
}
