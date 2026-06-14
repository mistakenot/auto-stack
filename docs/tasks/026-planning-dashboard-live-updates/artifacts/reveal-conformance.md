# Conformance: AC-3b — a newly-created doc is REVEALED in the nav tree

## The gap

026 AC-3 said: a `doc.changed` for a path the tree doesn't yet know triggers one
`doc.list` re-list "so the new node appears (`data-doc-count` grows)". It asserted
the **count**, never that the node became **visible**.

The explorer redesign (`feat/auto-ui-explorer-redesign`) made every group
collapsed-by-default and capped the Tasks group at 10 subgroups. That turned the
untested half of AC-3 into a user-visible bug: when an agent creates a new doc,
the tree re-lists (count grows) but the new node stays **hidden inside a folded
group** — the user "sees nothing change". The live pipeline (hook →
`agent.tool.post` → `/api/rpc` → `DeriveDocChanged` → `doc.changed` over WS → tree
re-list) was never broken; the consumer just never *revealed* the result.

## Acceptance (AC-3b)

When a `doc.changed` arrives for the active project carrying a path the tree does
not yet know:

1. the browser receives the `doc.changed` (WS delivery) — `window.__autoui`
   `doc.changed` counter increments;
2. the tree re-lists — `data-doc-count` grows by 1 (the node is in the model);
3. **the new node is VISIBLE** — a `[data-doc-path]` element for the new path
   exists in the DOM, because its ancestor group + subgroup auto-expand.

Negative: a `doc.changed` for an **already-known** path must NOT re-list or
force-open any group (the open-doc refresh in `content.js` handles edits; the
tree is already correct). Only unknown paths trigger reveal.

## Method

Browser-driven with `agent-browser` against the **embed** build, asserting via
`data-*` attributes + `window.__autoui` — never rendered-text diffs (a re-fetch
can leave text identical) and never Go tests (013 feedback: browser-layer defects
are invisible to them). Isolated temp fixture registry (`--projects … --port 0
--ready-file`); never touches `~/.auto/projects.json` or the real `docs/`.

Steps (see [`reveal-harness.sh`](./reveal-harness.sh)):

1. Build the embed binary so current `web/static/*` JS is baked in.
2. Fixture: a lowercase-id project with several task dirs so a group folds.
3. Serve with debug; open the explorer with `?debug=1`; select the project.
4. Record `data-doc-count` (= N) and the `doc.changed` counter.
5. Create a **brand-new** task doc on disk, then fire `doc.changed` (`auto ui
   emit`, or a real `auto hooks fire` Write payload).
6. Assert the three AC-3b conditions: counter +1, `data-doc-count` == N+1, and a
   `[data-doc-path]` node for the new path is present.

## Results

| Run | doc.changed received | re-list (count+1) | **node VISIBLE** |
|-----|----------------------|-------------------|------------------|
| pre-fix  | PASS | PASS | **FAIL** (hidden in folded group) |
| post-fix | PASS | PASS | **PASS** |

Also verified end-to-end against the real `auto-stack` registry via an actual
`auto hooks fire` Write payload: `data-doc-count` 191→192, the new node visible,
the `Tasks` group auto-expanded to exactly the one new node (no unrelated groups
opened), throwaway file cleaned up.

## The fix

`auto-ui/web/static/tree.js`. On an unknown-path `doc.changed`, before the
re-list, add the path's ancestor `group:`/`sub:` tokens to a sticky `expanded`
set (`expandTokensForPath`, derived from the existing `flashTokensForPath` so the
reveal targets match the renderer's node keys). `Collapsible` gains a `forceOpen`
prop that opens on its rising edge (the user can still re-collapse). `GroupBody`
also forces past the Tasks 10-cap when the revealed subgroup is beyond it, so the
new node can't re-list invisibly behind "show N more".

## Harness gotcha (fixed in the script)

`strings "$BIN" | grep -q parseDocChanged` under `set -o pipefail` falsely reports
failure: `grep -q` exits on the first match and closes the pipe, so `strings`
dies with SIGPIPE (141) and pipefail surfaces that as the pipeline status. Count
matches (`grep -c`) instead, so the reader consumes all input.
