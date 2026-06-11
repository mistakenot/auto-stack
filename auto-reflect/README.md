# auto reflect

Analyze past coding sessions and turn what you learn into a repository **playbook** of rules
that guide future agent runs — then close the loop by measuring whether those rules actually
helped.

`auto reflect` is one tool in the [auto-stack](../README.md). It consumes the shared session
corpus (via `auto search` / `auto etl`) on the read side and writes a small, durable,
event-sourced rule store on the write side.

> Run `auto reflect quickstart` for the always-current, copy-pasteable version of everything
> below.

---

## The mental model

Knowledge flows through three tiers, mirroring a working-memory architecture (raw logs →
working memory → procedural memory):

```
  sessions            observations              rules                 retrieval loop
 (auto etl) ──mine──▶  (working memory) ──consolidate──▶ (playbook) ──surface──▶ agent task
                         cheap, evidence-           gated, deterministic        feedback ──┐
                         linked, idempotent         ≥2-session threshold                   │
                              ▲                                                             │
                              └──────────────── what was missing (gap) ◀────────────────────┘
```

The defining choice: **observations do not become rules directly.** Capturing a finding is
cheap and judgment-light; turning findings into a generalizable rule is hard and needs
multiple examples. Splitting the two keeps the playbook small and high-signal, makes the
"≥2 distinct sessions" evidence bar structural rather than a convention, and keeps an LLM out
of the promotion path (the LLM *proposes*; deterministic CLI gates *decide*).

### Core concepts

- **Observation** — a situated, evidence-linked finding (`kind` = correction | pattern | gap
  | incident). Carries ≥1 evidence session id, an optional quote, domain tags, and a severity.
  Append-only; re-mining the same session is idempotent.
- **Rule** — a `{use_when, content, causal_note, domain, type}` record with a **lifecycle**
  (`draft → confirmed → stale`) and `observation_ids` provenance. `type` is `soft` (guidance)
  or `hard` (always surfaced for a matching domain).
- **Event log** — the canonical store. Every action (rule created/edited, observation,
  retrieval, selection, feedback, consolidation) is an append-only JSONL event. The playbook
  is a *derived projection* (a fold over the log) cached in `playbook.json`; delete it and
  `rebuild` any time.

---

## Setup

```bash
cd /path/to/your/repo
auto reflect init             # global settings + this repo's local state
# auto reflect init --project   # repo-local state only (events dir + playbook)
```

Everything defaults to **JSON on stdout**; pass `--format text` for human-readable output.
Diagnostics and errors go to stderr; every hard error carries a remediation hint.

---

## Command reference

### 1. Observe — capture working memory

```bash
auto reflect observation add \
  --kind gap \
  --subject "no guidance on scoping go build to a module" \
  --evidence-session "$SID" \
  --evidence-message "$SID-98" \
  --evidence-quote  "go build ./... rebuilt everything and was slow" \
  --domain go --severity normal        # kinds: correction|pattern|gap|incident

auto reflect observation list --kind gap --domain go --since 14d --unconsolidated
```

`--unconsolidated` shows only observations not yet folded into a rule.

**Evidence comes straight from `auto search`.** A reflecting agent mines sessions with
`auto search`, and each message-scope hit returns a `sessionId` and a `messageId` (format
`{sessionId}-{index}`). Pass those as `--evidence-session` and the optional `--evidence-message`
to pin an observation to the exact transcript moment — the durable, drift-resistant anchor is
the `--evidence-quote`. `--evidence-session` alone is fine for a whole-session observation;
repeat the trio (paired by position) to cite several moments.

### 2. Consolidate — observations → rules

Hand `consolidate` a **delta document**. The CLI gates deterministically: a `create-draft`
needs evidence spanning **≥2 distinct sessions** (or `--force`, or a high-severity
observation), dedupes against live rules, and flags conflicts. Use `--dry-run` to preview.

```bash
auto reflect consolidate - --dry-run <<'JSON'
{ "deltas": [
  { "op": "create-draft",
    "use_when": "editing go files",
    "content":  "After modifying a Go file, run go build ./... from that file's module dir",
    "causal_note": "unbuilt files hide compile errors that surface later as a batch",
    "domain": ["go"], "type": "soft",
    "observation_ids": ["ob-1a2b3c4d", "ob-5e6f7a8b"] } ] }
JSON
```

Delta ops: `create-draft`, `attach-evidence` (add provenance to an existing rule), `merge`,
`deprecate`. Output: `{applied, skipped (with reasons), conflicts}`.

Promote what earns it; retire what doesn't:

```bash
auto reflect rule promote <r-id>   # draft → confirmed; refuses if provenance < 2 sessions
auto reflect rule retire  <r-id>   # any → stale (never surfaced by retrieve again)
```

You can also hand-author a rule directly with `rule create` (and `rule list`/`get`/`edit`).

### 3. Retrieve loop — put rules to work

A two-phase retrieval (predicates first, content second) with a feedback gate so signal is
never silently dropped. Each step mints ids you thread to the next.

```bash
# 1. Predicates only (no content). Drafts surface flagged; --no-drafts → confirmed-only;
#    stale never surfaces. Domain-matched hard rules are always injected.
RT=$(auto reflect retrieve "add --json flag to auto env" --domain go,cli | jq -r '.[0].retrieval_id')

# 2. Commit to an ordering → reveals content, mints a feedback id per rule.
FB=$(auto reflect select "$RT" | jq -r '.[0].feedback_id')

# 3. ...do the work, using the rules...

# 4. Close the loop. rankings must cover every outstanding feedback id.
auto reflect feedback "{\"outcome\":\"success\",\"summary\":\"shipped\",
  \"rankings\":[{\"feedback_id\":\"$FB\",\"rank\":1,\"reason\":\"gave the exact pattern\"}],\"gap\":null}"

# 5. Gate: exits 0 only when no feedback ids are outstanding in scope.
auto reflect gate check
```

### 4. Read — signal & raw events

```bash
# Per-rule signal + the unconsolidated-observation backlog. Output is an OBJECT:
#   { "unconsolidated_observations": N, "rules": [ {rule_id, surfaced, selected,
#     selection_rate, feedback_count, rank_distribution, outcome_counts}, ... ] }
auto reflect stats                       # read rows from .rules[]

# Raw, read-only view over the canonical event log (newest-first).
auto reflect events list --type feedback --since 7d
auto reflect events list --type observation --type consolidation   # --type is repeatable
```

---

## Worked example (end to end)

```bash
# observe the same kind of pain in two different sessions
AUTO_SESSION_ID=s1 auto reflect observation add --kind gap --subject "go build scope" --evidence-session s1 --domain go
AUTO_SESSION_ID=s2 auto reflect observation add --kind gap --subject "go build scope" --evidence-session s2 --domain go

auto reflect stats | jq .unconsolidated_observations          # → 2

# consolidate: 2 sessions clears the gate → a draft rule with provenance
auto reflect consolidate "{\"deltas\":[{\"op\":\"create-draft\",\"use_when\":\"editing go files\",
  \"content\":\"run go build ./... from the module dir\",\"causal_note\":\"late build errors\",
  \"domain\":[\"go\"],\"type\":\"soft\",\"observation_ids\":[\"<ob1>\",\"<ob2>\"]}]}"

auto reflect stats | jq .unconsolidated_observations          # → 0 (both now consolidated)
auto reflect rule promote <r-id>                              # draft → confirmed
auto reflect retrieve "add a go flag" --domain go             # the rule now surfaces
```

A single-session `create-draft` is **skipped** with a reason (use `--force` to override); a
high-severity (incident) observation bypasses the threshold so one catastrophic event can
become a rule immediately.

---

## Data & storage

All state lives under `.auto/reflect/` in the repo (project-scoped, committed to git so rules
travel with the repo and are reviewable in PRs):

| Path | What |
|------|------|
| `.auto/reflect/events/` | Append-only canonical event log, sharded by host/day/worktree (concurrent appends never touch the same bytes) |
| `.auto/reflect/playbook.json` | Folded rule snapshot — a disposable cache; `rebuild` regenerates it |
| `~/.auto/reflect/settings.json` | Global settings |

Event types: `rule_created`, `rule_edited`, `retrieval`, `selection`, `feedback`,
`observation`, `consolidation`. Only `rule_created` / `rule_edited` mutate the rule
projection; observations and consolidation links never dirty the snapshot.

Session identity is auto-detected from the environment (`AUTO_SESSION_ID` override →
`CODEX_SESSION_ID` → `CLAUDE_SESSION_ID` → `CLAUDE_CODE_SESSION_ID`) so reflect events join to
the transcripts `auto etl` / `auto search` index.

---

## Architecture

```
internal/
  observations/  observation model, validation, id minting
  rules/         rule model, matching/scoring, fold (projection), snapshot load/rebuild
  consolidate/   the deterministic consolidation gates (threshold, dedupe, conflict)
  events/        append-only event log: envelope, sharding, append-with-seq, read/fold
  loop/          retrieval state machine (retrieve → select → feedback → gate) + stats fold
  store/         path helpers + JSONL/JSON file primitives (flock'd, atomic)
  cli/           cobra commands (json default, text via --format)
  app, config, gitutil, timefilter   shared plumbing
```

The design principle throughout: **events are canonical, rules are a derived fold.** LLM
judgment lives in the *calling skill* (which observation to record, how to cluster, how to
draft a rule); the CLI stays deterministic so the same log always folds to the same playbook.

---

## Where this fits

`auto reflect` is the learning layer of the auto-stack. The longer-term goal is autonomous
reflection runs: an agent mines `auto search` for recurring problems, records observations,
and a consolidation pass drafts rules for human promotion — closing the loop from session
history to a self-improving playbook. See
[`docs/epics/001-reflect-playbook-loop.md`](../docs/epics/001-reflect-playbook-loop.md) for
the roadmap and `docs/self-improving-playbook-retrieval.md` for the full design.
