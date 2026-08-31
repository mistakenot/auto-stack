// Package rootcmd exposes auto-mail's command tree for mounting under the
// unified `auto` binary. It holds no domain logic and never touches the store —
// it is a wiring surface, not an API (D-062-8).
package rootcmd

import (
	"io"
	"os"

	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-mail/internal/cli"
	"github.com/spf13/cobra"
)

// New builds the auto-mail command tree (mounted as `auto mail`).
func New(stdout, stderr io.Writer) *cobra.Command {
	cwd, _ := os.Getwd()
	return cli.NewRootCmd(app.New(stdout, stderr, cwd))
}
