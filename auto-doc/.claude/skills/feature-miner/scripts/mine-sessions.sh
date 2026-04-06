#!/usr/bin/env bash
# mine-sessions.sh — Run parallel cass searches for feature mining
# Usage: mine-sessions.sh [--since YYYY-MM-DD] [--limit N]
#
# Searches coding session history across multiple query categories
# to find patterns in how AI agents interact with documentation.
# Output: .tmp/feature-miner-results.json

set -euo pipefail

SINCE_FLAG=""
LIMIT=15
OUTPUT_DIR=".tmp"

while [[ $# -gt 0 ]]; do
    case $1 in
        --since) SINCE_FLAG="--since $2"; shift 2 ;;
        --limit) LIMIT="$2"; shift 2 ;;
        *) echo "Unknown arg: $1" >&2; exit 1 ;;
    esac
done

mkdir -p "$OUTPUT_DIR"

# Check cass health
if ! cass health --json 2>/dev/null | grep -q '"healthy": *true'; then
    echo "ERROR: cass is not healthy. Run 'cass index' first." >&2
    exit 1
fi

echo "Mining coding sessions for documentation patterns..."
echo "  Since: ${SINCE_FLAG:-all time}"
echo "  Limit per category: $LIMIT"
echo ""

# Define search categories as query|label pairs
CATEGORIES=(
    "where is the documentation how to find docs|doc_discovery"
    "cannot find documentation missing docs no documentation|doc_frustration"
    "reading CLAUDE.md AGENTS.md documentation index|doc_usage"
    "search keyword grep docs find documentation file|search_behavior"
    "stale outdated documentation wrong instructions broken|doc_staleness"
    "autodoc docm tree stale fix agents search|autodoc_cli"
    "documentation frontmatter title summary hash|frontmatter_usage"
    "need to update docs documentation out of date|doc_maintenance"
)

RESULTS_FILE="$OUTPUT_DIR/feature-miner-results.json"
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

# Run searches in parallel
PIDS=()
for entry in "${CATEGORIES[@]}"; do
    IFS='|' read -r query label <<< "$entry"
    (
        # shellcheck disable=SC2086
        cass search "$query" \
            --limit "$LIMIT" \
            --json \
            --max-content-length 500 \
            $SINCE_FLAG \
            2>/dev/null \
        | python3 -c "
import sys, json
data = json.load(sys.stdin)
data['_category'] = '$label'
data['_query'] = '$query'
json.dump(data, sys.stdout)
" > "$TEMP_DIR/$label.json" 2>/dev/null
    ) &
    PIDS+=($!)
done

# Wait for all searches
for pid in "${PIDS[@]}"; do
    wait "$pid" 2>/dev/null || true
done

# Merge results
python3 -c "
import json, glob, os

results = {'categories': {}, 'summary': {'total_hits': 0, 'categories_searched': 0}}
for f in sorted(glob.glob('$TEMP_DIR/*.json')):
    try:
        with open(f) as fh:
            data = json.load(fh)
        label = data.get('_category', os.path.basename(f).replace('.json',''))
        query = data.get('_query', '')
        hits = data.get('hits', [])
        results['categories'][label] = {
            'query': query,
            'total_matches': data.get('total_matches', 0),
            'hits': hits
        }
        results['summary']['total_hits'] += len(hits)
        results['summary']['categories_searched'] += 1
    except (json.JSONDecodeError, FileNotFoundError):
        pass

with open('$RESULTS_FILE', 'w') as f:
    json.dump(results, f, indent=2)

print(f'Results written to $RESULTS_FILE')
print(f'Categories searched: {results[\"summary\"][\"categories_searched\"]}')
print(f'Total hits: {results[\"summary\"][\"total_hits\"]}')
print()
for cat, data in results['categories'].items():
    print(f'  {cat}: {data[\"total_matches\"]} matches ({len(data[\"hits\"])} returned)')
" 2>&1

echo ""
echo "Done. Results in $RESULTS_FILE"
