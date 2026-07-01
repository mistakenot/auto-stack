I want to build a reusable end-to-end testing harness for AutoSkill This will cover as much of the functionality as possible like in terms of like the different layers we touch So what I'm thinking is we have a docker compose file that stands up a harness We have we simulate a remote git repository by having a docker container running with like a git server host That's you know other containers can connect to and git pull from we then have the system under test Which it will be a docker file which will you know build? The latest version of the code base from source and then run that inside the containers The way I'm thinking that this will work is that we we have a very kind of basic set of wrapper scripts around this Maybe just Python for now which will basically like stand this up and kind of act as like the You know entry points into this thing so that the actual tests themselves can either be Run using like it like I'm kind of thinking that the Python will take all of the infrastructure abstract over it and create almost like a really simple deep deep DSL and then that DSL can either be scripted for like regular, you know end-to-end tests or You know an a coding agent could just do probes which are like Informal test testing that isn't written down, but it can just play with the DSL and You know kind of in the same way that playwright abstracts over a lot the browser Complications and flakiness that's kind of what I'm thinking maybe this DSL could do But let's just start there and experiment see how far we get

For the V1, the completion signal will be we can stand up Docker compose with a couple of subcontainers One of those containers builds the latest version of the tool and runs that inside the container Another container acts as like a git server which is hosting a agent skills repository I just want a basic test that can basically simulate setting up a new project with auto skills, cloning skills from that remote repository And that's it


I'm thinking that the system under test container will, you know, to actually take actions within that you go through the Python DSL or the Python library and Python library abstracts around creating, you know, SSH commands that run inside that container. Another thing we need to really try to defend against flakiness as much as possible. So in all places wherever we can like in Docker compose in our Python DSL, we always want like some sort of granular validation that previous class have worked. So like when we set up the Git server, you know, we want that Docker infrastructure to really obviously and quickly fail if that's not working for some reason.

---

## V1 Status: COMPLETE

The harness is built and working. Run `uv run pytest -v` to execute the test suite (6 tests, ~70s).

## Architecture

- **git-server**: Alpine container running nginx + fcgiwrap + git-http-backend as an HTTPS smart HTTP git server. Self-signed CA + server cert generated at build time. Seeds a bare repo with fixture skills and serves it over `https://git-server/repos/skills.git`
- **sut** (system under test): Two-stage Dockerfile — `golang:1.26-alpine` builds the `auto` binary from the monorepo source, `alpine:3.21` runs it. Entrypoint waits for the CA cert from a shared volume before accepting commands
- **Transport**: Real HTTPS over Docker network — exercises the full auto-skill remote code path: `transport.CanonicalizeURL()` → blobless cache clone (`--filter=blob:none`) → `Realize` (`--refetch`) → `git archive` extraction → render to targets. No shortcuts
- **TLS trust chain**: `gen-certs.sh` creates an EC CA + server cert with SAN=DNS:git-server. The CA cert is shared via a Docker volume (`shared-certs`). The SUT entrypoint configures `git config --global http.sslCAInfo` to trust it
- **Python DSL**: `Harness` class wraps Docker Compose lifecycle + `docker compose exec` for command execution. `Result` type with `.stdout`, `.stderr`, `.exit_code`, `.json()`, `.ok`

## Usage

```bash
# Run the test suite
cd auto-skill/harness
uv run pytest -v

# Interactive probing via CLI
uv run harness up
uv run harness run sut "auto skill add 'https://git-server/repos/skills.git' --skill deploy-checklist"
uv run harness run sut "auto skill sync --text"
uv run harness down

# Import in Python
from harness.core import Harness, GIT_REMOTE_URL
h = Harness()
h.up()
h.fresh_workspace("my-test")
h.trust_source("/workspace/my-test")
r = h.run("sut", f"cd /workspace/my-test && auto skill add '{GIT_REMOTE_URL}' --skill deploy-checklist")
print(r.json())
h.down()
```

## Decisions

- **HTTPS git server**: nginx + fcgiwrap + git-http-backend on Alpine. Self-signed CA with EC keys (prime256v1). This exercises the real transport layer — `CanonicalizeURL()` normalizes all remote URLs to HTTPS, so HTTPS is the only scheme that tests the full cache/clone/realize path.
- **Build freshness**: Full `COPY . .` from monorepo root. The `.dockerignore` at the monorepo root excludes `.git`, `.tmp` (1.8GB), `bin`, `dist`, and other heavy dirs to keep the build context manageable.
- **Python DSL scope**: CLI-first (`uv run harness up/run/down`), importable second. Same `Harness` class in both modes.
- **Fixture skills repo**: Static fixtures in `harness/fixtures/`. Two SKILL.md files (deploy-checklist, code-review). Git-init'd into the bare repo at startup.
- **Test runner**: pytest with session-scoped fixture for `up()`/`down()`.
- **Trust gate**: Tests pre-approve `https://git-server` via `auto skill trust add` since the trust gate is fail-closed for non-TTY usage.

## Key Files

```
harness/
├── docker-compose.yaml          # Two services: git-server, sut
├── Dockerfile.git-server        # nginx + fcgiwrap HTTPS git server
├── Dockerfile.sut               # Two-stage: build auto binary, run in Alpine
├── fixtures/skills/             # Fixture SKILL.md files seeded into the repo
├── scripts/
│   ├── gen-certs.sh             # EC CA + server cert generation
│   ├── init-repos.sh            # Seed bare repo, start fcgiwrap + nginx
│   ├── nginx.conf               # HTTPS reverse proxy to git-http-backend
│   └── sut-entrypoint.sh        # Wait for CA cert, configure git trust
├── src/harness/
│   ├── core.py                  # Harness + Result classes
│   └── cli.py                   # Click CLI (up/down/status/run/skill)
├── tests/
│   ├── conftest.py              # Session-scoped harness fixture
│   └── test_add_sync.py         # 6 E2E tests covering init → add → sync
└── pyproject.toml
```

## Open Questions

- **Coverage targets (post-V1)**: Template rendering with `customize:` vars, `update --check` for drift detection, upstream rename handling.
- **Parallel test isolation**: Each test creates its own workspace, but they share the SUT container. If tests need true isolation, we could add per-test workspace cleanup.

## DSL Improvement

Aim: make the harness more reusable, easier to maintain, easier to keep up to date when the code base changes.

Can we experiment by layering another API ontop of the CLI to add things like:
- RenameRemoteSkill("old-name", "new-name")
- SyncSkillsFromRemote()

Each command class also contains the instructions to _assert each command individually ran ok_ between uses
Can still drop down to just plain old SSH if you need to

We should use pydantic and make each command fully self describing. then in the cli, we make it easy for coding agents to both a. discover all commands with schemas and b. run commands against the harness.

This is how we unlock better probing / ad hoc testing experince.

put all command classes in single file so its easy to see them all
