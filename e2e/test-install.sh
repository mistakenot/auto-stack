#!/usr/bin/env bash
#
# E2E test: install auto-stack from GitHub releases into a fresh Linux container.
# Simulates a new user running the install script from the README.
#
# Usage:
#   ./e2e/test-install.sh          # test latest release
#   ./e2e/test-install.sh v0.1.0   # test a specific tag
#
set -euo pipefail

TAG="${1:-}"
IMAGE="ubuntu:24.04"
CONTAINER_NAME="autostack-install-test-$$"
REPO="mistakenot/auto-stack"
INSTALL_URL="https://raw.githubusercontent.com/${REPO}/main/install.sh"
BINARIES="autodoc autoetl autosearch autowatch"

cleanup() {
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "=== auto-stack install e2e test ==="
echo "Image:  $IMAGE"

# If a tag was given, verify the release exists before spinning up a container.
if [ -n "$TAG" ]; then
    echo "Tag:    $TAG"
    if ! gh release view "$TAG" --repo "$REPO" >/dev/null 2>&1; then
        echo "FAIL: release $TAG not found" >&2
        exit 1
    fi
else
    echo "Tag:    (latest)"
fi

# Start a fresh container.
echo ""
echo "--- starting container ---"
docker run -d --name "$CONTAINER_NAME" "$IMAGE" sleep 300 >/dev/null
docker exec "$CONTAINER_NAME" apt-get update -qq >/dev/null
docker exec "$CONTAINER_NAME" apt-get install -y -qq curl ca-certificates >/dev/null 2>&1

# Run the install script exactly as a new user would.
echo "--- running install.sh ---"
docker exec "$CONTAINER_NAME" bash -c "curl -fsSL '$INSTALL_URL' | bash"

# Validate each binary is installed, executable, and runs.
echo ""
echo "--- validating binaries ---"
FAIL=0
for bin in $BINARIES; do
    BIN_PATH="/root/.local/bin/$bin"

    # Exists and is executable?
    if ! docker exec "$CONTAINER_NAME" test -x "$BIN_PATH"; then
        echo "FAIL: $bin not found or not executable at $BIN_PATH"
        FAIL=1
        continue
    fi

    # Runs without crashing? (--help or no-args should exit 0 or 1, not 127/139)
    EXIT_CODE=0
    docker exec "$CONTAINER_NAME" "$BIN_PATH" --help >/dev/null 2>&1 || EXIT_CODE=$?
    if [ "$EXIT_CODE" -ge 126 ]; then
        echo "FAIL: $bin exited with $EXIT_CODE (crash or not found)"
        FAIL=1
        continue
    fi

    echo "  OK: $bin"
done

echo ""
if [ "$FAIL" -ne 0 ]; then
    echo "=== FAILED ==="
    exit 1
fi
echo "=== PASSED ==="
