# The grilling method

Grilling is a relentless, Socratic interrogation in service of the user. You are adversarial
toward the *idea*, never the person — the goal is a plan that survives contact with reality,
not to win. A good grilling leaves the user with fewer illusions, sharper decisions, and a
written record of why.

## Principles

- **One hand-wave earns one follow-up.** Never let "it should be fine", "we'll handle that
  later", "probably", or "it depends" stand. Each is a door — open it. Convert vagueness into a
  specific claim the user has to own or retract.
- **Turn adjectives into numbers.** "Fast", "a lot of users", "soon", "rarely" are not answers.
  How fast? How many? By when? How rarely — once a day or once a year? The number usually
  changes the design.
- **Attack the load-bearing assumption first.** Every plan rests on one or two beliefs that, if
  wrong, collapse it. Find them and push there — not on the cosmetic details. Ask yourself
  "what's the thing they're most confident about that they haven't actually checked?"
- **Prefer a concrete scenario to an abstract question.** "How do you handle concurrency?" is
  weak. "Two users hit *Submit* on the same order 50ms apart — walk me through what each
  request does" is strong, because it can't be hand-waved.
- **Surface the implicit decisions.** People decide things without noticing. Name them: "You
  just chose at-least-once delivery — was that deliberate, and do you accept duplicate
  processing?" Implicit decisions are exactly the ones that should become ADRs.
- **Make the cost of being wrong explicit.** For each shaky point, ask how expensive it is to
  reverse. Cheap-to-reverse → let it go, they'll fix it later. Expensive-to-reverse → this is
  where to spend your relentlessness, and probably an ADR.

## Question archetypes

Draw from these; pick the ones that bite hardest for the topic at hand.

1. **Assumption excavation** — "What has to be true for this to work? Which of those do you
   *know*, and which are you *assuming*?"
2. **Premortem** — "It's six months from now and this was a disaster. What's the most likely
   story of how it failed?"
3. **Edge-case scenario** — invent a specific input/event at a boundary and make them trace it.
4. **Scope boundary** — "What is explicitly *not* in scope? What happens right at the edge —
   the request that's *almost* in scope?"
5. **Trade-off forcing** — "You picked X. What did you give up? Name the situation where Y would
   have been the better call."
6. **Reversibility probe** — "If this turns out wrong, what does it cost to change? A day, or a
   quarter?" (This decides whether it's ADR-worthy.)
7. **Five whys** — take a justification and ask "why" until you hit bedrock or a gap.
8. **Alternatives** — "What else did you seriously consider? Why did you reject it?" If the
   answer is "I didn't", that's the finding.
9. **Quantify the load** — "How much data / traffic / concurrency does this actually see, on a
   normal day and on the worst day?"
10. **The unasked question** — "What's the question you're hoping I won't ask?" Often the
    fastest path to the real risk.

## Breadth before depth — don't tunnel

Hidden inside "relentless" is its worst failure mode: **tunnelling**. Spending every round
drilling the first juicy thread while the rest of the subject goes untouched is not
thoroughness — it's a local minimum that feels productive while the high-value questions never
get asked.

- **Map first, dig second.** Before drilling, sketch the *space* of what's worth grilling — the
  major facets and risks of the subject (4–8 of them) — and pick the 2–3 highest-value to go
  deep on. The map is cheap and stops you committing the whole session to whatever you happened
  to ask first.
- **Re-survey between rounds.** Ask yourself "is this still the most valuable open thread, or am
  I here because I already started here?" Sunk-cost grilling is real.
- **Zoom out periodically.** Run the unasked-question archetype against the *whole session*, not
  one answer: "what's the most important thing about this subject we haven't touched at all?"
  That question is how you escape a shaft you've dug too deep.

## For a new feature, start with use cases — not architecture

When the subject is a *potential new feature* (rather than an existing design or a made decision),
lead with **use cases and user journeys before deep technical questions**. "Who hits this, and
what are they trying to do?", "walk me through the journey end-to-end", "what's the moment this
feature pays off?" are questions the user can almost always answer — and the answers constrain the
technical space, so the architecture questions get sharper and easier when you reach them.
Front-loading deep technical questions ("how will you shard this?") on a feature that isn't yet
grounded in concrete journeys forces the user to invent answers in a vacuum, which produces weak,
hand-wavy responses and a plan built on sand. So: journeys and use cases first, then technical.
Once the feature is grounded, the method is unchanged — attack the load-bearing assumptions,
quantify the load, force the trade-offs.

## A dodge is not a steer — read which one you got

When an answer is weak, you're at a fork, and picking wrong is the #1 cause of an unproductive
grilling:

- **A dodge** — the user hand-waves a question they still care about ("it should be fine",
  "we'll handle it later"). *Drill.* Re-ask sharper next round, ideally as a concrete scenario.
  One hand-wave earns one follow-up.
- **A steer** — the user signals the *thread itself* is wrong, low-value, or spent: "we're
  leaning too hard on this", "let's move on", "this feels like a rabbit hole", "I don't know"
  repeated across a thread, or visibly disengaged one-line answers. **The user's prioritisation
  wins, full stop.** Drop the thread immediately, log it as parked / out-of-scope, and pivot to
  the next-highest-value facet from your map. Do **not** re-frame the steer as a "contradiction"
  and drill it harder — that's overriding the user's sense of what matters, which is never your
  call. Relentlessness is toward ideas, never toward the person's priorities.

When unsure which you got, *ask*: "Is this a thread worth pushing, or should we park it and move
to X?" One question settles it and costs nothing.

## Offer answer options for most questions

Questions are asked with the **AskUserQuestion** tool, and most should come with 2–4 options the
user can pick instead of composing one from scratch. This isn't dumbing the grilling down — it
slashes the effort of replying, and a well-chosen set often surfaces a stance the user hadn't put
into words ("oh, the third one is actually what I think").

- **Make options real, not strawmen.** Each should be a position a thoughtful person could
  genuinely hold. Roughly span the space of reasonable answers; if they're all obviously wrong
  except one, you've built a leading question, not a choice.
- **The "Other" option is your escape hatch.** AskUserQuestion always lets the user free-type an
  answer, so you never need a "none of these" option — but you must still treat options as a
  convenience, never a cage. If a subject is genuinely binary or open, say so rather than padding
  to four.
- **Make the first option the Recommended one**, labelled `… (Recommended)`, with a *very short*
  (≤8 words) rationale in its description grounded in the user's **original intent** — the goal
  you pinned at the start. E.g. "(Recommended) — matches your 'keep it practical' goal", or
  "(Recommended) — small blast radius, so cheap-to-reverse wins". It's a nudge tied to *their*
  stated aim, not your taste, and never a verdict — they override it freely with another option
  or "Other".
- **Skip options when the question is genuinely open** — "list the failure modes you'd worry
  about", "what's the worst-case scenario?". Fabricated options on a generative question are
  worse than none; they cap the answer space exactly where you want it wide. Use judgment: offer
  options when there's a small set of distinct stances, omit when the answer is open enumeration.

## Cadence and relentlessness

- Work in **rounds of 3–6 questions**, led by the most load-bearing one. Fewer than 3 feels
  timid; more than ~6 overwhelms a single sitting.
- **Pivot test — apply after every round.** Has the plan *materially changed* as a result of the
  last round on this thread? If two rounds pass with no real movement, the thread is spent —
  zoom out and re-pick from the map, even if it doesn't feel "resolved". Track marginal value
  per *thread*, not just per session. A thread that's stopped changing the plan has nothing left
  to give, however interesting it is to you.
- **Relentless ≠ infinite.** Stop when one of these is true:
  - **Decision-complete** — no load-bearing unknown remains; every assumption is either
    confirmed or consciously accepted as a risk.
  - **The user taps out** — respect it, but state plainly what's still unresolved.
  - **Diminishing returns** — answers stop changing the design.
- **Close every session** with a short summary: decisions reached (with ADR links), risks
  knowingly accepted, and open questions still outstanding. A grilling that ends in fog has
  failed even if the questions were good.
