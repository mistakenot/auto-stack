// tree.js — the planning-docs nav tree. Fetches the flat doc.list for a project
// and groups it CLIENT-SIDE by path prefix (the backend stays generic) into a
// collapsible tree. Each leaf routes to a doc via onSelect(path, type).
//
// Grouping (pure string parsing on path, always under docs/):
//   docs/tasks/NNN-slug/<file>  -> Tasks       -> NNN-slug   -> file
//   docs/epics/<file> | phase*/  -> Epics       -> (phase dir)
//   docs/research/<file>         -> Research
//   docs/reference/<file>        -> Reference
//   docs/experiments/<dir>/<f>   -> Experiments -> experiment dir
//   docs/spikes/<file>           -> Spikes
//   docs/<file> (direct child)   -> Root docs
//   docs/<other>/...             -> generic group named after that segment
import { useState, useEffect, useRef } from "preact/hooks";
import { html } from "htm/preact";
import { call, on, onStatus, whenOpen, recordError } from "./rpc.js";
import { parseDocChanged } from "./docevents.js";
import { setUIState } from "./uistate.js";

// How many Tasks entries the tree shows before the "show more" toggle.
const TASKS_DEFAULT_LIMIT = 10;

// taskId pulls the leading integer out of a "NNN-slug" task folder name (e.g.
// "023-reflect-miner-queue" -> 23). Unnumbered names return -1 so they sink to
// the bottom of the descending sort.
function taskId(name) {
  const m = name.match(/^(\d+)/);
  return m ? parseInt(m[1], 10) : -1;
}

// Stable display order for the known top-level groups; unknown segments are
// appended afterwards in discovery order.
const GROUP_ORDER = [
  "Tasks",
  "Epics",
  "Research",
  "Reference",
  "Experiments",
  "Spikes",
  "Root docs",
];

// groupDocs turns a flat [{id, path, type}] list into an ordered tree:
//   [{ name, subgroups: [{ name, leaves: [{path, type}] }], leaves: [...] }]
// Groups with a sub-grouping (Tasks/Experiments/Epics-phases) put files under
// subgroups; flat groups put files directly in leaves.
function groupDocs(docs) {
  const groups = new Map(); // name -> { name, subgroups: Map, leaves: [] }

  const group = (name) => {
    if (!groups.has(name)) {
      groups.set(name, { name, subgroups: new Map(), leaves: [] });
    }
    return groups.get(name);
  };
  const sub = (g, name) => {
    if (!g.subgroups.has(name)) g.subgroups.set(name, { name, leaves: [] });
    return g.subgroups.get(name);
  };

  for (const d of docs) {
    const leaf = { path: d.path, type: d.type };
    // Strip the leading docs/ segment; everything is rooted there.
    const rel = d.path.replace(/^docs\//, "");
    const seg = rel.split("/");
    const head = seg[0];

    if (head === "tasks" && seg.length >= 3) {
      sub(group("Tasks"), seg[1]).leaves.push(leaf);
    } else if (head === "epics") {
      const g = group("Epics");
      if (seg.length >= 3 && seg[1].startsWith("phase")) {
        sub(g, seg[1]).leaves.push(leaf);
      } else {
        g.leaves.push(leaf);
      }
    } else if (head === "research") {
      group("Research").leaves.push(leaf);
    } else if (head === "reference") {
      group("Reference").leaves.push(leaf);
    } else if (head === "experiments" && seg.length >= 3) {
      sub(group("Experiments"), seg[1]).leaves.push(leaf);
    } else if (head === "experiments") {
      group("Experiments").leaves.push(leaf);
    } else if (head === "spikes") {
      group("Spikes").leaves.push(leaf);
    } else if (seg.length === 1) {
      group("Root docs").leaves.push(leaf);
    } else {
      // Unknown first segment after docs/ -> a generic group named for it.
      group(head).leaves.push(leaf);
    }
  }

  // Materialize in a stable order: known groups first, then any extras.
  const names = [...groups.keys()];
  names.sort((a, b) => {
    const ia = GROUP_ORDER.indexOf(a);
    const ib = GROUP_ORDER.indexOf(b);
    if (ia === -1 && ib === -1) return a.localeCompare(b);
    if (ia === -1) return 1;
    if (ib === -1) return -1;
    return ia - ib;
  });

  return names.map((name) => {
    const g = groups.get(name);
    // Tasks sort by biggest task id first (newest), unnumbered names last;
    // every other group keeps a plain alphabetical order.
    const subNames =
      name === "Tasks"
        ? [...g.subgroups.keys()].sort((a, b) => {
            const d = taskId(b) - taskId(a);
            return d !== 0 ? d : a.localeCompare(b);
          })
        : [...g.subgroups.keys()].sort((a, b) => a.localeCompare(b));
    return {
      name: g.name,
      subgroups: subNames.map((n) => g.subgroups.get(n)),
      leaves: g.leaves,
    };
  });
}

// Leaf renders one selectable doc node carrying the acceptance attributes. The
// row's left padding encodes its depth (group=1, subgroup=2 levels of indent).
function Leaf({ leaf, selected, onSelect, depth }) {
  const isSel = leaf.path === selected;
  return html`
    <li>
      <a
        href="#"
        class=${"row leaf" + (isSel ? " active" : "")}
        data-testid="doc-node"
        data-doc-path=${leaf.path}
        data-doc-type=${leaf.type}
        style=${{ paddingLeft: 0.45 + depth * 0.78 + "rem" }}
        onClick=${(e) => {
          e.preventDefault();
          onSelect(leaf.path, leaf.type);
        }}
      >
        <span class=${"ftype ftype-" + (leaf.type || "markdown")}></span>
        <span class="label">${leaf.path.split("/").pop()}</span>
      </a>
    </li>
  `;
}

// Collapsible holds a label with open/closed state and renders children when
// open. Folded by default; `defaultOpen` (set when the group holds the selected
// doc) starts it expanded so a deep-linked file stays visible. Clicking toggles.
// `kind` selects group vs subgroup styling; `depth` drives indentation.
function Collapsible({ label, kind, depth, defaultOpen, children }) {
  const [open, setOpen] = useState(!!defaultOpen);
  return html`
    <li>
      <button
        type="button"
        class=${"row row-" + kind}
        style=${{ paddingLeft: 0.45 + depth * 0.78 + "rem" }}
        onClick=${() => setOpen(!open)}
      >
        <span class=${"caret" + (open ? " open" : "")}>▸</span>
        <span class="label">${label}</span>
      </button>
      ${open && html`<ul>${children}</ul>`}
    </li>
  `;
}

// GroupBody renders a group's subgroups (then its direct leaves). When `limit`
// is set (the Tasks group), only the first `limit` subgroups show by default,
// with a "show N more" toggle. The cap is overridden when the selected doc lives
// in an otherwise-hidden subgroup, so a deep-link stays visible.
function GroupBody({ group, selected, onSelect, subHasSelected, limit }) {
  const [showAll, setShowAll] = useState(false);
  const subs = group.subgroups;
  const selIdx = subs.findIndex(subHasSelected);
  const forceAll = limit > 0 && selIdx >= limit;
  const capped = limit > 0 && !showAll && !forceAll ? subs.slice(0, limit) : subs;
  const hidden = subs.length - capped.length;
  return html`
    ${capped.map(
      (sg) => html`
        <${Collapsible} key=${sg.name} label=${sg.name} kind="sub" depth=${1} defaultOpen=${subHasSelected(sg)}>
          ${sg.leaves.map(
            (leaf) => html`
              <${Leaf} key=${leaf.path} leaf=${leaf} selected=${selected} onSelect=${onSelect} depth=${2} />
            `
          )}
        <//>
      `
    )}
    ${hidden > 0 &&
    html`
      <li>
        <button
          type="button"
          class="row row-more"
          style=${{ paddingLeft: 0.45 + 1 * 0.78 + "rem" }}
          onClick=${() => setShowAll(true)}
        >
          <span class="more-label">show ${hidden} more…</span>
        </button>
      </li>
    `}
    ${group.leaves.map(
      (leaf) => html`
        <${Leaf} key=${leaf.path} leaf=${leaf} selected=${selected} onSelect=${onSelect} depth=${1} />
      `
    )}
  `;
}

export function DocTree({ project, worktree, selected, onSelect }) {
  const [docs, setDocs] = useState([]);
  const [error, setError] = useState(null);

  // Fetch the flat doc.list. Gate on whenOpen() so a cold load doesn't reject
  // "not connected" before the socket is open. Omit an empty worktree.
  const fetchList = async () => {
    setError(null);
    try {
      await whenOpen();
      const params = {};
      if (project) params.project = project;
      if (worktree) params.worktree = worktree;
      const res = (await call("doc.list", params)) || [];
      setDocs(res);
      setUIState({ docCount: res.length });
    } catch (e) {
      recordError("doc.list", e);
      setError("doc.list failed: " + (e && e.message ? e.message : String(e)));
    }
  };

  // Re-fetch on mount and whenever project/worktree change.
  useEffect(() => {
    fetchList();
  }, [project, worktree]);

  // Reconnect self-heal: re-list on a FRESH transition to "open" (i.e. after we
  // were not open). Track the previous status so the initial "open" — already
  // handled by the mount fetch — doesn't double-fetch.
  const prevStatus = useRef(null);
  useEffect(() => {
    const off = onStatus((s) => {
      const was = prevStatus.current;
      prevStatus.current = s;
      if (s === "open" && was !== null && was !== "open") fetchList();
    });
    return off;
  }, [project, worktree]);

  // Live nav-tree refresh (026). Keep a ref snapshot of the known paths so the
  // doc.changed handler — registered once per {project,worktree} — can test
  // membership without re-subscribing on every fetch.
  const knownPaths = useRef(new Set());
  useEffect(() => {
    knownPaths.current = new Set(docs.map((d) => d.path));
  }, [docs]);

  // A doc.changed for the active project carrying a path the tree does NOT yet
  // know about (a newly created doc) triggers exactly one re-list so the new node
  // appears. Known-path edits need no re-list — the open-doc refresh (content.js)
  // handles those and the tree is already correct. The re-list also reconciles
  // any concurrent deletions against fresh server truth.
  useEffect(() => {
    const off = on("doc.changed", (ev) => {
      const c = parseDocChanged(ev);
      if (c.project !== project) return;
      if (!c.path || knownPaths.current.has(c.path)) return;
      fetchList();
    });
    return off;
  }, [project, worktree]);

  const groups = groupDocs(docs);

  // Folded-by-default, but keep the path to the open doc expanded so a deep-link
  // (or a freshly selected file) stays visible on mount.
  const subHasSelected = (sg) => sg.leaves.some((l) => l.path === selected);
  const groupHasSelected = (g) =>
    g.leaves.some((l) => l.path === selected) || g.subgroups.some(subHasSelected);

  return html`
    <aside class="sidebar">
      <div class="rail-head">
        <span class="rail-title">Explorer</span>
        <span class="rail-count">${docs.length}</span>
      </div>
      <nav class="tree" data-doc-count=${docs.length}>
        ${error && html`<p class="tree-msg err">${error}</p>`}
        ${docs.length === 0 && !error && html`<p class="tree-msg">No docs found.</p>`}
        <ul>
          ${groups.map(
            (g) => html`
              <${Collapsible} key=${g.name} label=${g.name} kind="group" depth=${0} defaultOpen=${groupHasSelected(g)}>
                <${GroupBody}
                  group=${g}
                  selected=${selected}
                  onSelect=${onSelect}
                  subHasSelected=${subHasSelected}
                  limit=${g.name === "Tasks" ? TASKS_DEFAULT_LIMIT : 0}
                />
              <//>
            `
          )}
        </ul>
      </nav>
    </aside>
  `;
}
