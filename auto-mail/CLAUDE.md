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
- **Handle** — a relative alias for an Address (`#parent`), resolved at send
  time and never stored.
- **Subagent** — an in-process child agent, the only sender that may use
  `#parent`.

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

## Relative handles and the Subagent marker

`auto mail send --to '#parent'` is the one Handle that exists. It resolves to the
Address of the Subscription held under the caller's own Binding — the from
ladder's rung 2, asked with the Binding the caller computed for itself. `#` is
reserved for Handles, so a Handle can never be stored and an Address can never
begin with one; `subscribe`, `list --address` and `send --from` all reject one.
Four refusals, four sentinels, four different fixes: see `mail/handle.go`.

Only an **in-process Subagent** may resolve it, and what makes a caller one is a
**marker** the hook wrote: `~/.auto/mail/alpha-agents/<binding-hash>/<id-hash>`,
one file per `agent_id`, written on `PreToolUse`/`PostToolUse` and removed by
that agent's own `SubagentStop`, with a 15-minute TTL as a crash backstop only.
The directory name is T1's flag name — the same hash of the same Binding pair —
because the marker and the pending flag join on the same key, and two spellings
of one key is how they would eventually disagree. `auto mail reset` removes the
markers with the store: a stale marker makes `#parent` *resolve* rather than
refuse, which fails plausibly instead of loudly.

Three rules that are load-bearing rather than stylistic:

- **The hook never opens the store, and never touches the marker path for an
  agent with no `agent_id`.** An ordinary tool call costs exactly what it cost
  before markers existed.
- **One file per `agent_id`, written by rename.** A single shared slot lets a
  parent tick erase a live child's marker; a truncating write lets a refresh
  briefly make one Subagent look like none.
- **The Sender is established caller-side** (`mail.CallerSender`) and passed
  into `Send`, never derived inside the Client — from T3 the Client may be an
  RPC hop, and "who am I" is a question about the *caller's* filesystem.

A Subagent's mail carries envelope `attributes`: `senderKind` always, plus
either `senderAgentType` (exactly one Subagent live and it has a name) or
`senderAmbiguous: true`. The name is never guessed — with several live the
sending process has no id of its own, and a sibling's name is worse for a
supervisor than no name. Attributes follow the *sender*, not the Handle, and are
omitted entirely for anyone who is not a Subagent, so an ordinary delivery still
prints exactly the four keys it always did.

## Layout

```
auto-mail/
├── mail/                # THE ONLY EXPORTED DOMAIN API — the Client seam
│   ├── handle.go        # the `#` reservation and the four refusals
│   └── subagent.go      # the hook-written marker and CallerSender
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
surface. Waking an idle agent, and the RPC client that makes `mail.Client` have
a second implementation, are T3's — which is why the conformance suite asserts
the handle and attribute contracts at the interface rather than only against the
direct client.

Every Handle other than `#parent` is parked, not planned: the `#` prefix is
reserved for the whole family, so `#children` or `#new` can be added later
without an escape from an address space that already allowed them.
