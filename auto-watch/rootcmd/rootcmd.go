package rootcmd

import (
	"io"

	"github.com/mistakenot/auto-watch/internal/app"
	"github.com/mistakenot/auto-watch/internal/cli"
	"github.com/spf13/cobra"
)

func New(stdout, stderr io.Writer) *cobra.Command {
	return cli.NewRootCmd(app.New(stdout, stderr))
}
