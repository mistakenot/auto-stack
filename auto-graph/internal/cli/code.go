package cli

import (
	"github.com/spf13/cobra"
)

func newCodeCmd() *cobra.Command {
	codeCmd := &cobra.Command{
		Use:   "code",
		Short: "Code analysis commands",
	}

	codeCmd.AddCommand(newCodeGraphCmd())
	codeCmd.AddCommand(newCodeContextCmd())

	return codeCmd
}
