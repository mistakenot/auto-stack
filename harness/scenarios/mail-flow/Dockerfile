# Single-target image for the mail-flow scenario (D-062-1).
#
# The `builder` stage compiles the real `auto` binary once from monorepo source;
# the `host` stage carries it into one container that stands in for one machine.
# Unlike event-flow there is only ONE runtime image, because mail is host-global
# and the harness's own convention gives each container a distinct host id — so
# two containers would be two hosts, not two agents on one.
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
COPY . .
RUN cd auto-cli && go build -o /usr/local/bin/auto ./cmd/auto

# ── host: one host id, one ~/.auto, two registered project workspaces ────────
# Each "agent" is a separate `auto` invocation run with its cwd in one of the
# workspaces. That is genuinely two agents on one host, and it exercises the
# concurrent-writer case D-11 names without inventing a shared-HOME volume the
# harness does not have.
FROM alpine:3.21 AS host
RUN apk add --no-cache git && \
    git config --global user.email "agent@harness.local" && \
    git config --global user.name "Agent Harness" && \
    git config --global init.defaultBranch main
COPY --from=builder /usr/local/bin/auto /usr/local/bin/auto
COPY harness/scenarios/mail-flow/scripts/host-entrypoint.sh /host-entrypoint.sh
RUN chmod +x /host-entrypoint.sh
WORKDIR /workspace
ENTRYPOINT ["/host-entrypoint.sh"]
