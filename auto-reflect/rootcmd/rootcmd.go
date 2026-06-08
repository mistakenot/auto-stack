package rootcmd

import (
	"io"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/cli"
	"github.com/spf13/cobra"
)

// New builds the auto-reflect root command using the given output writers.
func New(stdout, stderr io.Writer) *cobra.Command {
	return cli.NewRootCmd(app.New(stdout, stderr))
}
