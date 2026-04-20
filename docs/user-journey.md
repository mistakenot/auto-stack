---
hash: "4a6d6cd7"
id: "81eb7b5a"
read_when: "understanding the end-to-end auto-stack workflow and architecture"
summary: "End-to-end walkthrough of the Auto stack: from doc management and session ETL through search, reflection, and automated task scheduling."
title: "Auto Stack User Journey"
---

# User Journey

An end to end showcase of what our desired end goal is, from setting up a new repository through to building a autonomous, self correcting coding system, using the full stack of tools we are building here.

These tools are designed to be used by both humans and coding agents, but **agents are the primary users**. Command outputs default to JSON unless otherwise noted (e.g. `autodoc fix` outputs LLM-friendly markdown, `autosearch session get` outputs a markdown transcript). Human-readable text mode is available via flags where needed.

Our first big milestone will be setting up a simple index-search-reflect workflow in this repository. Longer term milestones will include the newer tools like `autowatch` and `autograph`.

## 1. Init docs and start coding

```bash
# Creates a ./doc folder, with .auto/docs/settings.json file created
# Note: init command on any tool does the same thing if ~/.auto doesn't exist - it creates ~/.auto/settings.json and sets it to { "host": "[current machine hostname"] }
autodoc init
```

Now the user can start creating docs/requirements.md with the plan for the project. They also start creating other documentation files, going into depth on how the project will work. After working for a bit, their code agent runs

```bash
autodoc fix
```

This does a few things:
- Makes sure all docs files in `./docs` have valid front matter, with `id`, `hash`, `title` and `summary` (initially empty, to be populated in next step)
- Then it outputs a LLM friendly text instruction which highlites a few issues:
  - some docs don't have a non-empty `summary` value, tells the agent which files and how to fix
  - tells the agent to run `autodoc fixed ./docs/file.md` once done
  - empty `id` are set to new random values for the docs that dont have it, 8 char hex.
- The agent follows the instructions, setting summaries for the files that need it, one at a time.
- Agent runs `fixed` against the files it updates. This updates the `hash` value for those docs to match `substr(hash(content), 8)` for those files.
- The agent does this until `autodoc fix` returns no errors.

Eventually they start implementing the work and running claude code and codex. As the agents write files, they add `[autodoc(...)]` style links in code comment locations to indicate which doc contributes to understanding that file, class or function.

The `[autodoc(...)]` comments take this form: `[autodoc($docId@$docHash, @codeBlockHash)]`. 
- `docId` is the `id` value from the front matter of the doc.
- `docHash` is the `hash` value from the front matter of the doc
- `codeblockHash` is the hash of the indentation-scoped code block surrounding the tag. Scope is determined by collecting all subsequent lines at >= the tag line's indentation, stopping at the first shallower line (blank lines are transparent). If the tag is at column 0, scope extends to EOF. The autodoc tag itself is stripped before hashing.

This way, `autodoc` can automatically detect drift between code and docs and raise it as an issue.


After some further churn and work, the agent wants to commit. Before doing so, it runs `autodoc fix`. This does this:
- Runs the same checks as above
- Also validates the `[autodoc(...)]` links in code.

It finds that some code blocks have changed since the doc was updated and flags this in the output of hte `fix` command. For these errors, the error contains all the information the agent needs to fix this issue:
- Where the `[autodoc]` comment is
- What the existing code hash is (from the file) and what it should be (calculates the new hash, which will be different if code has changed since last updated)
- What the existing doc hash is, and what the new / unstaged doc hash is (might be the same if code changed, not doc)

The agent works through the changes by:
- Updating doc / code to be what they should be
- Updating the code hash in the code file
- Running `autodoc fixed ./` on the doc file if required

And keeps going until all errors are gone. We can now commit our work.

## 2. Backing up session history

After a bunch of sessions, we decide to back up our coding agent history using `autoetl`.

```bash
# Reads Claude Code session logs from ~/.claude/projects (default)
# Normalizes into partitioned parquet under two datasets: messages, sessions
# Incremental: only rewrites current time period, past partitions are immutable
autoetl run --input ~/.claude/projects --output ~/.auto/etl/output
```

The output directory looks like this:

```
~/.auto/etl/output/
  messages/year=2025/week=12/messages.parquet   ← one file per ISO week
  messages/year=2025/week=13/messages.parquet
  sessions/year=2025/month=03/sessions.parquet  ← one file per month
```

Now, all our sessions are stored in a normalised way on the filesystem. We can do OLAP queries using `duckdb` against the parquet files.

```bash
# Inspect the schemas
duckdb -c "DESCRIBE SELECT * FROM '~/.auto/etl/output/sessions/**/*.parquet'"
# ┌──────────────────────┬─────────┐
# │     column_name      │  column_type  │
# ├──────────────────────┼─────────┤
# │ id                   │ VARCHAR  │  -- session UUID (or agentId for subagents)
# │ parent_session_id    │ VARCHAR  │  -- set when is_subagent=true
# │ host_id              │ VARCHAR  │
# │ agent                │ VARCHAR  │  -- "claude"
# │ subagent_name        │ VARCHAR  │  -- e.g. "Explore", empty for parent sessions
# │ is_subagent          │ BOOLEAN  │
# │ workspace            │ VARCHAR  │  -- cwd of the session
# │ git_remote           │ VARCHAR  │  -- git remote origin URL, for cross-host project identity
# │ model                │ VARCHAR  │
# │ source_path          │ VARCHAR  │  -- original JSONL file path
# │ first_message_at     │ BIGINT   │  -- unix ms
# │ last_message_at      │ BIGINT   │  -- unix ms
# │ total_input_tokens   │ BIGINT   │
# │ total_output_tokens  │ BIGINT   │
# │ total_tokens         │ BIGINT   │
# │ total_bytes          │ BIGINT   │
# │ total_output_bytes   │ BIGINT   │
# │ total_input_bytes    │ BIGINT   │
# │ transcript_full      │ VARCHAR  │  -- all messages concatenated, 100% raw tool outputs
# │ transcript_truncated │ VARCHAR  │  -- same but long tool outputs mid-truncated
# │ year                 │ INTEGER  │
# │ month                │ INTEGER  │
# │ schema_version       │ INTEGER  │  -- currently 1
# └──────────────────────┴─────────┘

duckdb -c "DESCRIBE SELECT * FROM '~/.auto/etl/output/messages/**/*.parquet'"
# ┌──────────────────────┬─────────┐
# │     column_name      │  column_type  │
# ├──────────────────────┼─────────┤
# │ id                   │ VARCHAR  │  -- "{session_id}-{index}", e.g. "abc123-0"
# │ session_id           │ VARCHAR  │
# │ host_id              │ VARCHAR  │
# │ index                │ INTEGER  │  -- 0-based position within session
# │ role                 │ VARCHAR  │  -- user/assistant/tool/system
# │ content              │ VARCHAR  │  -- full content including file contents inline
# │ content_truncated    │ VARCHAR  │  -- same but long tool outputs mid-truncated
# │ timestamp            │ BIGINT   │  -- unix ms
# │ tool_name            │ VARCHAR  │  -- e.g. "Read", "Edit", "Bash"
# │ tool_input           │ VARCHAR  │  -- JSON string of tool parameters
# │ tool_file_path       │ VARCHAR  │
# │ tool_file_start_line │ INTEGER  │
# │ tool_file_num_lines  │ INTEGER  │
# │ tool_file_total_lines│ INTEGER  │
# │ bash_command         │ VARCHAR  │  -- extracted command for bash/shell tools
# │ input_tokens         │ INTEGER  │
# │ cache_input_tokens   │ INTEGER  │
# │ output_tokens        │ INTEGER  │
# │ workspace            │ VARCHAR  │  -- denormalized from session
# │ git_remote           │ VARCHAR  │  -- denormalized from session
# │ git_branch           │ VARCHAR  │  -- active branch at message time
# │ model                │ VARCHAR  │  -- denormalized from session
# │ parent_session_id    │ VARCHAR  │  -- denormalized from session
# │ is_subagent          │ BOOLEAN  │  -- denormalized from session
# │ source_line_index    │ INTEGER  │  -- line in original JSONL, for stable ordering
# │ year                 │ INTEGER  │
# │ week                 │ INTEGER  │
# │ month                │ INTEGER  │
# │ schema_version       │ INTEGER  │
# └──────────────────────┴─────────┘

# Example queries
duckdb -c "
  SELECT id, workspace, model,
    epoch_ms(first_message_at) as started,
    total_tokens
  FROM '~/.auto/etl/output/sessions/**/*.parquet'
  WHERE is_subagent = false
  ORDER BY first_message_at DESC
  LIMIT 10
"

# Find most-edited files across all sessions
duckdb -c "
  SELECT tool_file_path, count(*) as times
  FROM '~/.auto/etl/output/messages/**/*.parquet'
  WHERE tool_name = 'Write'
  GROUP BY tool_file_path
  ORDER BY times DESC
  LIMIT 20
"
```

## 3. Search session history

But now we want to start by doing basic pattern detection to self-improve our project. To do that, we need to use `autosearch` to power full text search of our histories.

First, we need to index the files produced by `autoetl`...

```bash
# creates ~/.auto/search/settings.json
autosearch init

# runs incremental index over parquet output path, defaults to ~/.auto/etl/output.
# `--name` for output index defaults to `default`
# stores its index + incremental state in a sqlite file in ~/.auto/search/default.sqlite
autosearch index

# or this, which uses a different output path instead and a different input dataset
# stores its index + incremental state in a sqlite file in ~/.auto/search/s3index.sqlite
autosearch index --name s3index --input s3://mybucket --key ~/.ssh/my_key
```

Autosearch creates these tables:

<!-- I'm still unsure what the best schema + query patterns are here -->

**sessions_complete**
- contains one row per session
- messages are concatonated together into one `transcript` field.
- tool outputs have their middle truncated if they are too long.
- do full text indexing over `transcript` and other key fields

Now, we are ready to test this with some basic queries. There is one unified `search` command. The `--scope` flag controls *what* is searched (messages or session transcripts), and `--mode` controls *how* (BM25 by default, semantic later). This keeps the CLI surface small — one command to learn, composable with all the standard filters.

Query syntax is an app-level query language, not raw SQLite FTS syntax:
- plain text is supported by default
- quoted phrases are supported: `"\"auth middleware\" retry"`
- boolean operators are supported: `AND`, `OR`, `NOT`
- `autosearch` translates this into the underlying search engine format internally, so callers do not need to know FTS quirks

```bash
### Message-level search (--scope messages, the default)
# Searches individual messages in the index. Returns precise hits with
# snippets, message IDs, and surrounding context. This is the default
# because most searches start by finding a specific moment in a session.

# find messages containing "Exit code 0" — returns message-level hits
autosearch search "Exit code 0"

# same thing, explicit scope
autosearch search "Exit code 0" --scope messages

# time filters work on both scopes — based on message timestamp for
# messages, session start time for sessions
autosearch search "Exit code 0" --since 5d
autosearch search "Exit code 0" --after 2026-03-01 --before 2026-03-07

# message-scope search runs over the `content_truncated` field
#
# --cwd filters to sessions from this workspace. Defaults to current
# directory. Useful when you have sessions from many projects indexed.
autosearch search "Exit code 0" --cwd /home/charlie/src/my-project-root

# --remote filters by git remote instead of local path. Use this when
# you want cross-host matching for the same repo, even if workspace
# paths differ between machines.
autosearch search "Exit code 0" --remote git@github.com:org/my-project.git

# --cwd and --remote are mutually exclusive in v1. If you want to filter
# by local path, use --cwd. If you want cross-host repo identity, use
# --remote.

# --highlight wraps matching terms in **bold** markers in the snippet,
# so the caller can see exactly which words matched without parsing offsets
autosearch search "Exit code 0" --since 5d --highlight

# --request-id attaches a caller-chosen correlation ID echoed back in
# the response _meta block. Agents firing multiple searches can match
# results back to the originating query.
autosearch search "Exit code 0" --request-id "reflect-run-42"

### Session-level search (--scope sessions)
# Searches full session transcripts (the `transcript_truncated` field).
# Returns sessions ranked by BM25 relevance. Use this when you want to
# find *which sessions* are about a topic, not which specific message.
# The reflection workflow typically starts here to triage sessions, then
# drills into individual messages.

# rank sessions by relevance to "auth middleware e2e"
autosearch search "auth middleware e2e" --scope sessions

# find sessions from last 5 days about auth — good starting point for
# a reflection agent that wants to review recent auth-related work
autosearch search "auth middleware e2e" --scope sessions --since 5d

# cross-message patterns work naturally at session scope because the
# full transcript is one document — "user asked about X AND agent hit Y"
# would never match a single message but can match a session transcript
autosearch search "flaky test AND retry" --scope sessions --since 1w

### Semantic search (stretch goal, --mode semantic)
# Uses vector embeddings instead of BM25. Finds conceptually related
# content even without exact term overlap. Works with both scopes.
autosearch search "Auth tests are failing" --mode semantic
autosearch search "Auth tests are failing" --mode semantic --scope sessions

### Rule-based analysis (fix)
# Bundled search rules that run multiple queries under the hood, similar
# to `autodoc fix`. Returns LLM-friendly output saying what was found,
# which rule flagged it, and what the agent should consider doing next.
# likely a v2 feature after core indexing/search/retrieval is working.
autosearch fix --since 5d
```

### Search response format

Search responses are a JSON object with a `hits` array and a `_meta` block. The hit shape depends on the `--scope`:

**Message-scope hits** (`--scope messages`, default):

```json
{
  "_meta": {
    "scope": "messages",
    "elapsed_ms": 12,
    "total_matches": 3,
    "request_id": "reflect-run-42",
    "wildcard_fallback": false
  },
  "hits": [
    {
      "id": "[stable identifier — same query + same params + same result = same id, so we know which results we've already analysed]",
      "sessionId": "[session this message belongs to]",
      "messageId": "[message id]",
      "messageType": "user|assistant|tool|system",
      "score": 1.8,
      "snippetStartIndex": 40,
      "snippetEndIndex": 60,
      "snippet": "[matching snippet, with **bold** markers when --highlight is set]",
      "previousMessageId": "[preceding message id]",
      "nextMessageId": "[following message id]"
    }
  ]
}
```

**Session-scope hits** (`--scope sessions`):

```json
{
  "_meta": {
    "scope": "sessions",
    "elapsed_ms": 25,
    "total_matches": 5,
    "request_id": "reflect-run-42",
    "wildcard_fallback": false
  },
  "hits": [
    {
      "id": "[stable identifier]",
      "sessionId": "[session id]",
      "score": 2.4,
      "workspace": "/home/charlie/src/my-project",
      "firstMessageAt": 1709312400000,
      "lastMessageAt": 1709316000000,
      "totalMessages": 42
    }
  ]
}
```

**`_meta` fields:**
- `scope` — which index was searched (`messages` or `sessions`)
- `request_id` — echoed from `--request-id` if provided, so the caller can correlate responses to requests
- `wildcard_fallback` — `true` when a message-scope exact-term search returned sparse results and was automatically retried with prefix-expanded terms. This tells the caller the results are broader than what was literally asked for.
- `elapsed_ms` / `total_matches` — timing and count for diagnostics

**Wildcard auto-fallback:** for message-scope exact-term search, when the initial search returns very few results, `autosearch` automatically retries with prefix-expanded terms (e.g. `"Exit code"` → `"Exit* code*"`). The `_meta.wildcard_fallback` flag signals when this happened so agents know the results are approximate matches, not exact.

`autosearch` also has a few helper commands. These return LLM friendly output, meaning that it mid-truncates very long outputs (e.g. reading a file) and tries to produce a single output that comfortably fits in the context window...


```bash
# outputs markdown transcript from the session
# - this is reconstructed from the underlying message rows, not by
#   dumping the pre-concatenated session transcript blob directly
# - each message has XML wrappers, like <user id="$messageId" ts="2025-01-01T20:01:02">...</user>, <agent id="$messageId" ts=2025-01-01T20:01:04">...</agent>, etc
# - if a single message is very long, it mid-truncates it
# - subagents: we dont' include the full logs of the subagents, but make it clear when the agent started it/read the response, like <subagent_start  id="$messageId" sessionId="[subagent session id]">[subagent prompt]</subagent_start> and <subagent_output sessionId="..">...</subagent_output>
# - calling `autosearch session $subAgentSessionId` returns a similar result for the subagent
autosearch session get $sessionId

# returns json object with top line stats and metadata:
# - first message time / last message time
# - bytes in count, bytes out count
# - number of messages broken down by: total, tools, bash, read file, write file
# - etc, as much as we can think of
# - transcript_summary is first N chars of the first message + last N chars of the last message with "..." in the middle
# - machine-readable callers can pass --request-id and get a small `_meta`
#   block back alongside the `session` object
autosearch session describe $sessionId

# this returns the FULL message as text
autosearch message get $messageId

# similar to above, returns top level metadata + preview of content (first N chars) as json
# includes denormliased session data to - session id, first session message at, last session message at, etc.
# - machine-readable callers can pass --request-id and get a small `_meta`
#   block back alongside the `message` object
autosearch message describe $messageId

# finds other messages that are similar to the given message id
# returns a search-style `_meta` + `hits` response rather than a single
# describe object, so agents can rank and inspect multiple similar
# messages. Hits include snippet text and neighboring message ids.
# likely a v2 feature once semantic search exists.
autosearch message similar $messageId
```

Questions:
- how to identify the message type? or filter by it?

This forms the building blocks for a self-reflection loop, where we want to:
1. Narrow down to one project
2. Look for recent errors / failures
3. Probe and explore to get the wider context of what the intent was and went wrong
4. Decide on a good new rule to add to the project to prevent it happening again
5. Add that to the session memory

**Example** the user observes the coding agent getting stuck when running E2E tests. The underlying cause is that the context is getting filled with junk that confuses the agent, making it too hard to identify what the underlying cause is. The agent tries to fix the wrong things, reruns the test, and just makes things worse. The fix would be to limit the amount of logging that passing tests produce into the agent context to give it headroom to debug and fix the 1/100 failing tests. but we don't know this yet.

1. User asks reflection agent "find why e2e tests are taking so long and not getting fixed"
2. Agent observes the package.json command `npm run e2e` to start tests, and searches for matching sessions:
   `autosearch search "npm run e2e" --scope sessions --since 2w`
3. It gets 10 session hits ranked by BM25 relevance and notes the session ids.
4. It starts by taking the session ids, and running a duckdb query to rank them by total token usage
5. It takes the highest usage session id and drills into the messages:
   `autosearch search "Exit code" --scope messages --cwd /home/charlie/src/my-project`
6. It finds the specific error messages and pulls context with `autosearch session get $sessionId`

This is the smallest useful dogfood loop for `autoreflect` v1:

1. use `autosearch` to find the sessions that contain the lesson
2. inspect the transcripts and decide on one good repository rule
3. use `autoreflect rule create` to write that rule into the repo playbook
4. use `autoreflect lookup` later to retrieve that rule before doing similar work again

Example commands:

```bash
# find recent E2E-related sessions
autosearch search "npm run e2e" --scope sessions --since 2w

# inspect the most relevant session
autosearch session get $sessionId

# drill into the failing messages if needed
autosearch search "Exit code" --scope messages --cwd /home/charlie/src/my-project

# check whether a similar rule already exists
autoreflect lookup "e2e flaky logs"

# write the learned rule into project memory
autoreflect rule create \
  --content "Keep passing test logs short so failing E2E tests are easier to debug" \
  --category testing \
  --tag e2e \
  --tag logs \
  --tag flaky

# later, before working on similar problems, retrieve matching rules
autoreflect lookup "e2e flaky logs"
```

Example initial playbook:

```json
{
  "schema_version": 1,
  "rules": [
    {
      "id": "r-1a2b3c4d",
      "content": "Keep passing test logs short so failing E2E tests are easier to debug",
      "category": "testing",
      "tags": ["e2e", "logs", "flaky"],
      "created_at": "2026-03-21T12:34:56Z",
      "updated_at": "2026-03-21T12:34:56Z"
    }
  ]
}
```

## After the end of the day, we index the sessions

- set up a job using `autowatch` to do the following:
- `autoetl` to gather all code sessions into structured files, synced to S3, encrypted client side, runs every hour
- we ask a reflection agent to take a look and suggest improvements
- it uses `autosearch` to incrementally index new content

## Reflection agentn looks for signals
- use `duckdb` to run structured queries:
  - token usage?
  - hottest files?
  - calculate `lines_changed` / `tokens_used` to see which sessions are more efficient?

- agent then starts basic, and looks for sessions that didn't go well
  - `autosearch search "Exit code: 1" --scope sessions --since 1w`
  - Finds a session with bad exit codes
  - `autosearch session get $sessionId` to pull pretty printed transcript
  - finds flaky tests because the AI kept trying to use `playwright mcp` instead of `agent-browser`
  - runs `autoreflect rule create --content "Prefer agent-browser over playwright mcp for flaky E2E debugging" --category testing --tag e2e --tag flaky --tag browser`
  - later coding sessions use `autoreflect lookup "playwright flaky e2e"` to retrieve that rule
  - future versions should check for similar rules first and support update/merge flows

  - next, looks for interview sessions (quick succession usage of question/answers / AskUserQuestion tool) that glean intel from the user
    - looks at responses, compares to question and context they were asking in (looks at repo files, state of files at the time of questions)
    - NOTE: the agent probably needs a way to be able to pull contents of a file at a previous git hash. Maybe just git can do this.
    - spots recurring pattern: user always asks to just "keep it simple, this is a demo"
    - suggests new general rule: "Keep things simple whilst we are in demo phase"

  - next, looks for files edited multiple times in the same message chain that dont have a user message between them

## X. Automate work in the repository

After getting more comfortable and finding our groove, I decide to automate a lot more of the maintenance work of the project

```bash
# Ran in the project dir, creates `.auto/watch/project.json` with empty configuration
# Also adds the current dir to `~/.auto/watch/settings.json`
autowatch init # (--project-id $name is optional, set to folder name otherwise, fails if it conflicts with global config)
```

The empty project config looks like:

```json
{
  "id": "my-project", // this is set from the folder name
  "tasks": {},
  "triggers": {}
}
```

First, lets automate etl.

```bash
# this task would execute in the project directory via bash
# saves this configuration under `.tasks` in `./.auto/watch/project.json`
autowatch task create --id run-etl --bash "autoetl run"
# this will run in the context of the current project directory, will look for this task
#  and run it in process, blocking. useful for testing.
autowatch task run --id run-etl

# this trigger will go off once a day
autowatch trigger create --id daily --cron "0 0 * * *"
# associate the task with the trigger
autowatch trigger add-task --trigger daily --task run-etl
```

What about a claude code automation? Say we want to run a regression review agent once a day. The user creates a new claude skill called `/regression-review` and saves it in the project. This skill specifies what to do:
1. Look at commits from lsat 24 hours using `git` + `gh` tools
2. Look for patterns / code smells that break out guidance
3. Make fixes, commit to a branch called `fix/regression-$date` and open a PR.

Now to set up the automation:

```bash
# Create a task which will run in the context of a claude code session. For claude tasks, autowatch will:
# - create a new git workspace from `main` / default branch
# - start a claude code session, passing in the string as instruction.
# - these are fire and forget, autowatch just tracks when it completes to show it as done but doesn't block on it.
autowatch task create --id regression-review --claude "/regression-review on commits from last 24 hours"

# Set up the automation, reusing existing trigger.
autowatch trigger add-task --trigger daily --task regression-review
```

## Y. Automate work across the whole machine

Some tasks, like `autoetl run` actually run across all sessions on a single host, so we decide to make these global tasks / triggers, instead of repo specific.

```bash
# All of these commands store their configuration in `~/.auto/watch/project.json` instead of the repo.
autowatch task create --id run-etl --bash "autoetl run" --global

autowatch trigger create --id daily --cron "0 0 * * *" --global

autowatch trigger add-task --trigger daily --task run-etl --global
```
