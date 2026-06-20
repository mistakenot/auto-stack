# 027 Conformance Testing Strategy

Manual test plan for verifying plan lifecycle status and executor liveness in auto-ui.
This is a specification — not executable code.

## 1. Setup

### Build

Dual build to verify both paths:

```bash
# Embedded (production)
make build

# Dev mode (live-from-disk assets)
go build -tags dev -o bin/auto ./auto-cli/cmd/auto
```

### Fixture

Create an isolated fixture directory with lowercase-kebab project id:

```
/tmp/conformance-027/
  projects.json          # single project pointing at docs-root below
  docs-root/docs/tasks/
    900-executing-plan/plan.html    # pd-meta: status=executing, branch=feat/900
    901-merged-plan/plan.html       # pd-meta: status=merged, branch=null
    902-planning-plan/plan.html     # pd-meta: status=planning, branch=null
```

Each `plan.html` must have a valid `<script type="application/json" id="pd-meta">` block
in `<head>` and a `<pd-doc>` element with appropriate `status` attribute.

### Launch

```bash
AUTO_UI_DEBUG=1 bin/auto ui serve --port 0 --ready-file /tmp/conformance-027/ready --projects /tmp/conformance-027/projects.json
```

Wait for the ready file, read the assigned port.

## 2. Status Assertions

Open the explorer in a browser at `http://localhost:<port>/?debug=1#/explore`.

1. The executing plan's tree node (`data-doc-path` ending in `900-executing-plan/plan.html`)
   must have `data-plan-status="executing"` and a visible spinner element
   (`span.plan-spinner[data-plan-status="executing"]`).

2. The merged plan's node (`901-merged-plan/plan.html`) must NOT have
   `data-plan-status="executing"`. Its `data-plan-status` should be `"merged"`.

3. The planning plan's node (`902-planning-plan/plan.html`) must have
   `data-plan-status="planning"`.

4. If a `<pd-doc status="approved">` attribute is set on the executing plan,
   the tree node must show a review-state pill with `data-review-state="approved"`.

## 3. Liveness Assertions

### 3a. Active liveness

POST a hand-crafted `agent.tool.post` JSON-RPC notification to `POST /api/rpc` from
localhost (loopback-only guard passes without Origin):

```bash
curl -s -X POST http://localhost:<port>/api/rpc \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "method": "agent.tool.post",
    "params": {
      "project": "<fixture-project-id>",
      "branch": "feat/900",
      "data": { "path": "docs/tasks/900-executing-plan/plan.html" }
    }
  }'
```

Poll the executing plan's tree node: `data-liveness` should be `"active"`.

### 3b. Idle transition

Append `?liveWindowMs=2000` to the URL to override the default 120s active window.
Wait >2 seconds after the last POST. Poll the node: `data-liveness` should transition
to `"idle"`.

### 3c. Main/null-branch exclusion (AC-5b)

A plan with `branch: null` or `branch: "main"` must NEVER receive a `data-liveness`
attribute. Verify: the merged plan (`901-merged-plan`) and planning plan (`902-planning-plan`)
have no `data-liveness` attribute regardless of bus traffic.

## 4. AC-4 Repaint Assertion

Test that editing an existing plan's `pd-meta` triggers a re-list and repaint:

1. Confirm `902-planning-plan/plan.html` shows `data-plan-status="planning"`.

2. Rewrite its `pd-meta` on disk: change `"status": "planning"` to `"status": "executing"`.

3. Emit a `doc.changed` for that path:
   ```bash
   bin/auto ui emit --project <fixture-project-id> --path docs/tasks/902-planning-plan/plan.html
   ```

4. Poll the node (up to 3s): it should gain `data-plan-status="executing"` WITHOUT
   `data-doc-count` increasing (the path was already known, so this is a re-list of
   existing docs, not a new-doc discovery).

## 5. Grep Gates

Structural invariants enforced by grep over the source:

```bash
# on("  subscriptions only in store.js
grep -rl 'on("' auto-ui/web/static/ | sort
# Expected: only store.js

# onAny( only in rpc.js (definition) and store.js (subscription)
grep -rl 'onAny(' auto-ui/web/static/ | sort
# Expected: rpc.js, store.js
```

Any additional files in either result is a conformance failure (029 grep gate violation).

## 6. Teardown

```bash
find /tmp/conformance-027 -delete
# Stop the server (kill the process)
```
