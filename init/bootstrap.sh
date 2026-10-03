#!/usr/bin/env bash
#
# bootstrap.sh — set up a new (or existing) project with the auto coding stack.
#
# Stack/language agnostic. Idempotent: every step checks before it acts, and
# anything already present (written by you, a teammate, or a previous run) is
# left alone — the script warns instead of overwriting.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/mistakenot/auto-stack/main/init/bootstrap.sh | bash
#   bootstrap.sh [--dir DIR] [--skill-module NAME]... [--no-commit] [--no-daemon]
#
# What it does (each step is skipped when already done):
#   1.  install `auto` (latest GitHub release), plus pinned `sops`, `age` and `br`
#       (beads) into ~/.local/bin
#   2.  git init (branch main) if DIR is not already inside a git work tree
#   3.  generate the default age key if missing; write .sops.yaml pointing at it;
#       create a sops-encrypted secrets.yaml with one example key
#   4.  AGENTS.md at the root; CLAUDE.md and GEMINI.md as symlinks to it
#   5.  docs/ with `auto doc` initialised (global + project) and a seed README
#   6.  `auto skill` initialised (global + project, targets claude,agents) and the
#       mistakenot/skills modules installed (default: planning-workflow)
#   7.  beads issue tracking (`br init`)
#   8.  host config + project registered with the `auto watch` daemon registry;
#       user daemon installed when systemd --user is available
#   9.  agent hooks (`auto hooks install`) + Session-Id commit trailer hook
#   10. scripts/ folder
#   11. compose.yaml with a commented-out postgres example
#   12. `auto env` wired to compose: each worktree gets its own compose project
#       name and ports via a generated, gitignored ./.env, and services run under
#       `sops exec-env secrets.yaml` (scripts/dev-services.sh)
#   13. initial commit — only when this run created the repository
#
# Flags:
#   --dir DIR             project directory (default: current directory; created if missing)
#   --skill-module NAME   mistakenot/skills module to install (repeatable; default:
#                         planning-workflow). Pass `--skill-module none` to skip.
#   --no-commit           never create the initial commit
#   --no-daemon           don't install the auto watch systemd user daemon
#
set -euo pipefail

# The whole script runs inside one { ... } block, so bash reads all of it before
# executing anything. Without this, under `curl | bash` any command that reads
# stdin would swallow the rest of the script.
{

# --- pinned tool versions (bump deliberately; checksums are SHA-256) ---------

SOPS_VERSION="v3.13.3"
AGE_VERSION="v1.3.2"
BR_VERSION="0.7.4"
SKILLS_INSTALL_URL="${SKILLS_INSTALL_URL:-https://raw.githubusercontent.com/mistakenot/skills/main/install.sh}"
AUTO_INSTALL_URL="${AUTO_INSTALL_URL:-https://raw.githubusercontent.com/mistakenot/auto-stack/main/install.sh}"
BIN_DIR="$HOME/.local/bin"

sops_sha256() {
    case "$1" in
        linux-amd64)  echo e5bec3346a873ae91d871550f3e698c1aad962aff462a080e40f25fde17fef6b ;;
        linux-arm64)  echo 53b0abacd38ef1b12a66d6c100956691b9cefce018d91f81e73ddf7438b94d77 ;;
        darwin-amd64) echo 42162d5cef10b74fcf80a045a70e658d7ce6e63d6ea1be6f347e44015714468d ;;
        darwin-arm64) echo b97c0d434aab577dc40310e8d22ff9e45eef4c80638ab978daae9b4681c59286 ;;
    esac
}

age_sha256() {
    case "$1" in
        linux-amd64)  echo cbe24006683f8eb669266162894b9a522a1af52f2665fbc63a4bb032ed26ac10 ;;
        linux-arm64)  echo 6b8dc4333c53a5a57c9e5834e3a48f92605d7154014cd07269ff3327db5d37f4 ;;
        darwin-amd64) echo 1d1e4bc66e1427edad7739ae7616157de0e79db8b6d2a1497d7d9925fb06a539 ;;
        darwin-arm64) echo e2020b073c44f692685a24d6abc378817eb81ffaaf49fd0531ef8565f767f2f5 ;;
    esac
}

br_sha256() {
    case "$1" in
        linux-amd64)  echo 5263fa20f988588b88320856e7a1086505d28a2456628b85cc8f6ec51e0af732 ;;
        linux-arm64)  echo 082d02cda1919d6b1587c0a932f5842fa510a7a18d80db0d2a85988959c4ae5b ;;
        darwin-amd64) echo 38d2691fcf4921af6d1b4772b8c45c7c8c6dfdbea3b432064fb2ce6a71936e6e ;;
        darwin-arm64) echo 4e7619b919c0f720d0d520c51ce3b1c730387293cb6fe159e1f1cba8512ffad7 ;;
    esac
}

# --- output helpers ------------------------------------------------------------

WARNINGS=()

step() { printf '\n==> %s\n' "$*" >&2; }
ok()   { printf '    ok    %s\n' "$*" >&2; }
skip() { printf '    skip  %s\n' "$*" >&2; }
warn() { printf '    WARN  %s\n' "$*" >&2; WARNINGS+=("$*"); }
die()  { printf '\nERROR: %s\n' "$*" >&2; exit 1; }

# Run an auto subcommand quietly; show its output only when it fails.
run_auto() {
    local out
    if ! out=$(auto "$@" 2>&1); then
        printf '%s\n' "$out" >&2
        die "\`auto $*\` failed (output above). Fix the issue and re-run bootstrap — completed steps are skipped."
    fi
}

# Append a line to a file unless an identical line is already there.
ensure_line() {
    local file="$1" line="$2"
    touch "$file"
    if ! grep -qxF -- "$line" "$file"; then
        # Make sure we start on a fresh line.
        if [ -s "$file" ] && [ -n "$(tail -c1 "$file")" ]; then printf '\n' >> "$file"; fi
        printf '%s\n' "$line" >> "$file"
        ok "$file: added '$line'"
    fi
}

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
    else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

# Download $1 to $2 and verify its SHA-256 against $3.
fetch_verified() {
    curl -fsSL "$1" -o "$2"
    local got
    got=$(sha256_of "$2")
    [ "$got" = "$3" ] || die "checksum mismatch for $1 (expected $3, got $got)"
}

# --- args ----------------------------------------------------------------------

PROJECT_DIR="."
DO_COMMIT=1
DO_DAEMON=1
SKILL_MODULES=()

usage() {
    cat <<'EOF'
Usage: bootstrap.sh [--dir DIR] [--skill-module NAME]... [--no-commit] [--no-daemon]

Set up a new or existing project with the auto coding stack. Idempotent: rerun
any time; existing files are left alone and conflicts are reported as warnings.

  --dir DIR             project directory (default: current directory)
  --skill-module NAME   mistakenot/skills module to install (repeatable;
                        default: planning-workflow; `none` to skip)
  --no-commit           never create the initial commit
  --no-daemon           don't install the auto watch systemd user daemon
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --dir) [ $# -ge 2 ] || die "--dir needs a value"; PROJECT_DIR="$2"; shift 2 ;;
        --dir=*) PROJECT_DIR="${1#--dir=}"; shift ;;
        --no-commit) DO_COMMIT=0; shift ;;
        --no-daemon) DO_DAEMON=0; shift ;;
        --skill-module) [ $# -ge 2 ] || die "--skill-module needs a value"; SKILL_MODULES+=("$2"); shift 2 ;;
        --skill-module=*) SKILL_MODULES+=("${1#--skill-module=}"); shift ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown argument: $1 (run with --help for usage)" ;;
    esac
done
[ ${#SKILL_MODULES[@]} -gt 0 ] || SKILL_MODULES=(planning-workflow)

# --- preflight -----------------------------------------------------------------

step "Preflight"

for cmd in curl git tar; do
    command -v "$cmd" >/dev/null 2>&1 || die "'$cmd' is required but not installed — install it with your package manager and re-run."
done

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    arm64|aarch64) ARCH=arm64 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
esac
PLATFORM="$OS-$ARCH"
case "$PLATFORM" in
    linux-amd64|linux-arm64|darwin-amd64|darwin-arm64) ;;
    *) die "unsupported platform: $PLATFORM" ;;
esac

mkdir -p "$BIN_DIR" "$PROJECT_DIR"
PROJECT_DIR=$(cd "$PROJECT_DIR" && pwd)
cd "$PROJECT_DIR"
PATH_HAD_BIN=1
case ":$PATH:" in *":$BIN_DIR:"*) ;; *) PATH_HAD_BIN=0; export PATH="$BIN_DIR:$PATH" ;; esac

PROJECT_NAME=$(basename "$PROJECT_DIR")
# One id for the auto project registry and the compose project name prefix.
# Must match the registry's ^[a-z0-9]+(-[a-z0-9]+)*$ (also a valid compose name).
PROJECT_ID=$(printf '%s' "$PROJECT_NAME" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]\{1,\}/-/g; s/^-//; s/-$//')
[ -n "$PROJECT_ID" ] || PROJECT_ID=project
ok "project: $PROJECT_DIR (id $PROJECT_ID, $PLATFORM)"

# --- 1. tools ------------------------------------------------------------------

step "Install tools"

if command -v auto >/dev/null 2>&1; then
    skip "auto already installed ($(auto --version 2>/dev/null || echo unknown)) — run \`auto update\` to upgrade"
else
    curl -fsSL "$AUTO_INSTALL_URL" | bash >&2
    command -v auto >/dev/null 2>&1 || die "auto install finished but \`auto\` is not on PATH (expected $BIN_DIR/auto)"
    ok "auto $(auto --version)"
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

if command -v age >/dev/null 2>&1 && command -v age-keygen >/dev/null 2>&1; then
    skip "age already installed ($(age --version))"
else
    fetch_verified "https://github.com/FiloSottile/age/releases/download/${AGE_VERSION}/age-${AGE_VERSION}-${PLATFORM}.tar.gz" \
        "$TMP/age.tgz" "$(age_sha256 "$PLATFORM")"
    tar -xzf "$TMP/age.tgz" -C "$TMP"
    install -m 0755 "$TMP/age/age" "$TMP/age/age-keygen" "$BIN_DIR/"
    ok "age $AGE_VERSION -> $BIN_DIR"
fi

if command -v sops >/dev/null 2>&1; then
    skip "sops already installed ($(sops --version 2>/dev/null | head -1))"
else
    SOPS_OS_ARCH="${OS}.${ARCH}"
    fetch_verified "https://github.com/getsops/sops/releases/download/${SOPS_VERSION}/sops-${SOPS_VERSION}.${SOPS_OS_ARCH}" \
        "$TMP/sops" "$(sops_sha256 "$PLATFORM")"
    install -m 0755 "$TMP/sops" "$BIN_DIR/sops"
    ok "sops $SOPS_VERSION -> $BIN_DIR"
fi

if command -v br >/dev/null 2>&1; then
    skip "br already installed ($(br --version 2>/dev/null))"
else
    # musl builds on Linux: static, so they run on any distro.
    case "$PLATFORM" in
        linux-*) BR_ASSET="linux_musl_${ARCH}" ;;
        *) BR_ASSET="${OS}_${ARCH}" ;;
    esac
    fetch_verified "https://github.com/Dicklesworthstone/beads_rust/releases/download/v${BR_VERSION}/br-${BR_VERSION}-${BR_ASSET}.tar.gz" \
        "$TMP/br.tgz" "$(br_sha256 "$PLATFORM")"
    mkdir -p "$TMP/br" && tar -xzf "$TMP/br.tgz" -C "$TMP/br"
    install -m 0755 "$TMP/br/br" "$BIN_DIR/br"
    ok "br $BR_VERSION -> $BIN_DIR"
fi

# --- 2. git --------------------------------------------------------------------

step "Git repository"

CREATED_REPO=0
if git -C "$PROJECT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    skip "already a git repository ($(git rev-parse --show-toplevel))"
    [ "$(git rev-parse --show-toplevel)" = "$PROJECT_DIR" ] \
        || warn "$PROJECT_DIR is a subdirectory of the repo at $(git rev-parse --show-toplevel); files are created here, not at the repo root"
else
    git init -q -b main
    CREATED_REPO=1
    ok "git init (branch main)"
fi

# --- 3. secrets (age + sops) ----------------------------------------------------

step "Secrets (age + sops)"

# Same default location sops itself searches.
if [ -n "${SOPS_AGE_KEY_FILE:-}" ]; then
    AGE_KEY_FILE="$SOPS_AGE_KEY_FILE"
elif [ "$OS" = darwin ] && [ -z "${XDG_CONFIG_HOME:-}" ]; then
    AGE_KEY_FILE="$HOME/Library/Application Support/sops/age/keys.txt"
else
    AGE_KEY_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/sops/age/keys.txt"
fi

if [ -s "$AGE_KEY_FILE" ]; then
    skip "age key exists: $AGE_KEY_FILE"
else
    mkdir -p "$(dirname "$AGE_KEY_FILE")"
    chmod 700 "$(dirname "$AGE_KEY_FILE")"
    (umask 077 && age-keygen -o "$AGE_KEY_FILE" 2>/dev/null)
    ok "generated age key: $AGE_KEY_FILE (back this up — losing it means losing access to secrets)"
fi
AGE_PUBKEYS=$(age-keygen -y "$AGE_KEY_FILE")
AGE_PUBKEY=$(printf '%s\n' "$AGE_PUBKEYS" | head -1)
ok "age public key: $AGE_PUBKEY"

key_in_sops_config() {
    local k
    while IFS= read -r k; do
        [ -n "$k" ] && grep -qF -- "$k" .sops.yaml && return 0
    done <<< "$AGE_PUBKEYS"
    return 1
}

if [ -f .sops.yaml ]; then
    skip ".sops.yaml exists — not modified"
    key_in_sops_config || warn ".sops.yaml does not list your age key. Ask a teammate to add $AGE_PUBKEY to .sops.yaml and run \`sops updatekeys secrets.yaml\`."
else
    cat > .sops.yaml <<EOF
# sops configuration — which keys encrypt which files.
# To give a teammate access: append their age public key (comma-separated) to
# \`age\` below, then run \`sops updatekeys <file>\` for each secrets file.
creation_rules:
  - path_regex: (^|/)secrets(\.[^/]+)?\.(ya?ml|json|env)$
    age: >-
      $AGE_PUBKEY
EOF
    ok ".sops.yaml -> $AGE_PUBKEY"
fi

if [ -f secrets.yaml ]; then
    skip "secrets.yaml exists — not modified"
    if ! sops --decrypt secrets.yaml >/dev/null 2>&1; then
        warn "cannot decrypt secrets.yaml with your age key — ask a teammate to add $AGE_PUBKEY to .sops.yaml and run \`sops updatekeys secrets.yaml\`"
    fi
elif ! key_in_sops_config; then
    warn "not creating secrets.yaml: your age key is not a recipient in .sops.yaml (see warning above)"
else
    printf 'example_key: example-value\n' > secrets.yaml
    if sops --encrypt --in-place secrets.yaml 2>"$TMP/sops.err"; then
        ok "secrets.yaml (encrypted; edit with \`sops secrets.yaml\`)"
    else
        rm -f secrets.yaml
        warn "could not encrypt secrets.yaml: $(cat "$TMP/sops.err") — check .sops.yaml creation_rules match secrets.yaml"
    fi
fi

# --- 4. agent instruction files --------------------------------------------------

step "AGENTS.md (+ CLAUDE.md, GEMINI.md symlinks)"

if [ -e AGENTS.md ] || [ -L AGENTS.md ]; then
    skip "AGENTS.md exists — not modified"
elif [ -f CLAUDE.md ] && [ ! -L CLAUDE.md ]; then
    # Converge an existing Claude-only setup: its content becomes the shared file.
    mv CLAUDE.md AGENTS.md
    ok "moved existing CLAUDE.md to AGENTS.md (content preserved; CLAUDE.md becomes a symlink)"
else
    cat > AGENTS.md <<EOF
# $PROJECT_NAME

Instructions for coding agents working in this project. \`CLAUDE.md\` and
\`GEMINI.md\` are symlinks to this file — edit \`AGENTS.md\` only.

## Layout

- \`docs/\` — project documentation, managed by \`auto doc\` (frontmatter + freshness hashes).
- \`scripts/\` — project scripts (setup, maintenance, one-offs).
- \`compose.yaml\` — local development services (Docker Compose).
- \`secrets.yaml\` — secrets, encrypted with sops + age. Never commit plaintext secrets.

## Dev environment

- \`auto env up\` / \`auto env down\` start and stop this worktree's services. Each git
  worktree gets its own compose project name and ports, rendered from
  \`.auto/env/files/.env\` into \`./.env\` (generated and gitignored — don't edit it).
- Services run under \`sops exec-env secrets.yaml\`, so secrets are available to the
  compose file as \`\${VAR}\` without a plaintext file. Inspect the resolved config with
  \`scripts/dev-services.sh config\`.

## Issues

- Work is tracked with beads (\`br\`): \`br ready\`, \`br create\`, \`br update <id> --status=in_progress\`,
  \`br close <id>\`. Run \`br sync --flush-only\` before committing so \`.beads/\` is up to date.

## Secrets

- Read: \`sops -d secrets.yaml\`. Edit: \`sops secrets.yaml\`.
- Run a command with secrets as env vars: \`sops exec-env secrets.yaml '<command>'\`.
- Grant access: add the person's age public key to \`.sops.yaml\`, then \`sops updatekeys secrets.yaml\`.
EOF
    ok "created AGENTS.md"
fi

for f in CLAUDE.md GEMINI.md; do
    if [ -L "$f" ] && [ "$(readlink "$f")" = AGENTS.md ]; then
        skip "$f -> AGENTS.md"
    elif [ ! -e "$f" ] && [ ! -L "$f" ]; then
        ln -s AGENTS.md "$f"
        ok "$f -> AGENTS.md"
    elif [ -f "$f" ] && [ ! -L "$f" ] && cmp -s "$f" AGENTS.md; then
        ln -sf AGENTS.md "$f"
        ok "$f was identical to AGENTS.md; replaced with symlink"
    else
        warn "$f exists and differs from AGENTS.md — left as is. Merge it into AGENTS.md, then \`ln -sf AGENTS.md $f\`."
    fi
done

# --- 5. docs ---------------------------------------------------------------------

step "Docs (auto doc)"

run_auto doc init
run_auto doc init --project
ok "auto doc initialised (global + project)"

if [ -z "$(ls -A docs 2>/dev/null)" ]; then
    cat > docs/README.md <<'EOF'
---
title: "Documentation"
summary: "Entry point and conventions for this project's documentation."
read_when: "adding a new doc or looking for where documentation lives"
---

# Documentation

Project docs live in this folder and are managed by `auto doc`.

- Every doc starts with frontmatter: `title`, `summary`, and `read_when`.
- After editing a doc, run `auto doc fixed <path>` to refresh its freshness hash.
- `auto doc stale` lists docs whose content or linked code changed; `auto doc fix` explains how to resolve them.
- `auto doc agents` refreshes the doc index embedded in `AGENTS.md`.
EOF
    run_auto doc fixed docs/README.md
    ok "docs/README.md"
else
    skip "docs/ already has content"
fi

# --- 6. skills -------------------------------------------------------------------

step "Skills (auto skill)"

run_auto skill init -y
run_auto skill init --project --target claude,agents -y
ok "auto skill initialised (global + project, targets claude,agents)"

if [ "${SKILL_MODULES[*]}" = none ]; then
    skip "skill modules (--skill-module none)"
else
    # Host-wide approval to fetch skills from GitHub; a no-op once approved.
    run_auto skill trust add https://github.com:443
    for module in "${SKILL_MODULES[@]}"; do
        # The mistakenot/skills installer maps a module to its skills and runs
        # `auto skill add`; re-adding already-installed skills changes nothing.
        if curl -fsSL "$SKILLS_INSTALL_URL" | bash -s -- --module "$module" >"$TMP/skills.log" 2>&1; then
            ok "skill module $module (mistakenot/skills)"
        else
            cat "$TMP/skills.log" >&2
            die "installing skill module '$module' failed (output above). Re-run bootstrap once fixed."
        fi
    done
fi

# --- 7. beads ------------------------------------------------------------------------

step "Issue tracking (beads)"

if [ -d .beads ]; then
    skip ".beads/ exists"
else
    br init --prefix "$PROJECT_ID" --quiet </dev/null
    ok "br init (issue prefix $PROJECT_ID)"
fi

# --- 8. auto watch registration ----------------------------------------------------

step "Register with auto watch"

run_auto init
# An id already recorded in .auto/watch/project.json takes precedence over --project-id.
run_auto watch init --project-id "$PROJECT_ID"
ok "project registered in ~/.auto/projects.json (the daemon picks it up on its next tick)"

if [ "$DO_DAEMON" -eq 0 ]; then
    skip "daemon install (--no-daemon)"
elif [ -f "$HOME/.config/systemd/user/autowatch.service" ] || [ -f /etc/systemd/system/autowatch.service ]; then
    skip "auto watch daemon already installed"
elif command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
    if auto watch daemon install >"$TMP/daemon.log" 2>&1; then
        ok "auto watch daemon installed (systemd user unit)"
    else
        warn "auto watch daemon install failed: $(tail -3 "$TMP/daemon.log" | tr '\n' ' ') — run \`auto watch doctor\`"
    fi
else
    warn "systemd --user is not available, so the auto watch daemon was not installed. Run \`auto watch start\` in the foreground, or \`sudo \"\$(command -v auto)\" watch daemon install --system\`."
fi

# --- 9. hooks ----------------------------------------------------------------------

step "Agent + git hooks"

run_auto hooks install
ok "agent hooks wired (.claude/settings.json, .codex/hooks.json)"

HOOK_DIR=$(git rev-parse --git-path hooks)
if [ -f "$HOOK_DIR/prepare-commit-msg" ]; then
    if grep -q 'Session-Id' "$HOOK_DIR/prepare-commit-msg"; then
        skip "Session-Id commit trailer hook already installed"
    else
        warn "$HOOK_DIR/prepare-commit-msg already exists (not ours) — Session-Id trailers not installed. Merge it with \`auto config init --project\`'s hook by hand if you want commit↔session linkage."
    fi
else
    run_auto config init --project
    ok "Session-Id commit trailer hook (links commits to agent sessions)"
fi

# --- 10. scripts ----------------------------------------------------------------------

step "scripts/"

if [ -d scripts ]; then
    skip "scripts/ exists"
else
    mkdir scripts
    ok "scripts/"
fi

if [ -f scripts/dev-services.sh ]; then
    skip "scripts/dev-services.sh exists — not modified"
else
    cat > scripts/dev-services.sh <<'EOF'
#!/bin/sh
# Start, stop or inspect this worktree's Docker Compose services.
#
#   scripts/dev-services.sh up|down|config
#
# `auto env up` / `auto env down` call this (see .auto/env/config.json) after
# rendering ./.env, which carries this worktree's COMPOSE_PROJECT_NAME and ports.
# When secrets.yaml exists, compose runs under `sops exec-env`, so its keys are
# available to the compose file as ${VAR} — no plaintext secrets on disk.
set -eu
cd "$(dirname "$0")/.."

compose() {
    if [ -f secrets.yaml ]; then
        sops exec-env secrets.yaml "docker compose $*"
    else
        docker compose "$@"
    fi
}

action="${1:-}"
case "$action" in
    up|config)
        if [ -f secrets.yaml ] && ! sops --decrypt secrets.yaml >/dev/null 2>&1; then
            echo "cannot decrypt secrets.yaml with your age key — ask a teammate to add your public key to .sops.yaml and run \`sops updatekeys secrets.yaml\`" >&2
            exit 1
        fi
        # Compose refuses `up` with zero services; a broken file or missing docker still fails here.
        services=$(compose config --services)
        if [ -z "$services" ]; then
            echo "the compose file defines no services; nothing to $action"
            exit 0
        fi
        if [ "$action" = up ]; then compose up -d --wait; else compose config; fi
        ;;
    down)
        # Stopping needs no secrets, so it works even when secrets.yaml can't be decrypted.
        services=$(docker compose config --services 2>/dev/null) || services=unknown
        if [ -z "$services" ]; then
            echo "the compose file defines no services; nothing to stop"
            exit 0
        fi
        docker compose down --remove-orphans
        ;;
    *)
        echo "usage: $0 up|down|config" >&2
        exit 2
        ;;
esac
EOF
    chmod +x scripts/dev-services.sh
    ok "scripts/dev-services.sh (compose up/down under sops exec-env)"
fi

# --- 11. docker compose -----------------------------------------------------------------

step "Docker Compose"

EXISTING_COMPOSE=""
for f in compose.yaml compose.yml docker-compose.yaml docker-compose.yml; do
    [ -f "$f" ] && { EXISTING_COMPOSE="$f"; break; }
done

if [ -n "$EXISTING_COMPOSE" ]; then
    skip "$EXISTING_COMPOSE exists — not modified"
else
    cat > compose.yaml <<'EOF'
# Local development services.
#
# Run via `auto env up` / `auto env down`, not `docker compose` directly: auto env
# renders ./.env first, which gives every git worktree its own compose project
# name (COMPOSE_PROJECT_NAME) and host ports, so parallel worktrees don't collide.
# Port variables come from .auto/env/files/.env — add a line there for each new
# service port, e.g. REDIS_PORT={{.Port.redis}}. Keys in secrets.yaml (sops) are
# available as ${VAR} too, via scripts/dev-services.sh.
#
# Example — replace `services: {}` with:
#
# services:
#   postgres:
#     image: postgres:17-alpine
#     environment:
#       POSTGRES_USER: postgres
#       POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-postgres}   # set it in secrets.yaml
#       POSTGRES_DB: app
#     ports:
#       - "${POSTGRES_PORT:-5432}:5432"
#     volumes:
#       - postgres-data:/var/lib/postgresql/data
#     healthcheck:
#       test: ["CMD-SHELL", "pg_isready -U postgres"]
#       interval: 2s
#       retries: 30
#
# volumes:
#   postgres-data:

services: {}
EOF
    ok "compose.yaml (postgres example commented out)"
fi

command -v docker >/dev/null 2>&1 || warn "docker is not installed — \`auto env up\` needs docker + the compose plugin"

# --- 12. auto env ------------------------------------------------------------------------

step "Per-worktree environments (auto env)"

if [ -f .auto/env/config.json ]; then
    skip ".auto/env exists"
else
    run_auto env init
    ok "auto env init"
fi

# Only fill in config.json when it is still the empty skeleton `auto env init` writes.
if grep -q '"up_command": ""' .auto/env/config.json && grep -q '"down_command": ""' .auto/env/config.json \
    && [ "$(grep -c . .auto/env/config.json)" -le 4 ]; then
    cat > .auto/env/config.json <<'EOF'
{
  "up_command": "sh scripts/dev-services.sh up",
  "down_command": "sh scripts/dev-services.sh down",
  "port_base": 20000,
  "port_stride": 100
}
EOF
    ok ".auto/env/config.json -> scripts/dev-services.sh"
else
    skip ".auto/env/config.json already configured — not modified"
fi

mkdir -p .auto/env/files
if [ -f .auto/env/files/.env ]; then
    skip ".auto/env/files/.env template exists"
else
    cat > .auto/env/files/.env <<EOF
# Rendered by \`auto env up\` into ./.env — do not edit the rendered copy.
# Docker Compose reads ./.env automatically, so each worktree gets its own
# project name (containers, networks, volumes) and its own ports.
COMPOSE_PROJECT_NAME=${PROJECT_ID}-{{.BranchSlug}}
POSTGRES_PORT={{.Port.postgres}}
EOF
    ok ".auto/env/files/.env template (COMPOSE_PROJECT_NAME + POSTGRES_PORT per worktree)"
fi

# The rendered ./.env is generated per worktree and must not be committed.
if git ls-files --error-unmatch .env >/dev/null 2>&1; then
    warn ".env is tracked in git, but auto env generates it per worktree — move its contents into secrets.yaml or the template, then \`git rm --cached .env\`"
elif [ -f .env ] && ! grep -qxF '.env' .auto/env/.generated 2>/dev/null; then
    warn "./.env exists and was not generated by auto env — \`auto env up\` will refuse to overwrite it. Move its contents into .auto/env/files/.env or secrets.yaml."
fi
ensure_line .gitignore "/.env"

run_auto env agents
ok "auto env registered in AGENTS.md"

# --- finalise -------------------------------------------------------------------------------

step "Finalise"

run_auto doc agents
run_auto doc search reindex
ok "doc index refreshed in AGENTS.md"

if [ "$CREATED_REPO" -eq 1 ] && [ "$DO_COMMIT" -eq 1 ]; then
    if git config user.name >/dev/null && git config user.email >/dev/null; then
        git add -A
        git commit -q -m "chore: bootstrap project with the auto stack"
        ok "initial commit $(git rev-parse --short HEAD)"
    else
        warn "git user.name/user.email not set — skipped the initial commit. Configure them, then \`git add -A && git commit\`."
    fi
elif [ -n "$(git status --porcelain)" ]; then
    skip "commit (existing repository) — review with \`git status\` and commit when ready"
fi

printf '\n' >&2
if [ ${#WARNINGS[@]} -gt 0 ]; then
    printf 'Bootstrap finished with %d warning(s):\n' "${#WARNINGS[@]}" >&2
    for w in "${WARNINGS[@]}"; do printf '  - %s\n' "$w" >&2; done
else
    printf 'Bootstrap finished: %s is set up with the auto stack.\n' "$PROJECT_DIR" >&2
fi
if [ "$PATH_HAD_BIN" -eq 0 ]; then
    printf 'Add %s to your PATH:  export PATH="%s:$PATH"\n' "$BIN_DIR" "$BIN_DIR" >&2
fi
exit 0
}
