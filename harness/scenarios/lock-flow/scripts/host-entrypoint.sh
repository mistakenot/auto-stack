#!/bin/sh
# Seed ONE host id, register ONE project workspace on it, wire the hooks that
# enforce locks, and opt the project in with one lock Group. Two Workers on one
# host = two AUTO_LOCK_WORKER values against one ~/.auto, not two containers
# (D-11).
set -e

: "${HOST_ID:?HOST_ID must be set}"

mkdir -p "$HOME/.auto"
printf '{"hostId":"%s"}\n' "$HOST_ID" > "$HOME/.auto/host.json"

# One workspace, on main, with the lockable files present so an Edit payload
# names something real. It is the main checkout on purpose: with no tmux pane
# and no AUTO_LOCK_WORKER the Worker here is *bare* (D-6), which is one of the
# cases the tests exercise.
ws=/workspace/project
mkdir -p "$ws/db/schema" "$ws/db/migrations" "$ws/src"
cd "$ws"
git init -q
printf 'export const users = {};\n' > db/schema/users.ts
printf -- '-- 0001 init\n' > db/migrations/0001_init.sql
printf 'export const app = {};\n' > src/app.ts
git add -A
git commit -q -m init

# Register the project; the lock store keys locks on the registered id.
auto init --project

# Wire `auto hooks fire` onto every Claude/Codex event in the project-local
# config. The PreToolUse entry is the ONE thing that makes a lock enforceable
# (D-13): without it the guard never runs.
auto hooks install

# Opt in. init scaffolds an example group and reports whether the enforcing
# hook is installed — gate on that report, because a stand-up whose hook is
# missing would make every deny assertion below fail for the wrong reason.
auto lock init --project | grep -q '"claude_hook_installed": true'

# Replace the scaffold with the scenario's real Group. The description is what
# a blocked agent is shown, so the tests assert on it verbatim.
cat > .auto/lock/settings.json <<'EOF'
{
  "identity": "auto",
  "groups": [
    {
      "name": "drizzle-schema",
      "globs": ["db/schema/**", "db/migrations/**"],
      "description": "Ordered migrations clash if two branches change the schema in parallel."
    }
  ]
}
EOF

# The config must validate and the whole setup must be enforceable before any
# test runs: doctor exits non-zero on a failing check. (The bare-identity row
# is a warning here, not a failure — AUTO_LOCK_WORKER is set per invocation.)
auto lock doctor > /dev/null

# Fail-fast: the ready file is the healthcheck's gate, and it is written only
# once every gate above has succeeded (set -e aborts otherwise).
printf '{"hostId":"%s","workspace":"%s","group":"drizzle-schema"}\n' \
  "$HOST_ID" "$ws" > /tmp/lock-ready.json

exec sleep infinity
