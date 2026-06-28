---
name: reflect-playbook
description: "Drives the full auto reflect loop end to end — mine old sessions (with cursor tracking), capture evidence-linked observations, consolidate them into gated playbook rules, and retrieve rules for a task with closed-loop feedback. Use when 'reflect on sessions', 'mine sessions for rules', 'build/curate the playbook', 'consolidate observations', 'promote a rule', 'retrieve rules for this task', or running the reflection loop. Backed by the `auto reflect` event store. Not for one-off session-search reports (use reflect-on-agent-sessions) or git/PR mining (use learning-diary)."
---

# Reflect Playbook

Turn evidence from finished work into reusable, task-facing guidance, then put
that guidance to work — all through the `auto reflect` event store. This is one
composite skill with four stages; each stage has a reference file you read on
demand.

```text
            mine old sessions            capture                consolidate              put to work
   ┌──────────────────────────┐  ┌──────────────────┐  ┌────────────────────┐  ┌─────────────────────────┐
   │ miner next → ack (cursor)│→ │ observation add  │→ │ consolidate + rule │→ │ retrieve→select→feedback│
   └──────────────────────────┘  └──────────────────┘  │ promote/retire/grad│  │ →gate (loop closes)     │
              ▲                                          └────────────────────┘  └─────────────────────────┘
              └────────────────── feedback `gap` reports seed the next observation ──────────────────────────┘
```

| Stage | Read this | What it does | Core commands |
|---|---|---|---|
| **0 · Mine** | `references/mine.md` | Walk old sessions oldest-first, **track where you got to** | `miner next` / `miner ack` / `miner status` |
| **1 · Observe** | `references/observe.md` | Capture cheap, evidence-linked findings | `observation add` / `observation list` |
| **2 · Consolidate** | `references/consolidate.md` | Cluster observations → gated draft rules; curate lifecycle | `consolidate` / `rule promote\|retire\|graduate` |
| **3 · Retrieve** | `references/retrieve.md` | Surface rules for a task; close the loop with feedback | `retrieve` → `select` → `feedback` → `gate check` |

Read **only** the reference(s) for the stage you're running. Don't load all four.

## Supersedes the legacy `playbook-*` trio

This replaces `playbook-observe` / `playbook-refine` / `playbook-search`, which
wrote hand-rolled YAML (`docs/reflection/observations.yaml`, `rules.yaml`,
`retrievals.ndjson`) and a hand-maintained `cursors:` ledger via a bundled
`reflect.py`. **Use this skill instead** for any repo where `auto reflect init`
has been run. Everything those skills did by hand the tool now owns:

| Legacy (yaml + reflect.py) | Now (`auto reflect`) |
|---|---|
| `reflect.py gen-id` time-ordered IDs | tool mints `ob-*` / `r-*` / retrieval / feedback ids |
| `cursors:` ledger in observations.yaml | `miner ack` / `miner next` (see Stage 0) |
| `yq -i` append to observations.yaml | `observation add` (append-only events) |
| hand-distil rules.yaml | `consolidate` with deterministic gates |
| `retrievals.ndjson` telemetry | `retrieve`/`select`/`feedback` events + `stats` |

Do **not** dual-write the old yaml files in a repo that uses `auto reflect`.

## One-time setup

```bash
cd /path/to/repo
auto reflect init            # global settings + this repo's local state
# auto reflect init --project  # repo-local state only (events dir + playbook)
```

Creates `.auto/reflect/events/` (append-only canonical log), `.auto/reflect/playbook.json`
(a disposable folded snapshot — `auto reflect rebuild` any time), and `~/.auto/reflect/settings.json`.

## Invariants (true for every stage)

- **The tool owns identity and cursors.** Never hand-roll ids, timestamps, or a
  progress ledger — `observation add`, `consolidate`, `retrieve`/`select`, and
  `miner ack` mint and persist them. This is the whole reason to prefer the tool.
- **Gates are deterministic, not advisory.** `consolidate` refuses a `create-draft`
  under 2 evidence sessions; `rule promote` needs ≥3 tasks or ≥2 sessions; `gate
  check` fails until feedback is submitted. Don't paper over a gate — satisfy it
  (more evidence) or use the documented escape (`--force`) and say so.
- **Append-only + idempotent.** Observations and events are immutable once
  written; re-running a stage with no new input is a no-op. Capture first,
  generalize later.
- **Default output is JSON.** Thread minted ids between steps with `jq`. Add
  `--format text` only for human reading.

## Health check (any time)

```bash
auto reflect stats                       # per-rule surfaced/selected/feedback + unconsolidated backlog
auto reflect miner status                # how much session history is still unmined
auto reflect events list --type feedback --since 7d   # raw, read-only audit of recent loop signal
```

## Typical entry points

- "Mine the last N sessions for lessons" → Stage 0, then 1 per session.
- "Turn these observations into rules" / "curate the playbook" → Stage 2.
- "What rules apply to this task?" (and close the loop after) → Stage 3.
