# ADR format (decision records)

Decisions that come out of a grilling are recorded as **numbered ADRs** — one short file per
decision in `docs/adr/`, named `NNNN-slug.md` (`0001-event-sourced-orders.md`,
`0002-postgres-for-write-model.md`, …). Create `docs/adr/` lazily — only when the first ADR is
actually written.

## Template

```md
# {Short title of the decision}

{1–3 sentences: the context, what was decided, and why.}
```

That's the whole thing. An ADR can be a single paragraph — the value is in recording *that* a
decision was made and *why*, not in filling out a form. Use the project's ubiquitous language
in the wording if a glossary is present.

## Optional sections

Add these only when they earn their place; most ADRs won't need any of them.

- **Status** (`proposed | accepted | deprecated | superseded by ADR-NNNN`) — when a decision is
  likely to be revisited.
- **Considered options** — only when the rejected alternatives are worth remembering.
- **Consequences** — only when there are non-obvious downstream effects to flag.

## Numbering

Scan `docs/adr/` for the highest existing number and increment by one. Never reuse or
renumber — superseding is done with a new ADR that references the old one.

## When to write one

A grilling surfaces many answers but not all are ADR-worthy. Write an ADR only when **all three**
are true (this is exactly what the reversibility probe is for):

1. **Hard to reverse** — changing your mind later has real cost.
2. **Surprising without context** — a future reader will look at the result and wonder "why on
   earth did they do it this way?"
3. **A real trade-off** — there were genuine alternatives and one was chosen for specific
   reasons.

If a decision is easy to reverse, skip it — you'll just reverse it. If it's not surprising,
nobody will wonder. If there was no real alternative, there's nothing to record beyond "we did
the obvious thing." Everything else from the grilling that isn't ADR-worthy still belongs in the
session's closing summary and the grilling doc — just not as an ADR.

## Don't lose the near-misses

Decisions that *aren't* settled but *are* important become **open questions** in the grilling
doc's summary, not ADRs. An assumption the user knowingly accepts as a risk can be a one-line
ADR ("We assume X; if it's false, Y breaks") — recording an accepted risk is often the most
valuable record of all.
