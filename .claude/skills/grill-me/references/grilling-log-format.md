# The grilling log

Questions are asked **live with the AskUserQuestion tool** — fast, in-session, with pickable
options. But every question and the answer it got is **appended to one well-known log** so the
thinking is never lost and you can pick a subject back up later:

`docs/grilling/grilling-log.md` — single file, append-only, newest at the bottom. Create it
lazily on the first grilling.

## Rules

- **Append only.** Never rewrite, reorder, or delete an earlier section. Each new batch of
  questions goes at the bottom.
- **One section per batch.** Each AskUserQuestion call (a round) becomes one section, and it
  **opens with a context line** so a cold reader knows where the questioning came from.
- **Real answers only.** Record what the user actually chose or typed — never the recommended
  option unless they picked it, never an invented answer.

## Format

```md
## {YYYY-MM-DD} — {subject}

**Context:** {1–2 sentences — what prompted this round, the subject under pressure, and the
user's intent/goal. On a follow-up round, say what the previous answers were pushing on, e.g.
"Round 2 — pressing on the vague answer to the rollback question."}

**Q — {short label}:** {the question as asked}
**A:** {the option the user picked, or their free-typed answer}

**Q — {short label}:** {the question}
**A:** {answer}
```

A multi-round grilling just appends several of these sections, each with its own context line.
A whole new subject later appends another `## {date} — {subject}` section to the same file.

## Asking the questions (AskUserQuestion)

- Up to **4 questions per call**. For most questions, give **2–4 options** with the **first one
  recommended** — label it `… (Recommended)` and give a ≤8-word reason tied to the user's intent
  in its description. "Other" is always available, so that's the escape hatch; you don't add a
  "none of these" option yourself.
- **Skip options** for genuinely open questions ("list the failure modes you'd worry about") —
  let the user free-type; fabricated options there cap the thinking.
- Ask the round, then append the Q&A section to the log **before** asking the next round, so the
  log never falls behind the conversation.

## Relationship to ADRs

The log is the raw transcript of *everything* asked and answered. The subset of answers that
clear the ADR bar (hard to reverse + surprising + real trade-off) are *also* written as numbered
ADRs in `docs/adr/` — see [references/adr-format.md](references/adr-format.md). The log is the
firehose; the ADRs are the curated decisions.
