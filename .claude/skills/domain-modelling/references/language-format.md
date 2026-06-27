# UBIQUITOUS_LANGUAGE.md format

The language file is a glossary and nothing else — not a spec, not a scratchpad, not a
home for implementation decisions. It captures the canonical word for each domain concept
so that humans, code, and agents all speak the same language.

It lives at `docs/concepts/UBIQUITOUS_LANGUAGE.md` and is linked from the root `CLAUDE.md`
with an `@` reference so every agent picks it up automatically.

## Structure

```md
# Ubiquitous Language

The canonical vocabulary for {project}. When a concept below has a canonical term, use it;
treat the words under _Avoid_ as wrong.

## {Optional grouping, e.g. Ordering}

**Order**:
A confirmed request from a customer for one or more items.
_Avoid_: Purchase, transaction, cart
_Has_: one Customer, many Line Items

**Line Item**:
A single item-and-quantity within an Order.
_Avoid_: Order row, entry

**Invoice**:
A request for payment sent to a customer after delivery.
_Avoid_: Bill, payment request
_Has_: one Order

**Customer**:
A person or organization that places orders.
_Avoid_: Client, buyer, account
_Has_: many Orders
```

Each entry is: the **term** (bold), a one-or-two-sentence definition, an `_Avoid_:` line, and —
for entities that relate to other entities — an optional `_Has_:` line.

- The `_Avoid_:` line lists the rejected synonyms. It's the active ingredient that lets the
  skill catch drift later, so never omit it (write `_Avoid_: —` if there genuinely are no
  competing words yet).
- The `_Has_:` line captures relationships to *other canonical terms* using only `one` and
  `many` — e.g. `_Has_: one Customer, many Line Items`. This is the conceptual shape of the
  domain, not a database schema: no foreign keys, no join tables, no nullable/optional
  modelling, no attributes or columns. Just which entities own or contain which. Omit it for
  value-like terms that don't have relationships (e.g. a money concept). Every entity named on
  a `_Has_:` line must itself be a term defined in the glossary — relationships only ever point
  at the ubiquitous language, never at undefined nouns.

## Rules

- **Be opinionated.** When several words exist for one concept, pick the best and list the
  rest under `_Avoid_`. A glossary that refuses to choose is useless.
- **Keep definitions tight.** One or two sentences. Define what the thing IS, not what it
  does or how it's implemented.
- **Only domain-specific terms.** General programming concepts (timeout, retry, cache, DTO)
  don't belong even if the project leans on them heavily. Before adding a term, ask: is this
  unique to *this* project's domain, or generic engineering vocabulary? Only the former.
- **Group when clusters emerge.** Use `##` subheadings once natural groupings appear (e.g.
  Ordering, Billing). A flat list is fine while the domain is small and cohesive.
- **No implementation detail.** Class names, table names, and library choices belong in code
  and ADRs, not here.
- **Keep relationships conceptual.** `_Has_:` lines use only `one`/`many` between defined
  terms. The moment you're tempted to write a foreign key, a cardinality like `0..*`, an
  optional/nullable flag, or an attribute — stop. That's modelling for code or SQL, and it
  belongs there, not in the glossary. The glossary answers "what relates to what", not "how
  it's stored".

## Optional: multiple contexts

Most repos need one file. If the project spans clearly separate domains (e.g. a billing
engine and a content CMS in one monorepo) you may split into per-area files and add a short
map at the top of `UBIQUITOUS_LANGUAGE.md` pointing to each. Don't reach for this until a
single flat glossary genuinely starts fighting itself — premature splitting just adds
ceremony. See [references/multi-context.md](references/multi-context.md) for when and how.
