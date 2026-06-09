package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# auto ui quickstart

Serve the auto-ui local web dashboard from a single Go binary. The dashboard is a no-build
Preact + htm single-page app served either embedded (shipped binary) or live from disk (dev loop).

## Core workflow

### 1. Initialize settings

` + "```" + `bash
auto ui init
auto ui doctor
` + "```" + `

This creates ` + "`~/.auto/ui/settings.json`" + ` (default port 8080) and verifies the configuration.

### 2. Serve the dashboard

` + "```" + `bash
# Serve on the configured port (default 8080)
auto ui serve

# Override the port
auto ui serve --port 9090
` + "```" + `

Then open http://localhost:8080 in a browser.

### 3. Develop the frontend (live-from-disk)

` + "```" + `bash
# Build the auto binary with the dev tag (from repo root), then run it from
# the auto-ui/ module root so assets resolve live from web/static:
go build -tags dev -o bin/auto ./auto-cli/cmd/auto
cd auto-ui && ../bin/auto ui serve
` + "```" + `

Edit files under ` + "`web/static/`" + ` and refresh the browser — no Go rebuild required.

Run ` + "`auto ui <command> --help`" + ` for full flag details on any command.
`

func newQuickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Print an LLM-friendly guide to using autoui",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), quickstartMarkdown)
			return nil
		},
	}
}
