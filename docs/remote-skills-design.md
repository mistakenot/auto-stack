---
hash: "028ce428"
id: "75ae66eb"
read_when: "implementing auto-skill's remote install/customize/update/export pipeline, the skills.yaml/lock.json formats, the git cache, deterministic skill hashing, or the migrate-from-vercel command"
summary: "End-to-end design for turning auto-skill into a native tool for installing, customizing, updating, and exporting agent skills from remote repos: a global git cache, deterministic templating with literal/file-ref replacements, section-level doc extraction, a composite skill_version hash, version pinning policy, the skills.yaml + lock.json schemas, the full CLI surface, and a migrate-from-vercel path."
title: "Remote Skill Management — Design"
---

# Remote Skill Management — Design

> Companion to [`vercel-skills-gap-analysis.md`](../auto-skill/docs/vercel-skills-gap-analysis.md)
> (the *what's missing* map) and [`opensource-ideas.yaml`](../auto-skill/docs/research/opensource-ideas.yaml)
> (the ranked borrowable mechanisms, `I001`–`I013`). This doc is the *how* — the
> concrete design we'll build to make auto-skill manage skills from remote repos
> without shelling out to `npx skills`.

## Goal

Replace the current `auto skill sync` shell-out to `npx skills add` with a native
Go pipeline that can: **add** a remote skill, **customize** it per-project
deterministically, **update** it, and **export** it into the coding agents the
user actually runs. The design optimizes for auto-stack's context (local-first, a
dev machine that already has `git` + git auth, a handful of reused skill repos)
rather than vercel's (a published npx tool that must run anywhere, fast,
unauthenticated, against arbitrary repos). That context flip is why several
vercel mechanisms are *simplified away* here.

## Core concepts

- **Authored skill** — a skill you write, living at `./skills/<name>/` and
  committed. The authoring source of truth; always authoritative, wins any name
  collision.
- **Vendored skill** — a skill pulled from a remote source. It has no committed
  home of its own — its source of truth is the git cache (pinned in the lock);
  `sync` renders it straight into the output targets.
- **Source** — a remote repo a vendored skill comes from, pinned in the lock.
- **Output target** — an agent skills directory that `sync` writes into, e.g.
  `.claude/skills/` (Claude style) and `.agents/skills/` (generic style). Every
  target receives **both** authored and vendored skills. Derived output —
  auto-skill owns and regenerates it.
- **Replacement** — a per-skill customization value (a literal or a file-ref)
  that fills a `{{handlebars}}` placeholder in a template at render time.
- **Render** — the pure function `(template, replacements, resolved-files) →
  SKILL.md`. No code execution.
- **Sync** — the pipeline: ensure cache → render vendored skills + read authored
  skills → write both into every output target. There is **no** intermediate
  *project-local* `./skills/.vendor/` staging tree; rendered bytes go straight to
  the targets. Vendored repos/skills are still cached **on the machine** — in the
  global git cache (`~/.auto/skills/upstream/`), not in the project.

### Layering

Project-local authored skills **shadow** vendored skills of the same name (borrow
the `seenNames` dedupe + shadowing rule, `I006`). The shadow is applied per target
as `sync` writes it: if an authored and a vendored skill share a name, only the
authored one is written. A thin authored skill that overrides a vendored one is
just customization taken to its conclusion.

## Path layout

```
# per-machine (global, never checked in)
~/.auto/skills/settings.json                         # machine defaults (cache loc, default --jobs)
~/.auto/skills/upstream/<host>/<repo-path>.git       # global git cache (bare, blobless)
~/.auto/skills/trust.json                            # machine-local approved endpoints (transport gate)
~/.auto/skills/receipts/<project-id>.json            # what THIS machine wrote (prune deletion authority)
~/.auto/projects.json                                # EXISTING shared project registry (id↔path↔remote) — reused

# per-project (in-repo, checked in)
.auto/skills/skills.yaml                             # project config: replacements, version policy, targets
.auto/skills/lock.json                               # dependency identity lock (what to fetch)
.auto/skills/manifest.json                           # derived render state + managed set (declares desired)
./skills/<name>/                                     # authored skills (authoring source) — committed
.claude/skills/<name>/                               # Claude-style target — COMMITTED by default (commit_targets: true)
.agents/skills/<name>/                               # generic-style target — committed by default
.auto/skills/.sync-journal                           # transient sync write-ahead journal — gitignored
```

> **Project identity** reuses the existing `auto` concept: `auto init --project`
> registers the repo in `~/.auto/projects.json` with a stable `id` (+ `path`,
> normalized `remote`, `tools`). That `id` is the `<project-id>` keying machine-local
> receipts; `auto skill init` adds `skill` to the project's `tools`. **Worktrees**
> (auto-env) share the project `id`, but receipt entries key on each target's
> **absolute path**, so distinct worktrees never collide. `cache prune
> --unreferenced` reads project paths straight from `projects.json` — no separate
> registry file.

<!-- RESOLVED(P1): Project target configuration has no persisted schema
REVIEW: `init --project` later asks for targets, `auto_update`, default version, and commit-vs-gitignore policy, but the per-project layout contains only `skills.yaml` and `lock.json`, and the shown `skills.yaml` schema has no target or commit-policy fields. The only settings file shown is global, even though these choices are project-specific. Define one authoritative project settings location/schema (and reconcile it with the current `.auto/skill/settings.json` path) or two developers can run `sync` against different target sets from the same checkout.
AUTHOR: `skills.yaml` (checked in) is now the single authoritative project config — added top-level `targets:` and `commit_targets:` keys (see the schema). `~/.auto/skills/settings.json` holds machine-only defaults (cache location, default `--jobs`); every project-affecting choice lives in the committed `skills.yaml`, so a shared checkout always syncs the same target set. The old `.auto/skill/` (singular) path is gone — all paths use `.auto/skills/`.
-->

> Output targets are **derived**, but **committed by default** (`commit_targets:
> true`) for zero-setup checkouts — agents work without anyone running `sync`. Set
> `commit_targets: false` to gitignore + regenerate instead (clean diffs, like
> `node_modules`). Either way `sync` prunes only skills it owns per the manifest +
> machine-local receipts — never foreign/ad-hoc skills (those are surfaced for
> `adopt`). See **Managed vs. ad-hoc skills**.

## Global git cache

A single content-addressed cache of upstream repos. Cloning (vs. vercel's
GitHub-Trees-API-no-clone path) is the *simpler* choice in our context: it reuses
the user's existing git auth, hits no REST rate limits, and handles every git
host with one code path — deleting the need for a GitHub-API client and a
provider abstraction (`I009`'s GitHub specifics and `I011` both drop out).

- **Layout / identity:** `~/.auto/skills/upstream/<host>/<repo-path>.git`, a
  **bare** clone per repo, where `<repo-path>` is the *full ordered path* (so
  nested GitLab subgroups like `acme/platform/skills` work — see Source formats).
  The key is the **canonical repo identity**: lowercased host + full path, with
  scheme/userinfo/port/`.git` stripped — so HTTPS and SSH forms of one repo share
  a cache by design. On reuse the bare repo's configured `origin` is verified
  against the canonical fetch URL; a genuine mismatch (distinct remotes that would
  collide) gets a short URL-hash suffix to disambiguate.
- **Safe path encoding:** each decoded path component is validated and encoded
  before it becomes a directory name — `.`/`..`/empty, `/` and `\`, control chars,
  and platform-reserved names (`CON`, `NUL`, trailing dot/space, …) are
  percent-encoded; a post-join `isSubpathSafe` check confirms the result stays
  under `~/.auto/skills/upstream/` before any write.

<!-- RESOLVED(P1): Repository paths are not safely encoded as cache paths
REVIEW: The normalized remote path is used directly under `~/.auto/skills/upstream/<host>/<repo-path>.git`, but the design never validates or encodes decoded path components. Inputs containing `.`/`..`, encoded separators, backslashes on Windows, or platform-reserved names can escape or alias the cache root. The earlier repository-identity review explicitly required safe encoding; define a component encoding plus post-join containment check before this path can be used for writes.
AUTHOR: Added component encoding + post-join containment: each segment is validated/percent-encoded (rejecting `.`/`..`, separators, backslashes, reserved names) and the joined cache path is confirmed under the cache root before any write.
-->

<!-- RESOLVED(P2): Cache key can alias distinct remotes
REVIEW: The cache is called content-addressed, but `<host>/<owner>/<repo>` is repository-addressed and omits scheme, port, and the exact remote identity. HTTPS and SSH forms intentionally collapse, while self-hosted URLs or rewritten remotes can collide at the same path; whichever URL cloned first then controls later fetches. Specify a canonical repository identity, validate an existing cache's configured remote before reuse, and use a collision-resistant key when distinct remotes cannot safely share objects.
AUTHOR: Key is now the canonical repo identity (lowercased host + full path); reuse verifies the bare repo's `origin` against the canonical URL and appends a URL hash to disambiguate genuine collisions. Full path also fixes nested groups.
-->

- **Bare blobless clone:** `git clone --bare --filter=blob:none` (no `--sparse` —
  sparse-checkout needs a worktree a bare repo doesn't have). Trees/history come
  down; blobs are fetched on demand.
- **Realizing blobs (online):** after resolving the commit, `add`/`sync`/`update`
  fetch the objects reachable for that commit so extraction has every blob locally;
  after this step the resolved commit is **fully present** (objects, not just ref).
- **Offline guarantee:** `sync --check` / `doctor` run with promisor/lazy fetch
  **disabled** (`GIT_NO_LAZY_FETCH=1`). A missing object never triggers the
  network — they report an *incomplete cache* (remediation: `run auto skill sync`).
  "Commit present" means "objects present."

<!-- RESOLVED(P1): Bare sparse clone is invalid and partial blobs break offline checks
REVIEW: `git clone --bare --sparse` fails because sparse-checkout requires a worktree; sparse has no useful meaning for this bare cache. Separately, `--filter=blob:none` leaves promised blobs absent, and `git archive` may lazily fetch them. That contradicts the later guarantees that `doctor`/`sync --check` are offline and that a present commit means the cache satisfies a pinned dependency. Define how required blobs are materialized during online operations and how offline commands disable promisor fetches and report an incomplete cache.
AUTHOR: Dropped `--sparse` (invalid on bare). Cache is bare + blobless; online ops explicitly realize the resolved commit's objects before extraction; offline ops set `GIT_NO_LAZY_FETCH=1` and report an incomplete cache rather than lazily fetching. "Present" now means objects present.
-->

- **Extract without a checkout:** `git -C <cache> archive <sha>:<subpath>` streams
  the skill subtree at an exact commit — no working tree. Extraction is
  **validated before any write**: symlink and special entries (devices,
  gitlinks/submodules) are rejected (never followed), and file-count + total
  uncompressed-size limits guard against hostile repos.

<!-- RESOLVED(P1): Archive extraction does not define symlink and resource safety
REVIEW: A git tree cannot contain `..` path components, but it can contain symlink entries whose targets escape the extraction/output root. Copying the archived subtree or later following a side-file link can therefore read or write outside the intended directory. The design also has no file-count or expanded-size limits for untrusted repos. Require safe archive-entry validation (including rejecting or explicitly preserving-without-following symlinks and special entries) plus resource limits before extraction/copy.
AUTHOR: Extraction now validates every archive entry before writing: symlinks/devices/gitlinks are rejected (never followed), with file-count and total-size limits against hostile repos.
-->

- **Update:** `git fetch` the cache; resolve `ref` → commit SHA. Freshness is the
  native primitive, not a tree-SHA hack.
- **Transport safety:** every `url` — from `add` and from the checked-in lock that
  `sync` consumes — is validated against an **allow-list of schemes** (`https`,
  `ssh`, `git`, `file`). Remote-helper / option-style URLs (`ext::`, `fd::`,
  anything starting with `-`) are rejected, and git args are always passed after
  `--`.
- **Trust is machine-local, not checked-in.** Effective approval lives in
  machine-local state (`~/.auto/skills/trust.json`), never in the repo. The
  approved *identity* is a **canonical endpoint** — `scheme://host:port` for remote
  sources, or a canonical absolute path for `file:`/local sources — so approving
  `https://github.com` does **not** authorize `git://github.com`, another port, or
  arbitrary local reads. `skills.yaml` may *request* hosts (`trusted_hosts:`) for
  convenience, but a request never authorizes itself: `sync` fetches an endpoint
  only if it is approved in machine-local state, and approval requires an explicit
  user action (`auto skill trust add`, interactive). So a malicious branch that
  edits both `lock.json` and `skills.yaml` still hits an unapproved-endpoint stop
  on checkout — closing the post-checkout-hook vector.
- **Bootstrap.** On first encounter of a requested-but-unapproved endpoint, an
  interactive `sync` prompts `Approve <endpoint>? [y/N]` and records the approval.
  In CI / `-y` / non-TTY it **fails closed** (listing endpoints to approve) unless
  `--trust-requested` (or `AUTO_SKILL_TRUST_REQUESTED=1`) explicitly opts into
  trusting the `skills.yaml` `trusted_hosts` list for that run.

<!-- RESOLVED(P1): Checked-in trust cannot authorize a checked-in lock
REVIEW: `trusted_hosts` lives in checked-in `skills.yaml`, so the same malicious branch that changes `lock.json` can add its host to `trusted_hosts`; `sync` then sees no approval boundary at all. This does not close the post-checkout-hook vector claimed by the resolved review. Persist effective approval in machine-local state (with the project config only requesting trust), and require an explicit user action before that state changes.
AUTHOR: Moved effective trust to machine-local `~/.auto/skills/trust.json`; `skills.yaml trusted_hosts` only *requests* trust and never self-authorizes. `sync` fetches only machine-approved endpoints, and approval requires an explicit `auto skill trust add`. A branch changing lock + requested hosts together still stops at an unapproved endpoint. (This reverses the earlier "trust lives in skills.yaml" choice.)
-->

<!-- RESOLVED(P2): Host-only trust conflates distinct transports and cannot name local paths
REVIEW: The fetch surface includes HTTPS, SSH, cleartext `git`, alternate ports, and `file`/local paths, while the only trust identity and CLI argument is `<host>`. Trusting `github.com` therefore implicitly authorizes every allowed scheme/port for that hostname, and there is no representable approval for the separately required local-path gate. Define trust over a canonical scheme+host+port endpoint and a canonical local path (or remove unsupported transports); do not let HTTPS approval silently authorize `git://` or arbitrary local reads.
AUTHOR: Trust identity is now a canonical endpoint (`scheme://host:port`, or a canonical absolute path for local sources), not a bare host — approving HTTPS doesn't authorize `git://`, another port, or local reads; local paths are representable as their own trust entries.
-->

<!-- RESOLVED(P1): A checked-in lock can drive unsafe git transports
REVIEW: The no-RCE claim is too broad because `sync` consumes a checked-in `url` and invokes git, while the source model allows generic git transports. Git can dispatch remote helpers/protocols, and post-checkout hooks are explicitly encouraged to run `sync`; a malicious lock change could therefore trigger connections or helper execution merely by checking out a branch. Define a strict allowed-scheme/protocol policy, reject option-like and helper-style URLs (for example `ext::`), pass arguments after `--`, and require explicit trust/approval for newly introduced hosts or local paths.
AUTHOR: Added a scheme allow-list (https/ssh/git/file), rejection of helper/option-style URLs, args-after-`--`, and a trusted-hosts gate — a lock that adds a new host/local path needs explicit `trust`/re-`add` before sync fetches it. Closes the post-checkout-hook vector.
-->

- **Public vs private:** identical commands. Public → anonymous HTTPS clone, no
  token. Private → `git` transparently uses the user's ssh key / credential helper
  / `gh`; our code never touches credentials.
- **Credential hygiene:** credential-bearing URLs (`https://user:token@host/…`,
  token query params) are **rejected at parse**; only a sanitized canonical URL is
  stored in the lock. Auth comes from the user's git config, never the source
  string.

<!-- RESOLVED(P1): Source URLs can leak credentials into the checked-in lock
REVIEW: The accepted HTTPS URL surface does not forbid userinfo or token-bearing query parameters, yet `url` is copied into checked-in `lock.json`. An input such as `https://user:token@host/repo` would violate the claim that the tool never touches credentials and can commit a secret. Reject credential-bearing URLs and persist a sanitized canonical display URL separately from any fetch URL/credential-helper state.
AUTHOR: Credential-bearing URLs are now rejected at parse; the lock stores only a sanitized canonical URL. Auth is delegated to git's credential machinery, never persisted.
-->

- **Privacy:** the cache holds private content on the user's disk (fine — never
  sync `~/.auto/skills/upstream/` anywhere). The lock references private sources by
  sanitized URL + SHA only, never content.
- **No RCE:** clone / `archive` / checkout never run remote hooks or code (together
  with the transport allow-list above).
- **GC / prune:** `auto skill cache prune` defaults to **eviction** by age/size
  (LRU; defaults `--max-age 90d`, `--max-size 5g`) — always safe, since anything
  evicted is re-fetched on next `sync`.
  `--unreferenced` additionally drops repos no *known* project references, where
  "known" = project paths in the existing `~/.auto/projects.json` registry (written
  by `auto init --project`). Prune never claims a reference-completeness it can't have.

<!-- RESOLVED(P2): Global cache pruning cannot know all project references
REVIEW: The cache is global, but no global registry of project roots/lockfiles is designed. `cache prune` therefore cannot determine that a repo has "no lockfile references" outside the current project, and LRU is a materially different policy that can evict dependencies needed for offline reproducibility. Specify reference discovery/registration and stale-project handling, or define prune honestly as cache eviction independent of lock references.
AUTHOR: Prune is now two honest modes — default age/size eviction (safe, re-fetchable) and `--unreferenced` against a project registry that `init`/`sync` maintain. No false "no references anywhere" claim.
-->

- **Concurrency:** a file lock around fetch/extract per cached repo; extraction
  always targets immutable/ephemeral output, never a shared mutable checkout.

## Versioning convention

Intent and resolution live in different files:

- **`skills.yaml` = intent** — which version you want.
- **`lock.json` = resolution** — the exact commit that intent resolved to.

**Default policy is `latest`** (track the default branch); override per-skill or
via `shared`.

```yaml
shared:
  version: latest            # default policy for every skill
skills:
  deploy:                    # version omitted → inherits `latest`
    replacements: { ... }
  release:
    version: v2.3.0          # pinned tag
  experimental:
    version: a1b2c3d4        # pinned commit (hard freeze)
  preview:
    version: "branch:next"   # track a non-default branch
```

Spec forms (bare strings resolved best-effort: tag → branch → commit-prefix; use
`tag:` / `branch:` / `commit:` to force):

| Spec | Resolves to | Moves on `update`? |
|---|---|---|
| `latest` (default) | newest commit on default branch | yes |
| `branch:<name>` | newest commit on that branch | yes |
| `<tag>` | the commit the tag resolves to (annotated tags peeled) | re-resolved on explicit `update` only (force-moves warned); never floated by `sync` |
| `<sha>` | that commit | never (reported as pinned) |

<!-- RESOLVED(P2): Tag movement policy contradicts the pinned-version contract
REVIEW: This table says a repointed tag moves on `update`, while the next section says tag specs are always respected and never float, and the quickstart says updates leave tags pinned. Those behaviors produce different commits after the same command. Decide whether a tag is an immutable resolution after `add` or a ref that is re-resolved, and specify annotated-tag peeling and force-move handling accordingly.
AUTHOR: Settled: a `<tag>` is pinned to its resolved commit in the lock and is **re-resolved only by explicit `update`** (annotated tags peeled to the commit; a force-moved tag advances with a warning). `auto_update` sync floats `latest`/`branch:` only — never tags. The auto_update text and quickstart now state this consistently.
-->

### `latest` is live by default (`auto_update`)

By **default `auto_update: true`**: every `sync` re-resolves `latest` and
`branch:` specs to current upstream HEAD *before* rendering — you're always on the
newest version without thinking about it. A `<sha>` never floats; a `<tag>` floats
only on explicit `update` (peeled, force-move warned), never on `sync`.

```yaml
auto_update: true            # default — sync floats latest/branch: to HEAD before rendering
```

The lock still records the commit each `sync` resolved to, so a render stays
auditable, `skill_version` stays meaningful, and tag/commit-pinned skills
reproduce exactly. `add` / `update` also resolve and write the commit.

<!-- RESOLVED(P1): Default sync violates the lock-reproducing CLI contract
REVIEW: With `auto_update: true`, ordinary `sync` fetches floating refs, advances `commit`, and rewrites the checked-in lock. The CLI table later describes `sync` as `npm ci`, says it realizes the lock exactly, and says it does not move the lock; the hooks section also proposes running it automatically after checkout. Beyond the contradiction, this makes unreviewed upstream prompt/script changes enter agent discovery paths during a routine sync. Separate reproducible `sync` from explicit `update` (or rename/document the mutating operation and make the lock/supply-chain review semantics explicit).
AUTHOR: Reconciled the contract rather than the default (always-latest stays the chosen default). `sync` now has TWO documented modes keyed on `auto_update`: OFF = pure `npm ci` (reproduce the locked commit, no fetch, lock unchanged); ON (default) = `update`-then-`ci` (float `latest`/`branch:` first, advance + rewrite the lock, then render). The CLI table, command note, and hooks section now say this explicitly, and the supply-chain implication is called out below. Teams that want reproducibility + change review set `auto_update: false`.
-->

**Supply-chain note.** Because the default `auto_update: true` makes `sync` pull
the newest upstream into agent discovery paths, **unreviewed** upstream
prompt/skill changes can land on a routine sync. That's the freshness trade-off of
the default. Mitigations already in the design: the transport allow-list +
trusted-hosts gate (a *new* host/path needs explicit `trust`), and `lint` running
on rendered output. Teams that need to review upstream changes before they reach
agents set `auto_update: false` and advance deliberately with `auto skill update`.

**Want reproducible-by-lock instead?** Two composable options:

- set `auto_update: false` — then `sync` reproduces the locked commit (does not
  float or re-resolve refs; it may still fetch the *pinned* commit's objects on a
  cache miss — see **locked materialization**) and only `auto skill update` advances
  it. npm's `ci` vs `update` split.
- run `auto skill sync --locked` for a **one-shot** reproduce regardless of config:
  `--locked` (alias `--no-update`) overrides `auto_update: true` for that invocation.
  This is what `doctor`'s repair hint uses, so fixing target drift never silently
  floats dependencies. Precedence: `--locked` > `auto_update`.
- or pin individual skills to a `<tag>`/`<sha>` while everything else floats.

<!-- RESOLVED(P2): There is no one-shot locked render when auto-update is enabled
REVIEW: `doctor` tells users to run `auto skill sync` to repair target drift, but with the default `auto_update: true` that remediation also fetches and advances every floating dependency. The CLI has no `--locked`/`--no-update` override, so repairing the current reviewed lock requires editing committed project policy first. Add an invocation-level locked mode and define its precedence over `auto_update`.
AUTHOR: Added invocation-level `auto skill sync --locked` (alias `--no-update`) overriding `auto_update: true` for that run; `doctor`'s repair hint uses it. Precedence `--locked` > `auto_update`. Wired into the CLI tree + options table.
-->

### Freshness visibility

Two separate, honest questions:
- *Is my render current with the lock?* → `auto skill sync --check` (offline).
- *Is there newer upstream?* → `auto skill update --check` (does a `git fetch`,
  compares HEAD vs locked commit, writes nothing).

## Customization (replacements)

Substitution is limited to two value types — no scripts, no code execution
(deliberately closing the door vercel nailed shut with its `---js` refusal,
`I008`). This makes render a pure, hashable function.

1. **Literal** — a scalar value, inlined verbatim.
2. **File-ref** — a pointer to a repo file (or a section of one); its content is
   inlined. Leading YAML frontmatter is **stripped by default** (so a doc's
   autodoc `hash`/`id` metadata never leaks into the skill, and can't collide with
   the skill's own frontmatter); set `strip_frontmatter: false` to keep it.

A template declares what's customizable via `customize:` frontmatter; `skills.yaml`
supplies the values. The exact contract:

- **`customize:` schema** — `customize: { <var>: { required: bool (default
  false), default: <string>, description: <string> } }`; referenced in the body as
  `{{ .var }}` (dot = data-object field access — valid Go `text/template`).
- **Engine** — Go `text/template`, fixed `{{ }}` delimiters. The parsed AST is
  **restricted to an allow-list**: only field-access actions (`{{ .var }}`) and the
  literal-brace escape are permitted; function calls, pipelines, control actions
  (`if`/`range`/`with`), and built-ins are rejected at parse, so the accepted
  grammar is exactly the promised one. Values substitute as **raw text** (no
  HTML/shell escaping — skills are prose, not an injection sink).

<!-- RESOLVED(P1): The declared placeholder syntax is not valid Go text/template data access
REVIEW: In Go `text/template`, `{{ var }}` resolves `var` as a function name, not as a key in the data object; map/field access is `{{ .var }}`. The engine also exposes built-ins and pipeline syntax unless the parsed AST is explicitly restricted. As written, the documented templates fail to parse or accept a larger grammar than promised. Choose valid placeholder syntax (or a custom parser) and specify AST validation that permits only the intended variable and literal-brace forms.
AUTHOR: Placeholders are now `{{ .var }}` (valid field access), and the parsed AST is restricted to an allow-list — only field-access actions + the literal-brace escape; functions/pipelines/control actions/built-ins are rejected at parse. Accepted grammar == promised grammar.
-->

- **Resolution rules** — an **undeclared** placeholder → hard error; a declared var
  with no value and no `default` → hard error if `required`, else the empty string;
  a literal `{{` in body content is written `{{ "{{" }}`.
- **Versioned** — the renderer's behavior carries a `render_version` (recorded in
  the manifest). A bump is detectable; on the next `sync` it triggers a **one-time
  lazy re-render of all skills** (output is derived, so the churn is expected on a
  tool upgrade) — no special migrate command.
- **Lint** checks every required var has a value, every supplied value maps to a
  declared var (warn on unknown), and the **rendered** output fits the token budget
  — reusing auto-skill's existing thresholds (body **warn >4000 / error >8000
  tokens**; aggregate listing **warn >2000 / error >4000**; estimate chars/4).
  `lint` is the gate (errors fail it); `sync` only **warns** on budgets (advisory,
  never blocks a render). A whole-file include is the usual budget-buster.

<!-- RESOLVED(P2): Template evaluation semantics are not specified
REVIEW: Deterministic rendering needs more than naming `{{handlebars}}`: the document does not define the `customize:` schema, whether placeholders are escaped or raw, how optional values without defaults behave, whether undeclared placeholders are errors, or how literal braces in code examples are represented. Different Go template libraries and escaping modes produce different bytes. Specify a minimal grammar and exact failure/substitution rules, and version that renderer behavior.
AUTHOR: Specified the full contract above: `customize:` schema, Go `text/template` data-only with `{{ }}`, raw (unescaped) substitution, hard-error on undeclared/required-missing, `{{ "{{" }}` for literal braces, and a `render_version` so engine changes are a detectable bump. Determinism is now pinned.
-->

Deliberate non-features (each protects determinism):
- **No interpolation in literals** (no `${ENV}`, no value→value refs).
- **No globs in file-refs** (one ref → one file or one section).
- **File-refs resolve inside the repo only** — containment is enforced on the
  **fully symlink-resolved** real path (not just lexical `..`/absolute cleaning,
  `I004`); a ref that *is* or *traverses* a symlink leaving `--root` is **rejected**
  (TOCTOU-safe open where practical). Same repo → same bytes on every machine.
- **Referenced content is inserted raw**, never re-templated (no recursion/cycles).

<!-- RESOLVED(P1): Lexical containment does not contain symlinked file references
REVIEW: `isSubpathSafe`-style path cleaning only rejects `..`/absolute escapes. A checked-in path such as `docs/runbook.md` can be a symlink to a file outside `--root`, causing `sync` to inline secrets or host-local content and making the same lock render differently across machines. Define symlink policy and enforce containment on the resolved path (with TOCTOU-safe opening where practical), or reject symlinks in file-ref components.
AUTHOR: Containment is now enforced on the fully symlink-resolved real path; a file-ref that is or traverses a symlink out of `--root` is rejected (TOCTOU-safe open where practical). Lexical cleaning alone is no longer trusted.
-->

### Why file-refs matter here

A file-ref *is* auto-stack's "docs are authoritative" model made executable.
Today autodoc *detects* drift between a skill and its source doc and warns; with
file-refs the skill is *regenerated from* the doc, so for any included section
drift is impossible by construction. The skill stops duplicating doc content and
starts including it.

## Section extraction (file-refs)

A file-ref may point at a whole file or a single markdown section. Section
selection is **heading-based, best-effort** — but the fuzziness is isolated to
*matching*; everything downstream is deterministic.

```yaml
conventions:
  file: docs/conventions.md
  section: "Deploy"           # best-effort heading match
  include_heading: false      # default: true
```

`section: ["Deploy", "Staging"]` (path form) disambiguates when the same title
appears more than once.

### Matching (tunable, best-effort)

A heuristic cascade over ATX heading titles — start minimal, adjust as real
misses appear:

1. exact normalized match — `casefold(collapse_ws(trim(title)))` equality.
2. GitHub-style slug match (`"Deploy Steps"` ↔ `deploy-steps`).

- **Multiple matches** → take the **first in document order** (deterministic) and
  emit a `sync`/`lint` warning naming the collision; use the path form to
  override.
- **Zero matches** → **hard error**. Best-effort decides *which* heading, never
  *whether* one exists — a skill must not render with missing content.

### Extraction (fixed, deterministic)

Once a heading is chosen, nothing is heuristic. `extract(file_bytes, selector) →
canonical_bytes`:

1. Decode UTF-8; normalize newlines to LF.
2. Strip leading YAML frontmatter (`---` … `---`/`...`) — default; skipped only if
   `strip_frontmatter: false`.
3. Scan line-by-line, **code-fence-aware**: a fence opens on
   `^ {0,3}(```{3,}|~~~{3,})` and closes on a matching fence; lines inside a
   fence are never headings.
4. Identify ATX headings (Setext not supported):
   `^ {0,3}(#{1,6})[ \t]+(.+?)([ \t]+#+)?[ \t]*$` → `level`, `title`.
5. Resolve the target heading (matching cascade above; path form walks
   parent→child, each step unique within its parent's extent).
6. **Extent:** matched heading line → up to the next heading with
   `level ≤ matched` (code-aware), else EOF — i.e. the heading plus all nested
   subsections.
7. Apply `include_heading` (default `true`; drop the heading line if `false`).
8. **Canonicalize:** strip leading/trailing blank lines; LF; strip trailing
   whitespace per line; exactly one trailing newline.

`content_hash = sha256(canonical_bytes)`. Whole-file = no selector = the file
after frontmatter strip + canonicalize. Frontmatter stripping (step 2) applies to
**both** whole-file and section injection, and is the default for either; `content_hash`
covers what's actually inlined, so toggling `strip_frontmatter` moves the hash.

Because the hash is over the *extracted* bytes, editing an unrelated section of a
doc does not bump the skill — drift detection is scoped to the section. Renaming
the matched heading fails loud at `sync`/`lint`.

## Deterministic hashing (`skill_version`)

`skill_version` is the digest of the **entire rendered output tree** — the exact
bytes `sync` writes to a target — not just the SKILL.md template. It is the single
integrity value used for both freshness and tamper detection.

```
# render the skill into an in-memory tree, canonicalize each file, then:
skill_version = sha256(canonical_json({
  "render_version": <int>,                   # renderer/stamp schema version
  "files": [                                 # sorted by path; EVERY emitted file
    { "path": <rel>, "mode": "100644"|"100755", "sha256": <hex of EMITTED bytes> },
    ...
  ]
}))

per-file canonicalization (applied at render, before write AND hash):
  text   → LF + strip trailing whitespace per line + exactly one trailing newline
  binary → byte-for-byte, no transform
  classification: text iff valid UTF-8 with no NUL byte, else binary (deterministic)
```

- **Every emitted file is hashed** — `SKILL.md` plus `references/`, `scripts/`,
  `assets/` — by path, mode, and the **exact bytes written**. An upstream script or
  reference change now moves the version.
- **Hash == emitted bytes.** Canonicalization happens *during render*, and the hash
  is taken over those final bytes — so two inputs that differ only in stripped
  whitespace either render identically (same hash, correct) or differently
  (different hash). Never "same version, different bytes."
- The provenance stamp is added **after** this digest and **excluded** from it (no
  self-reference) — `metadata.auto_skill` is stripped/ignored when hashing.
- **Uniform across authored and vendored.** A vendored input is `template@commit +
  replacements + resolved file-refs`; an authored input is its own tree under
  `./skills/<name>/`. Both reduce to "hash the rendered output tree," so authored
  and vendored skills share one deterministic version definition.
- **Text vs binary.** Only the templated `SKILL.md` is rendered; side files are
  copied. Canonicalization applies to **text** files only (valid UTF-8, no NUL);
  **binary** assets (images, fonts, archives in `assets/`) are copied and hashed
  byte-for-byte, never transformed — so they aren't corrupted and still version
  exactly.

<!-- RESOLVED(P1): Text canonicalization is undefined and destructive for binary assets
REVIEW: The tree explicitly includes `assets/`, but the per-file rule applies LF conversion, trailing-whitespace stripping, and a terminal newline to every emitted file. Those operations require a text decoding policy and will corrupt PNGs, archives, fonts, and other valid binary skill assets. Classify text files with a deterministic rule and hash/copy binary files byte-for-byte, or hash every non-templated side file exactly as stored.
AUTHOR: Canonicalization now applies to text files only (valid UTF-8, no NUL byte); binary assets are copied and hashed byte-for-byte with no transform. Only SKILL.md is templated; side files are copied (text canonicalized, binary verbatim). No more PNG/font corruption.
-->

<!-- RESOLVED(P1): Supporting files are absent from the skill version
REVIEW: Skills explicitly include `references/`, `scripts/`, and `assets/`, and `git archive` extracts the whole skill subtree, but `skill_version` hashes only the SKILL.md template and replacements. An upstream script or reference can change while the composite hash remains identical; phase C will then skip the existing target and `sync --check` can report it current. Hash every emitted path (path, mode/type, and bytes) or define a full rendered-tree digest in addition to the SKILL.md render hash.
AUTHOR: Redefined `skill_version` as a digest of the **entire rendered output tree** — every emitted file (SKILL.md + references/scripts/assets) by path, mode, and exact bytes. An upstream side-file change now moves the version, so phase C won't skip it and `--check` won't report it current.
-->

<!-- RESOLVED(P1): Template normalization allows one version to identify different output bytes
REVIEW: The version hash strips trailing whitespace and normalizes newlines from the template, but render is defined over `template_bytes_at_commit` and nowhere says those normalized bytes are what gets emitted. Two commits that differ only in stripped whitespace can therefore have the same `skill_version` while producing different target files; incremental sync would retain whichever bytes were written first. Hash the exact canonical bytes that are emitted (or render from the normalized representation) and define output canonicalization explicitly.
AUTHOR: The hash is now taken over the **exact emitted bytes** after a defined per-file canonicalization that is applied at render time (so emitted == hashed). Whitespace-only differences either vanish (same output) or change the hash; no "same version, different bytes" gap remains.
-->

Uses:
- **`doctor` / `sync --check` (offline):** recompute from cache + current
  values/docs; mismatch ⇒ "stale, run `auto skill sync`". Catches local doc edits
  with zero network.
- **`update` (online):** `git fetch`, resolve `ref`→sha; new sha ⇒ upstream moved.

Both are fixed by `sync`: re-resolve → `git archive` → render → rewrite the lock.

## `skills.yaml` schema

```yaml
# .auto/skills/skills.yaml  — the authoritative, checked-in project config
auto_update: true            # default; sync floats latest/branch: before render. set false to pin to the lock
targets: [claude, agents]    # output target styles sync writes to (default)
commit_targets: true         # default: targets are committed (zero-setup checkouts). false = gitignore + regenerate
trusted_hosts: [github.com]  # REQUESTED hosts (convenience only); effective approval is machine-local — auto skill trust add

shared:
  version: latest            # optional default version policy
  replacements:              # optional; merged into every skill (skill wins)
    project_name: auto-stack

skills:
  deploy:
    version: latest          # optional; inherits shared/default
    replacements:            # fills the skill's {{handlebars}}
      deploy_target: gke-prod              # literal
      build_cmd: make build                # literal
      runbook:                             # file-ref (whole file)
        file: docs/deploy-runbook.md
        strip_frontmatter: true            # default — drop the doc's leading --- block
      conventions:                         # file-ref (section)
        file: docs/conventions.md
        section: "Deploy"                  # best-effort heading
        include_heading: false
```

Effective replacements for a skill = `shared.replacements` merged with
`skills.<name>.replacements` (skill wins). Type-by-shape: scalar = literal;
mapping with `file:` = file-ref. **Literals are strings** — a non-string YAML
scalar (number, bool, null, date) must be quoted and is carried, rendered, and
hashed as its exact string form (one serialization for both render and hash).

<!-- RESOLVED(P2): YAML scalar literals have no canonical type or rendering
REVIEW: YAML scalars include strings, booleans, integers, floats, null, and tagged/timestamp-like values, while a literal is described as being inlined "verbatim." After YAML parsing the original lexical form and quoting are not generally preserved, and canonical JSON may distinguish values differently from the template renderer. Restrict literals to strings or define accepted scalar types plus one canonical serialization used for both rendering and hashing.
AUTHOR: Literals are now restricted to strings — non-string scalars must be quoted and are treated as their exact string form for both rendering and hashing. Removes YAML type-coercion ambiguity.
-->

## `lock.json` schema

```jsonc
// .auto/skills/lock.json — checked in, alphabetized, NO timestamps (merge-friendly)
// DEPENDENCY IDENTITY ONLY (what to fetch). Derived render state lives in manifest.json.
{
  "version": 1,
  "skills": {
    "deploy": {
      "source": "github.com/acme/agent-skills",     // <host>/<repo-path> — mirrors cache path
      "url": "https://github.com/acme/agent-skills", // sanitized canonical URL (no credentials)
      "version_spec": "latest",                     // intent, copied from skills.yaml
      "ref": "main",                                // what was requested/resolved against
      "commit": "e5c075e3a8…",                      // RESOLVED sha → reproducibility
      "subpath": "skills/deploy",
      "private": false,
      "local": false,                               // true = local git source (non-portable)
      "state": "resolved"                           // "resolved" | "unresolved" (migration)
    }
  }
}
```

- `commit` (resolved) + `ref`/`version_spec` (intent) make renders replayable and
  tell `update` what may move.
- **Intent reconciliation:** when `skills.yaml`'s `version` differs from the lock's
  `version_spec`, the lock is *stale-by-intent*. With `auto_update: true`, the next
  `sync` re-resolves the new spec and rewrites the lock entry; with `auto_update:
  false`, `sync --check`/`doctor` report the mismatch ("intent changed — run `auto
  skill update <name>`") and `update` reconciles. `sync` never silently keeps a
  commit that contradicts a changed spec, so config and lock can't diverge
  indefinitely.

<!-- RESOLVED(P1): Editing authoritative version intent has no defined reconciliation path
REVIEW: `skills.yaml` is declared authoritative intent and `lock.version_spec` is a copy, but the design never defines what happens when a user edits `skills.<name>.version`, especially with `auto_update: false`. `sync` is described as not moving the lock, while `update` is described as advancing only floating specs. Define the mismatch state and the command that resolves a changed tag/branch/commit intent; otherwise config and lock can disagree indefinitely or different implementations can silently choose different commits.
AUTHOR: Defined the stale-by-intent state and its resolver: `auto_update: true` reconciles on the next `sync`; `auto_update: false` surfaces the mismatch via `--check`/`doctor` and reconciles via `auto skill update <name>`. `sync` never keeps a commit that contradicts a changed spec.
-->

- **Lock = dependency identity only.** All *derived* render state — resolved
  replacement literals, file-ref `content_hash`es (the `file → skills` reverse-index
  edges, used so `sync` knows what to re-render and what `auto-watch` keys on),
  `template_hash`, and `skill_version` — lives in `manifest.json`, **not** the lock,
  because it changes with `skills.yaml`/docs rather than with the dependency. So a
  locked `sync` (`auto_update: false`) that re-renders after a value/doc edit
  rewrites the **manifest**, never `lock.json` — the "lock unchanged" contract holds.
- **Authored skills are NOT in the lock** — no source/SHA; they're committed files.
- Private leaks nothing: `private: true` + sanitized url + sha, no content.

<!-- RESOLVED(P1): Pinned sync still has to rewrite derived lock fields
REVIEW: `lock.json` stores replacement literals, extracted-file hashes, and `skill_version`, all of which change when `skills.yaml` or a referenced project document changes. The design also says `sync` fixes those changes, yet the auto-update-off contract says pure `npm ci`, lock unchanged, and the CLI table says the lock moves only when a ref floats. Either remove project-derived render state from the dependency lock or state that locked `sync` rewrites these fields and adjust `--check`, CI, and the "lock unchanged" contract accordingly.
AUTHOR: Removed project-derived render state from the lock — `template_hash`, resolved `replacements`, file `content_hash`es, and `skill_version` now live in `manifest.json` (derived, regenerable). The lock is pure dependency identity, so locked `sync` rewrites the manifest (not the lock) when only values/docs change. "Lock unchanged under locked sync" is now true.
-->

## CLI surface

Mental model — three load-bearing verbs on the install/ci/update pattern:

| Verb | Analogy | Does | Moves lock? | Network |
|---|---|---|---|---|
| `add <source>` | `npm install <pkg>` | add a new upstream dep | yes | yes |
| `sync` | `npm ci` (auto_update off) / `update`+`ci` (on, default) | off: reproduce the locked commit + render; on: float `latest`/`branch:` first, then render | only when `auto_update` floats | fetches pinned objects on cache miss; re-resolves refs only when `auto_update` floats |
| `update [name]` | `npm update` | advance floating specs to latest, re-render | yes | yes |

```
auto skill
  # setup
  init [--project] [-y]         interactive setup wizard (--project) or global settings;
                                  -y = non-interactive (defaults, overridable by flags)
  quickstart                    happy-path walkthrough
  docs                          full command reference
  doctor                        diagnose config + drift (offline)

  # authoring — local skills
  create <name> --description   scaffold an authored skill
  lint [path]                   validate skills (runs on RENDERED output)

  # dependencies — remote skills
  add <source>                  add upstream dep → cache → discover → lock → render into targets
                                  flags: --skill <name>..|'*', --path <dir>.., --list, --full-depth,
                                         --version <spec>, --as <name>, --no-sync
  remove <name> [--local|--vendored]  remove a skill (selector required if both exist)
  adopt  [name...]              move ad-hoc skills from targets into ./skills/ (filesystem move + git add)
                                  flags: --all, --from <target>, --force, -y
  update [name...] [--check]    advance locked commits to latest ref, re-render
  sync [--check] [--locked] [--target t...] [--jobs n]  reconcile lock+values → cache → render → write to targets

  # inspection — resource triad (JSON default, --format text for humans)
  list [--local|--vendored]     all skills (default: both): ids + metadata + stale flag
  describe <name>               provenance: source, ref, commit, skill_version, replacements
  get <name>                    full rendered SKILL.md

  # sub-resources
  source  list | describe <id>          upstream deps from lock.json
  cache   list | prune | path           global git cache (~/.auto/skills/upstream)
  target  list                          configured output targets (default: claude, agents)
  trust   list | add <endpoint> | remove <endpoint>   machine-local approved endpoints (~/.auto/skills/trust.json)

  # migration
  migrate vercel [--from <path>] [--dry-run]
```

Command notes:
- **`init`** — `--project` launches an **interactive wizard** (the default mode)
  that sets a project up end-to-end: pick output targets (default `claude` +
  `agents`), `auto_update` on/off (default on), the default version policy
  (default `latest`), and whether targets are gitignored or committed — then
  scaffolds `./skills/`, `.auto/skills/skills.yaml`, `.auto/skills/lock.json`, and
  `.gitignore` entries. `-y` skips the prompts and takes defaults, with any
  provided flags overriding them (`--target <style>`, `--auto-update` /
  `--no-auto-update`, `--default-version <spec>`, `--commit-targets` /
  `--no-commit-targets`). Bare `init` (no `--project`) writes global
  `~/.auto/skills/settings.json`. Idempotent.
- **`add`** — parse the source (see **Source formats**) → resolve `version`→commit
  via cache (clone if absent) → discover skills in the repo (see **Skill discovery
  within a source**) → `git archive` each chosen skill's subpath → write lock +
  `skills.yaml` stub → (unless `--no-sync`) render into every output target.
  `--skill` picks specific skills (default all), `--path` restricts where to look,
  `--list` previews without adding, `--as` renames a **single** selected skill
  (hard error if combined with a multi-skill add — rename the rest in `skills.yaml`).

<!-- RESOLVED(P2): Singular alias is undefined for a multi-skill add
REVIEW: `--skill` is repeatable and defaults to importing every discovered skill, while `--as` supplies one local name. The quickstart demonstrates two selected skills together with one `--as`, but does not say which entry is renamed or how the other is named. Either restrict `--as` to exactly one selected/discovered skill or define a per-skill mapping surface.
AUTHOR: `--as` is now valid only when exactly one skill is selected/discovered; pairing it with a multi-skill add is a hard error. Multi-skill imports keep their (validated) upstream names. Fixed the quickstart that paired `--as` with two `--skill`s.
-->

- **`sync`** — read lock+`skills.yaml`; ensure each locked commit is cached; resolve
  replacements + render the full tree; write derived render state to `manifest.json`;
  then for every output target write **both** rendered vendored and authored
  `./skills/**` skills (authored shadows vendored on a clash), pruning only
  manifest-owned orphans confirmed by a machine-local receipt (foreign skills left
  for `adopt`). `--check` = dry-run, comparing each target's on-disk tree digest to
  the expected `skill_version`; exits non-zero if any target is stale (CI gate).
  By default (`auto_update: true`) floats `latest`/`branch:` to HEAD first;
  `auto_update: false` (or `--locked`) reproduces the locked commit. **`--target`
  implies `--locked`** — scoping to a subset is a partial/repair op, so it never
  floats refs or advances the lock; advancing all deps requires a full sync (which
  writes every configured target).

<!-- RESOLVED(P1): Target-scoped sync can advance the global lock without updating other targets
REVIEW: `sync --target claude` scopes writes, but with default auto-update it still floats refs and rewrites the one project-wide lock. The unselected `agents` target is then stale against the newly committed resolution even though the command can otherwise succeed. Define whether target-scoped sync is forced locked, updates every configured target when the lock advances, or records and reports the unselected targets as an incomplete transaction.
AUTHOR: `--target` now **implies `--locked`**: a scoped sync reproduces the locked commit and never floats/advances the project-wide lock, so it can't leave unselected targets stale against a new resolution. Floating (lock-advancing) happens only on a full sync, which writes every configured target.
-->

- **`doctor`** — offline: compare each target's on-disk tree digest to the expected
  `skill_version` and flag mismatches (hint: `run auto skill sync --locked`, which
  repairs without floating deps); also lists managed orphans (pruned next `sync`)
  and foreign/ad-hoc skills (candidates for `adopt`).
- **`adopt`** — move foreign (un-managed) skills found in targets into `./skills/`
  via a **staged filesystem move** (copy → verify → remove) then `git add` — never
  `git mv` (the source is usually untracked/gitignored); the next `sync` re-renders
  them. See **Managed vs. ad-hoc skills**.

<!-- RESOLVED(P2): The adopt command note still specifies git mv
REVIEW: This command note still requires `git mv`, directly contradicting the resolved adoption section, which correctly changed the operation to staged filesystem copy/verify/remove followed by `git add` because the source is normally ignored and untracked. An implementer following the CLI contract will reproduce the original failure; make the command note use the resolved algorithm.
AUTHOR: Command note now matches the resolved algorithm — staged filesystem move (copy → verify → remove) then `git add`, not `git mv`.
-->

- **`list`/`describe`/`get`** — cheap→full per the resource convention; truncated
  output prints the exact command to recover the full version.

### Source formats

`add` normalizes every surface form to one descriptor — `<host>/<repo-path>`
(+ optional ref + subpath), where `<repo-path>` is the **full ordered path** (so
nested groups nest correctly) — which is also the cache path
(`~/.auto/skills/upstream/<host>/<repo-path>.git`) and the lock `source` field.
These all resolve to `github.com/mistakenot/skills`:

| Input | Notes |
|---|---|
| `mistakenot/skills` | bare `owner/repo` → default host `github.com` |
| `github.com/mistakenot/skills` | host + path, scheme inferred |
| `https://github.com/mistakenot/skills` | full URL; trailing `.git` / `/` optional |
| `git@github.com:mistakenot/skills.git` | SSH |
| `https://github.com/mistakenot/skills/tree/<ref>/<subpath>` | browser deep link → sets ref + subpath |

Credential-bearing URLs (userinfo / token query params) are **rejected at parse**
(see Global git cache → Credential hygiene); only a sanitized canonical URL is kept.

<!-- RESOLVED(P2): Deep-link parsing is ambiguous for refs containing slashes
REVIEW: Git branch and tag names can contain `/`, so `/tree/<ref>/<subpath>` cannot be split by position without knowing which prefix resolves as a ref. The stated normalization can misparse `tree/feature/x/skills/foo` as ref `feature`. Define a longest-resolving-ref algorithm (and its network/error behavior), or require explicit `--version`/`--path` when the deep link is ambiguous.
AUTHOR: `/tree/<ref>/<subpath>` is now split by a **longest-resolving-ref** algorithm — candidate prefixes are checked against the repo's refs (via `ls-remote`/after clone) and the longest that resolves wins; the remainder is the subpath. If nothing resolves (offline or genuinely ambiguous), `add` errors and requires explicit `--version` + `--path`.
-->

Other hosts use the same rules: `gitlab.com/acme/group/skills`,
`https://gitlab.com/...`, any `git@host:…` / `ssh://…` URL (generic git fallback).
Normalization: scheme inferred when omitted; trailing `.git`/`/` stripped; host
lowercased (path preserved); the full repo path is an **ordered sequence** (no
owner/repo assumption); a `/tree/<ref>/<subpath>` (GitHub) or `/-/tree/...`
(GitLab) segment splits via the longest-resolving-ref rule above.

<!-- RESOLVED(P2): Repository identity assumes one owner level
REVIEW: The normalized descriptor and cache layout are `<host>/<owner>/<repo>`, but GitLab and many self-hosted forges support arbitrarily nested groups. Treating `acme/platform/skills` as owner/repo is ambiguous and can collide with other paths or produce the wrong clone URL. Model the full repository path as an ordered sequence and encode it safely in both cache paths and source IDs.
AUTHOR: Descriptor and cache layout are now `<host>/<repo-path>` with the full ordered path, so nested GitLab subgroups (`acme/platform/skills`) map correctly and don't collide. Matches the cache-identity fix above.
-->

**Local sources** (`./skills-repo`, `/abs/path`) are handled separately, since the
vendored model needs a git commit:

- a **git repo** → resolved to a commit like any source and tracked in the lock,
  but flagged `local: true` — its `url` is a machine path, so it is **not portable**
  to another checkout (`sync`/`update` elsewhere report it missing).
- a **non-git directory** → not vendored; `add` **imports** it (copies the skills
  into `./skills/` as authored), since there's no commit to pin. This is the
  reproducible path for ad-hoc local skills.

Import — **and authored-tree reads during `sync`** — apply the **same safety
policy** as remote extraction: reject escaping symlinks / special files, enforce
file-count and total-size limits, and verify source/destination containment; an
existing `./skills/<name>` is a collision (refuse unless `--force`).

<!-- RESOLVED(P2): Local imports bypass the archive safety policy
REVIEW: Remote extraction rejects symlinks, special files, excessive file counts, and oversized trees, but non-git import is only described as copying into `./skills/`. A local directory can contain an escaping symlink or device and can overlap the destination, causing secret ingestion, recursion, or unsafe output later. Define the same entry/resource checks for import and authored-tree sync, plus source/destination containment and collision behavior.
AUTHOR: Import and authored-tree sync now run the same entry/resource validation as remote extraction (symlink/special-file rejection, file-count + size limits, source/dest containment) and treat an existing `./skills/<name>` as a collision (refuse unless `--force`).
-->

<!-- RESOLVED(P1): Local sources cannot satisfy the commit-based lock model
REVIEW: Local paths are copied and not cached, yet every vendored lock entry requires a git `commit`, `ref`, and archive-at-SHA source of truth. A local directory may not be a git repo, can change after `add`, and may not exist on another checkout, so update, sync, migration, and reproducibility semantics are undefined. Either exclude local paths from the vendored/lock model, snapshot them into a content-addressed store with a tree digest, or require a git repository and define how its identity is made portable.
AUTHOR: Split local handling: a local **git repo** is pinned to a commit and locked with `local: true` (non-portable, reported missing on other checkouts); a **non-git dir** is *imported* into `./skills/` as an authored skill rather than vendored. Either way the commit-based lock invariant holds.
-->

### Skill discovery within a source

A source repo can hold one skill or dozens, anywhere in its tree. `add` separates
**where to look** from **which to take** — mirroring `npx skills`' flexibility.

**Where to look (discovery roots), in priority order:**

1. **An explicit folder** — `--path <dir>` (repeatable), or the `<subpath>` from a
   `/tree/<ref>/<subpath>` deep link. When given, *only* those folders are scanned.
2. **Otherwise the default container scan** — repo root (if it has `SKILL.md`),
   then `skills/`, then the agent dirs (`.agents/skills/`, `.claude/skills/`, …).
   Each container is walked one level for the flat layout
   (`skills/<name>/SKILL.md`) and one extra level for catalog layouts
   (`skills/<category>/<name>/SKILL.md`); a `SKILL.md` found shallower **shadows**
   anything nested under it (borrow `I006`).
3. **`--full-depth`** — recursive fallback for skills in non-standard locations
   (used automatically if the default scan finds nothing, or when forced).

Cross-container **dedupe by name**: a source repo that committed its rendered
output will expose the same skill in several places (e.g. `skills/foo` *and*
`.claude/skills/foo` *and* `.agents/skills/foo`). Copies collapse **only when their
full tree digests are identical** (preferring `skills/`, then root, then an agent
dir). If same-named copies have **diverged**, discovery is a hard error listing
each path + digest and requires an explicit `--path` to choose — never a silent pick.

<!-- RESOLVED(P2): Name dedupe silently discards divergent skill trees
REVIEW: The precedence rule assumes same-named copies are equivalent but never compares their full tree digests. If `skills/foo` and `.agents/skills/foo` have diverged, `add` silently selects one and hides the conflict, potentially installing a stale or incomplete tree. Collapse only byte-identical trees; otherwise fail discovery with the paths/digests and require explicit `--path`.
AUTHOR: Dedupe now collapses only byte-identical (digest-equal) trees; divergent same-named copies are a hard error showing paths+digests and require `--path`. No silent selection of a stale/incomplete tree (mirrors the adopt divergence rule).
-->

**Which to take:**

- **Default: all** discovered skills — each becomes its own lock entry + render,
  sharing the source/commit, differing by `subpath` and name.
- **`--skill <name>`** — **exact** skill-name match (not fuzzy, unlike doc-section
  selection). Pass it multiple times to import several skills
  (`--skill release --skill changelog`), or `'*'` for all. Names with spaces must
  be quoted.
- **Invalid upstream names** — a discovered skill whose name doesn't satisfy the
  enforced schema (`^[a-z0-9]+(-[a-z0-9]+)*$`) is **not silently normalized**:
  `--list` flags it "needs `--as`", and import is **rejected** unless a valid
  `--as <name>` is supplied. We never advertise a name `lint`/`sync` would later
  refuse, and never create an unsafe output directory from upstream text.

<!-- RESOLVED(P2): Remote-name handling conflicts with the enforced skill schema
REVIEW: Current auto-skill validation requires `^[a-z0-9]+(?:-[a-z0-9]+)*$`, so quoting a discovered name with spaces cannot make it a valid local skill name or safe output directory. Define whether invalid upstream names are rejected with `--as` remediation or deterministically normalized, including collision handling; do not advertise names that later lint/sync cannot accept.
AUTHOR: Invalid upstream names are rejected (not silently slugified) with `--as` remediation; `--list` marks them "needs --as". A name lint/sync can't accept is never advertised or installed; `--as` (single-skill only) supplies the valid one.
-->

- **`--list`** prints what was discovered and exits — preview before choosing.
- **No `--skill` given → import all** (the default; same behavior in a TTY and in
  `-y` / non-interactive runs — no selection prompt). Use `--list` to preview or
  `--skill` to narrow.

`--path` and `--skill` compose: `--path` scopes the search, `--skill` filters the
results. A `--skill` that matches nothing in scope is a hard error that lists what
*was* found (remediation per repo convention).

```bash
auto skill add mistakenot/skills --list                          # what does this repo expose?
auto skill add mistakenot/skills --skill deploy                  # take just one
auto skill add mistakenot/skills --skill '*'                     # all, explicitly (CI/non-interactive)
auto skill add mistakenot/skills --path .agents/skills           # only look in a specific folder
auto skill add github.com/mistakenot/skills/tree/main/packages/x # ...or scope via the URL subpath
auto skill add mistakenot/skills --full-depth                    # search the whole tree
```

### Output targets

`sync` writes into output targets directly — there is no canonical
`./skills/.vendor/` staging tree. Each target receives the union of (rendered
vendored skills) + (authored `./skills/**` skills), with authored shadowing
vendored on a name clash.

Two default targets cover most setups in two writes:

| Style | Path | Covers |
|---|---|---|
| `claude` | `.claude/skills/` | Claude Code |
| `agents` | `.agents/skills/` | the generic `.agents` ecosystem (Codex, Cursor, OpenCode, Cline, …) |

Targets are configurable (add/remove styles in project settings); these two are
the defaults. This replaces vercel's 70-agent registry with a small set of output
*styles* (`I001` shrinks to ~2 defaults). **Copy/render only** — no symlinks:
rendered output is per-project and per-target, so vercel's symlink-to-canonical
model doesn't apply. A `--global` variant (writing to `~/.claude/skills/` etc.)
can follow later.

## Sync performance (parallel fetch)

`sync`'s bottleneck is the network — exactly where `npx skills` is slow. The
pipeline is structured in three phases so the slow part is fully parallel:

**A. Plan (no network).** Read lock + `skills.yaml`; compute the *distinct* set of
repos that actually need a network op:

- **dedupe by repo** — N skills from one repo = **one** fetch, not N;
- **skip what the cache already satisfies** — a commit whose objects are present
  needs nothing; with `auto_update` off (or `--locked`), present commits need
  nothing. Often phase B is empty and `sync` is effectively offline.
- **locked materialization** — on a **cache miss** (fresh checkout, or a commit
  evicted by `cache prune`), even a locked/pinned `sync` fetches the **exact pinned
  commit's objects**; it does *not* re-resolve the ref, so it stays reproducible.
  "Locked = no fetch" was imprecise — locked means **no ref re-resolution**. If the
  server no longer advertises the pinned object (history rewrite, force-push, GC),
  `sync` fails with "pinned commit unavailable upstream" rather than silently
  advancing.
- floating specs (`latest`/`branch:`) on an `auto_update` run additionally
  **re-resolve** the ref, then fetch.

<!-- RESOLVED(P1): Locked sync cannot be network-free on a cache miss
REVIEW: The auto-update-off contract says `sync` does not fetch and the CLI network table only mentions floating refs, but a fresh checkout or a cache entry evicted by `cache prune` has no objects for the locked commit; the cache section promises eviction is recovered by the next `sync`. Define locked materialization as an allowed clone/fetch that does not re-resolve the ref, including the failure when a server no longer advertises the pinned object, and correct the network/"no fetch" claims.
AUTHOR: Defined locked materialization — a locked sync may fetch the exact pinned commit's objects on a cache miss (no ref re-resolution → still reproducible), and fails with "pinned commit unavailable upstream" if the object is gone. Corrected "no fetch" to "no ref re-resolution" and updated the CLI network column.
-->

**B. Fetch in parallel.** Clone/fetch the needed repos concurrently through a
bounded worker pool (`--jobs`, default 8). Fetches are network-bound, so
concurrency above core count helps; the cap keeps us polite and dodges host
rate-limit / SSH connection limits. Each repo takes its per-repo cache lock;
blobless cloning keeps each transfer small. A single repo failing does **not**
abort the run — errors are collected and reported, valid repos still process, and
`sync` exits non-zero if any failed (per repo convention).

**Commit protocol (crash-consistent).** `sync` uses a write-ahead **journal**
(`.auto/skills/.sync-journal`) and a fixed commit order so any crash leaves a
recoverable state:

1. Stage each skill's rendered tree into a temp dir on the **same filesystem** as
   its target (so the swap is an atomic rename); journal the intended writes/prunes
   + digests.
2. Swap staged dirs in per skill via rename; an existing non-empty dir is moved to a
   journaled trash dir first, never deleted in place (handles non-empty/cross-FS).
3. Write machine-local **receipts** (what this machine wrote, with digests).
4. Write `manifest.json`, then `lock.json` — both atomic rename, manifest before lock.
5. Clear the journal (the commit point), then drop journaled trash.

A non-empty journal on the next run triggers **recovery**: re-derive desired state
and roll forward (re-apply staged) or back (restore trash), reconciling
receipts+manifest so tool output never looks foreign and the manifest never claims
bytes the tool didn't write. Pruning stays **suppressed when the desired set is
incomplete** (a failed fetch never deletes or half-advances).

<!-- RESOLVED(P1): The sync transaction has no crash-consistent commit protocol
REVIEW: Per-directory swaps and one atomic `lock.json` rename do not form one transaction, and the ownership-authoritative `manifest.json` is omitted entirely. A crash after an output swap but before the manifest/lock writes can make tool-written output appear foreign; the opposite ordering can make the manifest claim bytes not written by the tool. Atomic directory replacement is also not portable across existing non-empty directories or target filesystems. Define a journal/recovery protocol and the commit order across every selected target, the lock, and the manifest (or explicitly make each skill resumable and prove all intermediate states safe).
AUTHOR: Replaced the loose "atomic swap" with a journaled, ordered commit protocol (stage same-FS → swap per skill, old moved to journaled trash → receipts → manifest → lock → clear journal). A non-empty journal triggers recovery (roll forward/back) reconciling receipts+manifest, so a crash can't make output look foreign or the manifest claim un-written bytes; non-empty/cross-FS dirs use staging+trash, not in-place replace.
-->

<!-- RESOLVED(P1): Partial sync has no atomicity or rollback contract
REVIEW: Continuing after a repo failure while processing valid repos can partially advance an auto-updating lock, rewrite some targets, and prune others before exiting non-zero. Parallel workers also need a single serialization point for `lock.json`. Define a transaction boundary: stage all lock/output changes, atomically replace files/directories only after the required plan succeeds, and suppress pruning on an incomplete desired-state calculation (or explicitly specify and test resumable partial-state semantics).
AUTHOR: Added a transaction boundary — staged renders atomically swapped per skill, single-writer atomic `lock.json` replace, and **pruning suppressed when the desired set is incomplete** (a failed fetch never triggers deletion or a half-advanced lock). Independent successes still apply; failures are isolated.
-->

**C. Process (parallel, pure).** Once content is local, extract (`git archive`) →
render → write to targets, parallel across skills and **incremental**: a target is
skipped only when its **actual on-disk tree digest** equals the freshly computed
`skill_version` (the full rendered-tree digest). A user edit, truncated side file,
or forged stamp changes the on-disk digest → mismatch → re-render. Presence and the
embedded stamp are never trusted on their own. Output is identical regardless of order.

<!-- RESOLVED(P1): Incremental sync ignores modified or corrupt target content
REVIEW: The skip condition checks only `skill_version` and existence. A user edit, truncated side file, or forged/copied stamp can leave target bytes different from the expected render while sync skips the directory; this also undermines `sync --check` as a CI gate. Compare a full target-tree digest (or expected staged tree) rather than trusting the embedded version stamp and presence alone.
AUTHOR: The skip condition now compares the **actual on-disk target tree digest** against the expected `skill_version` (full rendered-tree digest). Any edit/truncation/forged stamp forces a re-render; `--check` uses the same comparison, so it's a real CI gate.
-->

`sync --check` skips phase B entirely (offline), so it's always fast. `--jobs`
also bounds phase C's render parallelism.

## Managed vs. ad-hoc skills (pruning, renames, adoption)

Two messy realities to handle: skills get **renamed** (`beta-new-plan` →
`new-plan`), leaving an orphaned old copy in the targets; and **ad-hoc** skills
sometimes get hand-dropped straight into `.claude/skills/` or `.agents/skills/`
instead of `./skills/`. `sync` must clean up the first without clobbering the
second — and it can only tell them apart if it marks what it wrote.

### Ownership — checked-in manifest (desired) + machine-local receipts (deletion authority)

Two records, two jobs:

- **`.auto/skills/manifest.json`** (checked in) holds the **derived render state** —
  per skill: `template_hash`, resolved replacement literals, file-ref
  `content_hash`es + `matched_heading`, and `skill_version`; and per target: which
  skills are *managed* + their expected `skill_version`. It declares **what should
  exist** and is regenerable. It is **not** a deletion authority — a branch/merge/
  manual edit can add an entry for a pre-existing foreign dir.
- **Machine-local receipts** (`~/.auto/skills/receipts/<project-id>.json`, never
  checked in) record what **this machine actually wrote** (target → name → digest).
  These are the **deletion authority**.

A skill is prune-eligible only when (a) it's a manifest-managed orphan **and** (b) a
local receipt confirms this machine wrote that exact digest there. A manifest entry
introduced by someone's commit, with no local receipt, is **never auto-deleted** —
it's reported until this machine establishes it (renders + receipts it) on a normal
sync. The in-file `metadata.auto_skill` stamp stays informational only (added after
the digest, excluded from it); a hand-written `managed: true` proves nothing.

<!-- RESOLVED(P1): A checked-in manifest does not prove tool ownership
REVIEW: Moving ownership from a self-stamp to a checked-in manifest changes the location but not the trust property: a branch, merge, or manual edit can add `target/name/digest` for a pre-existing foreign directory, after which orphan pruning treats it as "provably ours" and may delete it. This is especially unsafe with automatic post-checkout sync. The prior review asked for project/root binding or an unforgeable ownership token; define a machine-local ownership secret/receipt or make imported manifest entries non-deleting until locally established.
AUTHOR: Added machine-local receipts (`~/.auto/skills/receipts/<project-id>.json`, not checked in) as the deletion authority; the checked-in manifest only declares desired/managed state. Prune requires BOTH a manifest orphan AND a matching local receipt (correct digest) — so a manifest entry from someone's commit can't authorize deleting a pre-existing foreign dir, even under automatic post-checkout sync. Imported-but-not-locally-established entries are reported, never deleted.
-->

<!-- RESOLVED(P2): Authored skill versions are undefined
REVIEW: Every rendered authored copy is stamped with `skill_version`, and list/check/incremental sync rely on that value, but the only version algorithm is defined for a remote template plus replacements and authored skills are excluded from the lock. Define an authored-tree hash (including side files and the render/stamp schema version) so authored target drift and changes have deterministic semantics.
AUTHOR: Resolved by the unified hashing definition — `skill_version` is the full rendered-output-tree digest for BOTH authored and vendored skills (authored input = its own `./skills/<name>/` tree incl. side files, plus `render_version`). The manifest stores that digest, so authored target drift has the same deterministic semantics as vendored.
-->

### Pruning orphans — renames need no special detection

On each `sync`, per target:

- compute the **desired set** = authored skills (`./skills/`) ∪ vendored skills
  (lock), after shadowing.
- a skill **in the manifest** whose name is **not** in the desired set is a
  **managed orphan**. It is deleted only when **both**: a machine-local receipt
  confirms this machine wrote it, **and** the on-disk dir still matches that
  receipt's digest. No local receipt (manifest entry from someone's commit), or a
  **modified** dir → do **not** delete; report for `adopt`/manual handling. A rename
  is exactly this — `new-plan` written, `beta-new-plan` left as a manifest orphan
  with a stale receipt → pruned. No rename detection needed.

<!-- RESOLVED(P1): A forgeable frontmatter stamp can delete user data
REVIEW: Ownership is inferred solely from content inside the file being considered for deletion. Any ad-hoc or third-party skill can already contain `metadata.auto_skill.managed: true`, accidentally or deliberately, and will then be pruned as an orphan. The recovery claim is also false for the explicitly supported gitignored/untracked-target mode. Use an external per-target manifest tied to a project/root identity (or an unforgeable ownership token plus manifest), verify the expected prior digest before deletion, and avoid default deletion when provenance cannot be proven.
AUTHOR: Ownership moved out of the file into a per-project manifest (`.auto/skills/manifest.json`) keyed by target+name+digest; the in-file stamp is informational only and never authorizes deletion. Prune verifies the recorded digest before removing — modified or foreign dirs are reported, not deleted — so a forged `managed: true` can't cause data loss, and the gitignored case is covered by the digest check, not a false git-recoverability claim.
-->

- a dir **not in the manifest** is **foreign / ad-hoc** → never deleted; reported as
  adoptable.

Deletions of committed targets are git-recoverable; for gitignored targets the
content is regenerable from lock + sources (it's derived) — the digest check is what
protects manual edits from silent loss.

#### Desired skill collides with a foreign dir

If a desired skill name (`foo`) collides with an existing **foreign**
(un-manifested) dir in a target, that's a **hard conflict**: `sync` neither
overwrites nor prunes it, and reports remediation — `adopt foo` (make it authored),
rename the incoming skill with `--as`, or `--force` to overwrite. Neither normal
sync nor pruning ever mutates a foreign directory.

<!-- RESOLVED(P1): Desired output colliding with a foreign skill has no safe behavior
REVIEW: The desired set may contain `foo` while the target already has an unstamped `foo`. The design simultaneously says sync writes every desired skill and never deletes foreign skills, but replacing/merging that directory would clobber foreign data while skipping it leaves the target unreconciled. Specify a hard conflict with remediation (`adopt`, rename, or explicit force) and ensure neither normal sync nor pruning mutates the foreign directory.
AUTHOR: Defined a hard conflict: a desired skill whose name matches a foreign (un-manifested) dir is neither overwritten nor pruned; sync reports it with remediation (`adopt`, `--as` rename, or `--force`). Foreign dirs are never mutated by normal sync or pruning.
-->

### Adopting ad-hoc skills → `./skills/`

`auto skill adopt` finds foreign (un-managed) skills in the targets and moves them
into the canonical `./skills/<name>/`, so they become authored and managed; the
next `sync` re-renders them into every target. Because an ad-hoc skill in a target
is usually untracked (gitignored target), adoption uses a **filesystem move**
(copy → verify → remove) then `git add`s the new `./skills/<name>/` — **not**
`git mv`. If `./skills/<name>/` already exists, `adopt` refuses unless `--force`;
the move is staged so a failure rolls back cleanly.

<!-- RESOLVED(P2): `git mv` does not work for the primary ad-hoc case
REVIEW: A skill hand-dropped into a usually gitignored target is untracked, so `git mv` fails with "not under version control." Adoption must use filesystem-safe copy/move semantics and then optionally `git add`, while defining rollback and behavior when `./skills/<name>` already exists.
AUTHOR: Adoption now uses a filesystem move (copy → verify → remove) then `git add`, not `git mv` (the ad-hoc dir is typically untracked). Defined existing-target behavior (refuse unless `--force`) and a staged move for clean rollback.
-->

```bash
auto skill adopt                 # list foreign skills in targets, pick which to adopt (interactive)
auto skill adopt new-plan        # adopt a specific one
auto skill adopt --all -y        # adopt everything found, no prompts
auto skill adopt new-plan --from claude   # disambiguate when copies in different targets diverge
```

If the same skill name sits in several targets, `adopt` compares their **full tree
digests**: identical → adopt the single copy; **divergent → hard error** listing
each path + digest, requiring `--from <target>` to choose. It never silently picks
one and discards the others.

<!-- RESOLVED(P2): Adoption chooses divergent copies non-deterministically
REVIEW: "Richest/most-recent" has no defined ordering, and filesystem mtimes differ across clones, copies, and platforms. More importantly, silently selecting one divergent skill can discard meaningful content in the others. Compare full tree digests; if copies differ, fail and present the conflicting paths/digests unless the user explicitly chooses a source.
AUTHOR: Replaced "richest/most-recent" with full-tree-digest comparison: identical copies adopt as one; divergent copies are a hard error showing paths+digests and require `--from <target>`. No silent, mtime-dependent selection or data loss.
-->

### Renamed *vendored* skills

For a vendored skill that upstream renamed, the locked `subpath` no longer resolves
on `update`; `auto skill update` reports it (“`beta-new-plan` not found at its
locked path — renamed or removed upstream?”) and you re-`add` the new name (or
`remove` the stale entry). Its orphan in the targets is pruned on the next `sync`
like any other. A stale `skills.yaml` `replacements` entry for a vanished skill is
flagged by `lint`/`doctor` ("no such skill — did you mean `new-plan`?").

### `doctor`

`auto skill doctor` summarizes both lists — managed orphans (pruned next `sync`)
and foreign skills (candidates for `adopt`) — so nothing rots silently.

## Developer quickstart

The full lifecycle end-to-end, exercising every command and flag. Persistent
flags available on **all** commands: `--root <path>` (override the project root —
where `./skills/` and `.auto/` live), `--format json|text` (default `json`;
diagnostics always go to stderr).

### 0. One-time setup

```bash
auto skill init --project          # interactive wizard: targets, auto_update, version policy, gitignore vs commit
auto skill init --project -y       # non-interactive: take defaults (claude+agents, auto_update on, latest)
auto skill init --project -y \     # ...or take settings from flags
  --target claude --no-auto-update --default-version latest --commit-targets
auto skill init                    # (global) write ~/.auto/skills/settings.json — idempotent
auto skill doctor --format text    # confirm config + report any drift
```

### 1. Author a local skill (optional)

```bash
auto skill create deploy \
  --description "Use when deploying the service to GKE." \
  --with-dirs                       # also create references/ scripts/ assets/
auto skill lint ./skills/deploy     # validate one skill; omit path to lint all
```

### 2. Add a remote skill dependency

```bash
# all of these resolve to the same repo — github.com/mistakenot/skills:
auto skill add mistakenot/skills                     # bare owner/repo → default host github.com
auto skill add github.com/mistakenot/skills          # host + path, no scheme
auto skill add https://github.com/mistakenot/skills  # full URL (.git optional)
auto skill add git@github.com:mistakenot/skills.git  # SSH

# paste a browser deep link — ref + subpath are read from the URL:
auto skill add https://github.com/mistakenot/skills/tree/v2.3.0/skills/release

# other hosts and local paths, same rules:
auto skill add https://gitlab.com/acme/group/skills
auto skill add ./local/skills-repo

# pin a version + rename a single skill (--as is single-skill only):
auto skill add mistakenot/skills \
  --skill release \                      # one skill
  --version v2.3.0 \                     # latest | <tag> | <sha> | branch:<name> (overrides a /tree/ ref)
  --as acme-release \                    # local name override (collision / clarity)
  --no-sync                              # write lock + skills.yaml entry but skip render for now

# import several at once (validated upstream names kept; no --as):
auto skill add mistakenot/skills --skill release --skill changelog
```

`add` resolves the version → commit via the git cache (cloning it blobless if
absent), `git archive`s the subpath, records the entry in `.auto/skills/lock.json`
+ a stub in `skills.yaml`, and (unless `--no-sync`) renders it into every output
target. See **Source formats** under the CLI surface for every accepted input.

### 3. Customize it

Edit `.auto/skills/skills.yaml` — add literals and file-refs under
`replacements:` (see schema above):

```yaml
auto_update: true                   # default — always-latest; set false to reproduce the lock
shared:
  version: latest
  replacements:
    project_name: auto-stack
skills:
  acme-release:
    version: v2.3.0
    replacements:
      changelog_path: CHANGELOG.md                 # literal
      conventions:                                 # file-ref, single section
        file: docs/conventions.md
        section: "Release"
        include_heading: false
```

### 4. Sync — render + write to targets (the workhorse)

```bash
auto skill sync                      # cache → render → write authored + vendored into all targets
auto skill sync --target claude      # scope to one target style (repeatable; default: claude + agents)
auto skill sync --check              # dry-run: report stale targets, exit non-zero if any (CI gate)
```

`sync` renders at the **locked** commit (reproducible) and writes both authored
and vendored skills into every output target. It re-renders any skill whose
`skill_version` changed because you edited a value or a referenced doc — no
network needed. By default (`auto_update: true`) it first floats `latest`/`branch:`
specs to current HEAD; set `auto_update: false` for reproducible-by-lock renders.

### 5. Inspect

```bash
auto skill list                      # all skills (authored + vendored), ids + metadata + stale flag
auto skill list --vendored           # only remote-sourced skills   (or --local for authored)
auto skill describe acme-release     # provenance: source, ref, commit, skill_version, replacements
auto skill get acme-release          # the full rendered SKILL.md
auto skill get acme-release --format text   # raw markdown to stdout
auto skill source list               # upstream deps as recorded in the lock
auto skill source describe github.com/acme/agent-skills
```

### 6. Update to newer upstream

```bash
auto skill update --check            # git fetch; report which floating skills have newer commits (writes nothing)
auto skill update                    # advance latest/branch: + re-resolve tags (force-moves warned); commits stay pinned
auto skill update acme-release       # scope to specific skills
```

### 7. Cache & targets

```bash
auto skill cache list                      # cached repos + sizes + last fetch
auto skill cache path github.com/acme/agent-skills   # print the on-disk cache path
auto skill cache prune --dry-run           # preview default eviction (age/size LRU)
auto skill cache prune                      # evict by age/size (safe — re-fetched on next sync)
auto skill cache prune --unreferenced      # also drop repos no known project references (registry)
auto skill cache prune --max-age 30d --max-size 2g   # tune eviction thresholds
auto skill target list                     # configured output targets (default: claude, agents)
```

<!-- RESOLVED(P2): Cache-prune examples and flags contradict the resolved policy
REVIEW: The cache design says default `prune` is age/size eviction and reference-aware deletion requires `--unreferenced`, but these examples say default prune shows/deletes repos with no lock references, and the full options table does not expose `--unreferenced` (or age/size controls). This is a destructive CLI contract mismatch; align the examples and complete the option surface before implementation.
AUTHOR: Examples now match the resolved policy — default `prune` is age/size LRU eviction (safe), `--unreferenced` adds registry-based reference-aware deletion, and `--max-age`/`--max-size` tune thresholds. Options table updated to expose them.
-->

### 8. Remove

```bash
auto skill remove acme-release            # errors if both an authored AND vendored skill share the name
auto skill remove acme-release --vendored # drop the lock entry (an authored shadow, if any, remains)
auto skill remove acme-release --local    # delete ./skills/<name>/ (a shadowed vendored skill re-appears next sync)
auto skill remove acme-release --yes      # skip confirmation
```

<!-- RESOLVED(P2): Remove is ambiguous when an authored skill shadows a vendored skill
REVIEW: The layering model explicitly allows authored and vendored skills with the same name, but `remove <name>` can mean deleting the authored directory or dropping the lock entry. A destructive command needs an unambiguous selector (for example `--local`/`--vendored`) and must state whether removing the authored shadow reveals and re-renders the vendored skill.
AUTHOR: `remove <name>` now requires `--local` / `--vendored` when both exist (hard error otherwise). `--local` deletes `./skills/<name>/` — a same-named vendored skill stops being shadowed and re-renders on the next sync; `--vendored` drops the lock entry and leaves any authored shadow. Flags added to the CLI tree + options table.
-->

### 9. Migrating from `npx skills`

```bash
auto skill migrate vercel --dry-run            # show what would import from ./skills-lock.json
auto skill migrate vercel --from ./skills-lock.json
auto skill sync                                 # resolve commits + render the migrated deps
# then: delete skills-lock.json and the npx skills wiring
```

### CI one-liner

```bash
auto skill sync --check --format json    # fail the build if any rendered skill is stale vs lock/values/docs
```

### Full options reference

| Command | Args | Options |
|---|---|---|
| `init` | | `--project`, `-y`, `--target <style>` (repeatable), `--auto-update` / `--no-auto-update`, `--default-version <spec>`, `--commit-targets` / `--no-commit-targets` |
| `create` | `<name>` | `--description <text>` (required), `--with-dirs` |
| `lint` | `[path]` | (persistent only) |
| `add` | `<source>` | `--skill <name>` (repeatable, or `'*'`), `--path <dir>` (repeatable), `--list`, `--full-depth`, `--version <spec>`, `--as <name>`, `--no-sync` |
| `sync` | | `--check`, `--locked` (alias `--no-update`), `--target <style>` (repeatable), `--jobs <n>` |
| `update` | `[name...]` | `--check` |
| `remove` | `<name>` | `--local`, `--vendored`, `--yes` |
| `adopt` | `[name...]` | `--all`, `--from <target>`, `--force`, `-y` |
| `list` | | `--local`, `--vendored` |
| `describe` | `<name>` | (persistent only) |
| `get` | `<name>` | (persistent only) |
| `source` | `list` \| `describe <id>` | (persistent only) |
| `cache` | `list` \| `path <id>` \| `prune` | `prune`: `--dry-run`, `--unreferenced`, `--max-age <dur>`, `--max-size <size>` |
| `target` | `list` | (persistent only) |
| `trust` | `list` \| `add <endpoint>` \| `remove <endpoint>` | (persistent only; endpoint = `scheme://host:port` or local path) |
| `migrate vercel` | | `--from <path>`, `--dry-run` |
| `doctor` | | (persistent only) |
| `quickstart` / `docs` | | (persistent only) |

Persistent (all commands): `--root <path>`, `--format json|text`.
`<spec>` for `--version`: `latest` (default) \| `<tag>` \| `<sha>` \| `branch:<name>`.

## `migrate vercel`

Reads vercel's checked-in `skills-lock.json` (entries:
`{source, sourceType, ref, skillPath, computedHash}`) and translates into
`.auto/skills/lock.json` + `skills.yaml`:

1. Each entry → a lock entry with `source`/`url`/`subpath` mapped across and
   `"state": "unresolved"` (no `commit` yet — their hash ≠ ours). `unresolved` is a
   **valid, versioned migration state**: only `add`/`update`/`sync` may consume it
   (resolve `ref`→commit, flip to `resolved`, write the manifest); `sync --check`/
   `doctor` report unresolved entries as "run `auto skill sync` to resolve" and
   never treat them as commit-addressable.

<!-- RESOLVED(P1): Migration emits lock entries that violate the lock schema
REVIEW: Every normal lock entry requires a resolved commit and hashes, while `sync`, `doctor`, and `sync --check` assume a commit-addressable cached tree; migration deliberately writes empty required fields without a state discriminator. With auto-update off, the documented sync path also refuses reconciliation except for version-intent changes. Define a versioned `unresolved` migration state and exactly which commands may consume it, or resolve entries before atomically publishing a valid lock.
AUTHOR: Migration writes a versioned `"state": "unresolved"` discriminator instead of empty required fields; only add/update/sync may consume+resolve it, and check/doctor report it as needs-resolve. No entry pretends to be commit-addressable before resolution.
-->

2. Seed `skills.yaml` per skill: empty `replacements: {}` **and an explicit
   `version` derived from the vercel `ref`** — a tag/sha ref becomes that pin, a
   branch ref becomes `branch:<name>`, a bare default-branch ref becomes `latest`.
   So a migrated skill isn't silently reclassified stale-by-intent and advanced to
   an unrelated commit on the first auto-update sync.

<!-- RESOLVED(P1): Migration does not preserve authoritative version intent
REVIEW: `skills.yaml` is authoritative for version policy, but migration seeds only replacements. The migrated entry therefore inherits the default `latest`, regardless of the vercel `ref` copied into the lock, and default-on sync can classify it stale-by-intent and advance to an unrelated default-branch commit. Seed an explicit version spec that preserves each migrated ref/resolution, or require the user to choose a policy before sync.
AUTHOR: Migration seeds an explicit `version` from each vercel `ref` (tag/sha → pin, branch → `branch:`, default → `latest`), so intent is preserved and default-on sync won't reclassify it stale-by-intent and jump to an unrelated commit.
-->

3. **Per-source handling** (return valid results, exit non-zero if any failed):
   map `github` / `gitlab` cleanly; for `local`, **apply the local-source split** —
   a git repo → `unresolved` lock entry (`local: true`, non-portable), a non-git
   dir → import into `./skills/` as authored (missing path → reported, skipped);
   **warn + skip** `node_modules` / `well-known` / `huggingface` / `mintlify`,
   listing them.

<!-- RESOLVED(P2): Vercel local migration contradicts the local-source split
REVIEW: This step says `local` entries map cleanly into the lock, but the resolved local-source design permits a lock entry only for a git repository and imports a non-git directory into `./skills/` as authored. Migration must inspect each local source and apply that split (including missing/non-portable path handling) rather than treating every vercel local entry as a vendored dependency.
AUTHOR: Migration now inspects each `local` source and applies the resolved split (git repo → local lock entry; non-git dir → authored import; missing path → reported/skipped) instead of treating every local entry as a vendored dep.
-->

4. Print: "migrated N deps, skipped M (unsupported); run `auto skill sync` to
   resolve commits and render." `--dry-run` reports without writing; `--from`
   overrides the default `./skills-lock.json`.

Migration is additive — it does not delete vercel's lock or touch installed
files. You delete `skills-lock.json` and drop the `npx` dependency yourself once
`sync` works.

## Deprecations / renames from today's surface

Decisive, no long-term aliases (per repo convention):

| Today | Problem | Becomes |
|---|---|---|
| `update` (updates the **binary**) | needs to mean "update skills" | move self-update to root `auto update`; reclaim `auto skill update` |
| `sync` (shells out to `npx skills add`) | the thing we're replacing | native `sync`; delete the `npx` exec |
| `ls` | ad-hoc vs. resource triad | `list` (+ `describe`/`get`) |
| `agents` (writes a snippet into CLAUDE.md/AGENTS.md) | overlaps the new output-target concept | fold into `init` (output dirs are `target list`) |

## Scope trims vs. vercel

Because our context differs, several vercel features are intentionally dropped:

| vercel feature | decision |
|---|---|
| 70-agent registry | hardcode the few agents in use (`I001` shrinks) |
| symlink + copy install modes | copy-only (templating forces it) |
| GitHub Trees API / blob fast-path | clone into the git cache instead (`I009` simplified) |
| provider abstraction / GitLab / `.well-known` | one git path covers all hosts (`I011` dropped) |
| plugin-manifest discovery | dropped (`I007`) |
| `find` / skills.sh registry | dropped (no central registry) |
| telemetry | dropped — local-first (`I013`) |

## Git hooks integration

The repo already installs pre-commit hooks (`make install-hooks`). This system
exposes a few things hooks would naturally call:

- **pre-commit — `auto skill sync --check`:** fail the commit if any output target
  is stale versus the lock, `skills.yaml`, or a referenced doc. The local twin of
  the CI gate. Most valuable when targets are committed (keeps the rendered
  `.claude/skills/` / `.agents/skills/` honest), and when a `file-ref` doc was
  edited — the reverse index (`file → skills`, built from the lock's
  `replacements.files[].path` edges) tells the hook exactly which skills a changed
  doc invalidates, without scanning everything.
- **pre-commit — `auto skill lint`:** validate skills (frontmatter schema, token
  budgets, terminal-escape + traversal safety) on the rendered output before they
  land.
- **post-merge / post-checkout — `auto skill sync --locked`:** re-materialize
  targets after a pull or branch switch so agent dirs match the new lock /
  `skills.yaml` (the `npm ci`-after-pull move). Required when `commit_targets:
  false` (targets are regenerated); a safety reconcile when committed. Use
  `--locked` so a routine checkout never floats deps.
- **(optional) pre-push — `auto skill update --check`:** warn (don't block) if a
  floating skill has newer upstream than the lock.

Two design choices make this cheap: `sync --check` is **offline** (recompute
`skill_version` from cache + values + docs, no network — it does not float even
when `auto_update` is on), and the lock's file-ref edges give the reverse index so
a doc change maps straight to its dependent skills.

## Open questions / future

### Decided (was open; now specified above)

- **Project identity** → reuse the existing `~/.auto/projects.json` `id` (from
  `auto init --project`); receipts key on absolute target path so **worktrees**
  share the id without colliding; `cache prune --unreferenced` reads `projects.json`.
- **Trust bootstrap** → interactive approve-prompt; fail-closed in CI unless
  `--trust-requested`.
- **`render_version`** → lazy one-time re-render on the next `sync` after a bump.
- **`commit_targets`** → defaults to **`true`** (targets committed for zero-setup
  checkouts).

### Defaults chosen (tune later, not blocking)

- **Heading matching** — exact-normalized + slug; divergent same-named copies error
  (looser rungs only if real usage misses).
- **Token budgets** — body warn >4000 / error >8000 tokens; listing warn >2000 /
  error >4000 (chars/4 estimate); `lint` gates, `sync` only warns.
- **`--jobs`** — default 8 (network-bound; also bounds render parallelism).
- **Cache prune** — `--max-age 90d`, `--max-size 5g` (LRU eviction).

### Deferred scope (intentionally out of v1)

- **Section fragment v2** — sub-section ranges / line anchors beyond a whole heading
  subtree.
- **`use` command** (`I012`) — pipeable prompt without installing.
- **`--global` install** variant (`~/.claude/skills/` etc.).
- **`auto-watch` wiring** — doc-change → re-render trigger via the `file → skills`
  reverse index.
- **More `migrate` sources** beyond vercel; provider abstraction / non-git hosts
  (`.well-known`, HF) remain dropped (`I011`) unless demand appears.

### Accepted tensions (decided — noted so they don't resurface)

- **`auto_update: true` default** trades supply-chain review for freshness
  (mitigated by machine-local transport trust + `lint`; opt out with
  `auto_update: false` or `--locked`).
- **Trust is machine-local, not shared** — teams re-approve per machine; the price
  of closing the checked-in-lock self-authorization hole.
- **`commit_targets: true` default** — rendered targets are committed (duplicated
  content + noisier diffs) in exchange for zero-setup checkouts; flip to `false` for
  `node_modules`-style regenerate-on-sync.
