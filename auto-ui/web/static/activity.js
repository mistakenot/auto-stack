// activity.js — "Recent activity" feed panel for the explorer. Tracks
// doc.changed WS events received during THIS browser session (since the
// explorer mounted), newest-first, deduped per doc path (repeated edits
// collapse to one entry with an edit count). Clicking an entry navigates
// to that doc via the existing hash routing.
import { useState, useEffect, useRef } from "preact/hooks";
import { html } from "htm/preact";
import { on } from "./rpc.js";
import { parseDocChanged } from "./docevents.js";

const MAX_ENTRIES = 20;

function relativeTime(ts) {
  const delta = Math.max(0, Math.floor((Date.now() - ts) / 1000));
  if (delta < 5) return "just now";
  if (delta < 60) return delta + "s ago";
  const m = Math.floor(delta / 60);
  if (m < 60) return m + "m ago";
  const h = Math.floor(m / 60);
  return h + "h ago";
}

function fileName(path) {
  if (!path) return "";
  return path.split("/").pop();
}

function dirPart(path) {
  if (!path) return "";
  const parts = path.split("/");
  if (parts.length <= 1) return "";
  return parts.slice(0, -1).join("/") + "/";
}

export function ActivityFeed({ project, onSelectDoc }) {
  // entries: [{ path, project, ts, count }] — newest-first, deduped by path
  const [entries, setEntries] = useState([]);
  // tick counter to refresh relative times every 15s
  const [, setTick] = useState(0);

  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 15000);
    return () => clearInterval(id);
  }, []);

  // Subscribe to doc.changed; filter to the active project.
  useEffect(() => {
    const off = on("doc.changed", (ev) => {
      const c = parseDocChanged(ev);
      if (!c.path) return;
      if (c.project !== project) return;

      setEntries((prev) => {
        const existing = prev.findIndex((e) => e.path === c.path);
        const count = existing >= 0 ? prev[existing].count + 1 : 1;
        const entry = { path: c.path, project: c.project, ts: Date.now(), count };
        const next = existing >= 0
          ? [entry, ...prev.filter((_, i) => i !== existing)]
          : [entry, ...prev];
        return next.slice(0, MAX_ENTRIES);
      });
    });
    return off;
  }, [project]);

  // Clear entries when the project changes.
  const prevProject = useRef(project);
  useEffect(() => {
    if (prevProject.current !== project) {
      setEntries([]);
      prevProject.current = project;
    }
  }, [project]);

  if (entries.length === 0) return null;

  return html`
    <section class="activity" data-testid="activity-feed" aria-label="Recent activity">
      <h3 class="activity-heading">Recent activity</h3>
      <ul class="activity-list" role="list">
        ${entries.map(
          (e) => html`
            <li key=${e.path}>
              <a
                href="#"
                class="activity-item"
                data-testid="activity-item"
                data-activity-path=${e.path}
                role="link"
                onClick=${(ev) => {
                  ev.preventDefault();
                  onSelectDoc(e.path);
                }}
              >
                <span class="activity-dot"></span>
                <span class="activity-body">
                  <span class="activity-name">${fileName(e.path)}</span>
                  <span class="activity-dir">${dirPart(e.path)}</span>
                </span>
                <span class="activity-meta">
                  <span class="activity-time">${relativeTime(e.ts)}</span>
                  ${e.count > 1 &&
                  html`<span class="activity-count" aria-label="${e.count} edits">${e.count}x</span>`}
                </span>
              </a>
            </li>
          `
        )}
      </ul>
    </section>
  `;
}
