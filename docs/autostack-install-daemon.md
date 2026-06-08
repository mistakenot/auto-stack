---
hash: "25b4aad9"
id: "394677e5"
read_when: "implementing daemon installation or systemd service management"
summary: "Design and implementation spec for installing and managing the autowatch daemon as a system systemd service running as a non-root user."
title: "autostack install-daemon"
---

# `autostack install-daemon`

This document describes the preferred production setup for running `auto watch` on a server, and how a future `auto watch daemon install` command should support that setup.

> **Post-upgrade (merged `auto` binary):** the watch daemon now ships as the single `auto`
> binary and the systemd unit invokes `ExecStart=…/auto watch start`. Deployments whose unit
> was generated before the merge have a stale `ExecStart=…/autowatch start` line that points at
> the removed `autowatch` binary. After upgrading, run `auto watch daemon install` once to
> regenerate the unit so it points at `…/auto watch start`.

The required behavior is:

- the daemon must continue running when no SSH sessions are active
- the daemon should start on boot
- installing or updating the daemon may use `sudo`
- the actual `autowatch` process must not run as `sudo` or root
- binary installation and daemon restart remain separate explicit steps

That means the correct service model is:

- a system `systemd` unit installed under `/etc/systemd/system/`
- `User=` and `Group=` set to a normal non-root user
- `ExecStart` pointing at that user's `auto` binary

## Recommendation

The best simple default is:

1. install binaries into the target user's `~/.local/bin`
2. install one system `systemd` unit for `autowatch`
3. configure that unit to run as the target non-root user
4. manage it with `sudo systemctl ...`

This matches the current `autowatch` runtime model:

- daemon state lives under `~/.auto/watch/`
- registered repos are stored in `~/.auto/watch/settings.json`
- the daemon already has its own 60 second loop
- the daemon already has a host-local lockfile
- one daemon can manage multiple repos

## Why System `systemd`

This is the right default because it gives us:

- survives logout
- starts at boot
- restart on crash
- standard logs via `journalctl`
- explicit operator workflow via `systemctl`
- no dependence on active login sessions

This is better than:

- cron wrappers
- `systemd --user` units tied to session behavior
- manually launched tmux sessions

## Runtime User Model

The installer may run with `sudo`, but the daemon must run as a normal user.

That means:

- root is only for writing the unit, reloading `systemd`, enabling, starting, and restarting the service
- the unit itself must set `User=<target-user>` and `Group=<target-user>`
- `HOME` must be set explicitly to that user's home directory
- the target user's `~/.local/bin/auto` should be the binary used by `ExecStart`

The daemon process must never run as root.

## Two Command Approach

The deployment model should use two explicit commands:

1. one command to install or update binaries
2. one command to install or restart the daemon service

This is better than making plain `make install` automatically restart a background service.

Why:

- `make install` is a binary install step, not a process manager
- restarting a system service may require `sudo`
- some machines will have binaries installed but no daemon configured
- operators may want to update binaries without bouncing the daemon immediately

So the intended workflow is:

```bash
make install
sudo systemctl try-restart autowatch.service
```

Or with future product commands:

```bash
make install
sudo autostack restart-daemon --service-name autowatch
```

For first-time setup:

```bash
make install
sudo autostack install-daemon --enable --start
```

For later updates:

```bash
make install
sudo autostack restart-daemon
```

## Binary Update Behavior

`make install` should not automatically restart `autowatch`.

That restart should be a second, explicit action.

The right default restart behavior is:

```bash
sudo systemctl try-restart autowatch.service
```

`try-restart` is preferable to `restart` because:

- it restarts the daemon if it is already running
- it does nothing if the service is installed but currently stopped
- it avoids unexpectedly starting the daemon on machines where the operator has intentionally left it stopped

`daemon-reload` is not required when only the binary changes. It is only needed when the unit file itself changes.

## Recommended Host Layout

Use the target normal user for runtime state and credentials.

Example:

```text
/home/alice/
  .local/bin/auto          # single merged binary (all tools are subcommands)
  .claude/...
  .auto/watch/...
  src/
    repo-a/
    repo-b/

/etc/systemd/system/
  autowatch.service
```

Key rules:

- `HOME` must be stable because `autowatch` stores config, logs, lockfiles, and runtime artifacts there
- watched repos should be readable and writable by the runtime user
- Claude credentials should belong to the same runtime user
- only trusted repos should be watched, because scheduled Claude runs use unattended execution

## One Daemon Per Host

The simplest and correct v1 model is one daemon per host and per runtime user.

That single daemon should watch multiple repos by reading `~/.auto/watch/settings.json`.

This is better than one service per repo because:

- the current lock model is host-local, not distributed
- one process is easier to inspect and restart
- one SQLite database holds all event logs and run state
- config changes are already picked up on the next tick without daemon restart

## Operator Workflow

The intended setup flow is:

```bash
which auto claude tmux git
auto watch doctor
```

For each repo:

```bash
cd ~/src/my-repo
auto watch init --project-id my-repo
auto watch task create --id nightly-etl --bash "auto etl run"
auto watch trigger create --id nightly --cron "0 2 * * *"
auto watch trigger add-task --trigger nightly --task nightly-etl
auto watch task run --id nightly-etl
```

Only after the foreground task path works should the daemon be installed and enabled.

## Proposed Unit

The default unit content should be conceptually equivalent to:

```ini
[Unit]
Description=autowatch daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=alice
Group=alice
Environment=HOME=/home/alice
Environment=PATH=/home/alice/.local/bin:/usr/local/bin:/usr/bin:/bin
WorkingDirectory=/home/alice
ExecStart=/home/alice/.local/bin/auto watch start
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Important:

- this is a system unit, not a user unit
- it should be written to `/etc/systemd/system/autowatch.service`
- the runtime user must be explicit
- the runtime user must not be root

## What `autostack install-daemon` Should Do

The command should be an installer for the system `systemd` unit, not a new process manager.

It should:

1. verify Linux + `systemd`
2. verify that `auto` exists on `PATH` or at an explicit `--bin` path
3. determine the intended runtime user
4. determine that user's home directory
5. render a unit file with explicit `User`, `Group`, `HOME`, `PATH`, and `ExecStart`
6. install the unit into `/etc/systemd/system/`
7. run `systemctl daemon-reload`
8. optionally run `systemctl enable --now`
9. print the next operator commands for verification

It should not:

- run the daemon as root
- require a dedicated new Unix user
- invent a second scheduler layer
- create one daemon per repo
- silently overwrite unrelated unit files
- hide the unit content from the operator
- restart unrelated services as a side effect of ordinary binary installation

## Runtime User Selection

The command needs an explicit runtime-user rule.

Recommended behavior:

- if `--user <name>` is provided, use that
- else if `SUDO_USER` is set, use `SUDO_USER`
- else use the current user, but reject `root`

That matters because the installer may be run via `sudo`, and in that case the current process user is `root` but the daemon must still run as the original non-root user.

## Recommended CLI Shape

The simplest useful command shape is:

```bash
sudo autostack install-daemon \
  --user alice \
  --bin /home/alice/.local/bin/auto
```

Recommended optional flags:

- `--service-name autowatch`
- `--home /home/alice`
- `--working-dir /home/alice`
- `--path /home/alice/.local/bin:/usr/local/bin:/usr/bin:/bin`
- `--enable`
- `--start`
- `--dry-run`
- `--print-unit`

### Expected behavior

- default behavior installs the unit and prints what changed
- `--dry-run` prints the unit and intended actions without writing anything
- `--print-unit` prints only the rendered unit content
- `--enable` runs `systemctl enable`
- `--start` runs `systemctl start`

`--enable --start` together should be the normal path.

## Separate Restart Command

If we add daemon lifecycle commands under `autostack`, it should also include an explicit restart command for rolling out new binaries to a running daemon.

Example:

```bash
sudo autostack restart-daemon --service-name autowatch
```

That command should:

1. verify the named service exists
2. run `systemctl try-restart <service>`
3. print the resulting status command

It should not rebuild binaries itself.

That separation keeps responsibilities clear:

- build and copy binaries first
- restart the daemon second

## Status Command

If we add daemon lifecycle commands under `autostack`, we should also add a status command.

Example:

```bash
autostack status
```

That command should explicitly check whether the daemon is:

- not installed
- installed but stopped
- installed and running

### What it should check

At minimum:

1. whether `/etc/systemd/system/autowatch.service` exists
2. whether `systemctl is-enabled autowatch.service` reports enabled
3. whether `systemctl is-active autowatch.service` reports active

If the service is active, it should then also read the richer runtime view from `auto watch status`.

### Suggested output shape

Text mode should make the distinction obvious:

```text
daemon installed: yes
daemon enabled: yes
daemon running: yes
service manager: systemd
runtime user: alice
```

If available, append the richer `auto watch status` summary after that.

JSON mode should separate install state from process state:

```json
{
  "daemon": {
    "installed": true,
    "enabled": true,
    "running": true,
    "serviceName": "autowatch.service",
    "unitPath": "/etc/systemd/system/autowatch.service",
    "manager": "systemd",
    "user": "alice"
  },
  "runtime": {
    "daemon_running": true
  }
}
```

### Why this matters

`auto watch status` by itself only tells us about runtime state from the daemon's point of view.

That is not enough for daemon installation UX.

The operator needs to distinguish:

- no daemon unit installed at all
- daemon unit exists but is not enabled
- daemon unit exists and is enabled but not running
- daemon is installed and running normally

Without this, a failed install and a stopped daemon look too similar.

## Why This Should Live Under `autostack`

This command is broader than `autowatch` itself.

It is not about defining tasks or triggers. It is about installing stack infrastructure onto a host. Putting it under a future umbrella CLI such as `autostack` keeps that boundary clear:

- `autowatch` manages watch behavior
- `autostack` manages host-level installation and orchestration

## Permissions Model

The installer may require `sudo`.

That is acceptable because it is managing system-wide `systemd` state.

But the daemon itself must not run as root.

So the honest model is:

- use `sudo` for install, enable, start, restart, and status of the service
- use a normal user for the daemon process itself
- always render the unit with explicit runtime user information

## Safety Checks

Before writing the unit, the installer should check:

- `auto watch doctor` passes or at least runs
- `tmux`, `git`, and `claude` are visible in the configured service `PATH`
- the target home directory exists
- the target bin path exists and contains `auto`
- the chosen runtime user exists
- the chosen runtime user is not `root`

If checks fail, the command should stop with a concrete remediation hint.

## Post-Install Verification

After installation, the operator should be able to run:

```bash
sudo systemctl status autowatch
journalctl -u autowatch -f
autostack status
auto watch status
auto watch logs -n 50
auto watch health
```

These commands should be shown at the end of `autostack install-daemon`.

After a later binary update, the operator workflow should be:

```bash
make install
sudo systemctl try-restart autowatch.service
sudo systemctl status autowatch.service
```

## Implementation Tech Spec

This section defines how to implement the daemon-install feature in Go.

### Scope

Implement three commands under a new `autostack` CLI:

- `autostack install-daemon`
- `autostack restart-daemon`
- `autostack status`

This feature should only manage the daemon service lifecycle. It should not create tasks, triggers, or wrap `autowatch` business logic.

### Recommended Project Layout

Add a new standalone Go module for the umbrella CLI.

Suggested layout:

```text
autostack/
├── cmd/
│   └── autostack/
│       └── main.go
├── internal/
│   ├── cli/
│   │   ├── root.go
│   │   ├── install_daemon.go
│   │   ├── restart_daemon.go
│   │   └── status.go
│   ├── daemoninstall/
│   │   ├── spec.go
│   │   ├── resolve.go
│   │   ├── template.go
│   │   ├── install.go
│   │   ├── restart.go
│   │   ├── status.go
│   │   └── validate.go
│   └── shell/
│       └── runner.go
├── go.mod
└── go.sum
```

Why:

- `cli` stays thin and only handles flags, output mode, and exit behavior
- `daemoninstall` contains the actual service-management logic
- `shell` isolates process execution so tests can stub it cleanly

### Unit Template Location

The unit-file template should live in Go code, not as a checked-in `.service` file.

Reason:

- the unit is parameterized by runtime user, home directory, working directory, binary path, and service name
- we want one source of truth in code rather than a static file plus substitution logic
- dry-run and print-unit modes become trivial if the template is already in memory

Use a raw string constant or `text/template` inside `internal/daemoninstall/template.go`.

Suggested approach:

```go
const unitTemplate = `[Unit]
Description={{.Description}}
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User={{.RuntimeUser}}
Group={{.RuntimeGroup}}
Environment=HOME={{.HomeDir}}
Environment=PATH={{.PathEnv}}
WorkingDirectory={{.WorkingDir}}
ExecStart={{.BinPath}} start
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
`
```

Use `text/template` rather than manual string concatenation so the rendered unit is deterministic and easy to test.

### Core Types

Use explicit structs for resolution, rendering, and status.

Suggested types:

```go
type ServiceSpec struct {
    ServiceName string
    Description string
    RuntimeUser string
    RuntimeGroup string
    HomeDir     string
    WorkingDir  string
    BinPath     string
    PathEnv     string
    UnitPath    string
}

type InstallOptions struct {
    ServiceName string
    RuntimeUser string
    HomeDir     string
    WorkingDir  string
    BinPath     string
    PathEnv     string
    Enable      bool
    Start       bool
    DryRun      bool
    PrintUnit   bool
}

type ServiceStatus struct {
    Installed   bool           `json:"installed"`
    Enabled     bool           `json:"enabled"`
    Running     bool           `json:"running"`
    ServiceName string         `json:"serviceName"`
    UnitPath    string         `json:"unitPath"`
    Manager     string         `json:"manager"`
    User        string         `json:"user,omitempty"`
    Runtime     map[string]any `json:"runtime,omitempty"`
}
```

Keep these structs in `daemoninstall/spec.go`.

### Shell Execution Boundary

All shell-outs should go through one small interface so tests can stub them.

Suggested interface:

```go
type Runner interface {
    Run(ctx context.Context, name string, args ...string) (stdout string, stderr string, err error)
}
```

This should be enough for:

- `systemctl`
- `id`
- `getent`
- `which`
- `autowatch`

Avoid spreading `exec.Command` calls across many files.

### Runtime User Resolution

Implement one shared resolution path in `resolve.go`.

Rule:

1. if `--user` is set, use it
2. else if `SUDO_USER` is set, use `SUDO_USER`
3. else use the current user
4. reject `root`

Then resolve:

- home directory
- primary group
- default working directory
- default binary path

Preferred sources:

- `os/user.Lookup` for user and home lookup
- primary group from the same user lookup when available
- default `BinPath = <home>/.local/bin/auto`
- default `WorkingDir = <home>`
- default `UnitPath = /etc/systemd/system/<service>.service`

If `BinPath` does not exist, fail with a remediation hint:

```text
run make install first
```

### Validation Rules

Validation should be strict and centralized in `validate.go`.

At minimum validate:

- runtime user exists
- runtime user is not `root`
- home directory exists
- working directory exists
- binary path exists and is executable
- all path values are absolute
- service name matches a simple slug pattern, for example `^[a-z0-9]+(?:-[a-z0-9]+)*$`
- unit path is under `/etc/systemd/system/`
- `systemctl` exists on `PATH`

Reject suspicious values early rather than trying to sanitize everything.

### `install-daemon` Algorithm

The algorithm should be:

1. resolve `InstallOptions` into a fully populated `ServiceSpec`
2. validate the `ServiceSpec`
3. render the unit file into memory
4. if `--print-unit`, write the rendered unit to stdout and exit
5. if `--dry-run`, print the unit path, rendered unit, and planned `systemctl` actions, then exit
6. write the unit file atomically to `/etc/systemd/system/<service>.service`
7. run `systemctl daemon-reload`
8. if `--enable`, run `systemctl enable <service>.service`
9. if `--start`, run `systemctl start <service>.service`
10. print next verification commands

Do not automatically restart an already running daemon during install unless the user explicitly requested `--start`.

The installer should be idempotent:

- if the rendered unit matches the existing unit content exactly, still allow `--enable` and `--start`
- only rewrite the file when contents differ

### Atomic Unit Writes

Write the unit file atomically:

1. render unit content to memory
2. write to a temp file in the same directory
3. `fsync` if practical
4. `rename` over the target unit path

This avoids partially written unit files if the process is interrupted.

File mode should be `0644`.

### `restart-daemon` Algorithm

The algorithm should be:

1. resolve the service name, default `autowatch.service`
2. verify `/etc/systemd/system/<service>` exists
3. run `systemctl try-restart <service>`
4. optionally print `systemctl status <service>` guidance

Use `try-restart`, not `restart`.

This command should not:

- call `make install`
- rebuild binaries
- mutate the unit file

### `status` Algorithm

The algorithm should be:

1. resolve the service name and unit path
2. check whether the unit file exists
3. call `systemctl is-enabled <service>`
4. call `systemctl is-active <service>`
5. if the service is active, call `auto watch status --json`
6. return one combined payload that separates install state from runtime state

The command should distinguish clearly between:

- not installed
- installed but disabled
- installed and enabled but stopped
- installed and running

If `auto watch status --json` fails while the service is active, do not hide that. Return the installation state plus a runtime warning.

### Invoking `auto watch status`

When the service is active, the status command should enrich output by running the runtime CLI.

Recommended invocation:

```bash
sudo -u <runtime-user> <bin-path> status --json
```

Reason:

- it matches the daemon's actual runtime user context
- it uses the same `HOME` and runtime database the daemon is using
- it avoids accidentally reading root's home directory when the caller is using `sudo`

This means the install/status code needs to know the runtime user and `BinPath` from the service spec or parsed unit file.

### Parsing Existing Unit State

For status, do not rely only on assumptions from flags. Inspect the installed unit file when present.

At minimum extract or track:

- `User=`
- `Group=`
- `Environment=HOME=...`
- `ExecStart=...`

Simplest approach:

- parse the unit file line-by-line for the small set of keys we care about

There is no need for a full generic systemd parser in v1.

### Output Contract

Default output should be human-readable text.

Add `--json` for `status`.

`install-daemon` and `restart-daemon` can stay text-only in v1 unless broader `autostack` conventions require JSON everywhere.

In text mode:

- print success information first
- then print next commands

In JSON mode:

- stdout must contain only parseable JSON
- remediation or diagnostics go to stderr

### Failure and Remediation Rules

Every hard failure should include a concrete remediation hint.

Examples:

- missing binary: `run make install first`
- invalid runtime user: `rerun with --user <non-root-user>`
- missing unit: `run sudo autostack install-daemon first`
- inactive service: `run sudo systemctl start autowatch.service`

### Makefile Interaction

Do not embed service restarts into plain `make install`.

Instead, keep this split:

- `make install` copies binaries
- `autostack install-daemon` installs/enables/starts the service when needed
- `autostack restart-daemon` rolls forward a running service after a binary update

This matches the two-command approach documented above.

### Testing Strategy

#### Unit tests

Add unit tests for:

- runtime user resolution with and without `SUDO_USER`
- root rejection
- absolute-path validation
- unit rendering
- unit parsing for status
- dry-run output planning

#### Integration tests

Use temp directories and fake command runners for:

- install writes the correct unit content
- install does not rewrite when content is unchanged
- install calls `systemctl daemon-reload`
- `--enable --start` triggers the expected `systemctl` calls
- restart uses `try-restart`
- status returns installed/stopped/running states correctly

#### Optional privileged tests

Real systemd integration should be gated and opt-in.

For example:

- only run if `AUTOSTACK_E2E=1`
- only run in a container or VM with real systemd available

Do not make privileged systemd tests mandatory for normal `go test ./...` runs.

### Incremental Implementation Order

Implement in this order:

1. scaffold the `autostack` module and root CLI
2. add `Runner` abstraction
3. implement service-spec resolution and validation
4. implement embedded unit rendering
5. implement `install-daemon --dry-run` and `--print-unit`
6. implement real unit installation and `systemctl daemon-reload`
7. implement `--enable` and `--start`
8. implement `restart-daemon`
9. implement `status`
10. add tests

That keeps the risky privileged path late in the rollout while still allowing fast feedback on the pure logic early.

## Non-Goals

This proposal does not include:

- support for launchd
- support for Windows services
- Kubernetes deployment helpers
- container-first deployment patterns
- distributed locking across multiple hosts

Those can come later. The simplest correct thing for this mode is a system `systemd` installer for a single-user `autowatch` daemon.

## Acceptance Criteria And Validation Timing

Do not finalize acceptance criteria or validation steps yet.

Those sections should be added after:

1. `autostack install-daemon` is implemented
2. the daemon is installed on this machine
3. the installed service is exercised end-to-end on this machine

Reason:

- the final acceptance bar should match the real implementation, not an imagined one
- the validation section should be based on commands and checks that were actually used successfully here
- host-level service behavior needs to be verified on a real machine, not only inferred from design

When added later, the document should include two explicit sections:

- `Acceptance Criteria`
- `Validation Methods`

Those later sections should cover at least:

- unit file installed at the expected system path
- unit runs as the intended non-root user
- daemon survives logout and has no dependence on active SSH sessions
- daemon starts at boot
- `autostack status` correctly distinguishes installed, enabled, and running states
- `make install` followed by daemon restart rolls the service onto the updated binary
- `autowatch` runtime state is stored under the runtime user's home directory
- operator verification commands that were actually run successfully on this machine

## Summary

If we add `autostack install-daemon`, it should be a thin, explicit installer for one system `systemd` unit that runs one `auto watch start` process as a normal non-root user.

That is the best match for the final requirements here:

- survives logout
- no active SSH session required
- installer may use `sudo`
- daemon process itself must not be root
- one explicit binary install step
- one explicit daemon install or restart step
