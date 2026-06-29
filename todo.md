# TODO.md

> Organized as one `##` section per tool (canonical binary name), with `###`
> sub-groups inside larger ones. Cross-cutting notes live at the top; planned
> (not-yet-built) tools are grouped at the bottom.

## Bottlenecks — current state

What are the current bottlenecks?

- Validation / verification. Need to be able to view screenshots, terminal videos, verification agent outputs, etc.
- General sense of chaos. What do I have in flight? What is stuck in worktrees? What work has been orphaned from a worker? What is actually happening??
- Multi-stage work gets chocked up, too much gating, waiting, etc.
- Agents probably spend a lot of time down, waiting for things to do
- Do we have enough tmux worker instances? how to create/kill them automatically?

## Session quality scoring (cross-tool)

- [ ] Calculate a quality score per coding session based on ETL + search data
- [ ] Top score = agent one-shotted the implementation with minimal corrections
- [ ] Negative signals: multi-line edits to same file, repeated tool failures, user corrections, PR feedback requiring changes, long back-and-forth loops, rollbacks
- [ ] Positive signals: clean first-pass implementation, tests passing on first run, no user corrections, small diff-to-task ratio
- [ ] Should be fast to compute from existing parquet/indexed data (no LLM calls)
- [ ] Optionally include static analysis signals: cyclomatic complexity, lint warnings introduced, code churn
- [ ] Use scores to identify which workflows/prompts/patterns produce the best outcomes
- [ ] Feed into auto-reflect for pattern extraction (what do high-scoring sessions have in common?)

## auto-doc

- [ ] exclude .claude folder by default
- [ ] support filtering by `tags: []` front matter value (e.g. `tags: [needs-review]`), so autowatch cron triggers can pick up on tagged docs
- [ ] fix source tag scanning to skip directories (crashes on `.claude/skills/open-prose` directory). The `ignores` config only applies to doc scanning, not source tag scanning.

## auto-etl

Align with user-journey spec.

- [ ] add `quickstart` command
- [ ] fix: warning: `~/.auto/host.json` not found, using hostname:
- [ ] ensure sessions/messages include `model_id`
- [ ] **[PARKED — needs more design]** Live tier for monitoring: batch ETL (parquet) is too stale for live queries ("what is job X doing now?"); building a separate monitoring store would recreate data auto-etl already owns. Direction: one schema / one transform / many materializations — a hot tier (pure-Go SQLite, fed by a continuous tailer of the JSONL log) in front of parquet, behind one query surface; ETL becomes a compaction flush, not a daily job. DRAFT/RFC: `docs/live-tier-architecture.md`. Open questions to resolve before building: hot-store retention window, compaction trigger, multi-host query scope, hot/cold schema unification, replay-on-restart.

## auto-reflect

- [ ] **Playbook auto-injection — the "last mile" v1** (the impact unlock). Retrieval is currently a *manual* act (`/playbook-search`, `auto reflect retrieve`), so the playbook only helps when someone remembers it exists. v1: a hook (lean `UserPromptSubmit`) that fires at task start, retrieves the **top ~3** rules for the prompt, and **injects** them into the agent's context automatically — turning the playbook from a queryable store into an ambient assistant. Reuses the hook bus (task 021), the `retrieve` command, and the event log. **Task 054 (non-excluding IDF boost) was the precondition** — you can't auto-inject from a matcher that catastrophically excludes or floods context. Pair with a minimal feedback signal + one dogfood proof point; **spike first** (does it visibly help on a few tasks?) before building. Full writeup: `auto-reflect/docs/playbook-auto-injection.md`. Parked — circle back later.
- [ ] Capture requirements-scoping decisions as minable signal. Today the tool models `observation` (kind: `correction|pattern|gap|incident`) and `rule` — there is **no `decision` concept**, and nothing captures the answers a user gives to scoping questions (e.g. AskUserQuestion during `/new-task`). Those answers currently live only in the planning doc (`<pd-decision>`/`<pd-question>`/`<pd-answer>`), contextual commits, and the raw transcript — and the structured AskUserQuestion payload is dropped during ETL (see `docs/research/askuserquestion-analytics.md`). Proposal: add a `decision` observation kind (or a dedicated decision event) so scoping Q&A becomes first-class, queryable signal that can feed back into asking better questions. Related design: `docs/better-questions.md`, `docs/claude-decision-intelligence-deep-dive.md`.
- [ ] `auto reflect export` — self-contained HTML dump for full human inspection of the playbook, mirroring `auto search session export` (which emits a self-contained HTML work-graph map of a conversation). Today QC requires assembling the picture by hand across `rule get` → `observation list` → `auto search session get` — there is no single artifact to review. The export should render, in one file:
  - the **playbook**: every rule (all lifecycles) with use_when / content / causal_note / type / domain / lifecycle.
  - the **observations**: all 266+ observations grouped by kind/domain/severity, with their evidence quotes + file:line.
  - **lineage**: each rule expandable to the source observations it was distilled from (via `observation_ids`), and each observation linked back to its evidence session(s).
  - ideally **traces back to original conversations**: deep-link or inline each observation's evidence `session_id`/`message` into the corresponding `auto search session export`/`session get` rendering, so a reviewer can jump rule → observation → the exact transcript moment.
  - surface **coverage gaps**: which observations are `--unconsolidated` (folded into no rule), and any rules that can't auto-promote (thin/single-session evidence).
  - reuse the `auto search` HTML export machinery rather than reinventing the renderer.
- [ ] Capture concrete **trigger conditions** at observation time, not just a prose `use_when`. Today an Observation/Rule's applicability is a hand/LLM-written natural-language string (e.g. "when you run the tests, always output JSON"), and the matcher does lexical keyword overlap against it. The lesson is usually learned *while specific commands/tools were running* — so at observe time we should also capture the **trigger signature**: example bash commands (`go test ./...`, `pytest`), file globs/paths touched, tools used, and the operation in play. Two payoffs: (a) a future **bash-command / hook query interface** can match an incoming command against these exemplars far more precisely than keyword matching (see the retrieval-eval "query interface validity boundary" — trigger ≠ query ≠ matcher); (b) it enables **value injection** — a rule like "tests → JSON mode" knows the concrete commands it should rewrite/augment. Couples with the **faceted (verb/noun) tag** idea (the verb facet *is* the operation that triggers) — see `auto-reflect/experiments/retrieval-eval/DIARY.md` → "Future bench variants". Design Qs: schema for trigger exemplars on Observation vs Rule; how `consolidate` merges exemplars across observations; whether exemplars are evaluated as a separate retrieval signal or folded into `use_when`.
- [ ] Human-assisted Playbook curation loop for low-confidence Rules. `auto reflect review` should select Rules/Observations that need judgment (thin evidence, conflicting Observations, vague `use_when`, weak causal_note, low feedback signal, or uncertain promote/merge/split/retire action), emit a rich HTML review doc, let a human label/comment on it in `auto ui`, then let an agent consume those comments and apply deterministic Rule updates. The review doc should support labels like `promote`, `rewrite`, `split`, `merge`, `retire`, `needs-more-evidence`, and `wrong-scope`; inline comments should attach to specific Rule fields, source Observations, and Session evidence. The apply step should produce an auditable Reflect Event trail (`rule_edited`, `consolidation`, `feedback`, or follow-up Observation events) and summarize exactly which Rules changed.

## auto-search

- [ ] `truncateStr` in `auto-search/internal/cli/session.go` slices by bytes, not runes — same UTF-8 bug as the `search.TruncateAtRune` fix in PR #47 (`messages.go` / `cli/search.go` snippets). Fires at the 80-char tool-arg preview when args contain emoji, accented chars, or unicode in commands/paths. Route through `search.TruncateAtRune` (or duplicate the `utf8.RuneStart` advance loop) so tool tags in `session get` output stay valid UTF-8.

### Git-related APIs — low-level primitives

- [ ] list files ranked by edit frequency across sessions (most-touched hotspots), filterable by time range and remote.
- [ ] list files edited multiple times within a single session (rework signal — repeated edits suggest complexity or uncertainty).
- [ ] list file edit sequences for a target file — what files are typically edited before/after it (workflow adjacency graph).
- [ ] list tool failure hotspots by file — files associated with the most bash failures or tool errors (problematic areas).
- [ ] list branches ranked by session count and token spend — surface where effort concentrates and long-lived branches.
- [ ] list files edited within a short time window of each other (temporal co-edit pairs) — reveals implicit coupling not visible in imports.

### Query gaps — found during requirements-extraction research

- [ ] `search` command: add `--min-index` / `--max-index` filter on message index. Needed to isolate session-opening messages (e.g. "user messages where index < 5") without post-filtering JSON.
- [ ] Skill metadata indexing: `--skill` filter doesn't match skills invoked via `<command-name>` tags in user messages (e.g. `/new-task`, `/process-requirements`). Only ETL-tracked skills appear in `autosearch skills`. Need to extract skill name from `<command-name>` tags during indexing.

## auto-skill

- [ ] Test/eval mode: simulate skill picking across coding agents
  - load skill frontmatter (descriptions, trigger conditions) into different agents
  - run probabilistic tests with synthetic scenarios to verify correct skill selection
  - measure precision/recall: does the right skill get picked at the right time?
  - regression suite to catch skill description changes that degrade selection accuracy
- [ ] Skill health check: audit total skill load per agent
  - count total skills loaded, warn if too many or too few
  - measure total token cost of all skill frontmatter in system prompt
  - flag when combined skill descriptions consume too much context budget
  - identify redundant/overlapping skills that could be consolidated
- [ ] Skill sync checks. Skills can highlite what binaries are required to execute. then on sync, we check if these are installed and warn if not. `depends_on` or similar.

## auto-artifact image follow-ups

- [ ] Image artifact browsing and progressive disclosure
  - auto-generated descriptions per image (from context or vision model)
  - index file for progressive disclosure (like skill frontmatter — agents see summaries, fetch full images on demand)
  - list/show commands for existing artifacts
  - namespacing to separate different projects (e.g. S3 prefix per project)
  - auto-generates downsized preview thumbnails so agents can browse before fetching full-size originals
  - metadata includes token cost estimate and file size for budget-aware fetching

## auto-mail (planned)

- [ ] Using Using our existing RPC stuff that allows cross-host communication I'm interested in creating a very simple mail inbox system where agents can push messages to inboxes They can then also read from inboxes, pull messages from inboxes etc so that different projects can cross-communicate to each other We maybe keep this really flexible to begin with We have threads, which are a bit like threads and slack or whatever which have topic names or IDs Yeah, I'm not sure when you think about this more
- [ ] Thinking of this a bit like slack, where there are channels like #feeature-ideas that agents can post to, then other agents can peek channel messages and ack them to remove them. Or maybe more like email? not sure. I ilke the idea of virtulised channels, so session/agent identity isn't tied to the channels. But maybe we also have ways to target specific sessions to, we just discourage it. 
- Example use case: agent A is working on tool B. whilst doing this, it has an idea for a feature C. but it can't stop what its doing, so it needs somewhere to sling it where it can be picked up and worked on. Could write to a doc? yes but then you need to nudge another agent to work on it.
- 

## auto-hook

- [ ] capture codex / opencode hooks the same way we capture claude hooks.
- [ ] higher level abstraction for hooks. e.g: whenever an agent opens a pr or pushes to a pr, we want to hint it with some text to say "If you just pushed to a PR and have finished other work, check back in a few minutes and run /address-feedback", but we define this once and it works across all agents.
