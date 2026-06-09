# Plan: Task 019

## Summary

Build the event-sourced store bottom-up in four serial, compile-safe phases: events package first
(standalone), then the rules-projection rewrite *with* its CLI cutover (the old rule/lookup commands
can't compile against the new schema, so they change together), then the loop package + loop CLI +
legacy-feedback deletion, then e2e + docs + full quality gate.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| + | auto-reflect/internal/events/model.go | Envelope (`id,type,schema_version,seq,ts,host,session_id?,agent?,git,payload`), payload types, validation |
| + | auto-reflect/internal/events/shard.go | Shard naming `<host>-<YYYY-MM-DD>-<wt8>.jsonl` (wt8 = SHA-256(worktree root)[:8]) |
| + | auto-reflect/internal/events/log.go | `AppendEvent` (read max seq + append seq+1 under flock), `ReadAll` in `(ts, shard, seq)` order, sanitized git provenance |
| + | auto-reflect/internal/events/session.go | `DetectSessionID`/`DetectAgent` (moved from legacy feedback pkg) |
| ~ | auto-reflect/internal/gitutil/git.go | + lenient repo-detect (root via `--show-toplevel`; empty head/tree/remote on unborn HEAD) |
| ~ | auto-reflect/internal/store/paths.go | `+ EventsDir()`; drop `FeedbackPath()` (phase 3) |
| ~ | auto-reflect/internal/rules/model.go | New Rule schema; Playbook + `folded_through: {shard → last rule-event seq}` |
| ~ | auto-reflect/internal/rules/validate.go | New-field validation (enums, domain tags, non-empty use_when/content/causal_note, hard ⇒ ≥1 domain) |
| + | auto-reflect/internal/rules/projection.go | `Fold(events) → Playbook`; field deltas; conflict entries on from_version mismatch (last-writer-wins) |
| + | auto-reflect/internal/rules/snapshot.go | Snapshot read/write (atomic), rule-event-only staleness, rebuild |
| + | auto-reflect/internal/rules/match.go | ANY-of domain filter, keyword scoring (use_when=3, domain=1), hard-rule injection |
| + | auto-reflect/internal/rules/testdata/ | Checked-in golden event fixture (multi-shard, conflicts, mixed types) + `playbook.golden.json` |
| - | auto-reflect/internal/rules/lookup.go | Replaced by match.go |
| - | auto-reflect/internal/rules/store.go | Replaced by projection.go + snapshot.go |
| + | auto-reflect/internal/loop/service.go | `Retrieve`/`Select`/`SubmitFeedback`/`GateCheck`/`Stats` |
| + | auto-reflect/internal/loop/feedback.go | Feedback payload schema + `validate()` (rank permutation, gap grounding, outcome enum incl. `abandoned`) |
| - | auto-reflect/internal/feedback/ | Legacy annotations package deleted |
| ~ | auto-reflect/internal/cli/root.go | New command registration |
| ~ | auto-reflect/internal/cli/rule.go | `rule create\|edit\|list\|get` on new schema |
| + | auto-reflect/internal/cli/retrieve.go | `retrieve <intent> [--domain a,b] [--limit n]` |
| + | auto-reflect/internal/cli/select.go | `select <retrieval_id…>` (wrong-id-type remediation for `r-`/`fb-` args) |
| ~ | auto-reflect/internal/cli/feedback.go | Rewritten: `feedback <json\|-> [--session id]` gate submission |
| + | auto-reflect/internal/cli/gate.go | `gate check [--session id] [--since dur]` (no-session fallback: host+worktree+24h) |
| + | auto-reflect/internal/cli/stats.go | Per-rule surfaced/selected/selection_rate/feedback_count |
| + | auto-reflect/internal/cli/rebuild.go | Force refold; prints fold conflicts to stderr |
| ~ | auto-reflect/internal/cli/init.go | Events dir + snapshot seed; no feedback.jsonl |
| ~ | auto-reflect/internal/cli/quickstart.go | Full-loop walkthrough incl. jq id-capture examples |
| ~ | auto-reflect/internal/cli/cli_integration_test.go | Rewritten for the loop |
| - | auto-reflect/cmd/autoreflect/e2e_feedback_test.go | Legacy annotations e2e |
| + | auto-reflect/cmd/autoreflect/e2e_loop_test.go | AC-7 quickstart-sequence e2e (minted ids threaded) |

## Links

- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)
- [Loop flow artifact](./loop-flow.md)

## How to Test

- [ ] `auto-reflect/internal/events/log_test.go` — seq allocation under lock, shard naming, read order
- [ ] `auto-reflect/internal/rules/projection_test.go` — fold determinism, deltas, conflicts (AC-4, AC-5), golden-fixture fold (`internal/rules/testdata/events/` → `playbook.golden.json`; guards append-only schema stability)
- [ ] `auto-reflect/internal/rules/match_test.go` — scoring, ANY-of filter, hard injection (AC-1)
- [ ] `auto-reflect/internal/loop/feedback_test.go` — payload validation matrix (AC-3)
- [ ] `auto-reflect/internal/cli/cli_integration_test.go` — full loop via `runCLIAt()` (AC-1/2/3/3b/4/6)
- [ ] `auto-reflect/cmd/autoreflect/e2e_loop_test.go` — built binary, quickstart sequence (AC-7)

## Execution Sequence

```
Phase 1 (events pkg) --> Phase 2 (rules projection + rule CLI cutover)
                                       --> Phase 3 (loop pkg + loop CLI + legacy deletion)
                                                        --> Phase 4 (e2e + docs + quality gate)
```

Strictly serial — each phase rewrites code the next compiles against, and prior-task feedback
(017/018) mandates serial subagent dispatch in a shared worktree anyway.

## Plan

### Phase 1: `internal/events` — canonical store

- [ ] Step 1.1: Create `events/model.go`: envelope struct, payload types (`rule_created`,
      `rule_edited`, `retrieval`, `selection`, `feedback`), `schema_version` const, event
      validation returning `[]config.ValidationError`.
      **Verify**: `cd auto-reflect && go build ./...`
- [ ] Step 1.2: Create `events/shard.go`: shard filename from host (`auto-shared/config.EnsureHost()`),
      UTC date, and wt8 = first 8 hex of SHA-256 of the worktree root (from the lenient repo-detect
      added in Step 1.3 — root resolves even on an unborn HEAD).
      **Verify**: unit test — two distinct root paths on same host+day yield distinct filenames; same
      path is stable across calls.
- [ ] Step 1.3: Create `events/log.go`: `AppendEvent` opens shard `O_RDWR|O_CREATE`, takes
      `flock(LOCK_EX)` (pattern from `store/jsonl.go:15-51`), reads last line → max `seq`, appends
      event with `seq+1`, syncs. Populates ts (RFC3339), host, session/agent (Step 1.4), sanitized
      git provenance via a new lenient detect added to `internal/gitutil` (root from `git rev-parse
      --show-toplevel`, which works on an unborn HEAD; head/tree/remote empty when unresolvable —
      event writing never hard-fails in a commitless repo). `ReadAll` walks `EventsDir()`, decodes
      every line, sorts `(ts, shard-name, seq)`.
      **Verify**: unit tests — 50 sequential appends yield seq 1..50; 10 concurrent goroutine appends
      to one shard produce no gaps/duplicates; `ReadAll` order is deterministic across two runs;
      a remote URL with embedded token is stored credential-free; an append in a `git init`-only repo
      (no commits) succeeds with empty git provenance and a correctly-named shard.
- [ ] Step 1.4: Create `events/session.go`: port `detectSessionID`/`detectAgent` from
      `internal/feedback/service.go:258-276` (env order unchanged). Add `EventsDir()` to
      `store/paths.go` (keep `FeedbackPath` until Phase 3 so legacy code still compiles).
      **Verify**: `go build ./...` && `go test ./internal/events/...` passes; `go test ./...` still
      passes (legacy untouched).
- [ ] Step 1.5: Run `gofmt -l .` (expect empty) and `go vet ./...` in auto-reflect.
      **Verify**: both clean.
- [ ] Step 1.6: Commit: `feat(019): phase 1 - event-sourced store (internal/events)`

### Phase 2: `internal/rules` as projection + rule CLI cutover

- [ ] Step 2.1: Rewrite `rules/model.go`: `Rule{ID, Domain []string, UseWhen, Content, CausalNote,
      RuleType, Lifecycle, Version, CreatedAt, UpdatedAt}`; `Playbook{SchemaVersion, FoldedThrough
      map[string]int, Rules}`; keep `r-` ID + tag regexes. Rewrite `rules/validate.go`: enums
      (`hard|soft`; `draft|confirmed|stale`), domain tags pattern + dedupe, non-empty
      use_when/content/causal_note, **hard ⇒ ≥1 domain**.
      **Verify**: `go build ./internal/rules/...` (cli will not build yet — that's Step 2.4's job;
      do NOT run `go build ./...` until then).
- [ ] Step 2.2: Create `rules/projection.go` (`Fold`: apply rule events in `(ts, shard, seq)` order;
      field deltas; from_version mismatch → apply anyway, version = current+1, append conflict entry)
      and `rules/snapshot.go` (atomic write via `store.WriteJSONFile`; staleness compares
      `FoldedThrough` against per-shard max **rule-event** seq only; `Rebuild` returns conflicts).
      Delete `rules/store.go`, `rules/lookup.go`.
      Create a **checked-in golden fixture** at `internal/rules/testdata/events/` — a small curated
      multi-shard event log (two shard files, mixed event types) covering: rule_created, single- and
      multi-field rule_edited, a from_version conflict pair across shards, and interleaved
      retrieval/selection/feedback noise — plus `testdata/playbook.golden.json`, the expected fold
      output. This doubles as the append-only schema-stability guard: old events must fold
      identically under every future code version, or the golden test fails.
      **Verify**: `go test ./internal/rules/...` — fold of the golden fixture matches
      `playbook.golden.json` byte-for-byte and is identical across two runs (AC-4); v1→v2→v3 history
      reconstructable (AC-5); the fixture's conflict pair produces exactly one conflict entry and a
      deterministic winner; non-rule events in the fixture do not change `FoldedThrough`.
- [ ] Step 2.3: Create `rules/match.go`: normalize/dedupe keywords (port from old lookup.go), ANY-of
      `--domain` intersection filter, scoring use_when=3/domain=1 normalized, sort score DESC then ID
      ASC; hard injection against `--domain` list else intent keywords, `hard_injected` flag.
      **Verify**: `go test ./internal/rules/...` — table tests: hard rule surfaces with zero keyword
      score when domain matches intent keyword; ANY-of vs ALL-of pinned; no --domain + no intent
      overlap ⇒ hard rule absent.
- [ ] Step 2.4: Cut over CLI: rewrite `cli/rule.go` (`create` flags `--use-when --content
      --causal-note --domain --type [--lifecycle]` → `rule_created` event + refold; `edit <id>`
      field flags → ONE `rule_edited` event carrying `deltas: [{field, old, new}]` for all changed
      fields, one version bump per invocation; `list` (IDs + use_when + domain + type), `get <r-id>`
      full rule). Remove `lookup` from `cli/root.go`. Add `cli/rebuild.go`. Update `cli/init.go`:
      create `events/` dir + empty snapshot, stop creating playbook-with-rules/feedback.jsonl.

<!-- RESOLVED(P2): "one event per changed field + one version bump" collides with the fold's per-delta version/conflict rule
REVIEW: This step says a multi-field edit emits "one `rule_edited` event per changed field, one
version bump". But solution.md §2 (lines 65-76) defines `rule_edited` deltas as `{rule_id, field,
from_version, to_version, ...}` and the fold rule: "on a mismatch (from_version != current folded
version) the later delta still applies, version increments from the *current* folded version, and a
conflict entry is recorded". Walk a 2-field edit of a v1 rule under "one version bump": both events
carry from_version=1, to_version=2. Fold applies event A (1→2, current=1 matches) → version 2. Fold
applies event B (from_version=1, but current is now 2) → MISMATCH → conflict path fires → version
becomes 3 and a spurious conflict entry is emitted. So a perfectly ordinary single multi-field edit
is indistinguishable from a real concurrent-edit conflict, because the payload has no edit-group id —
which is exactly the signal the conflict path keys on. Pick one and make it consistent across
solution.md §2, AC-5, and this step: (a) one `rule_edited` event carrying multiple field deltas per
edit (then AC-5's "a rule_edited event records {field,...}" must allow a field set), or (b) chain
versions (field1 1→2, field2 2→3 — then "one version bump" is wrong, it's N), or (c) add an
edit/transaction id so the fold doesn't treat sibling deltas as a conflict.
AUTHOR: Accepted, option (a): one `rule_edited` event per edit invocation carrying a `deltas` array
of `{field, old, new}` plus a single `{from_version, to_version}` pair — an edit is atomic, sibling
field changes can't fake a conflict, and the fold's conflict path keys purely on cross-event
from_version mismatch as intended. Updated consistently: solution.md §2 (payload shape + conflict
rule wording), requirements AC-5 (deltas array, one bump per invocation), and this step.
-->

      **Verify**: `go build ./...` passes for the whole module again; `go test ./...` passes
      (legacy feedback tests still green, old rule/lookup integration tests rewritten or deleted
      for the new surface).
- [ ] Step 2.5: Integration tests in `cli_integration_test.go`: create → list → get → edit round-trip;
      delete `playbook.json` → next `rule list` refolds to identical content (AC-4); `rule create`
      with hard type + no domain exits non-zero with remediation.
      **Verify**: `go test ./internal/cli/...` passes.
- [ ] Step 2.6: `gofmt -l .` empty; `go vet ./...` clean.
- [ ] Step 2.7: Commit: `feat(019): phase 2 - rules as event projection + rule CLI`

### Phase 3: `internal/loop` + loop CLI + legacy deletion

- [ ] Step 3.1: Create `loop/service.go`: `Retrieve` (match → mint `rt-` ids → one `retrieval` event
      → predicate-only JSON), `Select` (resolve rt-ids from retrieval events; `r-`/`fb-` prefixed
      arg → targeted wrong-id-type error; mint `fb-` ids; one ordered `selection` event → content
      JSON), `Stats` (fold counts; all rules listed, `selection_rate` = 0 when surfaced == 0 — never
      NaN).
      **Verify**: `go build ./...`; unit tests — Select preserves input order; unknown rt-id and
      r- prefixed id produce distinct remediation messages; stats output containing a never-surfaced
      rule marshals to valid JSON with `selection_rate: 0`.
- [ ] Step 3.2: Create `loop/feedback.go` + `SubmitFeedback`/`GateCheck`: payload schema, validation
      (outcome `success|partial|fail|abandoned`; rankings exactly cover outstanding fb-ids, ranks a
      permutation of 1..N, reasons non-empty; gap ⇒ report+moment non-empty); `--session` override;
      gate scope = session else host+worktree shards within `--since` (default 24h); no-rules
      session passes.
      **Verify**: `go test ./internal/loop/...` — validation matrix (missing id, extra id, dup rank,
      ungrounded gap, bad outcome ⇒ structured errors; complete payload ⇒ event appended exactly
      once); gate scenario test: orphaned fb-ids older than window don't block; `--session` closes
      them with outcome `abandoned`.
- [ ] Step 3.3: CLI: add `cli/retrieve.go`, `cli/select.go`, rewrite `cli/feedback.go` (JSON arg or
      `-` stdin), add `cli/gate.go`, `cli/stats.go`. Register in `root.go`.
      **Verify**: `go build ./...`.
- [ ] Step 3.4: Delete legacy: `internal/feedback/` package, old annotation logic from CLI,
      `FeedbackPath()` from `store/paths.go`, `cmd/autoreflect/e2e_feedback_test.go` — **first
      relocating `TestMain` + `var testBinaryPath` (e2e_feedback_test.go:12-31) into
      `e2e_helpers_test.go`** so the `cmd/autoreflect` test package keeps compiling between Phase 3
      and Phase 4.
      **Verify**: `go build ./...` && `go test ./...` pass (incl. `go test ./cmd/...` compiling with
      the relocated TestMain); `rg -n "internal/feedback" auto-reflect/` returns nothing.

<!-- RESOLVED(P2): deleting e2e_feedback_test.go breaks cmd/autoreflect compilation — Step 3.4 verify will fail
REVIEW: I checked the harness. `TestMain` AND `var testBinaryPath` both live in
`cmd/autoreflect/e2e_feedback_test.go:12-31` — the file this step deletes. The RETAINED helper
`cmd/autoreflect/e2e_helpers_test.go:13` (`runBinary`) does `exec.Command(testBinaryPath, ...)`, i.e.
it references the var defined in the deleted file. So after Step 3.4 deletes e2e_feedback_test.go,
the `cmd/autoreflect` test package no longer compiles (`undefined: testBinaryPath`) and has no
`TestMain` to build the binary — yet the new harness/`TestMain` isn't created until Phase 4 Step 4.1.
Step 3.4's own verify (`go test ./...` passes) therefore CANNOT pass as sequenced. Fix: relocate
`TestMain` + `testBinaryPath` into `e2e_helpers_test.go` (or a new `e2e_main_test.go`) as part of
this deletion, so the package keeps compiling between Phase 3 and Phase 4 — or create the new
`TestMain` in the same step rather than waiting for 4.1.
AUTHOR: Accepted — Step 3.4 now relocates TestMain + testBinaryPath into e2e_helpers_test.go as part
of the same change, and its verify explicitly includes `go test ./cmd/...` compiling. Step 4.1 then
builds e2e_loop_test.go on the already-working harness.
-->

- [ ] Step 3.5: Rewrite `cli/quickstart.go`: init → rule create → retrieve → select → feedback →
      gate check, with explicit jq id-capture lines (e.g. `RT=$(auto reflect retrieve "..." | jq -r
      '.[0].retrieval_id')`).
      **Verify**: every command named in quickstart exists in `auto reflect --help` output (manual
      cross-check in test or by eye).
- [ ] Step 3.6: Integration tests: full loop happy path (retrieve → select → incomplete feedback
      rejected → complete feedback accepted → gate closed); retrieval/selection/feedback events on
      disk with correct linkage rt→fb (AC-1, AC-2, AC-3, AC-3b); `stats` after two simulated
      sessions returns expected counts (AC-6).
      **Verify**: `go test ./internal/cli/...` passes.
- [ ] Step 3.7: `gofmt -l .` empty; `go vet ./...` clean.
- [ ] Step 3.8: Commit: `feat(019): phase 3 - retrieval loop, feedback gate, legacy removal`

### Phase 4: E2E, docs, quality gate

- [ ] Step 4.1: Create `cmd/autoreflect/e2e_loop_test.go` on the existing TestMain/runBinary harness:
      temp git repo (`initGitRepo` pattern **plus a seed commit**, matching the legacy
      `TestE2EFeedbackAddList` pattern at e2e_feedback_test.go:49-50), `init --project`, then follow
      the quickstart sequence parsing JSON output to thread minted rt-/fb- ids; assert events files
      exist under `.auto/reflect/events/` with the `<host>-<date>-<wt8>.jsonl` name shape, gate exits
      non-zero before feedback and zero after (AC-7). Add one no-commit case: `rule create` in a
      `git init`-only repo succeeds with empty git provenance (degraded path from Step 1.3).
      **Verify**: `cd auto-reflect && go test ./cmd/...` passes.

<!-- RESOLVED(P2): the event system hard-depends on DetectRepo, which fails on an unborn HEAD — the e2e fixture has no commit
REVIEW: Two things converge here. (1) Both shard naming (wt8 = SHA-256 of `gitutil.DetectRepo().Root`,
Step 1.2) and every event's git provenance go through `gitutil.DetectRepo`. I read git.go:24-40:
DetectRepo runs `rev-parse HEAD` and `HEAD^{tree}` and returns an error if either fails. (2) I
verified `git rev-parse HEAD` on a freshly `git init`'d repo exits 128 ("unknown revision") — i.e.
DetectRepo errors on an unborn HEAD. The fixture this step names, `initGitRepo`
(cli_integration_test.go:398-406) — and `initE2ERepo` (e2e_helpers_test.go:34-42) — create a repo
with `git init` + config + remote but NO commit. So as planned, the very first event-writing command
in the e2e (`rule create`) will fail inside DetectRepo. Note the legacy happy-path test handles this
exactly right: `TestE2EFeedbackAddList` does `git add . && git commit -m seed` before invoking the
binary (e2e_feedback_test.go:49-50). So: the e2e setup must make an initial commit before any
event-writing command. More importantly for the design — solution.md/AC-7 never state AppendEvent's
behavior when DetectRepo fails. A user running `auto reflect init --project` then `rule create` in a
brand-new repo with no commits would hit a hard failure. Specify the degraded path (empty git
provenance + a root resolved via `rev-parse --show-toplevel`, which DOES work pre-commit) or document
that a commit is a precondition, and reword AC-7's "fresh repo" accordingly.
AUTHOR: Accepted, both halves. Design: events now use a lenient repo-detect added to gitutil (root
via `rev-parse --show-toplevel`, empty head/tree/remote on unborn HEAD; event writing never
hard-fails) — specified in solution.md §1 + Files note, Step 1.3 (with a no-commit unit test), and
the Changes table. Test: this step now seeds an initial commit for the happy path and adds an
explicit no-commit degraded-path case. AC-7's "fresh repo" is thereby honest: it works with or
without a first commit.
-->

- [ ] Step 4.2: Docs sweep: mark `auto-reflect/docs/requirements.md` superseded (header note → task
      019); check `auto-reflect/CLAUDE.md` and root docs index for stale command references
      (`auto reflect lookup`, `feedback add`); run `rg -n "reflect (lookup|feedback add)" --type md`
      and fix hits outside task-019 planning docs and historical design docs.
      **Verify**: rg returns only intentional historical references.
- [ ] Step 4.3: Full gate from repo root: `golangci-lint cache clean` (017/018 lesson: cross-worktree
      phantom findings), then `make check && make build && make test`.
      **Verify**: all three pass; `bin/auto reflect quickstart` prints the new walkthrough.
- [ ] Step 4.4: Manual smoke in a scratch repo outside git (`.tmp/`): run the quickstart commands
      as printed, confirm JSON-only stdout (pipe through `jq .` at each step) and remediation text
      on a deliberately incomplete feedback payload.
      **Verify**: every step's stdout parses as JSON; errors appear on stderr only.
- [ ] Step 4.5: Commit: `feat(019): phase 4 - e2e loop test + docs sweep`

## Success Criteria

- [ ] `make check`, `make build`, `make test` pass from repo root (includes gofmt, vet, lint,
      stale-refs — note: `stale-refs` only catches old *binary stems* like `autoreflect …`, NOT
      deleted subcommands in correct `auto reflect …` form; the last criterion below is enforced
      solely by Step 4.2's `rg` sweep)

<!-- RESOLVED(P3): make check's `stale-refs` does NOT catch deleted-subcommand references — don't conflate it with Step 4.2
REVIEW: `make check` does include a `stale-refs` target (Makefile:83,88), but I read
scripts/check-no-stale-binary-refs.sh: it only flags old BINARY stems (`autoreflect lookup`, etc.) —
correct-form invocations of deleted subcommands (`auto reflect lookup`, `auto reflect feedback add`)
are NOT matched. So `make check` does NOT enforce Success Criterion line 207 ("No references to
deleted surfaces remain … reflect lookup, feedback add"); that relies entirely on the manual `rg`
sweep in Step 4.2. Worth noting so an implementer doesn't see "make check passed" and skip 4.2.
AUTHOR: Noted inline on the criterion: stale-refs covers binary stems only; the deleted-subcommand
criterion is enforced by Step 4.2's rg sweep, not by make check.
-->

- [ ] AC-1: `retrieve` returns predicates only; hard rules injected; `retrieval` event appended — integration + unit tests green
- [ ] AC-2: `select` preserves order, mints fb-ids, appends ordered `selection` event — integration test green
- [ ] AC-3/AC-3b: incomplete/ungrounded feedback rejected with structured errors + remediation; gate exits non-zero→zero around a complete submission; no-rules session passes — loop unit matrix + integration tests green
- [ ] AC-4: snapshot deleted → refold byte-identical; events carry type/schema_version/ts/seq; two worktree-root paths ⇒ two shard files — projection/events unit tests green
- [ ] AC-5: edit history v1→vN reconstructable from events — projection test green
- [ ] AC-6: `stats` returns surfaced/selected/selection_rate per rule after ≥2 sessions — integration test green
- [ ] AC-7: e2e_loop_test drives init→create→retrieve→select→feedback→gate via the built binary following quickstart — green
- [ ] No references to deleted surfaces remain (`internal/feedback`, `reflect lookup`, `feedback add`) outside historical docs

## Execution Notes (from 017/018 feedback)

- **Dispatch phases serially**; verify each phase's files are on disk and `git -C <main-worktree>
  status` is clean before starting the next (concurrent subagents leaked writes into main twice).
- Run `golangci-lint cache clean` before trusting local lint in a worktree.
- `make vulncheck` may fail locally on go1.26.3 (pre-existing stdlib vulns; CI's 1.26.4 passes) —
  not task-introduced.
- Go build discipline: `go build ./...` after every file, per root CLAUDE.md.

## Open Questions

- (none — all resolved in requirements.md/solution.md review threads)
