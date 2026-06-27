// content.js — the type-aware document pane (presentational). The store owns the
// open-doc view-model and every side-effect (the doc.get fetch, the cache-bust
// nonce, the revision/last-updated bumps, and the live doc.changed refresh); this
// component just reads selectOpenDoc(state) and renders by type:
//   markdown -> the store's fetched markdown, via marked/dompurify, rendered inline
//   html     -> an <iframe src=/api/doc/raw?...&v=<nonce>> (never via doc.get)
// The pane carries data-revision / data-last-updated; the refresh button dispatches
// the store's refreshOpenDoc action (re-fetch markdown / bump the iframe nonce).
import { html } from "htm/preact";
import { marked } from "marked";
import DOMPurify from "dompurify";
import { recordError } from "./rpc.js";
import { useStore, selectOpenDoc, refreshOpenDoc } from "./store.js";

// rawURL builds the /api/doc/raw target for an html doc, URL-encoding path and
// worktree and appending the cache-bust nonce. Omits an empty worktree. hostId
// routes the raw fetch to the right backend (the server reads hostId — see
// proxy.go handleDocRawProxy); omitted when empty so a single-backend URL stays clean.
function rawURL(project, path, worktree, nonce, hostId) {
  const qs = new URLSearchParams();
  if (project) qs.set("project", project);
  qs.set("path", path);
  if (worktree) qs.set("worktree", worktree);
  qs.set("v", String(nonce));
  if (hostId) qs.set("hostId", hostId);
  return "/api/doc/raw?" + qs.toString();
}

// splitFrontmatter peels a leading YAML frontmatter block (--- … ---) off the
// markdown so it isn't rendered as raw text. Returns { meta: [{k,v}], body }.
// Best-effort flat `key: value` parse — nested YAML keys are shown by key only.
function splitFrontmatter(md) {
  if (!md.startsWith("---")) return { meta: [], body: md };
  const close = md.indexOf("\n---", 3);
  if (close === -1) return { meta: [], body: md };
  const block = md.slice(3, close);
  const rest = md.slice(close + 4).replace(/^\r?\n/, "");
  const meta = [];
  for (const line of block.split("\n")) {
    const m = line.match(/^([A-Za-z0-9_.-]+):\s*(.*)$/);
    if (!m) continue;
    let v = m[2].trim().replace(/^["']|["']$/g, "");
    if (v.length > 60) v = v.slice(0, 57) + "…";
    meta.push({ k: m[1], v });
  }
  return { meta, body: rest };
}

export function DocContent({ project, path, type, worktree, host }) {
  // The open-doc view-model is owned by the store (fetched/refreshed by its
  // route→selection orchestration + the single doc.changed subscription). Read
  // the whole slice; it re-renders only when the open doc actually changes.
  const od = useStore(selectOpenDoc);
  const { markdown, revision, lastUpdated, nonce, loading, error } = od;

  // The store derives the effective type from the open path; fall back to the
  // suffix of the prop path so the empty-pane / pre-fetch render is consistent.
  const effType =
    od.type || (path && path.endsWith(".html") ? "html" : "markdown");

  // refresh dispatches the store action (markdown re-fetch / html nonce bump).
  const refresh = () => refreshOpenDoc();

  // sanitizeMarkdown peels frontmatter, then wraps parse + sanitize in try/catch;
  // failures are recorded and surfaced inline rather than crashing the pane.
  const renderMarkdown = (body) => {
    try {
      return DOMPurify.sanitize(marked.parse(body));
    } catch (e) {
      recordError("markdown", e);
      return '<p style="color:red">Failed to render markdown.</p>';
    }
  };

  // Empty path -> placeholder (still carries the data-revision / -last-updated hooks).
  if (!path) {
    return html`
      <article class="editor" data-revision=${revision} data-last-updated=${lastUpdated}>
        <div class="canvas">
          <div class="placeholder">
            <span class="ph-mark"></span>
            <span class="ph-text">no document open</span>
            <span class="ph-hint">choose a doc from the explorer on the left</span>
          </div>
        </div>
      </article>
    `;
  }

  const url = rawURL(project, path, worktree, nonce, host);
  const fileName = path.split("/").pop();
  const dirPart = path.slice(0, path.length - fileName.length);
  const fm = effType === "markdown" ? splitFrontmatter(markdown) : null;

  // toolbar — breadcrumb path + filename on the left, actions on the right.
  const toolbar = html`
    <div class="toolbar">
      <div class="crumb">
        <span class="crumb-path">${dirPart}</span>
        <span class="crumb-name">${fileName}</span>
      </div>
      <div class="toolbar-spacer"></div>
      ${lastUpdated &&
      html`<span class="stamp">rev ${revision} · ${lastUpdated.slice(11, 19)}</span>`}
      ${effType === "html" &&
      html`<a class="tbtn" target="_blank" rel="noopener" href=${url}>open ↗</a>`}
      <button class="tbtn" data-testid="doc-refresh" onClick=${refresh}>↻ refresh</button>
    </div>
  `;

  return html`
    <article class="editor" data-revision=${revision} data-last-updated=${lastUpdated}>
      ${toolbar}
      ${error && html`<div class="pane-error">${error}</div>`}
      ${effType === "html"
        ? html`
            <div class="canvas is-html">
              <iframe
                class="doc-iframe"
                data-testid="doc-iframe"
                src=${url}
                onError=${() => recordError("iframe", "iframe load failed: " + path)}
              ></iframe>
            </div>
          `
        : html`
            <div class="canvas">
              ${loading && html`<div class="loading">Loading…</div>`}
              ${!loading &&
              html`
                ${fm.meta.length > 0 &&
                html`<div class="frontmatter">
                  <div class="fm-inner">
                    ${fm.meta.map(
                      (m) => html`<span class="fm-tag" key=${m.k}><b>${m.k}</b> ${m.v}</span>`
                    )}
                  </div>
                </div>`}
                <div class="prose" dangerouslySetInnerHTML=${{ __html: renderMarkdown(fm.body) }} />
              `}
            </div>
          `}
    </article>
  `;
}
