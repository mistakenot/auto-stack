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
    # Remove before writing — a running binary (e.g. the auto watch daemon) keeps
    # its old inode open, so the delete succeeds and the new file gets a fresh inode.
    if [ -f "${INSTALL_DIR}/${bin}" ]; then
        # fuser lists every process with this file mapped — including the parent
        # `auto update` process that is running this script (its own executable
        # IS ${INSTALL_DIR}/auto). Drop this script's parent PID so we only flag a
        # genuinely separate long-running user, e.g. the auto watch daemon.
        in_use=$(fuser "${INSTALL_DIR}/${bin}" 2>/dev/null | tr ' ' '\n' \
            | sed 's/[^0-9]//g' | grep -vx "$PPID" | grep -v '^$' || true)
        if [ -n "$in_use" ]; then
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

# Restart any services that were running during upgrade. The merged `auto`
# binary backs the `auto watch` daemon (systemd unit autowatch.service).
#
# Prefer the no-sudo user daemon: if a `systemctl --user` unit is active we can
# restart it ourselves (the default `auto watch daemon install` is user scope).
# Otherwise fall back to a printed hint — a system unit needs sudo, which this
# user-level installer does not have.
if [ -n "$RESTART_SERVICES" ]; then
    echo ""
    echo "The auto binary was running during install (likely the 'auto watch' daemon)."
    # is-active --quiet under `if` is safe: a non-zero exit just skips the branch.
    if command -v systemctl >/dev/null 2>&1 \
        && systemctl --user is-active --quiet autowatch.service; then
        # The binary is already replaced, so the restart MUST NOT abort the
        # script under `set -euo pipefail`. A failed restart degrades to the
        # manual-restart hint instead of failing `auto update`.
        if systemctl --user restart autowatch.service; then
            echo "  restarted auto watch (user) daemon"
        else
            echo "Could not restart the user daemon — restart it yourself:"
            echo "  systemctl --user restart autowatch.service"
            echo "  # or: auto watch daemon restart"
        fi
    else
        # No active user unit: it may be a system-mode unit (needs sudo, which
        # this user-level installer lacks) or not installed yet.
        echo "Restart it to pick up the new binary:"
        echo "  auto watch daemon restart                  # user daemon (no sudo)"
        echo "  sudo systemctl restart autowatch.service   # if managed by system systemd"
        echo ""
        echo "Tip: 'auto watch daemon install' runs without sudo by default (user unit)."
        echo "     System mode needs a sudo-reachable binary, e.g.:"
        echo "       sudo \"\$(command -v auto)\" watch daemon install --system"
        echo "       # or symlink once: sudo ln -sf \"\$HOME/.local/bin/auto\" /usr/local/bin/auto"
    fi
fi
