// content.js — the type-aware document pane. Generalizes doc.js's render,
// dispatching on the entry's type (or the path's .md/.html suffix):
//   markdown -> doc.get + marked/dompurify, rendered inline
//   html     -> an <iframe src=/api/doc/raw?...&v=<nonce>> (never via doc.get)
// The pane carries data-revision / data-last-updated; a refresh button forces a
// re-fetch (markdown) or bumps the iframe cache-bust nonce (html). There is NO
// doc.changed subscription here — this is the static explorer (liveness is 026).
import { useState, useEffect, useRef } from "preact/hooks";
import { html } from "htm/preact";
import { marked } from "marked";
import DOMPurify from "dompurify";
import { call, on, whenOpen, recordError } from "./rpc.js";
import { matchesDoc } from "./docevents.js";
import { setUIState } from "./uistate.js";

// resolveType derives the effective type: an explicit type wins, else fall back
// to the path suffix.
function resolveType(type, path) {
  if (type === "markdown" || type === "html") return type;
  if (path.endsWith(".html")) return "html";
  return "markdown"; // default / .md
}

// rawURL builds the /api/doc/raw target for an html doc, URL-encoding path and
// worktree and appending the cache-bust nonce. Omits an empty worktree.
function rawURL(project, path, worktree, nonce) {
  const qs = new URLSearchParams();
  if (project) qs.set("project", project);
  qs.set("path", path);
  if (worktree) qs.set("worktree", worktree);
  qs.set("v", String(nonce));
  return "/api/doc/raw?" + qs.toString();
}

export function DocContent({ project, path, type, worktree }) {
  const [markdown, setMarkdown] = useState("");
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);

  // Monotonic cache-bust nonce: incremented on every (re-)fetch / refresh so the
  // iframe v= changes and the browser re-requests. Starts non-zero.
  const [nonce, setNonce] = useState(1);

  // Revision counter — bumped on each (re-)fetch/nonce-bump and surfaced via
  // data-revision. A ref so closures always see the latest value.
  const revision = useRef(0);
  const [lastUpdated, setLastUpdated] = useState("");

  const effType = resolveType(type, path);

  // bump records one (re-)fetch/render: advance revision + last-updated and push
  // the snapshot into uiState for the /debug current-state section.
  const bump = () => {
    revision.current += 1;
    const now = new Date().toISOString();
    setLastUpdated(now);
    setUIState({ path, type: effType, revision: revision.current, lastUpdated: now });
  };

  // fetchMarkdown loads + renders the markdown body, recording any parse/sanitize
  // failure via recordError (these never reach rpc.js).
  const fetchMarkdown = async () => {
    if (!path || !project) return;
    setLoading(true);
    setError(null);
    try {
      await whenOpen();
      const params = { project, path };
      if (worktree) params.worktree = worktree;
      const res = await call("doc.get", params);
      setMarkdown(res && res.markdown ? res.markdown : "");
      setLoading(false);
      bump();
    } catch (e) {
      recordError("markdown", e);
      setError("doc.get failed: " + (e && e.message ? e.message : String(e)));
      setLoading(false);
    }
  };

  // For html docs there is no fetch — bumping the nonce re-points the iframe.
  const refresh = () => {
    if (effType === "html") {
      setNonce((n) => n + 1);
      bump();
    } else {
      fetchMarkdown();
    }
  };

  // (Re-)fetch markdown when the markdown target changes. For html docs the
  // initial render already bumps revision (below), so this effect is markdown-only.
  useEffect(() => {
    if (effType === "markdown") fetchMarkdown();
    // eslint-disable-next-line
  }, [project, path, worktree, effType]);

  // For html docs, bump revision/last-updated once when the target changes so the
  // pane reflects the active doc even though there's no fetch.
  useEffect(() => {
    if (effType === "html" && path) bump();
    // eslint-disable-next-line
  }, [project, path, worktree, effType]);

  // Live refresh (026): when a doc.changed matching THIS open doc arrives, apply
  // immediately — markdown re-fetches + re-renders, html bumps the iframe nonce.
  // Re-subscribes when the open target changes so refresh() never goes stale.
  useEffect(() => {
    const target = { project, path, worktree };
    const off = on("doc.changed", (ev) => {
      if (!matchesDoc(ev, target)) return;
      refresh();
    });
    return off;
    // eslint-disable-next-line
  }, [project, path, worktree, effType]);

  // sanitizeMarkdown wraps parse + sanitize in try/catch; failures are recorded
  // and surfaced inline rather than crashing the pane.
  const renderMarkdown = () => {
    try {
      return DOMPurify.sanitize(marked.parse(markdown));
    } catch (e) {
      recordError("markdown", e);
      return '<p style="color:red">Failed to render markdown.</p>';
    }
  };

  // Empty path -> placeholder.
  if (!path) {
    return html`
      <article data-revision=${revision.current} data-last-updated=${lastUpdated}>
        <p><em>Select a document.</em></p>
      </article>
    `;
  }

  const url = rawURL(project, path, worktree, nonce);

  return html`
    <article data-revision=${revision.current} data-last-updated=${lastUpdated}>
      <div style=${{ marginBottom: "0.5rem" }}>
        <button data-testid="doc-refresh" onClick=${refresh}>Refresh</button>
      </div>
      ${error && html`<p style=${{ color: "red" }}>${error}</p>`}
      ${effType === "html"
        ? html`
            <div>
              <p>
                <a target="_blank" rel="noopener" href=${url}>open in new tab</a>
              </p>
              <iframe
                data-testid="doc-iframe"
                src=${url}
                onError=${() => recordError("iframe", "iframe load failed: " + path)}
                style=${{ width: "100%", height: "75vh", border: "1px solid #ccc" }}
              ></iframe>
            </div>
          `
        : html`
            ${loading && html`<p>Loading...</p>`}
            ${!loading &&
            html`<div
              dangerouslySetInnerHTML=${{ __html: renderMarkdown() }}
            />`}
          `}
    </article>
  `;
}
