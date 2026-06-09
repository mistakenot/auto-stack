package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# auto reflect quickstart

Persist and retrieve repository-specific rules, then capture feedback events for what helped, harmed, or was missing.

## Setup

` + "```" + `bash
cd /path/to/your/repo
auto reflect init
` + "```" + `

## Rule memory workflow

` + "```" + `bash
# Persist a reusable lesson into the local playbook.
auto reflect rule create \
  --content "Keep passing test logs short so failing E2E tests are easy to debug" \
  --category testing \
  --tag e2e \
  --tag logs

# Retrieve relevant rules later.
auto reflect lookup "e2e logs flaky tests"
` + "```" + `

## Feedback workflow

` + "```" + `bash
# Helpful annotation on a file span.
auto reflect feedback add \
  --kind helpful \
  --file docs/setup.md \
  --start 12 \
  --end 18 \
  --comment "this section avoided redoing the install flow" \
  --effective-at 2026-05-01T10:00:00Z \
  --context "installing auto reflect in a fresh repo"

# Missing context annotation (no file needed).
auto reflect feedback add \
  --kind missing \
  --comment "missing docs for release rollback steps" \
  --effective-at 2026-04-28 \
  --context "writing release runbook automation"

# Harmful annotation with context to capture why it caused churn.
auto reflect feedback add \
  --kind harmful \
  --file docs/quickstart.md \
  --start 6 \
  --end 9 \
  --comment "outdated command led to repeated retries" \
  --effective-at 2026-05-03T14:30:00Z \
  --context "following first-time setup docs in CI container"

# Query recent feedback.
auto reflect feedback list --since 14d
auto reflect feedback list --kind harmful --file docs/
` + "```" + `

## Files created by auto reflect

- ` + "`" + `.auto/reflect/playbook.json` + "`" + `: repository-local rule memory
- ` + "`" + `.auto/reflect/feedback.jsonl` + "`" + `: append-only feedback event log
- ` + "`" + `~/.auto/reflect/settings.json` + "`" + `: global auto reflect settings
`

func newQuickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Print an LLM-friendly guide to using auto reflect",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), quickstartMarkdown)
			return nil
		},
	}
}
