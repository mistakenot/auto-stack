# Interview question bank

Reusable question patterns for `interview-user`. These are *patterns*, not a script — always
ground each question in something concrete you found while researching the repo (a real
clashing pair, a real fuzzy term, a real entity). A generic "what does X mean?" wastes the
user's time; "the code uses both `Account` and `Customer` for the buyer — which is canonical?"
forces a decision and teaches you something either way.

Ask in focused batches (the `AskUserQuestion` tool takes up to 4 at once). Prefer several
short, sharp rounds over one giant one. Each pattern below maps to a kind of decision the
glossary needs.

## 1. Pick the canonical word (resolve a synonym clash)

The highest-value question type — you can only ask it after research surfaces a real clash.

- "The code/docs use both **{A}** and **{B}** for the same thing — which is canonical, and is
  the other an `_Avoid_` synonym or actually a *different* concept?"
- "I see **{A}**, **{B}**, and **{C}** all used around {area}. Are these three names for one
  concept, or genuinely distinct things that happen to look alike?"

## 2. Tighten a fuzzy / overloaded term

- "When you say **{term}**, does that include {X}, or only {Y}?" (e.g. does 'fulfilment'
  include payment, or just shipping?)
- "**{term}** seems to mean different things in {place A} vs {place B} — is that one concept or
  two? If two, what do we call each?"

## 3. Probe a boundary with a concrete scenario

Stress-test relationships by inventing a specific case that forces precision.

- "If a Customer returns one item from a three-item Order, is that a partial **{X}**, a **{Y}**,
  or a **{Z}**? Are those different concepts in our domain?"
- "Walk me through what happens when {edge-case event}. Which of our terms applies at each
  step?" — gaps in the answer reveal missing terms.

## 4. Confirm or kill a candidate term

- "Is **{term}** real domain vocabulary, or an implementation detail that shouldn't be in the
  glossary?" (Class names, table names, framework concepts usually fail this test.)
- "I found **{term}** in {N} commits but nowhere in the current code — is it still a live
  concept, or has it been renamed/retired?"

## 5. Map a relationship between entities

Captures the `_Has_:` line — keep it at `one`/`many`, never schema.

- "Does a **{A}** have one **{B}** or many? And does a **{B}** belong to one **{A}** or
  several?" (e.g. an Order has many Line Items; a Line Item belongs to one Order.)
- "When you talk about a **{A}**, which other entities does it own or contain?" — the answer is
  its `_Has_:` line. Stop at the conceptual level; if the user starts describing columns or
  keys, steer back to "just which things relate to which".

## 6. Establish the definition itself

- "In one sentence: what *is* a **{term}** — not what it does, what it is?"
- "What's the difference between a **{term}** and the thing it's most often confused with?"

## What to do with the answers

Each resolved question becomes a glossary entry (term + tight definition + `_Avoid_` list)
written in immediately — don't batch. A "they're actually two different things" answer means
*two* entries plus mutual `_Avoid_` cross-references. A "that's just implementation" answer
means you correctly kept noise out of the glossary — record nothing.
