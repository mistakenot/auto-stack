---
hash: "ef56f516"
id: "8bd2891c"
read_when: "planning auto-etl schema changes, ATIF import/export, or training/eval datasets derived from coding-agent Sessions"
summary: "Research note comparing auto-etl's normalized parquet Session/Message format with Harbor ATIF, with concrete lessons for step reconstruction, identity, subagent references, context boundaries, multimodal assets, validation, and after-the-fact training/eval construction."
title: "What auto-etl Can Learn from ATIF"
---

# What auto-etl Can Learn from ATIF

## Purpose

This note records findings from comparing `auto-etl`'s normalized Session/Message parquet format with Harbor's Agent Trajectory Interchange Format (ATIF) RFC.

The goal is not to replace `auto-etl` with ATIF. The two formats optimize for different jobs:

- `auto-etl` is an analytical fact store for many coding-agent Sessions, optimized for DuckDB, `auto search`, `auto reflect`, and cross-Project analysis.
- ATIF is a portable trajectory document, optimized for replay, visualization, debugging, SFT, RL, and eval interchange.

The useful question is: which ATIF concepts should `auto-etl` borrow so its canonical data stays useful for downstream analytics while becoming easier to replay, validate, export, and use for training/eval construction after the fact?

Primary references:

- Harbor ATIF RFC: <https://github.com/harbor-framework/harbor/blob/main/rfcs/0001-trajectory-format.md>
- `auto-etl` canonical structs: `auto-shared/model/schema.go`
- `auto-etl` transform logic: `auto-etl/internal/transform/transform.go`
- `auto-etl` parser subagent handling: `auto-etl/internal/parser/parser.go`
- `auto-etl` normalized schema reference: `auto-etl/docs/reference/normalized-schema.md`
- Existing conformance research: `docs/research/alloy-core-data-model-conformance.md`

## Current auto-etl Shape

`auto-etl` stores one Session row per parsed Session file and one Message row per normalized content block. For Claude content arrays, text blocks, tool-use blocks, tool-result blocks, and thinking blocks become separate Message rows. Tool metadata is denormalized into columns such as `tool_name`, `tool_input`, `tool_use_id`, `bash_command`, `bash_exit_code`, `tool_file_path`, `duration_ms`, `interrupted`, and `tool_use_result_json`.

That shape is strong for queries:

- "Which files did agents read most?"
- "Which bash commands failed repeatedly?"
- "Which Sessions involved a Subagent?"
- "Which commits, PRs, or hook Events line up with a Session?"

The weakness is semantic reconstruction. A single meaningful agent turn may be spread across several rows:

1. assistant text row,
2. assistant tool-use row,
3. tool-result row,
4. later assistant response row.

Consumers can reconstruct the interaction, but they need pairing and adjacency logic. ATIF makes this grouping explicit with a `StepObject`.

## Current ATIF Shape

ATIF has a root `Trajectory` object with:

- `schema_version`,
- optional run-scoped `session_id`,
- optional per-document `trajectory_id`,
- an `agent` object,
- ordered `steps`,
- optional `final_metrics`,
- optional `continued_trajectory_ref`,
- optional `subagent_trajectories`,
- flexible `extra` objects.

Each `StepObject` represents a system prompt, user message, or complete agent turn. For an agent turn, the Step can include the assistant message, reasoning content, multiple tool calls, observation results, per-step metrics, `llm_call_count`, and `is_copied_context`.

The ATIF RFC is also explicit about multimodal content, subagent trajectory references, and context-management boundaries such as compaction, pruning, and knowledge injection.

## Key Finding

The formats are complementary.

`auto-etl` should keep parquet as canonical. It should not store ATIF JSON as the primary shape. But `auto-etl` should add enough structure to make ATIF export loss-minimized and deterministic.

The best path is a layered model:

1. Keep `sessions` and `messages` as the low-level fact tables.
2. Add derived tables/fields that express ATIF-like semantic boundaries: steps, LLM calls, observations, context boundaries, subagent references, assets, and diagnostics.
3. Add an `auto etl export atif` command as an interoperability and regression target.

## Lessons to Borrow

### 1. Add a Step Layer Above Messages

ATIF's strongest modeling improvement is the Step. A Step says: this user/system/agent event is the unit consumers should replay, grade, or train from.

`auto-etl` currently has `Message.index`, but that index is a normalized row index, not a turn/step boundary. A row can be just a tool-use block with empty `content`.

Recommendation:

- Add a derived `steps` dataset, or add stable `step_id`/`llm_call_id` fields to Messages.
- Define a Step as one of:
  - user input,
  - system Event,
  - one assistant LLM response and its emitted tool calls,
  - one deterministic agent dispatch with `llm_call_count = 0`,
  - one context-management Event.
- Preserve a `message_id` to `step_id` mapping for lossless drill-down.

Candidate dataset:

| Field | Meaning |
|---|---|
| `id` | Stable ID, e.g. `{session_id}-step-{n}` |
| `session_id` | Owning Session |
| `step_index` | 1-based step order within Session |
| `source` | `system`, `user`, `agent`, or `tool` if needed for legacy rows |
| `started_at` / `ended_at` | Unix ms bounds from member Messages |
| `model` | Model used for the agent Step |
| `llm_call_count` | 0 for deterministic dispatch, 1 for one inference, >1 if aggregated |
| `message_ids_json` | Ordered Message IDs included in the Step |
| `tool_call_ids_json` | Tool-use IDs emitted by the Step |
| `observation_message_ids_json` | Tool-result Messages paired to tool calls |
| `schema_version` | Step schema version |

This does not replace Messages. It makes replay and eval construction deterministic.

### 2. Separate Run Identity from Document Identity

ATIF v1.7 split `session_id` and `trajectory_id` because subagents and continuations can share a run identity while needing distinct document identities. This is directly relevant to `auto-etl`.

Current `auto-etl` behavior:

- parent Session ID is the raw Claude `sessionId`,
- Subagent Session ID is the Claude `agentId`,
- Subagent `parent_session_id` points at the parent raw `sessionId`.

That solved duplicate Session IDs, but it means `Session.id` is doing two jobs: row identity and sometimes source/run identity.

Recommendation:

- Keep `session_id`/`id` as the canonical row identity.
- Add `run_id`: the raw run-scoped ID shared by parent, subagents, and continuation segments when applicable.
- Add `source_document_id`: stable identity of the source transcript file/document.
- Add `source_document_kind`: `parent`, `subagent`, `continuation`, `compaction`, etc.

This makes it possible to ask both:

- "show me this Session row," and
- "show me the complete logical run, including parent, Subagents, and continuations."

### 3. Add First-Class Subagent References

`auto-etl` can tell that a Session is a Subagent and who its parent is. It does not fully model which parent tool call created which Subagent Session.

ATIF models this with `subagent_trajectory_ref`: the parent observation points to a complete subagent trajectory by `trajectory_id` or `trajectory_path`.

Recommendation:

Add a `subagent_refs` dataset:

| Field | Meaning |
|---|---|
| `id` | Stable reference ID |
| `parent_session_id` | Parent Session |
| `parent_step_id` | Step that delegated |
| `parent_message_id` | Message row containing the `Agent`/subagent tool call |
| `tool_use_id` | Tool-call pairing key |
| `child_session_id` | Canonical Subagent Session row |
| `child_run_id` | Raw run identity, if distinct |
| `child_source_path` | Transcript path |
| `subagent_name` | Subagent type/name |
| `summary` | Summary returned to parent, if present |
| `status` | `completed`, `interrupted`, `missing`, `orphan`, etc. |
| `duration_ms` | Delegation duration when known |
| `extra_json` | Tool/provider-specific metadata |

This would let downstream tools distinguish "Subagent exists somewhere in the same tree" from "this exact parent action delegated to that exact Subagent."

### 4. Model Observations Separately from Messages

ATIF explicitly separates tool calls from observations. `auto-etl` stores tool results as `role = tool` Messages, which is queryable but not semantically crisp.

Recommendation:

Add a derived `observations` dataset keyed by `tool_use_id`:

| Field | Meaning |
|---|---|
| `id` | Observation row ID |
| `session_id` | Owning Session |
| `step_id` | Agent Step that issued the tool call |
| `tool_use_id` | Correlation key |
| `tool_name` | Tool/function name |
| `result_message_id` | Message containing result content |
| `content` | Full result content or pointer |
| `is_error` | Tool-result error flag |
| `duration_ms` | Tool duration |
| `interrupted` | Cancelled/stuck signal |
| `exit_code` | Bash or process exit code where applicable |
| `extra_json` | Raw/provider-specific result envelope |

Messages remain canonical. Observations become the replay/eval surface.

### 5. Promote Context Management to a Data Contract

ATIF's context-management convention is highly relevant to coding-agent history. Agents routinely compact, summarize, prune, inject external knowledge, and continue after context transitions. Without explicit context boundaries, after-the-fact training examples can accidentally include context the model did not actually see, or train on duplicated copied context.

Recommendation:

Add a `context_events` dataset or equivalent Step metadata:

| Field | Meaning |
|---|---|
| `id` | Context Event ID |
| `session_id` | Owning Session |
| `step_id` | Step where boundary occurs |
| `event_type` | `compaction`, `pruning`, `injection`, `continuation`, `checkpoint`, `reset` |
| `boundary` | `replace`, `append`, `truncate` |
| `summary_message_id` | Message containing replacement summary, if any |
| `continued_session_id` | Next Session/segment, if known |
| `source_steps_json` | Prior steps summarized/pruned |
| `is_training_boundary` | Whether SFT/eval reconstruction should start a new segment |
| `extra_json` | Provider-specific data |

Also add `is_copied_context` at the Step or Message level. ATIF's rule is important: copied context is useful for auditability but must be excluded from supervised fine-tuning examples.

### 6. Add Multimodal Asset References

ATIF's content parts support text and image references without embedding image bytes in the JSON. `auto-etl` is currently text-oriented. Coding-agent Sessions increasingly include screenshots, browser images, diagrams, and generated assets.

Recommendation:

Add an `assets` or `content_parts` dataset:

| Field | Meaning |
|---|---|
| `id` | Asset/content-part ID |
| `session_id` | Owning Session |
| `message_id` | Parent Message |
| `part_index` | Order inside Message |
| `type` | `text`, `image`, `audio`, `video`, `file` |
| `media_type` | MIME type |
| `path_or_url` | Local path, artifact URL, or object-store key |
| `sha256` | Content hash |
| `bytes` | Byte size |
| `extracted_text` | OCR/transcription/caption when available |
| `extra_json` | Provider-specific metadata |

For existing text Messages, this can be empty. For future multimodal Sessions, it prevents lossy flattening.

### 7. Standardize `extra_json` Escape Hatches

ATIF uses `extra` consistently at root, agent, Step, tool call, observation, and metrics levels. `auto-etl` has some raw JSON preservation, but it is not systematic.

Recommendation:

Add consistent extension points:

- `session_extra_json`,
- `message_extra_json`,
- `step_extra_json`,
- `tool_call_extra_json`,
- `observation_extra_json`,
- `metrics_extra_json`.

Use typed columns for fields that are core to known workflows. Use `extra_json` for fields that are source-specific, experimental, or too sparse to justify a top-level column.

### 8. Enforce Relationship Validity or Emit Diagnostics

ATIF's reference implementation validates sequential step IDs, tool-call references, subagent refs, and multimodal content references. `auto-etl` has already found the same class of risks through Alloy: orphan Subagents, missing tool-use originators, duplicate tool-use IDs, and zero-timestamp partitions.

Recommendation:

Add a diagnostic dataset, tentatively `data_errors`, and make conformance errors first-class:

| Field | Meaning |
|---|---|
| `id` | Stable diagnostic ID |
| `session_id` | Affected Session or source Session |
| `message_id` | Affected Message, if any |
| `source_path` | Raw source path |
| `source_line_index` | Raw line index |
| `code` | `orphan_subagent`, `missing_tool_use`, `duplicate_tool_use_id`, `zero_timestamp`, etc. |
| `severity` | `warning` or `error` |
| `message` | Human-readable explanation |
| `value_json` | Offending value(s) |
| `remediation` | Concrete hint |
| `schema_version` | Diagnostic schema version |

Consumers should be able to decide whether to ignore warnings, block training export on errors, or include partial Sessions in analytics.

## Suggestions for After-the-Fact Training Construction

The hardest requirement is "after the fact": `auto-etl` should support creating training and eval examples from historical Sessions that were not instrumented with training labels at capture time.

That requires reconstructing what the agent actually saw, what it did, what feedback it got, and whether the outcome was useful. ATIF points at the missing pieces.

### 1. Build a Deterministic Step Reconstructor

Training data should not be built directly from raw Message rows. It should be built from reconstructed Steps.

Add a library function:

```text
ReconstructSteps(session_id) -> []Step
```

It should:

- group assistant text/thinking/tool-use rows into one agent Step where possible,
- pair tool-use rows with tool-result rows by `tool_use_id`,
- attach observations to the issuing Step,
- preserve all original Message IDs,
- mark malformed or ambiguous regions with diagnostics,
- produce stable output across runs.

This becomes the foundation for ATIF export, SFT example generation, eval trace replay, and quality scoring.

### 2. Track Effective Context Windows

For SFT/eval, "all previous Messages in the Session" is often the wrong prompt. Compaction, continuation, and context injection mean the actual model context may differ from the full transcript.

Add context reconstruction:

```text
EffectiveContext(step_id) -> context steps visible to the model
```

Rules:

- If no boundary exists, use prior non-diagnostic Steps in the same Session tree.
- If a `replace` compaction boundary exists, use the compaction summary plus Steps after the boundary.
- Exclude `is_copied_context = true` from supervised targets.
- Include system/developer instructions only if they were actually present in the model input.
- Emit a diagnostic when context cannot be reconstructed with confidence.

Without this, SFT examples may accidentally train on information that was only available to the analyst after the Session, not to the agent at the time.

### 3. Mark Training Eligibility Explicitly

Not every Step should become an SFT example.

Add a derived field or dataset:

| Field | Meaning |
|---|---|
| `step_id` | Candidate Step |
| `eligible_for_sft` | Boolean |
| `eligible_for_rl` | Boolean |
| `eligible_for_eval_replay` | Boolean |
| `exclusion_reason` | `copied_context`, `deterministic_dispatch`, `malformed_tool_pair`, `no_assistant_output`, `contains_secret`, `human_only`, etc. |
| `confidence` | `high`, `medium`, `low` |

ATIF's `is_copied_context` and `llm_call_count = 0` are the minimum signals to borrow.

### 4. Preserve Prompt/Completion Boundaries

For SFT, a training example needs:

- prompt/context,
- completion,
- metadata,
- exclusion rules.

`auto-etl` currently stores rows, not model-call boundaries. Add a derived `llm_calls` dataset:

| Field | Meaning |
|---|---|
| `id` | Stable LLM call ID |
| `session_id` | Owning Session |
| `step_id` | Step containing the call |
| `model` | Model name |
| `prompt_message_ids_json` | Messages/Steps in prompt context |
| `completion_message_ids_json` | Assistant output rows |
| `tool_call_ids_json` | Tool calls emitted by completion |
| `stop_reason` | Provider stop reason |
| `input_tokens_total` | Non-cached + cached input tokens |
| `cached_tokens` | Cache hits |
| `output_tokens` | Completion tokens |
| `reasoning_tokens` | If available |
| `prompt_hash` | Stable hash of reconstructed prompt |
| `completion_hash` | Stable hash of target completion |
| `reconstruction_confidence` | Confidence that boundary is exact |

Historical logs may not contain the exact provider prompt. Still, a deterministic best-effort reconstruction with confidence is better than ad hoc transcript slicing.

### 5. Add Cost and Token Semantics Compatible with ATIF

ATIF defines `prompt_tokens` as all input tokens, including cached tokens, and `cached_tokens` as the subset served from cache. `auto-etl` currently has non-cached input plus cache creation/read fields. That is more provider-specific but less directly comparable.

Recommendation:

Add derived metrics:

- `prompt_tokens_total = input_tokens + cache_creation_input_tokens + cache_read_input_tokens`,
- `cached_tokens = cache_read_input_tokens`,
- `cache_creation_input_tokens` remains provider-specific,
- `completion_tokens = output_tokens`,
- optional `cost_usd`.

Do not remove existing columns. Add comparable derived fields for eval/training exports.

### 6. Preserve Token IDs and Logprobs by Reference, Not Inline

ATIF supports `prompt_token_ids`, `completion_token_ids`, and `logprobs`. These are useful for RL and tokenizer-drift avoidance, but they are large and often absent in coding-agent logs.

Recommendation:

Use sidecar references rather than parquet columns full of huge arrays:

| Field | Meaning |
|---|---|
| `prompt_token_ids_ref` | Path/object key to token IDs |
| `completion_token_ids_ref` | Path/object key |
| `logprobs_ref` | Path/object key |
| `tokenizer` | Tokenizer/model revision |
| `tokenization_source` | `provider`, `reconstructed`, `unknown` |

For old Sessions, these fields will be empty. For future instrumentation, they make training export much stronger.

### 7. Derive Outcome Labels from Adjacent Data

After-the-fact eval construction needs outcomes. `auto-etl` is unusually well-positioned because it already ingests git history, GitHub PR data, hook Events, and Session Messages.

Add a derived `session_outcomes` dataset:

| Field | Meaning |
|---|---|
| `session_id` | Session being evaluated |
| `task_id` | Linked planning/beads/task ID if known |
| `commit_ids_json` | Commits attributed to the Session |
| `pr_ids_json` | PRs linked to those commits |
| `tests_run_json` | Test/build commands and results |
| `final_status` | `success`, `partial`, `failed`, `abandoned`, `unknown` |
| `label_source` | `merged_pr`, `tests_passed`, `user_feedback`, `manual`, `heuristic` |
| `confidence` | Confidence score |
| `evidence_json` | Evidence used to label |

This turns raw traces into eval records. A historical Session can become a training/eval candidate only when its label source and confidence are explicit.

### 8. Capture Verifier Events as First-Class Observations

For coding agents, tests and builds are the most reliable feedback. They should not be buried as arbitrary Bash output.

Recommendation:

Classify verifier observations:

- `go test`,
- `go build`,
- `pytest`,
- `npm test`,
- `ruff`,
- `go vet`,
- custom harness commands,
- CI check results from GitHub.

Add fields:

| Field | Meaning |
|---|---|
| `verifier_kind` | `unit_test`, `build`, `lint`, `typecheck`, `e2e`, `ci`, etc. |
| `verifier_command` | Command or CI check name |
| `verifier_status` | `passed`, `failed`, `skipped`, `timed_out` |
| `failure_signature` | Normalized error/failure hash |
| `target_paths_json` | Files/packages likely covered |

This supports after-the-fact reward construction: a Step followed by improved verifier results is valuable feedback; repeated same-failure verifier results indicate thrash.

### 9. Add Counterfactual/Eval Replay Support

Eval construction often needs to replay a historical situation with a different agent. For that, each candidate should expose:

- initial task prompt,
- repo state,
- relevant files/context,
- allowed tools,
- expected outcome/verifier,
- final reference patch or behavior.

`auto-etl` already has enough adjacent data to derive much of this, but not as a first-class object.

Add a derived `eval_cases` dataset:

| Field | Meaning |
|---|---|
| `id` | Stable eval case ID |
| `source_session_id` | Session mined from history |
| `initial_prompt` | First clean user intent |
| `project_id` / `repo_id` | Project identity |
| `base_commit` | Git commit at Session start, if known |
| `target_commit` | Commit(s) produced by Session |
| `changed_files_json` | Files touched |
| `verifier_command_json` | Commands that should pass |
| `success_criteria_json` | Derived or human-authored checks |
| `difficulty_features_json` | Fan-out, test coverage, prior churn, etc. |
| `label_confidence` | Confidence in the case |

This is the bridge from passive history to active benchmark construction.

### 10. Store Redaction and Privacy Decisions

Training construction needs a filtering story.

Add redaction metadata:

| Field | Meaning |
|---|---|
| `contains_secret` | Detected secret-like content |
| `contains_private_path` | Host/user-specific path information |
| `contains_third_party_code` | Large copied file/tool output |
| `license_risk` | Known or suspected license issue |
| `redaction_status` | `raw`, `redacted`, `excluded` |
| `redaction_reason` | Explanation |

This should apply at Message, Step, Asset, and exported-example levels.

## Suggested Phasing

### Phase 1: Make Existing Data Safer to Reconstruct

1. Update `auto-etl/docs/reference/normalized-schema.md` so it matches `auto-shared/model/schema.go`.
2. Add or finish `data_errors` diagnostics for orphan Subagents, missing tool-use originators, duplicate tool-use IDs, and zero-timestamp Messages.
3. Add `run_id` and `source_document_id` to Session rows.
4. Add a deterministic step reconstructor in code, even before adding a persisted `steps` table.

### Phase 2: Add Derived Replay Tables

1. Add `steps`.
2. Add `observations`.
3. Add `subagent_refs`.
4. Add `context_events`.
5. Add `auto etl export atif --session <id>` and golden fixtures.

### Phase 3: Add Training/Eval Construction

1. Add `llm_calls` with prompt/completion boundary reconstruction.
2. Add training eligibility/exclusion metadata.
3. Add verifier classification.
4. Add `session_outcomes`.
5. Add `eval_cases`.

### Phase 4: Add Future Capture Hooks

1. Capture exact tool definitions active during a Session.
2. Capture exact prompt token IDs/logprobs by sidecar reference when providers expose them.
3. Capture multimodal assets into an `assets` dataset.
4. Capture explicit context-management Events at runtime instead of only inferring them after the fact.

## What Not to Borrow

Do not store canonical Session history as one giant JSON trajectory. That would weaken the most valuable property of `auto-etl`: cheap cross-Session analytics over columnar facts.

Do not force every sparse ATIF field into top-level parquet columns. Token IDs, logprobs, costs, and multimodal assets should be optional sidecar or derived datasets unless they become common enough to justify hot columns.

Do not treat ATIF's `session_id` as equivalent to `auto-etl.Session.ID`. ATIF's latest semantics explicitly make `session_id` run-scoped, not document-scoped. `auto-etl` should make this distinction explicit rather than blur it.

## Open Questions

1. Should `steps` be persisted as parquet, or generated on demand from Messages?
2. What confidence threshold is acceptable for SFT export from reconstructed historical Sessions?
3. How should continuation Sessions be detected across Claude, Codex, and future agents?
4. Should exact prompt reconstruction be "best effort with confidence," or should SFT export require future instrumentation that records model input boundaries precisely?
5. How much copied source/tool output should be excluded from training examples by default?
6. Should `eval_cases` be generated only from Sessions linked to commits/PRs, or can high-confidence verifier-only Sessions qualify?

## Bottom Line

ATIF's main lesson is not "use JSON." It is "make the semantic interaction boundary explicit."

`auto-etl` already has the better storage substrate for Project-scale analysis. Borrowing ATIF's Step, observation, context, subagent-reference, multimodal, and training eligibility concepts would make that substrate much more useful for replay, eval, SFT/RL export, and long-term trajectory compatibility.

