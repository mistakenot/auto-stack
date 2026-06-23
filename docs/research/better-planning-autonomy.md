# Autonomous Planning for Software Factories — 2026 Update (last 6–12 months)

*Recency-biased refresh. Prioritizes Dec 2025–Jun 2026. Supersedes stale items in the prior report (notably GitHub Copilot Workspace, now discontinued). Current as of June 2026.*

---

## What changed since the last report

1. **Spec-Driven Development (SDD) is now the settled default, not an experiment.** GitHub Spec Kit crossed ~88k stars with 129 releases through April 2026 and supports 28 agent platforms; the pipeline is `/speckit.specify → /speckit.plan → /speckit.tasks → /speckit.implement`, with `/clarify` and `/analyze` as optional quality gates. The phrase converging across GitHub/AWS posts is "the spec is the prompt."

2. **OpenSpec (Fission AI, YC) is the new center of gravity for open-source SDD** — ~52k stars by June 2026, MIT-licensed, `npm i -g @fission-ai/openspec`. Its thesis: *"Generating code is now cheap. Correctness is still expensive."* It uses a strict state machine — **propose → apply → archive** (plus `onboard` for existing repos) — and is explicitly **brownfield-first**, physically isolating current state (`specs/`, the source of truth) from in-flight proposals (`changes/`, each a folder with `proposal.md`, `tasks.md`, delta specs). `openspec validate --strict` catches missing GIVEN/WHEN/THEN scenarios *before* code. Thoughtworks Radar placed it in "Assess" (April 2026). This is probably the closest off-the-shelf fit for a factory that operates on existing codebases.

3. **Google Antigravity (launched Nov 20, 2025) reframed the review problem around "Artifacts."** Instead of approving raw tool calls, agents emit verifiable deliverables — task lists, implementation plans, screenshots, browser recordings, diff summaries — and you comment on the plan *like a Google Doc* and the agent incorporates feedback **without stopping execution**. It has a **Planning Mode** (produces Implementation Plan + Task List for review) vs **Fast Mode** (skips the planning artifact for trivial edits), an **artifact review policy that can be set to "Always Proceed,"** and a Manager view that runs multiple models in parallel (e.g., a planner model, an implementer model, a test-writer model simultaneously). The async, comment-don't-block pattern is the single most directly relevant idea for cutting your gatekeeping.

4. **The practitioner frame shifted from "human-in-the-loop" to "human-on-the-loop."** Thoughtworks (April 2026) draws the line explicitly: in-the-loop = reviewing individual artifacts/decisions; on-the-loop = overseeing workflow *performance and reliability* without reviewing every change. The associated guidance (echoed in a WEF 2026 report) is to **redesign escalation around confidence thresholds and policy limits rather than process-compliance checkpoints.** This is the conceptual unlock for reducing gatekeeping without losing accountability.

5. **"Levels of autonomy" frameworks matured and the honest consensus is L3–L4.** Multiple 2026 write-ups (Swarmia, March 2026; MindStudio, April 2026) converge on a 5-level model from autocomplete (L1) to "dark factory" (L5). The repeated, sober conclusion: **most production teams in 2026 sit at Level 3–4, with Level 5 applied only narrowly** to high-frequency, unambiguous, strongly-verified changes. Tessl founder Guy Podjarny's self-driving-car analogy (L0–L5) remains the cleanest trust-calibration lens.

6. **New research formalized the spec/constitution layer and quantified autonomy growth.**
   - **Constitutional Spec-Driven Development** (arXiv 2602.02584, 31 Jan 2026): defines a *software constitution* — a versioned, machine-readable document of non-negotiable requirements with explicit CWE mappings and MUST/SHOULD/MAY enforcement levels (RFC 2119). Two findings matter for your factory: (a) **include only 3–5 task-relevant principles per request** — large constitutions blow the context window and degrade compliance; (b) **constitution/spec files are an attack surface** (prompt injection, "specification poisoning") and should get production-grade access controls and review.
   - **Anthropic, "Measuring AI agent autonomy in practice"** (18 Feb 2026): the 99.9th-percentile Claude Code turn duration **nearly doubled from <25 min to >45 min between Oct 2025 and Jan 2026**, and the rise was *smooth across model releases* — i.e., autonomy gains are coming from harness/workflow design, not just model capability. That's a direct signal that **your leverage is in the scaffolding, not in waiting for the next model.**

7. **A genuine caution surfaced in the data.** Google's 2025 DORA report (widely cited in Jan 2026 analyses) found that a 90% jump in AI adoption correlated with a ~9% rise in bug rates, a 91% increase in code-review time, and a 154% increase in PR size. Translation: dumping more autonomous output into an unchanged review process *moves* the bottleneck rather than removing it. Planning-stage rigor and on-the-loop review are the antidotes.

---

## The current best-practice planning pipeline

The field has converged on essentially one shape, with minor variations per tool:

```
Constitution / steering  →  Explore (context)  →  Specify  →  Clarify (batched)
   →  Plan (architecture)  →  Decompose (tasks)  →  Analyze (cross-artifact gate)  →  hand off
```

- **Constitution / steering files** (Spec Kit `/constitution`, Kiro `.kiro/steering/`, CLAUDE.md/AGENTS.md): persistent, project-wide rules the planner can't infer from code. 2026 restraint guidance: keep these tight — practitioners treat ~150–200 standing instructions as the rough ceiling before frontier models start dropping compliance, and Constitutional SDD found injecting only 3–5 relevant principles per request beats dumping the whole document.
- **Explore**: a dedicated read-only context-gathering pass. Claude Code auto-delegates to a **Haiku-powered Explore subagent** that searches the codebase in an isolated window and returns only summaries — reported to save ~40%+ of main-session tokens. Devin's DeepWiki/Search and Augment's Context Engine play the same role: infer requirements from existing artifacts so a human doesn't have to brief the agent.
- **Specify**: produce the spec as a persistent, version-controlled file (not a chat message). The 2026 "good spec" checklist (per Augment's six-element framing): outcomes, scope boundaries, constraints, prior decisions, task breakdown, and **verification criteria**.
- **Clarify (batched)**: ask once, early, only when uncertainty changes the artifact. Spec Kit's `/clarify` asks ≤5 mostly multiple-choice questions and writes answers into the spec; Claude Code uses the **AskUserQuestion** tool to "interview" you, then writes `SPEC.md`. Default low-uncertainty tasks to "proceed with documented assumptions."
- **Plan + Decompose**: translate spec → architecture → atomic, testable tasks. Encode acceptance criteria in EARS/Gherkin (GIVEN/WHEN/THEN) so they're machine-checkable.
- **Analyze (the gate that replaces line-by-line review)**: cross-artifact consistency check + checklist ("unit tests for the spec") + LLM-as-judge against an explicit rubric, run in a *separate* context from the producing agent.

---

## Reducing gatekeeping — concrete 2026 moves

- **Adopt the async artifact pattern (Antigravity-style).** Have the planner emit a reviewable plan artifact and let humans *comment without halting*. Set an explicit per-task review policy: "Always Proceed" for low-blast-radius work, "Approve plan" for the rest.
- **Calibrate by (uncertainty × blast radius), not by ceremony.** Auto-proceed past the spec gate for reversible, low-uncertainty features; require human sign-off for irreversible/ambiguous ones (auth, schema/data migrations, public API contracts). Implement gates behind flags so you can widen the autonomous envelope as success rates climb.
- **Move from in-the-loop to on-the-loop deliberately.** Stop reviewing every spec; instead monitor workflow-level signals: autonomous throughput, **review-time-per-PR** (if it's rising, your tasks are too big — shrink scope), task success rate, and a random audit/spot-check program even inside autonomous thresholds.
- **Make plans self-validating.** Generate tests/properties directly from acceptance criteria (Kiro does property-based testing via fast-check from EARS specs). A plan that ships its own checks needs far less human reading.
- **Show evidence, not assertions.** Current Claude Code guidance: have the agent surface the command it ran and the output, or a screenshot — reviewing evidence is faster than re-verifying, and it works for unattended runs. Pair with a **verification subagent** (a fresh model that tries to *refute* the plan), so the agent doing the work isn't the one grading it.

---

## Updated tool landscape (mid-2026)

| Tool | Best for | Planning-stage notes |
|---|---|---|
| **OpenSpec** (Fission AI) | Brownfield repos, low ceremony | propose→apply→archive; `validate --strict`; isolates state from proposals; MIT |
| **GitHub Spec Kit** | Cross-agent portability, greenfield | 28 agent platforms; `/clarify` + `/analyze` gates; no vendor lock-in |
| **AWS Kiro** | Enterprise, requirements traceability | requirements.md/design.md/tasks.md; EARS specs → property tests; Quick Plan mode skips gates |
| **Google Antigravity** | Agent-first async orchestration | Artifacts (plans, screenshots, recordings); comment-don't-block; multi-model parallel |
| **Augment Cosmos / Intent** | Org-scale coordination | coordinator proposes plan-as-spec; living bidirectional specs; Context Engine |
| **BMAD-METHOD** | Role-based agent teams | Analyst→PM→Architect→SM agents produce brief→PRD→architecture→stories |
| **Tessl** | Spec-as-source, API-hallucination control | living executable specs; Spec Registry grounds the planner |
| **Claude Code** | Harness-level planning + subagents | hard read-only Plan Mode; Explore subagent; `/effort max`; SPEC.md interview flow |

*(GitHub Copilot Workspace — cited in the prior report — was discontinued 30 May 2025. Its spec→plan→implement methodology lives on in Copilot coding agent + Spaces; use it only as a design reference.)*

---

## Recommendation for your factory (revised, recency-first)

1. **Pick your SDD harness by codebase shape, today.** Brownfield → start with **OpenSpec** (lowest ceremony, `onboard` reconstructs a starting spec set in ~30 min). Greenfield or multi-agent portability → **Spec Kit**. Don't build a planning pipeline from scratch; these encode the converged shape already.
2. **Write a tight constitution/steering file now** — it's the highest-leverage single artifact. Keep it under the ~150–200-instruction ceiling; inject only the 3–5 relevant principles per planning task. Treat it as security-critical (it's a prompt-injection surface).
3. **Make context-gathering autonomous** with an Explore/index pass (DeepWiki-style wiki or RAG over docs/ADRs/past specs) + MCP connectors to Jira/Linear so the planner infers requirements instead of being briefed.
4. **Install the `analyze` gate as your review replacement**: schema/template validator → LLM-as-judge (isolated context, rubric calibrated against ~20–50 human-graded specs) → cross-artifact consistency. Auto-pass above threshold; route the rest to a human.
5. **Switch to async artifact review + on-the-loop monitoring.** Adopt the comment-don't-block pattern; set per-task auto-proceed policies by blast radius; track review-time-per-PR and task success rate at the workflow level rather than reading every spec.
6. **Generate tests/properties from acceptance criteria** so plans validate themselves, and require a verification subagent to refute each plan before hand-off.

**Thresholds that should change your plan:**
- LLM-judge agreement with human spec-graders < ~80% on your sample → keep the human gate, fix the rubric.
- Review-time-per-PR rising → tasks are too big; tighten scope (this is the DORA failure mode in miniature).
- Rework traced to spec gaps > ~20% → clarification is firing too late or too rarely; move it earlier / raise sensitivity.
- Token spend spiking → check for unbounded critic/coordinator loops first; every coordinator needs explicit termination.

---

## Caveats (2026)

- Vendor star-counts, "40-hour feature in 8 hours" customer cases, and tool testimonials are marketing — validate on your own workloads. SDD output is non-deterministic (same spec, different plans across runs/models).
- The autonomy-levels frameworks are practitioner syntheses, not controlled studies; the "150–200 instructions" and "3–5 principles" numbers are rules of thumb, several from very recent (2026) write-ups and one peer-reviewed-pending preprint.
- Brownfield SDD is categorically harder than greenfield; reconstruct existing behavior before writing new specs.
- The core research constraint from the prior report still holds and is reinforced by Constitutional SDD: **don't let the generating model be its own verifier** — verification must come from an external critic/judge/validator.
- Several arXiv IDs here (2602.x) are Jan–Feb 2026 preprints; treat specific percentages as preliminary.
