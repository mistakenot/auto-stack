// Package rootcmd exposes auto-ui's command tree for mounting under the
// unified `auto` binary.
package rootcmd

import (
	"io"

	"github.com/mistakenot/auto-ui/internal/app"
	"github.com/mistakenot/auto-ui/internal/cli"
	"github.com/spf13/cobra"
)

// New builds the auto-ui command tree (mounted as `auto ui`).
func New(stdout, stderr io.Writer) *cobra.Command {
	return cli.NewRootCmd(app.New(stdout, stderr))
}
