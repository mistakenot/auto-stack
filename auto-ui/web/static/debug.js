// debug.js — the #/debug diagnostics page. One screenshot-able read-only view
// with four data-testid-tagged sections: connection, event log, error log, and
// current state. Everything is read from rpc.js (connInfo / recentErrors /
// on()), uistate.js (the cross-route snapshot), and a one-shot /api/hello fetch.
//
// The route is always reachable (read-only diagnostics on a trusted host). Only
// the pre-mount event backfill depends on ?debug=1, because window.__autoui — the
// ring that records notifications no view subscribed to — is gated behind the
// debug flag. Live subscriptions (on("doc.changed"/"ping")) work regardless.
import { useState, useEffect } from "preact/hooks";
import { html } from "htm/preact";
import { on, onStatus, connInfo, reconnectCount, recentErrors } from "./rpc.js";
import { uiState } from "./uistate.js";

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
  const [status, setStatus] = useState("connecting");
  const [hello, setHello] = useState(null);
  useEffect(() => {
    const off = onStatus(setStatus);
    let alive = true;
    fetch("/api/hello")
      .then((r) => r.json())
      .then((j) => {
        if (alive) setHello(j);
      })
      .catch(() => {});
    return () => {
      alive = false;
      off();
    };
  }, []);

  const info = connInfo();
  const reconnects = info.reconnects ?? reconnectCount();

  // --- Event log ------------------------------------------------------------
  // Live subscriptions from mount (work even with debug off), plus a one-time
  // backfill from the window.__autoui ring (present only under ?debug=1). Newest
  // first; de-dupe is not required.
  const [events, setEvents] = useState([]);
  useEffect(() => {
    const backfill = [];
    const ring = typeof window !== "undefined" ? window.__autoui : undefined;
    if (ring && Array.isArray(ring.events)) {
      for (const e of ring.events) {
        backfill.push(eventRow(e.t, e.method, e.params));
      }
    }
    // Reverse so the backfilled history is newest-first like live events.
    backfill.reverse();
    setEvents(backfill);

    const push = (method) => (params) =>
      setEvents((prev) => [eventRow(Date.now(), method, params), ...prev]);
    const offDoc = on("doc.changed", push("doc.changed"));
    const offPing = on("ping", push("ping"));
    return () => {
      offDoc();
      offPing();
    };
  }, []);

  // --- Error log + current state --------------------------------------------
  // Poll every 1s so the error ring and the cross-route uiState snapshot stay
  // current (the explorer components are unmounted here, so there is no DOM to
  // read and no render tick driven by their updates).
  const [errors, setErrors] = useState([]);
  const [snapshot, setSnapshot] = useState({ ...uiState });
  useEffect(() => {
    const refresh = () => {
      setErrors([...recentErrors()].reverse());
      setSnapshot({ ...uiState });
    };
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
