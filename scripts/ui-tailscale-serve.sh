#!/usr/bin/env bash
#
# Launch auto-ui locally on a well-known port and expose it over Tailscale Serve
# (tailnet-only, HTTPS) so you can examine it from any device on your tailnet.
#
# Usage:
#   scripts/ui-tailscale-serve.sh           # build + serve on port 8723
#   PORT=9090 scripts/ui-tailscale-serve.sh # override the port
#   DEV=1 scripts/ui-tailscale-serve.sh      # live-from-disk assets (-tags dev)
#
# Ctrl-C tears down both the local server and the Tailscale Serve mapping.
#
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# Default to an uncommon port so it doesn't collide with the usual 8080/3000/etc.
PORT="${PORT:-8723}"
DEV="${DEV:-0}"
BIN="bin/auto"

# --- Preflight: tailscale reachable + writable -------------------------------
# `tailscale serve` writes config, which needs root or a one-time operator grant
# (`sudo tailscale set --operator=$USER`). Read-only `serve status` works without
# it, so check reachability here and let the write below surface any auth error.
if ! command -v tailscale >/dev/null 2>&1; then
  echo "error: tailscale CLI not found on PATH" >&2
  exit 1
fi
if ! tailscale serve status >/dev/null 2>&1; then
  echo "error: cannot reach tailscaled — is Tailscale running and are you logged in?" >&2
  exit 1
fi

# --- Build ---------------------------------------------------------------
# Dev mode serves web/static/ live from disk (needs the `dev` build tag and the
# binary run from the auto-ui module root). Prod embeds the assets.
if [[ "$DEV" == "1" ]]; then
  echo "==> Building auto (dev assets, -tags dev)…" >&2
  (cd auto-cli && go build -tags dev -o "../$BIN" ./cmd/auto)
  SERVE_DIR="auto-ui"
else
  echo "==> Building auto…" >&2
  make build >/dev/null
  SERVE_DIR="."
fi

BIN_ABS="$(pwd)/$BIN"

# --- Cleanup -------------------------------------------------------------
SERVER_PID=""
cleanup() {
  echo "" >&2
  echo "==> Tearing down Tailscale Serve mapping for port $PORT…" >&2
  tailscale serve --bg --https="$PORT" off 2>/dev/null || true
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "==> Stopping auto-ui (pid $SERVER_PID)…" >&2
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
}
trap cleanup INT TERM EXIT

# --- Launch local server -------------------------------------------------
echo "==> Starting auto-ui on http://localhost:$PORT …" >&2
(cd "$SERVE_DIR" && exec "$BIN_ABS" ui serve --port "$PORT") &
SERVER_PID=$!

# Wait for the local server to accept connections before wiring Tailscale.
for _ in $(seq 1 50); do
  if curl -fsS -o /dev/null "http://localhost:$PORT/api/hello" 2>/dev/null; then
    break
  fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "error: auto-ui exited before becoming ready" >&2
    exit 1
  fi
  sleep 0.2
done

# --- Tailscale Serve -----------------------------------------------------
echo "==> Pointing tailscale serve at localhost:$PORT …" >&2
if ! tailscale serve --bg --https="$PORT" "http://localhost:$PORT"; then
  echo "" >&2
  echo "error: tailscale serve failed (often a permissions issue)." >&2
  echo "       Grant operator access once with:  sudo tailscale set --operator=\$USER" >&2
  exit 1
fi

DNSNAME="$(tailscale status --json 2>/dev/null \
  | grep -m1 '"DNSName"' | sed 's/.*: *"//; s/\.".*//; s/\.$//')"
echo "" >&2
echo "    auto-ui is live at: https://${DNSNAME:-<your-node>}:$PORT" >&2
echo "    (tailnet-only — reachable from any device on your tailnet)" >&2
echo "" >&2
echo "    Press Ctrl-C to stop and remove the serve mapping." >&2

# Block until the server exits (or Ctrl-C fires the trap).
wait "$SERVER_PID"
