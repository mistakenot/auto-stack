#!/usr/bin/env bash
#
# E2E test: install auto-stack from GitHub releases into a fresh Linux container,
# then run init for all tools and validate the resulting directory structure.
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
BIN_DIR="/root/.local/bin"
PROJECT_DIR="/root/src/testproject"

FAIL=0

cleanup() {
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

assert_file() {
    local path="$1"
    local desc="$2"
    if docker exec "$CONTAINER_NAME" test -f "$path"; then
        echo "  OK: $desc ($path)"
    else
        echo "  FAIL: $desc — missing $path"
        FAIL=1
    fi
}

assert_dir() {
    local path="$1"
    local desc="$2"
    if docker exec "$CONTAINER_NAME" test -d "$path"; then
        echo "  OK: $desc ($path)"
    else
        echo "  FAIL: $desc — missing $path"
        FAIL=1
    fi
}

assert_json_field() {
    local path="$1"
    local field="$2"
    local desc="$3"
    if docker exec "$CONTAINER_NAME" python3 -c "
import json, sys
d = json.load(open('$path'))
parts = '$field'.split('.')
for p in parts:
    d = d[p]
" 2>/dev/null; then
        echo "  OK: $desc ($path -> $field)"
    else
        echo "  FAIL: $desc — field '$field' missing in $path"
        FAIL=1
    fi
}

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

# Start a fresh container with git and curl.
echo ""
echo "--- starting container ---"
docker run -d --name "$CONTAINER_NAME" "$IMAGE" sleep 600 >/dev/null
docker exec "$CONTAINER_NAME" bash -c "apt-get update -qq && apt-get install -y -qq curl ca-certificates git python3 >/dev/null 2>&1"

# ============================================================
# Phase 1: Install binaries
# ============================================================
echo "--- running install.sh ---"
docker exec "$CONTAINER_NAME" bash -c "curl -fsSL '$INSTALL_URL' | bash"

echo ""
echo "--- validating binaries ---"
for bin in $BINARIES; do
    BIN_PATH="$BIN_DIR/$bin"

    if ! docker exec "$CONTAINER_NAME" test -x "$BIN_PATH"; then
        echo "  FAIL: $bin not found or not executable at $BIN_PATH"
        FAIL=1
        continue
    fi

    EXIT_CODE=0
    docker exec "$CONTAINER_NAME" "$BIN_PATH" --help >/dev/null 2>&1 || EXIT_CODE=$?
    if [ "$EXIT_CODE" -ge 126 ]; then
        echo "  FAIL: $bin exited with $EXIT_CODE (crash or not found)"
        FAIL=1
        continue
    fi

    echo "  OK: $bin"
done

# ============================================================
# Phase 2: Create a test project and run init
# ============================================================
echo ""
echo "--- setting up test project ---"
docker exec "$CONTAINER_NAME" bash -c "
    mkdir -p $PROJECT_DIR && cd $PROJECT_DIR &&
    git init -q &&
    git config user.email 'test@test.com' &&
    git config user.name 'Test' &&
    touch README.md && git add . && git commit -q -m 'init'
"

# Run global + project init for each tool that supports it.
echo "--- running tool init ---"

# autodoc: global init, then project init
docker exec "$CONTAINER_NAME" bash -c "cd $PROJECT_DIR && $BIN_DIR/autodoc init" 2>&1 | sed 's/^/  [autodoc] /'
docker exec "$CONTAINER_NAME" bash -c "cd $PROJECT_DIR && $BIN_DIR/autodoc init --project" 2>&1 | sed 's/^/  [autodoc] /'

# autosearch: global init only (no project init)
docker exec "$CONTAINER_NAME" bash -c "$BIN_DIR/autosearch init" 2>&1 | sed 's/^/  [autosearch] /'

# autowatch: global init then project init
docker exec "$CONTAINER_NAME" bash -c "cd $PROJECT_DIR && $BIN_DIR/autowatch init" 2>&1 | sed 's/^/  [autowatch] /'

# autoetl: no init command

# ============================================================
# Phase 3: Validate ~/.auto global structure
# ============================================================
echo ""
echo "--- validating ~/.auto (global) ---"

assert_dir  "/root/.auto"                    "global config root"
assert_file "/root/.auto/host.json"          "host identity"
assert_json_field "/root/.auto/host.json" "hostId" "host.json has hostId"

# autodoc global
assert_dir  "/root/.auto/doc"                "autodoc global dir"
assert_file "/root/.auto/doc/settings.json"  "autodoc global settings"

# autosearch global
assert_file "/root/.auto/settings.json"          "shared settings"
assert_json_field "/root/.auto/settings.json" "host" "shared settings has host"
assert_dir  "/root/.auto/search"                  "autosearch global dir"
assert_file "/root/.auto/search/settings.json"    "autosearch settings"
assert_json_field "/root/.auto/search/settings.json" "default_index" "autosearch has default_index"
assert_json_field "/root/.auto/search/settings.json" "default_input" "autosearch has default_input"

# autowatch global
assert_dir  "/root/.auto/watch"                   "autowatch global dir"
assert_file "/root/.auto/watch/settings.json"     "autowatch global settings"
assert_json_field "/root/.auto/watch/settings.json" "projects" "autowatch has projects array"
assert_dir  "/root/.auto/watch/runs"              "autowatch runs dir"

# ============================================================
# Phase 4: Validate project .auto structure
# ============================================================
echo ""
echo "--- validating $PROJECT_DIR/.auto (project) ---"

assert_dir  "$PROJECT_DIR/.auto"                        "project config root"

# autodoc project
assert_dir  "$PROJECT_DIR/.auto/doc"                    "autodoc project dir"
assert_file "$PROJECT_DIR/.auto/doc/settings.json"      "autodoc project settings"
assert_json_field "$PROJECT_DIR/.auto/doc/settings.json" "docsDir" "autodoc has docsDir"
assert_file "$PROJECT_DIR/.auto/doc/.gitignore"         "autodoc project gitignore"
assert_dir  "$PROJECT_DIR/docs"                         "docs directory created"

# autowatch project
assert_dir  "$PROJECT_DIR/.auto/watch"                  "autowatch project dir"
assert_file "$PROJECT_DIR/.auto/watch/project.json"     "autowatch project config"
assert_json_field "$PROJECT_DIR/.auto/watch/project.json" "id" "autowatch project has id"
assert_file "$PROJECT_DIR/.auto/watch/.gitignore"       "autowatch project gitignore"

# ============================================================
# Result
# ============================================================
echo ""
if [ "$FAIL" -ne 0 ]; then
    echo "=== FAILED ==="
    exit 1
fi
echo "=== PASSED ==="
