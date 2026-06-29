export const meta = {
  name: 'oracle-coverage-pilot',
  description: 'Coverage pilot: LLM oracle labels true rule-relevance over the whole 120-rule playbook for ~20 held-out queries',
  phases: [{ title: 'Judge', detail: 'one agent per query grades relevance of all 120 rules' }],
}

const RULES = __RULES__ // [{id, use_when, content, causal_note, domain, rule_type}]
let parsed = args
if (typeof parsed === 'string') { try { parsed = JSON.parse(parsed) } catch (e) { parsed = [] } }
const queries = Array.isArray(parsed) ? parsed : (parsed && parsed.queries) || []
if (!queries.length) { return { error: 'no queries parsed', argsType: typeof args } }
log(`Oracle pilot: judging ${queries.length} queries over ${RULES.length} rules`)

const VERDICT_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['relevant'],
  properties: {
    relevant: {
      type: 'array',
      description: 'ONLY the rules genuinely relevant to this intent. Empty if none apply.',
      items: {
        type: 'object', additionalProperties: false,
        required: ['rule_id', 'grade', 'why'],
        properties: {
          rule_id: { type: 'string' },
          grade: { type: 'integer', enum: [1, 2, 3], description: '1=marginally relevant, 2=relevant, 3=directly on-point' },
          why: { type: 'string', description: 'one line: how this rule applies to THIS intent' },
        },
      },
    },
  },
}

const rulesBlock = JSON.stringify(RULES)

function prompt(q) {
  return `You are a strict RELEVANCE ORACLE building a golden set for evaluating playbook retrieval. Given a coding task intent, decide which playbook rules a competent agent would GENUINELY benefit from retrieving BEFORE starting this work.

TASK INTENT: ${q.intent}
(topic: ${q.topic || "?"})

Below are all ${RULES.length} playbook rules as {id, use_when, content, causal_note, domain, rule_type}:

${rulesBlock}

Judge TRUE relevance over the WHOLE playbook:
- A rule is relevant only if its actual guidance (content/causal_note) APPLIES to this specific intent — it would change or inform what the agent does. Judge the full content, not just use_when.
- Be STRICT. Do NOT include a rule merely because it shares a domain/keyword. A rule about "AWS SigV4 signing" is NOT relevant to a tmux-debugging intent even though both might be tagged cli/go.
- Grade: 3 = directly on-point (clearly would help/prevent a mistake here); 2 = relevant; 1 = marginally relevant (tangential but a careful agent might use it).
- It is correct and expected to return an EMPTY list when no rule genuinely applies — many intents have no matching rule, and that is a real signal. Do not pad.
- IGNORE any notion of domain filtering; judge relevance unconstrained over all rules.

Return { relevant: [ {rule_id, grade, why}, ... ] }.`
}

const results = await parallel(
  queries.map((q) => () =>
    agent(prompt(q), { label: `oracle:${q.query_id}`, phase: 'Judge', model: 'sonnet', schema: VERDICT_SCHEMA })
      .then((v) => ({ query_id: q.query_id, intent: q.intent, domain_guess: q.domain_guess || [],
                      overlaps_mined_task: q.overlaps_mined_task,
                      relevant: (v && v.relevant) || [] }))
  )
)

const clean = results.filter(Boolean)
const withAny = clean.filter((r) => r.relevant.length > 0).length
const totalLabels = clean.reduce((n, r) => n + r.relevant.length, 0)
log(`Judged ${clean.length} queries; ${withAny} have >=1 relevant rule; ${totalLabels} relevance labels total`)
return { qrels: clean, stats: { queries: clean.length, with_relevant: withAny, total_labels: totalLabels } }
