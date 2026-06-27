# 046 conformance evidence

Captured by `driver.sh` (self-contained, isolated `$HOME`, never touches `~/.auto`).
Latest run: **PASS=8 FAIL=0** (`results.txt`).

```bash
bash docs/tasks/046-multi-host-spa/artifacts/evidence/driver.sh   # requires ./bin/auto-dev
```

The driver builds nothing — build first from the repo root:

```bash
go build -tags dev -o bin/auto-dev ./auto-cli/cmd/auto   # dev (live-from-disk) build used by the driver
make build                                               # embed build (run the same assertions against ./bin/auto)
```

## What was actually EXECUTED (real autowatch + auto-ui, agent-browser + WS probe)

| File | AC(s) | Shows |
|---|---|---|
| `single-backend.txt` / `single-backend.png` | AC-7, AC-4, AC-6 | 1 host-tagged switcher option; `host-badge` present (`hostId=alpha-host`); `nav data-doc-count=3` (docs listed through the backend); `backend-health` connected; conn `open`; the html doc's `iframe[data-testid=doc-iframe]` `src` carries `hostId=alpha-host`. |
| `dual-backend.txt` / `dual-backend-connected.png` | AC-1, AC-3, AC-6 | Two real autowatch backends with DISTINCT host ids (`alpha-host`, `beta-host`) and a COLLIDING project id (`demo`). Switcher = union of 2 options, each with distinct `data-host-id` + composite value `"<host>\n demo"`; `host-badge` present; two `backend-health` rows, both `connected`. |
| `dual-backend.txt` ("SERVER TRUTH" + "after drop + reload") / `dual-backend-after-drop.png` | AC-6 | After killing the beta-host backend: the direct `backends.list` WS probe shows beta `connected:false` + a `lastErr`; after a real page reload the `backend-health` beta row reads `data-connected="false"`, `data-state="disconnected"`. |

All eight scripted assertions pass (`results.txt`). The two-backend path is fully
executed here — it was NOT too flaky to stand up — so the browser evidence and the Go
tests agree.

## Key findings / gotchas discovered while capturing this

1. **AC-6 "updates without reload" is reconnect/reload-bound on the client.** The SPA
   fetches `backends.list` only on initial load and on a store (re)connect
   (`store.js` `fetchBackends` is called from `initStore` and the `onStatus` open
   transition). There is **no event-driven re-fetch** when a backend passively drops.
   The *server* (`Manager.Health()`) flips the dropped backend to
   `connected:false` immediately (confirmed by the "SERVER TRUTH" WS probe, and gated
   by `backends_list_test.go` + `manager_test.go`'s `TestBackendReconnectsAfterDisconnect`),
   but the rendered rows only update on the next store reconnect or a real page reload.
   This is the implemented Phase-3 behaviour; flagged here as the honest browser
   limitation behind AC-6.

2. **agent-browser navigation reload semantics.** `agent-browser open <url>` to a URL
   that differs only in the `#hash` is a same-document hash nav — it does NOT reload
   the page or re-init the store. To force a real reload (so the store re-fetches),
   change the query string (the driver uses `?debug=1&reload=1`) or
   `eval 'location.reload()'`.

3. **agent-browser `eval` flakiness on property-access returns.** Pipelined
   `eval` of `el.dataset.foo` / bare-string / `outerHTML` intermittently returns
   empty even when the element is present; a `.length` count eval and JSON-object
   returns are reliable. The driver asserts badge presence via
   `querySelectorAll(...).length` and captures the hostId VALUE via the object-form
   dump in `single-backend.txt`.

4. **agent-browser Chrome cache vs isolated `$HOME`.** Chrome lives under the REAL
   home; the servers run under an isolated `$HOME`. The driver runs agent-browser with
   `HOME=$ABHOME` (real home) while the servers use the temp `$HOME`.

## Authoritative gate

The Go suite is the authoritative gate for server-side multi-backend behaviour:

```bash
cd auto-ui && go build ./... && go vet ./... && go test ./...
```

See `../conformance.md` for the full AC-1…AC-7 specification and the per-AC Go-test
cross-references.
