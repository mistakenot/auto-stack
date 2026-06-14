// explorer.js — the planning-docs explorer shell. Composes the project switcher,
// the doc tree (left), and the type-aware content pane (right) into a two-pane
// layout, with all view state living in the hash (#/explore?project=…&path=…&worktree=…).
//
// On mount it gates on whenOpen() (cold-load readiness) then fetches project.list;
// it re-fetches on a fresh reconnect ("open" transition). An empty registry renders
// a friendly empty-state, not an error. The header hosts the switcher + a small
// connection indicator. This is the static explorer — no doc.changed wiring (026).
import { useState, useEffect, useRef } from "preact/hooks";
import { html } from "htm/preact";
import { setHash } from "./router.js";
import { call, onStatus, whenOpen, recordError } from "./rpc.js";
import { setUIState } from "./uistate.js";
import { DocTree } from "./tree.js";
import { DocContent } from "./content.js";

// deriveType maps a doc path suffix to the DocContent type. We don't thread the
// type through the hash — the suffix is authoritative.
function deriveType(path) {
  return path && path.endsWith(".html") ? "html" : "markdown";
}

// ConnIndicator — a small WS connection dot + label, distilled from the demo
// Dashboard's ping/WS status code. Subscribes to onStatus and maps the raw
// status to a human label. Carries data-conn-status (raw) for assertions.
export function ConnIndicator() {
  const [status, setStatus] = useState("connecting");
  useEffect(() => {
    const off = onStatus(setStatus);
    return off;
  }, []);

  // Map the raw rpc.js status to a label + dot color.
  const label =
    status === "open"
      ? "connected"
      : status === "closed"
        ? "closed"
        : "reconnecting";
  const color =
    status === "open" ? "#2ecc40" : status === "closed" ? "#ff4136" : "#ff851b";

  return html`
    <span
      data-testid="conn-indicator"
      data-conn-status=${status}
      style=${{ display: "inline-flex", alignItems: "center", gap: "0.35rem" }}
    >
      <span
        style=${{
          display: "inline-block",
          width: "0.6rem",
          height: "0.6rem",
          borderRadius: "50%",
          background: color,
        }}
      ></span>
      <small>${label}</small>
    </span>
  `;
}

export function Explorer({ params }) {
  const project = params.get("project") || "";
  const path = params.get("path") || "";
  const worktree = params.get("worktree") || "";

  const [projects, setProjects] = useState([]);
  const [error, setError] = useState(null);
  const [loaded, setLoaded] = useState(false);

  // fetchProjects gates on whenOpen() so a cold load (explorer is the landing
  // view) doesn't reject "not connected" before the socket is open.
  const fetchProjects = async () => {
    setError(null);
    try {
      await whenOpen();
      const res = (await call("project.list")) || [];
      setProjects(res);
      setLoaded(true);
    } catch (e) {
      recordError("project.list", e);
      setError(
        "project.list failed: " + (e && e.message ? e.message : String(e))
      );
    }
  };

  useEffect(() => {
    fetchProjects();
  }, []);

  // Reconnect self-heal: re-fetch the registry on a FRESH transition to "open"
  // (after we were not open). Track the previous status so the initial "open"
  // — already covered by the mount fetch — doesn't double-fetch.
  const prevStatus = useRef(null);
  useEffect(() => {
    const off = onStatus((s) => {
      const was = prevStatus.current;
      prevStatus.current = s;
      if (s === "open" && was !== null && was !== "open") fetchProjects();
    });
    return off;
  }, []);

  // The active project defaults to the hash project, else the first project.
  const activeProject =
    project || (projects.length > 0 ? projects[0].id : "");

  // Keep the cross-route snapshot's project in sync for /debug.
  useEffect(() => {
    if (activeProject) setUIState({ project: activeProject });
  }, [activeProject]);

  // Selecting a project routes to a fresh explore view (clears path).
  const onPickProject = (id) => {
    setUIState({ project: id });
    setHash("explore", new URLSearchParams({ project: id }));
  };

  // Selecting a doc leaf routes to it, preserving the active project + worktree.
  const onSelectDoc = (p) => {
    const next = { project: activeProject, path: p };
    if (worktree) next.worktree = worktree;
    setHash("explore", new URLSearchParams(next));
  };

  const header = html`
    <header
      style=${{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: "1rem",
        marginBottom: "0.75rem",
      }}
    >
      <div style=${{ display: "flex", alignItems: "center", gap: "0.75rem" }}>
        <strong>auto-ui</strong>
        ${projects.length > 0 &&
        html`
          <select
            data-testid="project-switcher"
            value=${activeProject}
            onChange=${(e) => onPickProject(e.target.value)}
          >
            ${projects.map(
              (p) => html`
                <option key=${p.id} value=${p.id} data-project=${p.id}>
                  ${p.name}
                </option>
              `
            )}
          </select>
        `}
      </div>
      <${ConnIndicator} />
    </header>
  `;

  // Inline error (e.g. project.list failed) — not the empty-state.
  if (error) {
    return html`
      <section>
        ${header}
        <p style=${{ color: "red" }}>${error}</p>
      </section>
    `;
  }

  // Empty registry -> a friendly empty-state, NOT an error.
  if (loaded && projects.length === 0) {
    return html`
      <section>
        ${header}
        <p data-testid="no-projects">
          <em>No projects registered. Add one to the project registry to get started.</em>
        </p>
      </section>
    `;
  }

  const type = deriveType(path);

  return html`
    <section>
      ${header}
      <div style=${{ display: "flex", gap: "1rem", alignItems: "flex-start" }}>
        <div style=${{ flex: "0 0 18rem", minWidth: "14rem" }}>
          <${DocTree}
            project=${activeProject}
            worktree=${worktree}
            selected=${path}
            onSelect=${(p) => onSelectDoc(p)}
          />
        </div>
        <div style=${{ flex: "1 1 auto", minWidth: 0 }}>
          <${DocContent}
            project=${activeProject}
            path=${path}
            type=${type}
            worktree=${worktree}
          />
        </div>
      </div>
    </section>
  `;
}
