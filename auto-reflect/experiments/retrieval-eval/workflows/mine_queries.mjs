export const meta = {
  name: 'mine-retrieval-queries',
  description: 'Mine realistic playbook-retrieval queries from held-out auto-stack sessions (one sonnet agent per session)',
  phases: [{ title: 'Mine', detail: 'one agent per held-out session extracts retrieval intents + domain guesses' }],
}

let parsed = args
if (typeof parsed === 'string') { try { parsed = JSON.parse(parsed) } catch (e) { parsed = [] } }
const sessions = Array.isArray(parsed) ? parsed : (parsed && parsed.sessions) || []
if (!sessions.length) { return { error: 'no sessions parsed', argsType: typeof args } }

const DOMAIN_VOCAB = ["architecture","aws","bus","ci","cli","dependencies","doc","etl","events","git","go","graph","hooks","monorepo","networking","parquet","property-based-testing","reflect","search","security","skill","testing","ui","watch"]
const TASK_SLUGS = __TASK_SLUGS__

log(`Mining retrieval queries from ${sessions.length} held-out sessions`)

const QUERIES_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['queries'],
  properties: {
    queries: {
      type: 'array',
      items: {
        type: 'object', additionalProperties: false,
        required: ['intent', 'domain_guess', 'topic', 'overlaps_mined_task', 'rationale'],
        properties: {
          intent: { type: 'string', description: 'how an agent would phrase the retrieval need at the START of this work — a short task intent, not a question to the user' },
          domain_guess: { type: 'array', items: { type: 'string' }, description: 'the domain tag(s) a hurried-but-competent agent would plausibly pass to retrieve, from the playbook vocab; empty array if they would likely pass none' },
          topic: { type: 'string', description: 'short label for what this work is about' },
          overlaps_mined_task: { type: 'string', description: 'the slug of a mined task whose topic this clearly overlaps (leakage flag), or "none"' },
          rationale: { type: 'string', description: 'one line: why this is a realistic retrieval need grounded in the session' },
        },
      },
    },
  },
}

function prompt(s) {
  return `You are mining REALISTIC PLAYBOOK-RETRIEVAL QUERIES from one past coding session in the auto-stack repo. These queries will evaluate which retrieval method best surfaces relevant playbook rules — so they must reflect genuine retrieval needs, phrased the way a coding agent would describe work it is ABOUT to start.

Session id: ${s.session_id}  (${s.msgs} messages)
Opening intent (preview): ${s.intent_preview}

DO THIS:
1. Read the session transcript: \`auto search session get ${s.session_id}\`. Skim for what the agent was actually trying to DO — the task(s), the subtasks, the moments where real implementation/debugging began. Tool-call noise can be skipped.
2. Extract 1-3 retrieval queries: for each distinct piece of substantive work, write the intent an agent SHOULD have retrieved playbook rules for at the start. Phrase it as a terse work intent (e.g. "add a --json flag to auto env", "debug why auto ui front-end state doesn't update on file create"), NOT as a question to a human and NOT as a summary of what happened.
3. For each query also give:
   - domain_guess: the domain tag(s) a hurried-but-competent agent would plausibly pass to \`auto reflect retrieve --domain ...\` for this intent. Choose from this playbook vocabulary: ${JSON.stringify(DOMAIN_VOCAB)}. It is FINE (and realistic) to guess imperfectly or to return an empty array if an agent would likely pass no domain. Do not overthink it — guess as a real agent skimming the task would.
   - topic: a short label.
   - overlaps_mined_task: if this work clearly overlaps one of these already-mined tasks, give its slug (leakage flag); else "none". Mined task slugs: ${JSON.stringify(TASK_SLUGS)}.
   - rationale: one line grounding it in the session.

RULES:
- Quality over quantity. If the session is trivial, meta (e.g. just running a skill, a commit, a status check), or has no real retrieval need, return an empty queries array.
- Each intent must be self-contained and retrievable — assume the matcher only sees this one line of text plus the domain_guess.
- Prefer intents that touch the kinds of concerns the playbook covers (go, testing, cli, git, etl, etc.), since those are what retrieval is for.

Return { queries: [...] }.`
}

const results = await parallel(
  sessions.map((s) => () =>
    agent(prompt(s), { label: `qmine:${s.session_id.slice(0, 8)}`, phase: 'Mine', model: 'sonnet', schema: QUERIES_SCHEMA })
      .then((r) => ({ session_id: s.session_id, queries: (r && r.queries) || [] }))
  )
)

const clean = results.filter(Boolean)
const flat = []
for (const r of clean) {
  for (const q of r.queries) {
    flat.push({ ...q, source_session: r.session_id, held_out: true })
  }
}
log(`Mined ${flat.length} queries from ${clean.length} sessions (${flat.filter(q => q.overlaps_mined_task !== 'none').length} flagged as overlapping a mined task)`)
return { queries: flat, sessions_mined: clean.length }
