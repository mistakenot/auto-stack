# Stage 0 · Mine — walk old sessions, track where you got to

Reading back over historical sessions is a long, resumable sweep. The **miner is
the cursor**: it ranks sessions you haven't mined yet, and `ack` records that
you finished one. You never maintain a `cursors:` ledger by hand — that was the
legacy `playbook-observe` approach and it is replaced here.

```text
  miner next ──pick top session──▶ read it (auto search) ──▶ Stage 1: observation add ──▶ miner ack
      ▲                                                                                       │
      └───────────────────── cursor advances; that session won't reappear in `next` ─────────┘
```

## The loop

```bash
# 0. Make sure the session history the miner ranks over is current.
auto etl run            # ingest any new raw transcripts (skip if already fresh)
auto search index       # refresh the search index used to read transcripts

# 1. What's left to mine, ranked by friction signals (hottest first)?
auto reflect miner next --limit 5
#   --all                include sessions from every workspace (default: this repo)
#   --include-subagents  also list subagent session ids per item
#   --limit 0            list everything still unmined

# 2. Read the chosen session's transcript (this is what you mine from).
auto search session get <session-id>
#   add --subagent / --no-subagent to scope; see the friction in context:
auto reflect miner describe <session-id>   # signals + this session's ack history

# 3. Extract findings → Stage 1 (references/observe.md). Cite this session id
#    in every observation so consolidation can count evidence later.

# 4. Record the outcome. THIS IS THE CURSOR WRITE — do it for every session you
#    open, even empty ones, or it resurfaces in `next` forever.
auto reflect miner ack <session-id> --observations 3
#   --status mined    (default) you extracted observations (--observations N)
#   --status empty    you read it, nothing worth capturing
#   --status failed   couldn't read/parse it
#   --status skipped  deliberately passing for now
```

## How far have I got?

```bash
auto reflect miner status          # coverage for this workspace (mined vs remaining)
auto reflect miner status --all    # across all workspaces
```

`status` is your resume point: stop any time, come back, run `miner next`, and
you pick up exactly where you left off — already-acked sessions never reappear.

## Friction signals (why a session is ranked high)

`miner next` orders by signals that correlate with "something went wrong here and
there's a lesson in it." Inspect them before committing time:

```bash
auto reflect miner signals <session-id> [<session-id> ...]
```

Prefer mining high-signal sessions first — they yield the densest observations
per minute. A low-signal session is fine to `ack --status empty` quickly.

## Scale / batch

- Mine **oldest-first within a friction tier** so evidence accumulates in causal
  order. `miner next` already ranks for you; within a batch, process the list top
  to bottom.
- For a big backlog, dispatch a sub-agent per session (or small batch): each
  agent reads one transcript, returns candidate observations as structured data,
  and the coordinator runs `observation add` + `miner ack`. The coordinator never
  needs the full transcripts in its own context.
- A session is acked **once**. Re-running the whole sweep is a no-op for already
  mined sessions — safe to repeat.

## Rules

- Always `miner ack` a session you opened — the ack is the cursor; skipping it
  breaks resumability.
- Cite the mined `session-id` in every observation you write from it.
- Don't hand-maintain any progress file; `miner status` is the source of truth.
- Refresh `auto etl run` + `auto search index` before a sweep so newly-arrived
  sessions are visible to `miner next`.

Next: **`references/observe.md`** to capture what you find.
