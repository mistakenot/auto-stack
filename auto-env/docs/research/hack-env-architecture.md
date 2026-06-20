---
hash: ""
id: "b84550b7"
read_when: "designing auto-env's environment/config resolution layer, an env-overlay model, a secrets story, or worktree-shared key handling; or comparing auto-env's template model against a mature sibling"
summary: "Architectural study of how hack-dance/hack models, resolves, encrypts, and injects per-project/per-worktree environment configuration: the four-layer YAML overlay model, scope flattening, AES-256-GCM secrets with a worktree-aware key chain, the modern-vs-legacy compose injection paths, host/session/lifecycle injection surfaces, and materialization/drift detection — with lessons for auto-env."
title: "Hack Env Architecture — A Deep Dive"
---

# Hack Env Architecture — A Deep Dive

> Research note for the auto-env project. Hack (`hack-dance/hack`) is a
> TypeScript/Bun local-first dev runtime and the closest mature sibling to
> auto-env's roadmap. This document dissects **how Hack's environment
> architecture works** — the env/config resolution and injection subsystem
> specifically — grounded in the source at commit `7da1cc25`. All file:line
> references are to that tree (cloned under `.tmp/borrow-from-oss/hack-dance-hack`).
> We study patterns, not code; Hack is TS, auto-env is Go.

## Contents

1. [Thesis: env is a resolved model, not a file](#1-thesis)
2. [On-disk topology: source-of-truth vs generated vs local-only](#2-on-disk-topology)
3. [The file format and scope model](#3-file-format)
4. [The resolution pipeline](#4-resolution-pipeline)
5. [Secrets architecture: crypto + the worktree-aware key chain](#5-secrets)
6. [Injection architecture: four surfaces, two strategies](#6-injection)
7. [Where env sits in the compose-override stack](#7-override-stack)
8. [State, materialization, and drift detection](#8-state-drift)
9. [Legacy handling and migration](#9-legacy)
10. [Architectural assessment](#10-assessment)
11. [Lessons for auto-env](#11-lessons)

---

## 1. Thesis: env is a resolved model, not a file <a id="1-thesis"></a>

The defining architectural decision is stated plainly in `docs/env.md`:

> *"Direct runtime injection is the default path … That means `.hack/.env` is no
> longer the primary runtime source of truth."*

Hack does **not** treat a `.env` file as the environment. It treats the
environment as a **pure function** that is recomputed on every command:

```
env(selection) = decrypt( merge( layered YAML files ) )  →  scoped string maps
```

The result is materialized into different shapes for different consumers
(compose, host shell, sessions) **at the moment of use**, and the on-disk
`.env` is a downgraded, opt-in *compatibility export* (`hack env materialize`),
not an input. This is the inversion worth internalizing: most tools treat
`.env` as the canonical store and everything else as derived; Hack treats the
**layered YAML + a key** as canonical and `.env` as a lossy derived artifact.

Three properties fall out of this choice:

- **Worktree isolation is native.** Because env is recomputed per-invocation
  from files that include worktree-local overlays, two worktrees of the same
  repo resolve different environments without any per-worktree setup step.
- **Secrets never need to live decrypted on disk.** The canonical files hold
  ciphertext; plaintext exists only transiently in the resolved model (with one
  important caveat — see §6).
- **There is a single resolution function** (`resolveProjectEnvConfig`) that
  every surface routes through, so compose, host exec, and sessions cannot
  drift apart in how they interpret the same files.

---

## 2. On-disk topology: source-of-truth vs generated vs local-only <a id="2-on-disk-topology"></a>

Hack draws a hard line between three classes of file. This taxonomy *is* the
architecture — it's what makes "commit the shared parts, never commit secrets,
never hand-edit generated state" enforceable.

```
<repo>/
├── .hack.secret.key                       # LOCAL-ONLY  — encryption key (gitignored)
└── .hack/
    ├── hack.env.default.yaml              # SOURCE       — committed base overlay
    ├── hack.env.<overlay>.yaml            # SOURCE       — committed named overlay (e.g. qa)
    ├── hack.env.local.yaml               # LOCAL-ONLY   — worktree default overrides (gitignored)
    ├── hack.env.<overlay>.local.yaml     # LOCAL-ONLY   — worktree overlay overrides (gitignored)
    ├── hack.config.json                  # SOURCE       — project config (incl. env.defaultOverlay)
    ├── docker-compose.yml                # SOURCE       — service definitions
    ├── .env                              # GENERATED    — opt-in compatibility export only
    ├── .env.state.json                   # GENERATED    — materialization digests (drift tracking)
    └── .internal/
        ├── compose.env.override.yml      # GENERATED    — per-service env override (machine-local)
        ├── compose.override.yml          # GENERATED    — internal DNS/TLS override
        └── ...                           # GENERATED    — never hand-edit
```

Key resolution-relevant paths in code:

- `resolveProjectEnvConfigPath` / `resolveProjectEnvLocalConfigPath`
  (`src/lib/project-env-config.ts:216,229`) — compute the four overlay paths.
- `resolveProjectEnvKeyPath` (`:254`) — the checkout-local `.hack.secret.key`.
- `resolveProjectEnvSharedKeyPath` (`:260`) — the **git-common-dir** key path
  (`commonDir/.hack.secret.key`, `:274`), the linchpin of worktree key sharing.
- `resolveProjectEnvStatePath` (`:416`) — `.hack/.env.state.json`.

The `.local.yaml` files are auto-added to git exclude
(`ensureProjectEnvLocalIgnoreEntries`, `:281`), so a developer who runs
`hack env add --local` never accidentally commits a worktree-private value.

---

## 3. The file format and scope model <a id="3-file-format"></a>

Each env file is a small, **validated** YAML contract (`parseProjectEnvConfig`,
`:526`):

```yaml
version: 1                       # must == PROJECT_ENV_CONFIG_VERSION (rejected otherwise, :537)
environment: default
secretsprovider: project_key     # must == "project_key" (rejected otherwise, :546)
values:
  global:                        # applied everywhere
    API_BASE_URL: "https://api.example.com"
  api:                           # applied only to compose service "api"
    PORT: "4000"
    SERVICE_TOKEN:
      secure: v1:<iv>:<tag>:<ciphertext>   # encrypted value
  host:                          # applied only to host-command injection
    REDISHOST: "127.0.0.1"
```

The `values` map is partitioned into **three kinds of scope**, and this
partition is the second-most-important idea in the whole system:

| Scope        | Applies to                                              |
|--------------|---------------------------------------------------------|
| `global`     | every service container and host command                |
| `<service>`  | only the matching compose service (overrides `global`)  |
| `host`       | only host-side execution (`hack host exec/shell`)       |

A value is either a **plaintext scalar** or a **secret object** `{ secure: "v1:…" }`
(`parseProjectEnvStoredValue`, `:608`; `isProjectEnvSecretValue`, `:879`). The
schema is intentionally tiny — version, provider, and a two-level value map —
which is what keeps the merge and validation logic small and total.

---

## 4. The resolution pipeline <a id="4-resolution-pipeline"></a>

`resolveProjectEnvConfig` (`src/lib/project-env-config.ts:684`) is the heart of
the system. It runs the same five stages for every consumer:

```
                ┌─────────────────────────── selection ───────────────────────────┐
  envName? ──▶  effectiveEnv = requested ?? config.env.defaultOverlay              │
                (--env=base forces null → only the default layer)                  │
                └──────────────────────────────────────────────────────────────────┘
                                          │
   ┌──────────────────────────────────────┼─────────────────────────────────────────┐
   │ STAGE 1  load up to 4 layers          ▼                                          │
   │   L1 hack.env.default.yaml      (committed base)                                 │
   │   L2 hack.env.<overlay>.yaml    (committed overlay)        if effectiveEnv != null│
   │   L3 hack.env.local.yaml        (worktree default override)                      │
   │   L4 hack.env.<overlay>.local.yaml (worktree overlay override) if effectiveEnv   │
   ├──────────────────────────────────────┼─────────────────────────────────────────┤
   │ STAGE 2  merge layers L1→L4, last-writer-wins per key, per scope                 │
   │          (mergeProjectEnvConfigLayers, :826 — Object.assign per scope)           │
   ├──────────────────────────────────────┼─────────────────────────────────────────┤
   │ STAGE 3  resolve the secret key — only if the merged config has any secret       │
   │          (hasSecretEntries gate, :762) → resolveProjectEnvKey                     │
   ├──────────────────────────────────────┼─────────────────────────────────────────┤
   │ STAGE 4  decrypt every scope's values (resolveProjectEnvScopeValues, :852)        │
   │          → globalEnv, hostEnv, and a raw per-scope value set                      │
   ├──────────────────────────────────────┼─────────────────────────────────────────┤
   │ STAGE 5  flatten per service:  serviceEnv[s] = { ...globalEnv, ...serviceScope }  │
   │          (:792-801 — service values override globals)                            │
   └──────────────────────────────────────────────────────────────────────────────────┘
                                          │
                                          ▼
        ProjectEnvResolvedConfig { globalEnv, hostEnv, serviceEnv, files,
                                   declaredScopes, unknownScopes }
```

Properties worth calling out, each verifiable in the source:

- **Precedence is two-dimensional.** Across *files*, later layers win
  (`default < overlay < local < overlay.local`, `:751-757`). Within a *scope*,
  service beats global (`{ ...globalEnv, ...scopedValues }`, `:798-800`). The
  two axes are orthogonal and both are last-writer-wins, which makes the model
  easy to reason about: "more specific file, more specific scope."

- **Worktree overrides win by construction.** L3/L4 are the gitignored
  `.local.yaml` files and they sit *last* in the layer array, so a worktree can
  shadow any shared value without editing tracked files. This is how Hack gets
  per-worktree env divergence for free.

- **The key is lazy.** `resolveProjectEnvKey` is called with
  `required: hasSecretEntries(merged)` (`:762`). A repo with no secrets never
  needs a key at all — resolution is fully functional with `keyText: null`, and
  `decryptProjectEnvStoredValue` only demands a key when it actually meets a
  `{ secure: … }` value (`:892`). Cost is paid only when secrets exist.

- **Unknown scopes are surfaced, not dropped.** Any scope that is neither
  `global`, `host`, nor a known compose service is collected into
  `unknownScopes` (`:782-785`) and warned about at `up` time (`:919-923`) —
  catching typo'd service names instead of silently ignoring them. But it still
  resolves the known scopes (graceful, not fail-closed).

- **`host` vs a service named `host`.** There's an explicit guard: if a compose
  service is literally named `host`, the reserved host scope is disabled to
  avoid collision (`hostScopeConflictsWithService`, `:773-781`). A small detail
  that signals how carefully the scope namespace is policed.

- **One pure function, total over missing files.** Every layer read tolerates a
  missing file (returns `exists: false`); only a *parse* error throws
  (`:730-749`). If all four layers are absent, the whole thing returns `null`
  (`:719-728`) and callers fall back to legacy or empty — env is optional.

---

## 5. Secrets architecture: crypto + the worktree-aware key chain <a id="5-secrets"></a>

### 5.1 The cipher

Secrets are sealed with **AES-256-GCM** (`PROJECT_ENV_ALGORITHM`, `:45`) using a
12-byte random IV (`:46`). The on-disk format is a colon-joined string
(`encryptProjectEnvValue`, `:1509`):

```
v1:<base64(iv)>:<base64(authTag)>:<base64(ciphertext)>
```

- `v1` is a version prefix (`PROJECT_ENV_SECRET_PREFIX`, `:44`) — forward-compat.
- The 256-bit key is derived as `sha256(keyText)` (`deriveProjectEnvKey`,
  `:1556`), so the stored key material can be any string (a base64url of 32
  random bytes by default, `:1468`) and is stretched to exactly 32 bytes.
- GCM gives authenticated encryption: `decryptProjectEnvValue` (`:1529`) sets
  the auth tag and `final()` throws on tamper — a corrupted/edited secret fails
  loudly rather than yielding garbage.

This is a deliberately boring, stdlib-only construction (Node `crypto`). No KMS,
no external secret store in the supported path — the entire secret system is
"a symmetric key in a gitignored file + AEAD."

### 5.2 The key resolution chain

The interesting architecture is *where the key comes from*. `resolveProjectEnvKey`
(`:1373`) tries four sources in strict order:

```
1. checkout-local   .hack.secret.key            (resolveProjectEnvKeyPath)
2. shared key       <git-common-dir>/.hack.secret.key   (resolveProjectEnvSharedKeyPath, :260/:274)
3. inherited key    <primary-worktree>/.hack.secret.key  (resolveInheritedProjectEnvKey, :1487)
4. env var          $HACK_ENV_SECRET_KEY                  (:1384)
   └─ else, if required → throw with a remediation message listing the paths tried (:1402)
```

The middle two rungs are the clever part and exist **specifically to serve git
worktrees**:

- **Shared key (rung 2)** resolves to the key under the *git common dir*
  (`resolveProjectEnvSharedKeyPath` calls into the worktree resolver and returns
  `commonDir/.hack.secret.key`). All linked worktrees of one repo share a single
  common dir, so they all read the *same* key — committed encrypted secrets
  decrypt identically across every worktree with zero copying.
- **Inherited key (rung 3)** handles the case where the shared location is empty
  but the *primary* checkout has a key: `resolveInheritedProjectEnvKey`
  (`:1487`) resolves the primary worktree root and reads its key. A linked
  worktree thus "inherits" the main checkout's key.

Writing follows the same hierarchy (`ensureProjectEnvSecretKey`, `:1413`): it
prefers the shared path when one exists, generates 32 random bytes if nothing is
found, `chmod 0600`s the file, and auto-adds it to `.gitignore` only when it's a
checkout-local key (`:1454,:1473`). The env-var fallback (rung 4) is what makes
the model portable to CI and managed containers (`hackdance/hack:slim`) without
ever baking the key file into an image.

> **Why this matters:** the naïve approach — "copy the gitignored
> `.hack.secret.key` into each worktree" — defeats the point of worktrees
> (untracked files don't come along). Hack instead *locates the key via git's
> own shared-state directory*, so one key transparently serves N worktrees. This
> is the single most worktree-native idea in the env system.

---

## 6. Injection architecture: four surfaces, two strategies <a id="6-injection"></a>

The resolved model feeds **four consumers**, and Hack uses **two different
injection strategies** depending on whether a value is global or service-scoped.

### 6.1 The four surfaces

| Surface              | Entry point                                   | Gets                          |
|----------------------|-----------------------------------------------|-------------------------------|
| Compose services     | `resolveComposeEnvOverrides` (`project.ts:992`) | global (process env) + per-service (override file) |
| Lifecycle/host procs | `resolveLifecycleEnvForProject` (`:956`)      | `globalEnv`                   |
| `hack host exec/shell` | `selectProjectEnvValuesForExecutionTarget` (`project-env-config.ts:121`) | global + service + **host** scope, host-rewritten |
| Sessions             | env-aware `hack session start/exec --env --service` | scoped workspace env          |

### 6.2 Two injection strategies for compose

This is the subtlest part of the architecture. When provisioning, Hack splits
the resolved env into two delivery channels:

1. **Globals → process environment.** `globalEnv` is returned as `env` and
   passed directly to `composeRuntimeBackend.up({ env })` (`handleUp`,
   `project.ts:4830`). Docker Compose then sees those as ambient variables.

2. **Service-scoped → a generated override file.**
   `resolveModernComposeEnvOverrides` (`:899`) builds, for each target service,
   `services.<svc>.environment = { ...resolved values... }` and writes it to
   `.hack/.internal/compose.env.override.yml` via `writeTextFileIfChanged`
   (idempotent — only rewrites on change, `:948`). That file is appended to the
   `-f` compose stack (§7).

```yaml
# .hack/.internal/compose.env.override.yml  (GENERATED, machine-local, gitignored)
services:
  api:
    environment:
      API_BASE_URL: https://api.example.com   # global, flattened in
      PORT: "4000"                              # service scope
      SERVICE_TOKEN: <decrypted plaintext>      # ← secret, DECRYPTED inline
```

> **Critical security nuance.** In the *modern* path the override file embeds the
> **decrypted, literal** resolved values — including secrets — because
> `serviceEnv[s]` is the already-decrypted map (`:927,:931`). So decrypted
> secrets *do* land on disk, but only under `.hack/.internal/` (gitignored,
> machine-local, never committed). The canonical store stays encrypted; the
> *generated runtime artifact* is plaintext. That's an explicit trade: Compose's
> own env interpolation can't decrypt, so the values must be concrete by the time
> Compose reads the file.

By contrast, the **legacy** path (`resolveComposeEnvOverrides` tail,
`:1040-1075`) writes `${KEY}` *interpolation placeholders* into the override and
relies on the process `env` to fill them (`buildComposeEnvInterpolation`,
`:825`) — keeping values out of the file. The modern path traded that indirection
for simplicity and per-service precision, accepting plaintext-in-`.internal`.

### 6.3 Host execution: the same model, container-isms rewritten

Host commands route through `selectProjectEnvValuesForExecutionTarget` (`:121`),
which differs from the compose view in two ways (`docs/env.md`):

- it layers the **`host` scope last**, so values like `REDISHOST: 127.0.0.1` can
  override the container-oriented `REDISHOST: redis`; and
- it **rewrites container-only hostnames** (`host.docker.internal`, compose
  service names) to `127.0.0.1` so a host-side `bun db:migrate` actually reaches
  the service.

`--target compose` opts back into the container-oriented view. This is a nice
piece of architecture: **one resolved model, two projections** (host vs
container), with the difference expressed declaratively (the `host` scope) plus
a small rewrite pass — rather than two parallel env systems.

---

## 7. Where env sits in the compose-override stack <a id="7-override-stack"></a>

Env injection is one layer in a broader **layered-compose** provisioning model.
`handleUp` (`project.ts:4770-4789`) assembles an ordered `-f` list and hands it
to `docker compose up`:

```
docker compose \
  -f .hack/docker-compose.yml \                         # (1) source of truth: services
  -f .hack/.branch/compose.<branch>.override.yml \      # (2) branch: rewrite Caddy labels → branch hosts
  -f .hack/.internal/compose.override.yml \             # (3) internal: DNS→CoreDNS, mount CA, SSL env, extra_hosts
  -f .hack/.internal/compose.env.override.yml \         # (4) env: per-service environment (this doc)
  -p <project>--<branch>  up   (with globalEnv in the process environment)
```

Each override is a pure function of resolved state and is regenerated
idempotently on every `up`. The env override is deliberately **last** among the
generated files, so per-service environment wins over anything an earlier
override set. This compositional design — *never mutate the user's compose file;
layer generated overrides on top* — is what lets env, networking, and branch
routing evolve independently. (See the companion provisioning notes; this doc
focuses on layer 4.)

---

## 8. State, materialization, and drift detection <a id="8-state-drift"></a>

Although runtime injection is canonical, Hack still supports exporting a
compatibility `.hack/.env` (`materializeProjectEnv`, `:1057`) for external tools.
Crucially, it does **not** export blindly — it tracks provenance:

- Alongside the materialized output it writes `.hack/.env.state.json` with a
  **digest of every input file** (`buildProjectEnvStateDigests`, `:1111`) plus
  the selected overlay/service.
- `inspectProjectEnvMaterialization` (`:1122`) /
  `collectProjectEnvMaterializationIssues` (`:1304`) recompute the digests and
  report exactly which inputs changed (or which scope vanished) since the last
  materialize, each with the remediation command.
- `hack doctor` consumes this to warn when `.hack/.env` is stale and points at
  `hack env materialize` (`docs/env.md`).

This is a clean answer to the perennial "my `.env` is out of date" problem: the
derived artifact carries a content hash of its inputs, so staleness is
*detectable* rather than a silent footgun. (This is the pattern captured as
idea **I002** in the borrow backlog.)

---

## 9. Legacy handling and migration <a id="9-legacy"></a>

The architecture is versioned and self-migrating, which is worth noting as a
maturity signal:

- The resolver first tries the modern YAML path; if no YAML layers exist it falls
  back to the legacy `hack.env.json` / `.env.<overlay>` contract
  (`resolveComposeEnvOverrides` → `resolveHackEnv`, `:1011`).
- `hack up` detects a legacy contract with no modern config and offers to migrate
  inline (`maybePromptLegacyProjectEnvMigration`, `:843`), or prints the
  `hack doctor --migrate-env-config` hint in non-interactive shells.
- `migrateLegacyProjectEnv` (`:1824`) and the `normalizeLegacy*` family
  (`:2159-2360`) translate old top-level config, overlays, control-plane, and
  secret-backend settings into the new model, including cleaning up encrypted
  backend artifacts.

The lesson: the canonical format is a **validated, versioned contract** (`version`
+ `secretsprovider` are both hard-checked, `:537,:546`), which is what makes a
forward migration tractable instead of a guessing game.

---

## 10. Architectural assessment <a id="10-assessment"></a>

**What's genuinely strong:**

- **Single resolution function, many projections.** Every surface (compose,
  host, lifecycle, session) calls `resolveProjectEnvConfig`. There is no second
  code path that can interpret the files differently. The host-vs-container
  difference is one declarative scope + one rewrite pass, not a fork.
- **Two-axis last-writer-wins precedence** (file layer × scope) is simple to hold
  in your head and covers shared/overlay/worktree and global/service/host with
  the same primitive (`Object.assign`).
- **The key chain is the standout.** Locating the secret key via the git common
  dir / primary worktree is the right primitive for worktree-based development
  and is the part most worth stealing.
- **Lazy key + total-over-missing** keep the common (no-secrets) case zero-cost
  and make env entirely optional.
- **Versioned, validated, self-migrating** contract.

**Sharp edges / trade-offs:**

- **Decrypted secrets hit disk** in the modern compose path (`.internal/`,
  gitignored). Acceptable, but it *is* plaintext-at-rest in a temp artifact; a
  reader must trust `.internal/` is never committed or shared. The legacy
  `${KEY}`-interpolation approach avoided this.
- **Symmetric-only, no rotation story** surfaced in the code. Rotating a key
  means re-encrypting every secret; there's no envelope/KMS rung.
- **Scope ↔ service coupling** means a renamed compose service silently strips
  that scope's overrides (mitigated only by the `unknownScopes` warning).
- **Complexity.** `project-env-config.ts` is ~2400 lines with a flagged
  `noExcessiveCognitiveComplexity` on the core resolver (`:683`). The richness
  has a maintenance cost.

---

## 11. Lessons for auto-env <a id="11-lessons"></a>

auto-env today resolves environments by **substituting `${PORT_*}`/`${BRANCH}`
tokens into a single Process Compose template**. Hack's env system is a different
and more elaborate beast; the contrast is instructive.

| Dimension            | auto-env (today)                          | Hack                                                |
|----------------------|-------------------------------------------|-----------------------------------------------------|
| Canonical input      | one `process-compose.template.yaml`       | four-layer YAML overlay set + a key                 |
| What varies per WT   | allocated `${PORT_*}` + `${BRANCH}` tokens| any value, via gitignored `*.local.yaml` overlays   |
| Secrets              | none                                      | AES-256-GCM, worktree-shared key chain              |
| Scoping              | flat substitution                         | global / per-service / host scopes                  |
| Injection            | render template → process-compose         | process env (globals) + generated per-service override |
| Derived `.env`       | n/a                                       | opt-in, with input-digest drift detection           |
| Resolution shape     | string replace                            | one pure function, many projections                 |

**Worth borrowing (and already in the backlog):**

- **The worktree-shared key chain** (idea **I017**) — *if* auto-env ever grows a
  secrets feature, locating the key via `git rev-parse --git-common-dir` rather
  than copying a gitignored file is the correct worktree primitive. This is the
  highest-leverage idea in Hack's env system.
- **Input-digest drift detection** (idea **I002**) — auto-env already writes a
  manifest of generated files; adding a sha256 of the *source template + resolved
  inputs* to each registry entry gives `auto env status` a "your generated config
  is stale, re-run `up`" signal for free. Directly serves auto-env's stated
  config-hash-drift backlog item.
- **The layered-overlay model** (idea **I022**) — a committed base template plus a
  gitignored per-worktree overlay that *wins*, feeding `internal/template` before
  `${PORT_*}` substitution, would let an agent tweak one worktree's env without
  dirtying tracked files. This is a structural change (an afternoon-plus), only
  worth it if multi-file env config is actually wanted.

**Worth resisting:**

- **Don't adopt the full scope/secret/materialize stack wholesale.** Most of
  Hack's complexity serves Docker-host duality (the `host` scope, 127.0.0.1
  rewrites, compose interpolation) and a hosted-secrets history — both largely
  non-goals for auto-env, whose runtime is Process Compose (host processes that
  see the worktree directly) rather than containers needing injected hostnames.
- **Keep the "env is optional, one file is the only hard input" stance.** Hack
  earns its richness because it's a general dev runtime; auto-env's value is a
  *thin* deterministic port allocator. Borrow the **patterns that are worktree-
  native** (key chain, drift digests, local-overlay-wins) and leave the
  container-env machinery behind.

**One concrete north-star:** Hack proves you can make per-worktree environment
divergence *fall out of the file model* (gitignored `.local` overlays + a
git-common-dir key) instead of being a special code path. auto-env's deterministic
slot already gives it per-worktree *ports* for free; the same "derive from
worktree identity, don't special-case it" philosophy is the thread connecting
the two systems.

---

*Source: `hack-dance/hack` @ `7da1cc25`. Primary files studied:
`src/lib/project-env-config.ts`, `src/commands/project.ts` (`handleUp`,
`resolve*ComposeEnvOverrides`), `docs/env.md`, `docs/architecture.md`. Companion
backlog: `auto-env/docs/research/opensource-ideas.yaml` (ideas I002, I017, I022).*
