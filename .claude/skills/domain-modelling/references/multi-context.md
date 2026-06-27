# Multiple contexts and the CONTEXT-MAP

Most repos need a single `docs/concepts/UBIQUITOUS_LANGUAGE.md`, and you should default to
that. Reach for multiple contexts only when one flat glossary genuinely starts fighting
itself — premature splitting just adds ceremony and cross-references nobody maintains.

## When to split

Split into multiple contexts when **the same word correctly means different things in
different parts of the system** and forcing one definition would be a lie. The textbook case:
"Account" means a login in the identity area but a ledger in the billing area — both are right,
in their own context. When you find yourself writing "Account (in billing)" vs "Account (in
auth)", that's the signal the domain has more than one *bounded context*.

Do **not** split just because the project is large, or has many terms, or has several
directories. Size alone is fine in one glossary with `##` groupings. Split only for genuine
meaning-conflicts across areas.

## How to structure it

Keep `docs/concepts/UBIQUITOUS_LANGUAGE.md` as the entry point and turn its top into a **map**
that points to each context's own glossary:

```md
# Ubiquitous Language — Context Map

This project spans multiple bounded contexts; each owns its own vocabulary. A word may mean
different things in different contexts — always resolve terms within a context.

## Contexts

- [Ordering](../../src/ordering/CONTEXT.md) — receives and tracks customer orders
- [Billing](../../src/billing/CONTEXT.md) — invoices and payments
- [Identity](../../src/identity/CONTEXT.md) — accounts, login, permissions

## Relationships

- **Ordering → Billing**: Ordering emits `OrderPlaced`; Billing consumes it to raise an Invoice.
- **Ordering ↔ Identity**: share `CustomerId` only.
- ⚠️ **"Account"** means a *login* in Identity but a *ledger* in Billing. Never use it bare.
```

Each per-context file is just a normal glossary in the [references/language-format.md](references/language-format.md)
format, scoped to that one area. Place it wherever that context's code lives (e.g.
`src/ordering/CONTEXT.md`) so it sits next to what it describes.

## Working with a multi-context repo

- When resolving or challenging a term, first infer **which context** the current topic belongs
  to, then resolve within that context's glossary. If it's ambiguous, ask the user which
  context they mean — that ambiguity is often itself a sign of leaking concepts.
- Record cross-context relationships (events, shared types, naming collisions) in the map's
  **Relationships** section. The collisions — the same word meaning two things — are the most
  valuable lines in the whole map, because they're exactly what causes confusion in
  conversation and code review.
