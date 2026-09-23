# Single-target image for the lock-flow scenario (D-11).
#
# The `builder` stage compiles the real `auto` binary once from monorepo source;
# the `host` stage carries it into one container that stands in for one machine.
# There is only ONE runtime image because the lock store is host-global and the
# harness's own convention gives each container a distinct host id — so two
# containers would be two hosts, not two Workers on one.
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
COPY . .
RUN cd auto-cli && go build -o /usr/local/bin/auto ./cmd/auto

# ── host: one host id, one ~/.auto, one project, two Workers ─────────────────
# Each "Worker" is a separate `auto` invocation run with AUTO_LOCK_WORKER set to
# worker-a or worker-b in the same workspace. That is genuinely two Workers on
# one host sharing one store, and it exercises the override rung of the identity
# chain without needing tmux or linked worktrees in-container.
FROM alpine:3.21 AS host
RUN apk add --no-cache git && \
    git config --global user.email "agent@harness.local" && \
    git config --global user.name "Agent Harness" && \
    git config --global init.defaultBranch main
COPY --from=builder /usr/local/bin/auto /usr/local/bin/auto
# A stub GitHub CLI: `auto lock clear` verifies a holder's PR through
# `gh pr list`, and this stub answers from a file the tests write, so a PR can
# be flipped between OPEN and MERGED without a network. See scripts/gh.
COPY harness/scenarios/lock-flow/scripts/gh /usr/local/bin/gh
COPY harness/scenarios/lock-flow/scripts/host-entrypoint.sh /host-entrypoint.sh
RUN chmod +x /usr/local/bin/gh /host-entrypoint.sh
WORKDIR /workspace
ENTRYPOINT ["/host-entrypoint.sh"]
