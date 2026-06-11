// doc.js — doc viewer with live reload on doc.changed events.
// Lists docs via doc.list, renders markdown via marked, and re-fetches
// the open document when a matching doc.changed notification arrives.
import { useState, useEffect, useRef } from "preact/hooks";
import { html } from "htm/preact";
import { marked } from "marked";
import DOMPurify from "dompurify";
import { call, on } from "./rpc.js";
import { setHash } from "./router.js";

export function DocView({ params }) {
  const project = params.get("project") || "";
  const path = params.get("path") || "";
  const worktree = params.get("worktree") || "";

  const [docs, setDocs] = useState([]);
  const [markdown, setMarkdown] = useState("");
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);

  // Track the currently open doc so the event handler closure sees updates.
  const openRef = useRef({ project, path, worktree });
  openRef.current = { project, path, worktree };

  // Fetch the doc list on mount or when project/worktree changes.
  useEffect(() => {
    const listParams = {};
    if (project) listParams.project = project;
    if (worktree) listParams.worktree = worktree;
    call("doc.list", listParams)
      .then((res) => setDocs(res || []))
      .catch((e) => setError("doc.list failed: " + e.message));
  }, [project, worktree]);

  // Fetch the doc content whenever the path changes.
  const fetchDoc = () => {
    if (!path || !project) return;
    setLoading(true);
    setError(null);
    const getParams = { project, path };
    if (worktree) getParams.worktree = worktree;
    call("doc.get", getParams)
      .then((res) => {
        setMarkdown(res.markdown || "");
        setLoading(false);
      })
      .catch((e) => {
        setError("doc.get failed: " + e.message);
        setLoading(false);
      });
  };

  useEffect(fetchDoc, [project, path, worktree]);

  // Subscribe to doc.changed for live reload.
  useEffect(() => {
    const off = on("doc.changed", (ev) => {
      const open = openRef.current;
      if (!open.path) return;
      if (ev.project !== open.project) return;
      if (ev.path !== open.path) return;
      // A missing worktree on the open doc matches any worktree.
      if (open.worktree && ev.worktree && ev.worktree !== open.worktree) return;
      // Match — re-fetch the doc.
      fetchDoc();
    });
    return off;
  }, [project, path, worktree]);

  // Navigate to a doc when clicking in the picker.
  const selectDoc = (docPath) => {
    const qs = new URLSearchParams();
    if (project) qs.set("project", project);
    qs.set("path", docPath);
    if (worktree) qs.set("worktree", worktree);
    setHash("doc", qs);
  };

  return html`
    <section>
      <h1>Docs</h1>
      ${error && html`<p style=${{ color: "red" }}>${error}</p>`}

      <div style=${{ display: "flex", gap: "2rem" }}>
        <aside style=${{ minWidth: "220px", maxWidth: "300px" }}>
          <h3>Documents</h3>
          ${docs.length === 0 && html`<p><small>No docs found.</small></p>`}
          <ul style=${{ listStyle: "none", padding: 0 }}>
            ${docs.map(
              (d) => html`
                <li key=${d.path} style=${{ marginBottom: "0.25rem" }}>
                  <a
                    href="#"
                    onClick=${(e) => {
                      e.preventDefault();
                      selectDoc(d.path);
                    }}
                    style=${{
                      fontWeight: d.path === path ? "bold" : "normal",
                    }}
                  >
                    ${d.path}
                  </a>
                </li>
              `
            )}
          </ul>
        </aside>

        <article style=${{ flex: 1 }}>
          ${loading && html`<p>Loading...</p>`}
          ${!loading &&
          path &&
          html`<div dangerouslySetInnerHTML=${{ __html: DOMPurify.sanitize(marked.parse(markdown)) }} />`}
          ${!loading &&
          !path &&
          html`<p><em>Select a document from the list.</em></p>`}
        </article>
      </div>
    </section>
  `;
}
