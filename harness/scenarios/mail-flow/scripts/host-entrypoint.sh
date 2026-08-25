#!/bin/sh
# Seed ONE host id, register two project workspaces on it, and initialise the
# alpha mail store. Two agents on one host = two registered workspaces sharing
# one ~/.auto, not two containers (D-062-1).
set -e

: "${HOST_ID:?HOST_ID must be set}"

mkdir -p "$HOME/.auto"
printf '{"hostId":"%s"}\n' "$HOST_ID" > "$HOME/.auto/host.json"

# `auto init --project` derives the project id from the repo directory name, so
# project-a and project-b register as distinct projects on the same host.
for ws in /workspace/project-a /workspace/project-b; do
  mkdir -p "$ws"
  cd "$ws"
  git init -q
  git commit -q --allow-empty -m init
  auto init --project
done

# The mail store. `alpha` is in the filename, not only in the docs (G10 / D-2).
auto mail init

# Fail-fast: the ready file is the healthcheck's gate, and it is written only
# once every gate above has succeeded (set -e aborts otherwise).
printf '{"hostId":"%s","workspaces":["/workspace/project-a","/workspace/project-b"]}\n' \
  "$HOST_ID" > /tmp/mail-ready.json

exec sleep infinity
