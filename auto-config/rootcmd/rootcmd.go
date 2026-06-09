// Package rootcmd exposes auto-config's command tree for mounting under the
// unified `auto` binary.
package rootcmd

import (
	"io"
	"os"

	"github.com/mistakenot/auto-config/internal/app"
	"github.com/mistakenot/auto-config/internal/cli"
	"github.com/spf13/cobra"
)

// New builds the auto-config command tree (mounted as `auto config`).
func New(stdout, stderr io.Writer) *cobra.Command {
	cwd, _ := os.Getwd()
	return cli.NewRootCmd(app.New(stdout, stderr, cwd))
}
