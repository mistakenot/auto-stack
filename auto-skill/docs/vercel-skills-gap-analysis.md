---
hash: ""
id: "4ac5dd90"
read_when: "scoping auto-skill toward remote skill install/update/distribution, or deciding what to build to replace the npx skills shell-out"
summary: "What the vercel skills CLI does that auto-skill doesn't, and the concrete pieces needed to make auto-skill a native tool for installing, updating, and managing skills from remote repos."
title: "auto-skill vs. vercel-labs/skills — Feature Gap & Path to Parity"
---

# auto-skill vs. vercel-labs/skills — Feature Gap & Path to Parity

> Part of a set of three:
> - [`docs/research/opensource-ideas.yaml`](research/opensource-ideas.yaml) —
>   ranked borrowable mechanisms (`I001`–`I013`).
> - **this doc** — the higher-level map: *which capabilities are missing* and
>   *what it takes* to turn auto-skill into a tool that installs/updates/manages
>   skills from remote repos, the way `npx skills` does.
> - [`docs/remote-skills-design.md`](../../docs/remote-skills-design.md) —
>   the concrete end-to-end design (git cache, deterministic templating,
>   `skills.yaml`/`lock.json`, CLI surface, `migrate vercel`). *(repo-root `docs/`)*

## TL;DR

auto-skill today is an **authoring and validation** tool: it scaffolds, lints,
and lists `SKILL.md` files in a local `./skills/` directory, and tracks drift
between a skill and its source docs. It is excellent at the things `vercel-labs/skills`
does *not* do.

What it is **not**, today, is a **distribution** tool. The one place auto-skill
touches remote/multi-agent distribution — `auto skill sync` — does so by shelling
out to the very tool we'd be replacing:

```go
// internal/cli/sync.go
exec.CommandContext(ctx, "npx", "skills", "add", "./skills",
    "--agent", "codex", "claude-code", "--full-depth", "-y")
```

And `auto skill update` updates the **auto-stack binary**, not skills.

So "build our own version of `skills`" reduces to a clear statement: **replace the
`npx skills` shell-out with native Go that can parse a source, fetch it without a
clone, export it into many agents' discovery paths, record a lockfile, and check
freshness.** This doc enumerates those pieces.

## Where each tool sits

| | auto-skill | vercel-labs/skills |
|---|---|---|
| **Primary job** | Author + validate skills in your repo | Install + manage skills across agents |
| **Direction of data** | You write skills → lint them | Remote repo → your machine |
| **Strengths** | `lint` schema, doc-drift detection, scaffolding, JSON-first | Remote fetch, 70+ agent targets, install/update/lockfiles |
| **Stack** | Go / Cobra, pure-Go, local-first | TypeScript / Node, npx-distributed |
| **Remote sources** | None (shells out to `npx skills`) | GitHub / GitLab / git / local / well-known |

These are complementary halves. The goal isn't to discard auto-skill's authoring
strengths — it's to add the distribution half natively.

## Capability parity matrix

Legend: ✅ native · 🟡 partial / shells out · ❌ missing · ➖ intentionally out of scope

| Capability | auto-skill | vercel skills | Gap → borrow ref |
|---|---|---|---|
| Scaffold a new skill (`create`/`init`) | ✅ | ✅ | — |
| Lint against a best-practices schema | ✅ (rich) | ❌ | *auto-skill's edge* |
| Doc-drift / freshness (autodoc hashes) | ✅ | ❌ | *auto-skill's edge* |
| List local skills (`ls`) | ✅ | ✅ | — |
| Parse remote sources (owner/repo, URLs, git, local) | ❌ | ✅ | `I009` |
| Fetch a skill without a full clone (Trees API) | ❌ | ✅ | `I009` |
| Pluggable host providers (`.well-known`, GitLab…) | ❌ | ✅ | `I011` |
| Install into a specific agent's dir | 🟡 (`npx`) | ✅ | `I001` |
| Multi-agent export (70+ agents, path registry) | ❌ | ✅ | `I001`, `I002` |
| Detect which agents are installed | ❌ | ✅ | `I001` (detectInstalled) |
| Canonical store + symlink-per-agent | ❌ | ✅ | `I010` |
| Lockfile (global + checked-in project) | ❌ | ✅ | `I010` |
| Update check w/o downloading (tree-SHA) | ❌ | ✅ | `I010` |
| `remove` installed skills | ❌ | ✅ | (follows from `I010`) |
| `find` / search a registry | ❌ | ✅ | `I012` (find half) |
| `use` — run a skill as a prompt, no install | ❌ | ✅ | `I012` |
| Catalog-layout (`<category>/<name>`) discovery | ❌ | ✅ | `I006` |
| Plugin-manifest discovery (`.claude-plugin`) | ❌ | ✅ | `I007` |
| `metadata.internal` hidden skills | ❌ | ✅ | `I005` |
| Terminal-escape sanitization of metadata | ❌ | ✅ | `I003` |
| Path-traversal-safe names / extraction | 🟡 (name regex only) | ✅ | `I004` |
| Usage telemetry | ➖ | ✅ (opt-out) | `I013` (rejected — local-first) |

## The missing pieces, grouped

Five capability areas separate "lint my skills" from "manage skills from remote
repos." Roughly in dependency order:

### 1. Source resolution — *understand where a skill comes from*
**Missing:** a parser that turns `owner/repo`, full GitHub/GitLab URLs,
`/tree/<branch>/<path>` deep links, bare git URLs, `owner/repo#ref@skill`
fragments, and local paths into one normalized descriptor.
**What it takes:** a `source` package (`ParsedSource{Type, URL, Ref, Subpath,
SkillFilter}`) with a matcher table. Pure string work, well-tested in their
`source-parser.test.ts`. *(Borrow `I009`.)*

### 2. Remote fetch — *get the files without cloning the world*
**Missing:** the ability to read a skill's files from a remote repo.
**What it takes:** a GitHub **Trees API** fast path
(`/git/trees/<branch>?recursive=1`) with branch fallback (`ref→main→master→HEAD`)
and lazy auth (anonymous first, token from `GITHUB_TOKEN`/`GH_TOKEN`/`gh auth
token` only after a 403), plus a **shallow-clone fallback** for non-GitHub git.
This is also where the **provider abstraction** (`I011`) eventually plugs in so
GitLab / `.well-known` self-hosting aren't special-cased. Net new HTTP + a
dependency on the GitHub API (the only real external dependency introduced).
*(Borrow `I009`; defer `I011`.)*

### 3. The agent registry — *know where every agent keeps its skills*
**Missing:** the single most load-bearing piece. A mapping of ~70 agents to their
project (`.claude/skills/`) and global (`~/.claude/skills/`) discovery paths, with
aliases that group agents sharing `.agents/skills/`, per-agent quirks
(project-only agents), and a `detectInstalled()` probe.
**What it takes:** a Go `AgentConfig` registry. Keep it the **single source of
truth** and codegen the docs/tables from it (auto-skill already uses the
marker-codegen pattern for its autodoc index), validated in CI so it can't drift.
*(Borrow `I001` + `I002`.)*

### 4. Install + lifecycle — *write, track, update, remove*
**Missing:** native install, a lockfile, update detection, and removal. Today
`sync` delegates all of this to `npx`.
**What it takes:**
- **Install:** a canonical copy per skill + relative symlinks into each selected
  agent dir (copy-mode fallback when symlinks fail).
- **Lockfiles:** a global lock (versioned, auto-wiped on format bump, tracks a
  GitHub **tree SHA** per skill folder) and a **checked-in project lock**
  (content-hash, timestampless + alphabetized to stay merge-friendly).
- **Update:** compare stored tree SHA vs. live tree SHA → reinstall only what
  changed, with **zero content download** for unchanged skills. This is the same
  hash-freshness idea auto-skill already applies to autodoc links, lifted to whole
  skill folders.
- **Remove:** falls out of the lockfile + registry.
*(Borrow `I010`.)*

### 5. Discovery + UX polish — *find, use, and stay safe*
**Missing:** richer discovery and the convenience commands.
**What it takes:**
- **Discovery:** depth-2 catalog walk (`skills/<category>/<name>/SKILL.md`) with a
  shadowing rule + dedupe (`I006`); optional plugin-manifest discovery (`I007`).
- **`use`:** materialize a skill and emit a pipeable `<SKILL.md>…</SKILL.md>`
  prompt — works on **local** skills with no remote fetch, so it's a cheap early
  win (`I012`).
- **`find`:** keyword/interactive search against a registry API (`I012`).
- **Safety hardening (do first, independent of everything else):** strip terminal
  escapes from untrusted metadata before printing (`I003`) and make name/path
  handling traversal-safe before any write (`I004`). These matter the moment you
  ingest skills you didn't author.

## What auto-skill should NOT lose

The point of going native is to *add* distribution without surrendering the
authoring edge that `vercel-labs/skills` lacks entirely:

- The **`lint`** schema (frontmatter rules, trigger phrases, token budgets,
  secret detection, weak-opening, link resolution).
- **Doc-drift detection** via autodoc hash-freshness — the conceptual cousin of
  the tree-SHA freshness check, and unique to this stack.
- **JSON-first, local-first** behavior. In particular, **telemetry stays out**
  (`I013` rejected): unlike `npx skills`, nothing phones home.

A distribution-capable auto-skill would be the only tool that both **validates**
skills to a real schema *and* **installs/updates** them across agents.

## Suggested sequencing

A pragmatic order that front-loads safety and standalone wins, then builds the
remote stack bottom-up:

1. **Harden now (independent, S):** `I003` terminal-escape sanitization,
   `I004` traversal-safe names, `I008` frontmatter type checks, `I005`
   `metadata.internal`. None of these need remote fetch; all improve the tool today.
2. **`use` + richer discovery (M):** `I012` (`use` on local skills), `I006`
   catalog-layout discovery. Still no network.
3. **The registry (L):** `I001` agent registry + `auto skill export`/`sync`
   (native), `I002` codegen-from-registry. **This is the moment `sync` stops
   shelling out to `npx skills`.**
4. **Remote fetch + lifecycle (L):** `I009` source-parser + Trees-API fetch,
   then `I010` install/lockfile/update. After this, `auto skill add <source>`
   and `auto skill update` (for skills) exist natively.
5. **Later / optional:** `I011` provider abstraction (GitLab, `.well-known`),
   `I007` plugin-manifest discovery, a `find` registry backend.

After steps 3–4, the `npx skills` dependency in `sync.go` can be deleted and
auto-skill is a self-contained skill manager.
