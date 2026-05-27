# Deployable Architecture

Practical guidance for taking the orthogonal-questioning experiment's findings into production. Companion to the [experiment synthesis README](README.md).

## Why per-dim classifiers + active learning is the right shape

Across the four phases we established:

1. The original "cosine similarity over PCA basis" approach **does not work** (Phases 1-3 ruled it out across real and synthetic data).
2. Information about user preferences **is recoverable** from decision-text embeddings (R²=0.28 via linear probe in Phase 3).
3. Binarizing to **signs** boosts usable accuracy from ~28% R² to ~76% per-dim sign accuracy (Phase 4).
4. **Per-dim classifiers** separate legible from invisible preference axes; a joint probe averages them and loses information.
5. **Active learning** over per-dim uncertainty converges in 4-5 questions on linguistically-legible dimensions.

The deeper reason this beats cosine: cosine similarity is unsupervised and assumes the embedding space is isotropic with respect to your task. A learned probe is supervised and learns which directions in 1536-dim space matter for *your specific task*. This is the classic geometric-vs-discriminative distinction. Embeddings trained for general semantic similarity weight many directions that have nothing to do with user preferences; supervised classifiers can ignore those.

## Bootstrap process

### Step 1: Pick your 6-10 preference dimensions

Look at the top decision types in mined session data (`auto-etl` output, top categories from `extraction_stats.json` in the experiment artifacts) and group them into preference axes that are:

- **Conceptually distinct** — "strict vs lenient" is not the same as "verbose vs terse"
- **Likely to have distinct vocabulary in user messages** — the linguistic-legibility check; Phase 3+4 showed some preferences (D5 schema rigidity, D6 API explicitness) don't recover regardless of method
- **Worth asking about** — don't include axes the user would never disagree with

For auto-stack specifically, ~6 axes feels right: rigor, verbosity, durability, error-stance, dependency-appetite, scope-discipline. Skip axes Phase 3+4 showed don't recover.

### Step 2: Build a labeled training set

Two paths:

- **Synthetic-first (cheaper)**: use the Phase 3 generator pattern. Sample fake users with random preference vectors, render their decisions via LLM, validate on real Phase 1 data. ~$1 in API costs.
- **Real-first (better signal)**: label ~200 real `AskUserQuestion` events from `autosearch search 'User has answered your questions'` with their dimension positions, by hand or via LLM-as-judge. ~1 hour of effort.

### Step 3: Train one binary classifier per dimension

For each axis:
1. Take all decisions touching that axis
2. Embed them with `text-embedding-3-small`
3. Train a `LogisticRegression` (or any binary classifier) to predict the sign of the user's preference along that axis

Save the 6-10 weight vectors. The entire "model" is six dot products and a sigmoid — ~36 KB total.

### Step 4: Identify which dimensions are legible

From classifier accuracy on a held-out set:
- Dimensions with accuracy ≥ 0.7 → **inferable from decisions**
- Dimensions with accuracy < 0.7 → **ask-only**

Phase 4 found 6/8 of its dimensions inferable; expect similar on real data. The exact split depends on your domain.

### Step 5: Calibrate the question budget

Run the AL simulation on held-out users: how many questions to reach all-signs-correct? Phase 4 got 4.17. Set the budget at 5-6 to give yourself headroom.

## Usage walkthrough

A session walkthrough showing what users would see:

1. **User runs `/new-task fix the search filtering bug`.** The task description gets embedded with `text-embedding-3-small`.

2. **Cold-start prediction.** If this is a known user with prior sessions: load their previous decision embeddings, run each through the classifiers, average the sign predictions. Start with ~75% confidence per dimension before asking anything. If new user: start at the population centroid.

3. **Identify the gaps.** For each dimension, the classifier returns a probability. Sort by uncertainty (closest to 0.5). Pick the top-K most uncertain dimensions.

4. **Ask those K questions.** Bundled into one `AskUserQuestion` call, written in plain language. "How aggressively should this validate input? Strict (reject anything malformed) / lenient (coerce and warn) / off (caller's problem)."

5. **Update predictions.** The user's answers fix the signs on the dimensions they answered. Re-run inference on remaining dimensions.

6. **Build the spec.** Translate the final sign vector into a structured `requirements.md`. Each axis becomes a section: "error handling: fail-fast; validation: strict; logging: verbose; ..."

7. **Capture new evidence.** The user's confirmed answers + the agent's enacted code (from git) feed back into the training set on a rolling window. Per-dim classifiers retrain monthly.

### What it actually feels like

Before (current `/new-task`):

> "What's the storage backend?" "How should we handle malformed input?" "What's the test strategy?" "Should this be a CLI or a library?" ... 15 questions over 8 minutes.

After:

> "Best guess: postgres, fail-fast, JSON output, E2E tests, single-command CLI. Three things I'm not sure about: (a) ...; (b) ...; (c) ...." ... 3 questions, user confirms or corrects in 90 seconds.

The system isn't 10× smarter. It just stops asking questions where prior sessions already answered.

## Integration with auto-stack

- `auto-reflect` already has rule-extraction infrastructure — extend it to maintain the per-dim classifiers
- `autosearch` provides labeled training data via the `AskUserQuestion` mining pattern from Phase 1
- The `/process-requirements` skill is the natural integration point: load classifiers → predict signs from task + history → ask only uncertain dimensions → emit `requirements.md`
- Classifiers are tiny (~36 KB total). Ship them with the skill

Implementation effort: ~one focused week for an initial version. The math is already proved out.

## Honest comparison vs a 20-template baseline

For one user at auto-stack's current scale (~670 sessions), **20 well-curated task templates probably match the classifier approach on cost-effectiveness, and might beat it on inspectability.**

### Where templates win

- **Inspectable** — user can read all 20 and know what they'll get
- **Cheap to bootstrap** — half a day of writing vs a week of ML
- **Predictable** — same task always produces same questions
- **Easy to fix** — find bad template, edit it

### Where classifiers win

Three places, all real but only at scale:

1. **Combinatorial coverage.** 6 binary dims = 64 preference profiles. 20 templates cover ~30%. With 5+ users with different preferences, you'd need ~100 templates and they'd be 80% duplicative. Classifiers compose: 6 small models cover all 64 combinations for free.

2. **Personalization without per-user templates.** Two users, identical task description. Template: same questions for both. Classifier: load each user's prior decisions, predict their signs, ask only about the uncertain ones. User A gets 2 questions, user B gets 5.

3. **Graceful drift handling.** Templates are frozen at last-edit time. If a user's preferences shift, templates still ask the old way. Classifiers retrain on rolling-window data and pick up drift automatically.

### Numbers, concretely

| Metric | 20 templates | Per-dim classifier |
|---|---|---|
| Bootstrap effort | ~half day | ~one week |
| Questions per task (estimated) | 4-6 | 3-5 |
| Maintenance | manual edits | passive retraining |
| Cold-start (new task type) | "no template applies" | sign predictions still work |
| Cold-start (new user) | works immediately | needs population prior |
| Auditability | easy | medium |

The marginal improvement is maybe 1-2 questions per task. Real but not transformative.

### The hybrid is probably the right answer

The best architecture for auto-stack today:

1. **20 task templates as the base layer** — covers 70% of cases, fast, inspectable
2. **Per-user preference overrides on top** — small lookup table ("this user always picks strict validation") built from explicit feedback
3. **Per-dim classifier as a fallback** for tasks that don't match templates well — detects "novel task" via low similarity to all templates, falls back to predict-then-ask

Most of the classifier benefits at a fraction of the cost. The classifier is the long-tail handler, not the primary mechanism.

### When the full classifier framework actually pays off

Worth building when *any* of these hold:

- Multi-user with divergent preferences (templates explode combinatorially)
- More than ~50 distinct task types (hand-curation becomes its own job)
- Preference drift over time (templates need recurring upkeep)
- Tasks that mix dimensions in novel ways (templates have to enumerate; classifiers compose)
- The cost of an extra question is high (interactive UX where question count affects completion)

For a solo developer on personal projects, none are urgent. For auto-stack-as-a-product with multiple users, all eventually matter.

## What I'd actually do

If shipping in the next month: **20 templates + 5-10 per-user preference overrides.** Engineering time that would go into classifiers goes into better templates instead.

If this is a 6-month roadmap: **start with templates, instrument the system to log "template didn't fit, had to override significantly" events, build the classifier only when those events accumulate.** Let the data tell you when the simpler solution is breaking down.
