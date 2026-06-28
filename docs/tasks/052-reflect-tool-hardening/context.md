# Context: Task 052 — reflect-tool-hardening

Codebase grounding for the high-priority `auto reflect` fixes planned in
[plan.html](plan.html). All paths relative to repo root. Module:
`auto-reflect/` (Go, cobra). Events are append-only JSONL under
`.auto/reflect/events/`; `playbook.json` is a disposable folded cache.

## Key Files

### Event model & store
- `auto-reflect/internal/events/model.go:63-74` — `Event` envelope: `id`
  (`^ev-[0-9a-f]{8}$`), `type`, `schema_version` (const `SchemaVersion = 1`,
  line 14), `seq`, `ts`, `host`, `session_id`, `git`, `payload` (raw JSON).
- `auto-reflect/internal/events/model.go:139-158` — `FeedbackPayload`
  (`outcome`, `summary`, `rankings[]`, `gap`) and `FeedbackGap` (`report`,
  `moment`). **The gap data F4 must surface lives here**, inside `feedback`
  events.
- `auto-reflect/internal/events/model.go:178-188` — `ObservationPayload`
  (`observation_id` `^ob-[0-9a-f]{8}$`, `kind`, `subject`, `evidence[]`,
  `domain`, `severity`). Note: observation `kind` may be `gap` — a *different*
  concept from a feedback gap (clarify in docs).
- `auto-reflect/internal/events/log.go` — `events.ReadAll(root)` reads all
  shards; `newEventID()` mints `ev-` ids from `crypto/rand`; `AppendEvent`
  appends one event. `EventsDir` resolved via `store.EventsDir`.

### CLI plumbing
- `auto-reflect/internal/cli/root.go:55-83` — command tree assembly
  (`cmd.AddCommand(...)`); **new `gap` and `doctor` nouns register here**.
  No `doctor` command exists today.
- `auto-reflect/internal/cli/root.go:85-100` — `writeJSON` helper +
  `normalizeFormat` (`--format json|text`). Every command marshals through
  `writeJSON`.
- `auto-reflect/internal/cli/events.go:52-160` — `events list`: the closest
  pattern to mirror for `gap list`. Builds an `eventView` that **intentionally
  drops `payload`** (line ~30-32) and a `summarizePayload` one-liner (line
  ~188-235); supports `--since/--after/--before` via `timefilter`. Output:
  `{"scope":"repo","events":[...]}`.
- `auto-reflect/internal/cli/validation.go` — `writeValidationErrors` renders
  the shared `{code,field,message,value}` contract to **stderr**.

### F2 — pending-count divergence
- `auto-reflect/internal/cli/miner.go:280-309` — `miner status` computes
  `pending` by reading sessions + events, folding coverage, filtering
  subagents + scope, counting sessions whose `MaxTerminalVersion < miner.Version`.
- `auto-reflect/internal/miner/miner.go:221-276` — `PendingCount` does the
  **same computation independently** (this is the duplication that drifts).
- `auto-reflect/internal/loop/service.go:366-375` — `Stats()` calls
  `miner.PendingCount(...)` → surfaces as `pending_to_mine`. Fix: collapse
  `miner status` onto `miner.PendingCount` so there is one counting function.

### F4 — `gap list`
- Mirror `events list` (above) + `observation list`
  (`auto-reflect/internal/cli/observation.go:109-219`) for flag shape. New
  `cli/gap.go`: filter `TypeFeedback` events, decode `FeedbackPayload`, skip
  when `Gap == nil`, project `{id (feedback_id or event id), ts, session_id,
  report, moment}`. Per resource pattern, `list` only (gap has no large body).

### F5 — output envelopes (current shapes)
- `observation add` → `{created,scope,observation}` (`cli/observation.go:83`);
  `observation list` → `{scope,observations[]}` (`:203`).
- `rule create/edit/promote/retire/graduate` → `{<verb>:true,scope,rule}`
  (`cli/rule.go:99,230,565`); `rule list` → `{scope,rules[]}` (`:307`);
  `rule get` → bare `Rule` (`:346`).
- `retrieve` → **bare array** `[...]` (`cli/retrieve.go:46`); `select` → **bare
  array** `[...]` (`cli/select.go:36`). Highest-risk consumers for a breaking
  envelope change.
- `consolidate` → `{applied[],skipped[],conflicts[],dry_run}`
  (`cli/consolidate.go:107-112`); `rebuild` → `{rebuilt,rule_count,conflict_count}`
  (`cli/rebuild.go`); `feedback` → `{recorded:true}` (`cli/feedback.go:59`);
  `miner status`/`stats` → bare objects.

### F6 — rule timestamps (root cause found)
- `auto-reflect/internal/rules/projection.go:66-67,102` — `Fold` **does** set
  `CreatedAt`/`UpdatedAt` from `ev.TS`. Not the bug.
- `auto-reflect/internal/cli/rule.go:298-305` — `rule list` builds a
  hand-rolled map that **omits `created_at`/`updated_at`** → `jq .created_at`
  is `null`. This is the F6 "null". `rule get` (`:346`) marshals the full
  `Rule` and is fine.
- `auto-reflect/internal/cli/consolidate.go:201,219-220` — `consolidate
  --dry-run` returns the `candidate` rule, which has no timestamps (no event
  exists yet) → the F6 `""`. Arguably correct; decide whether to omit.

### F7 — content-derived ids (confirmed needs code change)
- `auto-reflect/internal/rules/model.go:84-90` — `NewRuleID()` = `crypto/rand`.
- `auto-reflect/internal/observations/model.go:96-102` — `NewObservationID()` =
  `crypto/rand`.
- `auto-reflect/internal/loop/service.go:419-432` — `newRetrievalID`/
  `newFeedbackID` via `newPrefixedID` = `crypto/rand` (ephemeral loop ids).
- `auto-reflect/internal/cli/consolidate.go:202,485` — `createDraft`/`split`
  mint via `NewRuleID()`. Within one process dry-run==apply, but the spike ran
  them as **separate invocations** → different random ids (the F7 pain).
- `auto-reflect/internal/rules/projection.go:52-55` — a `rule_created` for an
  **existing id is ignored** ("original wins"). Content-derived ids therefore
  make re-running an identical consolidate **idempotent** — a key synergy.

### F9 — init writes `.gitignore`
- `auto-reflect/internal/store/paths.go:17-35` — `EnsureStateDir`,
  `EventsDir`, `PlaybookPath` (state dir `.auto/reflect`).
- `auto-reflect/internal/cli/init.go:42-64` — init flow; seam for an
  idempotent `.gitignore` write is right after `os.MkdirAll(eventsDir,...)`.

### F1 — doctor / staleness
- `auto-reflect/internal/loop/feedback.go:197-219` — `gate check` is the
  closest structured-result pattern in-module.
- Template to copy: `auto-graph/internal/cli/doctor.go` —
  `{check,status(pass/fail/warn),message,hint}` JSON, exit 1 on any fail.

### F8/F3 — docs
- `auto-reflect/internal/loop/feedback.go:17-30` — `outcome` enum
  `success|partial|fail|abandoned` (distinct from `miner ack` status
  `mined|empty|failed|skipped`).
- `auto-reflect/internal/miner/score.go:16-57` (line 53) — `correction_density`
  = corrections per 100 user messages (regex over user-role messages), weight
  0.4 in priority score.
- `auto-reflect/internal/cli/quickstart.go` — embedded happy-path doc; carries
  `jq` id-threading examples and the `stats` shape (update for F5/F4/F8/F3).

### F11 — legacy yaml
- `docs/reflection/observations.yaml` (389 KB) exists; **no Go code reads it**
  (`grep observations.yaml auto-reflect/**/*.go` → no matches). Safe to delete.

## Patterns
- **Output:** JSON default, 2-space indent; stdout = payload only, diagnostics
  + remediation hints → stderr; exit non-zero on any error even with partial
  results. (`CLAUDE.md` Cross-Project Guidance; `docs/auto-package-patterns.md:297-313`)
- **Resource triad** (`docs/auto-package-patterns.md:239-295`): `list` returns
  ids + metadata (no bodies), `get <id>` is full fidelity, `search` for ID-less
  discovery; truncation prints the exact recovery command. → `gap list` is
  list-only; `rule list` keeping content out is correct (but timestamps are
  metadata and belong in list — the F6 fix).
- **Validation:** one shared `validate()` returning `[{code,path,field,message,value?}]`;
  every hard error carries a remediation hint. (`CLAUDE.md`)
- **init idempotency / `.gitignore`:** mirror `auto-doc/internal/commands/init.go:82-89`
  — stat-then-skip-if-exists write of a per-state-dir `.gitignore`; ignore
  disposable artifacts (`playbook.json`) only, never the canonical `events/`.
- **Append-only invariant:** `events.SchemaVersion=1` must fold identically
  forever; F5 changes CLI *output* only — no payload field renames, no schema
  bump (plan.html D-5). Ids keep `^(ob|r)-[0-9a-f]{8}$` format; only minting
  changes (D-6).
- **Ubiquitous language:** Observation, Rule, Event, Playbook
  (`docs/concepts/UBIQUITOUS_LANGUAGE.md`).

## Related Tasks

- **019 playbook-retrieval-loop** (`48ef1e1`, #64) — laid the append-only event
  log, id minting (`rules.NewRuleID`/`newPrefixedID`, random-prefixed, NOT
  content-derived), and per-command `writeJSON` map shapes 052 reworks.
  Gap/feedback events (`events/model.go` `FeedbackGap`) are **capture-only by
  design**; structured gap read-back was explicitly deferred — so F4 adds it
  net-new (not completing a stub). Feedback lesson: run full `make
  check`/golangci every phase; verify CLI with `--help` before documenting flags.
- **023 reflect-miner-queue** (`11e43a4`, #77) — defines the pending-count
  semantics F2 must preserve: `miner.PendingCount` (`miner/miner.go:223`) =
  in-scope top-level sessions not terminal at current version (`failed` is
  retryable). **AC contract: `coverage_pct` is `null` (never 0/100) when
  `total_sessions==0`; source-state must not be reported as coverage** — the F2
  collapse and F1 doctor must keep the null-vs-zero distinction. CLI `miner
  status` (`cli/miner.go:280`) hand-rolls a pending loop separate from
  `PendingCount` — exactly the duplication F2 removes.
- **049 reflect-audit-lineage-lint** (`8a3cf41`, #110) — validation/doctor
  conventions to mirror: one shared `ValidateRule` (`rules/validate.go`) +
  `validateEvidence` (`observations/model.go`) returning structured
  `{code,path,field,message,value}` errors; exact regex for ids/hashes;
  lifecycle gating (`enforced` ⇒ `lint_ref`). Lesson: adding to a shared
  valid-set has wide blast radius; validation gates have cross-layer blind spots.

No prior attempt exists for content-derived ids, an output envelope, a reflect
`doctor`, or .gitignore-on-init — all four 052 pillars are greenfield (no
reverted attempts). The only `doctor` reference is
`auto-graph/internal/cli/doctor.go`.

_Line drift since first pass (paths all verified to exist):_ projection
`CreatedAt` L66; rule-list map L296; consolidate `NewRuleID` L202/L485; miner CLI
pending L280; `PendingCount` `miner/miner.go:223`.

## Doc/skill update surface (F4/F5/F8/F3)
- `.claude/skills/reflect-playbook/{SKILL.md,references/{mine,observe,consolidate,retrieve}.md}`
  — id-extraction `jq` paths (F5), gap-grep → `gap list` (F4), outcome enum +
  correction_density (F8/F3), observation-`kind gap` vs feedback-gap clarity.
- `auto-reflect/internal/cli/quickstart.go` — same, embedded.
- `auto-reflect/CLAUDE.md` + autodoc index — add `gap`/`doctor` (run `auto doc fix`).
