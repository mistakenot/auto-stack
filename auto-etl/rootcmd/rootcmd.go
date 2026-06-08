package rootcmd

import (
	"io"

	"github.com/mistakenot/auto-etl/cmd"
	"github.com/spf13/cobra"
)

// New returns the auto-etl command tree for mounting as `auto etl`.
//
// auto-etl writes via bare fmt.Print* to os.Stdout, so the writer arguments are
// ignored (identical to production). The signature is kept for uniformity with
// the other tool wrappers.
func New(stdout, stderr io.Writer) *cobra.Command {
	return cmd.NewRootCmd()
}
