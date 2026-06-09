package cli

import (
	"fmt"

	"github.com/mistakenot/auto-ui/internal/config"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize shared and auto ui settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sharedPath, _, sharedCreated, err := config.EnsureSharedSettings()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			uiPath, _, uiCreated, err := config.EnsureUISettings()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Shared settings: %s\n", sharedPath)
			if sharedCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created shared settings.json.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Shared settings.json already exists.")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "UI settings: %s\n", uiPath)
			if uiCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created auto ui settings.json.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Autoui settings.json already exists.")
			}
			return nil
		},
	}
}
