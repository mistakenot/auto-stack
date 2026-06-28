# Stage 3 · Retrieve — surface rules for a task, then close the loop

The retrieval loop mints ids you must thread from one step to the next. A gate
blocks completion until you submit feedback, so signal is never silently lost.

```text
  retrieve  ──retrieval_id──▶  select  ──feedback_id (per rule)──▶  …do the task…  ──▶  feedback  ──▶  gate check
  (predicates,                 (reveals content,                                       (rank every       (passes once
   no content)                  commits an ordering)                                    feedback_id)      none outstanding)
```

## 1. Retrieve — predicates only, no content

```bash
RT=$(auto reflect retrieve "add --json flag to auto env" --domain go,cli | jq -r '.[0].retrieval_id')
```

- Returns `use_when`/`domain` predicates + a `retrieval_id` per match — **not the
  rule content yet** (cheap rung; content is revealed at `select`).
- Domain-matched **hard** rules are always surfaced (`hard_injected: true`).
- **Draft** rules are surfaced flagged (`draft: true`); pass `--no-drafts` for
  confirmed-only.
- **enforced** and **stale** rules are never surfaced (a linter covers the first;
  the second is retired).

## 2. Select — commit to an ordering, reveal content

```bash
FB=$(auto reflect select "$RT" | jq -r '.[0].feedback_id')
```

`select` takes the `retrieval_id`, reveals the full content of the rules you
commit to (most-interesting first), and mints **one `feedback_id` per rule**.
Capture them all — you must account for every one at step 4.

## 3. Do the task

Use the surfaced rules while you work.

## 4. Feedback — close the loop

```bash
auto reflect feedback "{
  \"outcome\": \"success\",
  \"summary\": \"shipped the flag\",
  \"rankings\": [
    {\"feedback_id\": \"$FB\", \"rank\": 1, \"reason\": \"told me the exact flag pattern\"}
  ],
  \"gap\": null
}"
# or pipe a document:  echo "$PAYLOAD" | auto reflect feedback -
```

Contract (enforced):

- `rankings` must cover **every outstanding `feedback_id`** in scope.
- `rank` is a **permutation of 1..N** (no gaps, no dupes) — relative usefulness.
- `reason` is **required per id**.
- `gap` is optional, but when present **both `report` and `moment` are required**.
  A `gap` is the bridge back to Stage 1: it names guidance that *should* have
  existed — capture it as an `observation` next.

## 5. Gate — prove the loop closed

```bash
auto reflect gate check   # exit 0 only when no feedback_ids are outstanding in scope
```

Scope = the detected session, else this host + worktree within a 24h lookback.
Skills/hooks call this to block "done" until feedback exists. If it fails, you
have unanswered `feedback_id`s — submit feedback (step 4) before claiming done.

## Inspect signal

```bash
auto reflect stats
#   { unconsolidated_observations, rules: [ { rule_id, surfaced, selected,
#     selection_rate, feedback_count, rank_distribution, outcome_counts } ] }
#   read per-rule rows from .rules[]  (jq -r '.rules[].rule_id')

auto reflect events list --type feedback --type observation --since 7d   # raw, read-only; --type repeatable
```

Low `selection_rate` on a surfaced rule, or poor `rank_distribution`, is a signal
to `rule edit` it sharper or `rule retire` it. A recurring `gap` across feedback
is a signal to observe + consolidate a new rule.

## Rules

- Thread the ids with `jq`; never invent a `retrieval_id` or `feedback_id`.
- Account for **every** `feedback_id` in `rankings`, or `gate check` fails.
- Treat a `gap` report as a TODO: feed it back into Stage 1 as an observation.
- `select` is a commitment — it reveals content and starts the feedback clock.

Loop back to **`references/observe.md`** with any gap you surfaced.
