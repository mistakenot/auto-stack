package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# autoreflect quickstart

Persist and retrieve repository-specific rules, then capture feedback events for what helped, harmed, or was missing.

## Setup

` + "```" + `bash
cd /path/to/your/repo
autoreflect init
` + "```" + `

## Rule memory workflow

` + "```" + `bash
# Persist a reusable lesson into the local playbook.
autoreflect rule create \
  --content "Keep passing test logs short so failing E2E tests are easy to debug" \
  --category testing \
  --tag e2e \
  --tag logs

# Retrieve relevant rules later.
autoreflect lookup "e2e logs flaky tests"
` + "```" + `

## Feedback workflow

` + "```" + `bash
# Helpful annotation on a file span.
autoreflect feedback add \
  --kind helpful \
  --file docs/setup.md \
  --start 12 \
  --end 18 \
  --comment "this section avoided redoing the install flow" \
  --context "installing autoreflect in a fresh repo"

# Missing context annotation (no file needed).
autoreflect feedback add \
  --kind missing \
  --comment "missing docs for release rollback steps" \
  --context "writing release runbook automation"

# Harmful annotation with context to capture why it caused churn.
autoreflect feedback add \
  --kind harmful \
  --file docs/quickstart.md \
  --start 6 \
  --end 9 \
  --comment "outdated command led to repeated retries" \
  --context "following first-time setup docs in CI container"

# Query recent feedback.
autoreflect feedback list --since 14d
autoreflect feedback list --kind harmful --file docs/
` + "```" + `

## Files created by autoreflect

- ` + "`" + `.auto/reflect/playbook.json` + "`" + `: repository-local rule memory
- ` + "`" + `.auto/reflect/feedback.jsonl` + "`" + `: append-only feedback event log
- ` + "`" + `~/.auto/reflect/settings.json` + "`" + `: global autoreflect settings
`

func newQuickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Print an LLM-friendly guide to using autoreflect",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), quickstartMarkdown)
			return nil
		},
	}
}
