// explorer.js — the planning-docs explorer shell. Composes the project switcher,
// the doc tree (left), and the type-aware content pane (right) into a two-pane
// layout, with all view state living in the hash (#/explore?project=…&path=…&worktree=…).
//
// It is presentational: the store owns project.list (with the registry fetch +
// reconnect re-fetch) and the connection slice. The shell reads projects / the
// active project / conn via useStore and routes selection through thin actions.
// An empty registry renders a friendly empty-state, not an error. The header
// hosts the switcher + a small connection indicator.
import { html } from "htm/preact";
import {
  useStore,
  selectProjects,
  selectActiveProject,
  selectConn,
  selectProject,
  selectDoc,
} from "./store.js";
import { DocTree } from "./tree.js";
import { DocContent } from "./content.js";

// deriveType maps a doc path suffix to the DocContent type. We don't thread the
// type through the hash — the suffix is authoritative.
function deriveType(path) {
  return path && path.endsWith(".html") ? "html" : "markdown";
}

// ConnIndicator — a small WS connection dot + label. Reads the store's conn slice
// (the store mirrors rpc.onStatus into it) and maps the raw status to a human
// label. Carries data-conn-status (raw) for assertions.
export function ConnIndicator() {
  const { status } = useStore(selectConn);

  // Map the raw rpc.js status to a human label (the dot color is CSS-driven off
  // data-conn-status — see .conn in app.css).
  const label =
    status === "open"
      ? "connected"
      : status === "closed"
        ? "closed"
        : "reconnecting";

  return html`
    <span class="conn" data-testid="conn-indicator" data-conn-status=${status}>
      <span class="conn-dot"></span>
      <span>${label}</span>
    </span>
  `;
}

export function Explorer({ params }) {
  const path = params.get("path") || "";
  const worktree = params.get("worktree") || "";

  // projects + the active project + conn all come from the store (it owns the
  // project.list fetch, the reconnect re-fetch, and the selection mirror). The
  // store's selectActiveProject resolves the hash project, else the first project
  // — the same default this shell used to compute locally.
  const projects = useStore(selectProjects);
  const activeProject = useStore(selectActiveProject);
  const conn = useStore(selectConn);

  // Empty-state vs cold-load: only show the no-projects empty-state once the
  // socket is open and the registry came back empty (so a connecting cold load
  // doesn't flash the empty-state before project.list resolves).
  const loaded = conn.status === "open";

  // Selecting a project routes to a fresh explore view (clears path) via the
  // store's thin action (it setHashes; the store's onRouteChange does the rest).
  const onPickProject = (id) => {
    selectProject(id);
  };

  // Selecting a doc leaf routes to it via the store's thin action (it preserves
  // the active project + worktree from the current selection, then setHashes).
  const onSelectDoc = (p) => {
    selectDoc(p);
  };

  // The topbar is constant across states (empty / populated) so the app shell
  // never collapses — only the workbench body below it changes.
  const topbar = html`
    <header class="topbar">
      <div class="brand">
        <span class="brand-mark"></span>
        <span class="brand-name">auto<span class="dot">·</span>docs</span>
        <span class="brand-sub">planning explorer</span>
      </div>
      <div class="topbar-spacer"></div>
      ${projects.length > 0 &&
      html`
        <select
          class="switcher"
          data-testid="project-switcher"
          value=${activeProject}
          onChange=${(e) => onPickProject(e.target.value)}
        >
          ${projects.map(
            (p) => html`
              <option key=${p.id} value=${p.id} data-project=${p.id}>
                ${p.name || p.id}
              </option>
            `
          )}
        </select>
      `}
      <${ConnIndicator} />
    </header>
  `;

  // shell wraps the topbar + a body region; body varies by state.
  const shell = (body) => html`<div class="shell">${topbar}${body}</div>`;

  // Empty registry -> a friendly empty-state, NOT an error. (project.list errors
  // are recorded at the rpc.js layer and surfaced in /debug's error log.)
  if (loaded && projects.length === 0) {
    return shell(html`
      <div class="workbench">
        <article class="editor">
          <div class="placeholder" data-testid="no-projects">
            <span class="ph-mark"></span>
            <span class="ph-text">no projects registered</span>
            <span class="ph-hint">add one to the project registry to get started</span>
          </div>
        </article>
      </div>
    `);
  }

  const type = deriveType(path);

  // DocTree is keyed by project+worktree so a switch REMOUNTS it: its
  // useStore(s => selectDocs(s, project, worktree)) selector is captured once
  // (useEffect deps []), so a reused instance would keep selecting the previous
  // project/worktree's docs. Remounting re-captures the selector against the new
  // props.
  return shell(html`
    <div class="workbench">
      <${DocTree}
        key=${activeProject + "@" + worktree}
        project=${activeProject}
        worktree=${worktree}
        selected=${path}
        onSelect=${(p) => onSelectDoc(p)}
      />
      <${DocContent}
        project=${activeProject}
        path=${path}
        type=${type}
        worktree=${worktree}
      />
    </div>
  `);
}
