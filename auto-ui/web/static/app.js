// app.js — no-build Preact + htm app.
// State lives in the URL hash: the active view and the Home counter (?n=).
import { render } from "preact";
import { useState } from "preact/hooks";
import { html } from "htm/preact";
import { parseHash, setHash, onRouteChange } from "./router.js";

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

// Dashboard — fetches /api/hello from the Go server and renders the message.
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
  return html`
    <section>
      <h1>Dashboard</h1>
      <button onClick=${fetchGo}>fetch from go</button>
      ${result &&
      html`<p>go says: ${result.message} (mode=${result.mode})</p>`}
      ${error && html`<p style=${{ color: "red" }}>error: ${error}</p>`}
    </section>
  `;
}

// App derives the current view from the hash and renders the matching view.
function App() {
  const { view, params } = parseHash();
  const body =
    view === "dashboard" ? html`<${Dashboard} />` : html`<${Home} params=${params} />`;
  return html`
    <div>
      <${Nav} view=${view} />
      ${body}
    </div>
  `;
}

// Re-render the whole app into #app whenever the route changes.
function mount() {
  render(html`<${App} />`, document.getElementById("app"));
}

onRouteChange(mount);
mount();
