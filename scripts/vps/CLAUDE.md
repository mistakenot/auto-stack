`setup-vps.sh` takes a brand-new Ubuntu server and installs the baseline requirements for this stack. It requires `sudo`, is idempotent, and installs Claude Code, Codex CLI, Gemini CLI, OpenCode CLI, Agent Browser CLI, and supporting tooling we use frequently.

## What was added recently

- OpenCode CLI install is done via npm package `opencode-ai` (binary: `opencode`).
- Agent Browser CLI install is done via npm package `agent-browser` (binary: `agent-browser`), followed by `agent-browser install`.
- After ntm install, script runs `ntm config set projects-base ~/src` to normalize workspace location.
- Script also runs `tmux set-window-option -g mode-keys vi` (when `tmux` is available).
- Script creates `~/post_install.md` as a handoff template for coding-agent post-install tasks (only if missing).
- `post_install.md` includes a manual Claude post-auth `~/.claude/settings.json` block with required defaults.
- Rootless Docker setup now skips cleanly when `systemctl --user` or `dockerd-rootless-setuptool.sh` are unavailable (common in Docker containers).
- Google Cloud CLI apt key is written in dearmored keyring format (`gpg --dearmor`) so `apt-get update` does not fail with missing pubkey errors.

## Fast Docker test (Ubuntu 24.04)

Use this when validating script changes without touching a real VPS:

```bash
docker run --rm \
  -v "$(pwd)/scripts/vps/setup-vps.sh:/tmp/setup-vps.sh:ro" \
  ubuntu:24.04 bash -lc '
    set -euo pipefail
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y sudo curl ca-certificates git wget gnupg
    useradd -m -s /bin/bash tester
    echo "tester ALL=(ALL) NOPASSWD:ALL" >/etc/sudoers.d/tester
    chmod 440 /etc/sudoers.d/tester
    su - tester -c "bash /tmp/setup-vps.sh"
  '
```

## Useful verification checks

After script execution in the container:

```bash
su - tester -c '
  export NVM_DIR="$HOME/.nvm"
  . "$NVM_DIR/nvm.sh"
  command -v opencode
  opencode --version
  command -v agent-browser
  agent-browser --version
'
```

Expected behavior in container tests:

- Rootless Docker section prints a skip message instead of failing.
- `opencode` resolves from the active Node/npm global bin path.
- `agent-browser` resolves from the active Node/npm global bin path.
- Script exits `0` if all installs complete.
