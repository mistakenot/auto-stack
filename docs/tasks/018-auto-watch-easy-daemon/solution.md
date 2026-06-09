# Solution: Task 018

## Approach

Parameterize the **existing** `auto-watch/internal/daemoninstall` package by a `Scope`
(`user` | `system`), default to **user**, and wire the one-command-update + self-healing
pieces around it. No new daemon architecture — the system-unit code already exists; we add a
user-scope code path and flip the default.

1. **Add a `Scope` to the daemon manager + options.** Introduce `daemoninstall.Scope` (`user`,
   `system`, default `user`). Thread it through so these four seams switch on scope:
   - **Unit dir** (`manager.go:39`): `system` → `/etc/systemd/system`; `user` → `<home>/.config/systemd/user`.
   - **systemctl args** (`install.go`/`status.go`/`restart.go`): `user` prefixes `--user`.
   - **Unit `WantedBy`** (`template.go:26`): `system` → `multi-user.target`; `user` → `default.target`.
   - **Unit-path validation** (`validate.go`): allow `~/.config/systemd/user/…` for user scope.
2. **Default to no-sudo user install (AC-1).** `auto watch daemon install` (no flags) → resolve
   runtime user = current user, ensure `~/.config/systemd/user/` exists, write unit,
   `systemctl --user daemon-reload && enable && start`. Add `--system` to opt into today's
   behavior. Scope-aware output hints (no `sudo systemctl status` line in user mode).

<!-- RESOLVED(P2): `systemctl --user` needs XDG_RUNTIME_DIR / a user D-Bus session
REVIEW: Every `systemctl --user …` call (both the Go install path here and the install.sh
restart hook in step 4) requires `XDG_RUNTIME_DIR=/run/user/$UID` and a running user-bus;
without them systemctl exits with "Failed to connect to bus". In a normal interactive login this
is present, but `auto update` over a plain non-login SSH (or any context where the user manager
isn't started) will hit this. The design doesn't mention the dependency or how it surfaces.
Decide: (a) detect a missing user bus and emit a clear remediation rather than a raw systemctl
error, and (b) confirm the Runner inherits the caller's environment (the install path goes through
`shell.Runner`/`ExecRunner` — verify it passes XDG_RUNTIME_DIR through). Note this in the docs
caveat too.
AUTHOR: Accepted. Step 2 now (a) preflights the user bus in user scope — if `XDG_RUNTIME_DIR` is
unset or `systemctl --user` can't connect, emit a clear remediation ("no user D-Bus session;
log in / `loginctl enable-linger`, or run with `--system`") instead of a raw "Failed to connect to
bus"; (b) requires verifying `ExecRunner` passes the caller's env through (it must inherit
`XDG_RUNTIME_DIR`/`DBUS_SESSION_BUS_ADDRESS` — `exec.Command` does by default unless `Env` is set;
confirm `ExecRunner` doesn't override `Env`). install.sh's restart is already guarded
(`is-active` only succeeds with a working bus). The docs caveat (step 8 / plan 5.x) calls out the
user-bus requirement and the `--system` fallback for non-login/headless contexts. Plan Step 1.5a +
Step 5.1 added.
-->

3. **Boot/logout persistence (AC-6).** After a user-scope install, best-effort
   `loginctl enable-linger <user>`; on failure (polkit/permission) print a clear warning that
   the daemon is session-scoped until linger is enabled. Never silently succeed.
4. **One-command update (AC-2) — done in `install.sh`, not Go.** `auto update` already re-runs
   `install.sh`. Extend `install.sh`: after replacing the binary, if `systemctl --user is-active
   autowatch.service` → `systemctl --user restart autowatch.service` (no sudo). If only the
   **system** unit is active (needs sudo) → keep the printed hint. This makes user-mode update a
   single command and avoids coupling `auto`'s Go modules across the `internal/` boundary.
5. **System-mode sudo reachability (AC-3).** Document `sudo "$(command -v auto)" watch daemon
   install --system` as the canonical system invocation (works with no symlink). Additionally,
   when `auto watch daemon install --system` can't proceed because it isn't running as root,
   emit a remediation that includes the optional `sudo ln -sf <binpath> /usr/local/bin/auto`
   so bare `sudo auto …` works thereafter. (No user-run step can write `/usr/local/bin`.)
6. **Self-healing doctor (AC-4).** Add `checkDaemonUnit` to `auto watch doctor`: locate the unit
   (user scope, then system), parse `ExecStart`, verify the referenced binary exists + is
   executable. Flag a dangling/old (`…/autowatch start`) ExecStart with remediation
   `auto watch daemon install`. Re-running install is already idempotent (regenerates the unit).
7. **Legacy migration (AC-5).** No new command — `auto watch daemon install` regenerates the
   unit and (user mode) restarts via the install.sh hook; for an existing **system**
   `autowatch.service`, the documented path is `sudo "$(command -v auto)" watch daemon install
   --system && sudo systemctl restart autowatch.service`. Service identity (`autowatch.service`,
   `Description=autowatch daemon`) is retained; only `ExecStart` changes. Doctor surfaces the need.
8. **Docs (AC-7).** Rewrite the "Why System `systemd`" section of
   `docs/autostack-install-daemon.md` to a **user-first** rationale (linger covers
   logout/boot; system mode is the headless/multi-user opt-in). Add a daemon install + update
   section to `README.md`. Fix `auto-watch/internal/cli/quickstart.go` so its "user service"
   wording matches reality.

### Scope plumbing (outline)
```go
// daemoninstall/spec.go
type Scope string
const ( ScopeUser Scope = "user"; ScopeSystem Scope = "system" )
type InstallOptions struct { /* …existing… */ Scope Scope } // default ScopeUser when ""

// manager.go — derive unit dir + systemctl prefix from scope
func (m *Manager) unitDirFor(s Scope, home string) string // user→home/.config/systemd/user
func (m *Manager) systemctl(ctx, s Scope, args...) // prepends "--user" when ScopeUser
```

## Files
```
~ auto-watch/internal/daemoninstall/spec.go        # + Scope type; Scope field on Install/Restart/StatusOptions
~ auto-watch/internal/daemoninstall/manager.go     # unit dir + systemctl args derived from scope (was hardcoded /etc/systemd/system)
~ auto-watch/internal/daemoninstall/resolve.go     # user scope: unit dir under home/.config/systemd/user; runtime user = current user
~ auto-watch/internal/daemoninstall/template.go    # WantedBy by scope (multi-user.target | default.target)
~ auto-watch/internal/daemoninstall/install.go     # systemctl[--user]; mkdir user unit dir; best-effort loginctl enable-linger (user)
~ auto-watch/internal/daemoninstall/status.go      # scope-aware systemctl + unit-path read
~ auto-watch/internal/daemoninstall/restart.go     # scope-aware try-restart
~ auto-watch/internal/daemoninstall/validate.go    # allow ~/.config/systemd/user/ unit paths for user scope
~ auto-watch/internal/cli/daemon.go                # + --system flag (default user); scope-aware output hints
~ auto-watch/internal/doctor/doctor.go             # + checkDaemonUnit (dangling/old ExecStart → remediation)
~ install.sh                                       # user-scope: systemctl --user restart if active (no sudo); system: keep hint
~ docs/autostack-install-daemon.md                 # rewrite system-vs-user rationale → user-first; update examples
~ README.md                                        # add `auto watch daemon install` + update section
~ auto-watch/internal/cli/quickstart.go            # accurate user-service install/update wording
~ auto-watch/internal/daemoninstall/daemoninstall_test.go  # user-scope install/status/restart cases (fakeRunner)
~ auto-watch/internal/doctor/*_test.go             # checkDaemonUnit cases
```

## Test Coverage

| AC  | Test Type | File |
|-----|-----------|------|
| AC-1 | unit (fakeRunner) | `daemoninstall_test.go` — user install: unit under `~/.config/systemd/user`, `systemctl --user daemon-reload/enable/start`, `WantedBy=default.target`, no sudo |
| AC-2 | shell/e2e | `e2e/test-install.sh` (or a focused bats-style check): `install.sh` runs `systemctl --user restart` only when the user unit is active; no-op on fresh install |
| AC-3 | unit + docs | `daemoninstall_test.go` (`--system` reproduces `/etc/systemd/system` + `multi-user.target`); README/daemon-doc examples verified |
| AC-4 | unit | `doctor_test.go` — `checkDaemonUnit` flags missing/old `ExecStart` binary with remediation; OK when valid |
| AC-5 | unit + docs | `daemoninstall_test.go` (regenerate over an existing `…/autowatch start` unit → `…/auto watch start`, identity retained); documented migration path |
| AC-6 | unit (fakeRunner) | `daemoninstall_test.go` — user install attempts `loginctl enable-linger`; linger failure surfaces a warning, doesn't fail the install |
| AC-7 | manual/doc review | README + `docs/autostack-install-daemon.md` + quickstart examples reviewed/verified |

## Out of Scope
- Changing what the daemon *does* (trigger evaluation, task scheduling, run execution).
- macOS / `launchd` daemonization — Linux + systemd only (a future task).
- Multi-host / remote daemon orchestration.
- Actually migrating this host's running `autowatch.service` (tooling + documented path only).
- Auto-creating `/usr/local/bin/auto` from a user-run installer (no privilege); system mode
  documents the full-path invocation + optional manual symlink instead.

## Rejected Alternatives
- **`auto update` restart via a Go callback into `auto-shared/update.Run`** — would couple the
  shared updater to daemon logic, and the top-level `auto update` (in `auto-cli`) can't import
  `auto-watch/internal/daemoninstall` (Go `internal/` rule). Doing the restart in `install.sh`
  (which both `auto update` and the curl installer run) is simpler and module-clean.
- **Keep system-unit-by-default, add `--user` opt-in** — leaves the sudo/`secure_path` friction
  as the default experience; rejected per the resolved requirement (user-first).
- **A separate `auto watch daemon migrate` command** — unnecessary; `install` already regenerates
  idempotently and `doctor` detects the stale unit. Less surface, same outcome.
- **A systemd `PathChanged` watch unit to auto-restart on binary change** — too clever, adds a
  second unit to manage; the install.sh restart hook covers the real update flow.
