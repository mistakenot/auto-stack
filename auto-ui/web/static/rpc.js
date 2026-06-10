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

const pending = new Map(); // id -> { resolve, reject }
const notifyHandlers = new Map(); // method -> Set<handler>
const statusHandlers = new Set();

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
      const hs = notifyHandlers.get(msg.method);
      if (hs) for (const h of hs) h(msg.params);
    }
  };

  const reconnect = () => {
    if (status === "closed" && ws !== sock) return; // already replaced
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
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      reject(new Error("not connected"));
      return;
    }
    const id = nextId++;
    pending.set(id, { resolve, reject });
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

connect();
