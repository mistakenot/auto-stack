package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs",
		Short: "Show command reference",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			docs := strings.Join([]string{
				"# autoui commands",
				"",
				"- `init`: initialize shared and autoui settings (~/.auto/ui/settings.json).",
				"- `doctor`: check settings validity and configured port, report as JSON.",
				"- `quickstart`: show a minimal happy-path workflow.",
				"- `docs`: show this command reference.",
				"- `update`: check for and install the latest auto-stack release.",
				"- `serve`: serve the auto-ui dashboard locally.",
				"  - `--port`: port to serve on (overrides settings.json; default 8080).",
			}, "\n")
			_, err := fmt.Fprintln(cmd.OutOrStdout(), docs)
			return err
		},
	}
}
