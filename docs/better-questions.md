# Ask better questions

How to go from "i have an idea" -> fully mapped out requirements quicker?

## The problem

The user spends a lot of time specifying clear and detailed requirements for each new task. Many of these contain decisions that recur across tasks — storage backend, test strategy, CLI surface shape, validation approach — but the agent has no memory of past decisions and either re-asks or guesses wrong.

## The insight

Requirements mining (see requirements-mining.md) originally framed this as extracting reusable rules into a YAML playbook. But the research from session data shows the real opportunity is different: teach the agent to ask better questions and assume answers to questions it has high confidence data for.

Human decisions exist on a confidence spectrum, not a binary known/unknown:

- **Stage 0 — Unknown.** Agent doesn't know to ask. User has to volunteer the info or correct after the fact. This is where most effort is wasted today.
- **Stage 1 — Learned question.** Agent has seen this category of decision before. It asks proactively: "In past sessions you've chosen between file-based and DB-based storage. Which do you want here?" User still decides, but isn't surprised by the question.
- **Stage 2 — Default with override.** Agent has seen the same answer enough times. It defaults and says why: "Defaulting to postgres (chosen DB over files in 4/5 recent tasks). Say otherwise if this case is different." User can override but usually doesn't need to.
- **Stage 3 — Codified standard.** Graduated into CLAUDE.md or project conventions. Applied silently.

The system is a decision maturity pipeline, not a static playbook.

## Data sources for learning decisions

Research using autosearch found these sources of human decision data across 203 indexed sessions:

### Explicit decisions (structured)

- **AskUserQuestion answers** — 226 answers across 55 sessions. Structured question=answer pairs, each is a single human decision. Examples: "How should init commands be structured?"="Separate subcommands", "For mock mode, how should fake responses be generated?"="LLM-generated fake response"
- **Query:** `autosearch search '"User has answered your questions"' --role tool --limit 100`

### Direct requirement statements

- **/new-task command args** — 7 sessions. User's raw requirement text in `<command-args>`. Dense with implicit decisions.
- **Query:** `autosearch search '"command-name" AND "new-task"' --role user --limit 50`
- **/process-requirements command args** — 6 sessions. Same pattern.
- **Query:** `autosearch search '"command-name" AND "process-requirements"' --role user --limit 50`
- **Opening user messages with intent language** — 51 messages with "I want to" / "I want you to", 11 in first 10 messages of their session.
- **Query:** `autosearch search '"I want to" OR "I want you to"' --role user --limit 100`

### Mid-session corrections and refinements

- **Mind-changes and corrections** — unprompted user messages that redirect or add requirements mid-stream. "I think I've changed my mind and I think we should store these scheduler jobs in the database rather than storing them on disk..."
- **Query:** `autosearch search '"changed my mind" OR "actually" OR "we should" OR "lets change"' --role user --limit 100`
- **Numbered decision lists** — users sometimes answer multiple open questions in one message: "1. yes do that 2. use job_id 3. use events table"
- **Query:** `autosearch search '"1." AND "2." AND "3."' --role user --limit 50`
- **Constraint injections** — user adds a constraint the AI didn't consider: "we need to make it clear in prompt guidance that doltdb uses mysql sql syntax"
- **Query:** `autosearch search '"we need to" OR "make sure" OR "dont forget" OR "make it clear"' --role user --limit 100`

### Requirement-style language

- **Should/must statements** — 56 messages across 47 sessions.
- **Query:** `autosearch search '"it should" OR "must be" OR "should be able to"' --role user --limit 100`

## Synthetic data expansion: decomposing implicit decisions

The /new-task args are dense with implicit decisions the user made without being asked. Each one can be decomposed into individual question-answer pairs, expanding the dataset significantly.

Example from real data:

> "i am interested in adding a module in auto-graph that can a. be pointed at a typescript code base and b. using a mixture of ast-grep and file reading, quickly build a graph of the files including links as to how they import / reference each other. for now we can just focus on imports of files and not symbols"

Implicit decisions embedded in this single input:

- Scope: which language? -> typescript only
- Tooling: what parsing approach? -> ast-grep + file reading (not full LSP, not tree-sitter)
- Data model: what are nodes? -> files
- Data model: what are edges? -> import relationships
- Scope boundary: symbols or just files? -> just files for now
- Architecture: new module or extend existing? -> new module within auto-graph
- Scope boundary: all relationship types? -> just imports

Later in that same session the user volunteered unprompted: "long term i want this tool to eventually be a graph based context engine, that can provide a bundle of context to a user or agent for performing coding tasks." That's the strategic why — no question prompted it. It's arguably the most important input in the session.

### Pipeline for synthetic expansion

1. Extract all /new-task args, /process-requirements args, early user messages, mid-session corrections
2. For each, use an LLM to decompose into implicit Q&A pairs with a category tag
3. Merge with the 226 explicit AskUserQuestion pairs (already structured)
4. Cluster by category across all sources
5. The gap between what the agent asked (explicit) and what the user had to front-load (implicit) is exactly what needs to be learned

Mid-session corrections are especially valuable in reverse: "changed my mind, use postgres instead of files" means there was an implicit question ("storage backend?") that was answered wrong by default and then corrected. That's a question with high value to ask explicitly next time.

Unprompted addenda reveal question categories the agent doesn't have yet — strategy/vision, future extensibility, motivation/why. These are blind spots.

## What the agent could do with learned decision patterns

### Smarter task kickoff

- Before writing requirements, scan decision history for this category of work. "You're building a CLI command — in past tasks you've decided on: JSON default output, --since flag, quickstart + doctor subcommands. I'll assume these unless you say otherwise."
- Present a pre-filled decision checklist at task start instead of a blank canvas. User crosses out what doesn't apply, adjusts what's different, confirms the rest. 30 seconds instead of 10 minutes.
- When the user gives a short /new-task prompt, auto-expand using learned patterns before asking questions. "You said 'add search filtering.' Based on past tasks I'm assuming: case-insensitive matching, normalized input, one filter mode at a time, structured validation errors. Here's what I still need to know: [2 specific questions]."
- Detect when a new task is similar to a past task and surface the diff: "This looks like the etl pipeline work from session X. Key decisions from that: parquet, incremental, fail-fast. Which apply here?"

### Smarter question-asking

- Know which questions the user always answers the same way and which they genuinely deliberate on. Never ask the settled ones. Always ask the contested ones.
- Rank questions by information value. "Storage backend?" changes everything downstream — ask first. "What should the flag be called?" is low-value — just pick and let the user rename.
- Learn question ordering from past sessions. In smooth sessions, which questions were asked first? In sessions with lots of corrections, which questions were missing? The gap is the agent's blind spot.
- Detect when a mid-session correction maps to a question that should have been asked upfront. Retroactively create that question for future tasks.
- Adapt question depth to task complexity. One-liner bugfix doesn't need 15 questions. New module does. Signal is in the data: /new-task args that are one sentence vs three paragraphs.

### Self-correcting defaults

- Track answer consistency per decision category. "Test strategy: real DB" chosen 5/5 times = hard default. "Output format: JSON vs markdown" chosen 3/2 = still ask.
- Detect when a default breaks. If user overrides a default, weaken confidence. Two overrides in a row = demote back to a question.
- Detect project-specific vs cross-project patterns. "Use postgres" might be a gtm-langchain-demo preference. "Structured validation errors" is cross-project. Scope accordingly.
- Surface graduating defaults periodically: "You've consistently chosen X in the last 8 tasks. Want me to add this to CLAUDE.md so it's permanent?" User controls promotion.

### Downstream effects

- Requirements documents become diffs against defaults rather than from-scratch documents. User only specifies what's novel.
- Auto-reflect can score sessions by "how many mid-session corrections would a better upfront question have prevented." Direct quality metric for the question-asking system.
- Decision history becomes institutional knowledge. New projects inherit cross-project defaults.
- Eventually the agent drafts a complete requirements.md from a one-sentence prompt and presents it for approval — 80% of decisions are already known, user's job shifts from authoring to reviewing.

## Autosearch gaps blocking this work

Found during this research — tracked in todo.md:

- `search` needs a `--session` filter to scope queries to a single session
- `search` needs `--min-index` / `--max-index` to isolate early messages without post-filtering
- ~~`--tool-name AskUserQuestion` doesn't surface Q&A pairs — tool name not indexed~~ **Resolved (task 012):** the verbatim `toolUseResult` envelope is now captured in the `tool_use_result_json` column on the `role=tool` row, so Q&A pairs (`$.answers`) and per-question notes (`$.annotations.<question>.notes`) are queryable via `json_extract`, and `autosearch message describe <id>` surfaces the parsed envelope per row. (Tool-name FTS indexing remains a separate gap.)
- `--skill` filter doesn't match skills invoked via `<command-name>` tags (e.g. /new-task) — only ETL-tracked skills appear in the index
