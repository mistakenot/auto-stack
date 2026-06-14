// rpc.js — a tiny JSON-RPC 2.0 client over a single WebSocket connection.
//
// Framework-agnostic (no Preact import). One module-level singleton connection is
// shared across the app, so SPA re-renders on route changes do not reopen sockets.
//
// Three primitives, matching the server:
//   call(method, params) -> Promise   client RPC request, resolves on the
//                                      correlated response (matched by id)
//   on(method, handler)               subscribe to server push (id-less
//                                      notifications, e.g. "ping")
//   onStatus(handler)                 observe connection state changes

// Derive the WebSocket URL from the page origin. CRITICAL: behind
// `tailscale serve` the page is HTTPS, so the socket must be wss:// — using ws://
// on an https page fails the browser's mixed-content / handshake check.
function wsURL() {
  const scheme = location.protocol === "https:" ? "wss" : "ws";
  return `${scheme}://${location.host}/api/ws`;
}

let ws = null;
let status = "connecting"; // "connecting" | "open" | "closed"
let nextId = 1;
let backoff = 500; // reconnect delay, grows to a cap on repeated failures
let reconnects = 0; // number of reconnect cycles, surfaced via reconnectCount()

const pending = new Map(); // id -> { resolve, reject }
const notifyHandlers = new Map(); // method -> Set<handler>
const statusHandlers = new Set();

// --- Observability (AC-4 / AC-5 substrate) --------------------------------
//
// A bounded ring records every received server notification so the /debug page
// can backfill pre-mount history even for events no view subscribed to. It is
// exposed as window.__autoui ONLY when debug is enabled; the ring itself always
// records, so the buffer carries history regardless of the gate.
const MAX_EVENTS = 200;
const events = []; // [{ t, method, params }], newest pushed at the end
const counters = new Map(); // method -> count

function recordEvent(method, params) {
  events.push({ t: Date.now(), method, params });
  if (events.length > MAX_EVENTS) events.shift();
  counters.set(method, (counters.get(method) || 0) + 1);
}

// An always-on bounded error ring captures failures that flow through rpc.js
// (call() rejects, window.onerror, unhandledrejection) plus any explicit
// recordError() calls from views (e.g. content.js markdown/iframe failures).
const MAX_ERRORS = 100;
const errors = []; // [{ t, source, message }]

// stringifyErr coerces an arbitrary thrown value into a readable message.
function stringifyErr(err) {
  if (err == null) return String(err);
  if (err instanceof Error) return err.message || err.toString();
  if (typeof err === "string") return err;
  try {
    return JSON.stringify(err);
  } catch {
    return String(err);
  }
}

// recordError appends to the bounded error ring. Exported so views can record
// failures that never reach rpc.js (content.js markdown parse / iframe load).
export function recordError(source, err) {
  errors.push({ t: Date.now(), source, message: stringifyErr(err) });
  if (errors.length > MAX_ERRORS) errors.shift();
}

// recentErrors returns the live error ring (newest at the end).
export function recentErrors() {
  return errors;
}

// debugEnabled is true when the page opts into debug instrumentation via the
// ?debug=1 query param or a localStorage flag. localStorage access can throw in
// some contexts (sandboxed iframes, disabled storage), so guard it.
function debugEnabled() {
  if (new URLSearchParams(location.search).get("debug") === "1") return true;
  try {
    return localStorage.getItem("autouiDebug") === "1";
  } catch {
    return false;
  }
}

// Expose the live ring/counter objects under window.__autoui only when debug is
// on. Live references mean later pushes are visible to anything that read the
// object once. Production (debug off) never gets the property assigned.
if (debugEnabled()) {
  window.__autoui = { events, counters, max: MAX_EVENTS };
}

// Capture uncaught errors and rejections once at module init.
window.onerror = (message, source, lineno, colno, error) => {
  recordError("window.onerror", error || message);
};
window.addEventListener("unhandledrejection", (ev) => {
  recordError("unhandledrejection", ev.reason);
});

function setStatus(next) {
  status = next;
  for (const h of statusHandlers) h(status);
}

function connect() {
  setStatus(ws ? "connecting" : "connecting");
  const sock = new WebSocket(wsURL());
  ws = sock;

  sock.onopen = () => {
    backoff = 500; // reset on a successful connect
    setStatus("open");
  };

  sock.onmessage = (ev) => {
    let msg;
    try {
      msg = JSON.parse(ev.data);
    } catch {
      return; // ignore non-JSON frames
    }
    // A message WITH an id is a response to one of our calls; WITHOUT an id it
    // is a server-pushed notification.
    if (msg.id !== undefined && msg.id !== null && pending.has(msg.id)) {
      const { resolve, reject } = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.error) reject(new Error(msg.error.message || "rpc error"));
      else resolve(msg.result);
      return;
    }
    if (msg.method) {
      // Record on the SAME path that fans out to on() handlers, so the ring
      // captures notifications no view subscribes to.
      recordEvent(msg.method, msg.params);
      const hs = notifyHandlers.get(msg.method);
      if (hs) for (const h of hs) h(msg.params);
    }
  };

  const reconnect = () => {
    if (status === "closed" && ws !== sock) return; // already replaced
    reconnects++;
    setStatus("closed");
    // Fail any in-flight calls so callers don't hang forever.
    for (const [id, { reject }] of pending) {
      reject(new Error("connection closed"));
      pending.delete(id);
    }
    const delay = backoff;
    backoff = Math.min(backoff * 2, 5000);
    setTimeout(connect, delay);
  };

  sock.onclose = reconnect;
  sock.onerror = () => sock.close();
}

// call sends a JSON-RPC request and resolves with its result (or rejects on a
// JSON-RPC error / disconnect).
export function call(method, params) {
  return new Promise((resolve, reject) => {
    // Wrap reject so every RPC failure is recorded in the error ring, tagged
    // with the method that failed.
    const fail = (err) => {
      recordError("rpc:" + method, err);
      reject(err);
    };
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      fail(new Error("not connected"));
      return;
    }
    const id = nextId++;
    pending.set(id, { resolve, reject: fail });
    ws.send(JSON.stringify({ jsonrpc: "2.0", id, method, params }));
  });
}

// on subscribes to a server-push notification method. Returns an unsubscribe fn.
export function on(method, handler) {
  if (!notifyHandlers.has(method)) notifyHandlers.set(method, new Set());
  notifyHandlers.get(method).add(handler);
  return () => notifyHandlers.get(method)?.delete(handler);
}

// onStatus subscribes to connection-state changes and fires once with current.
export function onStatus(handler) {
  statusHandlers.add(handler);
  handler(status);
  return () => statusHandlers.delete(handler);
}

// reconnectCount returns how many reconnect cycles have occurred.
export function reconnectCount() {
  return reconnects;
}

// connInfo returns the current connection snapshot for the /debug page.
export function connInfo() {
  return { status, reconnects };
}

// whenOpen resolves once the socket is open: immediately if already open, else
// on the next status transition to "open". Mount-time fetches await this so a
// cold load doesn't reject with "not connected".
export function whenOpen() {
  if (status === "open") return Promise.resolve();
  return new Promise((resolve) => {
    const unsub = onStatus((s) => {
      if (s === "open") {
        unsub();
        resolve();
      }
    });
  });
}

connect();
