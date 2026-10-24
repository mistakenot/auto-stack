#!/usr/bin/env bash
#
# Remove closed (RESOLVED / REJECTED) review threads from regular docs once a
# review has been resolved, so a README or guide does not carry review history.
#
# Plan files keep their threads — there the thread is the decision log:
#   - anything under a docs/tasks/ folder
#   - pd-* HTML docs (plan.html, epic.html, rich docs)
# Sidecars (*.review.md) are left alone; the caller decides whether to keep them.
# UNRESOLVED threads are always kept: they still need an answer.
#
# Usage: strip-closed-threads.sh <scope-file>
#   <scope-file> lists one comment sink per line (written by the review skill's
#   Step 1). Prints each file it cleaned and how many threads it removed.

set -euo pipefail

SCOPE_FILE="${1:?usage: strip-closed-threads.sh <scope-file>}"

while IFS= read -r F; do
    [ -n "$F" ] && [ -f "$F" ] || continue
    case "$F" in
        *.review.md) continue ;;
        docs/tasks/*|*/docs/tasks/*) continue ;;
        *.md|*.markdown|*.html|*.htm) ;;
        *) continue ;;
    esac
    grep -q '<pd-' "$F" && continue

    BEFORE=$(grep -cE '<!-- (RESOLVED|REJECTED)\(P[123]\):' "$F" || true)
    [ "$BEFORE" -gt 0 ] || continue
    # Each thread sits on its own lines with a blank line either side. Swap each
    # for a marker, then collapse every run of markers (and the blank lines
    # around it) to one blank line — so back-to-back threads leave one gap, not
    # several. Threads at the top or bottom of the file leave no stray newlines.
    LC_ALL=C perl -0pi -e '
        s/<!-- (?:RESOLVED|REJECTED)\(P[123]\):.*?-->[ \t]*/\x00/gs;
        s/\n*\x00(?:\n*\x00)*\n*/\n\n/g;
        s/\A\n+//; s/\n*\z/\n/;
    ' "$F"
    echo "cleaned $F: removed $BEFORE closed thread(s)"
done < "$SCOPE_FILE"
