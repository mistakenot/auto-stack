# Context: Task 049 — reflect-audit-lineage-lint

Codebase grounding for the schema/model changes in [plan.html](plan.html): rule lineage,
task-keyed evidence, full audit-trail provenance on observations, and the `enforced`
lint-graduation lifecycle. All paths relative to `auto-reflect/` unless noted.

## Key Files

### Event model (canonical, append-only)
- `internal/events/model.go:53-64` — `Event` envelope (id, type, schema_version, seq, ts, host, **git{hash,remote}**, session_id, agent, payload). Per-evidence file/commit provenance is additive to payloads, not the envelope.
- `internal/events/model.go:69-78` — `RuleCreatedPayload` (rule_id, domain, use_when, content, causal_note, rule_type, lifecycle, observation_ids). **Add** `predecessor_ids`, `successor_ids`, `lint_ref`.
- `internal/events/model.go:80-95` — `FieldDelta{Field, Old any, New any}` + `RuleEditedPayload`. Deltas carry `any`, so a structured `lint_ref` object rides through edits cleanly.
- `internal/events/model.go:147-153` — `ObservationEvidence{SessionID, MessageID, Quote}`. **Add** optional `file`, `line_range` (or start/end), `commit`.
- `internal/events/model.go:159-168` — `ObservationPayload{ObservationID, Kind, Subject, Evidence, Context, SuggestedGeneralization, Domain, Severity}`. **Add** optional observation-level `task_id`.
- `internal/events/model.go:174-178` — `ConsolidationPayload{RuleID, ObservationIDs, Op}`. **Add** `split` to the op vocabulary.
- `internal/events/model.go:220-261` — `Validate()` (envelope only; payload rules live in owning packages). `IsRuleEvent()` at :213 — only rule_created/rule_edited dirty the projection; leave unchanged.

### Rules package
- `internal/rules/model.go:36-51` — `Rule` struct + lifecycle consts (`draft|confirmed|stale`). **Add** `LifecycleEnforced = "enforced"`, `PredecessorIDs`, `SuccessorIDs`, `LintRef`.
- `internal/rules/projection.go` — `Fold()` applies rule_created/rule_edited into the `Playbook`; `applyDelta()` switches on field name. **Add** delta cases for the new fields; set lineage/lint_ref on create.
- `internal/rules/validate.go` — `validLifecycles` map + `ValidateRule()` enum message. **Add** `enforced` to the valid set and message.
- `internal/rules/match.go` — `surfaceableLifecycle()` gates retrieval: `stale` excluded, `draft`/`confirmed` surface. **Per Q-1: `enforced` must NOT surface in `retrieve`** (treated like stale for matching), but is listable via `rule list --lifecycle enforced`.

### Consolidate package
- `internal/consolidate/consolidate.go:21-26` — op consts (`create-draft|attach-evidence|merge|deprecate`). **Add** `OpSplit = "split"`.
- `internal/consolidate/consolidate.go:28-43` — `EvidenceMinSessions = 2`, `DedupeScoreThreshold = 0.5`. **Add** `ConfirmMinTasks = 3` for the task-count promote path.
- `Coverage` / `EvidenceGate()` (:113, :148) — resolves observation ids → distinct evidence **sessions**. **Add** a distinct-**task** count over provenance observations' `task_id`.
- `DetectDuplicate()` (:172) / `DetectConflicts()` (:198) — split's new children must pass the same dedupe gate as create-draft.

### Observations package
- `internal/observations/model.go:70-80` — `Input` (parallel evidence slices: Sessions/Quotes/Messages paired by index). **Add** `TaskID` (scalar) + per-evidence `EvidenceFiles`/`EvidenceCommits`/`EvidenceLineRanges` slices.
- `internal/observations/model.go:141-164` — `validateEvidence()` enforces quote/message counts ≤ session count. **Extend** the same positional-pairing rule to file/commit/line-range slices.
- `internal/observations/model.go:188-216` — `Payload()` assembles `ObservationEvidence`. **Extend** to pair the new fields and set observation-level `task_id`.

### CLI package (cobra; JSON default, `--text` opt-in)
- `internal/cli/rule.go:355-412` — `newRulePromoteCmd()`: gate at :398 uses `consolidate.Coverage` distinct sessions; remediation hints on failure. Mirror for the task path + a new `newRuleGraduateCmd()`.
- `internal/cli/rule.go:246-315` — `newRuleListCmd()` lifecycle filter (:262). **Add** `enforced` as a valid `--lifecycle` value.
- `internal/cli/observation.go:32-97` — `newObservationAddCmd()` flag wiring. **Add** `--task-id`, `--evidence-file`, `--evidence-commit`, `--evidence-line-range` (StringArray, positional pairing).
- `internal/cli/consolidate.go` — delta processor; `deprecate()`/`merge()` methods are the pattern for a new `split()` method.

## Patterns
- **Additive schema evolution** — new fields are `omitempty`; old events fold identically; `SchemaVersion` stays `1`. Only backward-incompatible changes bump it (`events/model.go:10-14`).
- **Event-sourced** — events are canonical; `playbook.json` is a disposable fold cache. Every mutation = append event(s) then refold. Sharded by host+day (multi-worktree safety).
- **Structured `ValidationError{code, path, field, message, value}`** reused via `auto-shared/config` (`CLAUDE.md`, `auto-package-patterns.md:359-376`).
- **Two-stage discover→promote** — heuristic discovery never writes durable rules; promotion is an explicit gated write (`docs/trauma-candidate-promotion-pattern.md`).
- **Rule verbs vs consolidate ops** — `promote`/`retire` are dedicated `rule` subcommands (lifecycle transitions); `merge`/`deprecate` are consolidate ops (rule-set restructuring). → **`graduate` mirrors promote/retire (rule verb); `split` mirrors merge (consolidate op).**
- **Output contract** — stdout = parseable JSON only; diagnostics + remediation hints to stderr; every hard error carries a "run X" hint (`CLAUDE.md`).
- **Ubiquitous language** — `Rule`, `Observation`, `Lifecycle` are canonical (avoid status/state/phase for lifecycle). `enforced`/`lineage`/`lint` are not on any _Avoid_ list (`docs/concepts/UBIQUITOUS_LANGUAGE.md`).
- **Prior-art for file provenance** — `docs/v1-feedback-rule-memory-synthesis.md:24-48` already designed file-span annotations (`file`, `start_line`, `end_line`, `content_snippet`, `snippet_sha256`, `head_blob_sha`, `observed_blob_sha`). Align observation evidence field names with this shape; keep the heavy blob-SHA capture out of scope (record-best-effort path + line range + commit).

## Test surface (table-driven; helper builders `validEvent()`, `obEvent()`)
- `internal/events/model_test.go`, `internal/rules/projection_test.go`, `internal/rules/match_test.go`,
  `internal/consolidate/consolidate_test.go`, `internal/observations/model_test.go`,
  `internal/cli/*_integration_test.go`, `cmd/autoreflect/e2e_lifecycle_test.go` (add graduate/split flows).

## Related Tasks / Docs
- `docs/self-improving-playbook-retrieval.md` — V2 lifecycle + provenance design; anticipated a future `parent_rule_id` (this task delivers full lineage instead).
- `docs/v1-feedback-rule-memory-synthesis.md` — file-span provenance prior art (see Patterns).
- `docs/trauma-candidate-promotion-pattern.md` — gated promotion philosophy the graduate/promote gates inherit.
- The `/playbook-*` skills (`.claude/skills/playbook-{observe,refine,search}`) are the YAML-file analogue this engine is meant to host; their `successor_ids`/`predecessor_ids`/`files`/`task_id` schema is the parity target.

### Git history & prior-art (CB3)
- **`SchemaVersion` has never been bumped** (stuck at `1` since launch). Every prior schema change was additive with `omitempty`: `RuleCreatedPayload.ObservationIDs`, `ObservationPayload.{Context,SuggestedGeneralization,Domain}`, `ObservationEvidence.{MessageID,Quote}`. → confirms the no-bump plan.
- **Commit `4009d3c`** (`feat(reflect): Phase 1 — Observe → Consolidate`) introduced `TypeConsolidation`/`ConsolidationPayload`, the `observation` event, and made `draft|confirmed|stale` real retrieval gates. This is the most relevant precedent for adding ops + lifecycle handling.
- **Commit `025ae02`** is the template for wiring a new consolidate op + a new `rule` subcommand (op const → pure gate fn in package → `cli/consolidate.go` does I/O + event writes → single post-batch refold via `projection.applyDelta()`; rule verb mirrors `promote`/`retire` with `refoldAndGet()` + `writeRuleResult()` helpers).
- **Commit `11e43a4`** (task 023, miner-queue) added a whole new event type (`session_mined`) with **no version bump** and used a `*int` nullable for graceful degradation — direct precedent for additive growth.
- **`internal/rules/testdata/playbook.golden.json`** is a load-bearing **schema-stability guard** (task 019 feedback). The backward-compat AC-7 test should extend/lean on this golden fixture; updating it must be deliberate.
- **Task 019 lesson:** run `make check` after *every* phase (golangci-lint debt accumulates otherwise), not just at the end.
- No related task under `docs/tasks/` other than the reflect-family ones above; lifecycle/provenance work all lives in `auto-reflect/docs/`.
