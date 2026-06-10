package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const quickstartMarkdown = `# auto reflect quickstart

Two stages feed the repository playbook, then a retrieval loop puts it to work:

1. **Observe** — record cheap, evidence-linked observations as you notice them.
2. **Consolidate** — turn clusters of observations into draft rules (gated on >=2 distinct
   sessions of evidence), then promote the ones that earn it.
3. **Retrieve loop** — surface rules for a task, commit to an ordering, do the work, and close
   the loop with feedback. A gate blocks until feedback is submitted, so signal is never lost.

## Setup

` + "```" + `bash
cd /path/to/your/repo
auto reflect init            # global settings + this repo's local state
# auto reflect init --project  # set up only this repo's local state (events dir + playbook)
` + "```" + `

## 1. Observe (working memory)

Record a situated finding. Cheap, append-only, idempotent — capture first, generalize later.
Every observation needs at least one evidence session id.

` + "```" + `bash
auto reflect observation add \
  --kind gap \
  --subject "no guidance on scoping go build to a module" \
  --evidence-session "$CLAUDE_CODE_SESSION_ID" \
  --evidence-quote "go build ./... rebuilt everything and was slow" \
  --domain go \
  --severity normal            # kinds: correction|pattern|gap|incident; severity: normal|high

# List / filter observations. --unconsolidated shows only those not yet folded into a rule.
auto reflect observation list --kind gap --domain go --since 14d --unconsolidated
` + "```" + `

## 2. Consolidate (observations -> rules)

Hand a delta document to ` + "`consolidate`" + `. The CLI gates deterministically: a
` + "`create-draft`" + ` needs observation evidence spanning >=2 distinct sessions (or
` + "`--force`" + `, or a high-severity observation). Use ` + "`--dry-run`" + ` to preview.

` + "```" + `bash
auto reflect consolidate - --dry-run <<'JSON'
{ "deltas": [
  { "op": "create-draft",
    "use_when": "editing go files",
    "content": "After modifying a Go file, run go build ./... from that file's module dir",
    "causal_note": "unbuilt files hide compile errors that surface later as a batch",
    "domain": ["go"], "type": "soft",
    "observation_ids": ["ob-1a2b3c4d", "ob-5e6f7a8b"] }
] }
JSON
# Drop --dry-run to apply. Output: {applied, skipped (with reasons), conflicts}.
# Other ops: attach-evidence (rule_id + observation_ids), merge (rule_ids), deprecate (rule_id).
` + "```" + `

Drafts are candidate state. Promote what earns it; retire what doesn't.

` + "```" + `bash
auto reflect rule promote <r-id>   # draft -> confirmed; refuses if provenance < 2 sessions (--force overrides)
auto reflect rule retire  <r-id>   # any -> stale (never surfaced by retrieve again)
` + "```" + `

## Author rules directly (optional)

Consolidation is the usual path, but you can hand-author a rule too:

` + "```" + `bash
auto reflect rule create \
  --use-when "writing flaky end-to-end tests" \
  --content "Keep passing test logs short so failing E2E tests are easy to debug" \
  --causal-note "noisy passing logs hid the real failure during a debug session" \
  --domain testing --type soft   # --lifecycle defaults to draft

auto reflect rule list --lifecycle draft   # filter by lifecycle; omit to list all
auto reflect rule get <r-id>               # full rule incl. observation_ids provenance
auto reflect rule edit <r-id> --content "..."   # all changed fields = one versioned edit
auto reflect rebuild                       # force a refold of the playbook snapshot
` + "```" + `

## 3. Retrieval loop

The loop mints ids you must thread from one step to the next. Capture them with jq.

` + "```" + `bash
# 1. Retrieve predicates for your task (no content yet). Domain-matched hard rules
#    are always surfaced (hard_injected). Draft rules are surfaced flagged (lifecycle:
#    "draft", draft: true) so you can opt in; pass --no-drafts to confirmed-only. Stale
#    rules are never surfaced. Capture the retrieval_ids:
RT=$(auto reflect retrieve "add --json flag to auto env" --domain go,cli | jq -r '.[0].retrieval_id')

# 2. Select the rules you care about, most interesting first. This reveals content
#    and mints a feedback_id per rule. Capture the feedback_id:
FB=$(auto reflect select "$RT" | jq -r '.[0].feedback_id')

# 3. ...do the coding task, using the rules...

# 4. Close the loop. rankings must cover EVERY outstanding feedback_id; rank is a
#    permutation of 1..N; reason is required per id. gap is optional but when present
#    both report and moment are required.
auto reflect feedback "{
  \"outcome\": \"success\",
  \"summary\": \"shipped the flag\",
  \"rankings\": [{\"feedback_id\": \"$FB\", \"rank\": 1, \"reason\": \"told me the exact flag pattern\"}],
  \"gap\": null
}"

# You can also pipe the JSON document via stdin:
echo "$PAYLOAD" | auto reflect feedback -

# 5. Gate: skills/hooks call this. Exits 0 only when no feedback_ids are outstanding
#    in scope (the detected session, else this host+worktree within a 24h lookback).
auto reflect gate check
` + "```" + `

## Stats & raw events

` + "```" + `bash
# Per-rule signal + the unconsolidated-observation backlog. Output is an object:
#   { "unconsolidated_observations": N, "rules": [ {rule_id, surfaced, selected,
#     selection_rate, feedback_count, rank_distribution, outcome_counts}, ... ] }
# Read the per-rule rows from .rules[] (jq -r '.rules[].rule_id').
auto reflect stats

# Raw, read-only view over the canonical event log (newest-first).
auto reflect events list --type feedback --since 7d
auto reflect events list --type observation --type consolidation   # repeatable --type
` + "```" + `

## Files created by auto reflect

- ` + "`" + `.auto/reflect/events/` + "`" + `: append-only canonical event log (sharded by host/day/worktree)
- ` + "`" + `.auto/reflect/playbook.json` + "`" + `: folded rule snapshot (a disposable cache; rebuild any time)
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
