# Context: Task 016

Verified codebase facts grounding [solution.md](./solution.md) — where each dropped
signal is produced (auto-etl) and where it must be surfaced (auto-search).

## Key Files — auto-etl (producer)

- `auto-etl/internal/parser/parser.go:88-97` — `ContentBlock` already has `Text`,
  `Input`, `Content`, `ToolUseID`, and **`IsError`** (`:96`). Missing only
  `Thinking`/`Signature`/`Data`. Raw thinking block:
  `{"type":"thinking","thinking":"…","signature":"…"}`; redacted block:
  `{"type":"redacted_thinking","data":"<encrypted>"}` → stored as a `[redacted]`
  marker row with the encrypted `data` kept in the `thinking_signature` column.
- `auto-etl/internal/parser/parser.go:79-85` — `ParsedUsage` **already has**
  `CacheCreationInputTokens` + `CacheReadInputTokens`. The split is lost in the
  transform, not the parser.
- `auto-etl/internal/parser/parser.go:126-131` — `rawMessage` (`role`,`content`,`model`,`usage`); add `StopReason`. Mirror onto `ParsedMessage` (`:72-77`) + constructor (`:237-242`).
- `auto-etl/internal/parser/parser.go:100-115` — `rawLine`; add `Version`,
  `PermissionMode`, `AttributionSkill`. Mirror onto `ParsedLine` (`:27-48`) +
  constructor (`:224-243`). `permissionMode` and `version` are **top-level fields on
  message lines** (verified: `version` on 64 lines, `permissionMode` present on the
  standalone `type:"permission-mode"` lines in c9124cdf and on message lines in other
  CC versions — schema doc `claude-project-files-schema.md:81,88`). The scan loop
  (`:191-247`) decodes **every** line into `rawLine`, so adding the fields captures
  the value from both message lines and the standalone `permission-mode` line — read
  `line.PermissionMode`/`line.Version`, do not switch on `type=="permission-mode"`.
  `model` is captured per session at `:205-207`. No code reads
  version/permissionMode/attributionSkill today.
- `auto-etl/internal/transform/transform.go:233-348` — content-block
  `switch block.Type` with `default: continue` (**:346-347** = where thinking is
  dropped). Roles: text→`line.Message.Role` (`:235`), tool_use→`RoleAssistant`
  (`:246`), tool_result→`RoleTool` (`:298`).
- `auto-etl/internal/transform/transform.go:212,230` —
  `msg.CacheInputTokens = CacheCreation + CacheRead` (the merge to preserve-alongside).
- `auto-etl/internal/transform/transform.go:297-344` — tool_result case; `block.IsError` available but never written.
- `auto-etl/internal/transform/transform.go:174-182` — the `system/turn_duration`
  pre-pass: the **precedent** for a session-level `permission-mode`/`version` accumulator.
- `auto-etl/internal/transform/transform.go:385-412` — `AgentSession{…}` literal (add `PermissionMode`,`Version`); `:417-444` `makeBaseMessage`; append+`msgIndex++` at `:350-351`.
- `auto-etl/internal/transform/transform.go:122-127,268-272,305` — current `skill_name`
  population (only from the `Skill` tool's `input.skill`). AC-3 broadens this.
- `auto-etl/internal/model/model.go:5` — `SchemaVersion = 5` → 6. `:19-24` roles
  (add `RoleThinking`). `AgentMessage` `:27-91` / `AgentSession` `:94-140` (add columns).
- `auto-etl/internal/writer/writer.go:87-105` — `parquet.NewGenericWriter[T]` (`:98`);
  schema is **struct-tag-derived** → adding a field needs no writer change.

## Key Files — auto-search (consumer)

- `auto-search/internal/indexdb/schema.go:13` — `SchemaVersion = 8` → 9 (rebuild
  trigger). `messages` table `:59-94`, `sessions` table `:31-57`. `role` indexed
  `:107`, `skill_name` indexed `:106`. `messages_fts` (`:127-135`) over
  `content_truncated` → thinking content auto-FTS-indexed via existing triggers
  (`:157-172`); opt-in exclusion is a WHERE filter, not an FTS change.
- `auto-search/internal/model/parquet.go:5-47,50-99` — `ParquetSessionRow` /
  `ParquetMessageRow` mirrors (add matching tagged fields).
- `auto-search/internal/indexdb/messages.go:12-69` — `InsertMessage` (column list
  `:41-52`, args `:54-63`). `sessions.go:9-47` — `InsertSession`.
  `indexer.go:285-297,299-318` — `insert*FromParquet` positional wiring.
- `auto-search/internal/search/messages.go:647-658` — `normalizeRole` whitelists
  `user|assistant|tool` only → **must add `thinking`** (used by sessions.go:67).
  **Second independent copy** at `auto-search/internal/stats/validate.go:177-183`
  (caller `stats/validate.go:37`) — must ALSO add `thinking`, else `stats --role
  thinking` is rejected.
  `role = ?` appended only when `--role` set: `:244-247` (FTS) and `:406-409`
  (noFTS); session subquery `sessions.go:170-172`. **No default exclusion exists.**
  `MessageSearchOpts` `:67-92` (add `IncludeThinking`).
- `auto-search/internal/cli/search.go:169` — `--role` flag (help: "user, assistant,
  tool"); add `--include-thinking` + thread opt (`:102-121`,`:135-149`).
- `auto-search/internal/cli/session.go:336-380` — `roleTag`; `default` (`:343-344`)
  already renders `<thinking index=N>` — **no change**. `session get` `:191-230`
  selects via `indexdb.SessionMessages` (`:209`), which `SELECT … WHERE session_id=?
  ORDER BY message_index` (`query_sessions.go:320-334`, scan `:344-354`) with **no
  role filter today**. Add `--include-thinking` flag (`:228`) + optional `role!='thinking'`.
- `auto-search/internal/cli/message.go:43-50` — `message get` prints full `Content`
  unconditionally → **thinking reachable automatically**.

## Patterns

- **Schema-touching dual bump** (task 012/015 precedent): bump BOTH
  `auto-etl model.SchemaVersion` AND `auto-search indexdb.SchemaVersion`; mirror
  the field on `ParquetMessageRow`/`ParquetSessionRow`; add SQLite column; plumb
  `InsertX`/`insertXFromParquet`/query scan. Rollout = `autoetl run --full` then
  `autosearch index` (no `--rebuild` flag).
- **auto-etl decision principle** (`auto-etl/CLAUDE.md`): "Keep as much original
  data as possible… create new fields rather than replace" → drives AC-4 (add cache
  columns, keep the sum) and the keep-signature choice.
- **Build/test loop**: `go build ./...` after each Go edit; auto-etl fixtures in
  `auto-etl/.tmp/claude`, output to `.tmp/output`, inspect with `duckdb`.
- **Tests**: `e2e_test.go:155-161` `expectedMessages()` hard-codes "thinking
  skipped" — must update. `TestE2E_ToolUseBecomesAssistantRole` (`:238-262`) asserts
  role-count sum == total. `transform_test.go` builds `parser.ParsedLine` literals
  with raw-JSON `Message.Content` (a `{"type":"thinking",…}` block exercises the new
  case); see `TestTransformSession_SkillName_ToolUse:681`.

## Doc drift to reconcile (AC-6)

- `auto-etl/docs/claude-message-types-and-etl-mapping.md` already describes thinking
  as preserved "at SchemaVersion 6" (`:177-182,483`) — **aspirational/wrong** (code
  still drops it) and repeats the disproven "Claude Code redacts it" claim. Fix it +
  add stop_reason/permission_mode/version/is_error/cache-split/skill_name rows.
- `auto-etl/docs/reference/normalized-schema.md:24` says `Current value: 3` (stale;
  real is 5→6); `role` enum `:44` lacks `thinking`. Already drifted from `model.go`.
- `auto-search/docs/progressive-disclosure-audit.md:71-91` — the motivating audit
  (P1 thinking, P2 skill_name, P3 stop_reason/permission_mode/version/is_error/cache).
  Realistic test fixture: session `c9124cdf-…` (`/review-task 013`), 7 populated
  thinking blocks, `permissionMode: bypassPermissions`, version 2.1.168.

## Git History & Precedent (verified)

- **Phase shape**: tasks 012 & 015 both used **4 strictly-linear phases**, producer→consumer→
  test/CLI→docs+rollout. auto-etl always lands first (its parquet column names are what the
  search side mirrors). Quote (015 plan): *"Phase 2's parquet column names must match Phase 1's."*
- **Dual bump sequencing**: etl `model.SchemaVersion` bumps in the etl phase, search
  `indexdb.SchemaVersion` in the search phase — **same PR**, different commits. History
  confirms current values: etl `5` (012:2→3, 042857e:3→4, 015:4→5) → **016 = 6**; search `8`
  (012:5→6, 015:7→8) → **016 = 9**. The two are co-bumped on every parquet-layout change but
  are not lockstep-numbered (search bumps more often for index-only changes).
- **New `MessageRole` value has NO precedent** (`git log -S 'MessageRole'` → only the initial
  schema commit). `RoleThinking` is the first role-enum addition to the pipeline → the
  role-handling paths (transform `switch` case, `normalizeRole`, transcript exclusion, opt-in
  default filter) carry the most novelty and need direct test coverage.
- **Commits**: per-phase, not squashed (preserved through a merge commit). Pattern
  `feat(016): phase N - …` / `test(016): …` / `docs(016): …`, with contextual-commit
  `intent(task):` bodies and trailers `Co-Authored-By: Claude Opus 4.8 (1M context) …` +
  `Session-Id: …`. One PR from branch `task/016-etl-preserve-session-signal`.
- **CI gates** (`.github/workflows/ci.yml`, Go 1.26, on PR): `make check` (fmt+vet+lint),
  `make build`, `make test`, `make vulncheck`. Plus automated Claude review posts inline PR
  comments (budget an address-feedback pass). Per CLAUDE.md: `go build ./...` after each Go edit.

### Gotchas → explicit verify steps (from 012/015 feedback.md)

1. **No `autoetl transform` subcommand** — it's `autoetl run --full --only sessions`; main is at
   the module root, not `cmd/autoetl`. Confirm with `--help` first.
2. **`autoetl run --full` exits 1 on the unrelated `github` source** (404 on a missing repo
   aborts everything) — always scope rollout with `--only sessions`.
3. **`InsertMessage`/`InsertSession` are positional** — adding params breaks call-sites at
   compile time silently if order drifts. Only ~2 call-sites each (`indexer.go` + an
   integration test); grep before changing. **Both signatures change in 016** (message + session
   cols) → doubled risk. Verify: VALUES placeholder count == Exec arg count.
4. **Checked-in parquet fixtures predate new columns** — round-trip tests fail with *empty
   values, not a schema error*. Prefer generating fixtures into `t.TempDir()` at runtime (012's
   approach) over checked-in files; note `.gitignore` ignores `**/testdata/` (needs `git add -f`).
5. **`json_extract_string` is DuckDB-only** — SQLite (`modernc.org/sqlite`) has only
   `json_extract` (minor; no JSON-path SQL planned for 016).
6. **Backfill row counts are new baselines** — thinking-row count / skill_name coverage are new
   numbers, not matched against the old (pre-fix) values.

## Related Tasks

- **Task 012 (structured-tool-output)** — closest precedent: dual SchemaVersion bump (PR #49,
  branch `task/012-…`, 4 phases), ParquetMessageRow mirror, SQLite column, insert/query
  plumbing, both ETL docs updated; PR-review follow-up commit `9e745b5`.
- **Task 015 (session-intent-summary)** — added `AgentSession` fields with the same dual-bump
  pattern (PR #57); Phase 4 rollout was **no-commit** human acceptance: `autoetl run --full`
  (the bug above: it's `run`, not `transform`) + `autosearch index` (the index bump auto-triggers
  a full rebuild — no `--rebuild` flag).
