codex

# Better Questions: Five Deep Extensions

Date: 2026-05-26

## Executive Thesis

`docs/better-questions.md` is directionally right, but the next leap is not simply "mine decisions and ask fewer questions." That is the obvious path.

The deeper opportunity is to turn the entire agent workflow into a measurable, self-improving decision system:

- Questions should be selected by expected downstream damage, not by how often they appeared historically.
- Past corrections should become regret signals with a cost model, not just examples.
- Successful sessions should compile into reusable workflow programs, not just guidance docs.
- Learned preferences should become executable contracts that gate plans, PRs, hooks, and CI.
- We should be able to test new agent workflows overnight against historical tasks before trusting them in live work.

The 10x path is reducing requirement-writing effort. The 100x path is building a workflow wind tunnel where we can run hundreds of experiments on agent behavior while humans sleep.

## Calibration From Session History

I read `docs/better-questions.md`, `docs/requirements-mining.md`, `docs/user-journey.md`, `docs/doc-file-usage-findings.md`, and prior workflow analysis docs, then used `autosearch` to sample user decisions, corrections, and recurring constraints.

The main thing I inferred about your current knowledge level: you already understand the standard memory/defaults idea. You have repeatedly pushed the system toward agent-readable docs, skill-triggering, session mining, self-improvement, structural rather than tactical findings, and explicit workflow automation. So suggestions like "store answers in YAML" or "build a checklist of common questions" are below the bar.

Evidence from history:

- `autosearch search '"User has answered your questions"' --role tool` reported structured AskUserQuestion answers, including a prior count of 226 answers across 55 sessions.
- `autosearch search '"command-name" AND "new-task"' --role user` found dense task prompts where implicit decisions are already embedded.
- `autosearch search '"changed my mind" OR "actually" OR "we should" OR "lets change"' --role user` found high-value correction signals, including storage-model changes and schema decisions.
- `autosearch message get b6027a31-9f81-415a-97b1-1ba771d745db-2` shows you already identified hidden solution assumptions as a core failure mode.
- `autosearch message get 1fb24631-a49e-479e-8cb4-b0b74bd22ef5-154` shows you explicitly prefer structural gaps and recurrent workflow problems over local nitpicks.
- `autosearch message get 95bf7daa-d872-4c7d-b371-b2c99515c754-28` shows your long-term direction for `auto-graph`: a graph-based context engine spanning files, docs, commits, scripts, and agent task context.
- `autosearch message get 88943e4d-fa33-444d-9c32-4fe269718550-53` shows a stable preference: preserve original data immutably and add derived fields rather than rewriting source content.

External research anchors that matter here:

- SWE-agent argues that agent-computer interface design materially changes coding-agent performance, which supports investing in agent-native workflow surfaces rather than just better prompts: https://arxiv.org/abs/2405.15793
- DSPy shows that LM pipelines can be expressed as programs and optimized from examples/metrics, which supports treating workflows as learnable programs rather than hand-written prompts: https://arxiv.org/abs/2310.03714
- Reflexion and Voyager both point at the same pattern: feedback plus memory/skill libraries can improve future agent behavior without retraining base models: https://arxiv.org/abs/2303.11366 and https://arxiv.org/abs/2305.16291
- Claude Code hooks can inject context on prompt/session events, block tool calls, run agent hooks, and run async checks, which means many of these ideas can be built as thin orchestration layers around existing tools: https://code.claude.com/docs/en/hooks
- OpenAI Agents SDK tracing and trace grading show a mature direction for evaluating whole agent traces rather than just final text outputs: https://openai.github.io/openai-agents-python/tracing/ and https://developers.openai.com/api/docs/guides/trace-grading

## 1. Decision Frontier Compiler

### What

Before asking the user anything, the system generates a decision frontier: the smallest set of unanswered questions whose different answers would materially change the implementation plan.

This is not a smarter FAQ. It is a counterfactual compiler for requirements.

### Why This Is Non-Obvious

`better-questions.md` currently implies: ask questions the user has answered before, default when confidence is high, promote stable answers to standards.

That misses the real issue: confidence is not the same as importance.

A decision can be rare but catastrophic if guessed wrong. A decision can be historically contested but irrelevant to the implementation. The system should ask questions based on expected plan divergence, not just historical answer frequency.

The surprising version is: generate multiple plausible futures, diff them, then ask only about the branches that diverge enough to matter.

### Example

User says:

> add filtering to autosearch stats

The current approach might ask:

- Should output default to JSON?
- Should matching be case-insensitive?
- Should invalid filters fail fast?
- Should filters combine?

A Decision Frontier Compiler instead simulates variants:

- one-filter-only vs composable filters
- message-scope only vs session/message parity
- pre-FTS filtering vs post-FTS filtering
- strict schema validation vs permissive normalization

Then it detects which alternatives change files, tests, API shape, and migration risk. It might ask only one question:

> This changes the query planner if filters are composable. Historical convention says one filter mode at a time, but stats/search parity may justify an exception. Should this command allow combined filters, or stay one-filter-only?

Everything else becomes a default with evidence.

### How To Build It

Add a new command, probably under `auto-reflect` or a new `auto-question` package:

```bash
autoquestion frontier --cwd . --prompt "add filtering to autosearch stats" --json
```

Pipeline:

1. Retrieve similar historical tasks via `autosearch`, git commits, PR bodies, and task docs.
2. Extract candidate decision variables from those examples: storage, CLI shape, validation, data model, testing, migration, docs, concurrency, security.
3. Generate 2-4 compact alternative plans per high-uncertainty variable.
4. Diff the plans structurally: files touched, interfaces changed, tests required, data migration risk, reversibility, user-visible behavior.
5. Score each variable by blast radius and confidence.
6. Output a maximum of 1-4 questions, each with default, evidence, and what changes if answered differently.

The output should look like:

```json
{
  "assumptions": [
    {
      "decision": "output_format",
      "default": "json",
      "confidence": 0.96,
      "blast_radius": "low",
      "evidence": ["AGENTS.md", "12 prior CLI tasks"]
    }
  ],
  "questions": [
    {
      "question": "Should stats filters compose, or should only one filter mode be allowed?",
      "why_ask": "Different answers change query API, validation, and test matrix.",
      "default_if_no_answer": "one_filter_mode",
      "blast_radius": "high"
    }
  ]
}
```

### Why It Could 10x Workflow Speed

The user stops reviewing a generic question checklist and starts reviewing the actual architectural fork points. Most requirement sessions shrink to "here are the two places where your answer changes the implementation; everything else is defaulted."

### Why It Could 100x Workflow Development

Once decision frontiers are machine-generated, they become benchmarkable. We can ask: did this frontier predict the questions that later caused corrections? If not, the frontier generator improves. The question-asking system becomes experimentally optimizable.

### Complexity Cost

Medium-hard, but feasible in slices. The first version can use LLM-generated plan variants and simple textual diffs. Later versions can use `auto-graph` to estimate file/API blast radius more formally.

### First Thin Slice

Implement `autoquestion frontier` for one domain: Go CLI changes in auto-stack. Feed it 20 historical task prompts and compare its top questions to actual AskUserQuestion/correction events.

## 2. Regret Ledger And Question ROI

### What

Mine every correction, mind-change, hidden assumption, undo request, and failed implementation branch into a regret ledger. Use it to prioritize future questions by avoided rework cost.

This turns session pain into an economic model.

### Why This Is Non-Obvious

The obvious system learns from answers. The better system learns from regret.

Explicit AskUserQuestion answers are clean but biased: they only cover questions the agent knew to ask. The highest-value data is often where the agent did not ask, guessed wrong, and the user had to redirect.

Examples from history:

- `actully changed my mind again, this feels kinda anti-sql patterns...` changed session identity modeling.
- `I think I've changed my mind... store these scheduler jobs in the database rather than storing them on disk...` reversed a storage decision.
- `one of the most common class of errors is the AI making solution assumptions that aren't explicitly declared...` identifies hidden assumptions as a systemic failure mode.
- `no didnt ask you to start yet. undo that change` marks an autonomy-boundary miss.

These are more valuable than ordinary Q&A pairs because they expose what went wrong.

### How To Build It

Add regret extraction to `auto-reflect`:

```bash
autoreflect regrets mine --cwd . --since 90d --json > .auto/reflect/regrets.jsonl
```

Each regret event should include:

```json
{
  "id": "regret_...",
  "session_id": "...",
  "trigger_message_id": "...",
  "category": "wrong_storage_backend",
  "implicit_question": "Where should scheduler jobs be stored?",
  "wrong_default": "files_on_disk",
  "corrected_answer": "postgres_table",
  "cost": {
    "tokens_before_correction": 184000,
    "elapsed_ms_before_correction": 1230000,
    "files_touched_before_correction": 6,
    "tests_rerun": 3
  },
  "scope": "project:gtm-langchain-demo",
  "generalizable": true,
  "future_prompt": "For persistent user-visible jobs, ask storage backend before designing tools."
}
```

Then add:

```bash
autoquestion roi --prompt "..."
```

It should rank possible questions by:

- probability of wrong default
- expected cost if wrong
- cost of asking now
- reversibility of the decision
- scope confidence: global, repo-specific, subproject-specific, user-specific

### Why It Could 10x Workflow Speed

The agent asks fewer low-value questions and catches the questions that historically cause rewrites. It stops treating all uncertainty equally.

### Why It Could 100x Workflow Development

This gives us an objective metric for workflow improvement: avoided regret. We can compare two planning systems by replaying old tasks and asking which one would have surfaced the regret-causing question earlier.

This is the bridge from vibes to engineering.

### Complexity Cost

Medium. The hard part is not extraction; it is estimating cost and deciding generalizability. But even rough cost signals like tokens-before-correction, files-touched-before-correction, and elapsed-time-before-correction are enough to rank questions better than frequency alone.

### First Thin Slice

Mine only user messages matching correction language:

```bash
autosearch search '"changed my mind" OR "undo" OR "wrong" OR "not what i want" OR "actually"' --role user --cwd /home/vscode/src/auto-stack --limit 200
```

For each hit, pull previous/next messages and ask an LLM to emit: implicit question, wrong assumption, corrected answer, estimated regret category.

## 3. Workflow Wind Tunnel

### What

Build an offline replay harness that runs proposed workflow changes against historical tasks before deploying them live.

This is the biggest leverage idea in the set.

Instead of asking "will this new skill/prompt/hook make agents better?", we replay 20-100 past tasks in isolated worktrees and measure whether the new workflow reaches the accepted outcome faster, with fewer corrections, fewer failed commands, and fewer hidden assumptions.

### Why This Is Non-Obvious

Most agent workflow development is currently artisanal. We tweak CLAUDE.md, skills, prompts, hooks, or commands, then wait to see if future sessions feel better.

But auto-stack already has the raw material for a benchmark suite:

- initial user prompts
- task docs
- session transcripts
- tool traces
- git commits
- PRs
- tests/build results
- user corrections
- accepted final state

That means we can build a wind tunnel for agent behavior.

The surprising move is to treat old development sessions as regression tests for new workflow systems.

### How To Build It

Add `auto-eval` as the workflow experiment harness.

A replay pack contains:

```json
{
  "task_id": "auto-stack-2026-05-graph-doclinks",
  "initial_prompt_message_id": "...",
  "start_commit": "...",
  "accepted_commit": "...",
  "workspace": "/home/vscode/src/auto-stack",
  "required_docs": ["docs/tasks/.../requirements.md"],
  "success_checks": [
    "go test ./... in auto-graph",
    "expected files changed",
    "no hidden open questions"
  ],
  "known_regrets": ["wrong_build_directory", "missed_doc_link_tests"]
}
```

Then run:

```bash
autoeval replay --pack packs/auto-stack-cli-filtering.json --workflow current
autoeval replay --pack packs/auto-stack-cli-filtering.json --workflow question-frontier-v1
autoeval compare current question-frontier-v1 --metric regret_avoided
```

Metrics:

- elapsed time
- token count
- command failure count
- number of user/correction-equivalent interventions required
- test pass/fail
- diff similarity to accepted solution
- hidden assumptions introduced
- decision-frontier recall: did it ask/default the historically important decisions?

This can run overnight across dozens of small tasks. The output is a ranked report of which workflow changes actually improved outcomes.

### Why It Could 10x Workflow Speed

You stop manually evaluating workflow changes from anecdotes. Every improvement to planning, question asking, skill invocation, doc routing, and hooks can be tested quickly against real past work.

### Why It Could 100x Workflow Development

This changes the development loop from:

> invent workflow tweak -> wait days for real sessions -> manually judge vibes

Into:

> invent workflow tweak -> run 50 historical replays overnight -> merge only if metrics improve

That is how workflow engineering becomes a real discipline.

### Complexity Cost

Hard, but the first version can be small. We do not need perfect autonomous replay at first. We can start with read-only plan replay: feed the initial prompt plus repo context to different planning/question systems and score whether they recover the decisions that were eventually needed.

### First Thin Slice

Create 10 replay packs for planning-only evaluation:

- input: first user prompt and relevant docs
- target: accepted requirements/solution/plan plus known corrections
- evaluated output: questions asked, assumptions made, docs selected, validation plan

No code execution required in v1.

### External Anchor

OpenAI trace grading is directly relevant here because it evaluates traces, not just final answers. SWE-agent is relevant because it shows interface design materially affects coding-agent performance.

## 4. Workflow Genome Compiler

### What

Mine successful sessions into reusable workflow genomes: compact phase graphs that describe how a class of work actually gets done in this repo. Then compile those genomes into OpenProse programs, skills, hooks, and task templates.

This moves beyond decision memory into procedure memory.

### Why This Is Non-Obvious

`better-questions.md` focuses on decisions. But many productivity losses are not missing answers; they are missing choreography.

The difference:

- Decision memory says: "CLI output defaults to JSON."
- Workflow genome says: "For Go CLI feature work in auto-stack: read subproject CLAUDE.md -> inspect command patterns -> update parser/cli/tests -> build in nearest module after each Go edit -> add e2e fixture if behavior crosses CLI boundary -> run autodoc fix -> contextual commit."

That second object is much more powerful. It can drive subagents, hooks, tests, and reviews.

### How To Build It

Use session traces plus git history to infer phase graphs.

A genome might look like:

```yaml
id: go-cli-feature-auto-stack
applies_when:
  - repo: auto-stack
  - task_mentions: [cli, command, flag, output, filter]
phases:
  - name: orient
    reads:
      - subproject/CLAUDE.md
      - existing command implementation
      - existing tests
    outputs:
      - file map
  - name: design-question-frontier
    checks:
      - output format
      - flag conflicts
      - validation shape
      - filter semantics
  - name: implement-incrementally
    invariant: run go build ./... in relevant module after Go edits
  - name: validate
    commands:
      - go test ./...
      - CLI smoke tests where applicable
  - name: docs-and-commit
    commands:
      - autodoc fix
      - contextual commit
failure_recovery:
  - if go root has no go.mod, locate nearest module
  - if precommit fails, gofmt staged files and retry
```

Add commands:

```bash
autoworkflow mine --cwd . --class "go cli feature" --since 120d
autoworkflow suggest --prompt "add filtering to autosearch stats"
autoworkflow compile --genome go-cli-feature-auto-stack --to openprose
```

### Why It Could 10x Workflow Speed

A one-line task prompt can become a complete execution scaffold with the right docs, phases, validation commands, failure recovery rules, and subagent delegation plan.

### Why It Could 100x Workflow Development

Once workflows are genomes, they can evolve. The system can compare genomes, merge better phases, retire bad ones, and promote recurring successful choreography into skills automatically.

This is basically a Voyager-style skill library, but for software engineering workflows instead of Minecraft behaviors.

### Complexity Cost

Medium-hard. We can start with hand-labeled genomes for 3 common task types, then mine traces to validate/extend them.

### First Thin Slice

Manually author three genomes:

- Go CLI feature
- ETL/schema change
- doc/skill workflow change

Then use autosearch to detect sessions matching each genome and measure compliance: were expected docs read, builds run, tests run, corrections avoided?

## 5. Ghost User Critic With Executable Decision Contracts

### What

Build a user-specific evaluator that reviews generated requirements, solution docs, plans, and PRs before the human sees them. It predicts where you are likely to object, cites the historical evidence, and emits executable decision contracts that downstream hooks/tests can enforce.

This is not a chatbot pretending to be you. It is a scoped preference critic trained from your actual corrections, standards, and accepted decisions.

### Why This Is Non-Obvious

The obvious use of history is to answer questions for the agent.

The more powerful use is to create a reviewer that asks:

> Would the user reject this assumption, find this too vague, consider this over-engineered, or ask for stronger validation?

Your history has strong signals for this:

- You prefer structural insights over local nitpicks.
- You dislike undeclared solution assumptions.
- You want validation steps tied to each phase.
- You prefer stable fixtures over live external dependencies.
- You want stdout parseable in JSON mode and diagnostics on stderr.
- You prefer preserving original data and adding derived fields.
- You value active context delivery over passive indexes.
- You are willing to change direction when a data model feels anti-pattern.

A Ghost User Critic can flag these before you have to.

### How To Build It

Create `auto-eval` or `autoquestion critic`:

```bash
autoquestion critic docs/tasks/042-new-feature/requirements.md --profile user --json
autoquestion critic docs/tasks/042-new-feature/solution.md --profile user --strict
```

The critic outputs:

```json
{
  "likely_objections": [
    {
      "severity": "high",
      "claim": "The solution chooses file storage but requirements never justify storage backend.",
      "historical_basis": [
        "b6027a31-...-2: hidden assumptions cause wrong builds",
        "889f5e19-...-65: storage changed from files to DB mid-session"
      ],
      "recommended_fix": "Move storage backend into requirements as an explicit decision or ask the user."
    }
  ],
  "decision_contracts": [
    {
      "id": "json_stdout_parseable",
      "applies_when": "CLI command has JSON mode",
      "check": "stdout must parse as JSON; diagnostics go to stderr"
    },
    {
      "id": "no_undeclared_arch_decisions",
      "applies_when": "solution.md introduces storage, schema, concurrency, execution model, or external dependency",
      "check": "decision must cite requirements, user answer, or explicit assumption block"
    }
  ]
}
```

Decision contracts then become executable:

```bash
autocontract check docs/tasks/042-new-feature --json
autocontract check-pr 123 --json
```

Contracts should be enforceable at different levels:

- Requirements/solution review: no hidden architectural decisions.
- Implementation review: command output and validation behavior match conventions.
- Hook/CI level: Go files built in nearest module, JSON stdout parseable, no docs stale, no missing tests for acceptance criteria.
- PR review level: diff satisfies task contracts and does not violate promoted standards.

### Why It Could 10x Workflow Speed

The user reviews fewer bad drafts. The critic catches the predictable objections first, and the requirements/solution author fixes them before handoff.

### Why It Could 100x Workflow Development

Decision contracts turn preferences into a living test suite for agent behavior. The agent can no longer merely "remember" a preference; it has to satisfy it. Every new preference can become both guidance and an eval.

That closes the loop:

1. History produces preference/decision cards.
2. Cards produce defaults, questions, and contracts.
3. Contracts evaluate requirements/plans/code/traces.
4. Failures become new regret events.
5. Regret events improve the question/default system.

### Complexity Cost

Medium. The critic can start as an LLM rubric over known preferences, but contracts should be explicit YAML/JSON objects so they can become deterministic checks over time.

### First Thin Slice

Implement only one contract:

```yaml
id: no-undeclared-solution-assumptions
applies_when: solution.md introduces storage, schema, async execution, external dependencies, or CLI behavior
requires: each decision cites requirements.md, AskUserQuestion answer, AGENTS.md convention, or explicit Assumptions section
```

Run it against the last 10 task folders and manually inspect whether it catches real issues.

## How These Five Ideas Fit Together

These ideas are separate, but their real value is compositional.

A future task kickoff could look like this:

1. User gives a one-line task.
2. Active context router retrieves similar sessions, docs, workflow genomes, and decision cards.
3. Decision Frontier Compiler identifies the few questions whose answers materially change the plan.
4. Ghost User Critic reviews the generated requirements/solution for likely objections.
5. Decision Contracts attach to the task and follow it through implementation and PR review.
6. Workflow Wind Tunnel replays the new question policy against historical tasks overnight.
7. Regret Ledger records misses and updates the question/default system.

The user experience becomes:

> "I think this is a Go CLI feature. I found 14 similar tasks. I will apply 11 settled conventions, ask 2 high-blast-radius questions, read 3 docs, use the Go CLI workflow genome, and attach 5 contracts. Here is the requirements diff."

That is materially different from a checklist.

## Concrete Build Roadmap

### Phase 1: Decision Dataset

- Extract explicit AskUserQuestion Q&A pairs.
- Extract implicit decisions from `/new-task`, `/process-requirements`, early user prompts, and corrections.
- Add regret events with coarse cost fields.
- Store as JSONL under `~/.auto/reflect/decisions/` or project-local `.auto/reflect/decisions/`.

### Phase 2: Critic And Contracts

- Implement `no-undeclared-solution-assumptions` as the first contract.
- Implement `autoquestion critic` over requirements/solution docs.
- Use historical user corrections as evidence.

### Phase 3: Frontier MVP

- Implement planning-only `autoquestion frontier` for Go CLI tasks.
- Evaluate against 10 historical tasks.
- Metric: did it ask/default the decisions that later mattered?

### Phase 4: Workflow Genomes

- Hand-author three workflow genomes.
- Build matcher from user prompt -> genome.
- Compile genome into task checklist/OpenProse skeleton.

### Phase 5: Wind Tunnel

- Create 10 replay packs.
- Run planning-only replay first.
- Later add code-execution replay in isolated worktrees.

## What Not To Build First

Avoid these because they are obvious or low-leverage:

- A static YAML playbook of preferences without confidence, scope, regret, or cost.
- A long questionnaire generated from historical categories.
- A semantic search wrapper that returns more examples but does not decide what to ask.
- A dashboard of AskUserQuestion counts without downstream rework metrics.
- A doc index improvement that still depends on agents proactively deciding to read docs.

These are not wrong, but they are not the 10x/100x version.

## The Key Bet

The strongest bet is this:

> Agent workflow development becomes dramatically faster when every prompt, skill, hook, and planning change can be evaluated against historical traces as a regression suite.

`better-questions.md` can be the first product surface for that larger system. The document should evolve from "ask better questions" into "compile, test, and enforce better decisions."


## Addendum: Orthogonal Questioning And Requirement Vector Collapse

### Core Model

The vector-space metaphor is strong, but the important correction is this: the system should not treat the user's true intention as a single fixed embedding point that we can directly observe. It should treat intention as a posterior distribution over many possible requirement vectors.

At task start, the agent has:

```text
P(intent | prompt, repo, user_history, project_history)
```

Each question is an experiment. Each answer updates the posterior:

```text
P(intent | prompt, history, q1=a1, q2=a2, ...)
```

The goal is not to ask the most semantically different questions. The goal is to ask the questions that most reduce uncertainty over decisions that affect whether the final requirements will be accepted.

So the target objective is closer to:

```text
maximize expected reduction in user-relevant uncertainty
minus question cost
minus interruption cost
minus redundancy with already asked questions
```

A good question collapses the posterior along a high-variance, high-impact axis. A bad question either asks about a settled axis, a low-impact axis, or an axis already implied by a previous answer.

### What Orthogonality Should Mean

Naive orthogonality would be cosine distance between question embeddings. That is useful but insufficient.

Two questions can be semantically different but decision-equivalent:

- "Should output default to JSON?"
- "Should humans need a flag for readable output?"

Those are different strings but they collapse the same decision axis.

Two questions can be semantically similar but decision-orthogonal:

- "Should scheduler jobs live in Postgres?"
- "Should scheduler execution state live in Postgres?"

Those look similar but may affect different schema, access control, and lifecycle decisions.

The better definition:

> Questions are orthogonal if their possible answers produce independent changes in the posterior over implementation-relevant decisions.

Practically, represent each question by a decision-impact vector, not just a text embedding.

```json
{
  "question": "Where should scheduler jobs be stored?",
  "text_embedding": [0.12, -0.44, ...],
  "decision_impact_vector": {
    "storage_backend": 0.95,
    "schema_design": 0.86,
    "access_control": 0.52,
    "cli_surface": 0.21,
    "test_strategy": 0.74,
    "docs": 0.12
  },
  "answer_entropy": 0.71,
  "wrong_default_regret": 0.88
}
```

Then pick questions that are orthogonal in residual decision-impact space.

### Requirement Space Should Be Structured, Not Just Embedded

A pure embedding model will blur too much. We need a hybrid representation:

1. Text embedding of the user's prompt, generated requirements, historical prompts, answers, corrections, task docs, and final artifacts.
2. Structured decision dimensions extracted from requirements and sessions.
3. Graph context from code/docs/commits/files touched.
4. Regret/cost dimensions from historical rework.
5. Acceptance dimensions from final merged work, approved docs, or absence of later correction.

A requirement vector should be something like:

```json
{
  "semantic": [0.01, 0.44, ...],
  "decision_axes": {
    "storage": "postgres",
    "output_format": "json_default",
    "validation": "strict_structured_errors",
    "filter_semantics": "one_mode_at_a_time",
    "test_data": "stable_local_fixture",
    "implementation_scope": "minimal_first_slice",
    "docs": "update_active_docs_only",
    "execution": "worktree_pr_flow"
  },
  "confidence": {
    "storage": 0.82,
    "output_format": 0.97,
    "validation": 0.93,
    "filter_semantics": 0.64
  },
  "regret_weight": {
    "storage": 0.91,
    "output_format": 0.31,
    "validation": 0.77,
    "filter_semantics": 0.69
  }
}
```

The latent vector is useful for retrieval and clustering. The structured axes are what make question selection controllable and explainable.

### Question Selection Algorithm

A first real algorithm could be:

1. Build a prior over requirement vectors from the initial prompt.
2. Retrieve similar historical tasks using embeddings and structured filters.
3. Extract candidate decision axes from similar tasks, corrections, and requirements docs.
4. Estimate current confidence for each axis.
5. Estimate regret if the axis is guessed wrong.
6. Generate candidate questions that target one or more axes.
7. For each candidate question, estimate expected information gain over the posterior.
8. Residualize candidates against already known/asked axes to penalize redundancy.
9. Pick the question with maximum utility.
10. Stop when residual uncertainty is below a threshold or remaining uncertainty is low-impact.

Pseudo-score:

```text
score(q) = E[posterior_uncertainty_reduction(q)]
         * decision_blast_radius(q)
         * wrong_default_regret(q)
         * answerability(q)
         - user_interruption_cost(q)
         - redundancy_penalty(q, asked_questions)
```

Redundancy penalty should be computed over decision-impact vectors, not just question text.

The stopping rule matters. The agent should not ask until certainty is perfect. It should ask until the expected value of the next question is lower than the cost of asking.

### How To Estimate Information Gain Without A Perfect Model

We do not need a mathematically perfect Bayesian model to start.

For each candidate question, ask a model to simulate plausible answer branches:

```text
Question: Should scheduler jobs be stored on disk or in Postgres?
Branch A: disk
Branch B: Postgres
Branch C: no persistent scheduler jobs yet
```

For each branch, generate the requirements delta or plan delta. Then compare deltas:

- number of files affected
- API/CLI surface changes
- data model changes
- test fixture changes
- migration/reversibility
- acceptance criteria changes
- likely regret based on historical corrections

If answer branches produce nearly identical downstream requirements, the question has low information gain. If branches produce very different requirements, the question has high information gain.

This gives a practical implementation of orthogonality:

> A question is high-value when its answer branches spread far apart in downstream plan space and that spread is not explained by already-asked questions.

### Dataset We Can Build From Existing History

We can build a surprisingly rich supervised dataset from the data already in auto-stack.

Each training example should be a task episode:

```json
{
  "episode_id": "...",
  "initial_prompt": "...",
  "repo_state": {
    "commit": "...",
    "workspace": "...",
    "docs_index": ["..."]
  },
  "questions_asked": [
    {
      "message_id": "...",
      "question": "...",
      "options": ["..."],
      "answer": "...",
      "turn_index": 12
    }
  ],
  "implicit_decisions": [
    {
      "source": "initial_prompt|correction|requirements_doc|solution_doc|commit",
      "axis": "storage_backend",
      "value": "postgres",
      "evidence_message_id": "..."
    }
  ],
  "corrections": [
    {
      "message_id": "...",
      "wrong_assumption": "file_storage",
      "corrected_value": "postgres",
      "cost_before_correction": {
        "tokens": 184000,
        "tool_calls": 44,
        "files_touched": 6
      }
    }
  ],
  "final_requirements": "...",
  "final_solution": "...",
  "accepted_commit_range": "...",
  "outcome": "accepted|reworked|abandoned|unknown"
}
```

Sources:

- AskUserQuestion tool inputs and answers.
- `/new-task` and `/process-requirements` command args.
- Initial user prompt and early user messages.
- Mid-session corrections: "actually", "changed my mind", "wrong", "undo", "not what I want".
- Requirements, solution, context, and plan docs.
- PR comments and review threads.
- Git commits linked to sessions.
- Build/test failures and command loops as regret-cost signals.

The key label is not just "what answer did the user give?" The better label is:

> How much closer did this question move the agent's requirement vector toward the final accepted requirement vector?

### Constructing The Training Labels

For every historical episode, reconstruct three vectors:

1. Initial agent belief vector: inferred from the first draft plan/requirements or from what a baseline model would infer from the initial prompt.
2. Post-question belief vectors: inferred after each Q/A pair.
3. Final accepted intent proxy: inferred from final requirements, solution, merged code, tests, and any later user acceptance/correction.

Then score each question:

```text
value(q_i) = distance(belief_before_i, final_intent)
           - distance(belief_after_i, final_intent)
```

But distance should be hybrid:

```text
distance = semantic_embedding_distance
         + structured_decision_mismatch_penalty
         + regret_weighted_mismatch_penalty
         + acceptance_criteria_mismatch_penalty
```

This lets us train/rank questions by measured vector collapse, not vibes.

### Negative Examples Are As Important As Positive Examples

The dataset should include:

- Redundant questions: questions whose answer was already implied.
- Late questions: questions that were asked only after rework began.
- Missing questions: inferred from corrections where the agent never asked.
- Low-impact questions: questions that did not change the final requirements.
- Annoying questions: questions the user answered with "just do it", "no plan required", or equivalent.

Missing questions are especially valuable. A correction like:

```text
I think I've changed my mind and I think we should store these scheduler jobs in the database rather than storing them on disk...
```

Can become a synthetic positive training example:

```json
{
  "should_have_asked": "Where should scheduler jobs be stored?",
  "ideal_time": "before solution design",
  "answer": "Postgres database table",
  "regret_cost": "high"
}
```

### Orthogonal Question Bank

Over time, the system should learn a reusable basis of question axes. Example axes in this repo:

- Persistence: none, local file, SQLite, Postgres, parquet, git history, external API.
- Output contract: JSON default, text flag, markdown, parseable stdout, stderr diagnostics.
- Validation: fail-fast, partial results with non-zero exit, structured errors, warning-only.
- Scope boundary: current feature only, long-term extensibility, migration path, backwards compatibility.
- Test data: live API, stable checked-in fixture, git history, generated temp fixture, remote service.
- Agent workflow: plan-first, implement-now, delegate subagents, open-prose pipeline, PR review.
- Data fidelity: preserve raw, derive truncated view, redact credentials, normalize input.
- Discovery: passive docs, active doc routing, context bundle, graph traversal.
- Safety/autonomy: ask before destructive operation, proceed on safe read-only checks, stop on architecture ambiguity.

A good requirement session asks questions that span different axes. A bad one asks five variants of the same axis.

### Relation To The Five Existing Ideas

Orthogonal questioning slots into the earlier architecture directly:

- Decision Frontier Compiler becomes the mechanism that identifies high-variance axes.
- Regret Ledger provides weights for which axes are expensive when guessed wrong.
- Workflow Wind Tunnel evaluates whether the selected questions would have prevented historical corrections.
- Workflow Genome Compiler provides task-type-specific priors over axes.
- Ghost User Critic checks whether the final collapsed vector still violates known user preferences.

This is the mathematical backbone underneath the previous five ideas.

### MVP: `autoquestion collapse`

A concrete first command:

```bash
autoquestion collapse --prompt "add filtering to autosearch stats" --cwd . --max-questions 3 --json
```

Output:

```json
{
  "initial_intent_distribution": {
    "task_class": "go_cli_filtering",
    "top_axes": [
      {"axis": "filter_composition", "confidence": 0.52, "regret": 0.78},
      {"axis": "scope_parity", "confidence": 0.61, "regret": 0.64},
      {"axis": "output_contract", "confidence": 0.96, "regret": 0.31}
    ]
  },
  "defaulted_axes": [
    {
      "axis": "output_contract",
      "value": "json_default_stderr_diagnostics",
      "why": "High confidence from AGENTS.md and prior CLI work."
    }
  ],
  "questions": [
    {
      "question": "Should stats allow combined filters, or should it keep the existing one-filter-mode convention?",
      "targets_axes": ["filter_composition", "validation"],
      "expected_information_gain": 0.84,
      "orthogonality_to_previous": 1.0,
      "why_this_question": "Answer branches produce different query planner, validation, and test matrix."
    },
    {
      "question": "Should this filtering apply only to stats, or should search and stats keep exact parity?",
      "targets_axes": ["scope_parity", "api_consistency"],
      "expected_information_gain": 0.67,
      "orthogonality_to_previous": 0.81,
      "why_this_question": "This determines whether implementation is local or shared across search/stats."
    }
  ],
  "stop_reason": "Remaining uncertainty is low-impact or already covered by conventions."
}
```

### MVP Evaluation

Use 20 historical task episodes.

For each episode:

1. Hide the real questions and final requirements.
2. Give `autoquestion collapse` only the initial prompt and repo context.
3. Let it propose up to 3 questions plus defaults.
4. Compare against final accepted requirements and known corrections.

Metrics:

- Decision recall: did selected questions cover decisions that mattered?
- Redundancy: how correlated were selected questions' decision-impact vectors?
- Regret prevention: would any known correction have been prevented?
- Question count: how many questions to reach acceptable vector distance?
- User annoyance proxy: did it ask about high-confidence conventions?

### The Deep Bet

The deep bet is that requirements gathering can become active learning over a user-specific latent decision space.

The user does not need to fully specify every task. The agent does not need to guess blindly. The system should learn a basis for the user's decision space, maintain uncertainty over that basis, and ask the smallest set of near-orthogonal questions that collapses the posterior enough to proceed.

If we can build that, requirements docs become the output of a measurement process, not a manual writing process.

## Second Addendum: Geometry, Memes, And Deeper Constructs

### The User Intention Is Not A Point

The user's true intention should not be modeled as a single hidden point. It is better modeled as an acceptance basin.

There are many requirement vectors the user would accept because they preserve the important invariants, avoid known bad defaults, and leave reversible details open. The agent does not need to hit the exact center of the user's mind. It needs to enter the basin where the user says: yes, this is close enough, continue.

That reframes the objective:

```text
Do not minimize distance to an unknowable point.
Enter the acceptable region with minimum question cost and minimum future regret.
```

This matters because some dimensions do not need to be collapsed. If the implementation can safely choose either name, flag spelling, or internal helper shape later, asking about it is wasted precision. The system should distinguish:

- Critical dimensions: wrong answer changes architecture, tests, data model, security, or user-visible contract.
- Soft dimensions: wrong answer is easy to change later.
- Invisible dimensions: user likely does not care unless the choice causes downstream pain.
- Forbidden dimensions: areas the implementation must not enter.

The target is not perfect specification. The target is a safe-to-continue certificate.

### Orthogonality Lives In Consequence Space

A question is not orthogonal because its wording is different. It is orthogonal because its answer changes a different part of the future.

Memetic form:

> Stop measuring question distance. Measure future-world distance.

A candidate question should be represented by the vector of consequences its answers can induce:

```text
question_vector(q) = delta(files, APIs, schema, tests, docs, workflow, risk, reversibility)
```

Two questions are redundant if their answer branches change the same future-world coordinates, even if their wording is unrelated.

Two questions are orthogonal if they separate different clusters of possible futures.

That makes the practical test simple:

1. Generate plausible answers to a candidate question.
2. Generate compact plan deltas for each answer.
3. Embed and structure those deltas.
4. Compare the spread against other candidate questions.
5. Prefer questions whose spreads cover new consequence directions.

### Question Gram-Schmidt

We can steal the intuition from Gram-Schmidt orthogonalization.

Start with a pool of candidate questions. Each question has a decision-impact vector. Pick the highest expected-regret-reduction question first. Then project every remaining question onto the selected question's impact vector and keep only the residual.

Pseudo-process:

```text
selected = []
residual_space = all_decision_axes

while budget remains:
  score each question by impact in residual_space
  pick best q
  selected.append(q)
  residual_space = residual_space - projection(q)
```

This prevents the classic bad interview pattern:

1. Do you want JSON output?
2. Should output be parseable?
3. Should humans have text mode?
4. Should diagnostics go to stderr?

Those are not four independent questions. They are one output-contract region. A good system should either default the region from conventions or ask one high-leverage question that collapses the whole bundle.

A product command could be:

```bash
autoquestion gram-schmidt --prompt "..." --candidates candidates.json --budget 3
```

Output should show what each selected question covers and which candidates were suppressed as redundant.

### Requirements PCA

Historical requirements can be treated like a matrix:

```text
rows = task episodes
columns = decision axes
values = chosen option, confidence, regret, user correction cost
```

Run a conceptual PCA or factor analysis over that matrix. Not necessarily literal linear PCA; the important thing is to find the dominant axes along which your requirements actually vary.

Possible principal components in this repo:

1. CLI contract strictness: JSON stdout, stderr diagnostics, structured errors, fail-fast invalid usage.
2. Data fidelity: preserve raw, derive views, avoid destructive transforms, security redaction exception.
3. Test realism: stable local fixtures, e2e via actual CLI, avoid live external dependencies unless explicitly required.
4. Workflow autonomy: plan-first for complex work, implement-now for small fixes, ask before destructive operations.
5. Context delivery: active doc routing, graph context, subproject CLAUDE.md, avoid passive indexes.
6. Scope discipline: first slice now, long-term extensibility recorded but not overbuilt.
7. Agent orchestration: subagents for parallel exploration, coordinator for synthesis, stop on architecture ambiguity.

Questions should be picked to collapse uncertainty along these principal components first, not along arbitrary topical categories.

Memetic form:

> Ask along the user's principal components, not the agent's anxieties.

### Regret Curvature

Some axes are curved: small mistakes cause huge downstream pain.

Example: choosing file storage instead of Postgres for scheduler jobs is not a small displacement. It changes schema, tools, access control, tests, and lifecycle. The local distance in text space may look small, but the regret curvature is high.

Other axes are flat: a flag name, helper function name, or exact markdown heading can be changed cheaply.

The model should learn curvature from history:

```text
curvature(axis) = expected_rework_cost_when_wrong / apparent_semantic_distance
```

High-curvature axes deserve early questions. Low-curvature axes deserve defaults or reversible implementation.

Memetic form:

> Default the flat axes. Interrogate the curved axes.

### Bifurcation Questions

The highest-value questions are not preference questions. They are bifurcation questions.

A bifurcation question splits the future into genuinely different plan families:

- Is this a production feature or a spike?
- Is this state persistent or derived?
- Is this local to one command or a shared primitive?
- Should this be enforced by convention, hook, test, or runtime behavior?
- Is the source of truth code, docs, git history, database, or user input?
- Are we optimizing for immediate task completion or future workflow acceleration?

A preference question adjusts a local detail inside one plan family:

- Should the flag be called `--text` or `--format text`?
- Should the section be called Assumptions or Defaults?
- Should the helper file be named `query.go` or `filters.go`?

Preference questions can be useful, but asking them early is usually a smell.

Memetic form:

> One good bifurcation question beats ten preference questions.

### Invariant Questions

Sometimes the most powerful question is not "what do you want?" It is "what must remain true?"

Users often know invariants more clearly than implementations:

- Do not lose raw data.
- Do not make stdout unparsable in JSON mode.
- Do not depend on live APIs in tests.
- Do not silently create hidden global state.
- Do not let docs drift from code.
- Do not ask me questions whose answers are already in AGENTS.md.

Invariant questions carve forbidden regions out of the phase space. They can eliminate more bad futures than option questions.

Potential question form:

```text
What would make an implementation of this unacceptable even if the visible feature works?
```

Or, generated automatically:

```text
For this task I infer these invariants: no live API dependency, JSON stdout remains parseable, and raw session data is preserved. Are any of these wrong or missing?
```

This is efficient because one confirmation can collapse many dimensions at once.

Memetic form:

> Ask for invariants before preferences.

### Gauge Questions

Some questions do not locate the point in space. They define the coordinate system.

Examples:

- Are we writing product requirements, implementation plan, or exploratory spike notes?
- Should this be optimized for agents as primary users or humans as primary users?
- Is this repo-local convention or cross-project convention?
- Should this be treated as a long-term platform primitive or a tactical fix?

These are gauge questions. They determine how every later answer should be interpreted.

If the gauge is wrong, every later vector projection is distorted.

Memetic form:

> Before asking where the point is, choose the coordinate system.

### Phase-Transition Questions

Some thresholds change the nature of the task.

Examples:

- If this touches persistent schema, it stops being a small CLI change and becomes a migration/test-data problem.
- If this is cross-project, it stops being a local convention and becomes an install/sync/docs problem.
- If this is public-release-facing, it stops being a feature and becomes a supply-chain/security/docs problem.
- If this requires live external APIs, it stops being a unit-test problem and becomes a fixture/mock/replay problem.

The question system should detect possible phase transitions and ask only when near a boundary.

Question form:

```text
This looks like it may cross from local feature into shared platform primitive. Should I keep it local for now, or design the shared primitive?
```

Memetic form:

> Ask at phase boundaries, not in the middle of flat terrain.

### Symmetry-Breaking Questions

Some alternatives are equivalent from the agent's perspective but not from the user's perspective. These need a small nudge to break symmetry.

Example:

- `update` vs `upgrade` looked like naming, but the user preferred standardizing on one and removing aliases because ambiguous CLI surfaces are a design smell.

A symmetry-breaking question is useful when:

- multiple options are technically similar
- historical user taste strongly prefers one type of clarity
- making both options available would create long-term ambiguity

Question form:

```text
There are two technically easy routes: keep an alias for compatibility, or remove the old flag decisively. Your past preference is to remove ambiguous aliases. I will remove it unless this case needs compatibility.
```

Memetic form:

> Symmetry is not neutral when it creates future ambiguity.

### The Question Is A Lens, Not A Sentence

A question changes what the user sees. Bad questions force the user to answer at the wrong abstraction level.

Bad:

```text
Should the data be stored in file A or file B?
```

Better:

```text
Is this state something users will need to query, mutate, and permission independently?
```

The better question reveals the underlying decision dimension. It lets the user answer in terms of intent, not implementation mechanics.

This suggests a question rewrite model:

1. Generate implementation-level uncertainty.
2. Infer the user-intent-level decision behind it.
3. Ask at the intent level.
4. Translate answer back to implementation constraints.

Memetic form:

> Do not ask users to pick implementation details. Ask them to collapse intent dimensions.

### Option Sets As Landmarks

Multiple-choice options are not neutral. They create landmarks in the user's phase space.

A bad option set anchors the user around the agent's current imagination. A good option set spans the real decision axis.

Bad:

```text
Where should we store this?
1. JSON file
2. YAML file
3. SQLite
```

Better:

```text
What kind of state is this?
1. Ephemeral derived state, safe to regenerate
2. Local project state, human-editable and simple
3. Queryable product state with permissions and lifecycle
```

The second option set lets the answer imply storage. It is more orthogonal because it asks about the causal dimension, not the implementation surface.

A future `AskUserQuestion` helper could generate and score option sets by coverage:

```text
option_set_quality = axis_coverage - anchoring_bias - overlap_between_options
```

Memetic form:

> Options are coordinates. Bad options bend the space.

### The Agent's Starting Point Is Not Zero

The user's metaphor says the AI starts at random or zero. In this repo, the agent starts from a strong prior:

- AGENTS.md conventions
- subproject CLAUDE.md conventions
- historical user choices
- repository architecture
- task docs
- git history
- existing code patterns
- skill instructions

So the model should not start at zero. It should start at a prior mean plus covariance:

```text
intent_prior = learned_user_prior + repo_prior + task_class_prior + local_code_prior
```

The more important object is covariance. We need to know where uncertainty remains.

A mature system should say:

```text
I am already confident about output format, validation style, and test fixture strategy.
I am uncertain about scope parity and shared abstraction level.
So I will ask only about those.
```

Memetic form:

> The prior is free work. Spend questions only on covariance.

### Requirement Dark Matter

Some user intent never appears in initial prompts or answers but strongly affects satisfaction. It appears only when violated.

Examples:

- The user cares about not losing original data.
- The user cares about not overbuilding vague abstractions.
- The user cares about active doc usage over passive doc presence.
- The user cares about structural insights over local nitpicks.

This is requirement dark matter. It bends the trajectory even when not directly visible.

We infer it from corrections, repeated preferences, and what the user praises or rejects.

The Ghost User Critic is basically a dark-matter detector.

Memetic form:

> Every correction is evidence of invisible mass in intention space.

### Question Debt

When the agent skips a high-curvature question, it takes on question debt.

Question debt accrues interest as implementation proceeds:

- more files touched
- more tests written around possibly wrong assumptions
- more docs aligned to wrong design
- more user trust spent
- more context window consumed

At some point, answering the question later costs more than asking it early.

This suggests a runtime warning:

```text
You are about to write implementation code while storage_backend confidence is 0.41 and regret curvature is high. Ask or explicitly hedge before proceeding.
```

Memetic form:

> Unasked high-curvature questions become compound interest.

### Hedges As Alternatives To Questions

Sometimes the best move is not to ask. It is to design a reversible hedge.

If a decision is uncertain but cheap to defer, the agent should avoid interrupting the user and choose a design that preserves optionality.

Example:

```text
I am not going to ask whether this later needs semantic search. I will keep the index interface narrow enough that we can add semantic mode later without changing the CLI contract.
```

This is important because question-minimization is not always question-selection. Sometimes it is reversibility engineering.

A good model should choose among:

- Ask now.
- Default confidently.
- Hedge architecturally.
- Defer explicitly.
- Stop and require clarification.

Memetic form:

> The cheapest question is the one made unnecessary by reversible design.

### Active Learning With A Cost-Aware Oracle

In machine learning, active learning often assumes labels are cheap once requested. Here labels come from a human trying to build things. Questions have social and cognitive cost.

So the user is a costly oracle.

The objective should include:

- cognitive load of answering
- interruption cost
- user's expertise required
- risk of asking too implementation-specific a question
- trust cost from asking obvious questions
- delay cost from not proceeding

This leads to a cost-aware active learning objective:

```text
utility(q) = expected_regret_reduction(q) / human_cost(q)
```

But some questions have asymmetric cost. A question can be cheap if framed as a default with override:

```text
I will assume local fixtures, not live APIs, because that is your usual preference. Say otherwise if this case needs live integration.
```

This is a low-cost label request. It lets silence carry information, but only for high-confidence defaults.

Memetic form:

> Human attention is the scarce label budget.

### The Best Question May Be A Draft

Sometimes the most information-dense question is not a question. It is a concrete draft.

Instead of:

```text
What are all your requirements?
```

The agent writes:

```text
I think the requirements are: A, B, C. I am least certain about X and Y. Correct the wrong parts.
```

This lets the user react to a structured object. Reaction is often easier than generation.

In vector terms, the draft is a probe point. The user's edits are gradient feedback.

A system should compare two modes:

- Interrogative mode: ask explicit questions.
- Projective mode: project a requirements vector and ask for correction.

For expert users like you, projective mode may collapse more dimensions per turn because you can quickly spot wrong assumptions.

Memetic form:

> A draft is a high-bandwidth question.

### Requirement Tomography

This whole process resembles tomography. We cannot directly observe the user's intent object, so we send probes through it from different angles. Each answer gives a projection. Enough well-chosen projections reconstruct the shape.

Bad requirements gathering asks many parallel projections. Good requirements gathering rotates the probe angles.

Question orthogonality is tomography angle selection.

Potential product language:

```text
autoquestion tomo --prompt "..." --angles 3
```

Output:

```text
Probe 1: task class and scope boundary
Probe 2: persistence/source-of-truth invariant
Probe 3: validation and user-visible contract
```

Memetic form:

> Requirements are tomography, not transcription.

### The User Model Should Be Multi-Resolution

Some preferences are global. Some are repo-specific. Some are task-class-specific. Some are local to a single feature.

A flat user preference embedding will overgeneralize.

Use a multi-resolution prior:

```text
global_user_prior
  -> repo_prior
    -> subproject_prior
      -> task_class_prior
        -> episode_context
```

Example:

- Global: prefer explicit CLI surfaces, stable tests, no hidden destructive actions.
- Repo: auto-stack command outputs default to JSON unless documented otherwise.
- Subproject: auto-etl preserves raw data and adds derived fields.
- Task class: ETL changes need fixture/golden tests and migration caution.
- Episode: this particular task is a spike, so proof beats polish.

Question selection should ask only when priors at different resolutions disagree or confidence is low.

Memetic form:

> Do not average the user. Condition the user.

### Active Contradiction Discovery

The system should search for conflicting priors before asking questions.

Example:

- General convention: default to JSON output.
- Specific command class: `quickstart` outputs markdown.
- Current task: might be a docs/help command.

The right question is not "JSON or markdown?" It is:

```text
This command looks like a help/documentation command, which is one of the exceptions to JSON default. Should it follow the quickstart/docs markdown exception, or behave like ordinary data commands?
```

This is high-value because contradictions are where defaults fail.

Memetic form:

> Questions belong at prior conflicts.

### Question Smells

A few anti-patterns should be detectable:

- Cosmetic-first smell: asking naming/style before architecture is settled.
- Repeated-axis smell: multiple questions target the same decision axis.
- Convention-ignorance smell: asking what AGENTS.md already answers.
- Implementation-leak smell: asking the user to choose code structure when they care about behavior.
- Low-curvature smell: asking about a reversible choice.
- Missing-invariant smell: asking options without first identifying forbidden states.
- Late-bifurcation smell: asking a plan-family question after files have already been written.
- Fake-choice smell: offering options where one violates known constraints.

A question linter could flag these before `AskUserQuestion` is used.

```bash
autoquestion lint questions.json --context docs/tasks/042/requirements.md
```

Memetic form:

> Lint your questions like you lint code.

### Possible Data Products

This line of work suggests several concrete artifacts:

#### `question_axes.jsonl`

One learned axis per row:

```json
{"axis":"storage_backend","options":["none","file","sqlite","postgres","parquet"],"curvature":0.91,"scope":"cross_project"}
```

#### `question_events.jsonl`

Every asked, missed, redundant, or defaulted question:

```json
{"episode":"...","type":"asked","question":"...","axis":["storage_backend"],"answer":"postgres","value":0.82}
```

#### `intent_vectors.parquet`

Embeddings and structured axes for prompts, drafts, final requirements, corrections, and commits.

#### `regret_surfaces.parquet`

Estimated rework cost by axis and task class.

#### `question_basis.yaml`

Human-readable canonical basis for a repo or user:

```yaml
basis:
  - id: persistence-source-of-truth
    ask_when: state may be user-visible, mutable, queryable, or permissioned
    default: derive from nearest existing subsystem
    high_regret_if_wrong: true
  - id: output-contract
    ask_when: command is docs/help/export or conflicts with JSON default
    default: json_stdout_stderr_diagnostics
```

### Training Architecture

Do not train one giant model first. Build a pipeline of small learnable/rankable pieces:

1. Episode builder: reconstruct task episodes from sessions, docs, git history, PRs.
2. Axis extractor: map text and diffs to decision axes.
3. Intent vectorizer: produce hybrid semantic + structured vectors.
4. Question generator: propose candidate questions and default-with-override statements.
5. Consequence simulator: generate answer branches and plan deltas.
6. Question ranker: score information gain, regret reduction, orthogonality, human cost.
7. Question linter: detect smells.
8. Evaluator: replay historical episodes and score vector collapse.

This is easier to debug than a monolithic model and aligns with auto-stack's CLI-first, JSON-first style.

### Evaluation Beyond Accuracy

Accuracy is the wrong headline metric. Use these:

- Collapse efficiency: distance reduced per question.
- Regret recall: fraction of known regret axes identified before implementation.
- Orthogonality score: marginal information gain of each additional question.
- Convention suppression: percentage of high-confidence convention questions not asked.
- Acceptance-basin entry: whether final draft is accepted without major correction.
- Time-to-safe-plan: turns until the agent can proceed responsibly.
- Hedge quality: uncertain decisions deferred without blocking future change.

The strongest metric:

```text
accepted_requirements_distance_after_k_questions
```

Where distance is hybrid and regret-weighted.

### Hard Problems Worth Naming

#### Axis Drift

The user's decision axes evolve. What was a preference can become a convention. What was a convention can get overridden by new architecture.

Need time-aware priors and decay.

#### False Orthogonality

Questions can look independent but share hidden causal parents. Example: storage backend and access-control model often couple through product semantics.

Need consequence simulation to catch this.

#### Over-Collapsing

The agent can ask too many questions and collapse dimensions the user would rather leave flexible.

Need acceptance-basin stopping, not point-target stopping.

#### User State Dependence

The user's answer depends on whether they are brainstorming, planning, reviewing, or trying to ship.

Need session-mode detection.

#### Language-Implementation Gap

The same user phrase can map to different implementation choices in different repos.

Need repo/task priors.

#### Silent Satisfaction

No correction does not always mean correct. It may mean the user did not notice.

Need downstream tests, PR review, and later-session signals.

### Product Memes

These are worth keeping as internal language:

- Requirements are collapsed, not written.
- A good question is a steering vector, not a sentence with a question mark.
- Orthogonality lives in consequence space.
- Ask along the user's principal components, not the agent's anxieties.
- Default the flat axes. Interrogate the curved axes.
- Every correction is a fossilized missing question.
- Question debt compounds.
- A draft is a high-bandwidth question.
- Requirements are tomography, not transcription.
- The prior is free work. Spend questions only on covariance.
- Do not maximize curiosity. Maximize regret reduction.
- The goal is not certainty. The goal is entering the acceptance basin.
- Options are coordinates. Bad options bend the space.
- Lint your questions like you lint code.
- Ask at phase boundaries, not in flat terrain.
- The user is not hiding requirements. The agent is using the wrong coordinate system.
- If a question would not change the plan, it is probably documentation, not discovery.
- Human attention is the scarce label budget.

### Wild But Plausible Interface Ideas

#### 1. Requirements Radar

A small text UI showing uncertainty axes:

```text
Requirement Radar

high regret / low confidence:
  storage_backend        0.42 confidence  0.91 regret
  shared_abstraction     0.48 confidence  0.78 regret

high confidence defaults:
  output_contract        0.96 confidence  0.31 regret
  local_fixtures         0.89 confidence  0.73 regret

recommended question:
  Is this state user-visible/queryable enough to need Postgres, or is local derived state enough?
```

#### 2. Question Diff

Before asking, show why this question matters:

```text
If answer A: touch cli/search only, add filter validation tests.
If answer B: extract shared query planner, touch search + stats, add parity tests.
Delta: shared abstraction and larger test matrix.
```

This makes the question feel justified, not arbitrary.

#### 3. Silence-As-Answer Defaults

For high-confidence axes, emit default-with-override statements:

```text
Defaulting to stable local fixtures rather than live APIs. Say otherwise if this specifically needs live integration.
```

This uses silence as a weak label only when safe.

#### 4. Question Budget Declaration

At kickoff:

```text
I think I need at most two questions. After that I should be inside the acceptance basin.
```

This communicates discipline.

#### 5. Regret Replay

When proposing a question:

```text
I am asking because similar storage assumptions caused rework in sessions X and Y.
```

This teaches trust. The user sees the question is not random.

#### 6. Question Unit Tests

A task plan can include expected question behavior:

```yaml
question_tests:
  - should_not_ask: output format for ordinary CLI command
    because: AGENTS.md already fixes JSON default
  - should_ask: storage backend
    because: prompt introduces persistent user-visible state
```

#### 7. Acceptance Basin Check

Before implementation:

```bash
autoquestion basin docs/tasks/042/requirements.md --prompt "..."
```

Output:

```text
inside basin: likely
remaining high-regret uncertainty: none
remaining low-regret uncertainty: flag spelling, helper names
safe to proceed: yes
```

### The Sharpest Buildable Version

The best first implementation is not embeddings-first. It is regret-first plus consequence simulation.

Build this:

```bash
autoquestion propose --prompt "..." --cwd . --budget 3 --json
```

Internals:

1. Retrieve similar tasks and corrections.
2. Extract top uncertain axes.
3. Generate candidate questions.
4. For each question, simulate answer branches and plan deltas.
5. Score branch spread, regret weight, and redundancy.
6. Output selected questions plus suppressed defaults.

Then build the evaluator:

```bash
autoquestion eval --episodes .auto/eval/question-episodes.jsonl --budget 3
```

The first win is not a trained model. The first win is a measurable pipeline that can tell whether a question set was good.

### Final Construct

The full system is a question compiler.

Input:

```text
rough user prompt + repo context + history
```

Intermediate representation:

```text
latent intent distribution + decision axes + regret surface + workflow genome
```

Optimization:

```text
cost-aware active learning over consequence-space basis vectors
```

Output:

```text
minimal question set + defaulted assumptions + safe-to-continue certificate
```

That is the crisp version of the idea.

Not "an AI asks better questions."

A requirements compiler emits the minimum measurement plan needed to place the implementation inside the user's acceptance basin.

## Third Addendum: Blind Spot Cartography And Golden Decision Harvesting

The next jump is to stop treating questions as something the agent only asks during a task. If the user has a stable but partially hidden decision policy, then task-time questioning is only one sampling regime. It is often the worst regime: the user is busy, the agent is under time pressure, the decision is entangled with implementation details, and every clarification interrupts flow.

A better framing:

> We are not merely asking questions to finish the current task. We are running experiments against the user's latent decision function.

That suggests a new product layer: a no-task calibration loop that finds poorly understood directions in the user's preference space, generates tiny high-signal probes, asks the user like a quiz, and stores the answers as golden decision data.

This is not a survey. A survey asks what the user thinks in general. This system asks boundary-case questions designed to collapse uncertainty in the exact places where future agents are likely to make expensive mistakes.

### 1. Blind Spot Cartography

A blind spot is not simply "something we do not know". It is a decision region where ignorance is likely to cause bad work.

The system should maintain a map of decision axes. Each axis has estimated coverage, uncertainty, historical frequency, regret cost, contradiction count, and task relevance.

Example axes in this repo:

- Output defaults: JSON-first vs human-readable-first.
- CLI compatibility: remove deprecated aliases vs preserve backwards compatibility.
- Validation strictness: fail fast vs return partial valid results.
- Data preservation: preserve raw transcripts vs rewrite normalized data.
- Testing realism: checked-in fixtures vs live system integration.
- Planning depth: plan-first vs implement-now.
- Search behavior: recall broad context vs target narrow evidence.
- Agent autonomy: ask user vs make an assumption and proceed.
- Documentation mode: terse operational docs vs explanatory product docs.
- Data modeling: canonical denormalized journey format vs optimized secondary views.

For each axis, track:

```json
{
  "axis_id": "cli_output_default",
  "description": "Whether commands should default to JSON or human-readable output.",
  "coverage": 0.82,
  "entropy": 0.11,
  "historical_examples": 47,
  "contradictions": 1,
  "regret_cost": 0.77,
  "task_frequency": 0.68,
  "last_confirmed": "2026-05-26",
  "scope": "auto-stack/global-cli"
}
```

The blind-spot score should not be raw uncertainty. A rarely used uncertain axis does not matter much. A good score is closer to:

```text
blind_spot_score = uncertainty
                 * expected_task_frequency
                 * regret_cost
                 * contradiction_penalty
                 * decision_irreversibility
                 / evidence_count
```

This creates a useful ranking:

- High uncertainty, high regret, common decision: ask soon.
- High uncertainty, low regret, rare decision: ignore.
- Low uncertainty, high contradiction: ask a boundary probe.
- High confidence, recent correction: decay confidence and retest.

The tool should output a heatmap of the user's latent decision policy:

```bash
autoquestion blindspots --cwd . --json
autoquestion map --profile current-user --scope auto-stack --format text
```

Possible text output:

```text
Latent decision blind spots

1. e2e_test_realism
   score: 0.91
   reason: frequent test-design decisions, mixed historical behavior, high rework cost
   next probe: fixture vs live-history task card

2. planning_doc_depth
   score: 0.76
   reason: user often asks for deep thinking, but also values direct execution
   next probe: plan-first vs prototype-first boundary card

3. backwards_compatibility
   score: 0.63
   reason: strong explicit rule to remove deprecated flags, but unknown boundary for external users
   next probe: alias-removal regret card
```

The key is that the agent stops asking random clarifying questions and starts asking questions at the holes in its map.

Meme:

> Do not ask what you already know. Do not ask what does not matter. Ask where the map is blank and the cliff is nearby.

### 2. Types Of Unknowns

The system should distinguish several kinds of missing knowledge. They require different probes.

**Known unknowns**

The system knows the axis exists but lacks labels.

Example: whether the user prefers generated reports to include speculative future features or only current-state descriptions.

Probe style: direct forced-choice card.

**Unknown unknowns**

The system does not yet know the axis exists.

Example: the user may care strongly about preserving exact user wording in session-derived data, but no prior task exposed that preference.

Probe style: anomaly mining, failed prediction analysis, clustering corrections that do not fit existing axes.

**Conflicted priors**

History contains contradictory evidence.

Example: the user wants autonomy and persistence, but also wants careful question-asking when intention is unclear.

Probe style: boundary card that names the tradeoff explicitly.

**Thin axes**

There is one explicit rule, but not enough examples to infer scope.

Example: "Default command output to JSON" is clear for CLI tools, but does that apply to quickstart/docs commands?

Probe style: scoped counterexample card.

**Phase boundaries**

The policy changes abruptly under some condition.

Example: "Do not ask questions" becomes "ask before acting" when destructive actions, user preference ambiguity, or irreversible external effects enter the task.

Probe style: threshold card.

**Preference dark matter**

The user's choices imply an unseen force that is not represented in the model.

Example: the user repeatedly rejects technically correct ideas because they feel too generic, too average, or too much like agent slop. The visible axis might look like "design quality", but the hidden force is closer to "strong taste and non-obviousness".

Probe style: ask the user to reject among several plausible-but-wrong options and explain why.

### 3. Golden Decision Harvesting

The output of these probes should be golden decision data.

A golden decision is not a generic preference. It is a scoped, evidence-backed label for how the user wants decisions made in a class of situations.

Schema:

```json
{
  "golden_id": "golden_20260526_001",
  "axis_id": "backwards_compatibility",
  "card_id": "phantom_cli_alias_removal_003",
  "question": "A command has both --update and --upgrade. Existing docs mention --upgrade only. Do we keep --update as an alias?",
  "answer": "Remove --update unless external compatibility is documented.",
  "rationale": "Ambiguous aliases make behavior unclear and increase maintenance surface.",
  "scope": "auto-stack/cli-tools",
  "confidence": "user_explicit",
  "stability": "high",
  "created_at": "2026-05-26T00:00:00Z",
  "decay_after": null,
  "counterexamples": [
    "Keep aliases temporarily if a released external API documents them and migration warnings exist."
  ]
}
```

The important part is scope. Without scope, golden data becomes superstition.

Bad golden:

```text
The user hates aliases.
```

Good golden:

```text
For internal auto-stack CLI tools, remove deprecated flags decisively unless there is documented external compatibility risk. Prefer one explicit command surface over alias convenience.
```

Goldens should have passports:

- Scope: where this applies.
- Confidence: how direct the evidence is.
- Stability: whether this seems enduring or situational.
- Counterexamples: when not to apply it.
- Source: session, quiz card, code review, explicit rule, or task outcome.
- Backtest status: whether it predicts past decisions.

Meme:

> Golden labels are boundary markers, not commandments.

### 4. Phantom Task Forge

To collect golden data outside a real task, generate phantom tasks: fake but realistic task cards that force the same decisions real work would force.

A phantom task should be:

- Plausible in the current repo.
- Small enough to answer in under a minute.
- Targeted at one or two uncertain axes.
- Concrete enough to avoid abstract preference theater.
- Designed around a boundary, not an obvious average case.

Example phantom cards:

```text
Card: autosearch export format

We are adding `autosearch export` so another tool can consume slices of session history.

Option A: stream JSONL to stdout by default.
Option B: write parquet files under ~/.auto/search/export by default.
Option C: produce markdown because agents will read it most often.

Which default should we choose, and what condition would make you switch?
```

This targets:

- CLI output defaults.
- Machine consumption vs agent readability.
- Canonical data vs derived views.

```text
Card: ETL raw preservation

A raw Claude transcript contains noisy tool output and some malformed blocks.

Option A: rewrite the raw copy into a cleaned canonical version.
Option B: preserve raw bytes exactly, then derive normalized parquet and truncated views.
Option C: store only the normalized version because it is easier to search.

Which option is correct for auto-stack?
```

This targets:

- Data integrity.
- Reconstructability.
- Optimization layers.

```text
Card: stale CLI alias

`autoenv update` still works, but the docs now say `autoenv upgrade`.

Option A: keep both forever.
Option B: keep `update` with a warning for one release.
Option C: remove `update` now and fail with a remediation hint.

What should happen for internal tools? What would change your answer for a public release?
```

This targets:

- Backwards compatibility.
- Explicit CLI surfaces.
- Remediation quality.

```text
Card: agent asks or hedges

The user asks: "make the report more useful".

Option A: ask what useful means.
Option B: infer likely gaps from previous reports and edit directly.
Option C: make a reversible draft with assumptions listed at the top.

Which behavior should the agent choose when the edit is cheap and reversible?
```

This targets:

- Question threshold.
- Reversibility.
- Autonomy.

The system can generate these from actual materials:

- Recent tasks.
- Git history.
- Review comments.
- Failed builds.
- Repeated user corrections.
- Existing AGENTS/CLAUDE rules.
- Session search clusters.
- Files with high read/edit churn.

Command:

```bash
autoquestion forge --target e2e_test_realism --count 10 --repo . --json
```

Meme:

> Phantom tasks are wind-tunnel smoke. They make invisible preference currents visible.

### 5. Quiz Modes

Different uncertainty shapes need different quiz mechanics.

**Lightning cards**

A quick forced choice with optional rationale.

Use when the axis is known and the choice is common.

```text
For CLI list commands, default output should be:
A. JSON
B. table text
C. depends on command
```

**Boundary cards**

Ask where the preference flips.

```text
At what point should a CLI command stop returning partial valid results and fail entirely?
A. Any invalid item exists.
B. Invalid filter input exists, but corrupt stored items can be reported alongside valid output.
C. Always return what can be returned.
```

**Invariant cards**

Ask what must never be violated.

```text
Which constraint is non-negotiable for auto-etl?
A. Never modify raw copied transcripts.
B. Never emit invalid JSON to stdout.
C. Never lose file contents from tool reads/writes.
```

This is useful because invariants often matter more than preferences.

**Regret cards**

Ask which failure would be worse.

```text
Which mistake is more expensive?
A. Agent asks one unnecessary clarification.
B. Agent implements the wrong architecture for two hours.
```

Then vary the cost:

```text
What if the implementation takes only five minutes?
What if the question interrupts deep work?
What if the change is easy to revert?
```

This learns regret curvature.

**Pairwise draft cards**

Show two requirement fragments and ask which one better captures the user's intention.

```text
A. The command should support JSON output.
B. The command must default to parseable JSON on stdout and send diagnostics to stderr.
```

This teaches specificity standards.

**Conjoint cards**

Mix attributes to infer weights.

```text
Choose one implementation plan:

A. Fast prototype, weak tests, high learning value.
B. Slower implementation, strong e2e tests, less exploration.
C. Medium implementation, unit tests only, clear rollback path.
```

Over many cards, the system can infer tradeoff weights rather than isolated answers.

**Adversarial cards**

Present options that each satisfy one explicit rule and violate another.

```text
The user wants both autonomy and careful clarification.

A. Ask before editing because intention is ambiguous.
B. Edit directly because changes are reversible.
C. Draft two alternatives and let the user choose.
```

This reveals rule precedence.

**Calibration cards**

Ask how stable the answer is.

```text
How stable is this preference?
A. Global and durable.
B. Repo-specific.
C. Task-specific.
D. Weak preference; ask again near the boundary.
```

This prevents overfitting.

### 6. Active Learning For User Preference Space

The system should not ask quiz questions in a fixed order. It should choose the next card with active learning.

At any point, the user model has a posterior distribution over decision policies. A good question is one expected to reduce posterior uncertainty the most, weighted by future task value.

Informally:

```text
next_card = argmax(card) expected_information_gain(card)
                      * expected_future_use(card)
                      * regret_reduction(card)
                      / annoyance_cost(card)
```

This is the no-task equivalent of orthogonal questioning.

During a real task, the question objective is:

```text
Minimize uncertainty needed to continue safely.
```

During a calibration quiz, the question objective is:

```text
Maximize long-term reduction in expensive future misunderstandings.
```

The card generator should prefer questions whose possible answers land in different downstream behaviors. If every possible answer leads to the same implementation, the card is low value.

Meme:

> A question is only real if different answers cause different work.

### 7. D-Optimal Phantom Tasks

There is a more mathematical version.

Represent each phantom task as a vector of decision-axis loadings:

```text
card_17 = 0.8 * cli_output_default
        + 0.6 * downstream_machine_consumption
        + 0.3 * docs_vs_runtime_contract
```

The user answer gives labels along a combination of axes. The system should choose a batch of cards whose vectors are as independent as possible and cover high-uncertainty directions.

That is experimental design: choose probes that maximize the volume of uncertainty removed.

Practical approximation:

1. Generate 100 candidate phantom cards.
2. Embed each card and tag likely decision axes.
3. Estimate answer impact by simulating plausible user responses.
4. Penalize redundancy with already selected cards.
5. Select 5 cards that cover the most high-regret uncertainty with minimal overlap.

Command:

```bash
autoquestion quiz --budget 5 --target high-regret --selection d-optimal
```

Output:

```text
Selected 5 cards covering 11 uncertain axes.
Estimated future regret reduction: 38%.
Redundancy score: 0.12.
Annoyance budget: low.
```

This is the quiz version of Gram-Schmidt.

Earlier we had Question Gram-Schmidt for live clarification. Here we have Phantom Gram-Schmidt: generate fake work items whose decision vectors are maximally non-overlapping.

### 8. Item Response Theory For Agent Questions

Borrow a model from educational testing.

In item response theory, each question has properties:

- Difficulty: how hard it is for the person to answer.
- Discrimination: how well it separates different latent traits.
- Guessability: how likely random answers are.

For user preference quizzes, a card has analogous properties:

- Cognitive load: how annoying or complex it is.
- Discrimination: how much it separates possible user policies.
- Realism: how likely the answer transfers to real work.
- Boundary sharpness: whether the answer reveals a threshold.
- Scope clarity: whether the answer can be safely applied later.

A card like "Do you prefer quality or speed?" has low discrimination because everyone says "it depends".

A card like "For an internal auto-stack CLI used mostly by agents, should diagnostics ever go to stdout in JSON mode?" has high discrimination and high transfer.

The system should learn which card types work on this user. If the user gives long, thoughtful answers to boundary cards but short answers to abstract cards, the quiz engine should adapt.

Meme:

> Do not ask personality-test questions. Ask compiler-error questions for intent.

### 9. The Golden Quiz Loop

The end-to-end process:

1. Mine historical sessions for decisions, corrections, rework, and explicit rules.
2. Build a decision-axis map.
3. Score blind spots by uncertainty, frequency, regret, and contradiction.
4. Generate phantom task cards aimed at the top blind spots.
5. Select a non-redundant batch under an annoyance budget.
6. Ask the user 3-7 cards in a quickfire quiz.
7. Convert answers into scoped golden decision labels.
8. Backtest labels against historical sessions.
9. Promote stable labels into repo rules, skills, or agent memory.
10. Decay or retest labels when evidence conflicts.

Commands:

```bash
autoquestion quiz --profile current-user --budget 5 --target high-regret --repo .
autoquestion answer <card-id> --choice B --rationale "..."
autoquestion goldens list --scope auto-stack
autoquestion goldens backtest --since 180d
autoquestion promote --golden golden_20260526_001 --to AGENTS.md
```

This should feel less like a questionnaire and more like sharpening a compiler.

### 10. Preference Topology

Enough golden quiz data creates a topology of the user's preferences.

Instead of flat rules:

```text
Default to JSON.
```

The system learns regions:

```text
Default to JSON for operational CLI commands.
Use human-readable markdown for `quickstart` and `docs` commands because their primary consumer is an agent or human reading instructions.
Use JSONL for streamable machine ingestion.
Use parquet for canonical analytical datasets.
```

This is much more useful than a single preference because it encodes boundaries.

Other topology examples:

```text
Use checked-in fixtures for deterministic e2e tests.
Use live repo history only when validating integration with git semantics.
Use temp mock data for per-test mutation.
Never check generated throwaway data into git.
```

```text
Ask the user when a decision is irreversible, external, expensive, or changes product direction.
Proceed autonomously when the change is local, reversible, and consistent with existing rules.
Create a draft with assumptions when the task is ambiguous but cheap to inspect.
```

The product should visualize this as regions and boundary lines, not just memories.

Command:

```bash
autoquestion topology --axis agent_autonomy --format markdown
```

Possible output:

```text
Agent autonomy topology

Proceed without asking:
- Local reversible edits.
- Explicitly requested implementation work.
- Changes directly implied by existing AGENTS rules.

Ask before acting:
- Destructive git operations.
- External service side effects.
- Ambiguous product direction.
- Conflicting historical rules.

Draft instead of asking:
- User asked for ideation or prose expansion.
- Multiple plausible framings exist.
- The artifact can be reviewed cheaply.
```

This is a latent-space atlas.

### 11. Backtesting Goldens

A golden label is only valuable if it predicts real decisions.

After collecting quiz answers, replay old sessions and ask:

- Would this golden have changed the agent's behavior?
- Would that change have matched the user's correction?
- Did it avoid a question, avoid rework, or improve output quality?
- Did it overgeneralize and cause a bad assumption?

Backtest result schema:

```json
{
  "golden_id": "golden_20260526_001",
  "sessions_tested": 143,
  "applicable_cases": 19,
  "correct_predictions": 16,
  "incorrect_predictions": 1,
  "ambiguous_cases": 2,
  "estimated_questions_saved": 7,
  "estimated_rework_avoided": 4,
  "overreach_examples": ["session_abc123"]
}
```

A rule that cannot survive backtesting should stay as a weak preference, not become an instruction.

Meme:

> A golden that cannot predict the past should not steer the future.

### 12. The Synthetic Workbench

Phantom tasks can become a workbench for agent evaluation.

For each phantom task, store:

- The task card.
- The target axes.
- The user's answer.
- The expected agent behavior.
- Anti-behaviors that should fail.
- Example implementation decisions.

Then use these as eval cases for agents.

Example:

```json
{
  "eval_id": "eval_cli_stdout_json_001",
  "phantom_task": "Add a list command that reports valid and invalid items.",
  "target_axes": ["cli_output_default", "partial_results", "stderr_diagnostics"],
  "golden_behavior": [
    "stdout remains parseable JSON",
    "valid items are returned when possible",
    "validation errors include remediation hints",
    "process exits non-zero if any invalid items exist"
  ],
  "fail_behaviors": [
    "prints warnings to stdout before JSON",
    "drops valid items because one item is invalid",
    "returns string errors without structured fields"
  ]
}
```

This closes a loop:

```text
quiz answer -> golden label -> backtest -> eval case -> future agent selection/training
```

The user's taste becomes executable.

### 13. No-Task Data Collection Without Being Annoying

The failure mode is obvious: the system becomes a nagging preference quiz.

Mitigations:

- Use an annoyance budget.
- Ask only high-EV cards.
- Batch questions into explicit calibration sessions.
- Let the user answer with one keystroke plus optional rationale.
- Retire axes once confidence is high.
- Prefer cards generated from real repo history.
- Show why each card was asked.
- Convert answers into visible reusable artifacts.

Example quiz UI:

```text
autoquestion quiz --budget 5 --why

Card 1/5: CLI alias removal
Why this card: high uncertainty on backwards compatibility boundary; 14 recent CLI changes; AGENTS says remove deprecated flags, but release boundary is unknown.

Question:
An internal command has an old alias still used in one stale doc. Remove now or keep with warning?

A. Remove now with remediation hint.
B. Keep with warning for one release.
C. Keep indefinitely.
D. Depends; ask during task.
```

This makes the quiz respectful: every question must justify its existence.

Meme:

> Every question spends user attention. Make it show a receipt.

### 14. Decision Boundary Cards Beat Preference Questions

The system should almost never ask:

```text
Do you prefer speed or quality?
```

It should ask:

```text
A change can ship today with unit tests only, or tomorrow with e2e coverage. The command mutates config files. Which do you choose?
```

Then ask a boundary variant:

```text
What if the command only reads files?
What if the e2e test requires brittle timing?
What if this is an internal prototype?
```

This reveals the shape of the policy. The first answer is a point. The variants estimate the boundary.

Meme:

> Do not quiz the answer. Quiz the boundary.

### 15. User Latent-Space Calibration As A Product

This could become a first-class workflow:

```bash
autoquestion calibrate --repo . --minutes 5
```

Output:

```text
Calibration complete

Questions answered: 6
New goldens: 5
Updated goldens: 1
Axes improved:
- agent_autonomy: confidence 0.61 -> 0.78
- e2e_test_realism: confidence 0.34 -> 0.69
- backwards_compatibility: confidence 0.52 -> 0.81

Estimated impact:
- 11 fewer clarification questions over next 30 tasks
- 4 fewer likely rework loops
- 3 new eval cases generated
```

The pitch:

```text
Spend five minutes now to remove fifty minutes of future agent hesitation and rework.
```

This is meaningfully different from memory. Memory passively stores what happened. Calibration actively designs interactions to discover what matters.

### 16. Synthetic User Twins

Once there is enough golden data, create a lightweight user twin for simulation.

Not a full personality clone. A decision oracle.

Input:

```text
Task proposal + current repo context + candidate agent action
```

Output:

```json
{
  "likely_accept": false,
  "reason": "Violates JSON stdout convention and lacks remediation hint.",
  "relevant_goldens": ["golden_cli_json_stdout", "golden_error_remediation"],
  "ask_user_if": "The command is intended primarily for humans rather than agents."
}
```

This can run before the agent asks the real user. If the twin is confident, proceed. If not, ask a focused question or generate a hedge.

This is useful for:

- Requirement drafting.
- Plan review.
- PR review preflight.
- Skill generation.
- Agent behavior evaluation.
- Choosing whether to ask the user.

Meme:

> Build a small oracle for decisions, not a fake human.

### 17. Blind-Spot Mining From Session Data

How to find axes automatically:

**Correction mining**

Search for places where the user says variants of:

- "No, I meant..."
- "Don't do that"
- "Ignore it"
- "Think deeper"
- "That doesn't qualify"
- "Be more specific"
- "Use X instead"

Cluster the corrected behavior. Each cluster suggests a missing axis.

**Rework mining**

Find files edited repeatedly in one workflow. Repeated edits may indicate unstable requirements or poor initial assumptions.

**Question outcome mining**

Find questions the agent asked and measure whether they changed the work. Questions with no downstream effect are low-value. Questions followed by major changes reveal important axes.

**Abandoned branch mining**

If a task direction is dropped, infer the assumption that made it wrong.

**Review-comment mining**

Review comments are compressed preference labels. A repeated review comment theme is a decision axis begging to become a golden.

**AGENTS conflict mining**

Find pairs of rules that can conflict:

- Be autonomous vs ask clarifying questions.
- Default JSON vs docs/quickstart readability.
- Fail fast vs return partial valid results.
- Preserve raw data vs normalize for search.

Generate adversarial cards for those conflicts.

Command:

```bash
autoquestion mine --source autosearch --repo . --emit axes --json
```

### 18. The Difference Between Rules, Goldens, And Evals

The system needs three layers.

**Rules**

Human-readable instructions used directly by agents.

Example:

```text
In JSON mode, stdout must contain only parseable payload data. Diagnostics go to stderr.
```

**Goldens**

Scoped labeled decisions with evidence and counterexamples.

Example:

```text
When designing auto-stack CLI commands, choose parseable JSON stdout by default because downstream agents consume it.
```

**Evals**

Executable tests that check whether agents apply the rule in context.

Example:

```text
Given a task to add a list command with validation warnings, the agent must design stdout/stderr separation correctly.
```

Flow:

```text
quiz -> golden -> backtest -> rule -> eval -> skill
```

Skipping layers causes failure:

- Rule without golden: may be ungrounded.
- Golden without eval: may not affect behavior.
- Eval without rule: may be hard to generalize.
- Memory without scope: may become superstition.

### 19. What Makes This 10x Useful

The productivity gain is not that quizzes are faster than asking during tasks. The gain is that quiz answers can be reused across many tasks and compiled into the agent's behavior.

If one five-minute calibration session yields:

- 5 goldens.
- 3 rule updates.
- 4 eval cases.
- 1 improved skill.
- 10 fewer future clarifications.
- 3 avoided rework loops.

Then the calibration compounds.

The deepest opportunity is a flywheel:

```text
sessions create traces
traces reveal blind spots
blind spots generate quiz cards
quiz cards create goldens
goldens improve agents
good agents create cleaner traces
cleaner traces reveal sharper blind spots
```

This turns user interaction into training data without needing the user to label transcripts manually.

### 20. MVP Shape

Build the first version brutally simply.

Files:

```text
.auto/question/axes.jsonl
.auto/question/cards.jsonl
.auto/question/goldens.jsonl
.auto/question/backtests.jsonl
```

Commands:

```bash
autoquestion axes seed --repo .
autoquestion blindspots --repo .
autoquestion forge --axis e2e_test_realism --count 10
autoquestion quiz --budget 5
autoquestion goldens backtest --since 90d
autoquestion goldens export --format agents-md
```

Manual seed axes from existing repo rules first. Do not wait for perfect automatic inference.

Seed examples:

```json
{"axis_id":"cli_json_stdout","scope":"auto-stack/cli","description":"Whether operational CLI commands default to parseable JSON stdout."}
{"axis_id":"stderr_diagnostics","scope":"auto-stack/cli","description":"Whether diagnostics and remediation hints are kept off stdout in JSON mode."}
{"axis_id":"raw_data_preservation","scope":"auto-stack/data","description":"Whether raw session data is preserved exactly before derived transforms."}
{"axis_id":"question_threshold","scope":"agent_behavior","description":"When an agent should ask the user instead of proceeding with assumptions."}
{"axis_id":"phantom_task_realism","scope":"autoquestion","description":"How realistic synthetic tasks must be before their answers become goldens."}
```

First useful milestone:

```text
Given 20 manually seeded axes and 50 mined historical examples, generate 10 phantom task cards, ask 5, store goldens, and backtest whether those goldens would have predicted at least 10 prior user corrections.
```

This is buildable without a new model.

### 21. Final Construct: The Intent Wind Tunnel

The earlier document proposed a workflow wind tunnel for testing agents against simulated tasks. This addendum implies a more specific thing: an intent wind tunnel.

An intent wind tunnel does not test whether code compiles. It tests whether a proposed agent behavior bends correctly under the user's preference currents.

Inputs:

- Historical traces.
- Decision axes.
- Blind-spot map.
- Phantom tasks.
- Golden labels.
- User-twin oracle.

Outputs:

- Better questions.
- Fewer questions.
- More accurate requirements.
- Scoped rules.
- Agent evals.
- Updated skills.

Meme set:

```text
Blind Spot Cartography: map where the agent does not know the user's policy.
Golden Oracle Harvest: turn deliberate probes into reusable decision labels.
Phantom Task Forge: create fake work to extract real preferences.
Decision Tasting Menu: let the user sample tiny task bites.
Boundary Cards: ask where the rule flips.
Regret Cards: ask which wrong turn hurts more.
Invariant Cards: ask what must never break.
Preference Topology: learn regions, not slogans.
Intent Wind Tunnel: test agent behavior against the user's decision currents.
```

Most important sentence:

> Ask tiny fake tasks to prevent huge real rework.

