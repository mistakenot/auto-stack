#!/bin/sh
# Register each agent's autowatch RPC backend (the agents are already healthy via
# depends_on, so the default connectivity verify in `backends add` succeeds and
# learns each backend's host id), then serve auto-ui with the debug ring enabled
# — /api/debug/recent is the harness's assertion surface (D-3).
set -e

mkdir -p "$HOME/.auto"
printf '{"hostId":"auto-ui"}\n' > "$HOME/.auto/host.json"

# AGENTS is a space-separated list of agent service names (e.g. "agent-1 agent-2").
: "${AGENTS:?AGENTS must be set (space-separated agent service names)}"
for a in $AGENTS; do
  auto ui backends add "tcp://$a:7788" --name "$a"
done

export AUTO_UI_DEBUG=1
exec auto ui serve --port 8080
