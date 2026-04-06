#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="$HOME/.local/bin"

echo "Building autodoc..."
cd "$SCRIPT_DIR"
go build -o bin/autodoc ./cmd/autodoc/

mkdir -p "$INSTALL_DIR"
cp bin/autodoc "$INSTALL_DIR/autodoc"
chmod +x "$INSTALL_DIR/autodoc"

echo "Installed autodoc to $INSTALL_DIR/autodoc"
