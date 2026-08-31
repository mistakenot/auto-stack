# automail

A durable, addressed, at-least-once channel between agents on one host. Ships as
`auto mail` in the unified binary.

The stack already carries events *outward* (`auto-shared/bus` broadcasts what
happened to whoever is listening). Mail is the other direction: a message **to a
named recipient** that survives until someone explicitly acks it.

## Vocabulary

Canonical terms live in `docs/concepts/UBIQUITOUS_LANGUAGE.md` (§ Mail):

- **Mail** — the stored unit of an addressed, durable message between agents.
- **Address** — the virtual, free-form name a Mail is sent to. No physical
  identity is derivable from it.
- **Subscription** — a durable reader of an Address, with its own cursor and ack
  state.
- **Delivery** — one Subscription's copy of one Mail, plus its read/ack state.
- **Binding** — the opaque `(manager, target)` pair a Subscription is currently
  held by.

**Nothing here is called a message.** That word already names "a single
role-tagged exchange within a Session". The one exception is the `--message`
flag on `send`, which names the *body's own field* (`body: {"message": …}`), not
the entity. `mail/mail_test.go` enforces this.

## Delivery contract

**At-least-once, unordered. Consumers must be idempotent on the mail id.**
Nothing here promises exactly-once or ordering, and no test asserts ordering, so
the suite does not encode the accidental guarantee the store happens to provide.

Reading never retires mail: `ack` is always a separate explicit call, and it
reports `wonTransition` so a caller knows whether *this* call was the one that
transitioned the delivery.

## Alpha

This is alpha, and the marker is in the artifact rather than only in prose: the
store is `~/.auto/mail/alpha-store.db` and every event type is `alpha.mail.*`.

**No upcasters, no migrations, no compatibility guarantee — the store may be
wiped on upgrade.** `auto mail reset` is a supported operation, not a
workaround: it removes `alpha-store.db` **and** `alpha-flags/` and reports what
it removed, refusing a store that still holds events unless `--yes` is given.
Nothing outside mail may depend on the store's shape.

`auto-mail/conformance/seam_test.go` makes that last sentence executable. It is
scoped to the **mail store**, not to SQLite: a file is a violation when it names
the store (`alpha-store.db`, `alpha-flags`, an `.auto/mail` path) *and* reaches
for a database in the same file, or when it imports `auto-mail/internal/...`
from outside this module. Naming the store without opening it stays legal —
`auto-cli`'s hook test and the harness scenario both assert *about* the store
from outside — and a repo-wide ban on `modernc.org/sqlite` would fail
`auto-watch` and `auto-search`, which keep their own stores.

## Layout

```
auto-mail/
├── mail/                # THE ONLY EXPORTED DOMAIN API — the Client seam
├── rootcmd/             # mounting facade for `auto mail` (no domain logic)
├── cmd/automail/        # standalone binary entry point
└── internal/
    ├── app/             # stdout/stderr/cwd context
    ├── cli/             # cobra commands
    ├── config/          # ~/.auto/mail paths
    └── store/           # the event log and its projections
```

This is one deliberate exception to `docs/auto-package-patterns.md`, which says
"all implementation lives under `internal/` (no public API exports)". Mail is
explicitly a *seam* other tools consume — `auto-cli`'s hook is its first
consumer — so exactly one domain package is exported (`auto-mail/mail`),
alongside the mandatory `rootcmd` mounting facade every tool in this monorepo
has. Everything else, most importantly `internal/store`, stays internal and is
therefore unimportable from any other module. That makes "nothing outside the
mail package reads the store" a compile-time property rather than a convention.
**Read this as the reason for the exception, not as licence** — a new package
with no cross-tool seam still puts everything under `internal/`.

## Build

```bash
cd auto-mail
go build ./...
```

The merged `auto` binary is built from the repo root with `make build`.

## Test

```bash
cd auto-mail
go test ./...           # unit + in-process CLI tests, against a temp HOME
go test -race ./...     # the ack race and the concurrent-writer discipline
```

The end-to-end verdict is the `mail-flow` harness scenario, which drives the
real `auto` binary in a container:

```bash
cd harness
uv run harness mail-flow up
uv run pytest tests/mail_flow -v
uv run harness mail-flow down
```

## Not here yet

`quickstart` and `doctor` belong to epic 005's task T4, which owns the adoption
surface. `#parent` and relative handles are T2's; waking an idle agent is T3's.
