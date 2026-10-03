#!/usr/bin/env bash
#
# E2E test for init/bootstrap.sh: build a clean Ubuntu 24.04 image, run the
# bootstrap inside a throwaway container, then assert on the result (see
# scenarios.sh for what is checked).
#
# Usage:
#   init/test/run.sh           # bootstrap installs `auto` from the latest GitHub release
#   init/test/run.sh --local   # build `auto` from this checkout and pre-install it
#                              # (tests unreleased changes + the "already installed" path)
#   init/test/run.sh --shell   # same as default, but drop into a shell afterwards
#
set -euo pipefail

INIT_DIR=$(cd "$(dirname "$0")/.." && pwd)
REPO_ROOT=$(cd "$INIT_DIR/.." && pwd)
IMAGE="auto-bootstrap-test:ubuntu24.04"
CONTAINER="auto-bootstrap-test-$$"

LOCAL=0
SHELL_AFTER=0
for arg in "$@"; do
    case "$arg" in
        --local) LOCAL=1 ;;
        --shell) SHELL_AFTER=1 ;;
        -h|--help) sed -n '2,/^set -euo/p' "$0" | sed '$d; s/^# \{0,1\}//'; exit 0 ;;
        *) echo "unknown argument: $arg (see --help)" >&2; exit 2 ;;
    esac
done

cleanup() { docker rm -f "$CONTAINER" >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "--- building test image ($IMAGE)"
docker build -q -t "$IMAGE" "$INIT_DIR/test" >/dev/null

MOUNTS=(-v "$INIT_DIR:/init:ro")
if [ "$LOCAL" -eq 1 ]; then
    echo "--- building auto from $REPO_ROOT (linux/amd64, static)"
    make -C "$REPO_ROOT" dist GOOS=linux GOARCH=amd64 >/dev/null
    MOUNTS+=(-v "$REPO_ROOT/dist/auto-linux-amd64:/local-auto:ro")
fi

echo "--- running scenarios in a fresh container"
docker run -d --name "$CONTAINER" "${MOUNTS[@]}" "$IMAGE" sleep infinity >/dev/null

rc=0
docker exec -e LOCAL_AUTO="$LOCAL" "$CONTAINER" bash /init/test/scenarios.sh || rc=$?

if [ "$SHELL_AFTER" -eq 1 ]; then
    echo "--- dropping into the container (exit to clean up)"
    docker exec -it "$CONTAINER" bash || true
fi

exit "$rc"
