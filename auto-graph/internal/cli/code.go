package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCodeCmd() *cobra.Command {
	codeCmd := &cobra.Command{
		Use:   "code",
		Short: "Code analysis commands",
	}

	codeCmd.AddCommand(newCodeGraphCmd())

	return codeCmd
}

func newCodeGraphCmd() *cobra.Command {
	var format string
	var lang string

	cmd := &cobra.Command{
		Use:   "graph <dir>",
		Short: "Build a file-level import graph for a project directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Placeholder: will be implemented in Phase 4
			fmt.Fprintf(cmd.ErrOrStderr(), "code graph not yet implemented (dir=%s, format=%s, lang=%s)\n", args[0], format, lang)
			return &ExitError{Code: 1, Err: fmt.Errorf("code graph command not yet implemented")}
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "output format: json, dot, mermaid")
	cmd.Flags().StringVar(&lang, "lang", "", "language override (auto-detected from config files if omitted)")

	return cmd
}
