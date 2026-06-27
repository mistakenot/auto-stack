# Context: Task 044 (session-html-map)

Codebase + docs grounding for the `auto search session export <id>` command. See [plan.html](plan.html) for requirements and decisions; a working prototype lives in [artifacts/](artifacts/) (`sample-session.html`, `prototype-build_doc.py`, `prototype-template.html`).

## Key Files

### Where the command plugs in
- `auto-search/internal/cli/session.go:21-32` — `newSessionCmd()` assembles `list`/`get`/`describe`. Add `newSessionExportCmd()` here.
- `auto-search/internal/cli/session.go:191-232` — `newSessionGetCmd()` is the closest template: opens the index, calls `indexdb.SessionMessages`, writes output. Note its `--include-thinking` flag (line 230) — our `--exclude-thinking` is the inverse default.
- `auto-search/internal/cli/root.go:14-41` — `ExitError{Code,Err}` pattern; `RunE` returns `&ExitError{Code:1, Err: fmt.Errorf("...: %w", err)}`. Root maps it to the process exit code via `errors.As`.
- `auto-search/internal/cli/root.go:60` — `newSessionCmd()` registered on the root command.
- `auto-search/internal/config/settings.go` — `config.IndexPath(index)` → `~/.auto/search/<name>.sqlite`; `config.DefaultIndexName = "default"`.
- `auto-search/internal/indexdb/schema.go:184` — `indexdb.Open(dbPath)`; callers `defer db.Close()`.

### Query helpers (everything the work-graph model needs)
- `auto-search/internal/indexdb/query_sessions.go:287` — `GetSessionByID(db, id) (*SessionRow, error)` → full row incl. `FirstUserIntent` (untruncated — needed for sub-agent correlation), `SubagentName`, `TotalTokens`, `First/LastMessageAt`, `Model`, `Workspace`, `GitRemote`, `SourcePath`.
- `auto-search/internal/indexdb/query_sessions.go:326` — `SessionMessages(db, id, includeThinking) ([]MessageRow, error)`, ordered by `message_index`. Pass `includeThinking=true` by default; `false` for `--exclude-thinking`.
- `auto-search/internal/indexdb/query_sessions.go:93` — `ListSessions(db, &ListSessionsOpts{ParentSessionID: id})` → child sessions (`SessionListRow`). Use to enumerate sub-agents, then `GetSessionByID` each for full intent.
- `auto-search/internal/indexdb/query_sessions.go:392` — `CountSessionMessages(db, id) (SessionMessageCounts, error)` → `Total/User/Tool/Bash/ReadFile/WriteFile/Skill/SkillsUsed` (for header chips; we also derive counts while walking events).
- `auto-search/internal/indexdb/query_messages.go:8-56` — `MessageRow` fields used by the model: `Role`, `Content`, `ContentTruncated`, `ToolName`, `ToolInput`, `ToolUseID`, `BashCommand`, `BashExitCode`, `ToolFilePath`, `SkillName`, `DurationMs`, `Interrupted`, `IsError`, `OutputTokens`, `Timestamp`, `MessageID`, `ParentSessionID`, `IsSubagent`. **Note:** `ToolUseResultJSON` exists on the struct but `SessionMessages` does **not** SELECT it (only `GetMessageByID` does) — the builder uses the result row's `Content` only; the structured-envelope gap is accepted/deferred.

<!-- RESOLVED(P2): SessionMessages does not load ToolUseResultJSON
REVIEW: context lists ToolUseResultJSON as available on MessageRow, but `SessionMessages` (query_sessions.go:331–346) omits `tool_use_result_json` from its SELECT — only `GetMessageByID` populates it. The export builder should either extend SessionMessages or note the structured-envelope gap.
AUTHOR: Confirmed. Per user direction, we accept the gap for now (no query change): corrected the MessageRow field list above to note ToolUseResultJSON is not loaded by SessionMessages, and the builder uses the result row's Content only. Documented as a deferred limitation in plan.html → Verification → Known gaps.
-->

### Canonical schema / truncation
- `auto-shared/model/schema.go` — `DefaultTruncateMaxChars = 4096`, `DefaultTranscriptMaxChars = 512*1024`; `content` (full) vs `content_truncated` (~4k mid-truncated). Sub-agent fields: `parent_session_id`, `is_subagent`, `subagent_name`.

### HTML / embed precedent
- `auto-ui/web/embed_prod.go:1-24` — `//go:embed all:static` + `fs.Sub` is the repo's pattern for shipping static web assets in the binary. Our renderer will `//go:embed` a single viewer template (HTML+CSS+JS shell) and inject the model JSON.
- `auto-env/internal/template/template.go`, `auto-watch/internal/daemoninstall/template.go` — `text/template` precedent (no `html/template` in use). We inject a JSON blob into a `<script>`, so we use a placeholder replace + script-safe escaping, not `html/template` auto-escaping.

### Testing
- `auto-search/internal/cli/cli_integration_test.go:90-160` — `runCLI(t, args...)` harness (captures stdout/stderr/exit code via `app.New` + `cli.NewRootCmd`) and `setupIndexedFixtures(t)` (temp `$HOME` → `init` → `index --input testdata/etl-output`).
- `auto-search/internal/testutil/fixtures.go:28-132` — `GenerateFixtures(outputDir)` writes parquet sessions/messages incl. a parent (`test-session-1`) + subagent (`test-session-2`, `ParentSessionID=test-session-1`, `SubagentName=Explore`). Reusable for an export integration test; may need a richer fixture (a Bash tool row, an `Agent` dispatch row) to exercise the renderer.

<!-- RESOLVED(P2): Existing Bash row is not paired tool_use
REVIEW: `msg-005` is a standalone `role=tool` Bash row with no `tool_use_id` and no matching assistant `tool_use` row. The prototype/builder only emits Bash events from assistant tool_use rows paired to results, so this row will not appear until the planned paired fixture rows are added.
AUTHOR: Verified and flagged the fixture enrichment as required (not optional) in plan.html → Plan → Phase 3 step 2: add a paired assistant tool_use + tool_result (shared tool_use_id, with bash_exit_code/duration_ms) and an Agent dispatch matching test-session-2's intent, so AC-3 has a renderable Bash event.
-->

## Patterns

- **Sub-agent correlation** (verified against a real `/execute-task 041` session): the parent's `Agent` tool_use `ToolInput.prompt` exactly prefix-matches each child's `FirstUserIntent`; children are grouped by `parent_session_id`. Match in dispatch order with prompt-prefix as the key, order as the fallback. No schema change required. (The dispatch tool is named **`Agent`**, not `Task`, in current Claude Code data.)
- **Tool pairing**: a `tool_use` assistant row and its `tool_result` (role=`tool`) row share `ToolUseID`. Build a `map[ToolUseID]MessageRow` of results; `DurationMs`/`Interrupted`/`IsError`/`BashExitCode` live on the result row.
- **stdout vs stderr** (`docs/auto-package-patterns.md:296-312`, `CLAUDE.md:18-42`): stdout = parseable payload; diagnostics/errors → stderr. This command writes a **file**, so the path + byte size (and the >5MB `--light` hint) go to **stderr**; stdout stays empty (reserved for future `--out -` streaming).
- **Error remediation** (`CLAUDE.md`): every hard error carries a fix hint (e.g. "session not found … run: auto search index").
- **Existing `session get` rendering** (`session.go:310-412`) — `messageContent`, `roleTag`, `midTruncate`, `truncateStr` — show the established summary/truncation idioms to mirror (tool labels, `cmd=`/`path=` previews, `duration_ms` surfacing).

## Conventions / constraints to honor

- `export` is **not** in the documented resource verb triad (`list`/`describe`/`get` + `search`, `docs/auto-package-patterns.md:239-287`). It is an intentional new file-writing render verb (user decision); flag it as a deliberate deviation, not an oversight.
- Non-JSON output is explicitly allowed for transcript-retrieval commands (`auto-search/docs/requirements.md:26-34`) — precedent for emitting HTML.
- `--format html` default, `--format json` reserved/unimplemented (plan decision).
- Progressive-disclosure audit (`auto-search/docs/progressive-disclosure-audit.md`) names **thinking blocks** and **full-session retrieval** as the top P1 information losses — this export is the "full-session escape hatch" and should preserve thinking by default.

## Related Tasks
- **Task 013 (auto-ui-tech-base, `d3a659d`)** — introduced the `//go:embed all:static` + build-tag split (`embed_prod.go` / `embed_dev.go`) we copy for shipping the viewer template.
- **Task 015 (session-intent-summary, `f1f7467`)** — end-to-end schema threading (parquet → DDL → InsertSession → query → CLI) and the lesson that parquet fixtures must be regenerated after a schema change (a schema-version bump forces a full reindex). Relevant only if we extend the fixture.
- **Task 042 (auto-ui-proxy-backends)** — auto-ui embed pattern reference.
- **Progressive-disclosure audit** (`auto-search/docs/progressive-disclosure-audit.md`) — frames this export as the P1 "full-session escape hatch"; `transcript_full` is dropped at index time (`indexer.go:285`), so we reconstruct from `SessionMessages`, not a stored full transcript.

### Git provenance (the data this depends on)
- `41bf300` — added `ParentSessionID` + `--parent-session` drill-down + `bash_exit_code` (Search schema 3→4).
- `65189bb` — exposed `is_subagent` / `subagent_name` / duration + `--subagent`/`--no-subagent`.
- `042857e` — per-tool-call `duration_ms`, `tool_use_id` pairing, `interrupted`.
- `c11e5cb` — `tool_use_result_json` structured tool output.
- `e461855` — `session list`; closest CLI sibling to add `export` beside.
- **Caveat**: the progressive-disclosure audit claims thinking blocks are dropped in ETL — that is **stale** for recent data (the real `/execute-task 041` session's thinking blocks are indexed and rendered in the prototype). Export embeds thinking when present; `--exclude-thinking` simply omits it. No dependency on the audit's claimed gap.
