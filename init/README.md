# init — project bootstrap

Set up the current directory as a project with the auto coding stack:

```bash
curl -fsSL https://raw.githubusercontent.com/mistakenot/auto-stack/main/init/bootstrap.sh | bash
```

`bootstrap.sh` is stack/language agnostic and idempotent. Rerun it any time: every
step checks before it acts, and anything already present (yours, a teammate's, or
a previous run's) is left alone. Conflicts are reported as warnings with a fix,
never overwritten.

```bash
... | bash -s -- --dir path/to/project                 # somewhere other than $PWD
... | bash -s -- --skill-module planning-workflow \
                 --skill-module reflection             # choose mistakenot/skills modules
init/bootstrap.sh --no-commit --no-daemon              # from a checkout
```

Flags: `--dir DIR`, `--skill-module NAME` (repeatable; default `planning-workflow`,
`none` to skip), `--no-commit` (the initial commit is only made when the script
created the repo), `--no-daemon` (don't install the `auto watch` systemd user unit).

What you get:

| Piece | Result |
|---|---|
| Tools | `auto` (latest release); `sops`, `age`, `br` pinned + SHA-256 verified; all in `~/.local/bin` |
| Secrets | age key at the sops default path (generated only if missing), `.sops.yaml`, encrypted `secrets.yaml` |
| Agents | `AGENTS.md`; `CLAUDE.md` and `GEMINI.md` symlink to it (an existing `CLAUDE.md` becomes `AGENTS.md`) |
| Docs | `docs/` + `auto doc` (global + project), seed `docs/README.md`, doc index in `AGENTS.md` |
| Skills | `auto skill init` (targets `claude,agents`) + mistakenot/skills modules via its `install.sh --module` |
| Issues | beads: `br init` with the project id as issue prefix |
| Watch | host config, project registered with the `auto watch` daemon, user daemon if systemd is available |
| Hooks | `auto hooks install` (Claude/Codex → watch daemon), Session-Id commit trailer hook |
| Env | `compose.yaml` (postgres example commented out); `auto env` renders a per-worktree `.env` (`COMPOSE_PROJECT_NAME` + ports) and runs compose through `scripts/dev-services.sh` under `sops exec-env secrets.yaml` |

## Testing

```bash
init/test/run.sh           # Ubuntu 24.04 container, auto installed from the GitHub release
init/test/run.sh --local   # pre-install auto built from this checkout instead
init/test/run.sh --shell   # keep the container and open a shell after the checks
```

`test/scenarios.sh` runs inside the container. It feeds the script on stdin, the
same way the one-liner does, and covers four scenarios:

1. a fresh directory;
2. a rerun, which must change nothing: files, symlinks, HEAD, hooks, age key and registry;
3. per-worktree `auto env`, including a sops secret reaching compose;
4. a repo already set up by someone else, with a foreign sops key, its own hook, compose file and agent files. Their files must survive, and a rerun must again be a no-op.
