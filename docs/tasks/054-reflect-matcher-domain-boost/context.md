# Context: Task 054 — reflect-matcher-domain-boost

Codebase grounding for porting the retrieval-eval `idf-tag` variant (non-excluding
IDF-weighted domain boost) into the Go matcher. See `plan.html`.

## Key Files

### The change site
- `auto-reflect/internal/rules/match.go:65` — `MatchRules(rules, intent, domainFilter, includeDrafts)`. Hard gate at `:76-78` (`if len(filter) > 0 && !domainsIntersect(...) { continue }`) — **this is what gets removed**. Scorer `scoreRule` `:151-171` (use_when substring +3, domain tag +1). Normalization `:88,92` (`maxRaw = 4*len(keywords)`, `score = raw/maxRaw`). Hard-rule injection `:98-120` (injection set = filter else keywords). Lifecycle filter `surfaceableLifecycle` `:138-147`.
- `auto-reflect/internal/rules/model.go:37` — `Rule` struct. Confirmed fields: `Domain []string` `:39`, `UseWhen` `:40`, `RuleType` `:43`, `Lifecycle` `:44`. Constants `:23` `RuleTypeHard="hard"`; `:29-32` `LifecycleDraft/Confirmed/Stale/Enforced`.

### Callers of `MatchRules` (two production sites)
- `auto-reflect/internal/loop/service.go:67` — `Service.Retrieve(...)`, the live `auto reflect retrieve` path. `--domain` flows verbatim from `internal/cli/retrieve.go:66` (`StringSliceVar(&domain,"domain",...)`) → `:35` `svc.Retrieve(args[0], domain, limit, !noDrafts)`. **`--limit` is applied AFTER score-DESC sort** (`service.go:68-70`), so score *ordering* (not scale) decides the top-K cut; default `--limit 0` = all. `MatchScore` is persisted into the `retrieval` event (`service.go:84`, `events/model.go:115` `json:"match_score"`) — scale change is visible in the append-only log but **not** in CLI output (`retrieve.go:46-58` omits `match_score`).
- `auto-reflect/internal/consolidate/consolidate.go:250` — `DetectDuplicate(playbook, useWhen, domain)` calls `MatchRules(playbook, useWhen, domain, false)`. **THE LOAD-BEARING DEPENDENCY:** `:255-258` gates on `top.MatchScore < DedupeScoreThreshold` where `DedupeScoreThreshold = 0.5` (`:69`), explicitly calibrated to the [0,1] normalized scale (`:70-73`: "a use_when that fully overlaps … scores 0.75, so 0.5 catches strong overlap"). It passes the candidate rule's **own domain** as the filter — so an additive boost would *always* fire here and inflate scores, falsely tripping the dedupe gate. **Must be handled** (see Solution).

### Conformance harness (must stay green)
- `auto-reflect/experiments/retrieval-eval/src/retrieval_eval/baseline.py:109` — faithful port of v1 `match.go`; hard-excludes at `:122-124`, normalizes at `:126,133`. **FROZEN as the v1 system of record (decision D-1) — NOT rewritten.** `hard-gate == baseline` stays a self-check.
- `src/retrieval_eval/variants.py` — the **version registry + reference impl**. IDF table `:80` `tag_idf = {t: log(n/df) ...}`; boost `:153-158` `boost_idf_tag = Σ tag_idf[d] for d in rule.domain ∩ filt`; applied additively `:194-195` `if boost and filt: s += boost(...)`; `idf-tag` variant = lexical scorer + this boost, gate off (`:243-246`). Scores kept **raw** there (order-only). **The shipped matcher = `variants["idf-tag"]`; add a `SHIPPED` pointer.**
- `tests/test_variant_conformance.py:30-40` — pins `VARIANTS["hard-gate"] == baseline.match_rules` (keep, = v1 self-check). `variants.BASELINE = "hard-gate"` (`variants.py:257`).
- `conformance/` Go-vs-Python parity (`test_baseline_conformance.py`, `harness.py`) — **repoint to pin `variants[SHIPPED="idf-tag"]` vs the Go CLI** (instead of `baseline`).
- `conformance/fixtures.py:49-50` — synthetic queries (`"go",["testing"]`; `"go",["nonexistent-domain"]`) encode the OLD exclusion; **expectations re-derived against the shipped variant**.
- `conformance/harness.py:4-8` — builds a **hermetic throwaway store** because `auto reflect retrieve` appends a retrieval event (not read-only). New Go tests / eval runs must never touch the live `.auto/reflect` store.

### Existing Go tests to update (`auto-reflect/internal/rules/match_test.go`)
- `TestMatchDomainFilterIsAnyOfNotAllOf:53` — asserts a non-intersecting filter **excludes** the rule (`:64-67`). Under the boost model the rule stays (off-domain, no boost). **Assertion must change.**
- `TestMatchScoringAndOrdering:82` — hard-codes the normalized `0.75` (`:96`); verify still holds (lexical path unchanged when no filter).
- `TestMatchStale/EnforcedRuleNeverSurfaces:119,137` — pass a filter but the cause is lifecycle; verify they don't rely on the filter excluding a scored off-domain rule.
- Keep/verify: `TestMatchHardInjectionUsesDomainFilterWhenProvided:70`, `TestMatchDraftHardRuleRespectsIncludeDrafts:170`.

### Docs that drift
- `auto-reflect/docs/self-improving-playbook-retrieval.md:80-98` — Phase 1 still says retrieval "filters by `domain` tags first, then does BM25/keyword match." `:96` hard-injection invariant ("Hard rules that match on domain are *always* returned"). **Update the filter→boost description; preserve the hard-injection invariant.**
- `auto-reflect/docs/requirements.md:399-411` (V1, superseded) — ranking intent ("higher keyword overlap ranks higher", "content matches matter more than tag matches", "ties broken by id ascending") — the new scorer must still honor this; `match_score` documented as a response field (but retrieve.go no longer emits it).

## Patterns
- **Scale faithfulness:** variants.py ranks by **raw lexical + raw boost**. If Go normalizes lexical to [0,1] but adds raw IDF boost (~0.2–8), the boost would dominate keyword relevance and diverge from the validated variant. Fix: combine `raw(lexical)+raw(boost)` then divide by the existing `maxRaw` (a per-query positive constant) — preserves idf-tag ordering exactly, MatchScore may exceed 1.0.
- **Hard injection stays correct for free:** with boost on, an in-domain hard rule already scores `>0` via the boost and surfaces through the scored path; the injection `setdefault(…,0)` only adds the remainder — identical to variants.py.
- CLI/flag conventions (root `CLAUDE.md:20-43`): "remove deprecated flags decisively, don't carry confusing dual-mode aliases"; "default behavior useful — return all when no filter"; preserve `normalizeDomainFilter` (trim/lowercase/dedupe). A non-excluding boost is *more* aligned with "useful default" than the gate.
- Ubiquitous language (`docs/concepts/UBIQUITOUS_LANGUAGE.md:66-85`): canonical **Rule**, **Playbook**, **Event**. `domain`/`match_score`/`boost` are matcher-internal (no glossary term) — keep existing spellings (`Match`, `MatchScore`, `match_score`).

## Related Tasks / Evidence
- The retrieval-eval experiment (`auto-reflect/experiments/retrieval-eval/`) is the entire basis. `idf-tag`: ties hard-gate on good guesses (Δ nDCG@10 +0.015, n.s.), fixes wrong-guess collapse 0.000→0.230 (sig.). Validated by the Phase-5 second-judge gate (κ 0.62–0.82, no self-preference, ordering Spearman 1.0 under consensus). `bm25+idf-tag` scored best but is deferred (Q-1).

### Git history (CB3)
The matcher area has a shallow history — three squashed feature PRs:
- **`48ef1e1` feat(019): playbook-retrieval-loop** — birth of `MatchRules`: the 3/1 weights, `4*len` normalization, hard-rule injection, AND the hard domain-exclusion gate (introduced as part of "ANY-of domain match"). **Key finding: the gate was never deliberated against a boost** — no commit weighs the trade-off; it's original-design inertia. The boost direction is new (scoped only in 054).
- **`4009d3c` feat(reflect): Observe→Consolidate pipeline** — added `includeDrafts` + `surfaceableLifecycle`; introduced the `consolidate/` package and `DedupeScoreThreshold = 0.5`. Commit body documents the calibration: *"DedupeScoreThreshold=0.5 catches strong use_when overlap (identical use_when scores 0.75) without flagging merely domain-adjacent rules"* — confirms the [0,1] dependency D-2 protects.
- **`8a3cf41` feat(049): reflect-audit-lineage-lint** — added `LifecycleEnforced` to the never-surface set; split consolidate (lineage). No scorer change.

Related tasks: **019** (origin of the matcher/scorer), **049** (lifecycle enforced + consolidate lineage), **052-reflect-tool-hardening** (adjacent — hardens `cli/retrieve.go`/`loop/service.go` output shapes, same call sites, not the scorer).

### Path verification (CB3)
All Solution-tab paths confirmed present; nothing moved. **`auto-reflect/internal/consolidate/consolidate_test.go` already exists** (so it's an `edit`, not an `add`). The conformance basis is under `auto-reflect/experiments/retrieval-eval/`.
