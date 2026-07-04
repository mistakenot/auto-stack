#!/bin/sh
# Seed a distinct host id, register /workspace as a project, and start a
# co-located autowatch daemon: loopback hook-ingest (so `auto hooks fire` POSTs
# pass HookIngest's loopback-only gate) plus a TCP RPC listener on 0.0.0.0 so the
# auto-ui peer container can dial it and subscribe.
set -e

: "${HOST_ID:?HOST_ID must be set (distinct per agent container)}"

# Distinct host id per container — the daemon stamps this on every event, so it
# is what yields real multi-host attribution downstream (D-4 / D-6).
mkdir -p "$HOME/.auto"
printf '{"hostId":"%s"}\n' "$HOST_ID" > "$HOME/.auto/host.json"

cd /workspace
git init -q
git commit -q --allow-empty -m init
# Register /workspace so the daemon derives doc.changed for it (a registered
# project is required by bus.DeriveDocChanged).
auto init --project

auto watch start \
  --hook-addr 127.0.0.1:7787 \
  --rpc-addr tcp://0.0.0.0:7788 \
  --ready-file /tmp/watch-ready.json &

# Fail-fast: block until the daemon has bound BOTH listeners (ready-file is
# written only after the RPC + hook listeners are up). The healthcheck also
# gates on this file, so a daemon that never binds keeps the service unhealthy.
timeout 20 sh -c 'until [ -f /tmp/watch-ready.json ]; do sleep 0.2; done'

exec sleep infinity
