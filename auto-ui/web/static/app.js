// app.js — no-build Preact + htm app.
// The planning-docs explorer is the default view. All view state lives in the
// URL hash (#/<view>?<query>). A bare load (empty hash, or the legacy
// home/dashboard views) redirects to #/explore so the explorer is the landing.
import { render } from "preact";
import { html } from "htm/preact";
import { parseHash, setHash, onRouteChange } from "./router.js";
import { Explorer } from "./explorer.js";
import { Debug } from "./debug.js";
// The module-singleton store is the single source of client state: it owns the
// connection slice, the one doc.changed subscription, route→selection mirroring,
// and every server fetch (with a docsByProject cache). initStore() wires those
// side-effects exactly once and is called at startup BEFORE the first render, so
// the views (now presentational) read store slices that are already being kept
// in sync. (029, Phase 2)
import { initStore } from "./store.js";

// App derives the current view from the hash and renders the matching view.
// The explorer is the default: empty/legacy views fall through to it (and the
// hash is normalized to #/explore on mount — see normalizeHash).
function App() {
  const { view, params } = parseHash();
  // #/debug keeps Pico's centered container; the explorer owns the full viewport
  // (it renders its own full-height app shell — see explorer.js / app.css).
  if (view === "debug") {
    return html`<main class="container"><${Debug} /></main>`;
  }
  return html`<${Explorer} params=${params} />`;
}

// normalizeHash redirects bare/legacy landings to #/explore so the URL reflects
// the explorer landing (and a reload lands consistently). Guarded against loops:
// it only rewrites when the view is empty/home/dashboard.
function normalizeHash() {
  const { view } = parseHash();
  if (view === "" || view === "home" || view === "dashboard") {
    setHash("explore", new URLSearchParams());
    return true; // a hashchange will fire → mount() re-runs
  }
  return false;
}

// Re-render the whole app into #app whenever the route changes.
function mount() {
  if (normalizeHash()) return; // wait for the hashchange from the redirect
  render(html`<${App} />`, document.getElementById("app"));
}

// Wire all store side-effects once, before the first render, so the
// presentational views read slices the store is already keeping in sync.
initStore();

onRouteChange(mount);
mount();
