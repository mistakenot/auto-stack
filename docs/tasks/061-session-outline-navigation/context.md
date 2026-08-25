# Context: Task 061 — session-outline-navigation

Grounded codebase map for adding `auto search session outline`. See [plan.html](./plan.html).

## Reuse point: the work-graph builder

`sessionhtml.BuildModel(db, rootID, Options) (*Node, error)` already builds the entire
coordinator + nested-subagent tree and is **cleanly reusable without HTML rendering** — the
package "splits cleanly into a pure model builder (build.go) and a pure renderer (render.go)".

- `auto-search/internal/sessionhtml/build.go:31` — `BuildModel` entry point; recurses children via `Agent` tool events (`build.go:177-196`), `Child *Node` links the tree.
- `auto-search/internal/sessionhtml/model.go:66-84` — `Node` (exported, json-tagged): `Events []Event`, `Counts{Bash,File,Tool,Agent,Skill,Error}`, `DurationMs`, `TotalTokens`, `MsgCount`, `Depth`, `Intent`, `Title`, `DispatchLabel`, `SubagentName`, `IsSubagent`, `Workspace`, `Model`, `FirstMs`, `LastMs`.
- `auto-search/internal/sessionhtml/model.go:33-62` — `Event`: `Kind` (`user|assistant|thinking|tool|agent`), `Idx` (message_index), `MID` (message_id — the leaf-expansion key), `Summary`, tool fields (`Tool`, `Duration`, `IsError`, `Interrupted`, `Exit`), and `Child *Node` on agent dispatches.
- `auto-search/internal/sessionhtml/model.go:11-18` — `Options{IncludeThinking, Light}`. `Light` selects `content_truncated` bodies. For a bodies-free outline, either build with `Light:true` or ignore body fields and use `Event.Summary` (always populated).
- `render.go:21` (`Render`) is **not** needed — nothing in `BuildModel`'s output depends on it.

**Coupling to note:** `toolSummary`/`firstLine`/`norm` (`build.go:347-424`) hardcode widths (100/120/140/200) and are **unexported** — can't reuse for custom summary widths; re-derive from row fields if needed. `Node.DurationMs` is calendar span (`LastMs-FirstMs`, `build.go:145`), not `TotalTurnDurationMs` (real work time). `Event` carries no `ToolUseResultJSON` — for full tool-result leaves go through `GetMessageByID`.

## Key Files — indexdb query surface

- `auto-search/internal/indexdb/query_sessions.go:287` — `GetSessionByID(db, id) (*SessionRow, error)` (untruncated `FirstUserIntent`).
- `auto-search/internal/indexdb/query_sessions.go:326` — `SessionMessages(db, id, includeThinking) ([]MessageRow, error)`, ordered by `message_index ASC`.
- `auto-search/internal/indexdb/query_sessions.go:93` — `ListSessions(db, opts) ([]SessionListRow, total, error)`; `ListSessionsOpts` (`:41`) has `ParentSessionID`, `OnlyInterrupted`, `MinErrors`, `MinToolDurationMs`, `SortBy`.
- `auto-search/internal/indexdb/query_messages.go:59` — `GetMessageByID(db, id) (*MessageRow, error)` — **the only reader that hydrates `ToolUseResultJSON`**; this is the full-fidelity leaf path.
- `MessageRow` (`query_messages.go:9-56`) — segmentation-relevant fields all present: `MessageIndex`, `Role`, `ToolName`, `BashExitCode`, `IsError`, `Interrupted`, `DurationMs`, `Timestamp`, `ToolUseID`, `Content`/`ContentTruncated`, `SkillName`, `MessageID`.
- `SessionRow` (`query_sessions.go:10-38`) — carries `TotalTurnDurationMs`, `PermissionMode`, `FirstUserIntent`, `TranscriptTruncated`.

## Key Files — CLI wiring & conventions

- `auto-search/internal/cli/session.go:34-39` — **the only registration edit**: add `newSessionOutlineCmd()` to the `AddCommand` block under `newSessionCmd`. It then appears as `auto search session outline` through the umbrella binary automatically (`auto-cli/cmd/auto/main.go:48` mounts `auto-search/rootcmd`).
- `auto-search/internal/cli/session.go:87-95` — standard DB-open preamble to copy: `config.IndexPath` → `indexdb.Open` → error wrapped `"open index: %w; run: auto search index"` → `defer db.Close()`.
- `auto-search/internal/cli/session.go:161-176` / `:277-311` — JSON envelope convention: `map[string]any{"_meta": {request_id, elapsed_ms, …}, "<resource>": …}` + `json.NewEncoder(cmd.OutOrStdout())` + `SetIndent("", "  ")`. No shared helper — per-command.
- `auto-search/internal/cli/message.go:25-55` — `message get <id>` treats the ID as **opaque**, calls `GetMessageByID`, prints `.Content` raw. Outline leaf-expansion should do the same. (ID format `<session_id>-<message_index>` is assigned by ETL at `auto-etl/internal/transform/transform.go:480` but stays opaque at the CLI.)
- `auto-search/internal/cli/session.go:524` — `midTruncate` emits `…[truncated — run: auto search message get <id>]…`; the outline's elided regions must mirror this breadcrumb.
- Text mode: `auto-search/internal/cli/search.go:181` (`--text` flag) + `renderMessageHitsText` (`search.go:190-218`) — per-command renderer, no shared helper.
- `ExitError{Code, Err}` — `auto-search/internal/cli/root.go:14-24`; return `&ExitError{Code:1, Err:…}` on failure. Every hard error needs a remediation hint.
- File-writing precedent (validate up front, stdout clean, diagnostics to stderr, pre-check session exists): `session export` (`session.go:323-408`).

## Patterns & conventions to follow

- **Resource ladder** (`docs/auto-package-patterns.md:257-287`): `outline` is a **new intermediate rung between `describe` and `get`** — cheap, **bodies-free** (structure/IDs/metadata only), must not duplicate `get`'s full bodies, and every elision prints the exact next command (`session get <id>` / `message get <id>-N`).
- **CLI conventions** (`CLAUDE.md:98-117`): JSON default + `--text`; stdout=payload only, diagnostics→stderr; `--since/--after/--before`; fail-fast on bad flags; `--request-id` echoed in `_meta`; return all when no filters.
- **Ubiquitous language** (`docs/concepts/UBIQUITOUS_LANGUAGE.md`): use **Session/Subagent/Message**; reuse canonical fields `is_subagent`, `parent_session_id`, `subagent_name`, `duration_ms`. Call per-message leaves **Messages** (`<id>-<index>`), not "nodes/entries". `_Avoid_` "transcript" in new field names. `outline`/`segment`/`tree` are unclaimed novel terms — flag with a glossary note.
- **Known ETL-boundary limits** (`auto-search/docs/progressive-disclosure-audit.md`): true parent/child *branch* structure is lost (only linear order + subagent nesting survive); per-session `transcript_full` is dropped at index time (`indexdb/indexer.go:285`) — rely on per-message rows, not a full-session blob.
- **Error-count semantics differ**: `sessionhtml Counts.Error` = `is_error || bash_exit_code!=0` (`build.go:212`) vs `ListSessions error_count` = `bash_exit_code>0` (`query_sessions.go:190`). Pick one deliberately.

## Doc surfaces to update

- Quickstart string `auto-search/internal/cli/quickstart.go` (session sections `:115-222`); there are quickstart **integration tests** asserting content (`cli/cochange_integration_test.go:89-133`) — add matching coverage.
- No `docs` command exists in auto-search (root registers 11 commands, `cli/root.go:54-66`) — nothing to update there.
- `auto-search/CLAUDE.md` Documentation-Index is autodoc-generated — run `auto doc fix`, don't hand-edit. No `[autodoc()]` tags exist in auto-search Go today.
- Update the disclosure-ladder table in `auto-search/docs/progressive-disclosure-audit.md:32-43` to add the `outline` rung.

## Test patterns to mirror

- `runCLI(t, args...) (stdout, stderr, code)` — `auto-search/internal/cli/cli_integration_test.go:90-114`.
- `setupIndexedFixtures(t)` builds a temp-HOME index from committed parquet under `testdata/etl-output/` — `auto-search/internal/cli/cli_integration_test.go:143-160`. The fixture session/message definitions live in **`auto-search/internal/testutil/fixtures.go`** (NOT beside the CLI helper — there is no `auto-search/internal/cli/fixtures.go`): `test-session-1` (parent) + `test-session-2` (its subagent) + `test-session-3` (standalone).

<!-- RESOLVED(P3): Fixture helper path is ambiguous
REVIEW: I checked the repo and the fixture definitions are in `auto-search/internal/testutil/fixtures.go`, not beside the CLI integration helper. Please spell out the full path here so the executor does not look for a nonexistent `auto-search/internal/cli/fixtures.go`.
AUTHOR: Spelled out the full path — fixtures live in `auto-search/internal/testutil/fixtures.go`; the `setupIndexedFixtures` helper is in `cli_integration_test.go`. Explicitly noted there is no `auto-search/internal/cli/fixtures.go`.
-->

- Mirror `TestSessionDescribe` (`cli_integration_test.go:705-731`) for JSON assertions; mirror `session_export_test.go:20-63` for file-output/error-path cases (unknown session → non-zero + "session not found"; missing index → "auto search index" hint).

## Build / test / lint (executor)

No per-module Makefile; everything runs from the root `Makefile` looping over modules.
Fast inner loop from `auto-search/`:
- Build: `cd auto-search && go build ./...`
- Package test: `cd auto-search && go test ./internal/sessionoutline/...` (or `./internal/cli/...`)
- Full module: `cd auto-search && go test ./...`
- Lint: `cd auto-search && golangci-lint run ./...`
- Repo gate before PR: `make check && make test` from repo root.

⚠️ **`make test` depends on `verify-fixtures`.** Adding fixture *messages/sessions* ripples
fixed counts across indexer/search/stats/cli/testutil tests (this bit task 044). **Mitigation
baked into the plan:** unit-test the segmenter with **synthetic in-memory `Event`/`Node`
values (no index)**; CLI integration tests reuse the existing `test-session-1`/`test-session-2`
fixtures **without adding new rows** — they assert envelope/shape/flags, not per-signal
boundary correctness (that's the unit tests' job).

Commit style (from `git log`): `feat(061): …` subject + structured trailer lines
`intent(task)`, `decision(<area>)`, `rejected(...)`, `constraint(...)`, `learned(...)`,
`Co-Authored-By:`, `Session-Id:`.

## Related Tasks
- **044-session-html-map** (`adec795 feat(044): session-html-map …`) — direct predecessor;
  produced `session export` + the whole `sessionhtml` package. Landed as **4 phased
  sub-commits** (model+builder → renderer → CLI wiring → quickstart docs) — the template to
  mirror for `sessionoutline`. Decisions carried over: one flexible `Event` struct for all
  kinds; sub-agent correlation by prompt-prefix on full `FirstUserIntent`; unknown-id
  pre-check via `GetSessionByID` before emitting; `build_test.go` populates a real temp index
  via `indexdb.Create`+`InsertSession`/`InsertMessage`.
- **015-session-intent-summary** — added `first_user_intent` (used by the spine's correlation).
- **016-etl-preserve-session-signal** — the ETL side of the audit's P1–P3 losses (thinking,
  stop_reason, skill attribution, permission mode); explains why some are now partially
  recoverable but remain out of scope here.
- **010/011-autosearch-co-change** — the quickstart-assertion test pattern in
  `cochange_integration_test.go` (`TestQuickstartMentionsCoChange` @90,
  `TestQuickstartCoChangeSection` @102) to mirror for the new outline quickstart section.
- **Progressive-disclosure audit** — not a numbered task; commit `cbbbdae docs: audit
  autosearch progressive disclosure + codify resource CLI pattern` (also added the
  "Resource Subcommands" pattern). This task adds the `outline` rung to its ladder table.

**Path drift check (CB3): all Solution/context paths VERIFIED at the cited lines — no drift.**
