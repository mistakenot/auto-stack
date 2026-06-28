---
name: grill-me
description: "Relentlessly interrogates you to pressure-test a plan, design, or decision — surfacing hidden assumptions, edge cases, and forced trade-offs — and records the outcomes as numbered ADRs in docs/adr/. Use when 'grill me', 'stress-test this', 'poke holes in my plan', 'interrogate my design', 'challenge my thinking', or before a hard-to-reverse decision. Reads the ubiquitous-language glossary if present. Not for brainstorming new ideas (use generate-10-ideas) or routine task planning (use new-task)."
---

# Grill Me

Run a relentless, Socratic interrogation that pressure-tests whatever the user brings — a plan,
a design, a decision, a vague intention — until the load-bearing assumptions are exposed and the
real decisions are made and recorded. You are adversarial toward the *idea*, never the person:
the aim is a plan that survives contact with reality, plus a written trail of why each call was
made. The full method (principles, question archetypes, cadence, when to stop) is in
[references/grilling-method.md](references/grilling-method.md) — read it before grilling.

## On startup — orient (cheap, every run)

1. **Pin the subject and the intent.** What exactly is being grilled? If the user was vague
   ("grill me on the new sync feature"), get one sentence of scope before firing questions — you
   can't pressure-test a target you can't see. Also note their underlying *intent* — what a good
   outcome looks like for them — in a sentence; you'll use it to recommend answer options later.
2. **Pick up the ubiquitous language if it's there — but don't depend on it.** Check for
   `docs/concepts/UBIQUITOUS_LANGUAGE.md`. If it exists, read it and use the canonical terms in
   your questions and ADRs; if the grilling coins or sharpens a domain term, offer to record it
   with the `domain-modelling` skill. If it doesn't exist, carry on silently — this
   skill works perfectly well without a glossary and never requires one.
3. **Check prior records.** Glance at `docs/grilling/grilling-log.md` (past sessions on this
   subject) and `docs/adr/` (decisions already recorded), so you don't re-litigate settled ground
   — and so new ADRs get the next number.

## The grilling loop

Questions are asked live with the **AskUserQuestion** tool (fast, with pickable options), and
every question→answer pair is appended to one append-only log so the thinking is never lost:
`docs/grilling/grilling-log.md`. Format and rules in
[references/grilling-log-format.md](references/grilling-log-format.md).

1. **Map, then probe.** First sketch the *facets* worth grilling for this subject (4–8 risks /
   angles) and pick the 2–3 highest-value — your guard against tunnelling down the first thread
   (see *Breadth before depth* in the method). Then compose the sharpest questions for those
   facets, led by the most load-bearing one. Favour concrete scenarios over abstract asks.
2. **Ask with AskUserQuestion.** Up to 4 questions per call. For most questions, offer 2–4
   options and make the **first option the recommended one** — label it "(Recommended)" with a
   ≤8-word reason tied to the user's intent. The user can always pick "Other" to free-type, so
   that's your escape hatch. Skip options for genuinely open "list/enumerate" questions (let them
   free-type) — fabricated options there just cap the thinking.
3. **Log it.** Append the question→answer pairs to `docs/grilling/grilling-log.md` as a new
   section that opens with a short **context** line — the subject, the user's intent, and what
   prompted this round — so anyone reading cold knows where the questioning came from. Create the
   file lazily on the first grilling; append only, never rewrite earlier sections; record the
   user's *actual* answers (the option chosen or their typed text), never invent.
4. **Press — but read dodge vs steer.** For a weak answer the user still cares about (a *dodge*),
   re-ask *sharper* next round as a concrete scenario. But if the user signals the thread itself
   is low-value or spent (a *steer*: "leaning too hard on this", "let's move on", "rabbit hole",
   repeated disengaged answers), **drop it and pivot** to the next facet from your map — never
   re-frame a steer as a contradiction and drill harder. After two rounds with no material
   movement on a thread, zoom out and re-pick too (the pivot test). When unsure which you've got,
   just ask whether to push or park it.
5. **Record decisions as ADRs.** When an answer settles something **hard to reverse + surprising
   + a real trade-off**, write a numbered ADR in `docs/adr/NNNN-slug.md` per
   [references/adr-format.md](references/adr-format.md). Don't ADR the easy-to-reverse stuff —
   that just adds noise.
6. **Repeat** until decision-complete or the user taps out, then give a short closing summary
   (decisions made + ADR links, risks knowingly accepted, open questions). It's all preserved in
   the log.

## What earns an ADR

Be sparing — a grilling produces many answers, but most aren't decision records. Write an ADR
only when all three hold: it's **hard to reverse**, **surprising without context**, and **the
result of a real trade-off**. The reversibility probe in the grilling method is how you decide.
Everything else lives in the grilling log, not in `docs/adr/`. See
[references/adr-format.md](references/adr-format.md) for the (deliberately tiny) template and the
full rationale.

## Tone

Relentless, not hostile. The user *asked* to be grilled — honour that by actually pushing, not
lobbing softballs. But every question is in service of their plan; when they land a solid
answer, say so and move on. End in clarity, never in fog: a grilling that leaves the user more
confused than when they started has failed, however clever the questions were.
