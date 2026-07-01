---
hash: "d6889056"
id: "059e58d1"
read_when: "triaging or fixing auto-skill robustness/security findings, or planning further fuzz-test coverage"
summary: "Findings from an 8-agent, 353-probe fuzz campaign against the auto-skill CLI over the Docker Compose harness, ranked by severity with prioritized fixes."
title: "Auto-Skill Harness Fuzz Test Report"
---

# Auto-Skill Harness Fuzz Test Report

**Date:** 2026-07-01
**Harness version:** Docker Compose (git-server + SUT), built from source
**Agents deployed:** 8 parallel fuzz agents across 8 dimensions
**Total probes run:** 353
**Unique findings:** 55 (deduplicated from ~90 raw findings across agents)

## Executive Summary

The auto-skill CLI has strong security fundamentals -- command injection, symlink attacks, YAML deserialization bombs, and credential embedding are all properly defended. However, the fuzz campaign exposed a systemic **manifest/target ownership tracking brittleness**: at least six independent triggers (partial sync failure, lock deletion, trust cycling, cache loss, commit corruption, concurrent add) all converge on the same failure mode where rendered targets become classified as "foreign" and block subsequent syncs. A secondary theme is **silent success on partial failure** -- `add` exits 0 even when its internal post-add sync fails, causing cascading state corruption. The harness infrastructure itself has resource isolation gaps (zombie accumulation, no PID limits, mutable fixtures) that should be fixed before scaling the test campaign further.

## Methodology

| Dimension | Probes | Unique Findings |
|---|---|---|
| Input validation and boundary conditions | 78 | 13 |
| State management and data corruption | 33 | 17 |
| Git server transport edge cases | 52 | 27 |
| Cross-command workflow interactions | 32 | 11 |
| Output format consistency | 78 | 11 |
| Docker Compose harness infrastructure | 28 | 16 |
| Full skill lifecycle stress testing | 28 | 15 |
| Security boundaries and injection vectors | 24 | 11 |

All probes ran against the Docker Compose harness with real HTTPS transport, self-signed CA, blobless cache clones, and `git archive` extraction -- the full production code path.

## Findings by Severity

### Critical (0 findings)

No panics, crashes with stack traces, or unrecoverable data corruption were found.

### High (6 findings)

**H1. `add` exits 0 when post-add sync partially fails, causing cascading foreign-target corruption**

When `add` adds skills and the internal sync fails for any one skill (e.g. broken template), the command exits 0. The successfully-added skills get rendered to targets but the manifest is written incomplete. On subsequent `sync --locked`, ALL previously-rendered targets are classified as "foreign" and blocked with conflict errors, requiring manual `adopt` for each skill x target combination. Tested with both single-skill and batch (`--skill '*'`, 18 skills) adds.

```
$ auto skill add 'https://git-server/repos/skills.git' --skill '*' --format json
# exit code: 0, stdout: {"added":[...18 skills...]}
# stderr: sync error: render customizable: template_rejected: parse error: function "project_name" not defined

$ auto skill sync --locked  # subsequent sync
# exit code: 1
# error: conflict: desired skill "bulk-skill-1" collides with a foreign (un-managed) dir in target "agents"
# ... repeated for all 17 successfully-rendered skills x 2 targets = 34 conflict errors
```

Root cause: `add` treats the sync as best-effort (errors go to stderr, exit code stays 0) but the sync's manifest write is all-or-nothing -- a single render failure causes the manifest to omit all skills, orphaning those already written to disk.

**H2. Concurrent `add` operations cause silent data loss via TOCTOU race**

Two concurrent `add` commands in the same workspace both report success (exit 0), but only the last writer's skill persists in lock.json. No file locking is implemented.

```
$ auto skill add '...' --skill deploy-checklist & auto skill add '...' --skill code-review & wait
# Both report {"added":[...]} but lock.json contains only one skill
```

**H3. Path traversal via `targets` in skills.yaml writes files outside project root**

The targets list in skills.yaml accepts relative paths with `..` components. A crafted target like `../../../tmp/escape` causes sync to write rendered skill files to arbitrary directories.

```
# skills.yaml: targets: ["../../../tmp/escape"]
$ auto skill sync --text
Wrote 4 target file(s):
  + ../../../tmp/escape/code-review
  + ../../../tmp/escape/deploy-checklist
```

An attacker who can modify skills.yaml (e.g. via a malicious skill repo or PR) can write arbitrary SKILL.md content to any writable path.

**H4. Mixed-case skill names in lock.json create duplicate rendered targets**

Entries differing only in case (e.g. `deploy-checklist` and `Deploy-Checklist`) are both rendered as separate directories. On case-insensitive filesystems (macOS HFS+/APFS), one silently overwrites the other. No case-normalization or duplicate detection occurs.

**H5. Deleting lock.json then running `sync --locked` prunes all rendered targets**

When lock.json is deleted (merge conflict, accidental deletion), `sync --locked` prunes all rendered skill targets in both target directories while skills.yaml retains its entries. The user gets no warning that content was deleted.

```
$ rm .auto/skills/lock.json && auto skill sync --locked
# exit code: 0
# "pruned":["agents/deploy-checklist","claude/deploy-checklist"]
```

**H6. Zombie process accumulation in SUT container (harness)**

The SUT container uses `sleep infinity` as PID 1 with no init process. Every `auto skill add` spawns git subprocesses that become zombies. After moderate testing, 73+ zombie `[git]` processes were observed. Fix: add `init: true` to docker-compose.yaml.

### Medium (22 findings)

**Manifest/target ownership brittleness** (common root cause for M1-M4):

- **M1. Stale commit hash in lock.json causes own targets to be treated as foreign.** Non-locked sync resolves the new commit but classifies existing rendered dirs as "foreign" rather than "own stale targets." Requires manual `adopt` or `--force`.
- **M2. Trust removal + re-trust causes foreign collision on first sync.** During the untrusted period, the manifest drops the skill, making existing targets "foreign."
- **M3. Server outage + cache deletion makes previously synced skill unrecoverable without a second sync.** First sync after server restore fails with "foreign" collision; second sync recovers.
- **M4. `sync --locked` with one deleted target dir partially restores targets, reports conflict on the surviving one.** Leaves an inconsistent state where one target is managed and the other is foreign for the same skill.

**Input validation gaps:**

- **M5. `trust add` accepts `file://` and `ssh://` schemes.** Only `http://` is rejected. `file:///etc/passwd` is stored and could enable trusting arbitrary local paths as skill sources.
- **M6. `trust add` accepts wildcard `*`, empty host `https://`, and malformed URLs.** These junk entries pollute the trust store.
- **M7. `describe` command skips name format validation.** Unlike `get` and `remove` (which enforce `^[a-z0-9]+(-[a-z0-9]+)*$`), `describe` accepts any string and returns "unknown skill" instead of "invalid skill name."
- **M8. Empty string skill name in lock.json causes crash during sync.** No validation rejects empty names at parse time; the crash occurs in journaled commit with `rename ... invalid argument`.
- **M9. No version field validation in lock.json.** version:-1, version:0, and version:99999999 are all accepted silently. No forward-compatibility guard.
- **M10. Binary content in SKILL.md silently accepted and rendered.** 256 bytes of `/dev/urandom` passes add, sync, and lint (empty array) without any warning.

**State management:**

- **M11. Empty lock.json `{}` causes sync to silently orphan rendered targets.** Sync does not re-resolve skills from skills.yaml. Previously rendered targets become untracked.
- **M12. `add` without `init` creates skills.yaml with `targets:[]` but still renders to both target directories.** The targets configuration is ignored during add.
- **M13. `add` to empty lock `{}` writes version:0 instead of version:1.** Go zero-value default; version field should be explicitly initialized.
- **M14. `update` with nonexistent skill name silently succeeds (exit 0).** Reports `desired_complete: true` with no error, misleading callers.
- **M15. Strict JSON/YAML parsing rejects unknown fields, breaking forward compatibility.** A lock.json with an extra field fails: `json: unknown field "extra_field"`.
- **M16. Doctor silently skips `ownership_scan` when skills.yaml is corrupt.** The check is omitted from output rather than reported as failed.

**Harness issues:**

- **M17. Multiple concurrent agents clobber each other's Docker Compose stacks.** No project-name isolation; competing compose processes recycle container IDs.
- **M18. SUT can push to git-server, mutating shared fixture state.** `http.receivepack` enabled, no auth.
- **M19. `git init.defaultBranch` not set in SUT.** Creates `master` while git-server uses `main`.
- **M20. No resource limits on either container.** No PID, memory, or CPU constraints.
- **M21. `up()` method does not clean up on failure.** Failed attempts leave orphaned containers, blocking subsequent attempts until manual `docker compose down -v --remove-orphans`.

**Output consistency:**

- **M22. Inconsistent format flags: 3 different conventions across subcommands.** (A) `--format json|text`, (B) `--text` boolean flag, (C) both `--json` and `--text`. Scripts cannot use a uniform flag pattern.

### Low (19 findings)

- **L1. `trust add`/`trust remove` output goes to stderr, stdout is empty.** Breaks pipelines expecting stdout.
- **L2. `trust remove` for non-trusted endpoint returns exit 0.** Misleading idempotency.
- **L3. URL query strings and fragments passed unsanitized to git clone and cache path.** Creates junk cache directories with `?` and `#` in names.
- **L4. Unknown subcommand exits 0 instead of non-zero.** `auto skill frobnicate` prints help and exits 0.
- **L5. `cache path` returns hypothetical path for nonexistent repo (exit 0).** No existence check.
- **L6. `remove --vendored` does not clean up rendered target directories.** Reports them but leaves on disk; doctor classifies as "foreign."
- **L7. Sync with missing lock.json does not re-resolve skills from skills.yaml.** No new lock created; targets orphaned silently.
- **L8. Sync with unresolved state and `--locked` gives empty-commit error.** Should say "entry is unresolved," not "pinned commit unavailable."
- **L9. Very long URLs crash with raw OS `file name too long` error.** No URL length validation.
- **L10. Double-scheme URL `https://https://...` produces confusing trust error.** Should validate URL structure before processing.
- **L11. Empty-host URL `https:///path` produces confusing trust error.** Accepted by `trust add`.
- **L12. `create` with empty name defers to `--description` check.** Error ordering inconsistent.
- **L13. `init --project` succeeds in non-git directory.** Creates non-functional skill infrastructure.
- **L14. Lint returns empty array for binary and empty SKILL.md files.** Should flag degenerate content.
- **L15. Lint does not warn on SKILL.md with no YAML frontmatter.** No `name:` or `description:` required.
- **L16. `cache prune` has no `--force` or `--max-age` flag.** Recently-fetched caches always skipped.
- **L17. `cache prune --text` writes output to stderr instead of stdout.**
- **L18. `migrate vercel` emits JSON error array to stderr.** Only command with structured stderr errors.
- **L19. Template vars with function-call syntax (`{{project_name}}`) accepted at add time, rejected at render time.**

### Info / Positive findings (8 notable)

- Symlink attacks properly blocked in skill extraction with clear error messages.
- Large file DoS blocked with size limit (~1MB).
- YAML anchor/alias expansion (billion-laughs pattern) handled safely.
- Command injection via skill names, URLs, and flags all properly blocked.
- Git credential embedding in URLs properly rejected.
- Cache path traversal explicitly rejected (`invalid path component: ".."`).
- Tag-pinned skills stay pinned after update (correct version semantics).
- Unicode content in SKILL.md handled with full fidelity.

## Findings by Category

### Data Corruption / State Corruption (9 findings)

H1 (partial sync poisons manifest), H2 (concurrent add race), H4 (case-variant duplicates), H5 (lock deletion cascading prune), M1-M4 (foreign-target cluster), M11 (empty lock orphans targets).

**Common root cause:** The manifest is the single source of target ownership, but it gets rewritten on every sync. Any operation that causes a skill to drop out of the manifest (failed render, missing lock, untrusted source, stale commit) turns that skill's targets into "foreign" objects that block future syncs.

### Security (3 findings)

H3 (path traversal via targets in skills.yaml), M5 (file:// and ssh:// in trust), path traversal via `--path` flag escapes temp directory into host filesystem.

**Note:** Despite these gaps, the security posture is strong overall. Symlinks, command injection, credential embedding, cache path traversal, and YAML deserialization attacks are all properly defended.

### Missing Validation (11 findings)

M5-M10, M15, L14-L15, and lint gaps for binary/empty/no-frontmatter content.

### Incorrect Behavior / Silent Success (12 findings)

M12-M14, M16, M22, L1-L8, and the `add` exit-code-0-on-sync-failure pattern.

### Harness Infrastructure (6 findings)

H6 (zombies), M17-M21.

### Error Messages (5 findings)

L8-L11, L12.

## Patterns Observed

### 1. The "foreign target" failure mode is the #1 systemic weakness

Six independent triggers converge on the same broken state: rendered skill directories classified as "foreign" that block subsequent syncs. The pattern is:

1. Some operation causes the manifest to be rewritten without a skill entry
2. The rendered targets from that skill survive on disk
3. Next sync sees targets not in manifest -> classifies as "foreign" -> conflict error
4. Recovery requires manual `adopt` per skill per target, or `--force`

Triggers found: partial sync failure (H1), lock deletion (H5), stale commit (M1), trust cycling (M2), server outage + cache loss (M3), deleted target dir (M4).

**Fix:** The manifest write should be transactional with the target write. If a skill fails to render, its previous targets should remain in the manifest as "stale" rather than being dropped.

### 2. `add` treats its internal sync as best-effort but exits 0

The `add` command always exits 0 if the lock write succeeds, even when the internal sync fails. Since the sync writes the manifest, a sync failure means the manifest and targets diverge. This is the entry point for the H1 cascading corruption.

### 3. Forward/backward compatibility is fragile

Strict JSON deserialization (`DisallowUnknownFields`) means a lock.json written by a newer auto-skill version with extra fields is unreadable by an older version. No version-gated schema migration exists.

### 4. Output format conventions are inconsistent

Three different flag conventions (`--format`, `--text`, both) across subcommands make automation unreliable. Trust commands write to stderr. Some commands default to text, others to JSON.

### 5. Input validation is applied inconsistently

Name validation (`^[a-z0-9]+(-[a-z0-9]+)*$`) is enforced by `get`, `remove`, and `create` but not by `describe` or lock.json parsing. Trust endpoint validation rejects `http://` but accepts `file://`, `ssh://`, wildcards, and empty hosts.

### 6. Security is defense-in-depth (mostly working)

Most attack vectors are stopped, but sometimes by accident rather than by design. Path traversal in skill names is blocked by `os.MkdirTemp` rejecting separators, not by input validation. The `--path` flag traversal is stopped by filesystem permissions, not by path sanitization. The one real gap is H3 (targets in skills.yaml with `..` components).

## Recommendations (Prioritized)

### P0: Fix before next release

1. **Validate target paths in skills.yaml against traversal.** Reject any target containing `..` or absolute paths. This is the only confirmed arbitrary-write vulnerability. (H3)

2. **Make `add` exit non-zero when post-add sync fails.** Or at minimum, do not write targets if the manifest cannot be written completely. This prevents the H1 cascading corruption. (H1)

3. **Add file locking for lock.json writes.** Use `flock` or Go's `os.O_EXCL` to prevent concurrent add races. (H2)

4. **Make manifest writes transactional with target writes.** If a skill fails to render, carry its previous entry forward in the manifest rather than dropping it. This is the architectural fix for the "foreign target" cluster (M1-M4).

### P1: Fix soon

5. **Case-normalize skill names on lock.json parse.** Reject or merge entries that differ only in case. (H4)

6. **Validate skill names in lock.json at parse time.** Reject empty strings, names with path separators, names failing the canonical regex. (M8)

7. **Restrict `trust add` to HTTPS scheme only.** Reject `file://`, `ssh://`, wildcards, and structurally invalid URLs. (M5, M6)

8. **Apply name validation uniformly across all commands.** `describe` should use the same regex as `get` and `remove`. (M7)

9. **Make `update` exit non-zero for nonexistent skill names.** (M14)

10. **Guard against lock deletion: require confirmation or `--force` for sync --locked when lock is missing/empty.** (H5)

### P2: Should fix

11. **Use lenient JSON deserialization (allow unknown fields) for lock.json.** Add a version check that warns on future versions but still attempts to parse. (M15)

12. **Standardize output format flags.** Pick one convention (`--format json|text` or `--format` + `--text`) and apply across all subcommands. (M22)

13. **Make `trust add`/`trust remove` write to stdout, not stderr.** (L1)

14. **Exit non-zero for unknown subcommands.** (L4)

15. **Lint should flag binary, empty, and no-frontmatter SKILL.md files.** (L14, L15, M10)

16. **Strip URL query strings and fragments before cache path construction.** (L3)

### P3: Harness improvements

17. **Add `init: true` to SUT service in docker-compose.yaml.** Fixes zombie accumulation. (H6)

18. **Add `--project` flag or unique naming to harness compose invocations.** Prevents concurrent agent clobber. (M17)

19. **Disable `http.receivepack` on git-server.** Prevents fixture mutation. (M18)

20. **Set `git config --global init.defaultBranch main` in SUT Dockerfile.** (M19)

21. **Add resource limits (PID, memory) to docker-compose.yaml.** (M20)

22. **Add cleanup-on-failure to `Harness.up()`.** Call `down()` in except block. (M21)

23. **Add network policy or internal-only DNS to prevent external internet access from SUT.**

## Harness Coverage Assessment

### Well-covered

- **Add/sync/remove happy path and edge cases:** Thoroughly tested including batch ops, concurrent ops, partial failures, empty states, and rapid cycling.
- **Security boundaries:** Symlinks, command injection, path traversal, credential embedding, YAML bombs, large file DoS all tested with clear pass/fail results.
- **TLS transport + trust chain:** Full HTTPS exercised including trust add/remove cycling, server outage/restore, cert validation.
- **State corruption and recovery:** Lock deletion, corruption, empty lock, stale commits, missing files all probed.
- **Input validation:** Extensive fuzzing of skill names, URLs (double-scheme, empty-host, credentials, query strings, long URLs), and frontmatter content (binary, unicode, YAML bombs).
- **Output format consistency:** All subcommands tested for flag acceptance and output channel (stdout vs stderr).
- **Malformed SKILL.md handling:** Binary, empty, no-frontmatter, unclosed frontmatter, invalid YAML, and missing name all tested.

### Needs more testing

- **Template rendering with `customize:` variables.** Only one negative test (function-call syntax error). No positive tests with valid templates and variable substitution.
- **`migrate vercel` flow.** Only error-path tested (missing lock file). Needs a mock vercel skills-lock.json fixture for the happy path.
- **`adopt` workflow.** Referenced in error messages as the recovery path for "foreign" targets, but never exercised as a fuzz target.
- **Branch/tag pinning semantics.** Tag pinning tested (stays pinned, correct); branch tracking tested minimally. No tests for branch deletion, tag mutation, or version constraint conflicts.
- **Multi-file skills.** All test skills are single SKILL.md files. Skills with auxiliary files (scripts, configs, examples) are untested.
- **Concurrent operations from multiple workspaces sharing a cache.** The TOCTOU race was found for concurrent adds in one workspace; cross-workspace cache sharing is untested.
- **`create` + `lint` authoring workflow.** Create tested minimally (empty name, path traversal). The full author->lint->publish loop is untested.
- **Non-root user execution.** Both containers run as root. Permission-related bugs that would surface for regular users are invisible.
- **Offline/air-gapped operation.** `list` tested offline (works from local state); `add`, `create`, `doctor` not tested offline.
