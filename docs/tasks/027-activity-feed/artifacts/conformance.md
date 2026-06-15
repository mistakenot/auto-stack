# Conformance Harness: Task 027 — Activity Feed

> Acceptance for 027 validates the in-session "Recent activity" feed: doc.changed WS events
> populate a newest-first, deduped, clickable list in the explorer sidebar. Drive the served SPA
> with `agent-browser` and assert via `data-testid` attributes.

## Preconditions

- Built on tasks 024–026 — `project.list`, `doc.list`, `doc.get`, `ui emit`, `window.__autoui`,
  `docevents.js`, and the explorer shell must all exist.
- `agent-browser` available headless on this host.
- **Project ids MUST be lowercase** (`^[a-z0-9]+(?:-[a-z0-9]+)*$`).
- Binaries must be built from this worktree so `activity.js` is embedded/served.

## Fixture builder

```bash
TMP=$(mktemp -d)
mkdir -p "$TMP/proj-a/docs/tasks/001-demo" \
         "$TMP/proj-a/docs/tasks/002-second" \
         "$TMP/proj-a/docs/research"
printf '# Demo plan\nhello\n' > "$TMP/proj-a/docs/tasks/001-demo/plan.md"
printf '# Demo reqs\n'        > "$TMP/proj-a/docs/tasks/001-demo/requirements.md"
printf '# Second\n'           > "$TMP/proj-a/docs/tasks/002-second/plan.md"
printf '# Research\n'         > "$TMP/proj-a/docs/research/notes.md"
cat > "$TMP/projects.json" <<JSON
{"projects":[
  {"id":"proj-a","name":"Project A","path":"$TMP/proj-a","remote":"https://github.com/x/a.git"}
]}
JSON
```

## Build + launch

```bash
cd /path/to/worktree/auto-cli && go build -o /tmp/auto-027 ./cmd/auto
BIN=/tmp/auto-027
( cd /path/to/worktree/auto-ui && \
  AUTO_UI_DEBUG=1 "$BIN" ui serve --port 0 --ready-file "$TMP/ready.json" --projects "$TMP/projects.json" ) &
ADDR=$(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1])).addr)' "$TMP/ready.json")
PORT=${ADDR##*:}
```

## AC-1 — Feed appears after a doc.changed event

- [ ] Open explorer at `http://$ADDR/?debug=1#/explore?project=proj-a`.
- [ ] Confirm `[data-testid="activity-feed"]` does NOT exist (no events yet).
- [ ] Emit: `"$BIN" ui emit --port "$PORT" --project proj-a --path docs/tasks/001-demo/plan.md`
- [ ] Poll: `[data-testid="activity-feed"]` exists. `[data-testid="activity-item"]` count = 1.
- [ ] The item has `data-activity-path="docs/tasks/001-demo/plan.md"`.

## AC-2 — Deduplication and ordering

- [ ] Emit a second event for the SAME path: `docs/tasks/001-demo/plan.md`.
- [ ] `[data-testid="activity-item"]` count is still 1 (deduped).
- [ ] The item shows an edit count badge (the `Nx` indicator).
- [ ] Emit a DIFFERENT path: `docs/tasks/002-second/plan.md`.
- [ ] `[data-testid="activity-item"]` count = 2. The first item (newest) has
      `data-activity-path="docs/tasks/002-second/plan.md"`.

## AC-3 — Click navigates to the doc

- [ ] Click the first activity item (`docs/tasks/002-second/plan.md`).
- [ ] `location.hash` contains `path=docs%2Ftasks%2F002-second%2Fplan.md`.

## Cleanup

```bash
agent-browser close --all 2>/dev/null
kill %1 2>/dev/null
find "$TMP" -delete 2>/dev/null
```
