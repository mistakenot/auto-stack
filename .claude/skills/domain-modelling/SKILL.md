---
name: domain-modelling
description: "Builds and maintains a project's ubiquitous language — a DDD glossary of canonical domain terms at docs/concepts/UBIQUITOUS_LANGUAGE.md, linked from CLAUDE.md. Use when 'domain modelling', 'ubiquitous language', 'define our domain terms', 'set up the glossary', 'interview me about the domain', or when terminology is fuzzy or inconsistent. Sub-commands: init, interview-user, add-term, audit. Not for API/code reference docs (use revise-readme)."
---

# Domain Modelling

A *ubiquitous language* is a single agreed vocabulary for a project's domain — one canonical
word per concept, shared by the people, the code, and the agents working on it. When the
language is sharp, ambiguity disappears: "Order" means exactly one thing, and synonyms like
"purchase" or "transaction" are recognised as drift, not harmless variety. This skill builds
that glossary and then keeps everyone honest to it.

The glossary lives at **`docs/concepts/UBIQUITOUS_LANGUAGE.md`** and is linked from the root
`CLAUDE.md` with an `@` reference, so every agent loads it automatically. Its format and rules
are in [references/language-format.md](references/language-format.md) — read that file before writing or
editing the glossary, every time, so entries stay consistent. For a fully worked example to aim
at, see [references/example-glossary.md](references/example-glossary.md). If the project spans
multiple bounded contexts (the same word meaning different things in different areas), see
[references/multi-context.md](references/multi-context.md).

## First, orient

Whatever the user asks for, start by checking the current state — it decides everything else:

```
ls docs/concepts/UBIQUITOUS_LANGUAGE.md
```

- **If it exists**, read it fully before doing anything else. You cannot challenge drift,
  add a term, or audit without knowing what's already canonical.
- **If it doesn't exist** and the user wants to do real work (interview, add a term), the
  glossary needs setting up first — run `init` (offer it, don't silently scaffold a repo's
  CLAUDE.md without saying so).

## Sub-commands

Route by what the user asked for. If they named a sub-command (`init`, `interview-user`,
`add-term`, `audit`, `rename`), do that. If they spoke naturally ("help me pin down our domain
terms", "set this up", "is our code consistent with the glossary?"), map it to the closest one.

| Sub-command | When | What it does |
|---|---|---|
| `init` | No glossary yet, or "set up the ubiquitous language" | Scaffolds the file + wires the `@` link and enforcement directive into CLAUDE.md |
| `interview-user` | "interview me", "build out the language", glossary thin | Re-reads doc → researches the repo → interviews the user → fills the glossary |
| `add-term` | "let's add a term", a concept got resolved mid-chat | Captures/resolves one term and writes it in |
| `audit` | "check our code against the glossary", drift suspected | Validates the glossary + scans code/commits for missing or conflicting terms |
| `rename` | "rename X to Y", a canonical term changed | Updates the entry, retires the old word to `_Avoid_`, offers the code rename |

**The bundled `scripts/glossary.py` does the mechanical work** so you don't validate or draw by
eye. After any edit to the glossary, run it:
- `python3 scripts/glossary.py check docs/concepts/UBIQUITOUS_LANGUAGE.md` — structural lint:
  every entry has a definition + `_Avoid_`, every `_Has_:` target is a defined term, no word is
  both canonical and `_Avoid_`, no duplicates. Exit code 1 on errors.
- `python3 scripts/glossary.py diagram docs/concepts/UBIQUITOUS_LANGUAGE.md --write` — refreshes
  a mermaid ER diagram (from the `_Has_:` lines) in a marker-delimited block near the top.

---

### init

Goal: get the glossary file and its `CLAUDE.md` link in place, with as little ceremony as
possible. Don't try to fill it with terms here — that's `interview-user`'s job.

1. If `docs/concepts/UBIQUITOUS_LANGUAGE.md` already exists, say so and stop — offer
   `interview-user` or `add-term` instead. Don't clobber an existing glossary.
2. Otherwise, tell the user what you're about to create and where, and confirm before
   touching `CLAUDE.md` (it's their repo's root config — modifying it unannounced is the kind
   of surprise to avoid). Then:
   - Create `docs/concepts/UBIQUITOUS_LANGUAGE.md` with the header and a short usage note from
     [references/language-format.md](references/language-format.md), and either a couple of obvious
     seed terms or an explicit "(no terms yet — run interview-user)" placeholder.
   - Add an `@` link **and an enforcement directive** to the root `CLAUDE.md`, so every agent
     session doesn't just load the glossary but actively uses it. The directive is what turns a
     passive document into always-on discipline — without it, agents read the file and ignore
     it. Use a line like:
     `- Speak the project's ubiquitous language: see @docs/concepts/UBIQUITOUS_LANGUAGE.md — use the canonical term for each concept and flag any word on an _Avoid_ list.`
     Place it near other doc links if a natural spot exists. Make this idempotent — if a line
     already references the file, leave it alone.
3. Offer (don't impose) the CI/commit-time safety net — wiring `scripts/glossary.py check` into
   a pre-commit hook or CI so malformed entries fail fast. See
   [references/ci-enforcement.md](references/ci-enforcement.md). Only add it if the user agrees.
4. Tell the user the next step is usually `interview-user` to actually populate the language.

### interview-user

This is where the language gets built. The point of interviewing is that the *user* holds
domain knowledge the code can only hint at — what concepts mean, which word is "right", where
two things that look similar are actually different. Your job is to come prepared so their time
is spent on judgment calls, not on explaining what you could have read yourself.

**1. Re-read the glossary.** Always re-read `docs/concepts/UBIQUITOUS_LANGUAGE.md` first (run
`init` if it's missing). You're extending it, not starting from a blank page, and you must not
re-litigate terms already settled.

**2. Research the repo before asking anything.** Mine the actual material for candidate terms
and, crucially, for *conflicts* — the same concept named different ways, which is exactly what
a ubiquitous language exists to kill. Look at:
   - **Code** — module/directory names, core types/classes/entities, domain functions. These
     are the nouns the system is built around.
   - **Commits & PRs** — `git log` messages and PR titles reveal how the team actually talks
     about features, and where wording has shifted over time.
   - **Planning / task docs** — requirements, solution, plan docs (e.g. `docs/`, task folders)
     are pure domain language, often richer than the code.
   - **Existing docs** — README, ADRs, design notes.

   Come out of this with a draft list of candidate terms, proposed definitions, and a list of
   *suspected synonyms/conflicts* to resolve. This draft is the raw material for good
   questions — without it you'll ask generic, low-value things.

**3. Interview with `AskUserQuestion`.** Ask in focused batches (the tool takes up to 4 at a
time), grounded in what you found. Draw on the reusable patterns in
[references/interview-question-bank.md](references/interview-question-bank.md). Strong questions
force a decision:
   - **Pick the canonical word.** "The code uses both `Account` and `Customer` for the buyer —
     which is canonical, and is the other an _Avoid_ synonym or a genuinely different thing?"
   - **Tighten a fuzzy definition.** "When you say 'fulfilment', does that include payment, or
     only the shipping side?"
   - **Probe boundaries with a scenario.** "If a customer returns one item from a three-item
     order, is that a partial Cancellation, a Return, or a Refund? Are those three different
     concepts?"
   - **Confirm or kill a candidate.** "Is `Batch` real domain vocabulary, or just an
     implementation detail that shouldn't be in the glossary?"

   Keep going in rounds until the core domain is covered. It's better to run several short,
   sharp rounds than one giant one.

**4. Write as you resolve.** Each time a term is settled, write it into the glossary in the
[references/language-format.md](references/language-format.md) format — term, tight definition, the
`_Avoid_` list (this is what makes future drift detectable), and, for entities, a `_Has_:` line
capturing its relationships to other terms (`one`/`many` only — the conceptual shape, never a
schema). Capture decisions as they happen rather than batching at the end, so nothing is lost.

**5. Validate, draw, and hand off.** Run `scripts/glossary.py check` to catch any malformed
entry or dangling `_Has_:` link, then `scripts/glossary.py diagram ... --write` to refresh the
ER diagram. Summarise what was added/changed, and from here on speak the language (see
*Speaking the language* below).

### add-term

A lightweight path for when a single concept gets resolved mid-conversation and you don't need
a full interview. Re-read the glossary, write the one term in `language-format.md` form
(definition + `_Avoid_`, plus a `_Has_:` line if it's an entity with relationships), and check
it doesn't conflict with or duplicate an existing entry — if it does, surface the conflict and
resolve it with the user rather than adding a near-twin. Run `scripts/glossary.py check`
afterwards (and `diagram --write` if you added a relationship) so the structure stays sound.

### audit

Check whether the codebase still agrees with the glossary — drift creeps in as code evolves.
First run `scripts/glossary.py check` to catch *internal* problems mechanically (malformed
entries, dangling `_Has_:` links, canonical-vs-`_Avoid_` clashes), then re-read the glossary
and scan code, recent commits, and docs for problems the script can't see:
   - **Missing terms** — domain concepts clearly in use that the glossary doesn't define yet.
   - **Conflicts** — code using a word that's on an `_Avoid_` list, or using a canonical term
     to mean something different from its definition.
   - **Stale entries** — glossary terms that no longer appear anywhere in the project.

Report findings grouped by those buckets, each with file references, and propose concrete
fixes (add term, rename in code, update definition). Let the user decide; then apply the agreed
changes (glossary edits here, code renames only if they ask).

### rename

Renaming a canonical term is the most common — and most error-prone — glossary edit, so do it
in a fixed order rather than ad hoc:

1. **Re-read the glossary** and find the entry for the old term.
2. **Rename the entry** to the new canonical word, keeping its definition, `_Avoid_`, and
   `_Has_` intact.
3. **Retire the old word to `_Avoid_`.** The whole point of renaming is that the old word is
   now wrong — add it to the new entry's `_Avoid_` line so drift back to it gets caught.
4. **Fix references.** Update any other entry whose `_Avoid_` or `_Has_:` line named the old
   term so they point at the new one (a `_Has_: many Orders` must follow an Order→Receipt
   rename). `scripts/glossary.py check` will flag any you miss — run it.
5. **Offer the code rename.** Grep the codebase for the old term and propose the edits, but
   apply them only if the user agrees — renaming live identifiers is their call, not yours.
6. **Refresh the diagram** with `scripts/glossary.py diagram ... --write`.

---

## Speaking the language (active discipline)

Once the glossary exists, it isn't a document you file away — it's how you talk. In every
later conversation about this domain:

- **Use the canonical terms yourself**, in chat, code, comments, and commit messages.
- **Flag drift when you see it.** If the user (or the code) uses a word listed under `_Avoid_`,
  or uses a canonical term in a way that contradicts its definition, surface it plainly and
  propose the canonical term — e.g. *"Heads up: the glossary's canonical term is 'Order', and
  'purchase' is under _Avoid_. Did you mean Order?"* This is the whole point of writing the
  `_Avoid_` lists down; a glossary nobody enforces decays immediately.
- **Treat genuine new distinctions as glossary work, not corrections.** Sometimes the "wrong"
  word is actually a real concept the glossary is missing. When that's the case, don't just
  correct — propose an `add-term`. The user's wording is a signal, not always an error.

Keep this light and helpful, not pedantic: a quick flag and a question, then move on.
