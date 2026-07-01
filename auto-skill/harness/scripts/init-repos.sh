#!/bin/sh
set -e

BARE_PATH="/repos/skills.git"

git config --global --add safe.directory "$BARE_PATH"
git config --global user.email "fixture@harness.local"
git config --global user.name "Fixture Setup"
git config --global init.defaultBranch main

if [ ! -f "$BARE_PATH/HEAD" ]; then
    echo "Initializing bare repo at $BARE_PATH..."
    git init --bare "$BARE_PATH"

    WORK=$(mktemp -d)
    cd "$WORK"
    git init
    git remote add origin "$BARE_PATH"

    mkdir -p skills
    if [ -d /fixtures/skills ]; then
        cp -r /fixtures/skills/* skills/ 2>/dev/null || true
    fi

    git add -A
    git commit -m "Initial fixture skills" --allow-empty
    git push origin HEAD:main
    rm -rf "$WORK"

    # Enable smart HTTP serving
    git -C "$BARE_PATH" config http.receivepack true
    cp "$BARE_PATH/hooks/post-update.sample" "$BARE_PATH/hooks/post-update" 2>/dev/null || true
    chmod +x "$BARE_PATH/hooks/post-update" 2>/dev/null || true
    cd "$BARE_PATH" && git update-server-info

    echo "Bare repo seeded."
else
    echo "Bare repo already exists."
fi

# Share the CA cert so SUT can trust it
cp /certs/ca.crt /shared-certs/ca.crt 2>/dev/null || true

echo "Starting fcgiwrap + nginx (HTTPS)..."
spawn-fcgi -s /run/fcgiwrap.sock -U nginx -G nginx -- /usr/bin/fcgiwrap
exec nginx -g 'daemon off;'
