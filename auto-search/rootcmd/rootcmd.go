package rootcmd

import (
	"io"

	"github.com/mistakenot/auto-search/internal/app"
	"github.com/mistakenot/auto-search/internal/cli"
	"github.com/spf13/cobra"
)

func New(stdout, stderr io.Writer) *cobra.Command {
	return cli.NewRootCmd(app.New(stdout, stderr))
}
