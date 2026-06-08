#!/usr/bin/env bash
set -euo pipefail

REPO="mistakenot/auto-stack"
INSTALL_DIR="$HOME/.local/bin"
BINARIES="auto"

# Detect platform
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
esac

SUFFIX="${OS}-${ARCH}"

# Validate platform
case "$SUFFIX" in
    linux-amd64|darwin-arm64) ;;
    *) echo "Unsupported platform: $SUFFIX (supported: linux-amd64, darwin-arm64)" && exit 1 ;;
esac

# Get latest release tag
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
if [ -z "$TAG" ]; then
    echo "Failed to fetch latest release"
    exit 1
fi

echo "Installing auto-stack ${TAG} for ${SUFFIX}..."
mkdir -p "$INSTALL_DIR"

RESTART_SERVICES=""

for bin in $BINARIES; do
    URL="https://github.com/${REPO}/releases/download/${TAG}/${bin}-${SUFFIX}"
    echo "  ${bin}..."
    # Remove before writing — a running binary (e.g. autowatch daemon) keeps its
    # old inode open, so the delete succeeds and the new file gets a fresh inode.
    if [ -f "${INSTALL_DIR}/${bin}" ]; then
        if fuser "${INSTALL_DIR}/${bin}" >/dev/null 2>&1; then
            RESTART_SERVICES="${RESTART_SERVICES} ${bin}"
        fi
        rm -f "${INSTALL_DIR}/${bin}"
    fi
    curl -fsSL "$URL" -o "${INSTALL_DIR}/${bin}"
    chmod +x "${INSTALL_DIR}/${bin}"
done

echo ""
echo "Installed to ${INSTALL_DIR}:"
for bin in $BINARIES; do
    echo "  ${INSTALL_DIR}/${bin}"
done

# Check PATH
if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
    echo ""
    echo "Add ${INSTALL_DIR} to your PATH:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
fi

# Hint to restart any services that were running during upgrade. The merged
# `auto` binary backs the `auto watch` daemon (systemd unit autowatch.service).
if [ -n "$RESTART_SERVICES" ]; then
    echo ""
    echo "The auto binary was running during install (likely the 'auto watch' daemon)."
    echo "Restart it to pick up the new binary:"
    echo "  sudo systemctl restart autowatch.service   # if managed by systemd"
    echo "  # or: auto watch daemon restart"
fi
