#!/usr/bin/env bash
#
# AC-7 guard for task 017 (unify binaries into `auto`).
#
# Fails if any shipped, git-tracked string still INVOKES an old per-tool binary
# name (autodoc, autoenv, autoetl, autograph, autoreflect, autosearch, autoskill,
# autoui, autowatch, autoconfig) as a command. After the merge to a single `auto`
# binary, the only correct invocation form is `auto <tool> …`.
#
# A "match" is an old STEM immediately followed by whitespace and a subcommand or
# flag token — i.e. a command invocation. Bare tokens inside identifiers, import
# paths, config-dir stems (~/.auto/<tool>/), `[autodoc()]` data tags, and systemd
# SERVICE-IDENTITY strings are NOT invocations and are allowlisted below.
#
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

STEMS='autodoc|autoenv|autoetl|autograph|autoreflect|autosearch|autoskill|autoui|autowatch|autoconfig'

# Subcommand/flag tokens that, following a stem, mark a real invocation.
SUBCMDS='init|run|search|session|messages?|tree|stale|agents|fix|fixed|graph|quickstart|docs|doctor|update|serve|status|start|stop|task|trigger|logs|health|lookup|rule|create|up|down|code|context|lint|ls|index|stats|skills|clean|describe|get|list|co-?change|daemon'

PATTERN="\\b(${STEMS})[[:space:]]+(${SUBCMDS}|--[a-z])"

# Allowlist of legitimately-retained matches (NOT binary invocations):
#  - doc-index descriptions (auto-generated; every entry carries "Read when:")
#  - `[autodoc()]` data tags
#  - systemd service identity: the unit name, service base/description, the
#    --service-name value, Description= field, and the prose service phrases
#  - the deliberate "post-upgrade" note that names the removed/stale autowatch unit
ALLOW='Read when:|\[autodoc\(|autowatch\.service|defaultServiceBase|defaultDescription|--service-name autowatch|Description=autowatch|the autowatch daemon|autowatch systemd|autowatch daemon is already|stale .*autowatch|removed .*autowatch'

# Scan scope: tracked shipped surface only (built from `git ls-files`).
list_files() {
    {
        git ls-files | grep -E '^(README\.md|CLAUDE\.md|docs/user-journey\.md|docs/autostack-install-daemon\.md|auto-[a-z]+/CLAUDE\.md)$'
        git ls-files | grep -E '^auto-[a-z]+/.*\.go$' | grep -vE '_test\.go$|/cmd/genstats/|/fixturegen/'
        git ls-files | grep -E '(^|/)SKILL\.md$'
    } | sort -u | grep -vE '^docs/tasks/|^\.claude/worktrees/'
}

mapfile -t FILES < <(list_files)

violations=$(printf '%s\n' "${FILES[@]}" | xargs grep -HnE "$PATTERN" 2>/dev/null | grep -vE "$ALLOW" || true)

if [ -n "$violations" ]; then
    echo "ERROR: stale old-binary invocations found — use 'auto <tool> …' instead:" >&2
    echo "$violations" >&2
    echo "" >&2
    echo "If a match is a legitimately-retained reference (systemd service identity," >&2
    echo "import path, config-dir stem, or data tag), extend the allowlist in" >&2
    echo "scripts/check-no-stale-binary-refs.sh." >&2
    exit 1
fi

echo "stale-ref guard: OK (no old-binary invocations in shipped surface)"
