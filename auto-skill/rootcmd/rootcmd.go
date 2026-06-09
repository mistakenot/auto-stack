// Package rootcmd exposes auto-skill's command tree for mounting under the
// unified `auto` binary.
package rootcmd

import (
	"io"

	"github.com/mistakenot/auto-skill/internal/app"
	"github.com/mistakenot/auto-skill/internal/cli"
	"github.com/spf13/cobra"
)

// New builds the auto-skill command tree (mounted as `auto skill`).
func New(stdout, stderr io.Writer) *cobra.Command {
	return cli.NewRootCmd(app.New(stdout, stderr))
}
