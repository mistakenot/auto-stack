package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mistakenot/auto-shared/update"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for and install the latest auto-stack release",
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := update.Run(os.Stdout, os.Stderr)
		if err != nil {
			return err
		}
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

func newUpdateCmd() *cobra.Command {
	return updateCmd
}
