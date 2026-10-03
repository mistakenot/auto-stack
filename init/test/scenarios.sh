#!/usr/bin/env bash
#
# Runs INSIDE the test container (see run.sh). Exercises init/bootstrap.sh
# end to end and asserts on the resulting project. Keeps going after a failed
# check so one run reports every problem; exits non-zero if any check failed.
#
# Scenarios:
#   1. fresh    — empty directory → fully bootstrapped project
#   2. rerun    — bootstrap again on the fresh project → nothing changes
#   3. env      — auto env renders a distinct compose project + ports per worktree
#   4. existing — a repo already set up by "someone else" → their files survive,
#                 the gaps are filled, and a rerun is again a no-op
#
set -uo pipefail

BOOTSTRAP=/init/bootstrap.sh
export PATH="$HOME/.local/bin:$PATH"
LOG_DIR=$(mktemp -d)

PASS=0
FAIL=0
FAILED=()

section() { printf '\n=== %s\n' "$*"; }
pass() { PASS=$((PASS + 1)); printf '  PASS  %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAILED+=("$1"); printf '  FAIL  %s\n' "$1"; }

# check "description" cmd args... — passes when the command exits 0.
check() {
    local desc="$1"; shift
    if "$@" >/dev/null 2>&1; then pass "$desc"; else fail "$desc"; fi
}

# check_sh "description" 'shell snippet' — for pipelines and compound tests.
check_sh() {
    local desc="$1" snippet="$2"
    if bash -c "$snippet" >/dev/null 2>&1; then pass "$desc"; else fail "$desc"; fi
}

# Fingerprint of everything a rerun could change: every non-ignored file
# (content hash, or link target for symlinks), HEAD, hooks and the age key.
snapshot() {
    (
        cd "$1" || exit 1
        git ls-files -co --exclude-standard | sort | while IFS= read -r f; do
            if [ -L "$f" ]; then printf 'L %s -> %s\n' "$f" "$(readlink "$f")"
            else printf 'F %s %s\n' "$f" "$(sha256sum < "$f" | cut -c1-16)"; fi
        done
        printf 'HEAD %s\n' "$(git rev-parse HEAD 2>/dev/null)"
        printf 'HOOK %s\n' "$(sha256sum < .git/hooks/prepare-commit-msg 2>/dev/null | cut -c1-16)"
        printf 'KEY %s\n' "$(sha256sum < "$HOME/.config/sops/age/keys.txt" | cut -c1-16)"
        printf 'REGISTRY %s\n' "$(jq -c --arg p "$1" '[.projects[] | select(.path == $p) | .id]' "$HOME/.auto/projects.json")"
    )
}

# run_bootstrap "description" LOGNAME args... — runs bootstrap as a check,
# keeping its log in $LOG_DIR/LOGNAME.log and printing it on failure. The script
# is fed on stdin, exactly like the README's `curl ... | bash` one-liner.
run_bootstrap() {
    local desc="$1" name="$2"; shift 2
    if bash -s -- "$@" <"$BOOTSTRAP" >"$LOG_DIR/$name.log" 2>&1; then
        pass "$desc"
    else
        fail "$desc"
        sed 's/^/  | /' "$LOG_DIR/$name.log"
    fi
}

if [ "${LOCAL_AUTO:-0}" = 1 ]; then
    mkdir -p "$HOME/.local/bin"
    install -m 0755 /local-auto "$HOME/.local/bin/auto"
    echo "pre-installed local auto build: $(auto --version)"
fi

###############################################################################
section "1. fresh: bootstrap an empty directory"
###############################################################################

# Upper case + dot: exercises project-id slugging (-> fresh-project).
P="$HOME/Fresh.Project"
run_bootstrap "bootstrap exits 0" fresh --dir "$P"
cd "$P" || { echo "project dir missing — aborting"; exit 1; }

# tools
for t in auto sops age age-keygen br; do
    check "$t installed in ~/.local/bin" test -x "$HOME/.local/bin/$t"
done
check "auto runs" auto --version
check "sops runs" sops --version

# age + sops
KEY="$HOME/.config/sops/age/keys.txt"
check "age key generated at the sops default location" test -s "$KEY"
check_sh "age key is private (0600)" "[ \"\$(stat -c %a '$KEY')\" = 600 ]"
PUBKEY=$(age-keygen -y "$KEY" 2>/dev/null)
check ".sops.yaml exists" test -f .sops.yaml
check ".sops.yaml points at the local age key" grep -qF "$PUBKEY" .sops.yaml
check "secrets.yaml exists" test -f secrets.yaml
check "secrets.yaml is sops-encrypted" grep -q 'ENC\[' secrets.yaml
check_sh "secrets.yaml has no plaintext value" "! grep -q 'example-value' '$P/secrets.yaml'"
check_sh "secrets.yaml decrypts to the test key" \
    "cd '$P' && [ \"\$(sops -d --extract '[\"example_key\"]' secrets.yaml)\" = example-value ]"

# git
check "git repository initialised" git rev-parse --is-inside-work-tree
check_sh "branch is main" "cd '$P' && [ \"\$(git branch --show-current)\" = main ]"
check_sh "initial commit created" "cd '$P' && [ \"\$(git rev-list --count HEAD)\" = 1 ]"
check_sh "working tree clean after bootstrap" "cd '$P' && [ -z \"\$(git status --porcelain)\" ]"
check_sh "secrets.yaml is committed (encrypted)" "cd '$P' && git ls-files --error-unmatch secrets.yaml"

# agent files
check "AGENTS.md is a regular file" test -f AGENTS.md -a ! -L AGENTS.md
for f in CLAUDE.md GEMINI.md; do
    check_sh "$f is a symlink to AGENTS.md" "cd '$P' && [ -L $f ] && [ \"\$(readlink $f)\" = AGENTS.md ]"
done
check "AGENTS.md has the auto doc index" grep -q 'autodoc: start' AGENTS.md
check "AGENTS.md mentions auto skill" grep -q 'auto skill' AGENTS.md
check "AGENTS.md mentions auto env" grep -q 'auto env' AGENTS.md
check "AGENTS.md doc index lists docs/README.md" grep -q 'docs/README.md' AGENTS.md

# docs
check "docs/ exists" test -d docs
check "docs/README.md exists" test -f docs/README.md
check ".auto/doc/settings.json exists" test -f .auto/doc/settings.json
check_sh "auto doc doctor: all checks pass" \
    "cd '$P' && auto doc doctor --json | jq -e 'all(.[]; .status == \"pass\")'"
check "auto doc stale: nothing stale" auto doc stale

# skills
check ".auto/skills/skills.yaml exists" test -f .auto/skills/skills.yaml
check_sh "skills.yaml targets claude + agents" \
    "cd '$P' && grep -q -- '- claude' .auto/skills/skills.yaml && grep -q -- '- agents' .auto/skills/skills.yaml"
check_sh "auto skill doctor: ok" "cd '$P' && auto skill doctor | jq -e '.ok == true'"
check "github.com approved for skill fetching" grep -q 'https://github.com:443' "$HOME/.auto/skills/trust.json"
for sk in new-task new-solution new-plan execute-task complete-task; do
    check "planning-workflow skill $sk rendered for claude" test -f ".claude/skills/$sk/SKILL.md"
    check "planning-workflow skill $sk rendered for agents" test -f ".agents/skills/$sk/SKILL.md"
done
check "skills.yaml records planning-workflow skills" grep -q 'new-task:' .auto/skills/skills.yaml

# beads
check ".beads/ initialised" test -f .beads/config.yaml
check_sh "beads state is committed" "cd '$P' && git ls-files --error-unmatch .beads/config.yaml"
# Probe in a throwaway copy so the project itself stays untouched.
cp -r "$P" "$LOG_DIR/beads-probe"
check_sh "beads issue ids use the project id as prefix" \
    "cd '$LOG_DIR/beads-probe' && br create --title probe --type task >/dev/null && br list --json | grep -q '\"fresh-project-'"

# auto watch
check_sh "project registered in ~/.auto/projects.json" \
    "jq -e --arg p '$P' 'any(.projects[]; .path == \$p)' \$HOME/.auto/projects.json"
check_sh "registered with slugged id fresh-project" \
    "jq -e --arg p '$P' 'any(.projects[]; .path == \$p and .id == \"fresh-project\")' \$HOME/.auto/projects.json"
check ".auto/watch/project.json exists" test -f .auto/watch/project.json
check "host config exists" test -f "$HOME/.auto/host.json"
check "no-systemd daemon install is a warning, not a failure" grep -q 'systemd --user is not available' "$LOG_DIR/fresh.log"

# hooks
check_sh ".claude/settings.json wires auto hooks fire" "grep -q 'auto hooks fire' '$P/.claude/settings.json'"
check "Session-Id prepare-commit-msg hook installed" grep -q Session-Id .git/hooks/prepare-commit-msg

# scripts + compose
check "scripts/ exists" test -d scripts
check "scripts/dev-services.sh is executable" test -x scripts/dev-services.sh
check_sh "scripts/dev-services.sh is tracked" "cd '$P' && git ls-files --error-unmatch scripts/dev-services.sh"
check "compose.yaml exists" test -f compose.yaml
check "compose.yaml is valid" docker compose -f compose.yaml config -q
check "compose.yaml has a commented postgres example" grep -q '^#   postgres:' compose.yaml

# auto env
check_sh ".auto/env/config.json has up/down commands" \
    "cd '$P' && jq -e '(.up_command | length > 0) and (.down_command | length > 0)' .auto/env/config.json"
check ".auto/env/files/.env template exists" test -f .auto/env/files/.env
check_sh "env up_command routes through scripts/dev-services.sh" \
    "cd '$P' && jq -e '.up_command == \"sh scripts/dev-services.sh up\"' .auto/env/config.json"
printf 'services: [\n' > "$P/broken.yaml"
check_sh "dev-services up fails loudly on a broken compose file" \
    "cd '$P' && ! COMPOSE_FILE=broken.yaml sh scripts/dev-services.sh up"
rm -f "$P/broken.yaml"
check_sh "env template is committed (worktrees need it)" "cd '$P' && git ls-files --error-unmatch .auto/env/files/.env"
check ".gitignore ignores the rendered /.env" grep -qxF '/.env' .gitignore

###############################################################################
section "2. rerun: bootstrap the same project again"
###############################################################################

BEFORE=$(snapshot "$P")
run_bootstrap "second bootstrap exits 0" rerun --dir "$P"
AFTER=$(snapshot "$P")
if [ "$BEFORE" = "$AFTER" ]; then
    pass "rerun changed nothing (files, symlinks, HEAD, hooks, age key, registry)"
else
    fail "rerun changed nothing (files, symlinks, HEAD, hooks, age key, registry)"
    diff <(echo "$BEFORE") <(echo "$AFTER") | sed 's/^/  | /'
fi
check_sh "working tree still clean" "cd '$P' && [ -z \"\$(git status --porcelain)\" ]"
check "rerun reports skipped steps" grep -q 'skip  age key exists' "$LOG_DIR/rerun.log"

###############################################################################
section "3. env: per-worktree compose environments"
###############################################################################

cd "$P" || exit 1
check "auto env up (main, no services) succeeds" auto env up
check_sh "main .env: compose project fresh-project-main" "grep -qx 'COMPOSE_PROJECT_NAME=fresh-project-main' '$P/.env'"
check_sh "main .env: postgres port allocated" "grep -qE '^POSTGRES_PORT=[0-9]+$' '$P/.env'"
check_sh "rendered .env is gitignored" "cd '$P' && [ -z \"\$(git status --porcelain)\" ]"

WT="$HOME/fresh-project-feature"
check "git worktree add" git worktree add -q "$WT" -b feature/login
cd "$WT" || exit 1
check "auto env up (worktree) succeeds" auto env up
check_sh "worktree .env: compose project fresh-project-feature-login" \
    "grep -qx 'COMPOSE_PROJECT_NAME=fresh-project-feature-login' '$WT/.env'"
check_sh "worktree postgres port differs from main" \
    "[ \"\$(grep POSTGRES_PORT '$WT/.env')\" != \"\$(grep POSTGRES_PORT '$P/.env')\" ]"
# Uncomment the postgres example and put its password in secrets.yaml: the
# resolved config must carry the worktree's port and the decrypted secret.
sed -n '/^# services:/,/^#   postgres-data:/p' compose.yaml | sed 's/^# \{0,1\}//' > compose.pg.yaml
cp secrets.yaml "$LOG_DIR/secrets.yaml.bak"
check "set POSTGRES_PASSWORD in secrets.yaml" sops set secrets.yaml '["POSTGRES_PASSWORD"]' '"s3cret-pw"'
COMPOSE_FILE=compose.pg.yaml sh scripts/dev-services.sh config >"$LOG_DIR/pg-config.yaml" 2>&1
check_sh "postgres example binds the worktree port" \
    "grep -q \"published: \\\"\$(sed -n 's/^POSTGRES_PORT=//p' '$WT/.env')\\\"\" '$LOG_DIR/pg-config.yaml'"
check "secret from secrets.yaml reaches compose via sops exec-env" grep -q 'POSTGRES_PASSWORD: s3cret-pw' "$LOG_DIR/pg-config.yaml"
check_sh "decrypted secret never written into the worktree" "! grep -rq s3cret-pw '$WT' --exclude-dir=.git"
rm -f compose.pg.yaml && cp "$LOG_DIR/secrets.yaml.bak" secrets.yaml
check "auto env down (worktree)" auto env down
check "worktree .env removed by env down" test ! -e "$WT/.env"
cd "$P" && check "auto env down (main)" auto env down

###############################################################################
section "4. existing: a repo already set up by someone else"
###############################################################################

E="$HOME/existing-project"
mkdir -p "$E/docs" "$E/scripts"
cd "$E" || exit 1
git init -q -b main
printf '# Legacy\n\nLEGACY CLAUDE INSTRUCTIONS\n' > CLAUDE.md
printf '# Gemini\n\nDIFFERENT GEMINI INSTRUCTIONS\n' > GEMINI.md
printf 'node_modules/\n*.log\n' > .gitignore
printf '# Architecture\n\nhand-written doc\n' > docs/architecture.md
printf '#!/bin/sh\necho deploy\n' > scripts/deploy.sh
printf 'services:\n  redis:\n    image: redis:7\n' > docker-compose.yml
# Secrets owned by a teammate whose key we do NOT have.
TEAMMATE_KEY="$LOG_DIR/teammate.txt"
age-keygen -o "$TEAMMATE_KEY" 2>/dev/null
TEAMMATE_PUB=$(age-keygen -y "$TEAMMATE_KEY")
printf 'creation_rules:\n  - path_regex: secrets\\.yaml$\n    age: %s\n' "$TEAMMATE_PUB" > .sops.yaml
printf 'db_password: hunter2\n' > secrets.yaml
SOPS_AGE_KEY_FILE="$TEAMMATE_KEY" sops -e -i secrets.yaml
git add -A && git commit -qm "existing project"
printf '#!/bin/sh\necho custom-hook\n' > .git/hooks/prepare-commit-msg
chmod +x .git/hooks/prepare-commit-msg

# Remember the files bootstrap must not touch.
declare -A ORIG
for f in .sops.yaml secrets.yaml docker-compose.yml docs/architecture.md scripts/deploy.sh .git/hooks/prepare-commit-msg; do
    ORIG[$f]=$(sha256sum < "$f")
done
HEAD_BEFORE=$(git rev-parse HEAD)

run_bootstrap "bootstrap exits 0 on an existing project" existing --dir "$E"

for f in "${!ORIG[@]}"; do
    check_sh "$f untouched" "cd '$E' && [ \"\$(sha256sum < '$f')\" = '${ORIG[$f]}' ]"
done
check_sh "no commit made in an existing repo" "cd '$E' && [ \"\$(git rev-parse HEAD)\" = '$HEAD_BEFORE' ]"
check "existing CLAUDE.md content moved into AGENTS.md" grep -q 'LEGACY CLAUDE INSTRUCTIONS' AGENTS.md
check_sh "CLAUDE.md now a symlink to AGENTS.md" "cd '$E' && [ \"\$(readlink CLAUDE.md)\" = AGENTS.md ]"
check "differing GEMINI.md content preserved" grep -q 'DIFFERENT GEMINI INSTRUCTIONS' GEMINI.md
check_sh "differing GEMINI.md kept as a regular file" "cd '$E' && [ -f GEMINI.md ] && [ ! -L GEMINI.md ]"
check "warned about differing GEMINI.md" grep -q 'GEMINI.md exists and differs' "$LOG_DIR/existing.log"
check "warned that our key is not in .sops.yaml" grep -q 'does not list your age key' "$LOG_DIR/existing.log"
check "warned that secrets.yaml is not decryptable" grep -q 'cannot decrypt secrets.yaml' "$LOG_DIR/existing.log"
check "warned about foreign prepare-commit-msg hook" grep -q 'prepare-commit-msg already exists' "$LOG_DIR/existing.log"
check "no compose.yaml created alongside docker-compose.yml" test ! -e compose.yaml
check_sh "compose autodetects the existing docker-compose.yml" "cd '$E' && docker compose config --services | grep -qx redis"
check "scripts/dev-services.sh added next to existing scripts" test -x scripts/dev-services.sh
check_sh "dev-services up refuses when secrets.yaml can't be decrypted" \
    "cd '$E' && ! sh scripts/dev-services.sh up 2>'$LOG_DIR/existing-up.err' && grep -q 'cannot decrypt secrets.yaml' '$LOG_DIR/existing-up.err'"
check ".beads/ initialised in existing project" test -f .beads/config.yaml
check "planning-workflow skills installed in existing project" test -f .claude/skills/new-task/SKILL.md
check "no docs/README.md seeded into a non-empty docs/" test ! -e docs/README.md
check "existing .gitignore lines kept" grep -qxF 'node_modules/' .gitignore
check ".gitignore gained /.env" grep -qxF '/.env' .gitignore
check ".auto/skills/skills.yaml created" test -f .auto/skills/skills.yaml
check_sh "project registered" "jq -e --arg p '$E' 'any(.projects[]; .path == \$p)' \$HOME/.auto/projects.json"

git add -A && git commit -qm "bootstrap" >/dev/null 2>&1
BEFORE=$(snapshot "$E")
run_bootstrap "second bootstrap on existing project exits 0" existing-rerun --dir "$E"
AFTER=$(snapshot "$E")
if [ "$BEFORE" = "$AFTER" ]; then
    pass "rerun on existing project changed nothing"
else
    fail "rerun on existing project changed nothing"
    diff <(echo "$BEFORE") <(echo "$AFTER") | sed 's/^/  | /'
fi

###############################################################################
printf '\n=== %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
    printf 'Failed checks:\n'
    printf '  - %s\n' "${FAILED[@]}"
    printf 'Bootstrap logs: %s (use run.sh --shell to inspect)\n' "$LOG_DIR"
    exit 1
fi
