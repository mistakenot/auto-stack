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
# Minimal observation — just kind, subject, and one evidence session:
auto reflect observation add \
  --kind gap \                          # required: correction|pattern|gap|incident
  --subject "no guidance on scoping go build to a module" \  # required: what this is about
  --evidence-session "$SID" \           # required (>=1): session id this was observed in
  --domain go \                         # optional: domain tag(s); repeatable or comma-separated
  --severity normal                     # optional: normal (default) or high

# Full observation with all evidence and metadata flags:
auto reflect observation add \
  --kind correction \
  --subject "swallowed error hid a real bug" \
  --evidence-session "$SID" \           # repeatable: cite multiple sessions by repeating
  --evidence-quote "the error was silently swallowed" \  # optional: verbatim excerpt, paired by position to --evidence-session
  --evidence-message "$SID-98" \        # optional: message id from 'auto search', paired by position
  --evidence-file internal/cli/rule.go \  # optional: source file proving the observation, paired by position
  --evidence-line-range 12-20 \         # optional: line or range (e.g. 12-20), paired by position
  --evidence-commit "$(git rev-parse --short HEAD)" \  # optional: git commit hash (7-40 hex), paired by position
  --task-id 049-reflect-audit-lineage-lint \  # optional: originating task id
  --context "ran build after pulling into a stale worktree" \  # optional: situational context
  --suggested-generalization "fetch+pull before branching" \  # optional: candidate rule this might generalize to
  --evidence-command "git commit -am 'wip'" \  # optional: verbatim bash command from transcript — do not edit or reconstruct; omit if not about a command
  --evidence-touched-file "internal/cli/rule.go" \  # optional: literal file path created/edited in transcript — do not glob; omit if not about a file
  --domain go \
  --severity high \
  --format text                         # optional: json (default) or text

# --evidence-quote/-message/-file/-line-range/-commit are paired by position to
# --evidence-session. Repeat the group to cite multiple transcript moments.
#
# --evidence-command and --evidence-touched-file are NOT positional — each observation
# has at most one of each, independent of the evidence session count. These are
# trigger-instance seed fields: consolidate later generalizes them into rule matchers
# (command regex / file glob) for just-in-time interceptors. Copy-paste verbatim from
# the transcript; do not edit, normalize, or glob.
#
# Run 'auto reflect observation add --help' for full flag details.

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

# split: retire one too-broad rule into narrower drafts. The parent is deprecated
# and each child is minted as a draft with lineage back to the parent.
auto reflect consolidate - <<'JSON'
{ "deltas": [
  { "op": "split", "rule_id": "r-1a2b3c4d", "into": [
    { "use_when": "editing go files", "content": "run go build ./... from the module dir",
      "causal_note": "module-scoped builds catch errors fast", "domain": ["go"] },
    { "use_when": "editing go tests", "content": "run go test ./... from the module dir",
      "causal_note": "split off the test-specific guidance", "domain": ["go"] }
  ] }
] }
JSON
` + "```" + `

Drafts are candidate state. Promote what earns it; retire what doesn't.

` + "```" + `bash
auto reflect rule promote <r-id>   # draft -> confirmed; refuses if provenance < 2 sessions (--force overrides)
auto reflect rule retire  <r-id>   # any -> stale (never surfaced by retrieve again)
` + "```" + `

Graduate a confirmed rule into a deterministic static check. The rule stays true
but is now enforced by a linter, so it no longer needs to ride along in retrieval.

` + "```" + `bash
auto reflect rule graduate <r-id> --linter golangci-lint --check errcheck \
  --config-path .golangci.yml   # records a lint_ref and sets lifecycle=enforced

# enforced rules are EXCLUDED from retrieve (the linter covers them); list them with:
auto reflect rule list --lifecycle enforced
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

**Uniform id envelope.** Every command exposes a predictable top-level id so you
never have to know each command's bespoke nesting:

- Mutations (` + "`rule create/edit/promote/...`, `observation add`, `consolidate`, `feedback`" + `)
  carry a top-level ` + "`.id`" + ` (and ` + "`.ids`" + ` when several entities are touched, e.g.
  ` + "`consolidate`" + ` and ` + "`feedback`" + `) alongside the descriptive payload.
- Collection commands stay arrays, but each element carries a top-level ` + "`.[].id`" + `
  next to its descriptive id (` + "`retrieval_id`/`feedback_id`/`observation_id`/`rule_id`" + `,
  or the ` + "`ev-`" + ` event id for ` + "`events list`/`gap list`" + `). Either key works in jq.

` + "```" + `bash
# 1. Retrieve predicates for your task (no content yet). Domain-matched hard rules
#    are always surfaced (hard_injected). Draft rules are surfaced flagged (lifecycle:
#    "draft", draft: true) so you can opt in; pass --no-drafts to confirmed-only. Stale
#    rules are never surfaced. Capture the retrieval_ids (.id == .retrieval_id here):
RT=$(auto reflect retrieve "add --json flag to auto env" --domain go,cli | jq -r '.[0].id')

# 2. Select the rules you care about, most interesting first. This reveals content
#    and mints a feedback_id per rule. Capture the feedback_id (.id == .feedback_id):
FB=$(auto reflect select "$RT" | jq -r '.[0].id')

# 3. ...do the coding task, using the rules...

# 4. Close the loop. rankings must cover EVERY outstanding feedback_id; rank is a
#    permutation of 1..N; reason is required per id. gap is optional but when present
#    both report and moment are required. outcome is one of:
#      success | partial | fail | abandoned   (how the TASK ended — distinct from
#      the miner ack status mined|empty|failed|skipped, which is about mining a session).
auto reflect feedback "{
  \"outcome\": \"success\",
  \"summary\": \"shipped the flag\",
  \"rankings\": [{\"feedback_id\": \"$FB\", \"rank\": 1, \"reason\": \"told me the exact flag pattern\"}],
  \"gap\": null
}"
# A feedback 'gap' records guidance that SHOULD have existed (gap.report + gap.moment).
# Surface captured gaps later with 'auto reflect gap list' (below) and turn each into
# an observation. NB: a feedback gap is NOT the same as an observation of --kind gap —
# the former is a loop signal on a feedback event; the latter is a mined finding.

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

# Feedback gaps captured during the loop (guidance that should have existed).
# Each row is {id (the ev- feedback event id), session_id, ts, report, moment}.
# Feed each one back into Stage 1 as an 'observation add --kind gap'.
auto reflect gap list --since 7d
# Note: feedback gaps carry no domain, so 'gap list --domain ...' fails fast —
# use 'auto reflect observation list --kind gap --domain <tag>' for domain-scoped
# gap OBSERVATIONS instead (a different concept from a feedback gap).
` + "```" + `

## Doctor

` + "```" + `bash
# Structured health check of the reflect state (state dir, events shards decode,
# playbook.json freshness vs the folded log, leftover legacy files). Output is
# [{check, status, message, hint}]; exits non-zero if any check fails.
auto reflect doctor
` + "```" + `

## Mining queue

` + "```" + `bash
# See which sessions are ready to mine (ranked by friction signals). The dominant
# signal is correction_density — user corrections per 100 user messages (weight 0.4
# in the score; tool errors 0.25, failure markers 0.2, AskUser 0.15) — so sessions
# where the human had to course-correct a lot rank highest.
auto reflect miner next --limit 5

# After mining a session, record the outcome:
auto reflect miner ack <session-id> --observations 3
# (use --status empty|failed|skipped for non-standard outcomes)

# Check mining coverage:
auto reflect miner status

# Inspect signals for any session:
auto reflect miner describe <session-id>
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
