#!/bin/bash
set -euo pipefail

echo "==> Caching sudo credentials upfront..."
sudo -v
while true; do sudo -n true; sleep 60; kill -0 "$$" || exit; done 2>/dev/null &

# Helper: append to file only if line not already present
append_once() {
  grep -qxF "$1" "$2" || echo "$1" >> "$2"
}

# ── Rootless Docker ──────────────────────────────────────────────────────────
echo "==> Setting up rootless Docker..."
if ! command -v dockerd-rootless-setuptool.sh &>/dev/null; then
  echo "    dockerd-rootless-setuptool.sh not found, skipping"
elif ! command -v systemctl &>/dev/null; then
  echo "    systemctl not found, skipping"
elif ! systemctl --user show-environment &>/dev/null; then
  echo "    user systemd session unavailable, skipping"
else
  if ! systemctl --user is-enabled docker &>/dev/null; then
    dockerd-rootless-setuptool.sh install
  else
    echo "    rootless Docker already installed, skipping"
  fi
  systemctl --user enable docker
  systemctl --user start docker
  append_once 'export DOCKER_HOST=unix:///run/user/$(id -u)/docker.sock' ~/.profile
fi

# ── gh CLI ───────────────────────────────────────────────────────────────────
echo "==> Installing gh CLI..."
if ! command -v gh &>/dev/null; then
  type -p wget >/dev/null || sudo apt install wget -y
  sudo mkdir -p -m 755 /etc/apt/keyrings
  wget -qO- https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg > /dev/null
  sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null
  sudo apt update
  sudo apt install gh -y
else
  echo "    gh already installed, skipping"
fi

# ── nvm + Node LTS + pm2 ─────────────────────────────────────────────────────
echo "==> Installing nvm..."
if [ ! -d "$HOME/.nvm" ]; then
  NVM_VERSION=$(curl -s https://api.github.com/repos/nvm-sh/nvm/releases/latest | grep '"tag_name"' | cut -d'"' -f4)
  curl -o- "https://raw.githubusercontent.com/nvm-sh/nvm/${NVM_VERSION}/install.sh" | bash
else
  echo "    nvm already installed, skipping"
fi

echo "==> Installing Node LTS and pm2..."
export NVM_DIR="$HOME/.nvm"
\. "$NVM_DIR/nvm.sh"

if ! nvm ls --no-colors | grep -q 'lts/\*\|lts/' 2>/dev/null; then
  nvm install --lts
fi
nvm alias default 'lts/*'
nvm use default

if ! npm list -g pm2 --depth=0 &>/dev/null; then
  npm install -g pm2
else
  echo "    pm2 already installed, skipping"
fi

# ── ntm ──────────────────────────────────────────────────────────────────────
echo "==> Installing ntm..."
if ! command -v ntm &>/dev/null; then
  curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/ntm/main/install.sh | bash
else
  echo "    ntm already installed, skipping"
fi

# Add bash shell integration
append_once 'eval "$(ntm init bash)"' ~/.profile

echo "==> Configuring ntm projects base..."
if command -v ntm &>/dev/null; then
  ntm config set projects-base ~/src
else
  echo "    ntm not found, skipping config"
fi

echo "==> Configuring tmux mode keys..."
if command -v tmux &>/dev/null; then
  tmux start-server >/dev/null 2>&1 || true
  if tmux set-window-option -g mode-keys vi >/dev/null 2>&1; then
    echo "    tmux mode-keys set to vi"
  else
    echo "    unable to set tmux mode-keys, skipping"
  fi
else
  echo "    tmux not found, skipping"
fi

echo "==> Creating post-install handoff template..."
if [ ! -f "$HOME/post_install.md" ]; then
  cat > "$HOME/post_install.md" <<'EOF'
# Post-Install Steps (Coding Agent)

Use this file as the checklist for all manual and follow-up steps after `setup-vps.sh` completes.

## Host

- Hostname:
- Public IP:
- Provider:
- Date:

## Required Authentication

- [ ] Authenticate Claude CLI if needed
- [ ] Authenticate Codex CLI if needed
- [ ] Authenticate Gemini CLI if needed
- [ ] Authenticate OpenCode CLI if needed
- [ ] Authenticate Agent Browser CLI if needed

## Claude Post-Auth Settings

After Claude Code is initialized and authenticated, update `~/.claude/settings.json`:

```json
{
  "alwaysThinkingEnabled": true,
  "cleanupPeriodDays": 9999999,
  "skipDangerousModePermissionPrompt": true,
  "env": {
    "CLAUDE_BASH_MAINTAIN_PROJECT_WORKING_DIR": "1",
    "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1"
  }
}
```

- [ ] Apply the settings above in `~/.claude/settings.json`

## Environment Validation

- [ ] Verify `docker`, `gh`, `gcloud`, `claude`, `codex`, `gemini`, `opencode`, `agent-browser`, `ntm`, `br`
- [ ] Verify `ntm config get projects_base` points to `~/src`
- [ ] Verify `DOCKER_HOST` is set correctly for rootless Docker (if enabled)

## Project Bootstrap

- [ ] Clone required repos into `~/src`
- [ ] Run project-specific bootstrap commands
- [ ] Confirm agent config files are present (`AGENTS.md` / `CLAUDE.md`)

## Notes For Next Agent

- Open items:
- Blockers:
- Follow-up tasks:
EOF
else
  echo "    post_install.md already exists, skipping"
fi

# ── beads_rust (br) ──────────────────────────────────────────────────────────
echo "==> Installing beads_rust (br)..."
if ! command -v br &>/dev/null; then
  curl -fsSL "https://raw.githubusercontent.com/Dicklesworthstone/beads_rust/main/install.sh?$(date +%s)" | bash
else
  echo "    br already installed, skipping"
fi

# ── Google Cloud CLI ─────────────────────────────────────────────────────────
echo "==> Installing Google Cloud CLI..."
if ! command -v gcloud &>/dev/null; then
  sudo apt-get install -y apt-transport-https ca-certificates gnupg
  curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg \
    | gpg --dearmor \
    | sudo tee /usr/share/keyrings/cloud.google.gpg > /dev/null
  echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" \
    | sudo tee /etc/apt/sources.list.d/google-cloud-sdk.list > /dev/null
  sudo apt-get update
  sudo apt-get install -y google-cloud-cli
else
  echo "    gcloud already installed, skipping"
fi

echo ""
echo "Done. Re-login or 'source ~/.profile' for all PATH changes to take effect."
echo "Then run: gcloud init"

# ── Claude Code ──────────────────────────────────────────────────────────────
echo "==> Installing Claude Code..."
if ! command -v claude &>/dev/null; then
  curl -fsSL https://claude.ai/install.sh | bash
else
  echo "    claude already installed, skipping"
fi

# ── Codex CLI ────────────────────────────────────────────────────────────────
echo "==> Installing Codex CLI..."
if ! npm list -g @openai/codex --depth=0 &>/dev/null; then
  npm install -g @openai/codex
else
  echo "    codex already installed, skipping"
fi

# ── Gemini CLI ───────────────────────────────────────────────────────────────
echo "==> Installing Gemini CLI..."
if ! npm list -g @google/gemini-cli --depth=0 &>/dev/null; then
  npm install -g @google/gemini-cli
else
  echo "    gemini already installed, skipping"
fi

# ── OpenCode CLI ─────────────────────────────────────────────────────────────
echo "==> Installing OpenCode CLI..."
if ! npm list -g opencode-ai --depth=0 &>/dev/null; then
  npm install -g opencode-ai
else
  echo "    opencode already installed, skipping"
fi

# ── Agent Browser CLI ────────────────────────────────────────────────────────
echo "==> Installing Agent Browser CLI..."
if ! npm list -g agent-browser --depth=0 &>/dev/null; then
  npm install -g agent-browser
else
  echo "    agent-browser already installed, skipping"
fi

echo "==> Installing Agent Browser runtime..."
if command -v agent-browser &>/dev/null; then
  agent-browser install
else
  echo "    agent-browser not found, skipping runtime install"
fi
