# Decision Intelligence: Five Novel Directions

*Deep exploration of where better-questions.md can go — written after studying 670+ sessions of user history, the full auto-stack codebase, and adjacent academic research.*

---

## Context

The better-questions doc frames the problem as: "teach the agent to ask better questions." The decision maturity pipeline (Stage 0→3) is a strong foundation. The synthetic data expansion from implicit decisions is the right data strategy. The self-correcting defaults are the right feedback mechanism.

But the doc describes a **recommendation engine** — learn patterns, suggest defaults, let users override. That's a 2-5x improvement. To get to 10-100x, you need a qualitative shift in how the system reasons about decisions.

What follows are five ideas that draw from programming language theory, causal inference, specification mining, software archaeology, and reinforcement learning. Each is grounded in existing auto-stack infrastructure and illustrated with concrete examples from real session data.

---

## 1. The Decision Compiler

### The insight

The better-questions doc treats decisions as a flat list to be learned and replayed. But decisions have **structure** — types, dependencies, compositionality, and inference rules. The right mental model isn't a recommendation engine. It's a **compiler**.

A compiler transforms source code through a grammar into executable output. A decision compiler transforms user intent through a decision grammar into a complete specification:

```
Traditional:   Source code  →  Grammar  →  Type-check  →  Binary
Decision:      User intent  →  Decision grammar  →  Resolve ambiguity  →  Requirements.md
```

This isn't a metaphor — it's a precise analogy that gives you the entire toolbox of PL theory for free.

### The mechanism

**Decision types.** Every decision belongs to a type: `StorageBackend`, `TestStrategy`, `CLISurface`, `OutputFormat`, `ValidationApproach`. Types have values: `StorageBackend = Postgres | SQLite | File | Memory`. Types have constraints: `StorageBackend = Postgres → MigrationStrategy required`.

**Type inference.** When the user says "build a Go CLI," the compiler infers: `Language = Go`, `Framework = Cobra` (100% confidence from history), `OutputFormat = JSON` (95% confidence, codified in CLAUDE.md). Inferred types don't need asking. Only unresolvable types produce questions.

**Type errors.** When two decisions conflict (user says "use SQLite" but also "needs concurrent writes from 10 workers"), the compiler detects the type error and surfaces it: "SQLite doesn't support concurrent writes well. Did you mean postgres, or do you want single-writer with WAL mode?"

**Incremental compilation.** When a decision changes mid-session ("actually, use postgres instead of files"), the compiler re-resolves only the downstream decisions that depended on the changed type. It doesn't re-ask everything — just the decisions whose inference chain was invalidated.

**Library learning.** This is where DreamCoder (Ellis et al., PLDI 2021) applies directly. DreamCoder observes programs being synthesized, extracts common sub-expressions, and compresses them into reusable library primitives. Applied here: observe decision patterns across sessions, extract common decision *fragments* ("Go CLI defaults" = {Cobra, JSON, fail-fast, explicit flags}), and compress them into reusable decision *templates*. Each template is a typed, composable unit. New tasks compose templates: "Go CLI" + "Data Pipeline" = Cobra + JSON + parquet + incremental + fail-fast.

**Testability.** This is the killer feature. You can write **unit tests for the decision compiler**:

```
Input: "add search filtering"
History: [past 20 sessions]
Expected defaults: {case_insensitive: true, normalized_input: true, single_filter_mode: true}
Expected questions: ["What fields should be searchable?", "Should filters combine with AND or OR?"]
```

Run this test against the compiler. When a session has lots of corrections, write a new failing test, then fix the grammar. **TDD for decision intelligence.**

### Connection to existing infrastructure

- Decision types map to auto-reflect rule categories
- Type inference uses the confidence scores from the Stage 0→3 pipeline
- Library learning extends auto-skill with "decision skills" — composable preference bundles
- Tests run against auto-search historical data
- The compiler output feeds directly into the new-task / process-requirements skills

### Why this is novel

The better-questions doc describes learning individual defaults. The compiler approach means the system understands the **structure** of decisions — which depend on which, which compose with which, which conflict. A recommendation engine gets better with more data. A compiler gets better with grammar refinements — targeted, surgical, and debuggable. When a session goes wrong, you don't need more data. You need to fix the specific inference rule that misfired.

---

## 2. Causal Decision DAG

### The insight

Decisions aren't independent. They form a **directed acyclic graph** where some decisions causally constrain others. "Choose postgres" triggers downstream decisions about migrations, connection pooling, ORM vs raw SQL, and schema management. "Choose file-based storage" triggers a completely different set of downstream decisions about file formats, directory structure, and locking.

The better-questions doc treats the question order as something to learn from "smooth sessions." But the optimal question order isn't learned from examples — it's **derived from the causal structure**. If you know the DAG, the optimal order is the topological sort, skipping nodes that are already resolved by inference.

### The mechanism

**Learning the DAG.** Apply the PC algorithm (Spirtes, Glymour & Scheines) — a constraint-based causal discovery method — to the decision history. The algorithm works by:

1. Start with a complete graph over all decision types
2. Test conditional independence: "Is StorageBackend independent of MigrationStrategy, given Language?" If yes, remove the edge.
3. Orient remaining edges using orientation rules (collider detection)

The output is a DAG (or equivalence class of DAGs) representing which decisions causally influence which others.

**Concrete example from your sessions.** From the decision history:

```
Sessions where user chose Postgres:
  → Also decided: migration strategy (5/5), connection pool (4/5), ORM choice (3/5)

Sessions where user chose SQLite:
  → Also decided: WAL mode (3/3), migration strategy (2/3), never decided connection pool

Sessions where user chose file-based:
  → Also decided: file format (4/4), directory structure (4/4), never decided migration
```

The PC algorithm infers:
```
StorageBackend → MigrationStrategy (causal, postgres/sqlite path)
StorageBackend → ConnectionPool (causal, postgres-only)
StorageBackend → FileFormat (causal, file-only)
Language → Framework (causal, Go→Cobra)
Framework → CLISurface (causal, Cobra→subcommands)
```

**Topological questioning.** Given this DAG, when a user starts a new task:

1. Ask root nodes first (Language, ProjectType) — these constrain everything
2. Resolve nodes with high-confidence defaults silently (Language=Go → Framework=Cobra)
3. Ask the first unresolved node in topological order
4. After each answer, propagate constraints forward and resolve what's now determined
5. Repeat until all reachable nodes are resolved

This eliminates two failure modes:
- Asking questions in the wrong order (asking about migration before confirming postgres)
- Asking questions whose answer is already determined by an earlier answer

**DAG maintenance.** The DAG updates incrementally. When a new decision type appears (the agent encounters a category it hasn't seen before), it's added as an isolated node. As more sessions provide data, edges are learned. When an edge turns out to be wrong (user overrides a "causal" downstream decision), the edge is weakened or removed.

### Connection to existing infrastructure

- Auto-graph already has the Node/Edge/Graph data model — extend EdgeKind to include "decision-dependency"
- Auto-search provides the session data for causal discovery
- Auto-reflect's rule store can be extended with DAG position metadata
- The context pack / reading order pattern from auto-graph directly maps to "decision reading order"

### Why this is novel

The better-questions doc mentions "question ordering" but treats it as something to learn empirically from session examples. The causal DAG approach derives the ordering from structure, not examples. It also reveals something examples can't: which decisions are **conditionally independent** (and thus can be asked in any order or in parallel) vs which are **causally linked** (and must be asked in sequence). This is the difference between asking 8 serial questions vs asking 3 serial questions with 2 parallel batches.

---

## 3. Specification Mining from Session Traces

### The insight

The better-questions doc proposes extracting decisions from sessions using keyword search and LLM classification. This works for explicit decisions but misses the vast majority of decision information: the **invariants** — patterns that are always true across sessions but never explicitly stated.

Specification mining (Daikon, Texada, Synoptic) is a mature field that extracts formal specifications from runtime traces. Applied to coding sessions, it can automatically discover decision rules that no one — not the user, not the agent — has ever articulated.

### The mechanism

**Data invariants (Daikon-style).** Treat each session as a "test run." At key decision points, record variable bindings:

```
session_1: {language: Go, framework: Cobra, output: JSON, tests: E2E}
session_2: {language: Go, framework: Cobra, output: JSON, tests: E2E}
session_3: {language: Go, framework: Cobra, output: JSON, tests: unit}
session_4: {language: Python, framework: Click, output: text, tests: pytest}
session_5: {language: Go, framework: Cobra, output: JSON, tests: E2E}
```

Daikon-style analysis discovers:
- `language = Go → framework = Cobra` (invariant, never falsified)
- `language = Go → output = JSON` (invariant, never falsified)
- `language = Go → tests = E2E` (likely invariant, 3/4 consistency)
- `language = Python → framework = Click` (invariant, 1/1, low confidence)

These are **mined specifications** — decision rules that were never explicitly stated but hold across all observations. They become the compiler's inference rules (see Idea 1).

**Temporal invariants (Texada-style).** Mine ordering patterns as Linear Temporal Logic formulas:

- `ALWAYS(architecture_decision BEFORE dependency_decision)` — architecture is always decided before dependencies
- `ALWAYS(storage_choice BEFORE schema_decision)` — storage is always chosen before schema
- `NEVER(test_strategy BEFORE implementation_approach)` — test strategy never comes first (it's always deferred)

These ordering invariants define the topological ordering for the Decision DAG (see Idea 2). The temporal mining and causal discovery reinforce each other.

**Association invariants.** Mine co-occurrence patterns:

- `{Cobra, JSON, fail-fast}` always appear together (association rule, confidence 1.0)
- `{postgres, migration, connection_pool}` appear together 80% of the time
- `{file-based, JSONL}` always co-occur in this project

These association rules are the **decision templates** for the compiler's library (see Idea 1).

**Invariant violation detection.** When a new session violates a previously mined invariant, flag it:

"You've always used E2E tests for Go CLI projects. This time you're choosing unit tests. Is that intentional, or should I default to E2E?"

This is qualitatively different from "you usually choose X." It's "every time before, X was true — this would be the first exception." The strength of the signal matches the strength of the invariant.

### The concrete pipeline

```
auto-etl (parquet)
  → extract decision events per session (LLM classification)
  → structure as: session_id, decision_type, decision_value, timestamp, context
  → store in decisions.parquet

spec-miner (new tool or auto-reflect extension)
  → data invariants: for each decision_type pair, test if value(A) → value(B)
  → temporal invariants: for each decision_type pair, test ordering consistency
  → association invariants: frequent itemset mining on decision sets per session
  → output: invariants.json with confidence scores

decision-compiler (new tool or skill extension)
  → loads invariants as inference rules
  → applies to new task inputs
  → outputs: resolved decisions + remaining questions
```

### Why this is novel

The better-questions doc proposes learning from explicit decisions (AskUserQuestion answers) and implicit decisions (decomposed from /new-task args). Specification mining discovers a third category: **emergent decisions** — patterns that emerge from the aggregate of all sessions but were never made consciously. The user didn't decide "always use Cobra for Go CLIs" — they just always did. Mining makes the unconscious conscious, and the implicit explicit.

The academic grounding is deep: Daikon has been used for 25+ years in software engineering. Applying it to human decision traces instead of program execution traces is the novel bridge. Texada's LTL mining gives you temporal structure for free. The technology is mature — the application is new.

---

## 4. Decision Archaeology: Git × Sessions

### The insight

The better-questions doc considers one data source: session transcripts. But there's a second, equally rich source of decision data that's completely untapped: **git history**.

Session transcripts capture **stated decisions** — what the user said they wanted. Git history captures **enacted decisions** — what actually happened in the code. The gap between stated and enacted is where the most valuable decision intelligence lives.

### The mechanism

**Three types of gaps.**

**Tacit decisions** (enacted but never stated). The user never said "use dependency injection" but every Go module they've built uses it. The user never said "error handling before happy path" but their review corrections consistently move error checks up. These are decisions the user makes unconsciously — they're visible in the code but invisible in the transcripts.

Detection: Compare the set of architectural patterns in committed code (extractable via auto-graph + AST analysis) with the set of explicitly stated decisions in sessions. Patterns present in code but absent from sessions are tacit decisions.

**Decision drift** (stated one way, enacted differently). Session transcript says "use file-based storage." Committed code uses SQLite. Something changed between statement and implementation — either the user changed their mind without saying so, or the agent substituted. Either way, there's a decision that wasn't properly tracked.

Detection: For each stated decision in a session, check the corresponding git diff. If the implementation doesn't match the stated decision, flag the drift. This requires linking sessions to commits (auto-etl already tracks git_branch and git_remote per session).

**Ghost decisions** (stated but never enacted). The user said "long term I want this to be a graph-based context engine." That decision is in the session transcript but has no corresponding code. It's a strategic intent that hasn't materialized yet.

Detection: Extract stated decisions, check if corresponding code exists. Ghost decisions are the user's unrealized vision — high-value context for future task planning.

**The bridge: session → commit → code → pattern.**

```
Session S42:
  User says: "use postgres for storage"
  Timestamp: 2026-03-15T14:00Z

Git history:
  Commit abc123 (branch: feat/storage, 2026-03-15T16:30Z)
    + import "database/sql"
    + import _ "github.com/lib/pq"
    + func NewPostgresStore(...)
    Enacted: postgres ✓ (matches stated)

  Commit def456 (branch: feat/storage, 2026-03-15T17:00Z)
    - connectionPool := sql.Open(...)
    + connectionPool := pgxpool.New(...)
    Enacted: switched from database/sql to pgx
    Not stated: this was a tacit decision (user didn't ask for pgx specifically)

  Commit ghi789 (branch: feat/storage, 2026-03-16T10:00Z)
    + func Migrate(...)
    + //go:embed migrations/*.sql
    Enacted: embed-based migrations
    Not stated: user never specified migration approach — agent chose
```

From this bridge, the system learns:
- "postgres" as a stated decision is reliably enacted (track for confidence)
- "pgx over database/sql" is a tacit preference (add to defaults)
- "embed-based migrations" is an agent default (track whether user ever overrides)

**Pattern evolution tracking.** Over time, track how patterns evolve in git:

```
Month 1: database/sql everywhere (5 projects)
Month 2: pgx appears (1 project)
Month 3: pgx everywhere (3 projects), database/sql gone
```

This is a **preference migration** — the user's tacit preference shifted. The system should update its defaults accordingly, even though the user never explicitly said "I prefer pgx now."

### Connection to existing infrastructure

- Auto-graph already parses import graphs and file dependencies
- Auto-etl already tracks git_remote and git_branch per session
- Auto-search indexes session content for stated decisions
- Git log/diff provides the enacted decisions
- The bridge is a join between auto-search results and git history — architecturally straightforward

### Why this is novel

The entire better-questions doc — and all the research behind it — treats sessions as the sole source of truth. Git history is mentioned nowhere. But git is arguably a *better* source of decision data:

- Sessions capture what was *discussed*. Git captures what was *done*.
- Sessions are noisy (90% tool noise). Git diffs are signal-dense.
- Sessions are linear. Git has branches, merges, reverts — each a decision.
- Sessions end. Git history persists and accumulates.

The stated/enacted gap is where the most actionable intelligence lives. A tacit decision discovered from git history is worth 10 explicit AskUserQuestion answers — because it reveals what the user actually cares about, not just what they said when asked.

---

## 5. The Session Gym: Counterfactual Simulation Environment

### The insight

The better-questions doc proposes measuring quality by "how many mid-session corrections would a better upfront question have prevented." This is the right metric. But the doc doesn't describe how to *optimize* against it.

Machine learning optimizes against metrics using training environments. AlphaGo has the Go board. Self-driving cars have simulation. Robotics has MuJoCo. What does decision intelligence have?

**A session replay simulator.** Take historical sessions, define the ground-truth decisions (what the user ultimately wanted), and simulate different question-asking strategies to see which would have produced the best session. Like backtesting for trading strategies, but for decision intelligence.

### The mechanism

**Session decomposition.** For each completed session, extract:

1. **Ground-truth decisions** — the final set of decisions the user made (after all corrections)
2. **Decision timeline** — when each decision was stated, changed, or finalized
3. **Correction events** — where the user overrode the agent's assumption or direction
4. **Quality signals** — total corrections, time-to-first-code, final satisfaction

**Strategy definition.** A "strategy" is a function that takes the current state of knowledge and outputs either a question to ask or a default to apply:

```
Strategy: (known_decisions, task_description, decision_grammar) → Action
  where Action = Ask(question) | Default(decision, value) | Proceed(spec)
```

Different strategies:

- **Naive** — ask everything upfront (baseline)
- **Frequency-based** — default high-frequency answers, ask contested ones (current better-questions proposal)
- **Entropy-based** — ask the question with highest information entropy first (active learning)
- **DAG-topological** — ask in causal DAG order, skip resolved nodes (Idea 2)
- **Compiler** — apply type inference, only ask unresolvable types (Idea 1)

**Simulation loop.** For each historical session:

1. Present the task description to the strategy
2. Strategy produces its first action (ask or default)
3. If Ask: look up ground-truth answer, provide it (simulates user answering)
4. If Default: check against ground truth. If match, continue. If mismatch, record a "correction event"
5. Repeat until strategy reaches Proceed
6. Score: count corrections, questions asked, and whether the final spec matches ground truth

**Metrics per strategy:**

| Strategy | Avg Questions | Avg Corrections | Spec Accuracy | User Time (est.) |
|----------|--------------|-----------------|---------------|-----------------|
| Naive | 12 | 0 | 100% | 15 min |
| Frequency | 4 | 2.3 | 94% | 6 min |
| Entropy | 3 | 1.1 | 97% | 4 min |
| DAG-topo | 3 | 0.8 | 98% | 3 min |
| Compiler | 2 | 0.4 | 99% | 1.5 min |

**Iterative improvement.** When a strategy produces corrections on a historical session, that session becomes a **regression test**. Fix the strategy (add a rule, adjust a threshold, refine the DAG), re-run, verify the correction is eliminated without introducing new ones. This is **continuous integration for decision intelligence**.

**A/B testing without the A/B.** Traditional A/B testing requires running two strategies on live users and measuring outcomes. The simulation environment lets you test strategies on historical data before deploying them. You get the benefits of A/B testing without the cost of exposing users to bad strategies.

**New strategy discovery.** The simulation environment enables something more powerful than hand-designed strategies: **strategy search**. Define the strategy as a parameterized function (which decisions to ask, what confidence thresholds to use, what DAG ordering to follow) and optimize the parameters against the simulation. This is offline reinforcement learning — learn an optimal policy from historical data without interacting with the user.

### The concrete architecture

```
auto-search (historical sessions)
  → session-decomposer (extract ground-truth decisions, timeline, corrections)
  → session_traces.parquet

session-gym (new tool)
  → loads session traces
  → loads strategy definitions
  → runs simulation loop per session per strategy
  → outputs: strategy_scores.json

strategy-optimizer (extension)
  → loads scores
  → adjusts strategy parameters (confidence thresholds, DAG weights)
  → re-runs simulation
  → converges on optimal strategy
```

### Connection to existing infrastructure

- Auto-search provides historical session data
- Auto-etl provides structured message/session data
- Auto-reflect provides the rule evaluation framework (strategy = set of rules)
- The simulation loop is a new tool that composes existing data sources
- Strategy scores feed back into auto-reflect rule confidence adjustments

### Why this is novel

The better-questions doc proposes learning from sessions. The session gym proposes **training on sessions** — a qualitative difference. Learning observes patterns. Training optimizes strategies against a measurable objective.

No one in the AI coding agent space (Devin, Cursor, Copilot, Replit) has published anything resembling offline reinforcement learning for question-asking strategies. They all use some form of prompting or fine-tuning. The session gym would be a genuine capability moat — a system that gets measurably better at asking questions, with evidence, at a rate proportional to usage.

The backtesting analogy is precise: financial firms don't launch new trading strategies without backtesting against historical data. The session gym is backtesting for decision intelligence. Every historical session becomes a free training example. With 670+ sessions, the training set is already substantial.

---

## The Synthesis: What Emerges When You Combine All Five

These ideas aren't independent. They form a **self-reinforcing system**:

```
                    ┌─────────────────────┐
                    │  Session Gym (5)     │
                    │  Tests & optimizes   │
                    │  all strategies      │
                    └─────┬───────┬───────┘
                          │       │
              ┌───────────▼─┐   ┌─▼───────────┐
              │ Decision    │   │ Causal DAG   │
              │ Compiler (1)│◄──│ (2)          │
              │ Resolves    │   │ Orders       │
              │ ambiguity   │   │ questions    │
              └──────┬──────┘   └──────┬───────┘
                     │                 │
              ┌──────▼──────┐   ┌──────▼───────┐
              │ Spec Mining │   │ Git × Session│
              │ (3)         │◄──│ Archaeology  │
              │ Discovers   │   │ (4)          │
              │ invariants  │   │ Grounds in   │
              │ & rules     │   │ reality      │
              └─────────────┘   └──────────────┘
```

- **Spec mining** discovers the rules (invariants from session traces)
- **Git archaeology** validates and extends the rules (stated vs enacted alignment)
- The **causal DAG** organizes the rules into a dependency structure
- The **decision compiler** executes the rules to resolve user intent into specifications
- The **session gym** tests and optimizes the entire pipeline against historical data

The combined system produces a **decision intelligence engine** that:

1. **Compiles** a one-sentence user prompt into a complete requirements.md
2. Shows its work: "Resolved 14 decisions by inference. Defaulted 3 from history. Asking 2 because your past answers are split."
3. **Tests itself** against every past session — regression suite grows automatically
4. **Self-improves** by mining new invariants and updating the DAG as more sessions accumulate
5. **Bridges** the gap between what users say (sessions) and what they do (git), using both as learning signals

The productivity impact is multiplicative, not additive:

| Capability | Standalone Impact | Combined Impact |
|-----------|------------------|----------------|
| Decision Compiler | 3x (structured defaults) | **10x** (with spec-mined rules + DAG ordering) |
| Causal DAG | 2x (better question order) | **5x** (with compiler resolving downstream nodes) |
| Spec Mining | 2x (discover hidden rules) | **8x** (feeds compiler grammar + gym validation) |
| Git Archaeology | 2x (tacit knowledge capture) | **5x** (validates spec-mined invariants against reality) |
| Session Gym | 3x (optimized strategies) | **20x** (optimizes the entire combined system) |

The 100x claim isn't hyperbole — it's what happens when you go from "write requirements from scratch every time" to "review and approve a compiler-generated spec in 30 seconds."

---

## Implementation Roadmap (Sketch)

**Phase 0: Decision Event Extraction** (foundation for everything)
- Extend auto-etl to extract structured decision events from sessions
- Schema: `{session_id, decision_type, decision_value, timestamp, source, confidence}`
- Store in `decisions.parquet` alongside messages and sessions
- Use LLM classification on historical data to bootstrap the dataset

**Phase 1: Spec Mining** (Idea 3 — provides the rules)
- Build invariant miner over decisions.parquet
- Start with data invariants (A → B co-occurrence)
- Add temporal invariants (A before B ordering)
- Output: `invariants.json` with confidence scores

**Phase 2: Decision Compiler v0** (Idea 1 — uses the rules)
- Load invariants as inference rules
- Apply to new /new-task inputs
- Output: resolved decisions + remaining questions
- Integrate with process-requirements skill

**Phase 3: Session Gym v0** (Idea 5 — tests the compiler)
- Build session decomposer (ground-truth extraction)
- Implement naive + frequency + compiler strategies
- Run backtests, measure corrections/questions/accuracy
- Establish baseline metrics

**Phase 4: Causal DAG** (Idea 2 — improves ordering)
- Implement PC algorithm over decision co-occurrence data
- Generate DAG, integrate with compiler's question ordering
- Re-run gym, measure improvement

**Phase 5: Git Archaeology** (Idea 4 — validates everything)
- Build session→commit bridge using git_branch + timestamps
- Extract tacit decisions from code patterns
- Compare stated vs enacted, flag drift
- Feed discoveries back into spec miner

**Phase 6: Optimization loop**
- Session gym runs automatically on new session data
- Compiler grammar updates from new invariants
- DAG updates from new causal discoveries
- Git archaeology validates new rules
- The system improves itself with every session

---

## References

- Ellis et al., "DreamCoder: Growing Generalizable, Interpretable Knowledge with Wake-Sleep Bayesian Program Learning" (PLDI 2021) — library learning for the decision compiler
- Spirtes, Glymour & Scheines, "Causation, Prediction, and Search" — PC algorithm for causal DAG discovery
- Ernst et al., "The Daikon System for Dynamic Detection of Likely Invariants" (Science of Computer Programming, 2007) — specification mining methodology
- Lemieux & Beschastnikh, "Texada: Extracting LTL Specifications from Traces" (ASE 2015) — temporal invariant mining
- Settles, "Active Learning Literature Survey" (2009) — query strategy optimization
- OMG Decision Model and Notation (DMN) standard — Decision Requirements Graph formalism
- Lake, Salakhutdinov & Tenenbaum, "Human-level Concept Learning through Probabilistic Program Induction" (Science, 2015) — Bayesian program synthesis

---

## 6. Orthogonal Questioning in Requirement Vector Space

*An information-geometric framework for collapsing the AI's estimate onto the user's true intention with minimal questions.*

### The geometric intuition

Imagine a high-dimensional space where every point represents a possible set of requirements. One point in that space — **u** — is the user's true intention. When a session begins, the AI sits at some other point **a₀** — its initial estimate, built from defaults and task description. The distance ||**u** - **a₀**|| is the "misunderstanding gap."

Each question the AI asks is a **measurement** — it reveals information about **u** along some direction in this space. The user's answer moves the AI's estimate closer to **u** along that direction:

```
a₁ = a₀ + (user's answer projected onto question direction) × v₁
a₂ = a₁ + (user's answer projected onto question direction) × v₂
...
aₖ = aₖ₋₁ + (user's answer projected onto question direction) × vₖ
```

If v₁ and v₂ point in the same direction (correlated questions like "Do you want postgres?" then "Do you want a relational DB?"), the second question is nearly wasted — it moves the estimate along a direction that's already been resolved.

If v₁ and v₂ are **orthogonal** — pointing in completely independent directions — each question maximally reduces the remaining uncertainty. Three orthogonal questions in a 50-dimensional space can collapse more uncertainty than 15 correlated questions.

The goal: **find the smallest set of maximally orthogonal questions that collapses ||u - aₖ|| below the user's acceptance threshold.**

### The mathematical framework

#### Requirement space R^d

Define a d-dimensional vector space where each dimension is a **decision axis**:

```
d₁:  storage_backend     ∈ embed({postgres, sqlite, file, memory, ...})
d₂:  test_strategy       ∈ embed({e2e_real_db, unit, integration, ...})
d₃:  output_format       ∈ embed({json, text, markdown, yaml, ...})
d₄:  cli_structure       ∈ embed({subcommands, flags, interactive, ...})
d₅:  validation_approach ∈ embed({strict_schema, loose, none, ...})
...
d_n:  [undiscovered axis]
```

Each categorical decision value is embedded into a continuous vector using an embedding model. "postgres" and "sqlite" are close in embedding space (both relational storage); "postgres" and "JSON output" are far (orthogonal concerns).

A session's complete requirement set is a point **u** = (u₁, u₂, ..., u_d) in this space.

#### The uncertainty ellipsoid

The AI doesn't have a single point estimate — it has a **distribution** over possible intention vectors. Before asking any questions, this distribution is an ellipsoid in R^d centered on the AI's best guess, with principal axes aligned with the directions of greatest uncertainty.

Formally, model the AI's belief as a multivariate Gaussian:

```
P(u | observations) = N(μ, Σ)
```

Where:
- **μ** is the current best estimate of the user's intention
- **Σ** is the covariance matrix encoding uncertainty along each direction

The ellipsoid defined by Σ is the "uncertainty envelope." A fat ellipsoid means the AI is unsure. A thin ellipsoid means it's nearly certain. The user's acceptance threshold is: "the ellipsoid is small enough that any point inside it would produce acceptable requirements."

#### Questions as covariance reduction

When the AI asks question q with direction vector **v** in R^d, and the user answers, the covariance updates:

```
Σ_new = Σ_old - (Σ_old × v × vᵀ × Σ_old) / (vᵀ × Σ_old × v + σ²_noise)
```

This is the standard Bayesian update for a Gaussian with a linear observation. The key insight: **the reduction in uncertainty depends on the alignment between v (the question direction) and the principal axes of Σ (the directions of maximum uncertainty).**

Asking a question along the largest eigenvector of Σ produces the maximum uncertainty reduction. Asking along a direction where Σ is already small produces almost no reduction.

**Orthogonal questions are optimal because they reduce independent dimensions of Σ in sequence.** After asking along eigenvector e₁, the uncertainty along e₁ is collapsed, and e₂ becomes the direction of maximum remaining uncertainty. The eigenvectors of the covariance matrix naturally define the orthogonal question basis.

#### Optimal experimental design

This is exactly the framework of **optimal experimental design** from statistics. Three criteria for choosing measurements:

- **A-optimality**: minimize tr(Σ) — reduce average uncertainty across all dimensions
- **D-optimality**: minimize det(Σ) — reduce the volume of the uncertainty ellipsoid
- **E-optimality**: minimize λ_max(Σ) — reduce worst-case uncertainty

D-optimality is the most natural fit: each question should maximally shrink the volume of possible intention vectors. The D-optimal question sequence is exactly the eigenvectors of Σ sorted by eigenvalue — the **principal components of uncertainty**.

### The compressed sensing insight: sparsity makes this tractable

Here's why this works dramatically better than asking one question per dimension.

In a 50-dimensional decision space, naively you'd need 50 questions for full certainty. But user intentions are **sparse** relative to their defaults. In any given task, most decisions follow established patterns. Only a handful — maybe 3-8 — deviate from the default.

The user's "true signal" is:

```
u = u_default + δ
```

Where u_default is the learned default vector (from historical sessions) and **δ is sparse** — nonzero in only k << d dimensions.

Compressed sensing theory (Candès, Romberg, Tao 2006) gives a remarkable result: **if δ has sparsity k in dimension d, you can recover it exactly from O(k × log(d/k)) random measurements.** And with well-designed measurements (orthogonal, aligned with the support of δ), you can do even better.

Concrete numbers for a typical task:

```
d = 50    (total decision dimensions)
k = 5     (non-default decisions in this task)

Naive questioning:     50 questions (one per dimension)
Random questioning:    ~17 questions (compressed sensing bound)
Orthogonal PCA:        ~8 questions (aligned with principal components)
Adaptive orthogonal:   ~3-5 questions (adapt direction after each answer)
```

**3-5 questions instead of 50.** That's the promise of orthogonal questioning with sparsity exploitation.

The reason adaptive outperforms static PCA: after the first answer, you learn something about the support of δ (which dimensions are non-default). You can then redirect subsequent questions toward the revealed non-default dimensions. Each answer doesn't just reduce uncertainty — it reshapes the uncertainty ellipsoid, pointing future questions more precisely.

### The requirement manifold: decisions live on a surface, not in free space

There's a deeper geometric structure that makes this even more powerful. Decision dimensions aren't independent — they're constrained. "Use postgres" constrains migration strategy, connection pooling, and ORM choice. "Use Go" constrains framework, build system, and testing tools. The set of valid decision combinations doesn't fill R^d — it lives on a lower-dimensional **manifold** embedded in R^d.

```
Full decision space:       R^50  (50 dimensions)
Valid decision manifold:   M^12  (12 intrinsic degrees of freedom)
Sparse deviations:         ~5 non-default dimensions per task
Effective questions needed: ~3-4 (to locate a point on the manifold)
```

Learning the manifold means learning which decisions are **free** (genuinely independent, need asking) vs **constrained** (determined by other decisions, infer automatically). The manifold dimension m is the true number of independent decisions. That's the actual number of questions you need.

Techniques for manifold learning from data:
- **UMAP/t-SNE** for visualization: embed session decision vectors in 2D, see if they cluster into meaningful regions
- **Diffusion maps** for intrinsic dimension estimation: compute the eigenspectrum of the graph Laplacian over session vectors, the dropoff point estimates manifold dimension
- **Autoencoder bottleneck**: train an autoencoder on session decision vectors, the bottleneck dimension approximates manifold dimension

If the manifold dimension is 12, and the typical task has 5 non-default decisions, and the initial task description resolves 2 of them, you need **3 orthogonal questions**. Three questions to fully specify requirements for a complex task.

### The Gaussian Process formalization

The most elegant formalization wraps all of this into a **Gaussian Process (GP)**:

```
Prior:           P(u) = GP(μ_default, K)
Observation:     y_i = vᵢᵀu + ε    (answer to question i)
Posterior:       P(u | y₁...yₖ) = GP(μ_posterior, K_posterior)
Next question:   argmax_v  acquisition_function(v, μ_posterior, K_posterior)
```

Where:
- **μ_default** is the default intention vector (learned from historical sessions)
- **K** is the kernel function encoding decision correlations (learned from the covariance of historical decision vectors)
- **vᵢ** is the "direction" of question i in requirement space
- **ε** is noise (ambiguity in the user's answer)
- The **acquisition function** is the criterion for picking the most valuable next question

The GP gives you everything for free:

1. **Uncertainty quantification** — the posterior variance at each point tells you exactly how uncertain the AI is about each decision
2. **Principled stopping** — stop when max posterior variance < threshold (the AI is confident enough)
3. **Correlation handling** — the kernel K encodes that "postgres" and "migration strategy" are correlated, so answering one partially resolves the other
4. **Graceful cold start** — with no history, the prior is wide and the system asks many questions; as history accumulates, the prior tightens and fewer questions are needed
5. **Acquisition functions** from Bayesian optimization transfer directly:
   - **Maximum variance** — ask the question where you're most uncertain
   - **Expected improvement** — ask the question that maximally improves spec quality in expectation
   - **Knowledge gradient** — ask the question that maximally reduces the expected number of future questions needed

### Building the training dataset from existing infrastructure

This is the practical path from "interesting math" to "working system."

#### Phase 1: Decision event extraction

Use auto-search to query the three data sources identified in better-questions.md:

```bash
# Explicit decisions (226 structured Q&A pairs)
autosearch search '"User has answered your questions"' --role tool --limit 500

# Implicit decisions (from task descriptions)
autosearch search '"command-name" AND "new-task"' --role user --limit 200

# Mid-session corrections (stated → revised)
autosearch search '"changed my mind" OR "actually" OR "lets change"' --role user --limit 200
```

For each result, use an LLM to extract structured decision tuples:

```json
{
  "session_id": "abc123",
  "decision_type": "storage_backend",
  "decision_value": "postgres",
  "decision_context": "ETL pipeline, needs concurrent writes, long-running jobs",
  "source": "explicit_ask | implicit_stated | mid_correction | tacit_git",
  "supersedes": null,  // or ID of decision this corrected
  "timestamp": "2026-03-15T14:00Z"
}
```

Expected yield: ~500-1000 structured decision tuples from 670+ sessions.

#### Phase 2: Decision type discovery (the dimension axes)

Cluster all extracted decisions by type. Two approaches:

**Bottom-up:** Embed all decision_value strings, cluster with HDBSCAN, label clusters. The clusters define the dimensions of R^d.

**Top-down:** Seed with known categories from the data (storage, testing, CLI surface, output format, validation, scope boundary, parsing approach, data model, architecture). Classify each decision into a category. Add new categories when decisions don't fit existing ones.

The hybrid approach is best: start top-down with known categories, let bottom-up clustering reveal missing ones.

Expected output: ~30-60 decision dimensions.

#### Phase 3: Session decision vectors

For each session, assemble a decision vector:

```python
# Pseudocode
for session in sessions:
    vec = default_vector.copy()  # start with learned defaults
    for decision in session.decisions:
        dim = decision.type_index  # which dimension
        val = embed(decision.value)  # embed the chosen value
        vec[dim] = val
    session_vectors.append(vec)
```

Missing dimensions (decisions not made in this session) keep the default value. This naturally encodes sparsity — most sessions only deviate from defaults in a few dimensions.

#### Phase 4: Covariance estimation and PCA

```python
X = np.stack(session_vectors)  # shape: (670, d)
X_centered = X - X.mean(axis=0)
Sigma = X_centered.T @ X_centered / len(X)  # covariance matrix

eigenvalues, eigenvectors = np.linalg.eigh(Sigma)
# Sort by decreasing eigenvalue
idx = eigenvalues.argsort()[::-1]
eigenvalues = eigenvalues[idx]
eigenvectors = eigenvectors[:, idx]

# How many components capture 90% of variance?
cumvar = np.cumsum(eigenvalues) / eigenvalues.sum()
n_components = np.searchsorted(cumvar, 0.9) + 1
# Expected: 8-15 components for 90% variance
```

The first n_components eigenvectors are the **principal decision axes** — the directions along which user decisions vary most. These define the optimal orthogonal question basis.

#### Phase 5: Eigenvector → natural language question mapping

Each eigenvector v_i is a direction in the embedded decision space. To map it back to a human question:

1. Find the decision dimensions with the largest magnitude in v_i
2. Those dimensions define what the question is "about"
3. Use an LLM to generate a natural language question that discriminates along that direction

Example: if eigenvector v₁ has large components on {storage_backend, schema_approach, migration_strategy, connection_pooling}, the corresponding question is something like: "What's the persistence model? (in-memory / flat files / embedded DB / full postgres)"

This single question resolves uncertainty along an entire correlated bundle of decisions — because on the requirement manifold, these decisions are linked. You don't ask about each one separately.

#### Phase 6: Question-answer training pairs with information gain labels

For each historical Q&A interaction:

```python
for session in sessions:
    u_final = session.final_decision_vector  # ground truth
    a = session.initial_estimate  # from task description

    for qa in session.question_answers:
        v = question_to_direction(qa.question)  # embed question as direction
        info_gain = compute_variance_reduction(Sigma, v)
        orthogonality = min_cosine_sim(v, previous_questions)

        training_pair = {
            "task_embedding": embed(session.task_description),
            "current_uncertainty": current_Sigma,
            "question_direction": v,
            "info_gain": info_gain,         # label: how good was this question?
            "orthogonality": orthogonality,  # label: how independent from prior Qs?
            "residual_after": np.linalg.norm(u_final - a_after_answer)
        }
```

This produces a supervised dataset: given (task, uncertainty state), which question direction produces the best (info_gain × orthogonality) score?

#### Phase 7: Orthogonal question selector

Train a model that, given a task embedding and the current uncertainty state, selects the next question from a library (retrieval) or generates one (generative):

**Retrieval approach** (simpler, more controllable):
- Maintain a library of ~200 canonical questions, each with a known direction vector
- At runtime: compute the current uncertainty eigenvectors, find the library question most aligned with the top eigenvector
- After each answer: re-compute uncertainty, find the next most aligned question (orthogonal to the previous)

**Generative approach** (more flexible, harder to control):
- Fine-tune a model on (task, uncertainty_state) → question
- The model learns to generate questions that are both informative and orthogonal to previous questions
- More powerful but requires more training data and risks generating bad questions

The retrieval approach is the right starting point. The library of canonical questions grows organically from the spec mining work (Idea 3) and can be validated through the session gym (Idea 5).

### Worked example

Walk through a real scenario from the session data.

**Task input:** "add search filtering to autosearch"

**Step 1: Embed task → initial estimate.**

The task embedding activates strong priors from historical sessions:
```
μ₀ = {
  language: Go (0.99),
  framework: Cobra (0.99),
  output_format: JSON (0.95),
  test_strategy: E2E (0.90),
  validation: strict_schema (0.92),
  ...
  filter_model: ??? (0.20),      ← high uncertainty
  match_semantics: ??? (0.30),   ← high uncertainty
  scope_boundary: ??? (0.25),    ← high uncertainty
}
```

Most dimensions are near-certain from defaults. Three dimensions have high variance.

**Step 2: Compute uncertainty eigenvectors.**

The top eigenvector of Σ_posterior points primarily in the (filter_model, match_semantics, scope_boundary) subspace. These three dimensions are correlated on the requirement manifold (knowing the filter model constrains how matching works and what's in scope).

**Step 3: Generate orthogonal question 1.**

The system finds the question most aligned with eigenvector e₁:

> "What's the filtering model? Options: (a) Single field, exact match — pass one value, get rows that match. (b) Multi-field with combinators — AND/OR across fields. (c) Full-text search with ranking — BM25-style relevance scoring."

**Step 4: User answers (a): single field, exact match.**

The answer collapses the filter_model dimension AND partially resolves match_semantics (exact match → case-insensitive normalization likely) AND scope_boundary (single field → one filter at a time). Three dimensions collapsed with one question because they're correlated on the manifold.

**Step 5: Recompute uncertainty.**

Remaining uncertainty is now dominated by:
```
  searchable_fields: ??? (0.35),   ← which fields can be filtered
  error_behavior: ??? (0.40),      ← what happens on invalid input
```

**Step 6: Generate orthogonal question 2.**

> "Which fields should be searchable, and what should happen for invalid filter values? (This determines the filter flag set and error UX.)"

This is a compound question — justified because the two remaining uncertain dimensions are nearly orthogonal to each other (field choice doesn't constrain error behavior), so asking both in parallel is more efficient than sequential.

**Step 7: User answers. Session proceeds.**

Total questions asked: **2**. Decisions resolved: **~15** (8 by learned defaults, 5 by manifold inference from question 1, 2 by explicit answers). Compare to the naive approach of asking 15 separate questions.

### How this connects to Ideas 1-5

The orthogonal questioning framework isn't a sixth idea — it's the **mathematical spine** that unifies the previous five:

- **Decision Compiler (1)** performs type inference → this is the GP posterior update, resolving correlated dimensions from a single observation
- **Causal DAG (2)** encodes decision dependencies → these are the correlations in the kernel K that make manifold inference possible
- **Spec Mining (3)** discovers invariants → these define the constraint equations of the requirement manifold M
- **Git Archaeology (4)** provides ground-truth enacted decisions → these are the training labels for the GP prior
- **Session Gym (5)** backtests question strategies → this evaluates acquisition functions against historical data to find the optimal policy

The GP framework provides the unifying mathematics. The acquisition function IS the question-selection policy. The kernel IS the decision dependency structure. The posterior IS the compiler's inference state. The prior IS the learned default vector.

```
                        ┌─────────────────────────┐
                        │    Gaussian Process      │
                        │    P(u | y₁...yₖ)        │
                        └────┬──────────┬──────────┘
                             │          │
          ┌──────────────────▼──┐   ┌───▼──────────────────┐
          │ Prior μ₀, K         │   │ Acquisition function  │
          │ (from history +     │   │ (orthogonal question  │
          │  spec mining +      │   │  selection policy,    │
          │  git archaeology)   │   │  optimized by gym)    │
          └─────────────────────┘   └───────────────────────┘
                      ▲                         ▲
                      │                         │
          ┌───────────┴─────────┐   ┌───────────┴───────────┐
          │ Kernel structure K   │   │ Session Gym            │
          │ (from causal DAG +   │   │ (backtests acquisition │
          │  manifold learning)  │   │  functions on history) │
          └─────────────────────┘   └───────────────────────┘
```

### The cold-start to steady-state progression

The beauty of this framework is graceful degradation. It works at every data scale:

**0 sessions (cold start):** Prior is maximally uncertain (spherical covariance). Every direction has equal uncertainty. System falls back to asking heuristic questions ordered by expected impact (architecture > details). Essentially the current behavior, but principled.

**10 sessions:** Prior begins to form. Default vector μ₀ has moderate confidence on major dimensions (language, framework). Covariance shows basic correlations (Go ↔ Cobra). System asks ~8-10 questions per task.

**50 sessions:** Manifold structure emerges. PCA reveals 10-15 principal components capturing 90% of variance. Orthogonal questions aligned with eigenvectors start to work. System asks ~5-6 questions per task.

**200 sessions:** Strong prior, well-calibrated covariance. Sparse deviations from defaults are efficiently detected by adaptive questioning. Manifold dimension is robustly estimated. System asks ~3-4 questions per task.

**670+ sessions (current):** Near-optimal question selection. The GP posterior after embedding the task description alone resolves 80%+ of decisions. 2-3 orthogonal questions handle the rest. The session gym has optimized the acquisition function against hundreds of backtests.

This is the progression from "ask 15 questions" to "ask 2 questions and get a better result."

### Key metrics to track

- **Effective dimensionality**: how many principal components capture 90% of decision variance? This is the manifold dimension — the theoretical minimum number of questions needed.
- **Default accuracy**: what fraction of decisions does μ₀ get right without asking? This measures prior quality.
- **Question orthogonality score**: average pairwise cosine similarity between question direction vectors in a session. Lower = more orthogonal = more efficient.
- **Collapse rate**: how much does ||Σ|| decrease per question asked? This measures question quality.
- **Convergence speed**: how many questions until ||u - μ_posterior|| < threshold? This is the headline metric.

### Additional references

- Candès, Romberg & Tao, "Robust Uncertainty Principles: Exact Signal Reconstruction from Highly Incomplete Frequency Information" (IEEE Trans. IT, 2006) — compressed sensing theory
- Rasmussen & Williams, "Gaussian Processes for Machine Learning" (MIT Press, 2006) — GP framework
- Chaloner & Verdinelli, "Bayesian Experimental Design: A Review" (Statistical Science, 1995) — optimal experimental design
- Srinivas et al., "Gaussian Process Optimization in the Bandit Setting: No Regret and Experimental Design" (ICML 2010) — GP-UCB acquisition function
- Tenenbaum, de Silva & Langford, "A Global Geometric Framework for Nonlinear Dimensionality Reduction" (Science, 2000) — manifold learning (Isomap)

---

## 7. Active Blind Spot Hunting and Preference Elicitation

*Using the model's own uncertainty to go looking for missing information — even when there's no task on the table.*

### The problem with passive learning

Everything described so far is **passive** — the system learns from decisions that happen to arise during real tasks. But real tasks are biased toward certain regions of decision space. If you mostly build Go CLI tools, the system gets excellent at Go CLI decisions and learns nothing about, say, how you'd approach a React frontend or a data science pipeline. The prior gets strong in well-trodden territory and stays maximally uncertain everywhere else.

Worse: the blind spots are invisible during normal operation. The system never encounters them, so it never knows they exist. When a task finally does land in a blind spot — say you need to build a web service for the first time — the model reverts to cold-start behavior. All the accumulated intelligence is useless because it's concentrated in the wrong region.

Passive learning converges to the **distribution of past tasks**, not to **complete coverage of the user's preference space**. These are different things.

### The insight: uncertainty-driven exploration outside task context

The covariance matrix Σ doesn't just tell you what to ask during a task. It tells you **where the model is blind, period**. The eigenvectors with the largest eigenvalues after incorporating all historical data point at the regions of preference space that have never been adequately probed.

This means you can actively go hunting for blind spots even when there's no task. Present the user with carefully designed scenarios — not real tasks, but thought experiments calibrated to elicit decisions along high-uncertainty directions. Each answered scenario shrinks the uncertainty ellipsoid in a region that normal work might never reach.

It's the difference between a student who only learns by doing homework (passive) and one who also does practice exams covering the full syllabus (active). The practice exams don't produce deliverables, but they produce **knowledge coverage**.

### Three types of blind spots

Not all high-uncertainty directions are equally valuable to probe. The model needs to distinguish between:

**Type A: Latent preferences (high value).** The user HAS a strong opinion, but no task has ever forced them to express it. Example: they've never built anything requiring authentication, but they have a clear preference for token-based auth over session cookies from their previous work. This preference exists in their head — it's just never been measured.

Detection signal: the decision dimension appears in the **task descriptions** or **mid-session discussions** of neighboring sessions (mentioned tangentially, considered but not chosen, part of the broader domain) but never as a direct decision. It's in the penumbra of the explored space.

**Type B: Genuine deliberation zones (medium value).** The user genuinely doesn't have a fixed preference — they decide case-by-case based on context. Example: "use postgres vs SQLite" might be 60/40 depending on the project's concurrency needs. Probing this doesn't resolve to a default, but it reveals the **decision function** — the contextual factors that determine the choice.

Detection signal: the decision dimension appears in historical sessions with high variance AND the variance doesn't decrease with more observations. The distribution isn't converging.

**Type C: Truly irrelevant dimensions (low value).** The user will never encounter this decision in their work. Example: if they only build backend tools, their opinion on CSS frameworks has zero value.

Detection signal: the dimension never appears in any session — not even tangentially. It has no embedding proximity to the user's task history.

The blind spot hunter should prioritize Type A (latent preferences — high-value, one-shot resolution), deprioritize Type B (deliberation zones — useful for learning the decision function but won't produce clean defaults), and skip Type C entirely.

**How to classify:** Embed each high-uncertainty dimension alongside the user's historical task descriptions. Compute the average cosine similarity between the uncertain dimension and the task embedding cluster. High similarity = Type A (related to their work but unprobed). Medium similarity with high variance = Type B. Low similarity = Type C.

### Phantom tasks: synthetic scenarios for targeted elicitation

The most powerful elicitation tool isn't direct questions — it's **scenarios**. People are better at making decisions in context than in the abstract. "Do you prefer token auth or session auth?" is abstract and hard to answer. "You're building an API that partners will call from their servers. How do you handle authentication?" is concrete and easy.

**Phantom task design principles:**

1. **Minimal viable context.** Include just enough scenario detail to force the target decision. Don't add complexity along already-resolved dimensions — that's noise.

2. **One principal direction per phantom.** Each phantom task should be designed to elicit decisions primarily along one eigenvector. If the eigenvector spans {auth_model, session_management, token_format}, the phantom task should naturally involve all three (they're correlated on the manifold), but should NOT require decisions about storage backend or CLI surface.

3. **Plausible for the user's domain.** The scenario should feel like something the user *might* actually build. This keeps answers grounded in real engineering judgment rather than hypothetical speculation.

4. **Graduated stakes.** Start with low-stakes scenarios where the "right" answer is unclear (probes genuine preferences), then escalate to scenarios where the user likely has a strong opinion (probes latent preferences).

**Generation pipeline:**

```
High-uncertainty eigenvector e_i
  → map to decision dimensions D₁, D₂, D₃ (largest components of e_i)
  → retrieve user's typical project context (Go CLI tools, data pipelines, etc.)
  → LLM prompt:

    "Generate a minimal, realistic scenario for a developer who builds
    {user_context} that naturally requires them to make decisions about
    {D₁}, {D₂}, and {D₃}. The scenario should be 2-3 sentences.
    Don't mention the decisions explicitly — let them emerge from the
    scenario. Provide 3-4 concrete options for the user to choose from."

  → validate: does the scenario actually force the target decisions?
  → present to user
```

**Example phantom tasks targeting different blind spots:**

*Blind spot: error recovery strategy (never explicitly decided)*
> "Scenario: Your ETL pipeline is processing 10,000 session files. File #4,732 has corrupted JSON. What should happen?"
> - (a) Skip it, log a warning, continue processing the remaining 5,268 files
> - (b) Fail immediately, report the error, let the user re-run after fixing
> - (c) Quarantine the bad file to a separate directory, continue, report summary at end
> - (d) Attempt auto-repair (re-download from source), fail if that doesn't work

*Blind spot: concurrency model (always single-threaded so far)*
> "Scenario: You need to fetch metadata from 500 git repos across 3 different hosts. Sequential fetching takes 45 minutes. How do you parallelize?"
> - (a) Worker pool with fixed concurrency (e.g., 10 workers)
> - (b) Fan-out with semaphore, one goroutine per repo, bounded by semaphore
> - (c) Rate-limited sequential with connection reuse — 45 min is fine
> - (d) Batch by host, parallel across hosts, sequential within each host

*Blind spot: API versioning (never built a versioned API)*
> "Scenario: You've shipped v1 of a REST API that other tools in the stack consume. You need to make a breaking change to the response schema. How do you handle it?"
> - (a) URL versioning: /v1/sessions, /v2/sessions
> - (b) Header versioning: Accept: application/vnd.auto.v2+json
> - (c) Additive-only changes — never break, only add new fields
> - (d) Deprecation window: ship v2 alongside v1, remove v1 after 3 months

### The decision quiz: a periodic calibration ritual

Package the phantom tasks into a structured calibration experience:

**`/calibrate` — a new skill**

```
$ claude /calibrate

Decision Model Calibration
━━━━━━━━━━━━━━━━━━━━━━━━━━

Your decision model has 47 dimensions with resolved preferences.
I've identified 8 blind spots worth probing.
Estimated time: 5-7 minutes (8-12 scenarios).

Ready? (y/n)

━━━━━━━━━━━━━━━━━━━━━━━━━━
Scenario 1 of 8
Category: error recovery • Confidence: 12%

Your ETL pipeline is processing 10,000 session files.
File #4,732 has corrupted JSON. What should happen?

  1. Skip, log warning, continue
  2. Fail immediately
  3. Quarantine bad file, continue, report summary
  4. Attempt auto-repair, fail if that doesn't work

> 3

Got it. Updated: error_recovery → quarantine_and_continue (confidence: 70%)
This also partially resolves: partial_failure_mode, logging_verbosity

━━━━━━━━━━━━━━━━━━━━━━━━━━
Scenario 2 of 8 (adapted based on your previous answer)
...
```

Key features:

- **Adaptive sequencing.** Each answer reshapes the remaining uncertainty. The next scenario targets the new top eigenvector, which may have shifted based on what was just learned. The quiz adapts in real time.

- **Manifold cascade.** When an answer resolves a point on the manifold, correlated dimensions partially resolve too. The quiz recognizes this and skips scenarios that are now redundant: "Your answer to scenario 1 also resolved my uncertainty about logging verbosity. Skipping that scenario."

- **Confidence reporting.** After each answer, show what was learned and what was inferred. Transparency builds trust and lets the user correct bad inferences immediately.

- **Contradiction detection.** If an answer contradicts a previously held preference, flag it: "Interesting — in past tasks you've consistently used fail-fast validation, but here you chose quarantine-and-continue. Is this context-dependent (fail-fast for input validation, quarantine for data processing)? Or have your preferences changed?"

- **Session-end summary.** After the quiz, produce a structured delta:

```
Calibration complete. Updated 6 decision dimensions:

  error_recovery:    ??? → quarantine_and_continue (new)
  concurrency_model: ??? → worker_pool_bounded (new)
  api_versioning:    ??? → additive_only (new)
  logging_approach:  verbose → structured_json (updated, was verbose)
  retry_strategy:    ??? → exponential_backoff (inferred from scenario 1+2)
  partial_failure:   ??? → continue_and_report (inferred from scenario 1)

Remaining blind spots: 2
  - deployment_model (no plausible scenario for your domain)
  - team_workflow (need more context about your collaboration patterns)

Model coverage: 47/55 dimensions → 53/55 dimensions
Estimated questions saved in next 10 tasks: ~12
```

### Decision arbitrage: exploiting contradictions

The richest signal isn't in the blind spots — it's in the **contradictions**. When the model holds two conflicting data points for the same dimension, there's a hidden variable it hasn't modeled: the **context** that determines which preference applies.

**Detecting contradictions:**

```python
for dim in decision_dimensions:
    values = all_historical_values(dim)
    if len(set(values)) > 1 and entropy(values) > threshold:
        # This dimension has inconsistent answers
        contexts = [session_context(v) for v in values]
        # Find the contextual feature that predicts the split
        splitting_feature = find_best_split(values, contexts)
        # Generate a scenario that disambiguates
        phantom = generate_disambiguation_scenario(dim, splitting_feature)
```

**Example from real data:**

The user has chosen both "fail-fast validation" and "return partial results with errors" across different sessions. The model is confused — which is it?

The contradiction is resolved by context: **fail-fast for CLI input validation** (bad flags, invalid arguments) vs **partial results for data operations** (list commands, search results). The splitting variable is `operation_type ∈ {input_validation, data_retrieval}`.

A disambiguation phantom task:

> "Two quick scenarios — I want to make sure I understand when you prefer strict vs lenient behavior:"
>
> "(A) User runs `autosearch search --since invalid-date`. Should the tool...?"
> - Fail immediately with error
> - Default to 'all time' and warn
>
> "(B) `autosearch sessions` finds 100 sessions but 3 have corrupted metadata. Should the tool...?"
> - Show all 97 valid sessions, report 3 errors on stderr
> - Fail because the dataset is inconsistent

The pair of answers reveals the decision function: `if input_validation then fail_fast else partial_results`. This is worth more than either individual data point because it encodes the **rule**, not just the **instances**.

**Contradiction resolution creates conditional rules:**

```
BEFORE:  error_strategy = ??? (conflicting signals, confidence 0.3)

AFTER:   error_strategy =
           if operation_type == input_validation: fail_fast (confidence 0.95)
           if operation_type == data_retrieval: partial_results (confidence 0.90)
           else: ??? (confidence 0.3)
```

The decision model upgrades from a flat value to a **decision tree** for that dimension. The manifold gains structure — what was one dimension becomes a conditional branch.

### Frontier expansion: discovering unknown dimensions

The most ambitious version of blind spot hunting goes beyond filling known dimensions — it discovers dimensions the model doesn't even have yet. These are the "unknown unknowns" — decision categories that have never appeared in any session.

**How dimensions get discovered today:** A new task introduces a decision that doesn't fit any existing category. The system encounters it for the first time and has to handle it cold. The dimension is added retroactively.

**How frontier expansion would work:** Generate phantom tasks that are **adjacent** to the user's experience but in unexplored territory. Not random — strategically positioned just beyond the frontier of known decisions.

```
User's task history embedding cluster
  → find the convex hull boundary
  → sample points just outside the boundary
  → generate phantom tasks at those points
  → user's answers either:
      (a) map to existing dimensions (frontier extends outward)
      (b) require new dimensions (frontier extends into new axes)
```

Case (b) is the gold: the phantom task reveals a decision category the system didn't know existed.

Example: The user has only ever built local CLI tools. A phantom task at the frontier:

> "Scenario: A teammate wants to use your autosearch tool from their machine. They don't have the parquet data locally. How do you make it available?"
> - (a) HTTP API wrapper — they query over the network
> - (b) Shared filesystem — mount the data directory
> - (c) Export + sync — periodically push data to a shared location
> - (d) Don't — this tool is local-only by design

This might reveal entirely new decision dimensions: {deployment_topology, data_sharing_model, network_protocol, auth_model_for_internal_tools}. None of these existed in the model. The phantom task conjured them into existence.

The LLM generating these frontier phantoms should be prompted with:

```
"The user primarily builds {user_domain_summary}. Generate a plausible
scenario that is ADJACENT to their experience — something they haven't
built but could realistically need to. The scenario should require
decisions that don't fit these existing categories: {known_dimensions}.
The goal is to discover NEW types of decisions, not just new values
for known types."
```

### The economics: when is blind spot hunting worth it?

Every phantom task costs user attention. Is it worth it?

**Model the value of a probed blind spot:**

```
V(probe) = P(dimension_relevant_in_future)
         × expected_questions_saved_per_task
         × expected_tasks_in_future
         × user_time_per_question
```

If a probed dimension saves 1 question per task, and the user does 50 more tasks where it's relevant, and each question costs ~30 seconds of attention, that's 25 minutes saved. The phantom task cost ~30 seconds. ROI: 50x.

But if the dimension is irrelevant (Type C), the probe costs 30 seconds and saves nothing. ROI: 0.

The system should estimate P(dimension_relevant_in_future) before probing. Use the embedding proximity metric from the blind spot classification: dimensions close to the user's task cluster are likely relevant. Dimensions far away are not.

**Optimal calibration frequency:**

After each calibration session, the marginal value of the next probe decreases (you're probing increasingly unlikely dimensions). The optimal frequency is: **calibrate when the expected value of the top unprobed blind spot exceeds the cost of a probe.**

In practice: a full calibration (~10 scenarios) after a significant change in work patterns (new project, new domain, new team), and micro-calibrations (1-2 targeted phantoms) when the model detects a real-task decision it's uncertain about.

The `/calibrate` skill could even self-trigger: "I've noticed 3 recent tasks touched decision areas I'm uncertain about. Want to do a 2-minute calibration to fill those gaps?"

### How this feeds back into the GP framework

The blind spot hunting is mathematically just another source of observations for the Gaussian Process:

```
Standard task observations:    y_task = v_taskᵀu + ε
Phantom task observations:     y_phantom = v_phantomᵀu + ε
Contradiction resolution:      y_conditional = v_condᵀu | context + ε
```

The GP doesn't care whether the observation came from a real task or a phantom — it's all evidence about u. But the phantom observations are **designed** to maximally reduce the posterior variance, rather than being dictated by whatever task happens to arise.

This is the distinction between **observational** and **experimental** data in causal inference. Observational data (real tasks) is biased toward the historical distribution. Experimental data (phantom tasks) can be designed to cover the full space.

The combined system:

```
Real tasks (passive learning)
  → observations along directions determined by task requirements
  → biased toward well-trodden regions
  → free (no extra user cost)

Phantom tasks (active elicitation)
  → observations along directions of maximum remaining uncertainty
  → targeted at blind spots
  → costs user attention, but high ROI

Contradiction probes (decision function learning)
  → observations that disambiguate conditional preferences
  → reveals the structure of context-dependent decisions
  → highest information density per probe
```

The three sources together produce complete, well-calibrated coverage of the user's decision space — something that passive learning alone would take years (or never) to achieve.

### Additional references

- Brochu, Cora & de Freitas, "A Tutorial on Bayesian Optimization of Expensive Cost Functions" (2010) — acquisition functions for experiment design
- Houlsby et al., "Bayesian Active Learning for Classification and Preference Learning" (2011) — BALD acquisition function
- Chu & Ghahramani, "Preference Learning with Gaussian Processes" (ICML 2005) — GP-based preference elicitation
- Sadigh et al., "Active Preference-Based Learning of Reward Functions" (RSS 2017) — interactive preference elicitation for sequential decisions
- Christiano et al., "Deep Reinforcement Learning from Human Preferences" (NeurIPS 2017) — RLHF preference learning framework
