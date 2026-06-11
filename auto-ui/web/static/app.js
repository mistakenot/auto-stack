// app.js — no-build Preact + htm app.
// State lives in the URL hash: the active view and the Home counter (?n=).
import { render } from "preact";
import { useState, useEffect } from "preact/hooks";
import { html } from "htm/preact";
import { parseHash, setHash, onRouteChange } from "./router.js";
import { call, on, onStatus } from "./rpc.js";
import { DocView } from "./doc.js";

// Nav switches views by rewriting the hash. Home carries its counter (?n=).
function Nav({ view }) {
  const link = (id, label) => html`
    <button
      onClick=${() => setHash(id, new URLSearchParams())}
      style=${{ fontWeight: view === id ? "bold" : "normal", marginRight: "0.5rem" }}
    >
      ${label}
    </button>
  `;
  return html`
    <nav>
      <strong style=${{ marginRight: "1rem" }}>auto-ui</strong>
      ${link("home", "Home")}
      ${link("dashboard", "Dashboard")}
      ${link("doc", "Docs")}
    </nav>
  `;
}

// Home — the counter value is read from the hash ?n= and written back on +.
function Home({ params }) {
  const n = parseInt(params.get("n") || "0", 10) || 0;
  const inc = () => setHash("home", new URLSearchParams({ n: String(n + 1) }));
  return html`
    <section>
      <h1>Home</h1>
      <p>clicked: ${n} <button onClick=${inc}>+</button></p>
      <p><small>count persists in the URL (#/home?n=${n})</small></p>
    </section>
  `;
}

// Dashboard — exercises both transports against the Go server:
//   /api/hello  — classic one-shot fetch
//   /api/ws     — JSON-RPC over WebSocket: server-pushed `ping` notifications
//                 (live, every 1s) and a client-initiated `ping` RPC call.
function Dashboard() {
  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);
  const fetchGo = async () => {
    setError(null);
    try {
      const res = await fetch("/api/hello");
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      setResult(data);
    } catch (e) {
      setError(String(e));
    }
  };

  // WebSocket state: connection status, the latest server push, and the count
  // of pushes received this session.
  const [wsStatus, setWsStatus] = useState("connecting");
  const [lastPush, setLastPush] = useState(null);
  const [pushCount, setPushCount] = useState(0);
  const [pong, setPong] = useState(null);

  useEffect(() => {
    // Server push (#3): subscribe to the `ping` notification stream.
    const offPing = on("ping", (params) => {
      setLastPush(params);
      setPushCount((n) => n + 1);
    });
    const offStatus = onStatus(setWsStatus);
    return () => {
      offPing();
      offStatus();
    };
  }, []);

  // Client RPC (#1 + #2): call the `ping` method and show the correlated pong.
  const sendPing = async () => {
    try {
      const res = await call("ping", { seq: pushCount });
      setPong(res);
    } catch (e) {
      setPong({ error: String(e) });
    }
  };

  return html`
    <section>
      <h1>Dashboard</h1>

      <button onClick=${fetchGo}>fetch from go</button>
      ${result &&
      html`<p>go says: ${result.message} (mode=${result.mode})</p>`}
      ${error && html`<p style=${{ color: "red" }}>error: ${error}</p>`}

      <hr />
      <h2>WebSocket (JSON-RPC)</h2>
      <p>
        connection: <strong>${wsStatus}</strong>
      </p>
      <p>
        server push: <strong>${pushCount}</strong> ping(s) received${lastPush
          ? html` · latest seq ${lastPush.seq}`
          : ""}
      </p>
      <p>
        <button onClick=${sendPing} disabled=${wsStatus !== "open"}>
          send ping RPC
        </button>
        ${pong &&
        (pong.error
          ? html` <span style=${{ color: "red" }}>${pong.error}</span>`
          : html` <span>pong ✓ (seq ${pong.seq})</span>`)}
      </p>
    </section>
  `;
}

// App derives the current view from the hash and renders the matching view.
function App() {
  const { view, params } = parseHash();
  const body =
    view === "doc"
      ? html`<${DocView} params=${params} />`
      : view === "dashboard"
        ? html`<${Dashboard} />`
        : html`<${Home} params=${params} />`;
  // <main class="container"> is Pico's centered, padded page wrapper.
  return html`
    <main class="container">
      <${Nav} view=${view} />
      ${body}
    </main>
  `;
}

// Re-render the whole app into #app whenever the route changes.
function mount() {
  render(html`<${App} />`, document.getElementById("app"));
}

onRouteChange(mount);
mount();
