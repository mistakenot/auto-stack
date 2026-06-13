---
hash: "c340b4b0"
id: "d887098e"
read_when: "implementing or troubleshooting the auto watch daemon install command"
summary: "Requirements for making auto watch daemon install work without sudo using a user-level systemctl --user service, with single-command updates, optional --system mode, idempotent/self-healing install, and accurate docs."
title: "Requirements: Task 018 — Auto Watch Easy Daemon"
---

# Task 018: auto-watch-easy-daemon

## Problem
Standing up the `auto watch` background daemon is awkward. The documented setup
(`sudo auto watch daemon install`) writes a **system** unit into `/etc/systemd/system/`,
but the merged `auto` binary installs to `~/.local/bin`, which `sudo`'s `secure_path`
can't find — so the command fails out of the box (`sudo: auto: command not found`).
Updating is also two manual steps (`auto update`, then a manual daemon restart), and
there's no first-class path to migrate the legacy `autowatch`-named unit. For other
users to adopt it, install and update must be simple and, by default, require no `sudo`.

## Goals
- Make `auto watch daemon install` work with **no sudo** by default (a user-level
  `systemctl --user` service running as the invoking user).
- Make updating a **single command** — `auto update` refreshes the binary and restarts
  the daemon.
- Keep an explicit **`--system`** mode for headless / multi-user / start-at-boot-without-login
  servers, and make that mode's `sudo` invocation actually resolve `auto`.
- Be **idempotent and self-healing**: re-running install safely regenerates the unit;
  `auto watch doctor` detects a stale/dangling `ExecStart` (e.g. the legacy
  `autowatch start`) and prints the exact fix.
- Update docs so the install/update story is accurate and copy-pasteable.

## Acceptance Criteria

**AC-1: No-sudo user install (default)**
- Given a normal (non-root) user with `auto` on PATH
- When they run `auto watch daemon install` (no flags)
- Then a `systemctl --user` unit is written under `~/.config/systemd/user/`, enabled and
  started, the daemon runs as that user, and **no `sudo` is required**. `ExecStart` points
  at the user's `auto` binary with the `watch start` subcommand.

**AC-2: One-command update**
- Given a managed `auto watch` daemon is active
- When the user runs `auto update`
- Then the binary is refreshed AND the daemon is restarted onto the new binary, with no
  separate manual restart step (and no `sudo` in user mode).

**AC-3: System mode retained + invocable**
- Given `auto watch daemon install --system`
- When run with the necessary privileges
- Then today's system-unit behavior is reproduced (`/etc/systemd/system/…`, `User=<non-root>`),
  AND the install path makes `auto` reachable to `sudo` (e.g. a `/usr/local/bin` symlink) so
  `sudo auto watch daemon …` resolves rather than failing with "command not found".

**AC-4: Idempotent + self-healing**
- Given an already-installed (or stale) unit
- When the user re-runs `auto watch daemon install`, or runs `auto watch doctor`
- Then install regenerates the unit safely (no duplicate/broken state), and `doctor` flags a
  unit whose `ExecStart` references a missing or old-named binary and prints the exact remediation.

**AC-5: Legacy migration**
- Given an existing `autowatch.service` whose `ExecStart` is `…/autowatch start`
- When the user follows the documented migration (single command where possible)
- Then the unit is regenerated to `…/auto watch start` and the running daemon is moved onto it,
  preserving daemon state under `~/.auto/watch/`. Service identity (`autowatch.service` name,
  `Description=autowatch daemon`) is retained.

**AC-6: Boot / logout persistence (user mode)**
- Given a user-mode install
- When the user logs out or the host reboots
- Then the daemon keeps running / restarts (via `loginctl enable-linger`), and if linger can't
  be set without elevation the tool reports it clearly rather than silently leaving a
  session-scoped daemon.

<!-- RESOLVED(P2): "No-sudo survives logout/reboot" is conditional — keep docs honest
REVIEW: This AC and the user-first framing imply the no-sudo default survives logout and starts
at boot. That is only true once `loginctl enable-linger <user>` succeeds — and on a default
systemd/polkit host, a normal user enabling their own linger typically requires admin
authorization, so in the common no-sudo flow linger will FAIL and the `systemctl --user` service
is killed on logout (and won't start at boot). The design correctly chooses to warn on failure
(good), but the AC-7 doc rewrite (solution step 8 / plan step 5.1) must not state the no-sudo path
"survives logout/starts at boot" unconditionally — it must say that requires linger, which may
itself need a one-time elevated `sudo loginctl enable-linger <user>`. Otherwise the docs overpromise
exactly the property the task is selling. Suggest the install output print the precise linger
remediation when the best-effort attempt fails.
AUTHOR: Agreed — AC-6 already mandates "report clearly if linger can't be set", and I've made the
doc obligation explicit: plan Step 5.1 now requires the daemon-doc/README to state that user-scope
survives-logout/starts-at-boot ONLY after `loginctl enable-linger` succeeds (which may need a
one-time `sudo loginctl enable-linger <user>`), and to steer non-login/headless contexts to
`--system`. Step 1.5 already prints a warning on linger failure; AC-6's test asserts the warning.
No AC text change needed — the conditional honesty lives in the docs/output, where this thread asked.
-->


**AC-7: Docs accurate and verified**
- Given README and `docs/autostack-install-daemon.md`
- When a new user follows them
- Then the no-sudo happy path and the `--system` path are both documented with
  copy-pasteable, verified commands; no example references a binary/command that doesn't exist.

## Out of Scope
- Changing what the daemon *does* (trigger evaluation, task scheduling, run execution) — this
  task is only its install/update/lifecycle UX.
- macOS / `launchd` daemonization — **Linux + systemd only** for this task. macOS users run
  `auto watch start` manually (e.g. under tmux); launchd support is a future task.
- Multi-host / remote daemon orchestration.
- **Actually migrating this host's running `autowatch.service`** — this task ships the tooling
  and a documented one-command migration path; performing the migration on the current machine
  stays a manual step run when ready.
- Re-litigating the single-`auto`-binary merge (task 017, already shipped).

## Open Questions
- [x] Default mode: user-level `systemctl --user` (no sudo) as the default, with `--system` opt-in?
      → **Yes — user service is the default; `--system` is opt-in.**
- [x] Is macOS daemon support in scope, or Linux/systemd-only for now?
      → **Linux/systemd only.**
- [x] Should `auto update` **auto-restart** the daemon, or only **print a restart hint**?
      → **Auto-restart** a managed, active daemon after refreshing the binary.
- [x] Should this task also migrate the existing system `autowatch.service` on the current host?
      → **No — ship tooling + documented migration path only; host migration is a manual step.**
