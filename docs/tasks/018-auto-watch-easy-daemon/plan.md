# Plan: Task 018

## Summary
Parameterize the existing `auto-watch/internal/daemoninstall` package by a `Scope`
(`user`|`system`, default `user`), add the no-sudo user-level install + `enable-linger`,
a doctor unit check, an install.sh restart hook for one-command update, and update docs.

## Changes
| Symbol | File | Description |
|--------|------|-------------|
| + | `auto-watch/internal/daemoninstall/spec.go` | `Scope` type (`ScopeUser`/`ScopeSystem`); `Scope` field on Install/Restart/Status options (default user) |
| ~ | `auto-watch/internal/daemoninstall/manager.go` | unit dir + systemctl arg prefix derived from scope (was hardcoded `/etc/systemd/system`) |
| ~ | `auto-watch/internal/daemoninstall/resolve.go` | user scope: unit dir under `<home>/.config/systemd/user`; runtime user = current user |
| ~ | `auto-watch/internal/daemoninstall/template.go` | `WantedBy` by scope (`multi-user.target`\|`default.target`) |
| ~ | `auto-watch/internal/daemoninstall/install.go` | `systemctl --user …` for user scope; mkdir user unit dir; best-effort `loginctl enable-linger` + warning |
| ~ | `auto-watch/internal/daemoninstall/{status,restart}.go` | scope-aware systemctl + unit-path read |
| ~ | `auto-watch/internal/daemoninstall/validate.go` | allow `~/.config/systemd/user/` unit paths for user scope |
| ~ | `auto-watch/internal/cli/daemon.go` | `--system` flag (default user); scope-aware output hints |
| ~ | `auto-watch/internal/doctor/doctor.go` | `checkDaemonUnit`: dangling/old `ExecStart` → remediation |
| ~ | `install.sh` | user scope: `systemctl --user restart` if active (no sudo); system: keep hint |
| ~ | `README.md` | daemon install + update section |
| ~ | `docs/autostack-install-daemon.md` | rewrite "Why System systemd" → user-first; update examples |
| ~ | `auto-watch/internal/cli/quickstart.go` | accurate user-service install/update wording |
| ~ | `auto-watch/internal/daemoninstall/daemoninstall_test.go` | user-scope install/status/restart + enable-linger + `--system` parity |
| ~ | `auto-watch/internal/doctor/*_test.go` | `checkDaemonUnit` cases |

## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test
- [ ] `daemoninstall_test.go` — user install: unit path under `.config/systemd/user`, `systemctl --user daemon-reload/enable/start`, `WantedBy=default.target`, `ExecStart=<home>/.local/bin/auto watch start`, no sudo; `--system` parity (`/etc/systemd/system`, `multi-user.target`); enable-linger failure is non-fatal
- [ ] `doctor_test.go` — `checkDaemonUnit` flags a unit whose `ExecStart` binary is missing/old (`autowatch`) with remediation `auto watch daemon install`; passes when valid
- [ ] `install.sh` — `bash -n` clean; with a stub `systemctl` on PATH: user unit active → emits `systemctl --user restart autowatch.service`; not active → no restart (fresh install safe)
- [ ] `scripts/check-no-stale-binary-refs.sh` — still green after doc/quickstart edits
- [ ] `make check build test vulncheck` green; manual smoke: `auto watch daemon install --print-unit` (user) and `--system --print-unit` render the right unit

## Execution Sequence
```
Phase 1 (Scope core: daemoninstall) ──┬──> Phase 2 (CLI: --system, hints)
                                      └──> Phase 3 (doctor checkDaemonUnit)
Phase 4 (install.sh restart hook + system reachability)   ── independent ──┐
Phase 5 (docs: daemon-doc rationale, README, quickstart)  ── independent ──┤
                                                                           │
Phase 6 (verify: make check/build/test/vulncheck + smoke) <── after 2,3,4,5
```
Phases 4 and 5 touch disjoint files (install.sh/Makefile; *.md + quickstart.go) and depend only on
the settled design, so they can start immediately. **Per the task-017 lesson, the executor should
dispatch phases serially in the shared worktree (concurrent subagents there leak/lose writes) and
verify each phase's files on disk before the next.**

## Plan

### Phase 1: Scope core — parameterize daemoninstall  *(sequential; blocks 2 & 3)*
- [x] Step 1.1: Add `Scope` type + constants (`ScopeUser`, `ScopeSystem`) in `spec.go`; add a `Scope`
      field to `InstallOptions`/`RestartOptions`/`StatusOptions`; treat empty as `ScopeUser`.
- [x] Step 1.2: `manager.go` — derive the unit dir from scope (`system`→`/etc/systemd/system`,
      `user`→`<home>/.config/systemd/user`) and add a helper that prefixes `--user` to systemctl
      args for user scope. Route all `runSystemctl`/status/restart calls through it.
- [x] Step 1.3: `resolve.go` — for user scope, set unit dir under the resolved home and default the
      runtime user to the current user (no `SUDO_USER` needed); keep system-scope behavior identical.
- [x] Step 1.3a: **Reorder resolution so home precedes the unit path.** In user scope the unit dir
      is `<home>/.config/systemd/user`, so `resolveSpec` must resolve the runtime user + `HomeDir`
      (via `currentUser()` — in user scope runtime user == invoking user) **before** building the
      unit path; today `resolveTarget` runs first (resolve.go:10) using the static `m.unitDir`.
      System scope keeps using `/etc/systemd/system` (no home dependency, order-independent).
- [x] Step 1.3b: **Give the read paths a unit dir too.** `Status` (status.go:14) and `Restart`
      (restart.go:10) currently call `resolveTarget` with no user/home resolution. Add a small
      scope-aware helper that, for user scope, resolves the current user's home → unit dir so these
      commands locate `<home>/.config/systemd/user/autowatch.service` (not the system path). The
      `--system` branch is unchanged.

<!-- RESOLVED(P2): Unit path is resolved before home is known — ordering must change
REVIEW: For user scope the unit dir is `<home>/.config/systemd/user`, so it depends on the home
directory. But `resolveSpec` computes the unit path FIRST: `resolveTarget(...)` runs at
resolve.go:10 (returns `filepath.Join(m.unitDir, serviceName)`), while the runtime user's account
and `HomeDir` aren't resolved until resolve.go:20-41. So at the moment the unit path is built,
home isn't available. The solution's `unitDirFor(s, home)` helper takes `home` as a param but the
plan doesn't say where that home comes from this early. Worse, the read paths don't resolve a
runtime user at all: `Status` (status.go:14) and `Restart` (restart.go:10) call `resolveTarget`
with no user/home resolution, yet for user scope they must locate `<home>/.config/systemd/user/
autowatch.service`. Spell out the source of `home` for the user unit dir — presumably the
*invoking* user's home via `currentUser()` up front (in user mode runtime user == current user) —
and reorder install so home is resolved before the unit path, and add home resolution to the
user-scope branch of Status/Restart. Otherwise these commands compute the wrong (or system) path.
AUTHOR: Added Step 1.3a (reorder `resolveSpec`: resolve runtime user + HomeDir via `currentUser()`
BEFORE building the unit path in user scope; system scope stays order-independent on
`/etc/systemd/system`) and Step 1.3b (a scope-aware unit-dir helper for the read paths so `Status`
and `Restart` resolve `<home>/.config/systemd/user/…` for user scope instead of the system path).
-->

- [x] Step 1.4: `template.go` — render `WantedBy=default.target` for user scope, `multi-user.target`
      for system. `ExecStart={{.BinPath}} watch start` unchanged.
- [x] Step 1.5: `install.go` — ensure the user unit dir exists (mkdir) before write; use the
      scope-aware systemctl; after a user-scope enable+start, best-effort `loginctl enable-linger
      <user>` via the Runner — on error, append a clear warning to the result, do NOT fail install.
- [x] Step 1.5a: **User-bus preflight (user scope).** `systemctl --user` needs `XDG_RUNTIME_DIR`
      + a running user bus. Before the user-scope systemctl calls, detect a missing bus (env unset
      or a probe like `systemctl --user is-system-running`/`show` failing to connect) and return a
      clear remediation ("no user D-Bus session — start a login session, run `loginctl enable-linger
      <user>`, or use `--system`") instead of surfacing a raw "Failed to connect to bus". Also verify
      `shell.ExecRunner` does NOT override `cmd.Env` (so it inherits `XDG_RUNTIME_DIR` /
      `DBUS_SESSION_BUS_ADDRESS`); if it sets `Env`, pass these through. Add a `fakeRunner` test for
      the missing-bus remediation path.
- [x] Step 1.6: `validate.go` — accept unit paths under `<home>/.config/systemd/user/` for user scope
      (keep `/etc/systemd/system/` for system).
- [x] Step 1.7: Update/extend `daemoninstall_test.go` with `fakeRunner`: user-scope install asserts
      unit dir, `systemctl --user daemon-reload/enable/start`, `WantedBy=default.target`, enable-linger
      attempted + non-fatal on failure; status/restart use `systemctl --user`; add a `--system` parity
      test and a regenerate-over-stale-unit test (`…/autowatch start` → `…/auto watch start`, identity retained).
- [x] Step 1.8: Verify: `cd auto-watch && go build ./... && go vet ./... && go test ./...` all green.
- [x] Step 1.9: Commit: `feat(018): phase 1 - scope-parameterized daemoninstall (user default)`

### Phase 2: CLI surface — `--system` flag + scope-aware output  *(depends on Phase 1)*
- [x] Step 2.1: `cli/daemon.go` — add `--system` bool to `install` (and `restart`/`status` as needed);
      default omitted = user scope; map to `opts.Scope`. Pass scope through to the Manager calls.
- [x] Step 2.1a: **Flip the install defaults so the unit is enabled+started by default** (both
      scopes): define `--enable`/`--start` with default **`true`** in cobra; users opt out with
      `--enable=false` / `--start=false`. This makes a no-flag `auto watch daemon install` write +
      `daemon-reload` + `enable` + `start` (AC-1). The existing documented `--enable --start`
      invocation stays valid (now a no-op). Phase 5.1 must update the daemon-doc "Expected behavior"
      section to reflect enable+start as the default.

<!-- RESOLVED(P1): Plan never enables/starts the unit by default — AC-1 will fail
REVIEW: AC-1 says `auto watch daemon install` (no flags) must leave the unit "enabled and
started". But `cli/daemon.go:83-84` defaults `--enable`/`--start` to `false`, and `Install`
only runs `systemctl enable`/`start` when `opts.Enable`/`opts.Start` are set (install.go:64-75).
Solution step 2 *describes* "daemon-reload && enable && start" and the AC-1 test row expects
`systemctl --user daemon-reload/enable/start`, but no plan step (and no entry in the solution
Files list) actually flips those defaults. As written, a no-flag install would write the unit
and stop — never enabling or starting it. Add an explicit step: in user scope (or whenever
`--system` is absent) default Enable+Start to true, and decide whether `--system` keeps the
opt-in `--enable --start` behavior the existing docs show. Without this the headline AC fails.
AUTHOR: Added Step 2.1a — flip `--enable`/`--start` cobra defaults to `true` for `install` in
BOTH scopes (opt out via `--enable=false`/`--start=false`), so a no-flag install does
write → daemon-reload → enable → start (AC-1). The previously-documented `sudo … --enable --start`
system invocation stays valid (no-op now). Noted that Phase 5.1's daemon-doc rewrite must update
the "Expected behavior" section (which currently says default only writes the unit). The AC-1 test
row already asserts the enable/start systemctl calls, so coverage is in place.
-->

- [x] Step 2.2: Make printed hints scope-aware — user mode prints `systemctl --user status …` (no
      `sudo`); system mode keeps the `sudo systemctl …` hint.
- [x] Step 2.3: Update any `cli` tests broken by the new flag/output.
- [x] Step 2.4: Verify: `cd auto-watch && go build ./... && go test ./...` green; `auto watch daemon
      install --print-unit` (no flags) renders a user unit (dir `~/.config/systemd/user`,
      `WantedBy=default.target`, `ExecStart=<home>/.local/bin/auto watch start`) and exits 0;
      `auto watch daemon install --system --print-unit` renders the system unit.
- [x] Step 2.5: Commit: `feat(018): phase 2 - daemon install --system flag + scope-aware hints`

### Phase 3: Doctor — dangling ExecStart check  *(depends on Phase 1)*
- [ ] Step 3.1: `doctor.go` — add `checkDaemonUnit`: locate the unit (user dir, then system), and if
      present parse `ExecStart`, resolve the binary path, and verify it exists + is executable.
- [ ] Step 3.2: Report a failing check with remediation `auto watch daemon install` when the binary is
      missing or the ExecStart still names the old `autowatch` binary; report `Status: "ok"` with an
      explanatory `Message` (e.g. "no daemon unit installed") when no unit is present, and `"ok"` when
      the unit is valid. Reuse daemoninstall unit-path/scope helpers from Phase 1. (Two-state
      `ok`/`fail` only — no new `"skip"` status.)
- [ ] Step 3.3: Add `doctor` tests covering: missing-binary ExecStart → `fail` + remediation; valid
      unit → `ok`; no unit installed → `ok` with the explanatory message.

<!-- RESOLVED(P3): No "skip" status exists — use "ok" for the no-unit case
REVIEW: `model.DoctorCheck.Status` is a freeform string and `doctor.Run` (doctor.go:27-32) only
treats `"fail"` specially; every existing check uses `"ok"` or `"fail"` (no `"skip"`). A `"skip"`
value would technically pass but breaks the established two-state convention and any consumer that
switches on status. For the "no unit installed" case, prefer `Status: "ok"` with an explanatory
Message (e.g. "no daemon unit installed") rather than inventing a third status.
AUTHOR: Updated Steps 3.2/3.3 to use `Status: "ok"` + explanatory Message for the no-unit case
(dropped the "skip/ok" wording); the check stays two-state `ok`/`fail` consistent with existing
doctor checks.
-->

- [ ] Step 3.4: Verify: `cd auto-watch && go build ./... && go test ./...` green; `auto watch doctor`
      runs and includes the new check.
- [ ] Step 3.5: Commit: `feat(018): phase 3 - doctor detects dangling daemon ExecStart`

### Phase 4: install.sh restart hook + system reachability  *(independent; depends only on design)*
- [ ] Step 4.1: `install.sh` — after the binary is replaced, if `systemctl` exists and
      `systemctl --user is-active --quiet autowatch.service`, run `systemctl --user restart
      autowatch.service` (no sudo) and report it; otherwise keep the existing system-mode restart hint.
      Preserve the existing `fuser`/`RESTART_SERVICES` parent-PID logic.
- [ ] Step 4.1a: **The restart must be failure-tolerant under `set -euo pipefail`** (install.sh:2).
      The binary is already replaced (lines 43-57), so the `restart` call must NOT abort the script:
      run it as `if systemctl --user restart autowatch.service; then echo "restarted"; else echo
      "<print the manual restart hint>"; fi` (or `… || true` + warn). The `is-active --quiet` guard
      under `if` is already safe (non-zero just skips). A failed restart degrades to the
      "restart it yourself" hint, never a failed `auto update`.

<!-- RESOLVED(P2): Guard the restart — install.sh runs under `set -euo pipefail`
REVIEW: install.sh:2 sets `set -euo pipefail`. The binary is replaced earlier in the script
(lines 43-57), so a `systemctl --user restart` that exits non-zero (e.g. user bus unavailable,
or a transient unit failure) would abort the whole script AFTER the new binary is already in
place — leaving `auto update` reporting failure on an otherwise-successful binary update. The
`is-active --quiet` guard under `if` is safe (non-zero just skips), but the `restart` call itself
must be tolerant: wrap it `|| true` (or capture status and warn) so a failed restart degrades to
the "restart it yourself" hint rather than failing the update. Call this out explicitly in the step.
AUTHOR: Added Step 4.1a making the restart failure-tolerant — the `restart` runs under an `if`
(or `|| true`) so a non-zero exit (missing user bus, transient unit failure) degrades to the
manual-restart hint instead of aborting the already-successful binary update. Added a matching
assertion to Step 4.3's verify (stub systemctl returning failure on restart → script still exits 0).
-->

- [ ] Step 4.2: System-mode reachability (AC-3): document/emit the `sudo "$(command -v auto)" watch
      daemon install --system` form; have `auto watch daemon install --system` (when not root) print a
      remediation that includes the optional `sudo ln -sf <binpath> /usr/local/bin/auto`. (Doc + the
      Phase-2 hint; no user-run installer writes `/usr/local/bin`.)
- [ ] Step 4.3: Verify: `bash -n install.sh` clean; with a stub `systemctl` shim on `PATH` returning
      "active" for `--user is-active autowatch.service`, install.sh emits the `systemctl --user restart`
      line; with "inactive", it does not (fresh-install safe); and with the stub failing the `restart`,
      install.sh still exits 0 and prints the manual-restart hint (set -euo pipefail tolerance).
- [ ] Step 4.4: Commit: `feat(018): phase 4 - install.sh restarts user daemon on update`

### Phase 5: Docs sweep  *(independent; depends only on design)*
- [ ] Step 5.1: `docs/autostack-install-daemon.md` — rewrite the "Why System `systemd`" section to a
      user-first rationale: user scope is the no-sudo default; system mode is the headless/multi-user
      opt-in. **Be honest about conditions (do NOT overpromise):** state that user-scope
      survives-logout/starts-at-boot **only after `loginctl enable-linger <user>` succeeds**, which on
      a default polkit host may itself need a one-time `sudo loginctl enable-linger <user>`; and that
      `systemctl --user` requires a user D-Bus session (`XDG_RUNTIME_DIR`), so non-login/headless
      contexts should use `--system`. Update the "Expected behavior" section to reflect that install
      now enables+starts by **default** (Step 2.1a). Update examples to `auto watch daemon install`
      (user) and `… --system` (system). Keep service-identity strings.
- [ ] Step 5.2: `README.md` — add a "Run auto watch in the background" section: `auto watch daemon
      install` (no sudo), `auto update` keeps it current, and the `--system` opt-in note.
- [ ] Step 5.3: `auto-watch/internal/cli/quickstart.go` — make the install/update wording accurately
      describe the user-level service (matches the new default) + the one-command update.
- [ ] Step 5.4: Verify: `scripts/check-no-stale-binary-refs.sh` green; `cd auto-watch && go build ./...`
      (quickstart.go compiles); docs examples eyeball-verified against the implemented flags.
- [ ] Step 5.5: Commit: `docs(018): phase 5 - user-first daemon docs (doc + README + quickstart)`

### Phase 6: Verify end-to-end  *(sequential barrier; depends on 2,3,4,5)*
- [ ] Step 6.1: `make check` (fmt-check + vet + lint + stale-ref guard) green.
- [ ] Step 6.2: `make build` + `make test` green (incl. auto-watch daemoninstall + doctor tests).
- [ ] Step 6.3: `make vulncheck` green (note: toolchain ≥ go1.26.4 per task 017).
- [ ] Step 6.4: Manual smoke: `auto watch daemon install --print-unit` (user) shows
      `~/.config/systemd/user` + `WantedBy=default.target` + `ExecStart=…/auto watch start`;
      `--system --print-unit` shows `/etc/systemd/system` + `multi-user.target`; `auto watch doctor`
      runs and flags a crafted stale unit.
- [ ] Step 6.5: Commit: `feat(018): phase 6 - full verification green` (or fold fixes into prior phases).

## Success Criteria
- [ ] `auto watch daemon install` (no flags), as a normal user, creates+enables+starts a `systemctl --user` unit, no sudo (AC-1)
- [ ] `auto update` (→ install.sh) restarts an active user daemon with no sudo; system mode prints a hint (AC-2)
- [ ] `auto watch daemon install --system` reproduces the system unit; `sudo "$(command -v auto)" …` documented + remediation emitted (AC-3)
- [ ] re-running install is idempotent; `auto watch doctor` flags a missing/old `ExecStart` with remediation (AC-4)
- [ ] documented one-command migration regenerates `…/autowatch start` → `…/auto watch start`, identity retained (AC-5)
- [ ] user mode attempts `loginctl enable-linger` and reports clearly if it can't be set (AC-6)
- [ ] README + daemon doc + quickstart show the no-sudo happy path and `--system` path; guard green (AC-7)
- [ ] `make check build test vulncheck` green

## Open Questions
- (none — all resolved in requirements; design settled in solution.md)
