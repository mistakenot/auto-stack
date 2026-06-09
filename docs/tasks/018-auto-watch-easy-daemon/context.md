# Context: Task 018

Verified codebase facts grounding the no-sudo `auto watch` daemon design. See [solution.md](solution.md).

## Key Files

### Daemon install package (`auto-watch/internal/daemoninstall/`) — system-only today
- `manager.go:17-50` — `Manager` struct + `NewManager(runner shell.Runner)`. **`unitDir` is
  hardcoded to `/etc/systemd/system` at `manager.go:39`** — the primary seam for user scope.
  Manager has injectable seams: `getenv`, `geteuid`, `currentUser`, `lookupUser`, `lookupGroupID`,
  `lookPath` (all wired to stdlib in NewManager; map-mocked in tests).
- `manager.go:13-14` — `defaultServiceBase = "autowatch"`, `defaultDescription = "autowatch daemon"`
  (service identity — retained per task 017).
- `shell/runner.go:10-12` — `Runner` interface: `Run(ctx, name string, args ...string) (stdout, stderr string, err error)`. Single injection point for all systemctl/loginctl calls.
- `resolve.go:9-70` — `resolveSpec`; BinPath defaults to `filepath.Join(home, ".local","bin","auto")`
  (`:48`); PathEnv via `defaultPathEnv(home)` (`:109`) = `$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin`;
  UnitPath = `filepath.Join(m.unitDir, serviceName)` (`:79`).
- `resolve.go:84-106` — `resolveRuntimeUser`: explicit `--user` → `SUDO_USER` (`:87`) → current user;
  **rejects `root`** (`:99-104`).
- `template.go:9-27` — the **only** unit template; `[Service] ExecStart={{.BinPath}} watch start`
  (`:21`), `User=/Group=/Environment=HOME=/PATH=/WorkingDirectory=`, and **`[Install]
  WantedBy=multi-user.target`** (`:26`) — user scope needs `default.target`.
- `install.go:12-77` — `Install`: render → write unit (atomic, `writeFileAtomic`) →
  `systemctl daemon-reload` (`:59`) → `systemctl enable` if `opts.Enable` (`:64-68`) →
  `systemctl start` if `opts.Start` (`:70-75`). `runSystemctl` (`:122-141`) calls
  `m.Runner.Run(ctx, "systemctl", args...)` — **always bare `systemctl`, no `--user`**.
- `status.go:13-82` — `Status`: reads unit file, parses User/Group/Home/BinPath, then
  `systemctl is-enabled`/`is-active` (`readSystemctlState`, `:132-164`), then runtime
  `<BinPath> watch status --json` (`readRuntimeStatus`, `:166-194`; wraps with `sudo -u <user> env`
  when not already that user — user scope avoids the sudo wrap).
- `restart.go:9-48` — `Restart`: `systemctl is-active` then `systemctl try-restart <service>` (`:41`).
- `validate.go` — unit-path validation currently assumes `/etc/systemd/system/` (must allow
  `~/.config/systemd/user/` for user scope).
- `spec.go:15-52` — `InstallOptions` (ServiceName, RuntimeUser, HomeDir, WorkingDir, BinPath,
  PathEnv, Description, UnitPath, Enable, Start, DryRun, PrintUnit), `RestartOptions`,
  `StatusOptions`. **No scope/mode field yet** — add `Scope`.

### CLI (`auto-watch/internal/cli/daemon.go`)
- `:13-24` parent `daemon` cmd → `install|restart|status`.
- `:26-89` install flags: `--service-name`, `--user` (runtime user), `--home`, `--working-dir`,
  `--bin`, `--path-env`, `--enable`, `--start`, `--dry-run`, `--print-unit`. **No `--system`**.
  Output around `:70` assumes system unit (`sudo systemctl status …`).
- `:91-112` restart (`--service-name`), `:114-170` status (`--service-name`, `--json`).

### Update flow
- `auto-shared/update/update.go:34-67` — `Run(stdout, stderr)`: checks latest tag, else
  `runInstallScript(stderr, stderr, tag)` (`:57`) which pipes `install.sh` to `bash -s` (`:123-129`).
  **No daemon-restart hook.** (Task 017 routed install.sh output to stderr so the JSON result is
  the sole stdout payload.)
- `auto-cli/cmd/auto/main.go` `newUpdateCmd` and `auto-watch/internal/cli/update.go:11-28` — both
  call `update.Run` and marshal JSON. `auto-cli` is a separate module and **cannot import
  `auto-watch/internal/daemoninstall`** (Go internal rule) — hence the restart belongs in install.sh.
- `install.sh:5` `INSTALL_DIR="$HOME/.local/bin"`; `:43-51` removes old inode + `fuser`-detects a
  running daemon (filters parent PID), sets `RESTART_SERVICES`; `:66-73` prints a **restart hint
  only** (does not restart). No `/usr/local/bin` placement anywhere.
- `Makefile:123-135` `install` target: `cp bin/auto $(INSTALL_DIR)` with "text file busy" handling;
  `INSTALL_DIR ?= $(HOME)/.local/bin`. No symlink.

### Doctor
- `auto-watch/internal/doctor/doctor.go:16-34` — `Run` checks tmux, claude, git, settings,
  project config. **No daemon/unit checks** — add `checkDaemonUnit`.

### Tests (mock pattern to reuse)
- `daemoninstall/daemoninstall_test.go:17-49` — `fakeRunner` (ordered expected systemctl steps,
  `AssertDone`); `:51-139` `newTestRig` wires Manager seams to temp dirs + mock users/groups.
  `:214-241` asserts exact `daemon-reload/enable/start` sequence; `:309-413` status incl. the
  `sudo -u alice env … auto watch status --json` runtime call. New user-scope tests assert
  `systemctl --user …`, unit dir under home, `WantedBy=default.target`.

## Patterns
- **systemd interaction is fully behind `shell.Runner`** — every new systemctl/loginctl call must
  go through `m.Runner.Run` so it's testable with `fakeRunner` (don't shell out directly).
- **Scope switch, not a parallel implementation** — reuse `resolveSpec`/`Install`/`Status`/`Restart`;
  branch only at unit dir, systemctl args, `WantedBy`, and path validation.
- **CLI conventions** (root `CLAUDE.md:95-112`): JSON default on stdout, diagnostics to stderr;
  `init`/`doctor`/`quickstart` standard subcommands; config under `~/.auto/watch/`.
- **Service identity retained** (task 017): unit name `autowatch.service`, `Description=autowatch
  daemon`; only `ExecStart` is `auto watch start`.

## Design-doc tension to resolve (AC-7)
- `docs/autostack-install-daemon.md:50-66` "Why System `systemd`" **explicitly rejects
  `systemd --user`** as "tied to session behavior" and favors system units (survives logout,
  starts at boot, restart on crash). Task 018 reverses the default to user units; the rewrite must
  note that **`loginctl enable-linger`** addresses the logout/boot concerns the doc raised, and
  reposition system mode as the headless/multi-user opt-in. The doc's "the actual process must not
  run as root" (`:24`) constraint still holds (daemon runs as the user in both modes).
- `auto-watch/internal/cli/quickstart.go:87` already says "Install as a systemd **user** service"
  while the code installs a **system** unit — fixing this is part of AC-7.
- `README.md:25-46` documents curl-install + `auto update`, with **no** daemon install/update
  section — AC-7 adds one.

## Related Tasks / History
- **Task 017 (unify-binaries-into-auto), commit `cd80ea9`** — shipped the single `auto` binary; set
  `ExecStart=…/auto watch start`, default daemon `BinPath=~/.local/bin/auto`, runtime status
  `auto watch status --json` (added the `watch` infix), retained `autowatch.service` identity, and
  added the "post-upgrade: re-run `auto watch daemon install`" note this task builds on. Also brought
  `auto-watch` into the Makefile `PROJECTS` loop, so `daemoninstall_test.go` runs in CI (`make test`).
  Surfaced the `sudo`/`secure_path` gap (binary only in `~/.local/bin`) that AC-3 addresses, and the
  current `install.sh` shape (`BINARIES="auto"`, the `fuser`/`RESTART_SERVICES` block with the
  parent-PID exclusion) that AC-2 extends.
- **User-level systemd is GREENFIELD *for the daemoninstall package*.** `git log -S 'enable-linger'`
  and `-S 'XDG_RUNTIME_DIR'` return no matches (no prior linger / user-runtime-dir handling). There
  IS pre-existing `systemctl --user` usage to reference — `scripts/vps/setup-vps.sh:19-28` uses it
  to run Docker as a user service (`git log -S 'systemctl --user'` → cd80ea9, 67ee43e, aa506aa) — but
  nothing in `auto-watch` or `daemoninstall` does; the

<!-- RESOLVED(P3): `git log -S 'systemctl --user'` is NOT empty — tighten the claim
REVIEW: Verified: `git log -S 'systemctl --user'` returns 3 commits (cd80ea9, 67ee43e, aa506aa)
because `scripts/vps/setup-vps.sh:19-28` uses `systemctl --user` for Docker. `enable-linger` and
`XDG_RUNTIME_DIR` do return no matches. The "greenfield *for the daemoninstall package*"
conclusion still holds, but the blanket "all return no matches" is wrong and could mislead the
implementer into thinking there's zero prior `systemctl --user` usage to reference. Reword to
scope the claim to the daemoninstall package, and note setup-vps.sh as a pre-existing example.
AUTHOR: Reworded — scoped "greenfield" to the daemoninstall package, kept the verified no-matches
for `enable-linger`/`XDG_RUNTIME_DIR`, and pointed to `scripts/vps/setup-vps.sh:19-28` as a
pre-existing `systemctl --user` reference the implementer can borrow from.
-->

  `daemoninstall` package and its tests are **system-scope only** today. No earlier dedicated
  watch-daemon task exists — 017 is the only prior work touching this code.
