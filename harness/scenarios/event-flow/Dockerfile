# Multi-target image for the event-flow scenario (D-8).
#
# The `builder` stage compiles the `auto` binary once from monorepo source; the
# `agent` and `autoui` targets each copy that single binary and set their own
# entrypoint — no double compile. Compose selects a target via `target:`.
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
COPY . .
RUN cd auto-cli && go build -o /usr/local/bin/auto ./cmd/auto

# ── agent: co-located autowatch daemon + `auto hooks fire` source ────────────
# Each agent container fires hooks loopback-locally (passing HookIngest's
# loopback-only gate) and exposes its RPC listener on 0.0.0.0 for auto-ui.
FROM alpine:3.21 AS agent
# `auto watch start` runs a doctor precheck that hard-fails unless tmux (>=3.0)
# and a `claude` CLI are on PATH. tmux is real (alpine ships 3.x); the daemon
# never spawns claude in this scenario (we drive it via `auto hooks fire`), so a
# no-op `claude` stub satisfies the LookPath check without pulling the real CLI.
RUN apk add --no-cache git tmux && \
    git config --global user.email "agent@harness.local" && \
    git config --global user.name "Agent Harness" && \
    git config --global init.defaultBranch main && \
    printf '#!/bin/sh\nexit 0\n' > /usr/local/bin/claude && \
    chmod +x /usr/local/bin/claude
# The documented seam that points `auto hooks fire` at the co-located daemon's
# loopback hook-ingest listener (matches --hook-addr below).
ENV AUTO_WATCH_HOOK_ADDR=127.0.0.1:7787
COPY --from=builder /usr/local/bin/auto /usr/local/bin/auto
COPY harness/scenarios/event-flow/scripts/agent-entrypoint.sh /agent-entrypoint.sh
RUN chmod +x /agent-entrypoint.sh
WORKDIR /workspace
ENTRYPOINT ["/agent-entrypoint.sh"]

# ── autoui: auto-ui dashboard subscribing to each agent's RPC backend ────────
FROM alpine:3.21 AS autoui
COPY --from=builder /usr/local/bin/auto /usr/local/bin/auto
COPY harness/scenarios/event-flow/scripts/autoui-entrypoint.sh /autoui-entrypoint.sh
RUN chmod +x /autoui-entrypoint.sh
ENTRYPOINT ["/autoui-entrypoint.sh"]
