// debug.js — the #/debug diagnostics page (presentational). One screenshot-able
// read-only view with four data-testid-tagged sections: connection, event log,
// error log, and current state. The store is the single source of state: it owns
// the conn slice, the events ring (its one doc.changed + ping subscription, wired
// at startup so events accumulate regardless of which view is mounted), and the
// current-state snapshot (selectDebugSnapshot, replacing the deleted uistate.js
// mirror). The error log still reads rpc.js's recentErrors() ring; /api/hello is
// a one-shot fetch for the mode/message rows.
import { useState, useEffect } from "preact/hooks";
import { html } from "htm/preact";
import { connInfo, reconnectCount, recentErrors } from "./rpc.js";
import { useStore, selectConn, selectDebugSnapshot } from "./store.js";

// statusLabel maps the raw rpc.js status to a human label (open -> connected).
function statusLabel(status) {
  if (status === "open") return "connected";
  if (status === "closed") return "closed";
  return "reconnecting";
}

// fmtTime renders an epoch-millis timestamp as a local time string. Falls back
// to the raw value when it isn't a finite number.
function fmtTime(t) {
  if (typeof t !== "number" || !isFinite(t)) return String(t ?? "");
  return new Date(t).toLocaleTimeString();
}

// eventRow normalizes a raw __autoui ring entry or a live notification into the
// shape the event-log table renders. For doc.changed the changed path lives at
// params.data.path (NOT params.path); project/worktree are top-level.
function eventRow(t, method, params) {
  const p = params || {};
  return {
    t,
    method,
    project: p.project || "",
    path: (p.data && p.data.path) || p.path || "",
    params: p,
  };
}

export function Debug() {
  // --- Connection -----------------------------------------------------------
  // The conn slice (status + reconnects) is mirrored into the store from
  // rpc.onStatus; read it reactively so the page updates live without polling.
  const conn = useStore(selectConn);
  const status = conn.status;
  const [hello, setHello] = useState(null);
  useEffect(() => {
    let alive = true;
    fetch("/api/hello")
      .then((r) => r.json())
      .then((j) => {
        if (alive) setHello(j);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  // Prefer the store's reconnect count (kept current via the conn slice), falling
  // back to rpc.js directly for parity with the previous read.
  const info = connInfo();
  const reconnects = conn.reconnects ?? info.reconnects ?? reconnectCount();

  // --- Event log ------------------------------------------------------------
  // The store's events ring is the single source: initStore() subscribes once to
  // doc.changed + ping at startup, so events accumulate regardless of which view
  // is mounted (no per-page subscription, no window.__autoui backfill needed).
  // Render newest-first; de-dupe is not required.
  const storeEvents = useStore((s) => s.events);
  const events = storeEvents
    .map((e) => eventRow(e.t, e.method, e.params))
    .reverse();

  // --- Error log + current state --------------------------------------------
  // The current-state snapshot is derived from the store (selectDebugSnapshot
  // replaces the deleted uistate.js mirror) and updates reactively across nav.
  const snapshot = useStore(selectDebugSnapshot);

  // The error ring lives in rpc.js (not the store); poll it every 1s to keep the
  // error log current (call() rejects feed it asynchronously without a re-render).
  const [errors, setErrors] = useState([]);
  useEffect(() => {
    const refresh = () => setErrors([...recentErrors()].reverse());
    refresh();
    const id = setInterval(refresh, 1000);
    return () => clearInterval(id);
  }, []);

  const sectionStyle = { marginBottom: "1.5rem" };
  const thStyle = { textAlign: "left" };

  return html`
    <section>
      <header style=${{ marginBottom: "1rem" }}>
        <strong>auto-ui diagnostics</strong> <small>(#/debug)</small>
      </header>

      <section data-testid="debug-connection" style=${sectionStyle}>
        <h3>Connection</h3>
        <table>
          <tbody>
            <tr data-testid="debug-connection-row">
              <th style=${thStyle}>status</th>
              <td>${statusLabel(status)} <small>(${status})</small></td>
            </tr>
            <tr data-testid="debug-connection-row">
              <th style=${thStyle}>reconnects</th>
              <td>${reconnects}</td>
            </tr>
            <tr data-testid="debug-connection-row">
              <th style=${thStyle}>mode</th>
              <td>${hello ? hello.mode : "…"}</td>
            </tr>
            <tr data-testid="debug-connection-row">
              <th style=${thStyle}>message</th>
              <td>${hello ? hello.message : "…"}</td>
            </tr>
            <tr data-testid="debug-connection-row">
              <th style=${thStyle}>host</th>
              <td>${location.host}</td>
            </tr>
          </tbody>
        </table>
      </section>

      <section data-testid="debug-event-log" style=${sectionStyle}>
        <h3>Event log <small>(${events.length})</small></h3>
        ${events.length === 0
          ? html`<p><em>No events received yet.</em></p>`
          : html`
              <table>
                <thead>
                  <tr>
                    <th style=${thStyle}>type</th>
                    <th style=${thStyle}>time</th>
                    <th style=${thStyle}>project</th>
                    <th style=${thStyle}>path</th>
                    <th style=${thStyle}>raw</th>
                  </tr>
                </thead>
                <tbody>
                  ${events.map(
                    (e, i) => html`
                      <tr key=${i} data-testid="debug-event-row">
                        <td>${e.method}</td>
                        <td>${fmtTime(e.t)}</td>
                        <td>${e.project}</td>
                        <td>${e.path}</td>
                        <td>
                          <details>
                            <summary>raw</summary>
                            <pre>${JSON.stringify(e.params, null, 2)}</pre>
                          </details>
                        </td>
                      </tr>
                    `
                  )}
                </tbody>
              </table>
            `}
      </section>

      <section data-testid="debug-error-log" style=${sectionStyle}>
        <h3>Error log <small>(${errors.length})</small></h3>
        ${errors.length === 0
          ? html`<p><em>No errors recorded.</em></p>`
          : html`
              <table>
                <thead>
                  <tr>
                    <th style=${thStyle}>source</th>
                    <th style=${thStyle}>message</th>
                    <th style=${thStyle}>time</th>
                  </tr>
                </thead>
                <tbody>
                  ${errors.map(
                    (e, i) => html`
                      <tr key=${i} data-testid="debug-error-row">
                        <td>${e.source}</td>
                        <td>${e.message}</td>
                        <td>${fmtTime(e.t)}</td>
                      </tr>
                    `
                  )}
                </tbody>
              </table>
            `}
      </section>

      <section data-testid="debug-current-state" style=${sectionStyle}>
        <h3>Current state</h3>
        <table>
          <tbody>
            <tr data-testid="debug-state-row">
              <th style=${thStyle}>project</th>
              <td>${snapshot.project || "—"}</td>
            </tr>
            <tr data-testid="debug-state-row">
              <th style=${thStyle}>path</th>
              <td>${snapshot.path || "—"}</td>
            </tr>
            <tr data-testid="debug-state-row">
              <th style=${thStyle}>type</th>
              <td>${snapshot.type || "—"}</td>
            </tr>
            <tr data-testid="debug-state-row">
              <th style=${thStyle}>revision</th>
              <td>${snapshot.revision}</td>
            </tr>
            <tr data-testid="debug-state-row">
              <th style=${thStyle}>doc count</th>
              <td>${snapshot.docCount}</td>
            </tr>
            <tr data-testid="debug-state-row">
              <th style=${thStyle}>last updated</th>
              <td>${snapshot.lastUpdated || "—"}</td>
            </tr>
          </tbody>
        </table>
      </section>
    </section>
  `;
}
