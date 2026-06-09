#!/usr/bin/env bash
#
# E2E test: run the exact install command from README.md in a fresh
# Linux container and verify that every binary works.
#
# Usage:
#   ./e2e/test-curl-install.sh          # test latest release
#   ./e2e/test-curl-install.sh v0.11.0  # test a specific tag
#
set -euo pipefail

TAG="${1:-}"
IMAGE="ubuntu:24.04"
CONTAINER_NAME="autostack-curl-install-test-$$"
REPO="mistakenot/auto-stack"
INSTALL_URL="https://raw.githubusercontent.com/${REPO}/main/install.sh"
BINARIES="auto"
STEMS="config doc env etl graph reflect search skill ui watch"
OLD_BINARIES="autodoc autoenv autoetl autograph autoreflect autosearch autoskill autoui autowatch autoconfig"
BIN_DIR="/root/.local/bin"

FAIL=0

cleanup() {
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "=== auto-stack curl install e2e test ==="
echo "Image:  $IMAGE"

if [ -n "$TAG" ]; then
    echo "Tag:    $TAG"
    if ! gh release view "$TAG" --repo "$REPO" >/dev/null 2>&1; then
        echo "FAIL: release $TAG not found" >&2
        exit 1
    fi
else
    echo "Tag:    (latest)"
fi

# ============================================================
# Phase 1: Start container with minimal deps (just curl)
# ============================================================
echo ""
echo "--- starting container ---"
docker run -d --name "$CONTAINER_NAME" "$IMAGE" sleep 600 >/dev/null
docker exec "$CONTAINER_NAME" bash -c "apt-get update -qq && apt-get install -y -qq curl ca-certificates >/dev/null 2>&1"

# ============================================================
# Phase 2: Run the exact install command from README.md
# ============================================================
echo ""
echo "--- running: curl -fsSL install.sh | bash ---"
if [ -n "$TAG" ]; then
    # Pin to a specific tag's install script and override the tag lookup
    docker exec "$CONTAINER_NAME" bash -c "
        curl -fsSL 'https://raw.githubusercontent.com/${REPO}/${TAG}/install.sh' \
        | sed 's|TAG=\$(curl.*)|TAG=\"${TAG}\"|' \
        | bash
    "
else
    docker exec "$CONTAINER_NAME" bash -c "curl -fsSL '$INSTALL_URL' | bash"
fi

# ============================================================
# Phase 3: Validate every binary is installed and runs
# ============================================================
echo ""
echo "--- validating binary ---"
AUTO="$BIN_DIR/auto"
if ! docker exec "$CONTAINER_NAME" test -x "$AUTO"; then
    echo "  FAIL: auto not found or not executable at $AUTO"
    FAIL=1
else
    EXIT_CODE=0
    docker exec "$CONTAINER_NAME" "$AUTO" --help >/dev/null 2>&1 || EXIT_CODE=$?
    if [ "$EXIT_CODE" -ge 126 ]; then
        echo "  FAIL: auto --help exited with $EXIT_CODE (crash or not found)"
        FAIL=1
    else
        VERSION=$( docker exec "$CONTAINER_NAME" "$AUTO" --version 2>/dev/null || true )
        echo "  OK: auto ($VERSION)"
    fi
fi

echo "--- validating subcommands (auto <tool> --version) ---"
for stem in $STEMS; do
    EXIT_CODE=0
    docker exec "$CONTAINER_NAME" "$AUTO" "$stem" --help >/dev/null 2>&1 || EXIT_CODE=$?
    if [ "$EXIT_CODE" -ge 126 ]; then
        echo "  FAIL: auto $stem --help exited with $EXIT_CODE"
        FAIL=1
    else
        echo "  OK: auto $stem"
    fi
done

echo "--- asserting old per-tool binaries are NOT installed (AC-3) ---"
for old in $OLD_BINARIES; do
    if docker exec "$CONTAINER_NAME" test -e "$BIN_DIR/$old"; then
        echo "  FAIL: stale binary present: $BIN_DIR/$old"
        FAIL=1
    fi
done
echo "  OK: no old per-tool binaries installed"

# ============================================================
# Phase 4: Validate PATH hint was printed (install script UX)
# ============================================================
echo ""
echo "--- validating install output ---"
INSTALL_OUTPUT=$(docker exec "$CONTAINER_NAME" bash -c "curl -fsSL '$INSTALL_URL' | bash 2>&1" || true)
if echo "$INSTALL_OUTPUT" | grep -q "Installed to"; then
    echo "  OK: install script printed install location"
else
    echo "  FAIL: install script did not print install location"
    FAIL=1
fi

# ============================================================
# Result
# ============================================================
echo ""
if [ "$FAIL" -ne 0 ]; then
    echo "=== FAILED ==="
    exit 1
fi
echo "=== PASSED ==="
