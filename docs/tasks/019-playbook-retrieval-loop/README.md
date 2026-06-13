---
hash: "25eb60dd"
id: "ebe86543"
read_when: "understanding the auto-reflect playbook retrieval loop CLI surface or designing its API"
summary: "Artifact visualizing the final auto-reflect CLI surface for the playbook retrieval loop: rule authoring, retrieve/select/feedback/gate workflow, storage model, and stats."
title: "Auto Reflect Mock README — Task 019 API Preview"
---

# auto reflect — mock README (task 019 API preview)

> **Artifact, not shipped docs.** This is a visualization of the final CLI surface as designed in
> [solution.md](./solution.md), written as if v1 were already released. The real README/quickstart
> gets written during implementation.

`auto reflect` maintains a **playbook** of rules for this repository and runs a retrieval loop that
captures feedback signal every time an agent uses them. All state is an append-only event log
committed with the repo; the playbook file is just a derived view.

## Setup

```bash
auto reflect init --project    # creates .auto/reflect/{events/,playbook.json}
auto reflect quickstart        # LLM-friendly walkthrough of the whole loop
```

## Authoring rules

```bash
auto reflect rule create \
  --use-when "editing Go source files in a multi-module repo" \
  --content "After modifying a Go file, run 'go build ./...' from that file's module directory before moving on." \
  --causal-note "Unbuilt files hide compile errors that surface later as a confusing batch." \
  --domain go,build \
  --type soft
```
```json
{
  "id": "r-1a2b3c4d",
  "domain": ["build", "go"],
  "use_when": "editing Go source files in a multi-module repo",
  "rule_type": "soft",
  "lifecycle": "draft",
  "version": 1
}
```

- `--type hard` rules are admissibility constraints — they are *always* surfaced when their domain
  matches, regardless of match score, and must declare at least one `--domain`.
- Edits are field-level deltas, never rewrites — every edit bumps `version` and the full history
  stays reconstructable:

```bash
auto reflect rule edit r-1a2b3c4d --content "…sharper wording…"   # version 1 → 2
auto reflect rule list                  # all rules: id + use_when + domain + type
auto reflect rule get r-1a2b3c4d        # full rule, current version
```

## The loop (what an agent runs per task)

**1. Retrieve — predicates only, no content yet:**

```bash
auto reflect retrieve "add a --json output flag to auto env" --domain go,cli
```
```json
[
  { "retrieval_id": "rt-9f1e2d3c", "use_when": "editing Go source files in a multi-module repo",
    "domain": ["build", "go"], "rule_type": "soft" },
  { "retrieval_id": "rt-4b5a6f70", "use_when": "adding or changing JSON output of a CLI command",
    "domain": ["cli", "json"], "rule_type": "hard", "hard_injected": true }
]
```

**2. Select — commit to an interest ordering (most interesting first), receive content:**

```bash
RT1=$(auto reflect retrieve "..." | jq -r '.[0].retrieval_id')   # capture minted ids
auto reflect select rt-4b5a6f70 rt-9f1e2d3c
```
```json
[
  { "feedback_id": "fb-77aa88bb", "rule_type": "hard",
    "content": "In JSON mode, stdout carries payload only; diagnostics go to stderr.",
    "causal_note": "Mixed streams break every downstream jq/pipe consumer." },
  { "feedback_id": "fb-11cc22dd", "rule_type": "soft",
    "content": "After modifying a Go file, run 'go build ./...' from that file's module directory…",
    "causal_note": "Unbuilt files hide compile errors that surface later as a confusing batch." }
]
```

**3. Do the work.**

**4. Feedback — closes the loop; the gate stays shut until every `feedback_id` is accounted for:**

```bash
auto reflect feedback - <<'EOF'
{
  "outcome": "success",
  "summary": "flag added, tests pass",
  "rankings": [
    { "feedback_id": "fb-77aa88bb", "rank": 1, "reason": "caught a stdout/stderr mix in my first draft" },
    { "feedback_id": "fb-11cc22dd", "rank": 2, "reason": "followed it, but I knew this already" }
  ],
  "gap": {
    "report": "needed guidance on scoping the build to one module — repo-root build was slow",
    "moment": "after editing auto-env/internal/cli/env.go, waiting on go build ./... from root"
  }
}
EOF
```

- `outcome`: `success | partial | fail | abandoned`
- `gap` may be `null`, but if present it must cite the **moment** it was needed (grounding rule).
- Incomplete submissions (missing ids, duplicate ranks, ungrounded gap) exit non-zero with a
  structured error naming each missing piece.

**5. Gate — what hooks/skills call before letting the task finish:**

```bash
auto reflect gate check          # exit 0: clean. exit 1: lists outstanding fb-ids + the exact
                                 # 'auto reflect feedback …' command to run
```

- Scope: current session (`AUTO_SESSION_ID`/`CODEX_SESSION_ID`/`CLAUDE_SESSION_ID`, or `--session`);
  with no detectable session it checks this host + this worktree within `--since` (default `24h`).
- Orphaned sessions (crashed agent, abandoned branch) are closed explicitly:
  `auto reflect feedback --session <id> …` with `"outcome": "abandoned"`.
- A session that consumed no rules passes.

## Reading the signal

```bash
auto reflect stats
```
```json
[
  { "rule_id": "r-1a2b3c4d", "surfaced": 14, "selected": 11,
    "selection_rate": 0.79, "feedback_count": 11 }
]
```

```bash
auto reflect rebuild     # force refold of playbook.json from events;
                         # prints any concurrent-edit conflicts to stderr
```

## Storage model

```
.auto/reflect/
├── events/                                  # CANONICAL — append-only, committed
│   ├── carbon-2026-06-09-3fa1b2c8.jsonl     # <host>-<date>-<sha256(worktree)[:8]>.jsonl
│   └── carbon-2026-06-09-9d4e5f60.jsonl     # parallel worktree = different file → no merge conflicts
└── playbook.json                            # DERIVED — fold of rule events; committed for PR review;
                                             # rebuilt automatically when stale, disposable
```

Every event: `{id, type, schema_version, seq, ts, host, session_id?, agent?, git: {hash, remote}, payload}`
— remotes are credential-sanitized before write. Event types: `rule_created`, `rule_edited`,
`retrieval`, `selection`, `feedback`.

## Conventions

- JSON on stdout (payload only), diagnostics on stderr, `--format text` where you want prose.
- Every error tells you how to fix it.
- Deferred by design (the log already captures what they'll need): probe injection, fresh-agent
  session review, automated rule triage, A/B rule versions, contrastive analysis.
