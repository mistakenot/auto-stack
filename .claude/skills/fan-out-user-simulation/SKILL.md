---
name: fan-out-user-simulation
description: "Fans out a diverse cohort of simulated user personas 'using' the product (with mocked product responses) in parallel sub-agents, then synthesises their traces into ranked feature recommendations. Use when 'simulate users', 'user simulation', 'synthetic user research', 'what would users want', or deciding what to build next. Not for persona-free feature brainstorming (use generate-10-ideas)."
---

# Fan-Out User Simulation

Discover what to build next by watching a diverse cohort of fictional users try to get real jobs done with the product. This is Wizard-of-Oz prototyping turned inward: each simulated user interacts with the product, and the simulation *invents plausible product responses* on the fly. The product responses are fake — the value is in the behavioral trace: where users succeed, where they struggle, where they abandon, and what they reach for that doesn't exist yet.

Why this beats a single brainstorm: simulants run in isolation, so convergence (3+ independently inventing the same feature) is a demand signal. Friction and abandonment are churn signals. And a diverse cohort escapes the builder's box — imagining features for a market, not just for the maintainer.

## Roles: one orchestrator, many leaf simulants

**The orchestrator** (you) runs the full process below: brief, axis, cohort, fan-out, verify, synthesise.

**Simulants are leaf sub-agents.** Each receives a complete self-contained prompt (brief, persona card, trace format, report format). A simulant never reads this skill or spawns sub-agents — it plays its persona and returns its report. This keeps fan-out one level deep. If you are running this skill *as* a sub-agent, you are the orchestrator for that run — spawn simulants directly; do not add another layer.

## Process

### 1. Build the product brief

Scan the project to understand what it is today: README, docs, CLI help text, routes/screens, public API surface. Determine:

- What the product does and who it appears to be for
- The **product type** (CLI tool, web app, library/API, agent skill, service, ...) — this decides the trace format later
- Current capabilities, honestly stated — no aspirations

If the user gave a focus hint (e.g. "focus on the onboarding flow"), scope the brief to it.

Write the brief in ≤300 words. Every simulant receives it verbatim, so keep it factual and neutral: do not include roadmap ideas, known complaints, or feature wish-lists — anything in the brief anchors all seven imaginations and destroys the independence that makes convergence meaningful.

### 2. Choose the primary diversity axis

A cohort can be diverse along many axes, and *which one you pick decides what you learn*. Spreading users by expertise surfaces onboarding and power-user gaps; spreading them by industry surfaces vertical-specific needs; spreading them by role surfaces who the product is secretly excluding. There is no neutral default — silently spreading by skill level (the easy reflex) quietly answers a question the user may not have asked. So make this choice explicit and let the user own it.

Decide the axis in this order:

1. **Infer from intent.** If the request or focus hint names or implies an axis, use it. "What would enterprise vs solo users want" → organization scale. "How do people in different industries use this" → domain. "Where do new users get stuck" → expertise/adoption stage.
2. **If intent doesn't make the axis clear, ask** — use the AskUserQuestion tool. Don't guess. Offer a few candidate axes *tailored to this product*, each with a one-line gloss of what it would reveal. A useful menu to draw from (pick the 3–4 most relevant, and let the user supply their own):
   - **Job-to-be-done / use case** — the different goals people arrive with
   - **Expertise level** — newcomer → casual → power user
   - **Role / function** — e.g. engineer, PM, designer, ops, exec
   - **Industry / vertical** — the domain they work in
   - **Organization scale** — solo / small team / enterprise
   - **Adoption lifecycle** — evaluator / new adopter / veteran / churning
   - **Context of use** — device, accessibility needs, connectivity, environment

   Allow multi-select — combining two axes (e.g. industry × expertise) often yields the richest spread. Treat the answer as the cohort's *primary* spread.
3. **If you cannot ask** (the user requested a fully autonomous run, or no user is reachable), do **not** fall back to a single hidden axis. Deliberately blend two or three axes for maximum spread, and state at the top of the cohort table and report exactly which axes you chose and why — so the assumption is visible and the user can redirect with one comment instead of discovering it buried in the results.

### 3. Design the cohort

Default to **7 simulants** (use the user's number if they gave one). The primary axis from step 2 defines the main spread: each simulant occupies a distinct position along it. Vague personas produce vague ideas — LLM personas collapse toward the same polite, median behavior unless each one is pinned down with concrete specifics. Build each simulant a persona card:

- **Name & context** — specific, not generic: "Priya, solo founder of a 3-person legal-tech startup, demo to investors on Friday" beats "a startup founder"
- **Axis position** — where this persona sits on the primary diversity axis (the value that makes them distinct from the others)
- **Job to be done, with forces** — the job, plus: *push* (the struggling moment driving them here), *pull* (what attracted them), *anxiety* (what they fear about adopting), *habit* (what they currently do instead)
- **Temperament** — one of: impatient, skeptical, perfectionist, shortcut-seeker, explorer, by-the-book, tinkerer
- **Perturbation card** — one random situational constraint that injects entropy, e.g.: "on a train with flaky wifi", "boss is watching over your shoulder", "you have 90 seconds before a meeting", "migrating from a competitor and grumpy about it", "screen reader user", "it is 2am and production is down", "you are teaching this to a junior tomorrow"
- **Session goal** — the concrete thing they sit down to accomplish

Layer these **secondary** dimensions orthogonally on top, so personas vary on more than just the primary axis (unless one of them *is* the primary axis, in which case it's already covered):

- **Expertise** — with the product's domain and with tools in general
- **Time horizon** — spread across `today` (product as-is), `+3 months` (an invested regular user), and `+12 months` (the product won; what does their daily use look like?)
- **Stance coverage** — across the cohort, include at least one **lead user** stretching the product past its design intent, one **newcomer** with zero context, one **skeptic/evaluator** comparing against an alternative or deciding whether to leave, and one **adjacent-domain visitor** from a neighboring field the product wasn't built for. These are textures distributed across the primary-axis positions, not the spread itself.

### 4. Checkpoint: present the cohort

Show the user the **primary diversity axis** you used (so they can confirm it's the lens they care about) and a compact table — name, axis position, job to be done, horizon, temperament, perturbation — and ask them to approve, swap, or tweak personas before fanning out. This is the cheap moment to catch a misread of the product, a wrong axis, or a cohort skewed away from what the user cares about; after this point, each correction costs a full re-run.

If running non-interactively (no user available to respond, or the user asked for a fully autonomous run), skip the wait: include the cohort table in the final report and proceed.

### 5. Fan out the simulations

Spawn **one sub-agent per persona, all in parallel**. Isolation is the point: no simulant may see another's persona or trace. Each sub-agent receives a complete, self-contained prompt — the product brief, its own persona card only, the trace format for this product type, and the report format below. That prompt is everything the simulant needs; it does not read this skill or spawn sub-agents of its own (see Roles above).

Run every simulant to completion and collect all reports before doing anything else — do not launch them as fire-and-forget background work and end the session, or the synthesis (the entire payoff) never happens. If a simulant fails or returns nothing, note it in the report and synthesise from the rest.

Instruct each simulant along these lines:

> You are [persona]. Act out a single working session with the product, turn by turn:
>
> 1. Decide what to try next, driven by your goal and temperament.
> 2. Use the product — write the interaction down in the trace format.
> 3. Invent the product's response yourself. Make it *plausible, not idealized*: real products have latency, errors, limits, and confusing output. Let the product disappoint you sometimes — your reaction to failure is the most valuable data.
> 4. React in character. Record a brief internal monologue (💭). Decide your next move. Repeat.
>
> Continue for 10–20 meaningful turns, or until you achieve your goal or give up. Giving up is a legitimate ending — if you abandon, record the exact moment and the reason.
>
> You may use functionality that does not exist yet. If your horizon or instincts suggest a capability, simply try it as though it existed and tag it `[INVENTED]`. Do not step out of character to propose features — discover them by needing them.

Trace format by product type (pick what fits; adapt freely):

- **CLI tool** — fenced code blocks of command + invented output
- **Web/mobile app** — numbered narrated steps: "1. Signed in, saw a welcome notification. 2. Clicked it, got bounced to the inbox — confusing."
- **Library/API** — code snippets plus invented results/errors
- **Agent skill / prompt-driven tool** — conversation excerpts between the persona and the agent

The fidelity that matters is the *reaction*, not the mock. A one-line invented response followed by a genuine in-character reaction beats a pixel-perfect fake screen.

Each simulant returns this report to the parent:

```
## Simulant report: [Name]
**Persona:** [one-liner]
**Outcome:** success | partial | abandoned — [why]

### Trace
[the full turn-by-turn session]

### Wish list
- [feature wanted] — severity: blocker | major | nice-to-have — [the trace moment that produced it]

### Friction log
- [pain encountered in functionality that exists today]

### Invented features used
- [name] — [what it did] — [did it deliver?]

### Quote
"[one in-character line that captures the session]"
```

### 6. Synthesise

With all reports in hand:

1. **Cluster by underlying need, not surface feature.** "Export to CSV" from one simulant and "pipe to jq" from another are the same need: get my data out programmatically.
2. **Count convergence** — how many simulants independently hit each cluster. 3+ of 7 is a strong signal; weight blocker-severity wishes heavier.
3. **Classify on the Kano model** — table-stakes (absence causes abandonment), performance (more is better), delighter (unexpected joy).
4. **Rank** by convergence × impact.
5. **Preserve the outliers.** A brilliant idea invented by exactly one simulant goes in a Wildcards section — never average it away.
6. **Collect the friction log** across simulants — pain in *existing* functionality is the quick-wins list.
7. **Collect churn signals** — every abandonment, with its moment and cause.

### 7. Verify against the real codebase

Simulant claims are hypotheses. Before they reach the report, verify them against the actual codebase.

Collect every distinct claim — something *broken* (friction, bugs) or *missing* (feature gaps). Deduplicate across simulants. Spawn **one verifier sub-agent per claim, all in parallel**. Each receives the claim, the simulant quote(s), and instructions to search the codebase (read files, grep symbols, check CLI help, inspect routes/configs). Each returns:

- **Verdict:** confirmed | partially confirmed | refuted
- **Evidence:** file paths, code snippets, or absence-of-match

After all verifiers complete: drop refuted claims entirely — they don't appear in the report. Keep confirmed and partially-confirmed claims with the verifier's evidence attached. Re-rank surviving recommendations and friction items. If every claim in a cluster is refuted, the cluster drops. Refuted wildcards drop too.

### 8. Write the report

Write the full synthesis to `./simulations/<YYYY-MM-DD>-<focus-slug>.md`:

```
# User Simulation: [focus]
*[date] — [N] simulants — product: [one-liner]*

**Diversity axis:** [the primary axis the cohort was spread along, and — if it was chosen autonomously rather than requested — one line on why, so the reader can redirect it]

## Cohort
[the persona table from step 3]

## Top recommendations
### #N — [Feature name]
**Need:** [the underlying job]
**Convergence:** [k]/[N] simulants — [their names]
**Kano:** table-stakes | performance | delighter
**Horizon:** [when this matters]
**Verified:** confirmed | partially confirmed — [verifier evidence summary]
**Evidence:** "[verbatim trace quotes]"

## Wildcards
[one-off ideas worth a look, each with its evidence quote and verification verdict]

## Friction log (exists today, hurts today — verified)
[ranked quick wins, each with verifier evidence confirming the issue]

## Dropped claims (refuted by verification)
[claims verifiers found incorrect, with brief refutation reason]

## Churn signals
[each abandonment: who, at what moment, why]

## Appendix: full traces
[every simulant report, verbatim]
```

Then post a short summary in the conversation: the top 5 recommendations, the single best wildcard, and the report path.

## Guidelines

- **Plausible, not idealized.** A simulation where the product always works teaches nothing. Mocked responses must include realistic latency, errors, and rough edges.
- **Entropy through structure.** Variety comes from distinct persona cards and perturbation cards — never from telling a simulant to "act randomly", which produces incoherent noise.
- **In-character discipline.** Simulants are users, not product managers. Ideas must emerge from a need hit during the session, not from stepping back and brainstorming.
- **Honest synthesis.** Do not inflate convergence counts, and quote real trace lines as evidence. If the cohort found nothing compelling, say so — a null result is cheaper than building the wrong thing.
- **Don't leak ambition into the brief.** The product brief states what exists; the simulants supply the imagination.
- **The diversity axis is the user's call, not a default.** The axis you spread the cohort along silently decides which discoveries are even possible. When intent doesn't settle it, ask; when you must run autonomously, blend axes and declare them. A great cohort sampled along the wrong axis answers a question nobody asked.
