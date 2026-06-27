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
import { useStore, selectDocs, selectLiveness, selectActiveHost } from "./store.js";

// How many Tasks entries the tree shows before the "show more" toggle.
const TASKS_DEFAULT_LIMIT = 10;

// How long a touched node stays in its flash cycle (ms). Must be >= the CSS
// `flash-touch` animation so the .flash class isn't pulled mid-animation.
const FLASH_MS = 1200;

// flashTokensForPath maps a changed doc path to the set of node tokens that
// should flash: the leaf itself plus its ancestor chain (task-root subgroup and
// top-level group). Mirrors the grouping in groupDocs so the tokens line up with
// the `group:`/`sub:`/`leaf:` tokens the renderer computes per node. The group is
// included so a collapsed group still signals "something under me just changed".
function flashTokensForPath(path) {
  if (!path) return [];
  const rel = path.replace(/^docs\//, "");
  const seg = rel.split("/");
  const head = seg[0];
  const tokens = ["leaf:" + path];

  if (head === "tasks" && seg.length >= 3) {
    tokens.push("group:Tasks", "sub:Tasks/" + seg[1]);
  } else if (head === "epics") {
    tokens.push("group:Epics");
    if (seg.length >= 3 && seg[1].startsWith("phase")) tokens.push("sub:Epics/" + seg[1]);
  } else if (head === "research") {
    tokens.push("group:Research");
  } else if (head === "reference") {
    tokens.push("group:Reference");
  } else if (head === "experiments" && seg.length >= 3) {
    tokens.push("group:Experiments", "sub:Experiments/" + seg[1]);
  } else if (head === "experiments") {
    tokens.push("group:Experiments");
  } else if (head === "spikes") {
    tokens.push("group:Spikes");
  } else if (seg.length === 1) {
    tokens.push("group:Root docs");
  } else {
    tokens.push("group:" + head);
  }
  return tokens;
}

// expandTokensForPath returns the ancestor group/subgroup tokens that must be
// OPENED to reveal a doc at `path`. The leaf needs no token — it renders whenever
// its enclosing subgroup/group is open. Derived from flashTokensForPath so the
// reveal targets line up exactly with the nodes the renderer keys on.
function expandTokensForPath(path) {
  return flashTokensForPath(path).filter(
    (t) => t.startsWith("group:") || t.startsWith("sub:")
  );
}

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
    const leaf = { path: d.path, type: d.type, meta: d.meta };
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
function Leaf({ leaf, selected, onSelect, depth, flashId, project }) {
  const isSel = leaf.path === selected;
  const meta = leaf.meta;

  // Liveness: read from the store via selectLiveness selector
  const lv = useStore((s) => selectLiveness(s, project, meta?.branch));

  // Format liveness age display
  const formatAge = (ms) => {
    if (ms < 0) ms = 0;
    const secs = Math.floor(ms / 1000);
    if (secs < 60) return secs + "s ago";
    return Math.floor(secs / 60) + "m ago";
  };

  return html`
    <li>
      <a
        href="#"
        class=${"row leaf" + (isSel ? " active" : "")}
        data-testid="doc-node"
        data-doc-path=${leaf.path}
        data-doc-type=${leaf.type}
        data-plan-status=${meta?.status || null}
        data-review-state=${meta?.reviewState || null}
        data-liveness=${lv ? (lv.active ? "active" : "idle") : null}
        style=${{ paddingLeft: 0.45 + depth * 0.78 + "rem" }}
        onClick=${(e) => {
          e.preventDefault();
          onSelect(leaf.path, leaf.type);
        }}
      >
        <span class=${"ftype ftype-" + (leaf.type || "markdown")}></span>
        ${meta?.status === "executing" && html`<span class="plan-spinner" data-plan-status="executing"></span>`}
        ${meta?.status === "merged" && html`<span class="plan-status-merged">${"✓"}</span>`}
        ${meta?.reviewState && html`<span class="review-pill" data-review-state=${meta.reviewState}>${meta.reviewState}</span>`}
        <span class=${"label" + (flashId ? " flash" : "")} key=${"lbl" + (flashId || 0)}>${leaf.path.split("/").pop()}</span>
        ${lv && html`<span class=${"liveness " + (lv.active ? "liveness-active" : "liveness-idle")} data-liveness=${lv.active ? "active" : "idle"}>${lv.active ? "● active " : "○ idle "}${formatAge(lv.ageMs)}</span>`}
      </a>
    </li>
  `;
}

// Collapsible holds a label with open/closed state and renders children when
// open. Folded by default; `defaultOpen` (set when the group holds the selected
// doc) starts it expanded so a deep-linked file stays visible. Clicking toggles.
// `kind` selects group vs subgroup styling; `depth` drives indentation.
function Collapsible({ label, kind, depth, defaultOpen, forceOpen, flashId, children }) {
  const [open, setOpen] = useState(!!defaultOpen);
  // Live "reveal": when a new doc is created under this node, forceOpen flips to
  // true and we open. Acts on forceOpen's rising edge only (deps=[forceOpen]), so
  // the user can still collapse the node again afterward.
  useEffect(() => {
    if (forceOpen) setOpen(true);
  }, [forceOpen]);
  return html`
    <li>
      <button
        type="button"
        class=${"row row-" + kind}
        style=${{ paddingLeft: 0.45 + depth * 0.78 + "rem" }}
        onClick=${() => setOpen(!open)}
      >
        <span class=${"caret" + (open ? " open" : "")}>▸</span>
        <span class=${"label" + (flashId ? " flash" : "")} key=${"lbl" + (flashId || 0)}>${label}</span>
      </button>
      ${open && html`<ul>${children}</ul>`}
    </li>
  `;
}

// GroupBody renders a group's subgroups (then its direct leaves). When `limit`
// is set (the Tasks group), only the first `limit` subgroups show by default,
// with a "show N more" toggle. The cap is overridden when the selected doc lives
// in an otherwise-hidden subgroup, so a deep-link stays visible.
function GroupBody({ group, selected, onSelect, subHasSelected, limit, flashes, expanded, project }) {
  const [showAll, setShowAll] = useState(false);
  const subs = group.subgroups;
  const selIdx = subs.findIndex(subHasSelected);
  // Reveal past the cap: a just-created subgroup (forceOpen) beyond the limit must
  // be shown too, or the new node would re-list invisibly behind "show N more".
  const revealIdx = subs.findIndex((sg) => expanded && expanded.has("sub:" + group.name + "/" + sg.name));
  const forceAll = limit > 0 && (selIdx >= limit || revealIdx >= limit);
  const capped = limit > 0 && !showAll && !forceAll ? subs.slice(0, limit) : subs;
  const hidden = subs.length - capped.length;
  return html`
    ${capped.map(
      (sg) => html`
        <${Collapsible}
          key=${sg.name}
          label=${sg.name}
          kind="sub"
          depth=${1}
          defaultOpen=${subHasSelected(sg)}
          forceOpen=${expanded && expanded.has("sub:" + group.name + "/" + sg.name)}
          flashId=${flashes["sub:" + group.name + "/" + sg.name]}
        >
          ${sg.leaves.map(
            (leaf) => html`
              <${Leaf}
                key=${leaf.path}
                leaf=${leaf}
                selected=${selected}
                onSelect=${onSelect}
                depth=${2}
                flashId=${flashes["leaf:" + leaf.path]}
                project=${project}
              />
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
        <${Leaf}
          key=${leaf.path}
          leaf=${leaf}
          selected=${selected}
          onSelect=${onSelect}
          depth=${1}
          flashId=${flashes["leaf:" + leaf.path]}
          project=${project}
        />
      `
    )}
  `;
}

export function DocTree({ project, worktree, selected, onSelect }) {
  // The doc list is owned by the store (the doc.list fetch + docsByProject cache,
  // its reconnect re-list, and the doc.changed re-list on an unseen path all live
  // there). Read this project's cached slice; it re-renders when the list changes.
  // The host dimension isn't threaded as a prop yet (DocTree host wiring is
  // Phase 2); read the active host from the store so the cache lookup keys match
  // what fetchDocs wrote (host+project+worktree).
  const docs = useStore((s) => selectDocs(s, selectActiveHost(s), project, worktree));

  // The store's doc.changed subscription bumps lastDocChanged {path, seq} on every
  // touched path (monotonic seq). The tree consumes that single signal to drive
  // its purely-visual flash + reveal — no doc.changed subscription of its own.
  const lastDocChanged = useStore((s) => s.lastDocChanged);

  // Touch-flash state (driven by the store's lastDocChanged signal): a map of node
  // token -> monotonic flash id. The id drives a key-remount on each node's label
  // span so a CSS animation (re)plays on every touch, and lets a stale clear-timer
  // skip a superseded flash.
  const [flashes, setFlashes] = useState({});
  const flashSeq = useRef(0);

  // Force-open tokens (group:/sub:) for groups that must auto-expand to REVEAL a
  // newly-created doc. Without this, a new file re-lists into a collapsed group
  // and the user sees nothing change. Sticky once set; the user can re-collapse.
  const [expanded, setExpanded] = useState(() => new Set());

  // triggerFlash brightens the touched leaf + its ancestor chain, then schedules
  // them to fade back. Only stable setters/refs are touched, so the handler that
  // closes over this never goes stale.
  const triggerFlash = (path) => {
    const tokens = flashTokensForPath(path);
    if (tokens.length === 0) return;
    flashSeq.current += 1;
    const id = flashSeq.current;
    setFlashes((prev) => {
      const next = { ...prev };
      for (const t of tokens) next[t] = id;
      return next;
    });
    setTimeout(() => {
      setFlashes((prev) => {
        const next = { ...prev };
        // Only clear our own flash — a newer touch (different id) keeps running.
        for (const t of tokens) if (next[t] === id) delete next[t];
        return next;
      });
    }, FLASH_MS);
  };

  // Drive the live flash + reveal off the store's lastDocChanged {path, seq}
  // signal. The store bumps seq for EVERY touched path (any project) and, for an
  // unseen path under the active project, re-lists it into docsByProject. This
  // tree filters to its OWN project by reacting only when the changed path is part
  // of THIS project's docs — so a touch in another project is ignored (parity with
  // the old `c.project !== project` gate), while a node in this tree flashes + its
  // ancestors open (reveal). Because the seq bump can precede the store's async
  // re-list, the effect also re-runs on `docs` changes and fires once per new seq
  // as soon as the path appears (tracked via handledSeq) — this is how a brand-new
  // doc reveals + flashes the moment its re-list lands.
  const handledSeq = useRef(0);
  useEffect(() => {
    const { path, seq } = lastDocChanged;
    if (seq === 0 || !path || seq === handledSeq.current) return;
    // Filter to this project's tree: only react once the path is in our docs (an
    // unseen path appears here after the store's re-list lands; a path from another
    // project never does).
    if (!docs.some((d) => d.path === path)) return;
    handledSeq.current = seq;
    triggerFlash(path);
    const toOpen = expandTokensForPath(path);
    if (toOpen.length) {
      setExpanded((prev) => {
        const next = new Set(prev);
        for (const t of toOpen) next.add(t);
        return next;
      });
    }
    // eslint-disable-next-line
  }, [lastDocChanged.seq, docs]);

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
        ${docs.length === 0 && html`<p class="tree-msg">No docs found.</p>`}
        <ul>
          ${groups.map(
            (g) => html`
              <${Collapsible}
                key=${g.name}
                label=${g.name}
                kind="group"
                depth=${0}
                defaultOpen=${groupHasSelected(g)}
                forceOpen=${expanded.has("group:" + g.name)}
                flashId=${flashes["group:" + g.name]}
              >
                <${GroupBody}
                  group=${g}
                  selected=${selected}
                  onSelect=${onSelect}
                  subHasSelected=${subHasSelected}
                  limit=${g.name === "Tasks" ? TASKS_DEFAULT_LIMIT : 0}
                  flashes=${flashes}
                  expanded=${expanded}
                  project=${project}
                />
              <//>
            `
          )}
        </ul>
      </nav>
    </aside>
  `;
}
