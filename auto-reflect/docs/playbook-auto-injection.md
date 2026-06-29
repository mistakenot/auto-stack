---
hash: "f56de5ca"
id: "15d78006"
read_when: "designing how auto-reflect rules get surfaced into an agent's context automatically, or scoping the playbook auto-injection v1"
summary: "Research on auto-injecting retrieved playbook rules into an agent's context at task start — the impact unlock after task 054 — with a spike-first v1 plan, the precision tax, and links to the parked trigger/feedback threads."
title: "Playbook Auto-Injection (Last-Mile v1)"
---

# Playbook Auto-Injection — Research (the "last mile" v1)

> Read when: designing how auto-reflect rules get surfaced into an agent's context
> *automatically* (the retrieve→inject last mile), or scoping the first version of
> auto-reflect that makes the playbook demonstrably change agent behavior.

Status: **research / parked** (2026-06-29). Captured for a later cycle; nothing built.
Extends [self-improving-playbook-retrieval.md](self-improving-playbook-retrieval.md)
(the 5-phase loop) — this doc is specifically about *Phase 1 delivery*: getting the
retrieved rules in front of the agent without it having to ask.

## The reframe: we've been polishing upstream plumbing

The pipeline — mine sessions → consolidate into rules → **retrieve** (the matcher,
improved in task 054) — produces a *ranked list of rules*. But a ranked list nobody
reads has zero impact. As wired today, retrieval is a **manual act**: an agent (or a
skill like `/playbook-search`, or a bare `auto reflect retrieve` call) has to
*choose* to ask. So the playbook only helps when someone remembers it exists —
which is exactly when it's least needed.

**The binding constraint on impact is not ranking quality. It's that the playbook
is not present at the moment of work.**

## The v1: auto-inject at task start

A **hook that fires when work begins, retrieves the top ~3 rules for the stated
intent, and injects them into the agent's context — automatically.** That single
change turns the playbook from a queryable database into an *ambient assistant*
that's just there when a task starts.

It's small because it reuses what exists:
- the **hook event bus** (task 021, merged),
- the **`auto reflect retrieve`** command (already returns ranked predicates and
  appends a `retrieval` event),
- the **event log** (provenance for free).

The v1 trigger can be the dumbest possible one: the **user's prompt at session /
task start**, used verbatim as the `intent`. Richer trigger signatures (error
strings, file globs, tool-use, git-state — see the taxonomy in the retrieval-eval
DIARY) are *v2 precision*, not v1.

## Why this is the impact unlock — and why now

Ordering matters, and **054 is what makes auto-injection safe**:
- You cannot auto-inject from a matcher that *catastrophically excludes* on a wrong
  domain guess (pre-054 `hard-gate` → nDCG@10 0.000), nor from one that floods the
  context with most of the playbook. Auto-injection makes precision *cost real
  tokens and attention*.
- 054 gives a ranking that never deletes the right rule and ranks the relevant ones
  up. So the order was right: **fix the ranking (054), then start injecting it.**

Alternatives that are *not* the next step:
- *Better matcher / BM25 / faceted tags* — sharpening a knife nobody picks up.
- *More rules / better curation* — polishing a store nobody reads.
- *The feedback loop first* — there's nothing to give feedback on until injection
  exists. Injection is the precondition.

## Design sketch (for the later cycle)

- **Which hook.** `SessionStart` (inject once, broad) vs `UserPromptSubmit`
  (inject per task, fresher intent). Lean `UserPromptSubmit` — the per-task intent
  is the better query and avoids stale session-level injection. Open question below.
- **Intent.** v1: the user's prompt text → `auto reflect retrieve "<prompt>"`.
- **How many.** Top **~3** (precision over recall — context is a scarce,
  attention-limited sink; surfacing 100 rules *re-introduces* the dilution problem
  054 just fixed). Hard rules (admissibility constraints) always included.
- **Injection format.** A short, clearly-delimited "relevant playbook rules" block
  (use_when + content), labeled as advisory context, not instructions — the agent
  applies judgment. Keep it terse to bound the token tax.
- **Provenance.** The `retrieval` event already records what was surfaced; tie the
  `retrieval_id` to the session so feedback can later be attributed.

## The precision tax (the thing that makes it "great" vs merely "present")

Auto-injection is not free: every surfaced rule spends tokens *and* attention, and
LLMs degrade with noise ("lost in the middle"). A false-positive rule isn't inert —
it can mislead. So injection quality is gated by **top-K precision**, which is
exactly why 054 came first and why v1 should inject *few, high-confidence* rules.
This is the recall-vs-precision trade made concrete: recall sets the floor (never
hard-exclude — 054), ranking wins the game (get the few right ones to the top).

## Pair it with: minimal feedback + one proof point

Injection alone isn't provably valuable. Two cheap additions make it a *closed,
evidenced* v1:
1. **Minimal feedback signal** — at session end, capture whether the work went well
   (reuse the session-quality signal already specced in `todo.md`), logged against
   what was injected. Seed of the loop; start of the live A/B.
2. **One dogfood proof point** — a demonstrated case where an injected rule
   prevented a *known* mistake (e.g. a gotcha rule like "main is already used by
   worktree" surfacing before a stacked-PR task). First real evidence.

## Spike first, then build

The honest unknown (retrieval-eval's unknown #3): does offline ranking quality
translate to better *live* outcomes? Offline nDCG is a proxy (IIR §8.6). So:
- **Spike (recommended first):** wire the simplest possible injection on a handful
  of dogfood tasks, eyeball whether the surfaced rules are relevant and whether they
  change what the agent does. Evidence before plumbing.
- **Then task:** build the `UserPromptSubmit` hook + retrieve + inject + log as a
  proper feature once the spike shows lift.

## Open questions / risks
- **Hook choice:** `SessionStart` vs `UserPromptSubmit` (vs both) — per-task intent
  vs once-per-session breadth.
- **Top-K & threshold:** fixed top-3, or a score floor? With 054's composite score
  possibly >1.0, a fixed K is simpler and safer than a magic threshold.
- **Noise / over-injection:** does injecting on *every* prompt annoy or dilute?
  Maybe only inject when the top rule clears a confidence bar, or on task-shaped
  prompts only.
- **Measuring lift:** the session-quality signal is a proxy; a real A/B needs
  injection on/off across comparable tasks.
- **Interface generality:** v1 assumes intent-shaped prompts (the regime 054 was
  validated on). Bash-command / sidecar triggers are a different query distribution
  (see the retrieval-eval "query interface validity boundary").

## Relationship to parked threads
- **Trigger signatures** (error strings, globs, tool-use, git-state) → v2 precision:
  fire the right rules at the right *operation*, not just at session start.
- **Query interface** (keyword / bash-hook / live sidecar) → auto-injection *is* the
  bash-hook interface made real; the sidecar is the streaming variant.
- **Feedback-driven lifecycle** → once feedback flows, helpful rules get promoted and
  harmful ones retired automatically (the loop's back half).
