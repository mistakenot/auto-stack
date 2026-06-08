// Package rootcmd exposes auto-env's command tree for mounting under the
// unified `auto` binary.
package rootcmd

import (
	"io"
	"os"

	"github.com/mistakenot/auto-env/internal/app"
	"github.com/mistakenot/auto-env/internal/cli"
	"github.com/spf13/cobra"
)

// New builds the auto-env command tree (mounted as `auto env`).
func New(stdout, stderr io.Writer) *cobra.Command {
	cwd, _ := os.Getwd()
	return cli.NewRootCmd(app.New(stdout, stderr, cwd))
}
