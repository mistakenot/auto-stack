---
hash: "e8e3447e"
id: "43a05c62"
read_when: "designing autoreflect learning-loop architecture or evaluating rule-memory and skill-retrieval tradeoffs"
summary: "Research brief on recent agent self-improvement systems, memory architectures, retrieval strategies, and context-engineering patterns relevant to autoreflect design."
title: "Rules, Skills, and Playbooks for Self-Improving Coding Agents (Nov 2025 → Apr 2026)"
---

# Rules, Skills, and Playbooks for Self-Improving Coding Agents — State of the Art (Nov 2025 → Apr 2026)

Concise, technical brief. Coverage limited to the last ~6 months unless a paper is the canonical reference for terminology being actively built on. Embedded URLs are bare links rather than markdown footnotes.

---

## 1. Rule / Playbook Mining from Session History

### 1a. Foundational pattern (cited by everything else)
- **ACE — Agentic Context Engineering** (Stanford / SambaNova / Berkeley, arXiv 2510.04618, accepted ICLR 2026 Oral). Three-role loop: Generator (produces trajectories) → Reflector (extracts lessons from successes *and* failures) → Curator (applies localized "delta" bullet edits to a growing playbook). Key design choices: structured incremental updates instead of monolithic rewrites; "grow-and-refine" with semantic dedup; bullets carry metadata (helpful/harmful counters, ID). Reported gains: +10.6% on AppWorld agent tasks, +8.6% on finance, ~86.9% lower adaptation latency vs reflective-rewrite baselines. Open-source ref impl: github.com/sambanova/ACE. https://arxiv.org/abs/2510.04618
- **Dynamic Cheatsheet** (Suzgun et al., arXiv 2504.07952) — direct ancestor; persistent self-curated memory of code snippets and heuristics. Doubled Claude 3.5 Sonnet on AIME (23%→50%); GPT-4o Game-of-24 10%→99% by storing a Python brute-force routine. Critically, Full-History (FH) baseline often *underperformed* baseline — pure accumulation without curation/retrieval hurts. https://arxiv.org/abs/2504.07952
- **GEPA** (Agrawal et al., arXiv 2507.19457, ICLR 2026 Oral). Genetic-Pareto reflective prompt evolution. Reads full trajectories (errors, profiling output, reasoning logs) and proposes targeted prompt mutations. Beats GRPO by 6–19 pp using up to 35× fewer rollouts; +12 pp over MIPROv2 on AIME-2025. ACE explicitly positions itself as complementary: GEPA optimizes prompts offline; ACE evolves the playbook online. https://arxiv.org/abs/2507.19457

### 1b. Trajectory → reusable skills (the active 2026 frontier)
- **AutoRefine** (arXiv 2601.22758, 2026). Extracts *dual-form* "Experience Patterns" from execution histories: (a) specialized **subagents** with their own reasoning/memory for procedural subtasks, (b) **skill patterns** as guidelines or code snippets for static knowledge. Continuous *score / prune / merge* maintenance prevents repository degradation. ALFWorld 98.4%, ScienceWorld 70.4%, TravelPlanner 27.1%, with 20–73% step reductions. Concrete pattern schema: id, frequency, dimensions, failure_types, root_cause_cluster, severity, suggested_fix_type, example_sessions. https://arxiv.org/abs/2601.22758
- **Trace2Skill** (arXiv 2603.25158, Mar 2026). Dispatches a *parallel fleet* of sub-agents over a trajectory pool, extracts trajectory-specific lesson "patches," then **hierarchically consolidates** them via inductive reasoning into a single conflict-free SKILL.md. Beats Anthropic's official xlsx skill on SpreadsheetBench. Notable empirical finding: skills evolved by Qwen3.5-35B *transferred* to Qwen3.5-122B, +57.65 pp on WikiTableQuestions — challenges the assumption that experience must be model-specific. Beat ReasoningBank-style retrieval (top-1) using Qwen3-Embedding-8B. https://arxiv.org/abs/2603.25158
- **ReasoningBank + MaTTS** (Google Research, arXiv 2509.25140). Distills generalizable strategies from *self-judged* successes AND failures (failure-derived items are essential — success-only stores underperform). Test-time retrieval at top-1. Memory-aware test-time scaling: parallel/sequential rollouts feed contrastive signal back into memory. +34.2% relative effectiveness, –16% steps vs raw-trajectory or success-only memories. https://arxiv.org/abs/2509.25140
- **Memp** (Fang et al., arXiv 2508.06433, v4 Apr 2026). Procedural memory at two granularities: fine-grained step-by-step instructions AND higher-level script-like abstractions. Studies separate Build / Retrieval / Update strategies, with explicit *deprecation* of stale procedures. Procedural memory transfers strong→weak model with minimal overhead. https://arxiv.org/abs/2508.06433
- **Trajectory-Informed Memory Generation** (IBM Research, arXiv 2603.10600, 2026). Concrete adaptive retrieval: combines semantic similarity with metadata filtering and priority-based ranking; explicit task type / domain / execution-pattern tags. AppWorld gains compound on harder splits. https://arxiv.org/abs/2603.10600
- **SkillRL** (arXiv 2602.08234) and **SkillX** / **SkillFlow** (arXiv 2604.04804, 2604.17308) — RL-based recursive skill augmentation; SkillFlow is a benchmark explicitly for *skill lifecycle*: self-generated, revised, lifelong, transferable, trajectory-grounded, with a usage-vs-utility alignment metric.
- **EXIF** (arXiv 2506.04287) — Alice/Bob exploration loop; Webshop reward 2.0 → 52.0 with no human labels.
- **Audited Skill-Graph Self-Improvement (ASG-SI)** (arXiv 2512.23760, Dec 2025). Treats self-improvement as compilation into a directed skill graph. Promotions are *gated by verifier-backed evidence* (verifiable, decomposed rewards for tool-use correctness, outcome validity, skill reuse, composition integrity). Direct response to reward-hacking and untraceable behavioral drift in deployed self-improvement loops. https://arxiv.org/abs/2512.23760
- **Beyond Static Summarization: Proactive Memory Extraction** (arXiv 2601.04463) — agents must self-question their own analysis; reduces false-pattern false-positives.

### 1c. Prior art still being built on
- **ExpeL** (cross-task insights from successful trajectories) and **AutoGuide** (context-aware guidelines from contrastive success/failure pairs, retrieved per state). 2026 finding (Experiential Reflective Learning, arXiv 2603.24639): on Gaia2, AutoGuide is great on Search but *underperforms baseline on Execution* — generic "always-on" guidelines confuse the agent. ERL fixes this with relevance scoring + top-3 retrieval per ReAct turn (+7.8% over baseline, +5.2% over ExpeL). Direct lesson: **broad-spectrum injection of all rules degrades performance; per-state retrieval wins**.
- **AutoManual** (NeurIPS 2024 / referenced 2026) — Planner / Builder / Consolidator / Formulator pipeline producing categorized Markdown manuals; explicitly notes ExpeL's rule-score-based pruning is unreliable because the Builder gives overconfident scores; uses Consolidator merge instead.

### 1d. Lessons emerging across mining work
- Single-occurrence "patterns" are noise — practitioners enforce a **frequency ≥ 2 across distinct sessions** threshold before promoting an item to a rule (Vadim Nicolai's production Trajectory Miner, vadim.blog/trajectory-miner-research-to-practice).
- Contrastive (success vs failure) extraction outperforms success-only or raw-trajectory storage (ReasoningBank, AutoGuide, Trace2Skill).
- Model self-summary alone is insufficient. Cognition's Devin Sonnet-4.5 postmortem: when they removed external compaction in favor of the model's own notes, performance degraded — "the model didn't know what it didn't know"; sometimes used more tokens summarizing than solving. (cognition.ai/blog/devin-sonnet-4-5-lessons-and-challenges)

---

## 2. Storage, Versioning, Conflict Resolution, Decay

### 2a. Schemas in use
- **ACE bullets**: id, content, helpful_count, harmful_count; updated by deterministic merge logic, not LLM rewrite (this is what prevents context collapse).
- **AutoRefine pattern record**: dual-form (subagent vs declarative skill), with severity, suggested_fix_type, example_sessions, plus continuous score/prune/merge maintenance.
- **SKILL.md (open standard, originated by Anthropic Dec 2025, adopted by Cursor, OpenAI Codex CLI, Gemini CLI, GitHub, Antigravity)**: YAML frontmatter (name + trigger description) + Markdown body + optional `references/`, `assets/`, `scripts/` directories; `disable-model-invocation: true` for manual-only workflows; `allowed-tools` for restriction. https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview
- **Cursor Project Rules (.cursor/rules/*.mdc)**: frontmatter with `description`, `globs`, `alwaysApply`. Four types: Always, Auto-Attached (glob match), Agent-Requested (model decides via description), Manual (@-mention). Rule precedence: Team → Project → User; earlier wins on conflict; hub-imported rules are "agent-decided" only. https://docs.cursor.com/context/rules
- **Continue (.continue/rules)**: properties `globs`, `regex` (rule fires when file *content* matches), `description` (used for agent-requested activation when `alwaysApply: false`). Has `create_rule_block` tool so the agent can codify a rule from the current chat into a versioned `.md` file. https://docs.continue.dev/customize/deep-dives/rules
- **Windsurf**: split between **Memories** (auto-generated, contextual, local-only at `~/.codeium/windsurf/memories/`, *workspace-hash-keyed by directory path*) and **Rules** (`.windsurfrules`, version-controlled, predictable). Memories are explicitly NOT recommended for team-shared conventions because of the path-keyed storage and the contextual recall behavior.
- **AGENTS.md** (now stewarded by Agentic AI Foundation under the Linux Foundation; backed by OpenAI Codex, Amp, Jules, Cursor, Factory): plain Markdown, no frontmatter, supports nested files — *closest AGENTS.md to the edited file wins*; explicit chat prompts override.
- **Devin Playbooks + Knowledge Base + Session Insights**: playbooks are reusable system-prompt fragments for recurring tasks (e.g., `!triage-bug`, `!db-migration`), associated with snapshots; Devin can *analyze sessions to author or improve a playbook*, e.g. "compare 4 failed sessions to successes and update the playbook to handle FK dependencies." Knowledge base supports recurring "review entries and propose consolidated versions" jobs. https://docs.devin.ai/work-with-devin/advanced-capabilities

### 2b. Retrieval architecture choices
- **Hybrid is winning**. Mem0's ECAI 2025 / 2026 benchmark across 10 approaches on LOCOMO (arXiv 2504.19413) shows full-context approaches set the accuracy ceiling at impractical cost; vector-only loses on multi-hop; hybrid (vector + metadata + summarization) is the production sweet spot. (mem0.ai/blog/state-of-ai-agent-memory-2026)
- **Graph-based agent memory** is the active 2025–2026 research frontier (arXiv 2602.05665) — preserves relational and causal structure that flat embeddings drop.
- **Skill / rule retrieval at startup**: Anthropic Skills' "Progressive Disclosure" loads only YAML frontmatter (name + trigger description, ~80 tokens median per skill across Anthropic's 17 official skills). The body loads only when the model judges the skill relevant; bundled `references/`, `scripts/` load only when navigated. Empirical motivation: full load of the 133-skill Microsoft `agent-skills` repo would cost ~665K tokens (deepwiki.com/microsoft/agent-skills/5.4-ralph-loop). MCPJam reports MCP-style upfront tool loading degrades tool-use accuracy past 2–3 servers; progressive disclosure scales much further.
- **Cursor "Memories"** were *removed* in v2.1.x (late 2025); users were advised to migrate to Rules. Rationale signaled in forums: ambiguous semantics (when does a memory apply?) and privacy concerns. Cursor's hub treats all imported skills/plugins as "agent-decided rules" — they cannot be `alwaysApply`.

### 2c. Decay, pruning, conflict
- ACE: deterministic merging on bullets keyed by ID; semantic dedup with thresholds; bullets that consistently underperform (negative helpful_count) are pruned. *Not* model-driven rewrites.
- Memp: explicit Update phase that *deprecates* stale procedures; otherwise the repository would grow into noise.
- AutoRefine: *exponentially spaced* maintenance intervals (score / prune / merge), specifically because frequent maintenance over-fits and infrequent maintenance lets the repo degrade.
- Trace2Skill: hierarchy-guided "patch merging" with *dominance-aware consolidation* — patches compete on recurring, broadly-useful patterns; rare quirks lose. Open question: how to quantify dominance when signals are correlated/biased by domain skew.
- AutoManual finding: rule scores produced by an LLM Builder are *unreliable* — the LLM gives overconfident scores. Use a separate Consolidator agent to merge/delete instead of trusting scores. (Replicated concern in industry — see Replit below.)
- ASG-SI: skills are only *promoted* into the active graph when a verifier replays them and produces minimally-sufficient evidence bundles — explicit defense against reward hacking and silent drift.

---

## 3. Presenting Rules to Future Agents (the highest-leverage layer)

### 3a. The dominant production pattern: progressive / just-in-time disclosure
- **Anthropic Skills three-tier model** (Anthropic's "Equipping agents for the real world" engineering post, late 2025). L1 always-loaded YAML metadata; L2 SKILL.md body loaded when relevant; L3 bundled files navigated on demand. This is the API contract; the agent platform implements the loader. As of early 2026 the SKILL.md format is the de-facto open standard adopted by Cursor, Codex CLI, Gemini CLI, GitHub Copilot, Antigravity. Anthropic engineers report "hundreds of skills in production" internally; the most valuable section of any skill is the **"Gotchas"** list — common mistakes the model has actually hit, updated as new failures appear.
- **Replit "Decision-Time Guidance"** (blog.replit.com/decision-time-guidance, Feb 2026). Direct empirical critique of upfront rule injection: "Every trajectory is unique, so static prompt-based rules often fail to generalize—or worse, pollute the context as they scale." Replit injects situational instructions at *key decision moments* in the trajectory, with the execution environment itself acting as the guide rather than a global rulebook. Cited motivating papers: "Control Illusion: The Failure of Instruction Hierarchies in LLMs," "Lost in the Middle," and "RULER."
- **Cognition / Devin** (Sonnet 4.5 lessons): rebuilt context resets, parallel tool use, and Session Insights. Insights from one session inform the next via *improved prompts/playbooks* fed into new sessions, not by stuffing memories into context.
- **Anthropic "Effective Context Engineering"** (Sep 2025) and the cookbook "memory, compaction, and tool clearing" (platform.claude.com/cookbook) frame the problem as "find the *smallest set of high-signal tokens*". Three first-class primitives in production now: compaction (summarize context), tool-result clearing (drop bloated tool outputs while keeping the call), and `/memories` (durable note-taking outside the window). "Just-in-time exploration" is preferred over upfront load.
- **Anthropic "Effective harnesses for long-running agents"** describes a multi-context-window pattern: an *initializer* agent on first run prepares `claude-progress.txt` + feature requirement file → subsequent agents bootstrap from those artifacts. Pattern explicitly inspired by shift-handoff in human engineering.
- **Anthropic "Managed Agents"** virtualizes session/harness/sandbox; harness retrieves context via `getEvents()` slices over an append-only log rather than blob compaction. Notable: Anthropic publicly admits prior assumptions (e.g., context-anxiety mitigation for Sonnet 4.5) became *dead weight* on Opus 4.5 — harness rules go stale as models improve.
- **Sourcegraph Amp** retired `/compact` in favor of **`/handoff`** in 2026 — instead of in-place summarization (which corrupts long traces), generates a fresh thread with a curated prompt + reviewable artifact list. Direct rationale: cumulative summary distortion. Amp also uses AGENT.md and an Oracle-mode subagent for architecture review with isolated context.

### 3b. Token-budget management tactics in production
- Per-tool 25K-token cap on tool responses (Claude Code default). Encourage agents to make many small targeted searches over single broad ones.
- Sub-agent delegation as the dominant context-isolation mechanism: a subagent's task replaces "large blocks of messy exploration with small structured outputs" — recursive delegation handles tasks whose own context would otherwise rot. (mindstudio.ai context-rot guide; Boris Cherny/Anthropic best-practices.)
- Hooks are deterministic, CLAUDE.md is advisory. The widely-circulated rule of thumb: Claude follows CLAUDE.md ~80% of the time, dropping in long sessions and after compaction. PreToolUse / PostToolUse hooks operate *outside* the LLM's interpretive layer and are the only way to enforce hard rules (security, formatters, linters, file-size caps). 25 lifecycle events available, 4 handler types (command, http, prompt, agent) as of March 2026. (See "Your CLAUDE.md is a Suggestion. Hooks Make It Law" on Medium; Builder.io's 50 tips; smartscope.blog/en/generative-ai/claude/claude-code-hooks-guide; dev.to/boucle2026/what-claude-code-hooks-can-and-cannot-enforce — catalogues 190 known hook gaps.)
- **Skill-Activation Hook** pattern: a UserPromptSubmit hook matches keywords against a rules index and *injects only the relevant skill metadata* before Claude starts, sidestepping full skill enumeration entirely. (claudefa.st/blog/tools/hooks/hooks-guide)

### 3c. Task-conditioned retrieval (academic side)
- AutoGuide retrieves **per-state** guidelines. Confirmed in 2026 follow-ups: "irrelevant guidelines confuse decision-making" — broadcasting all extracted insights at every step (ExpeL's behavior) caused regressions on harder benchmarks.
- ReasoningBank uses task-query similarity at top-1 with strong embedding model (Qwen3-Embedding-8B in Trace2Skill comparisons). Top-1 outperforms top-3 in some settings because adding more retrieved items pulls in irrelevant strategies.
- ERL (Gaia2): contextually retrieves top-3 guidelines after each ReAct observation; injects after the observation, not as a system prompt prefix. +5.2% over ExpeL purely from retrieval gating.

### 3d. What Anthropic explicitly recommends for skill descriptions
- Description in YAML frontmatter must include both *what* and *when*: "[What it does] + [When to use it] + [Key trigger phrases]." This is the *only* signal the loader sees pre-activation, so it determines whether the skill fires at all.
- Avoid "tool-first" framing where the skill describes itself like a function spec; prefer "problem-first" framing tied to user phrases. (resources.anthropic.com/hubfs/The-Complete-Guide-to-Building-Skill-for-Claude.pdf)

---

## 4. Empirical Results, Failure Modes, Production Lessons

### 4a. Hard quantitative wins
| System | Setting | Headline metric | Source |
| --- | --- | --- | --- |
| ACE | AppWorld (DeepSeek-V3.1, ReAct) | 59.4% (matches IBM-CUGA on GPT-4.1 at 60.3%); +10.6% agent avg, +8.6% finance, –86.9% latency | arXiv 2510.04618 |
| GEPA | 6 tasks vs GRPO/MIPROv2 | +6 pp avg over GRPO with 35× fewer rollouts; +12 pp on AIME-2025 | arXiv 2507.19457 |
| Dynamic Cheatsheet | Game of 24, GPT-4o | 10% → 99% | arXiv 2504.07952 |
| ReasoningBank + MaTTS | WebArena, SWE-bench-Verified | +34.2% effectiveness, –16% steps | arXiv 2509.25140 |
| AutoRefine | ALFWorld / ScienceWorld / TravelPlanner | 98.4% / 70.4% / 27.1%; –20–73% steps | arXiv 2601.22758 |
| Trace2Skill | SpreadsheetBench | Beats Anthropic xlsx skill; transfers Qwen3.5-35B → 122B for +57.65 pp on WikiTableQuestions | arXiv 2603.25158 |
| Augment Context Engine MCP | 300 Elasticsearch PRs across Cursor/Claude Code/Codex | +70% quality with fewer turns and tool calls | augmentcode.com/changelog/context-engine-mcp-in-ga |
| SWE-Replay | SWE-Bench Verified | –17.4% cost while +3.8% pass | arXiv 2601.22129 |

### 4b. Failure modes documented in 2025–2026
- **Brevity bias** (ACE) — context optimizers compress to short generic instructions, losing tool-use detail and negative evidence.
- **Context collapse** (ACE) — iterative full-context rewriting gradually erases detail; the proximate cause of "rule rot" in playbooks updated by LLM rewriters.
- **Context rot** (Chroma research, popularized 2025–2026) — degradation observed at every input-length increment, not just near the limit; lost-in-the-middle U-shape persists. Coding agents spend 60%+ of turn 1 just searching (Cognition data); doubling task duration roughly *quadruples* failure rate. (morphllm.com/context-rot)
- **Compaction-cascade regression** — empirical, this period: OpenAI Codex CLI v0.118 lowered the compaction threshold and caused agents to *re-read* the same files after every compaction, multiplying token spend 10–20× (github.com/openai/codex/issues/16812). Documented: 89M tokens & 4 compactions in v0.116 vs 185M & 12 compactions in v0.118 for analogous tasks. OpenAI's GPT-5.1-Codex-Max system card and prompting guide explicitly position natively-trained compaction as the fix. The Sourcegraph Amp Handoff launch is a parallel reaction in production: "summary of summaries distort earlier reasoning."
- **CLAUDE.md drift** — community measurement: rules followed ~80% per turn, dropping further after compaction. Rules competing with task tokens for attention lose. Mitigation: split into hooks (deterministic) + small CLAUDE.md (advisory).
- **Skill marketplace risks** — first large-scale 2026 audits:
  - 40,285 SKILL.md skills on a major marketplace; supply heavily concentrated in software-engineering workflows; demand-supply mismatch by category; most fit prompt budgets but distribution is heavy-tailed (arXiv 2602.08004).
  - **26.1% of 31,132 audited skills contain ≥1 vulnerability** across 14 patterns (prompt injection, data exfiltration, privilege escalation); skills bundling executable scripts are 2.12× more vulnerable than instruction-only (arXiv 2601.10338, "Agent Skills in the Wild," Jan 2026).
  - **ClawHavoc campaign** (Jan–Feb 2026) infiltrated 1,200+ malicious skills into the OpenClaw marketplace (arXiv 2603.00195; arXiv 2604.02837). Implication for any rule/playbook system: *the security boundary is not the skill package* — runtime fetches and dependency installs extend the attack surface.
- **Memory provenance / poisoning risk** — AutoRefine notes that auto-extracted patterns can encode sensitive trajectory content; deploying a self-improving rule store requires audit + governance.
- **Hallucinated function signatures and stale-file mismatch** — Cognition documented that long Devin sessions reasoned over a model of the codebase that no longer matches disk; sub-agent delegation + checkpointed STATE.md files mitigate.
- **Reward / verifier hacking** — ASG-SI's central concern; promotion gating with verifier-backed evidence bundles is the proposed countermeasure in the deployed self-improvement setting.
- **Memory as path-keyed local file** — Windsurf's Memories storage at `~/.codeium/windsurf/memories/` is keyed by workspace directory hash, so two engineers cloning the same repo to different paths get *different memory stores*. Conclusion (iamraghuveer.com): use `.windsurfrules` (versioned in the repo) for team conventions; reserve Memories for individual preference.
- **Auto-rule creation pitfalls** — Cursor users pushed back on the 0.51 "Generate Memories" feature because it required code-sharing with Cursor's backend; many migrated to local MCP memory servers.

### 4c. Production-deployment patterns observed
- **Anthropic / Claude Code**: skills + hooks + sub-agents + plugins (skills bundled with hooks, MCP servers, output styles). Auto Memory + `/compact` + the new Setup hook (`claude --init`) explicitly intended for L5 ("zero-awareness-required") enforcement of context recovery.
- **Cognition / Devin**: Playbooks (reusable system-prompt + snapshot combos) + Knowledge Base + Session Insights for analyzing past sessions and proposing playbook updates. MultiDevin (1 manager + ≤10 workers) is delegated parallelism; Devin scans every PR for design-system violations as a daily audit.
- **Sourcegraph Amp**: AGENT.md + Oracle (deep architecture review) + parallel sub-agents + Handoff (replaces compaction). Smart/rush/deep mode routing per task.
- **Cursor**: rules hierarchy (Team → Project → User) + Skills (cross-portable SKILL.md) + Commands + auto-rule generation via `/Generate Cursor Rules`. Memories deprecated in 2.1.x.
- **Replit**: decision-time guidance harness; RulesSync to sync `replit.md` across projects; Agent 3/4 with self-test debugging loops; explicit avoidance of front-loaded rule prompts.
- **Augment Code**: Context Engine (semantic + structural index of the entire codebase, exposed as MCP server in Feb 2026); Memory Review with approval queue and `Source: Agent` / `Source: Correction` provenance fields; the queue is intended as an end-of-session ritual because in-context review beats out-of-context review.
- **Continue**: rules as `.continue/rules/*.md` with regex-on-content triggers and `create_rule_block` agent tool to write a rule from chat into a versioned file (closes the human-in-the-loop self-improvement loop cleanly).
- **Windsurf**: Memories (auto, contextual, local) + Rules (manual, predictable, repo-versioned).
- **Aider**: `CONVENTIONS.md` loaded read-only via `/read` to get prompt-cache benefits; configured in `.aider.conf.yml`; community repo of conventions at github.com/Aider-AI/conventions.
- **OpenAI Codex CLI**: native compaction (multi-context-window) + `.codex-plugin` manifest + bundled skills; client-side `/responses/compact` endpoint for stateless ZDR-friendly compaction.

### 4d. Open problems / live disagreements
- **Upfront vs just-in-time injection.** Replit's stance ("static rules pollute context") directly contradicts Cursor's `alwaysApply: true` and the older CLAUDE.md culture of "load all your conventions at session start." Both can win; which wins is task-dependent and trajectory-length-dependent.
- **LLM rule scoring is unreliable** (AutoManual). Counter-evidence in ACE: deterministic counters work, but only because the merge logic is non-LLM. Open question: can score-based pruning ever be made trustworthy without a separate verifier (cf. ASG-SI)?
- **Skill transfer across models.** Trace2Skill argues skills transfer up the model scale (Qwen 35B → 122B). ReasoningBank-style retrieval and most memory papers assume model-specific. Open question: when does declarative skill encoding > retrieval?
- **Memory operating system vs lightweight playbook file.** MemOS / EverMemOS / MemMachine (arXiv 2604.04853) push toward a full POMDP-grounded memory OS with distinct write/retrieve/summarize/forget operators; ACE / Skills push toward the simplest possible Markdown file. As of April 2026 the simple file is winning in production, but the "memory OS" frame dominates academic surveys (arXiv 2603.07670, Jan 2026).
- **Per-skill discovery cost vs marketplace scale.** With 40K+ skills available and a measured ~80 tokens/skill discovery cost, even progressive disclosure becomes a token problem at marketplace scale; the next layer is *task-conditioned skill catalog retrieval* (e.g., Skill-Activation hooks, MCP-mediated skill servers).
- **Whether to store success-only or success+failure.** ReasoningBank, AutoGuide, Trace2Skill all converge on "both" (failure is where the contrastive signal lives). ExpeL's success-only retrieval is now the documented weak baseline.
- **Determinism vs flexibility.** Hooks > prompts when violation cost is high; prompts > hooks when judgment is required. The widely-cited heuristic: "Does it have a clear trigger? Hooks. Always be mindful of X? CLAUDE.md."
- **Self-improvement plateaus.** o-mega and HyperAgents (Meta, 2026) raise the meta-question: agents that improve themselves still rely on a fixed meta-evaluator. Truly self-referential systems remain confined to coding-task domains where evaluation and self-modification are the same skill.

---

## Implications for an "Auto" CLI suite (auto-etl / auto-search / auto-reflect / auto-skill)

Synthesizing the evidence above into design constraints worth pressure-testing:

- **Mining (auto-reflect):** prefer ACE-style delta extraction with deterministic merges; retain both successes and failures (ReasoningBank); enforce frequency ≥ 2 across distinct sessions before promoting; require a "Gotchas" / negative-evidence field on every extracted rule (Anthropic-internal practice).
- **Storage (auto-etl): adopt the SKILL.md three-tier convention as the on-disk schema** so artifacts are portable across Claude Code / Cursor / Codex / Gemini CLI / Continue / Augment. Add explicit metadata fields beyond the open standard: `source_session_ids`, `helpful_count`, `harmful_count`, `last_validated`, `model_observed`, `severity`, `trigger_glob_or_regex`.
- **Indexing/search (auto-search):** hybrid (BM25 + vector + metadata filters), top-k small (1–3), task-conditioned. Mem0 LOCOMO benchmark and ERL on Gaia2 both show top-1/top-3 with strong filtering beats generous top-k. Avoid loading all rules upfront — model that as the antithesis (per Replit's blog).
- **Maintenance:** exponentially-spaced score / prune / merge (AutoRefine); never trust LLM-generated rule scores in isolation — pair with a separate Consolidator agent (AutoManual) or verifier-backed evidence (ASG-SI). Treat skill bodies as immutable per version; new lessons arrive as deltas.
- **Presentation:** YAML frontmatter description must encode trigger phrases AND when-to-use AND when-NOT-to-use (Anthropic guide). For autonomous runs, expose a Skill-Activation hook pattern (UserPromptSubmit → keyword match → inject only metadata of relevant skills) — this is the cheapest scalable injection mechanism observed in production.
- **Security:** pre-publish static + semantic scan (SkillScan-style), capability declarations, dependency pinning. The 26% vulnerability rate and ClawHavoc campaign make this non-optional for any system that ingests external skills.
- **Evaluation:** copy ReasoningBank's experimental harness (success rate + steps + token cost), and adopt SkillFlow's lifecycle metrics (Skill Eval, Self-Gen, Revision, Lifelong, Transfer, Traj-Grounded, Usage Eval) — gives a meaningful before/after for self-improvement claims rather than just final accuracy.
