// Package rootcmd exposes auto-plan's command tree for mounting under the
// unified `auto` binary. It holds no domain logic — it is a wiring surface,
// not an API.
package rootcmd

import (
	"io"
	"os"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/cli"
	"github.com/spf13/cobra"
)

// New builds the auto-plan command tree (mounted as `auto plan`).
func New(stdout, stderr io.Writer) *cobra.Command {
	cwd, _ := os.Getwd()
	return cli.NewRootCmd(app.New(stdout, stderr, cwd))
}
