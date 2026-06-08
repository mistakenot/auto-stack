package cli

import (
	"encoding/json"
	"fmt"

	"github.com/mistakenot/auto-shared/update"
	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Check for and install the latest auto-stack release",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := update.Run(cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			data, err := json.Marshal(result)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
}
