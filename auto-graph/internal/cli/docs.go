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
				"# auto graph commands",
				"",
				"- `init`: initialize shared and graph settings.",
				"- `doctor`: check dependencies (ast-grep) and settings, report as JSON.",
				"- `quickstart`: show a minimal happy-path workflow.",
				"- `docs`: show this command reference.",
				"- `update`: check for and install the latest auto-stack release.",
				"- `code graph <dir>`: scan a project directory and output the import graph.",
				"  - `--format`: output format — json (default), dot, mermaid.",
				"  - `--lang`: language override — typescript, go (auto-detected from tsconfig.json or go.mod).",
				"  - `--strict`: exit non-zero (code 3) if any import could not be resolved into the graph.",
				"- `code context <dir>`: build a context pack for seed files from the import graph.",
				"  - `--token-limit`: maximum token budget for the rendered output (required).",
				"  - `--file`: seed file path, repeatable (at least one required).",
				"  - `--format`: output format — markdown (default), json.",
				"  - `--lang`: language override — typescript, go (auto-detected from tsconfig.json or go.mod).",
				"  - `--strict`: exit non-zero (code 3) if any import could not be resolved into the underlying graph.",
			}, "\n")
			_, err := fmt.Fprintln(cmd.OutOrStdout(), docs)
			return err
		},
	}
}
