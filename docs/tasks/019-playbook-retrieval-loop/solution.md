# Solution: Task 019

## Approach

1. **New `internal/events` package — the canonical store.** Append-only JSONL events under
   `.auto/reflect/events/<host>-<YYYY-MM-DD>-<wt8>.jsonl`, where host comes from
   `auto-shared/config.EnsureHost()` and `wt8` is the first 8 hex chars of SHA-256 of the absolute
   worktree root path. Host separates machines, day bounds file size, and the worktree discriminator
   isolates parallel same-host worktrees — two worktrees committing events on the same day produce
   *different* paths, so the merged tree simply contains both files and git never sees divergent
   tails on one path. The reader treats the shard name as opaque (walks the whole dir), so renamed
   checkout paths just start a new shard.

<!-- RESOLVED(P1): host+day sharding does not isolate same-host/same-day parallel worktrees
REVIEW: The stated motivation (here and AC-4, and the source design doc lines 369-372) is that
concurrent worktrees must never collide. host+day sharding only separates *different hosts* and
*different days*. The primary use case — parallel auto-stack implementers on ONE machine on ONE day
— all resolve to the identical shard filename `.auto/reflect/events/<host>-<YYYY-MM-DD>.jsonl`.
At the disk level they don't corrupt each other (each worktree is a separate checkout, and flock
guards the write), so "never append to the same file" is true per-worktree-on-disk. But the OTHER
half of the motivation — "collides on the file tail under git merge" (design doc line 370) — is NOT
solved: when both worktrees commit their divergent `<host>-<date>.jsonl` and merge back to main,
git sees the same path with divergent appended tails → merge conflict, exactly the failure the
sharding was introduced to prevent. The design doc itself offers the fix: "shard by host+day
(or session)". Session-scoped (or pid/worktree-discriminated) shard names would isolate concurrent
same-host worktrees; host+day alone cannot. Either change the shard key or document that parallel
same-host worktrees committing on the same day will conflict-on-merge (and revise AC-4's claim).
AUTHOR: Accepted — shard key changed to host+day+worktree: `<host>-<YYYY-MM-DD>-<wt8>.jsonl` with
wt8 = SHA-256(worktree root path)[:8]. Stable per worktree (no per-invocation file explosion, unlike
a session-keyed shard which would also need a fallback when no session id is detectable), and
distinct across parallel checkouts, so merge-back never collides on a path. AC-4 in requirements.md
reworded to make this the testable contract; total order tiebreak updated to (ts, shard, seq).
-->

   Every event is an
   envelope `{id, type, schema_version, seq, ts, host, session_id?, agent?, git: {hash, remote},
   payload}`. `seq` is allocated by a new `AppendEvent` primitive in `internal/events` modeled on
   `store.AppendJSONLine` but read-modify-write: open `O_RDWR`, take the same `flock(LOCK_EX)`, read
   the shard's last line to parse the current max `seq`, append the new event with `seq+1`, sync.
   (`store.AppendJSONLine` itself is O_APPEND-only and never reads, so it cannot allocate sequences —
   it stays as the pattern reference, not the call target; context.md updated to match.)
   `git.remote` passes through the existing `gitutil` sanitizer. Provenance degrades gracefully on
   a repo with no commits (unborn HEAD): the worktree root is resolved via `git rev-parse
   --show-toplevel` (which works pre-commit) through a new lenient variant of `gitutil.DetectRepo`,
   and `git.hash`/`git.remote` are simply empty — event writing never hard-fails just because the
   repo is fresh. Event

<!-- RESOLVED(P2): monotonic seq cannot be allocated by reusing AppendJSONLine "as-is"
REVIEW: context.md (line 17) lists `store.AppendJSONLine` as "reused as-is" and this paragraph says
"seq is monotonic per shard (allocated under the existing flock in store.AppendJSONLine)". I read
jsonl.go:15-51 — AppendJSONLine opens with O_APPEND, takes LOCK_EX, and writes; it never READS the
file, so it has no way to know the current max seq to increment. Allocating a monotonic per-shard
seq requires a read-modify-write (scan existing lines for the last seq, then append) under the SAME
lock — which AppendJSONLine does not do. So either (a) AppendJSONLine must be extended/forked into a
new "append-with-seq" primitive (it is then NOT reused as-is — update context.md), or (b) seq is
derived at read time (line number), but then it is not persisted in the event and AC-4's "events
carry ... monotonic sequence" fails. Please specify which, and what reads the prior max under lock.
AUTHOR: Option (a). New `events.AppendEvent` does read-modify-write under flock(LOCK_EX): read last
line of the shard → parse max seq → append event with seq+1 → sync. Persisted seq, so AC-4 holds.
context.md's "reused as-is" entry corrected — AppendJSONLine is the locking/append pattern reference
for the new primitive, not the call target for event writes.
-->

   types: `rule_created`, `rule_edited`, `retrieval`, `selection`, `feedback`. Reader walks the
   events dir and yields a deterministic total order `(ts, shard-name, seq)`. Session/agent detection
   moves from the deleted feedback package into `events` (env: `AUTO_SESSION_ID` →
   `CODEX_SESSION_ID` → `CLAUDE_SESSION_ID`, `--session` flag overrides).

2. **Rewrite `internal/rules` as a projection.** New schema
   `{id, domain[], use_when, content, causal_note, rule_type: hard|soft, lifecycle: draft|confirmed|stale,
   version, created_at, updated_at}` — rule IDs keep `^r-[0-9a-f]{8}$`, domain entries keep the tag
   pattern. `Fold(events) → Playbook` applies `rule_created`/`rule_edited` in order;
   a `rule_edited` payload is one atomic edit: `{rule_id, from_version, to_version,
   deltas: [{field, old, new}]}` — a multi-field edit is ONE event with several deltas and one
   version bump, so sibling field changes can never masquerade as a concurrent-edit conflict.

   Fold conflict rule: edit events are applied in total order `(ts, shard-name, seq)` regardless of
   `from_version` — on a mismatch (two worktrees both edited from v3, merged) the later event still
   applies all its deltas, the rule's version increments by one from the *current* folded version,
   and the fold records a conflict entry `{rule_id, fields, expected: from_version, actual}`. Deterministic
   last-writer-wins; nothing is lost (both edits remain in the log and reconstructable history), and
   `rebuild` prints any conflicts to stderr so a human can review the losing edit.

<!-- RESOLVED(P3): Fold behavior on from_version mismatch (concurrent edits) is unspecified
REVIEW: rule_edited carries from_version/to_version, which implies the fold validates the delta
applies to the expected version. Two worktrees can both edit r-x from v3 → produce two `to_version:4`
deltas; after merge the fold sees two deltas both claiming from_version=3. What does Fold do — apply
both (last-writer-wins, silently losing one edit)? skip the second and warn? error? AC-5 says "full
history reconstructable" but doesn't define conflict resolution. This is the same parallel-worktree
scenario as the sharding comment above. Please state the fold's behavior when from_version doesn't
match the current folded version (and whether `rebuild` surfaces such conflicts).
AUTHOR: Specified above: apply in deterministic total order ignoring from_version for applicability,
version increments from current folded version, conflict entries collected and printed to stderr by
`rebuild`. Last-writer-wins is deterministic (same total order everywhere) and loses no history —
the overwritten edit stays in the log, satisfying AC-5's reconstructability.
-->

   Snapshot `playbook.json` (committed to git, human-reviewable in PRs) carries
   `{schema_version, folded_through: {shard → seq of last RULE event folded}, rules}`. Staleness is
   computed **only over `rule_created`/`rule_edited` events**: a command that reads rules compares
   `folded_through` against each shard's max *rule-event* seq and refolds only when a rule event is
   newer — `retrieval`/`selection`/`feedback` appends never dirty the snapshot, so loop traffic
   produces zero churn on the committed file. `auto reflect rebuild` forces a refold.

<!-- RESOLVED(P2): does folded_through track all events or only rule-affecting ones?
REVIEW: The snapshot (`playbook.json`) is committed to git. `folded_through: {shard → seq}` is
compared against the log to decide staleness. But `retrieval`, `selection`, and `feedback` events
are appended to the same shard files and advance each shard's max seq WITHOUT changing the rule
projection. If `folded_through` is "last seq per shard across all event types", then every single
`retrieve`/`get`/`feedback` makes the snapshot appear stale → triggers a refold → rewrites
`playbook.json`. Since playbook.json is committed, that means a spurious git diff on the committed
snapshot on essentially every loop invocation (the byte content is identical except folded_through),
producing constant churn and merge noise. Staleness should be computed only over rule_created/
rule_edited events (or folded_through should record the last *rule* seq, not the last seq). Please
clarify so reads don't rewrite the committed snapshot on non-rule events.
AUTHOR: Accepted — folded_through now records the seq of the last rule_created/rule_edited event per
shard, and staleness compares only rule-event seqs. Non-rule events never trigger a refold or
snapshot rewrite. Text updated above.
-->

   Matching: optional `--domain` filter is **ANY-of** — a rule is kept when `rule.domain` intersects
   the (normalized, deduped) flag list. Then normalized-keyword scoring over `use_when`
   (3 pts/keyword) + `domain` (1 pt/keyword) — same normalize/dedupe helpers as today's lookup.
   Hard-rule injection: the *match set* for hard rules is `--domain` list when provided, otherwise
   the normalized intent keywords; any `hard` rule whose `domain` intersects that set is appended to
   results regardless of score, flagged `hard_injected: true`. To make the guarantee meaningful,
   `validate()` requires `rule_type: hard` rules to declare ≥1 domain at create/edit time — a hard
   rule with no domain would be unreachable and is rejected with remediation.

<!-- RESOLVED(P3): "--domain exact filter" and "domain-matched hard rules" need defined semantics
REVIEW: Two undefined points the implementer will otherwise guess at:
1. `--domain go,cli` exact filter — is a rule kept if it matches ANY of the listed domains or ALL of
   them? (rule.domain is a list, the flag is a list.) AC-1 just says "domain filter". State any-vs-all.
2. "Domain-matched hard rules always included" — matched against what when the caller passes NO
   --domain flag? If hard injection keys off the --domain flag, then `retrieve "<intent>"` with no
   --domain surfaces zero hard rules, contradicting the goal "hard rules always surfaced". If it keys
   off keyword overlap between intent and rule.domain, define that. A hard rule with an empty/non-
   matching domain never surfaces under either reading — is that intended? The whole v1 hard-rule
   guarantee (Out of Scope defers *enforcement* but keeps "always surfaced") hinges on this.
AUTHOR: Defined above: (1) ANY-of intersection. (2) Hard injection matches rule.domain against the
--domain list when given, else against normalized intent keywords — so no-flag retrieves still
surface hard rules whose domain terms appear in the intent. The empty-domain hole is closed at the
source: validation rejects hard rules with no domain. Note "always surfaced" was always
domain-scoped per the design doc (Phase 1: "Hard rules that match on domain are always returned");
a hard rule for an unrelated domain staying silent is intended.
-->


3. **New `internal/loop` package — the agent-facing state machine.**
   - `Retrieve(intent, domains, limit)`: match, mint `rt-[0-9a-f]{8}` per result, append one
     `retrieval` event with items `[{retrieval_id, rule_id, rule_version, match_score, hard_injected}]`,
     return `[{retrieval_id, use_when, domain, rule_type}]` — no content.
   - `Select(orderedRetrievalIDs)`: resolve ids against retrieval events (unknown id → structured
     error + remediation; an `r-` or `fb-` prefixed argument gets a specific "wrong id type, you
     probably want `rule get` / already-selected" message), mint `fb-[0-9a-f]{8}` per rule, append
     one `selection` event preserving order, return `[{feedback_id, content, causal_note,
     rule_type}]` in the same order.
   - `SubmitFeedback(payload)`: JSON document (positional arg or stdin):
     ```json
     {
       "outcome": "success|partial|fail|abandoned",
       "summary": "...",
       "rankings": [{"feedback_id": "fb-…", "rank": 1, "reason": "..."}],
       "gap": {"report": "...", "moment": "what I was doing when I needed it"}  // or "gap": null
     }
     ```
     Validation (shared `validate()` style, `[]config.ValidationError`): outcome enum; rankings must
     cover exactly the outstanding feedback_ids for the session scope, ranks a permutation of 1..N,
     reason non-empty per id; if `gap` present both `report` and `moment` non-empty (grounding rule).
     Only a fully valid payload appends the `feedback` event.
   - `GateCheck(scope)`: outstanding = feedback_ids from `selection` events minus those covered by
     `feedback` events, scoped to the detected/passed session. If no session is detectable, the
     fallback scope is bounded: selections from **this host AND this worktree's shards AND within a
     lookback window** (default 24h, `--since` override) — ancient orphans from crashed/abandoned
     sessions can't wedge the gate for everyone. Two escape hatches for orphans: `feedback
     --session <id>` closes another session's loop explicitly, and the outcome enum gains
     `abandoned` (`success|partial|fail|abandoned`) so closing a dead session doesn't pollute the
     fail signal — degenerate-session filtering downstream gets a clean discriminator. Clean →
     exit 0; otherwise exit non-zero, stderr lists outstanding ids + the exact `auto reflect
     feedback …` remediation. A session that consumed no rules passes.

<!-- RESOLVED(P2): committed events + repo-scope fallback = permanently-unclosable gate
REVIEW: selection/feedback events are committed to git AND the gate's no-session fallback scopes to
"the whole repo log". Consequence: any session that ran `get` (minting fb-ids) but never submitted
feedback — agent crashed, branch abandoned, gate skipped before this command existed — leaves those
fb-ids outstanding in the committed log forever. The originating session is gone, so those ids can
never be closed. Any later `gate check` invoked WITHOUT a detectable AUTO_SESSION_ID (e.g. a CI hook
or complete-task running in an env that doesn't set it) then falls back to repo scope and fails
citing ancient un-closable ids — the gate is wedged for everyone. Need either: a way to scope the
fallback (e.g. only the current git HEAD/branch's sessions, or only sessions since a marker), an
expiry/"abandon" path for stale outstanding ids, or making "no detectable session" a pass rather
than a scan-everything fail. As specified, AC-3b's repo-scope fallback is a foot-gun.
AUTHOR: Accepted, fixed with both suggested mechanisms: (1) fallback scope narrowed to this host +
this worktree's shards + 24h lookback (--since override) — old/foreign orphans never block; (2)
explicit close path via `feedback --session <id>` plus a new `abandoned` outcome value so orphan
cleanup is distinguishable from real failures in later analysis. "No session → pass" was rejected:
it would make the gate trivially skippable in exactly the hook/CI contexts it exists for.
-->

   - `Stats()`: fold → per-rule `{surfaced, selected, selection_rate, feedback_count}` JSON. Every
     rule in the playbook is listed (per the list-commands-return-all convention);
     `selection_rate` is defined as `0` when `surfaced == 0` — unambiguous next to the visible
     `surfaced: 0`, and never produces a NaN for `encoding/json` to choke on.

<!-- RESOLVED(P3): selection_rate = selected/surfaced is undefined (NaN) for a never-surfaced rule, and Go's json.Marshal errors on NaN
REVIEW: `selection_rate` is selected/surfaced (README shows 11/14 = 0.79). If `stats` reports every
rule in the playbook (not just surfaced ones), a rule with surfaced=0 yields 0/0 = NaN. `encoding/json`
returns an error marshaling NaN (`json: unsupported value: NaN`), which would break the JSON-stdout
guarantee the whole tool rests on — not just produce an ugly number. Specify: either stats omits
never-surfaced rules, or selection_rate is defined as 0 (or null) when surfaced==0. Cheap to get
right; easy to miss in a unit test that only feeds surfaced rules.
AUTHOR: Specified: stats lists ALL rules (CLAUDE.md: list/read commands return all items when no
filters given), with selection_rate hard-defined to 0 when surfaced==0. Plan Step 3.1's verify now
includes a never-surfaced rule marshaling cleanly.
-->


4. **CLI surface (delete old, add new).** Remove `lookup`, old `rule create`, and the legacy
   feedback annotation commands. New tree:
   `rule create|edit|list|get`, `retrieve <intent>`, `select <retrieval_id…>`,

<!-- RESOLVED(P3): `get` and `rule get` are two different commands with different ID types
REVIEW: The tree defines both `auto reflect rule get <r-id>` (full-fidelity rule by r-… id) and a
top-level `auto reflect get <rt-id…>` (loop content-fetch by rt-… id). Same verb, two meanings,
two ID namespaces (r- vs rt-). Root CLAUDE.md → "Prefer explicit CLI surfaces: one clear command;
avoid ambiguous aliases." An agent (or human) can easily run `rule get` when they meant `get` or
pass an r- id to the loop `get`. Consider renaming the loop fetch to something unambiguous (e.g.
`fetch`/`reveal`/`select <rt-id…>`) so the two-phase loop verb is distinct from the resource `get`.
At minimum, the unknown-id error must detect "you passed an r- id to `get`" and remediate.
AUTHOR: Renamed to `select <retrieval_id…>` — it's the design doc's own Phase 2 name ("Select") and
describes what the agent is doing (committing to its interest ordering), leaving `get` solely to the
resource triad (`rule get`). Service method, CLI file (select.go), requirements.md goal/AC-2 prose,
and the loop-flow artifact all updated. The wrong-id-type remediation is also specified in the
Select() bullet above (r-/fb- prefixes get targeted messages).
-->

   `feedback <json|->` (submit), `gate check`, `stats`, `rebuild`, updated `init` (creates
   `events/` dir + empty snapshot; no `feedback.jsonl`) and a rewritten `quickstart` that walks the
   whole loop (AC-7 driver). All commands: JSON default, `--format text`, payload-only stdout,
   diagnostics to stderr, `ExitError` codes — per `docs/auto-package-patterns.md`.

5. **Tests at three levels** (see table). E2E follows the existing built-binary harness
   (`TestMain` builds once); the AC-7 test follows the `quickstart` sequence in a temp git repo —
   parsing each command's JSON output to capture the minted `rt-`/`fb-` ids and threading them into
   the next command, exactly as an agent would — and asserts events land on disk and the gate flips.
   `quickstart` itself shows the id-capture step explicitly (e.g. `... | jq -r '.[0].retrieval_id'`)
   so an agent following the doc knows where the ids come from.

<!-- RESOLVED(P3): "verbatim" quickstart execution is impossible for the get/feedback steps
REVIEW: `retrieve` mints rt-… ids and `get` mints fb-… ids at runtime (random hex), so the
`get rt-… rt-…` and `feedback '{… "feedback_id":"fb-…" …}'` lines in quickstart cannot contain
literal ids — they must be captured from the prior command's JSON and substituted. So the e2e test
cannot run quickstart "verbatim"/"commands … verbatim" (this line and the Test Coverage AC-7 row);
it must parse retrieve→get output and thread the minted ids through. Reword to "follows the
quickstart sequence, substituting the minted retrieval_ids/feedback_ids". Also: quickstart prose
should show the id-capture step explicitly (e.g. via jq) so a real agent following the doc knows
where the ids come from.
AUTHOR: Reworded here and in the AC-7 test row ("quickstart sequence, minted ids threaded"), and
added the requirement that quickstart itself demonstrates id capture (jq example) so the doc string
is honestly followable end to end.
-->


## Files

```
+ auto-reflect/internal/events/model.go        # envelope, payload types, schema_version, validation
+ auto-reflect/internal/events/shard.go        # shard naming (<host>-<date>-<wt8>.jsonl)
+ auto-reflect/internal/events/log.go          # AppendEvent (read-max-seq+append under flock, sanitized git provenance), ReadAll w/ (ts,shard,seq) order
+ auto-reflect/internal/events/session.go      # detectSessionID/detectAgent (moved from feedback pkg)
~ auto-reflect/internal/rules/model.go         # new Rule schema; Playbook + folded_through
~ auto-reflect/internal/rules/validate.go      # validate new fields (enums, domain tags, non-empty causal_note)
+ auto-reflect/internal/rules/projection.go    # Fold(events) -> Playbook; field-delta application
+ auto-reflect/internal/rules/snapshot.go      # snapshot read/write, staleness check, rebuild
+ auto-reflect/internal/rules/match.go         # domain filter + keyword scoring + hard-rule injection
- auto-reflect/internal/rules/lookup.go        # replaced by match.go
- auto-reflect/internal/rules/store.go         # replaced by projection.go + snapshot.go (newRuleID moves to events ids)
+ auto-reflect/internal/loop/service.go        # Retrieve/Select/SubmitFeedback/GateCheck/Stats
+ auto-reflect/internal/loop/feedback.go       # feedback payload schema + validate()
- auto-reflect/internal/feedback/              # legacy annotations deleted (model, service, validate, span)
~ auto-reflect/internal/store/paths.go         # + EventsDir(); drop FeedbackPath
~ auto-reflect/internal/cli/root.go            # new command registration
~ auto-reflect/internal/cli/rule.go            # create/edit/list/get on new schema (edit = field deltas, version bump)
+ auto-reflect/internal/cli/retrieve.go        # auto reflect retrieve <intent> [--domain a,b] [--limit n]
+ auto-reflect/internal/cli/select.go          # auto reflect select <retrieval_id...>
~ auto-reflect/internal/cli/feedback.go        # rewritten: feedback <json|-> [--session id] gate submission
+ auto-reflect/internal/cli/gate.go            # auto reflect gate check [--session id] [--since dur]
+ auto-reflect/internal/cli/stats.go           # auto reflect stats
+ auto-reflect/internal/cli/rebuild.go         # auto reflect rebuild
~ auto-reflect/internal/cli/init.go            # events dir + snapshot; no feedback.jsonl
~ auto-reflect/internal/cli/quickstart.go      # full-loop walkthrough (AC-7 source of truth)
~ auto-reflect/internal/cli/cli_integration_test.go  # rewritten for the loop
- auto-reflect/cmd/autoreflect/e2e_feedback_test.go  # legacy annotations e2e
+ auto-reflect/cmd/autoreflect/e2e_loop_test.go      # AC-7 quickstart-driven e2e
```

`internal/config`, `internal/timefilter`, `internal/app`, `rootcmd` are unchanged.
`internal/gitutil` gains one small addition — a lenient repo-detect (root via
`rev-parse --show-toplevel`; empty head/tree/remote when HEAD is unborn) consumed by `events`;
the sanitizer is reused untouched.

## Test Coverage

| AC    | Test Type   | File                                                  |
|-------|-------------|-------------------------------------------------------|
| AC-1  | integration | auto-reflect/internal/cli/cli_integration_test.go     |
| AC-1  | unit        | auto-reflect/internal/rules/match_test.go (hard injection, scoring) |
| AC-2  | integration | auto-reflect/internal/cli/cli_integration_test.go (order preserved, selection event) |
| AC-3  | unit        | auto-reflect/internal/loop/feedback_test.go (payload validation matrix) |
| AC-3  | integration | auto-reflect/internal/cli/cli_integration_test.go (incomplete → non-zero + remediation) |
| AC-3b | integration | auto-reflect/internal/cli/cli_integration_test.go (gate open → closed; no-rules session passes) |
| AC-4  | unit        | auto-reflect/internal/rules/projection_test.go (fold determinism), internal/events/log_test.go (order, sharding) |
| AC-4  | integration | auto-reflect/internal/cli/cli_integration_test.go (delete snapshot → rebuild identical) |
| AC-5  | unit        | auto-reflect/internal/rules/projection_test.go (field deltas, version history reconstruction) |
| AC-6  | integration | auto-reflect/internal/cli/cli_integration_test.go (stats after 2 sessions) |
| AC-7  | e2e         | auto-reflect/cmd/autoreflect/e2e_loop_test.go (built binary, quickstart sequence with minted ids threaded) |

## Out of Scope

- Probe injection, fresh-agent Phase 5 reviewer, signal-driven triage, A/B versions, contrastive
  loop, ablation calibration, cluster summaries (>30 matches), population broadcast.
- `confidence` field and decay.
- Automated hard-rule *enforcement* (compliance checking) — v1 only guarantees hard rules surface.
- BM25/embedding retrieval quality; keyword scoring only.
- Acting on gap reports (gap-to-rule pipeline) — capture only.
- Migration of existing `playbook.json` rules or `feedback.jsonl` annotations — old files are left
  on disk untouched but unread; old commands are deleted.
- Global scope (`~/.auto/reflect/`) rules/events.
- Stop-hook/skill wiring of `gate check` — consumers integrate it.
- Event-log compaction/archival; multi-host merge tooling (sharding makes appends merge-safe, which
  is enough for v1).

## Rejected Alternatives

- **Minted loop handle threaded by the agent**: an agent that loses the handle orphans the loop and
  the gate still needs a scan-everything fallback — env-session + repo fallback gets the same
  grouping without new state.
- **Repeated-flag feedback submission** (`--rank fb-x=1 --reason fb-x=…`): pairing nested
  rank/reason data through flag parsing is error-prone; a validated JSON payload keeps the gate
  atomic (all-or-nothing per AC-3).
- **Gitignored snapshot**: events-only in git would make PR review of rules unreadable; committed
  snapshot is the design doc's stated intent and the fold is deterministic, so drift is detectable.
- **Keeping legacy feedback annotations alongside**: two parallel feedback stores under one noun;
  user confirmed the old system is barely used — deleted instead.
- **Single `log.jsonl` event file**: concurrent worktrees/hosts collide on the file tail under git
  merge — sharding is the design doc's own NFR (its "host+day (or session)" key extended to
  host+day+worktree per review, since host+day alone still collides for same-host parallel worktrees).
- **Migrating old rules into the new schema**: old schema lacks `use_when`/`causal_note` (the fields
  the whole loop turns on); auto-filled placeholders would seed junk. User confirmed delete/rebuild.
