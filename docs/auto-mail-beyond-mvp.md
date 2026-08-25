---
hash: "093fe512"
id: "73ea8a99"
read_when: "extending auto mail past the MVP — fan-out, leases, multi-host, #new agent spawning — or checking whether a proposed mail feature was already considered and deferred"
summary: "The parked backlog for auto mail: every capability designed during the v1 riff but deliberately left out of the MVP, what each depends on, and which v1 constraint keeps it buildable. Includes the #new spawn-on-send address and the Orleans-style activation model."
title: "auto mail — Beyond the MVP"
---

# auto mail — Beyond the MVP

> **Status: PARKED BACKLOG.** Nothing here is committed, scheduled, or designed
> to implementation depth. This is the holding pen for capability we explored
> while scoping [epic 005](epics/epic-005-auto-mail-mvp.html) and deliberately
> cut. Read it before proposing a mail feature — it may already be here, with
> the reason it was deferred.

## Why this doc exists

The MVP is small on purpose. Almost everything below was designed far enough to
confirm it is **additive** — a new table, a new column, a new event type, a new
resolver case — and then dropped. The value of writing it down is not the
feature list; it's the record of *what makes each one still possible*, so a
future change doesn't quietly foreclose one.

## The rule that governs all of it

**Schema you own is cheap to migrate. Call sites you don't own are not.**

Consumers of `auto mail` include prompts, skills, and other agents' habits.
Those can't be grepped, migrated, or even reliably enumerated. So the MVP fixes
only the things that change *what a caller must do* — explicit ack, idempotent
consumers, virtual addresses, ULID ids, the at-least-once contract — and defers
everything that only changes tables.

Every item below is here because it passed that test. If a proposal fails it,
it isn't backlog — it's a v1 decision that needs reopening.

---

## 1. Delivery semantics

### 1.1 Competing consumers (queue mode)

The MVP gives every subscription its own copy of a message (broadcast). The
alternative — several workers competing for each message — is what a swarm
needs to divide work rather than duplicate it.

- **Shape:** a `mode` column on `subscriptions` (`broadcast` | `queue`).
- **Preserved by:** consumer state living in the `deliveries` join rather than
  on the message. Fan-out is extra rows; compete is a column.
- **Groundwork already in v1:** ack is a compare-and-set that reports whether
  the caller won the transition (epic G13). That is precisely the primitive
  competing consumers need — the remaining work is the lease, not the contention
  handling.

### 1.2 Holds / visibility timeout

A worker claims a message so others can't take it while it works. This is SQS's
visibility timeout, Pub/Sub's ack deadline, JetStream's `AckWait` — a
well-trodden pattern, not a novel one.

- **Shape:** `held_until` + `held_by` on `deliveries`, plus `held` / `released`
  / `expired` event types.
- **Do not use a fixed timeout.** Agent work ranges from 30 seconds to hours. A
  fixed hold is guaranteed wrong in both directions: too short and two agents
  do the same work (with a shared checkout, both may commit); too long and a
  crashed worker's task is invisible for the duration.

### 1.3 Heartbeat-extended leases

The fix for 1.2. A short initial hold that extends while the holder is alive.

- **We already have the heartbeat:** every hook fire updates `last_seen` on the
  binding, so a lease can auto-extend from demonstrated liveness with *no
  cooperation from the agent*.
- **Needs a ceiling.** An agent that is alive but stuck on something else would
  otherwise hold the lease forever. Cap with an absolute `max_hold`.
- **Caveat carried from prior incidents:** silence is not death. A quiet but
  healthy agent must not be treated as crashed, and any liveness test needs a
  paired "survives past the timeout" negative case, or the test encodes the bug.

### 1.4 Fencing tokens

At-least-once plus leases makes duplicate work *guaranteed*, not exceptional:
worker A takes a message, runs long, the lease expires, worker B takes it, then
A finishes and acks. You cannot tune this away — that's the exactly-once
impossibility, not a timeout-tuning problem.

- **Shape:** a monotonically increasing token per grant. The holder presents it
  on ack; a stale token means the holder lost the race.
- **Arrives with the lease**, not before — with no leases there is nothing to
  fence. What the MVP carries instead is the *contract* (idempotent consumers),
  which is the part that lives in call sites.

### 1.5 Poison messages and dead-lettering

Worker crashes on a message → lease expires → next worker takes it → crashes →
repeat. One bad message can consume an entire swarm in a loop.

- **Shape:** `delivery_count` on `deliveries`; park after N attempts.
- Cheap, and the failure without it is dramatic.

### 1.6 Ordering

Deliberately never promised, even though a single SQLite store delivers in
order by accident. An `ordering_key` slot is reserved and unused. Promising
order now means every consumer silently depends on it and a distributed
transport breaks all of them at once.

---

## 2. Addressing and activation

### 2.1 `#new` — spawn a fresh agent by mailing it

**The idea:** send to `#new@<project>` and the system creates a new pane in that
project running a fresh agent, which receives the message as its opening
context.

This is the most interesting item in this doc because it completes the Orleans
analogy. Orleans' defining trick is that *calling* a grain activates it — you
never create an actor, you just address one. We have the addressing half in v1;
`#new` is the activation half, in its explicit, caller-driven form.

**Why explicit-first is the right order.** `#new` is caller-controlled, so
nothing spawns by surprise. True implicit activation (2.2) is the same machinery
with the trigger moved, and it's much harder to reason about safety for. Build
the explicit one, learn from it, then decide.

**Design notes:**

- **It's a constructor, not an address.** `#new` names an intent to create a
  recipient, not an existing one. Per the resolve-at-send-time rule, the stored
  envelope must carry the address of the *newly created* agent — never `#new`
  itself — so the mailbox stays readable after the fact.
- **Half the machinery exists.** autowatch already does tmux-backed task
  execution and pane spawning; herdr pane creation is the missing backend.
- **Fork-bomb risk is the headline hazard.** An agent that mails `#new` on every
  error spawns unboundedly, and each spawn is a real agent burning real tokens.
  Needs, at minimum: a max-concurrent-spawn cap, a rate limit, and a
  **spawn-depth counter carried in the envelope** so a spawned agent that mails
  `#new` again terminates rather than recurses.
- **Worktree isolation, not just a pane.** Given that every pane on this host
  already shares the one primary checkout — the documented cause of a HEAD-flip
  incident across eight agents — a `#new` agent should get its own worktree.
  Spawning another pane into the shared checkout scales the existing problem.
- **Who acks?** The spawned agent binds an address and acks on completion. If it
  dies at birth the message is left unacked, which means `#new` is only really
  safe once leases and redelivery (1.2–1.5) exist. Until then, an unacked
  `#new` needs a visible failure rather than silence.
- **What does the new agent get?** The message body is its prompt, but it also
  needs project, worktree, and a reply address. This is the one case where the
  envelope's provenance fields become an *input* rather than a trace.

### 2.2 Implicit activation (full Orleans semantics)

Mail arrives for an address nobody is bound to → the daemon spawns an agent to
bind it. Same machinery as `#new`, but triggered by an unbound address rather
than by a special handle.

- **Preserved by:** the MVP guard rail that sending to an unsubscribed address
  persists rather than errors. That queued message is exactly the activation
  trigger.
- Strictly more dangerous than `#new` and should not precede it.

### 2.3 More relative handles

`#children`, `#siblings`, `#swarm`. All are resolver cases, all additive —
relative aliases resolve at send time and never enter stored data, so adding
one touches nothing.

Note that `#children` is inherently fan-out and `#parent` is inherently
single-recipient, so the alias set and the subscription mode question (1.1) are
coupled.

### 2.4 Prefix and attribute subscriptions

Subscribe to `auto-web/*`, or filter on envelope attributes. **Confirmed as v2**
— there is no use for it today.

Kept possible by two v1 choices: addresses are free-form strings whose
validation deliberately permits a separator (so hierarchical names stay
expressible by convention, even though nothing enforces them), and the envelope
carries an open `attributes` map that the MVP ships empty. The one thing that
would have foreclosed this — rejecting or normalising away separators — is
explicitly ruled out in the epic.

### 2.5 Request/response: send-and-wait

**The use case, and a common one.** A parent orchestrates several sub-agents. A
sub-agent hits a discovery or a problem it wants a decision on, mails the parent,
and then *waits*. The parent decides, and either kills the sub-agent or sends a
follow-up instruction.

This turns mail from fire-and-forget into a request/response channel, which is a
meaningfully different interaction shape. **Checked against the v1 decisions:
nothing blocks it.** Three things need care.

- **A blocking receive is a new verb, not a new schema.** The MVP has poll
  (`list`); this needs `wait` with a timeout. Purely additive — the client
  interface already anticipates a streaming/blocking variant.
- **The reply channel already works.** The parent replies to the envelope's
  `from`, which is a resolved absolute address (never `#parent`), and a send to
  an address with nothing bound persists rather than erroring — so the child can
  send first and subscribe after, in either order, without losing the reply.
  That guard rail was written for the cross-project journey and happens to
  protect this one too.
- **Correlation is expressible today** via the envelope's open `attributes` map,
  ahead of promoting a correlation id to a first-class field (§4).

**The one real interaction to watch.** A sub-agent blocked in `wait` may look
*idle* to hook-based liveness, since no hooks fire while it is parked. The
escalation policy could then try keystroke injection into a pane that is
mid-blocking-call. The fix is small but must be deliberate: a blocking waiter
registers itself on its binding so the notifier resolves the wait directly
instead of escalating. Liveness must distinguish **idle at a prompt** from
**blocked on a receive** — they are opposite situations that look identical from
the outside.

**Two hazards worth designing against.**

- **Deadlock.** Parent waits on child while child waits on parent, or an
  orchestrator blocks while all its workers block. Every wait needs a timeout,
  and a tree of waits needs a story for what a timeout means.
- **Kill is not mail.** Terminating a sub-agent is process control and stays
  out-of-band — the orchestrator already owns that through its spawner. A
  mail-mediated kill would be a *control channel* with its own authorization
  question (who may kill whom), and should not be smuggled in as a message kind.
  What mail must guarantee is the other half: a child killed mid-wait must not
  leave a hung process or an un-timed-out receive.

### 2.6 Roles

`role:reviewer@auto-stack` — an address resolved to whoever currently holds a
role rather than a fixed name. A resolver case over the binding table.

---

## 3. Distribution

The MVP is single-host. Multi-host was the constraint that shaped most of v1,
so almost nothing needs to change to get there.

- **Transport swap.** `auto-shared/transport` already has a tcp implementation
  and the RPC conformance suite already covers the wire. Multi-host becomes a
  dial string.
- **Log merge.** Append-only event logs plus sender-generated ULIDs make
  cross-host merge a union ordered by id, rather than conflict resolution over
  divergent mutable tables. This is the single biggest reason v1 is event
  sourced.
- **Local effectors.** Keystroke injection only works on the machine holding the
  pane, so delivery *effects* must run on the host owning the binding even if
  the store centralises. Never put an effector behind a central service.
- **The reconciling sweep.** Cut from the MVP because on one host the sender
  notifies inline and there are no pushes to drop. It returns with multi-host,
  where its job is healing missed notifications — push for latency, sweep for
  correctness.
- **Store clock authority.** Once leases exist across hosts, expiry must be
  decided by the store, not the worker, or clock skew silently double-grants.
- **Multi-user** (as distinct from multi-host) brings back the authorization
  question deliberately dropped in v1 — see 8.3.

---

## 4. Message model

All additive; all absorbed by the envelope's `attributes` map until they earn a
first-class field.

- **`kind` vocabulary** — `status` / `blocked` / `failure` / `request` /
  `finding` / `reply`. The field that makes mail machine-routable rather than a
  chat log: a supervisor can route on `kind=failure` without reading prose.
- **Threading** — `thread_id`, inherited by replies, with `Re:` prefixing.
- **Priority / importance** — pairs with the delivery policy table: normal mail
  waits for a nudge, urgent escalates.
- **Refs** — `path:line@commit` pointers, and `auto artifact` URLs for evidence.
  Chosen over attachments: no blob storage, no image conversion.
- **Expiry / TTL** — messages that stop mattering.
- **Correlation id** — the one genuine instance-scoped case: "reply from *that*
  activation, not whoever holds the address later." Belongs in the envelope,
  never as an address kind.

---

## 5. Integrations

These are where mail stops being a message bus and starts being infrastructure.

- **Mail as a watch trigger.** New `kind=request` for a project → autowatch
  dispatches a TaskDef. A bug report from project A literally spawns an agent in
  project B. This is the biggest differentiator over prior art, and it closes
  the loop with the existing `watch.task.*` events.
- **Mail → beads escalation.** A `request` that needs to survive as tracked work
  becomes a bead. **Mail is transport; beads is the work ledger — do not rebuild
  beads inside mail.**
- **`kind=finding` → auto reflect.** A finding mailed between agents *is* an
  Observation in all but name. Piping them into the reflect event store gives
  mail a second life as playbook input for near-free.
- **Session-manager consolidation.** Long-term intent is to move everything to
  herdr and retire ntm/tmux. The MVP does not do this and does not need to: mail
  stores its physical target as an opaque `(manager, target)` pair and delivers
  through a per-manager effector, so retiring a manager is deleting an
  implementation, not migrating data — bindings are a heartbeat-backed
  projection and stale rows age out on their own. Separately, autowatch's
  existing `Backend` (`Start` / `Kill` / `SessionExists`) abstracts sessions
  *it* creates; it is untouched by mail and is the natural home for `#new` (2.1)
  when that arrives. Consolidating the two is only worth doing once `#new`
  exists and the real shape is known.
- **auto-ui supervisor view.** Bindings plus delivery events are already enough
  to render who is alive, what is queued, and what is stuck. The bus events the
  MVP emits are the feed.

---

## 6. Adjacent but separate: file leases

The most valuable idea in the prior art (`mcp_agent_mail`'s file reservations),
and aimed squarely at a failure mode this repo has postmortems for — every pane
sharing one checkout, concurrent subagents leaking writes into the main
worktree.

**It is not mail.** The *mechanism* generalises — TTL, heartbeat extension,
fencing token, expiry are the same primitives as 1.2–1.4. The *addressing* does
not: a message hold is scoped to one id (exact match, trivial), a file lease is
scoped to a set of globs and needs overlap detection.

- **Suggested shape:** a small internal lease primitive keyed on an opaque
  `resource_key`, used for message holds first. File leases become a second key
  namespace over the same table plus glob-overlap logic — plausibly `auto lease`
  rather than `auto mail`.
- **The honest caveat:** an advisory lock nothing enforces is a coordination
  convention, not a lock. Making it real means a pre-commit hook that blocks
  conflicting commits — a separate piece of work with its own failure modes.

---

## 7. Post-alpha

The MVP buys freedom by marking event types and the store file `alpha`: no
upcasters, no migrations, reset supported. That debt comes due at GA.

- **Stable type names.** Dropping the `alpha` segment touches every subscriber
  — the accepted price of the disclaimer.
- **Upcasting.** Once events must survive a schema change, the `version` integer
  the MVP already carries becomes the key for upcasters. Carrying it from day
  one is why "absent implies 1" never becomes a permanent wart.
- **Retention.** An append-only log grows forever. Alpha's answer is "wipe it";
  GA needs a real one.
- **Export / archive.** A human-readable markdown or git-backed archive of
  threads, as the prior art does. Attractive for auditability, rejected for v1
  as too much machinery (their implementation needs two lock files).

---

## 8. Considered and rejected

Recorded so they don't get re-proposed as new ideas.

1. **cc / bcc** — a human affordance. `to[]` is sufficient for agents.
2. **Attachments with image conversion** — file refs plus `auto artifact` URLs
   cover the need without blob storage.
3. **Cross-project contact handshake / authorization** — single-host,
   single-user makes it ceremony. Returns only with multi-user (§3), gated then
   on both projects being in the local registry.
4. **Generated agent names** (adjective+noun, as in the prior art) — they name an
   *instance*, not a role, so a restarted agent gets a new name. Worst of both
   worlds: none of the automatic derivation, none of the stability.
5. **Autoincrement integer message ids** — forecloses distribution and dedup
   outright.
6. **Maildir-style file-per-message store** — genuinely better for lock-free
   concurrent append, but once subscriptions, read state, bindings and dedup
   exist it's a relational problem wanting transactions. Superseded by SQLite.
7. **Encryption / signing** — single-user host; no threat model that justifies it
   yet.
8. **Physical addresses alongside virtual ones** — two address spaces means every
   consumer handles both and the store indexes both, while the physical one
   decays. The real need underneath (which process is behind this address?) is
   answered by binding metadata, not a second namespace.

---

## 9. Reasoning worth keeping

The conclusions above rest on a few arguments that would be expensive to
re-derive.

- **Liveness picks the delivery mechanism.** A hook nudge and keystroke
  injection are not competing options — each covers the other's dead zone. A
  nudge can never reach an idle agent (no hooks fire); keystrokes can corrupt an
  active one (landing mid-tool-call or into a permission prompt). Hook `last_seen`
  is the discriminator, and it's already being written.
- **The alias/absolute split.** Relative handles (`#parent`, `#new`) resolve at
  send time; the envelope always stores the resolved absolute address. This is
  why every future alias is free — none of them enter stored data.
- **Parentage is ambient in hooks — for in-process subagents.** Verified against
  this repo's hook log: a subagent's own tool calls fire hooks stamped with the
  *parent's* `session_id` and `transcript_path`, plus its own `agent_id`. There
  is no join and no lag, so the intuitive objection that "session logs lag
  exactly when it matters" is wrong for this case. It does not extend to spawned
  panes, which get their own `session_id`, no `agent_id`, and no record of who
  spawned them — those still need the address stamped at launch. The residual
  problem is narrower than it first appears: the hook payload reaches the hook
  process, not arbitrary commands, and an in-process subagent shares the
  parent's environment, so the CLI cannot tell which of the two it is running
  as. See Q-4 on the epic.
- **"I crashed" is the least reliable message in the system.** A dying process
  rarely gets to send mail. Design for its *absence*: a session-end hook posts
  best-effort, and the supervisor infers failure from an expiring lease.
- **Event types become more permanent than schema.** Old events can't be
  rewritten, so an event-type change is versioning-and-upcasting rather than
  `ALTER TABLE`. That's the whole argument for the alpha marker — and the reason
  to keep the v1 vocabulary to three types.

---

## Open questions

Load-bearing, unresolved, and worth settling before any of §1–2 is built:

1. **Does `#new` spawn into a worktree or a pane in the existing checkout?** The
   shared-checkout postmortems argue for a worktree, but that makes spawning
   heavier and raises teardown questions.
2. **What is the spawn budget, and who owns it?** Per-agent, per-project, or
   host-global — and does exceeding it queue, reject, or degrade to a plain
   message?
3. **Do leases belong to `auto mail` or a separate `auto lease`?** The mechanism
   is shared; the addressing is not.
4. **Does the central store centralise at all under multi-host,** or do hosts
   keep local logs and gossip? Log-merge semantics make the second viable in a
   way it wouldn't be with mutable state.
