# Context: Task 063 — auto-mail #parent handle

Codebase facts gathered for [plan.html](plan.html) (epic 005, T2). Paths are
repo-root-relative.

**Status: the D-063-7 gate is satisfied.** PR #146 merged to `main` on
2026-08-31 as `0c73e78`, so `auto-mail/` now exists on `main` and the 063
worktree should be cut from `main`. This document was originally written against
the unmerged branch at `fbefad8`; every path and line number below has since
been re-verified against merged `main`, and the citations were corrected.

Two commits landed on the PR **after** this document was first written, and both
changed things it quotes:

- `94cc014` — the `--from-now` cursor became a store-owned log position
  (`subscriptions.from_cursor` is now an INTEGER `seq`, not a minted ULID), and
  the same sweep reordered `AddressForBinding` by `seq`. It also bumped
  `schemaVersion` to 2 and added an `ErrSchemaMismatch` guard.
- `9293cc4` — `store.Open` sets `busy_timeout` before `journal_mode = WAL`.

The affected claims are corrected in place below and flagged where a commit
message quoted here has been superseded.

## Key Files

### The two things `#parent` is built from (both already exist)

- `auto-mail/mail/binding.go:26-58` — `BindingFor(cwd)` and
  `BindingFromContext(captured, cwd)`. The ladder is `tmux_pane_id` →
  `NTM_SPAWN_BATCH_ID[/NTM_SPAWN_ORDER]` → resolved `cwd`. Its doc comment
  already names this task: "the subscribe process is a tool call and never sees
  the hook payload's session_id, and bridging that gap is exactly what D-13
  defers to T2."
- `auto-mail/mail/direct.go:164-189` — `resolveFrom`, the three-rung ladder.
  **Rung 2 is `#parent`'s resolution, already written**:
  `d.store.AddressForBinding(ctx, caller(in.Binding))`. Rung 3 falls back to
  `<projectId>/agent` via `projectAddress(cwd)`.
- `auto-mail/internal/store/project.go:712-736` — `AddressForBinding` is the
  lookup. When a binding holds several subscriptions it returns **the first one
  created by the log's ordering** (`ORDER BY s.seq`), not the lowest minted id:
  a `sub_` id is a ULID prefix, so "ordering by id would make this rung's answer
  depend on entropy. Ordering by the subscribed event's seq makes it depend on
  the store." (Changed by PR #146's post-review sweep, commit `94cc014` — later
  than the branch state the rest of this document was read against.)

### The artifact the marker is modelled on

- `auto-mail/mail/pending.go:44-48` — `flagName(b Binding)`:
  `hex(sha256(manager + "\x00" + target)[:8])`. The NUL separator is why
  `(a, bc)` and `(ab, c)` cannot collide. `Session` is deliberately excluded
  from the identity.
- `auto-mail/mail/pending.go:50-84` — `flagPath`, `addressable` (an empty pair
  is refused rather than becoming a global flag), and `HasPending`, whose
  contract is the one the marker copies: **no error return, no store open, every
  failure reads as "absent"**, because "this runs on every tool call of every
  agent on the host, and a caller in that position has nothing useful to do with
  a failure except ignore it."
- `auto-mail/mail/pending.go:17` — `var openStore = store.Open`, a package var
  *specifically so a test can count store opens*. AC-10 of task 062 asserts zero
  opens on both hook paths through it; task 063's marker path must keep that
  count at zero.
- `auto-mail/internal/config/settings.go:55-70` — `MailDirIn/StorePathIn/
  FlagsDirIn(home string)`: "The hook path needs the locations without
  consulting the environment, so every path helper has a home-relative twin."
  The marker directory needs the same twin.

### The one call site outside the mail package

- `auto-cli/cmd/auto/hookscmd.go:74-90` — the payload is already parsed into
  `map[string]any` and `stringField(payload, "cwd")` / `"hook_event_name"` are
  already read. `agent_id` / `agent_type` are two more `stringField` calls on a
  map that is already in hand.
- `auto-cli/cmd/auto/hookscmd.go:100-110` — `hooks.Append(env)` runs **before**
  the best-effort POST; `docs/auto-bus-spec.md:286` makes this ordering
  normative ("the durable hook-event append runs before the live post and is the
  canonical record").
- `auto-cli/cmd/auto/hookscmd.go:124-141` — the emission block T1 added:
  `emitAdditionalContext(out, stringField(payload, "hook_event_name"),
  mailNudge(cwd, hookCtx), matchHint(...))`, with the comment fixing mail first
  so "a project's hint rules must never be able to suppress or bury the nudge".
- `auto-cli/cmd/auto/hookscmd.go:147-171` — `mailNudge(cwd, hookCtx)`, the
  existing three-line adapter: resolve home, `mail.HasPending(home,
  mail.BindingFromContext(hookCtx, cwd))`, return `mail.NudgeText()`. The marker
  write is a sibling adapter of the same shape.
- `auto-cli/cmd/auto/hints.go:199-220` — `emitAdditionalContext`: PostToolUse
  only, drops empty fragments, writes **exactly one** `hookSpecificOutput`
  object or zero bytes.
- `auto-cli/cmd/auto/hooksinstallcmd.go:18,23,34,40` — the installed event sets.
  **`PreToolUse` and `SubagentStop` are both present for both `claude` and
  `codex`.** This is the dependency that makes the marker possible; nothing in
  `docs/` documents this set, so a regression here would be silent.

### Address validation and the envelope

- `auto-mail/mail/address.go:29-51` — `ValidateAddress`: rejects empty,
  whitespace-padded, control characters, and `> MaxAddressLength` (256). Its
  comment is explicit that `/` "is a permitted, ordinary character" and that
  permissiveness is deliberate (D-9). **It accepts `#parent` today**, which is
  why reservation must happen at the CLI boundary, above validation (D-063-1).
- `auto-mail/internal/store/project.go:253-264` — the envelope is
  `json.Marshal(map[string]any{"version", "to", "from", "sentAt"})` written into
  a TEXT column. Adding an `attributes` key is additive with **no schema
  change**.
- `auto-mail/internal/store/project.go:561-576` — `hydrate` decodes only
  `{"from"}` out of the envelope. Surfacing attributes on `list` means
  extending this struct and `ListedMail`.
- `auto-mail/internal/store/schema.go` — `schemaVersion = 2`, `CREATE TABLE IF
  NOT EXISTS` only, plus `events_no_update` / `events_no_delete` /
  `mail_no_update` / `mail_no_delete` triggers enforcing G1 in the database.
  **`Migrate` now guards the version**: a store stamped with a different version
  fails with `ErrSchemaMismatch` rather than being silently reopened, because
  `CREATE TABLE IF NOT EXISTS` would otherwise leave the old tables in place.
  G10 offers no migration, so the only remediation is `auto mail reset --yes`
  (which wipes without opening the store). Consequence for this task: adding an
  `attributes` key to the envelope is still additive and needs **no** version
  bump — the envelope is JSON in a TEXT column — but any change that does touch
  the schema must bump `schemaVersion` and will strand existing alpha stores.
  (Version 2 and the guard arrived in PR #146's post-review sweep, `94cc014`,
  later than the branch state the rest of this document was read against.)

### The harness (G15's oracle)

- `harness/src/harness/scenarios/mail_flow.py:30-52` — `WORKSPACE_A` /
  `WORKSPACE_B`, `FLAGS_DIR`, `UNINITIALISED_HOME`, `DEFAULT_POLL_TIMEOUT`.
- `harness/src/harness/scenarios/mail_flow.py:66-110` — `check_ready()`:
  fail-fast gates including "stray pending flags at stand-up" — the marker
  directory needs the same gate for the same reason.
- `harness/src/harness/scenarios/mail_flow.py:118-133` — `mail(agent, *args)`,
  base64-piped into `sh` so no argument is mangled by outer shell quoting. This
  is what lets a test pass a literal `#parent` safely.
- `harness/src/harness/scenarios/mail_flow.py:199-227` — `fire_hook(agent,
  agent_kind="claude", home=None)`: loads `fixtures/hooks/post-tool-use.json`,
  patches `cwd` and `tool_input.file_path`, base64-pipes it into `auto hooks
  fire`, asserts exit 0 because "a hook must never break the agent". The
  sub-agent fixtures extend this.
- `harness/scenarios/mail-flow/fixtures/hooks/post-tool-use.json` — the whole
  fixture is 8 lines: `hook_event_name`, `tool_name`, `session_id`, `cwd`,
  `tool_input.file_path`. Sub-agent fixtures are this plus `agent_id` and
  `agent_type`.
- `harness/CLAUDE.md:136-137` — "**Missing seams are findings, not patches.**
  Scenarios use existing product seams only."
- `harness/CLAUDE.md:74-98` (added by 062) — the mail-flow note: on-disk state
  is session-scoped and shared across tests in the module, so "a new test wants
  its own address and should ack everything it sends".

### The ontology (G16)

- `docs/concepts/UBIQUITOUS_LANGUAGE.md` — on `main` it has 13 terms (Session,
  Subagent, Message, Host, Project, Rule, Observation, Playbook, Event, TaskDef,
  Trigger, Skill, Context Pack). In the 062 worktree it has a `## Mail` section
  adding **Mail, Address, Subscription, Delivery, Binding**.
- **`Subagent` is already canonical** ("A Session spawned by another Session to
  handle a subtask… `_Avoid_`: Child session, nested session, sub-session"), so
  prose, code comments and error text must say *Subagent*, not "child session".
- Entry format: `**Term**:` on its own line, definition, mandatory `_Avoid_:`
  line (`—` if none), optional `_Has_:` line using only `one`/`many` + defined
  term names.
- `.claude/skills/domain-modelling/scripts/glossary.py` —
  `python3 .claude/skills/domain-modelling/scripts/glossary.py check docs/concepts/UBIQUITOUS_LANGUAGE.md`
  (exit 1 on error) and `… diagram docs/concepts/UBIQUITOUS_LANGUAGE.md --write`
  (regenerates the block between `<!-- ER-DIAGRAM:START … -->` and
  `<!-- ER-DIAGRAM:END -->`; a term with no `_Has_` and nothing pointing at it
  does not appear as a node at all).
- The frontmatter `summary` enumerates the terms and is mirrored into the root
  `CLAUDE.md` autodoc index. Three different commands, easily conflated:
  `auto doc fixed <file>` refreshes the freshness hash; **`auto doc agents`** is
  what rewrites the `<!-- autodoc: start -->` block in `AGENTS.md`/`CLAUDE.md`
  (`auto-doc/internal/commands/agents.go`); `auto doc fix` only *reports*, and
  its own output says "Run `auto doc agents` to update agent memory files"
  (`auto-doc/internal/commands/fix.go:227`). 062 made `autodoc-fix` a
  **blocking** pre-commit check.

## Patterns

- **Remediation is mandatory.** `docs/auto-package-patterns.md:454-468` and the
  root `CLAUDE.md`: "Every hard error should include a concrete remediation
  hint", and error messages must say (a) what is wrong and (b) how to resolve
  it. Hard errors use `&ExitError{Code: 1, Err: …}` — **exit codes other than
  0/1 are not an established convention**, so "not a Subagent" is a `Code: 1`.
- **JSON on stdout, diagnostics on stderr.** In JSON mode stdout stays strictly
  parseable; the remediation hint goes to stderr. On a hard error stdout is
  empty.
- **A hook can never fail.** `docs/auto-bus-spec.md:286`: `auto hooks fire`
  "always exits 0 so it cannot disrupt the agent's critical path", and the
  durable append precedes any best-effort work.
- **Sentinel errors, then wrap.** `auto-mail/mail/client.go:16-30` declares
  `ErrUnknownMail`, `ErrNoDelivery`, `ErrInvalidAddress`, `ErrStoreNotEmpty`;
  call sites wrap with `fmt.Errorf("…: %w", err)`. New failure modes follow that
  shape so the CLI can branch without string matching.
- **`docs` is the discoverable surface.** `auto mail docs` is an embedded
  markdown command reference (D-062-4 ships it in T1 precisely because "an agent
  must be able to discover the surface it is being asked to use"). A handle that
  is not in it does not exist as far as an agent is concerned.
- **Harness discipline**: fail-fast readiness gates before any assertion;
  bounded retry on the observable outcome, never poll-to-settle; presence not
  counts; a missing product seam is a finding, not something to patch around.
- **Makefile targets**: the default target is `build` and gates nothing. The real
  gates are `make check` (itself `fmt-check vet lint stale-refs`), `make test`,
  `make test-race` and `make vulncheck`. `RACE_PROJECTS` already reads
  `auto-shared auto-watch auto-mail` after 062, so the concurrent-Subagent tests
  need no Makefile edit.

## Verified ground truth — the hook linkage

Read from `~/.auto/hooks/raw/events-2026-08-25.jsonl` on 2026-08-25 (872 of that
day's payloads carry `agent_id`):

- A Subagent's own tool call fires `PreToolUse` / `PostToolUse` carrying the
  **parent's** `session_id` and `transcript_path`, plus the child's own
  `agent_id` (`atask-063-planner-ebd9de6a1b4a8259`) and `agent_type`
  (`task-063-planner`). No join, no lag.
- The **parent's own** tool calls carry no `agent_id` at all (confirmed on a
  `tool_name: "Agent"` PostToolUse — the very call that dispatched a Subagent).
  Presence of the field is the discriminator.
- `SubagentStop` carries `agent_id` plus `agent_transcript_path`. One observed
  `SubagentStop` had `agent_type: ""` with a non-empty `agent_id`, so
  `agent_type` is **not** guaranteed.
- Two different parent sessions were live concurrently in that log
  (`ef7110ff-…` in `/home/vscode/src/auto-stack` and `aafde709-…` in the 062
  worktree), each with its own Subagents — different `cwd`, therefore different
  cwd-rung bindings, therefore no cross-talk.
- The Subagent's process environment carries `CLAUDE_CODE_SESSION_ID` set to the
  **parent's** id, `CLAUDE_PID` set to the parent process, and no agent identity
  of any kind — D-13's premise, confirmed directly.
- Corroboration from the transcript side:
  `docs/reference/claude-project-files-schema.md:362-366` records `agentId` as
  "Absent" on parent lines and "Present on every" Subagent line. Note the
  spelling difference: transcripts use camelCase `agentId`/`agentType`, hook
  payloads use snake_case `agent_id`/`agent_type`.

## Git history (verified)

Commits whose decisions bind this task. Bodies read with
`git log --format='%h %s%n%b'`.

- `7615caa feat(062): phase 3 - in-band mail nudge on hook fire at stat cost` —
  the commit 063 sits directly on. Four constraints recorded in its body:
  **`binding-reuse`** — derive the Binding from `BindingFromContext(hookCtx, …)`,
  the context the hook *already captured*, never a second `BindingFor(cwd)`,
  because `CaptureContext` shells out to tmux; **`emission`** — exactly one
  `hookSpecificOutput` object or zero bytes, mail fragment first;
  **`nudge-text`** — the nudge is a constant, never interpolated;
  **`hook-safety`** — no error return, cannot block, every failure reads as
  "no mail".
- `871dafd feat(062): phase 2 - event log, projection, subscribe/send/list/ack` —
  **Subscription identity is `(address, caller binding)`**; the `from` ladder is
  recorded as a `constraint(seam)` with rung 2 as "the address of a subscription
  bound to the caller, lowest subscription id" (**superseded** by `94cc014`,
  which reorders that rung by the log's `seq` — see `AddressForBinding` above);
  **an unreadable project registry
  is treated exactly like an unregistered directory — `send` must never fail
  because the host's project list is missing.**
- `e1812b0 feat(062): phase 4 - cursor backfill, broadcast delivery and the
  mail.Client conformance suite` — `RunSuite(t, newClient)` exists *so T3's RPC
  client is a second `go test` target rather than a redesign* (D-062-5), which is
  why a new Client-visible behaviour belongs in the suite.
  `constraint(no-ordering-assertions)`: nothing may assert ULID ordering (G4).
  `constraint(stand-up-hygiene)`: `check_ready()` gates on the flag directory
  being empty-or-absent, because a stray flag is a false-positive nudge for
  whatever binds to that pair next — the marker directory needs the same gate.
  **Standing gap it names: an orphaned Subscription (binding row gone) is never
  reclaimed, and "a `#parent` resolver must decide what happens when the
  parent's binding is gone."** Task 063's answer is `ErrNoSupervisor` (AC-10).
- `fbefad8 feat(062): phase 5 - mail immutability, ack-race, reset, seam guard
  and the alpha contract` — the **seam guard's exact scope**: a file violates
  when it *names* the store and *reaches for a database in the same file*, or
  imports `auto-mail/internal/…` from outside the module. **Naming the store
  without opening it stays legal**, which is what `auto-cli`'s hook test and the
  harness already do. Also: a concurrency test sharing one `*sql.DB` proves
  nothing — N separate handles is what N CLI invocations actually are.
- `d43ee59 feat(062): phase 1 - ontology, module skeleton and the mail-flow
  scenario` — the glossary landed **before** any Go (G16). And a hard lexical
  constraint enforced by test: `auto-mail/mail/mail_test.go:63-75` greps types,
  funcs, fields, JSON tags, SQL table names and event types for the word
  *message* as a whole-word identifier component; the **only** permitted use is
  the `--message` body flag and the `{"message": …}` body key. New identifiers
  in this task (`Sender`, `ActiveAgent`, `Handle`) must stay clear of it.
- `d783cd8 feat(053): auto-hook-hints` — established that `auto hooks fire` may
  write to stdout **only** on `PostToolUse`. The 053 docs record why it matters:
  **Codex's `Stop` / `SubagentStop` reject invalid stdout.** So the marker write
  on `SubagentStop` must emit nothing at all.
- `71005da feat(020): auto hooks install` — the installed event allowlist,
  including `SubagentStop`, and the original invariant that `fire` is a pure
  observer that always exits 0.
- `1b1dada feat(022): hook-event-log` — `hooks.Append` writes the durable JSONL
  **before** the lossy POST; `ExtractSessionID` already reads
  `payload["session_id"]`.
- `66e9c4c feat: project registry plumbing` — `~/.auto/projects.json` is written
  atomically (temp + rename); read-modify-write locking was noted as a follow-up
  and never done.

There is **no `agent_id` handling anywhere** in `auto-shared/hooks/` or
`auto-cli/cmd/auto/` today (grep is empty). Task 063 introduces the first reader
of that field in this repo.

## Prior art for a marker file under `~/.auto`

- **062's pending flags** are the closest precedent and the shape to mirror:
  `~/.auto/mail/alpha-flags/<hex(sha256(manager+"\x00"+target))[:8]>`, empty
  file, `O_CREATE|O_WRONLY` 0644, directory `MkdirAll` 0755. Hashing rather than
  escaping because the identity components are not legal filenames; the NUL
  separator prevents `("a","bc")` colliding with `("ab","c")`; an empty pair is
  refused rather than allowed to hash to one shared global name. **No TTL** —
  cleanup is self-healing through `list`/`ack`.
- **`auto-watch`'s `daemon.lock` / `daemon.pid.json`**
  (`auto-watch/internal/config/paths.go:14-16`) — liveness is a flock, i.e. the
  OS's job, with **no stale-PID detection**; the JSON alongside it is advisory
  metadata readers degrade past silently. The lesson carried into D-063-9: pick
  a cleanup signal the OS or the producer gives you, and treat a timeout as a
  backstop rather than the mechanism.
- **055's safe-order cleanup rule** (`docs/tasks/055-autowatch-daemon-hardening/feedback.md:16`)
  — list candidates, remove, and only then forget them; a failed removal leaves
  the entry for the next pass. Applies directly to opportunistic marker expiry.
- **`~/.auto/skills/receipts/<project-id>.json`** and `~/.auto/etl/*/sync-state.json`
  — machine-local state keyed by a stable id, for shape reference.

## Related Tasks

- **Task 062 — auto-mail-walking-skeleton (epic 005, T1)** is the direct
  predecessor and the base. Its D-062-2 (binding as the hook↔CLI join key),
  D-062-3 (the stat-only pending flag), D-062-8 (one exported domain package)
  and D-062-9 (one `hookSpecificOutput` object per event) are all load-bearing
  here. Its **PR #146** is open and unmerged. **063 must not start until 062 is
  merged to `main`** (D-063-7).
- **Task 053 — auto-hook-hints** built `hints.go`, the
  `hookSpecificOutput.additionalContext` seam and the one-emission discipline.
- **Task 047 — hook-retarget-autowatch** made `auto hooks fire` the sole hook
  ingest, which is why the marker belongs there and not in a new hook.
- **Task 058 — promote-e2e-harness** created `harness/` and the "missing seams
  are findings" rule.
- **Task 022 — hook-event-log** created `~/.auto/hooks/raw/*.jsonl`, the log
  this task's ground truth was read from.

## Findings raised, not fixed

Two gaps noticed while gathering context. Neither is in scope; both are recorded
so they are not rediscovered.

- **The installed hook event set is undocumented.** Nothing in `docs/` records
  which events `auto hooks install` writes into an agent's settings. Task 063
  depends on `PreToolUse` and `SubagentStop` being among them, so it adds a
  regression test asserting it, but the documentation gap remains.
- **`agent_id` never reaches the bus.** `docs/auto-bus-spec.md:296-330` defines
  `ToolPost{tool, event, paths[], raw}` where `raw` is "the agent's original
  `tool_input` JSON, verbatim" — and `agent_id` is a *top-level* payload field,
  not inside `tool_input`. So a bus consumer (`auto-ui`'s future supervisor
  view) cannot currently tell parent work from Subagent work. Mail does not need
  the bus for `#parent` (D-062-6 keeps the bus out of T1/T2 entirely), so this
  is left alone.
