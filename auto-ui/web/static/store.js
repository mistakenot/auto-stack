// store.js — the single module-singleton store for the auto-ui SPA.
//
// One normalised source of client state (the rpc.js idiom: module-scope state +
// a pub/sub, no Context, no provider). Components read slices via the useStore
// hook (built on the same useState+useEffect subscribe shim ConnIndicator uses)
// and dispatch thin actions; the store owns ALL side-effects (the connection
// state, the single doc.changed subscription, route→selection mirroring, and
// every server fetch with a docsByProject cache).
//
// PHASE 1 (this file's first landing): the store is DORMANT. State + reducer +
// pub/sub + useStore + selectors + thin actions + initStore() all exist, but
// initStore() is NOT called yet (app.js only imports the module so it loads).
// The existing explorer/tree/content/debug keep their own fetches/subscriptions
// for now, so the app renders IDENTICALLY. Phase 2 calls initStore() and cuts
// the views over. See docs/tasks/029-auto-ui-state-refactor/plan.html.
import { useState, useEffect } from "preact/hooks";
import { setHash, onRouteChange, parseHash } from "./router.js";
import { call, on, onAny, onStatus, whenOpen, connInfo } from "./rpc.js";
import { parseDocChanged, matchesDoc } from "./docevents.js";

// --- Normalised state (single source of truth) ----------------------------
//
// The shape mirrors the Solution tab's <pd-code>. selection mirrors the hash
// (no host dimension — D-3). openDoc mirrors content.js's per-doc view-model;
// docsByProject is the AC-8 cache; events is the recent doc.changed + ping ring
// that /debug's event log reads.
function initialState() {
  return {
    conn: { status: "connecting", reconnects: 0 }, // from rpc.onStatus
    projects: [], // project.list -> [{id,name,path,remote}]
    docsByProject: {}, // { [project]: [{id,path,type}] } — doc.list cache (AC-8)
    selection: { project: "", path: "", worktree: "" }, // mirrors the hash (no host — D-3)
    openDoc: {
      path: "",
      type: "",
      markdown: "",
      revision: 0,
      lastUpdated: "",
      nonce: 1,
      loading: false,
      error: null,
    },
    lastDocChanged: { path: "", seq: 0 }, // signal: drives tree flash/expand
    events: [], // [{t, method, params}] — recent doc.changed + ping, for /debug
    liveness: { byKey: {}, now: 0 },
  };
}

let state = initialState();
const listeners = new Set();

// --- Pub/sub primitives ----------------------------------------------------

// getState returns a synchronous snapshot, for non-render reads (e.g. /debug
// selectors).
export function getState() {
  return state;
}

// subscribe registers a listener and returns an unsubscribe fn. Backs useStore.
export function subscribe(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function notify() {
  for (const fn of listeners) fn(state);
}

// dispatch is the reducer entrypoint: apply the action, and notify subscribers
// ONLY when the reducer returns a new state identity (the reducer is pure and
// returns the same reference for a no-op, so an irrelevant action never fans out
// a re-render).
export function dispatch(action) {
  const next = reducer(state, action);
  if (next === state) return;
  state = next;
  notify();
}

// MAX_EVENTS bounds the events ring (parity with rpc.js's ring cap).
const MAX_EVENTS = 200;

// --- Pure reducer ----------------------------------------------------------
//
// Every case returns a NEW state object when something changes, else the SAME
// reference (so dispatch can skip notifying on a no-op). Actions are plain
// {type, ...payload} objects dispatched by the side-effect wiring in initStore()
// and (in Phase 2) by component-facing thin actions.
export function reducer(state, action) {
  switch (action.type) {
    case "conn/set": {
      const { status, reconnects } = action;
      if (
        state.conn.status === status &&
        state.conn.reconnects === reconnects
      ) {
        return state;
      }
      return { ...state, conn: { status, reconnects } };
    }

    case "projects/set": {
      return { ...state, projects: action.projects || [] };
    }

    case "selection/set": {
      const { project, path, worktree } = action;
      const s = state.selection;
      if (s.project === project && s.path === path && s.worktree === worktree) {
        return state;
      }
      return { ...state, selection: { project, path, worktree } };
    }

    case "docs/set": {
      const { project, worktree, docs } = action;
      return {
        ...state,
        docsByProject: {
          ...state.docsByProject,
          [docsKey(project, worktree)]: docs || [],
        },
      };
    }

    case "docs/invalidate": {
      // Drop a project+worktree's cached doc.list so the next list-orchestration
      // re-fetches (used by reconnect and by a doc.changed re-list — D-7, no TTL).
      const { project, worktree } = action;
      const key = docsKey(project, worktree);
      if (!(key in state.docsByProject)) return state;
      const nextCache = { ...state.docsByProject };
      delete nextCache[key];
      return { ...state, docsByProject: nextCache };
    }

    case "openDoc/set": {
      // Replace the open-doc view-model wholesale (used when the target changes).
      return { ...state, openDoc: { ...state.openDoc, ...action.openDoc } };
    }

    case "openDoc/patch": {
      // Shallow-merge fields onto the current open doc (loading flags, markdown,
      // revision bump, nonce, error).
      return { ...state, openDoc: { ...state.openDoc, ...action.patch } };
    }

    case "docChanged/signal": {
      // Bump the lastDocChanged signal so the tree's flash/expand effect fires
      // for the touched path (monotonic seq so identical-path touches re-trigger).
      return {
        ...state,
        lastDocChanged: { path: action.path, seq: state.lastDocChanged.seq + 1 },
      };
    }

    case "liveness/note": {
      const { project, branch, t } = action;
      const key = project + "\0" + branch;
      return {
        ...state,
        liveness: { ...state.liveness, byKey: { ...state.liveness.byKey, [key]: t } },
      };
    }

    case "liveness/tick": {
      // Return SAME state ref when nothing is tracked → no re-render on idle dashboard
      if (Object.keys(state.liveness.byKey).length === 0) return state;
      return {
        ...state,
        liveness: { ...state.liveness, now: action.now },
      };
    }

    case "events/append": {
      const next = state.events.concat([action.event]);
      if (next.length > MAX_EVENTS) next.splice(0, next.length - MAX_EVENTS);
      return { ...state, events: next };
    }

    default:
      return state;
  }
}

// --- useStore hook ---------------------------------------------------------
//
// Subscribe a component to selector(state); re-render only when the selected
// slice changes (shallow compare). Built on useState+useEffect — no Context, no
// new importmap entry (AC-6). Mirrors ConnIndicator's
// useState + useEffect(()=>onStatus(set),[]) subscribe shim.
export function useStore(selector) {
  const [selected, setSelected] = useState(() => selector(state));
  useEffect(() => {
    // Re-read on every notify; only commit when the slice actually changed so a
    // component re-renders only for its own slice.
    const check = (s) => {
      const next = selector(s);
      setSelected((prev) => (shallowEqual(prev, next) ? prev : next));
    };
    const off = subscribe(check);
    // Reconcile against any state that changed between the initial useState read
    // and the subscribe (mirrors onStatus firing immediately).
    check(state);
    return off;
    // selector is treated as stable (module-level function), like onStatus.
    // eslint-disable-next-line
  }, []);
  return selected;
}

// shallowEqual compares two selected slices one level deep. Primitives compare
// by value; objects/arrays compare their own enumerable entries by reference, so
// a slice re-renders only when an entry identity changes (the reducer returns
// fresh references exactly when a slice changes).
function shallowEqual(a, b) {
  if (Object.is(a, b)) return true;
  if (typeof a !== "object" || a === null) return false;
  if (typeof b !== "object" || b === null) return false;
  const ka = Object.keys(a);
  const kb = Object.keys(b);
  if (ka.length !== kb.length) return false;
  for (const k of ka) {
    if (!Object.is(a[k], b[k])) return false;
  }
  return true;
}

// --- Selectors -------------------------------------------------------------
//
// Slice selectors are the per-view layer over the reusable store substrate.
// Cheap, pure reads of state; the seam where heavier view-model transforms can
// later move without touching display code.

export function selectProjects(state) {
  return state.projects;
}

// selectActiveProject resolves the effective active project: the hash project,
// else the first registered project (mirrors explorer.js's activeProject).
export function selectActiveProject(state) {
  return (
    state.selection.project ||
    (state.projects.length > 0 ? state.projects[0].id : "")
  );
}

export function selectConn(state) {
  return state.conn;
}

// docsKey keys the docsByProject cache by project AND worktree, because doc.list
// is sent with the selected worktree (fetchDocs) — two worktrees of the same
// project list different docs, so a project-only key would serve the first
// worktree's docs after switching. When no worktree is selected (the common
// case) the key collapses to the bare project id, so single-worktree behaviour
// is unchanged. Project ids are lowercase-kebab (no "@"), so the delimiter never
// collides.
function docsKey(project, worktree) {
  return worktree ? project + "@" + worktree : project;
}

// selectDocs returns the cached doc.list for a project+worktree (empty if unseen).
export function selectDocs(state, project, worktree) {
  return state.docsByProject[docsKey(project, worktree)] || [];
}

export function selectOpenDoc(state) {
  return state.openDoc;
}

const ACTIVE_WINDOW_MS = 120_000;

// selectLiveness returns { ageMs, active } for a (project, branch) pair, or null.
// Null when branch is missing/shared (main/master). Debug override via ?liveWindowMs=N.
export function selectLiveness(state, project, branch) {
  if (!branch || branch === "main" || branch === "master") return null;
  const key = project + "\0" + branch;
  const lastSeen = state.liveness.byKey[key];
  if (lastSeen === undefined) return null;
  const ageMs = state.liveness.now - lastSeen;
  const windowMs = _liveWindowOverride || ACTIVE_WINDOW_MS;
  return { ageMs, active: ageMs <= windowMs };
}

let _liveWindowOverride = 0;
// Allow conformance tests to override the active window via ?liveWindowMs=N (debug only)
if (typeof window !== "undefined" && window.__autoui) {
  const p = new URLSearchParams(location.search);
  const ov = parseInt(p.get("liveWindowMs"), 10);
  if (ov > 0) _liveWindowOverride = ov;
}

// selectDebugSnapshot replaces the deleted cross-route snapshot mirror for the
// /debug current-state section: {project, path, type, revision, docCount, lastUpdated}.
export function selectDebugSnapshot(state) {
  const project = selectActiveProject(state);
  const docs = selectDocs(state, project, state.selection.worktree);
  const od = state.openDoc;
  return {
    project,
    path: od.path,
    type: od.type,
    revision: od.revision,
    docCount: docs.length,
    lastUpdated: od.lastUpdated,
  };
}

// --- Thin actions (single update flow — D-6) -------------------------------
//
// User actions only setHash; the onRouteChange handler in initStore() performs
// the state update + fetch orchestration, so there is one update path and the
// URL, selection, and fetches never diverge. refreshOpenDoc is the exception:
// it re-fetches the open doc directly (the content pane's refresh button).

// selectProject routes to a fresh explore view for a project (clears path),
// mirroring explorer.js's onPickProject.
export function selectProject(id) {
  setHash("explore", new URLSearchParams({ project: id }));
}

// selectDoc routes to a doc leaf, preserving the active project + worktree,
// mirroring explorer.js's onSelectDoc.
export function selectDoc(path) {
  const { selection } = state;
  const project = selectActiveProject(state);
  const next = { project, path };
  if (selection.worktree) next.worktree = selection.worktree;
  setHash("explore", new URLSearchParams(next));
}

// refreshOpenDoc forces a re-fetch (markdown) / nonce-bump (html) of the open
// doc and bumps revision — the content pane's refresh button + the open-doc
// branch of the single doc.changed subscription both call this.
export function refreshOpenDoc() {
  return fetchOpenDoc({ force: true });
}

// --- doc.list call counter (AC-8 observable) -------------------------------
//
// A store-level counter of doc.list round-trips. A warm docsByProject cache must
// avoid a redundant doc.list, observable here (NOT via /api/debug/recent, which
// only sees server ingest events; a client doc.list is a WS request invisible to
// it). Exposed both as a module export (test hook) and, when debug is on, under
// window.__autoui.store so an agent-browser eval can read it.
let docListCalls = 0;

// docListCount returns the number of doc.list round-trips the store has made.
export function docListCount() {
  return docListCalls;
}

function bumpDocListCount() {
  docListCalls += 1;
  if (typeof window !== "undefined" && window.__autoui) {
    if (!window.__autoui.store) window.__autoui.store = {};
    window.__autoui.store.docListCalls = docListCalls;
  }
}

// --- Side-effect orchestration (wired by initStore, dormant in Phase 1) ----
//
// resolveType derives the effective open-doc type from the path suffix (parity
// with content.js's resolveType / explorer.js's deriveType).
function resolveType(path) {
  return path && path.endsWith(".html") ? "html" : "markdown";
}

// fetchProjects loads project.list once, gated on whenOpen() so a cold load
// doesn't reject "not connected". Mirrors explorer.js's fetchProjects.
//
// Cold-load ordering: the initial syncFromHash() runs before project.list
// resolves, so when the hash carries NO project the active project is still ""
// (no projects loaded yet) and the dependent doc.list is skipped. The OLD
// explorer re-listed reactively once project.list landed (tree.js keyed its fetch
// on the resolved activeProject prop). To preserve that "switcher AND tree
// populate without a reload" behaviour (025 AC-1), kick the dependent fetches for
// the now-resolvable active project here, but ONLY when the hash has no explicit
// project — an explicit-project hash already listed via syncFromHash.
async function fetchProjects() {
  try {
    await whenOpen();
    const res = (await call("project.list")) || [];
    dispatch({ type: "projects/set", projects: res });
    if (!getState().selection.project) {
      const active = selectActiveProject(getState());
      if (active) {
        fetchDocs(active);
        fetchOpenDoc();
      }
    }
  } catch {
    // project.list errors are recorded at the rpc.js layer (call() rejects feed
    // recordError); the store leaves projects empty.
  }
}

// fetchDocs lists a project's docs into the docsByProject cache, gated on
// whenOpen(). When force is false and the project is already cached, it skips the
// round-trip (the AC-8 warm cache). Mirrors tree.js's fetchList.
async function fetchDocs(project, { force = false } = {}) {
  if (!project) return;
  const worktree = getState().selection.worktree;
  // warm cache (AC-8), keyed by project+worktree so a worktree switch re-lists.
  if (!force && docsKey(project, worktree) in getState().docsByProject) return;
  try {
    await whenOpen();
    const params = {};
    if (project) params.project = project;
    if (worktree) params.worktree = worktree;
    bumpDocListCount();
    const res = (await call("doc.list", params)) || [];
    dispatch({ type: "docs/set", project, worktree, docs: res });
  } catch {
    // doc.list errors are recorded at the rpc.js layer.
  }
}

// fetchOpenDoc loads/refreshes the open-doc view-model from the current
// selection. Markdown re-fetches via doc.get; html bumps the cache-bust nonce
// (no fetch). Both bump revision + lastUpdated (parity with content.js's
// bump()). force re-fetches even when the target is unchanged (refresh button /
// open-doc doc.changed).
async function fetchOpenDoc({ force = false } = {}) {
  const { selection } = getState();
  const { project, path, worktree } = selection;
  const type = resolveType(path);

  if (!path) {
    // No open doc: reset the view-model (but keep the nonce monotonic).
    dispatch({
      type: "openDoc/set",
      openDoc: {
        path: "",
        type: "",
        markdown: "",
        loading: false,
        error: null,
      },
    });
    return;
  }

  const bump = (patch) => {
    const now = new Date().toISOString();
    dispatch({
      type: "openDoc/patch",
      patch: {
        path,
        type,
        revision: getState().openDoc.revision + 1,
        lastUpdated: now,
        ...patch,
      },
    });
  };

  if (type === "html") {
    // html: no fetch. A forced refresh bumps the cache-bust nonce so the iframe
    // re-points; a plain target change leaves the nonce. Both bump revision +
    // lastUpdated (parity with content.js: refresh() vs the html target effect).
    const patch = { markdown: "", loading: false, error: null };
    if (force) patch.nonce = getState().openDoc.nonce + 1;
    bump(patch);
    return;
  }

  // markdown: gate on whenOpen() then doc.get + render fields.
  if (!project) return;
  dispatch({ type: "openDoc/patch", patch: { loading: true, error: null } });
  try {
    await whenOpen();
    const params = { project, path };
    if (worktree) params.worktree = worktree;
    const res = await call("doc.get", params);
    dispatch({
      type: "openDoc/patch",
      patch: { markdown: res && res.markdown ? res.markdown : "", loading: false },
    });
    bump({});
  } catch (e) {
    dispatch({
      type: "openDoc/patch",
      patch: {
        loading: false,
        error: "doc.get failed: " + (e && e.message ? e.message : String(e)),
      },
    });
  }
}

// syncFromHash mirrors the hash into selection and orchestrates the dependent
// fetches: doc.list for the active project (cache-gated) and the open doc. The
// single update flow (D-6) — onRouteChange and the initial load both call this.
//
// ONLY the explore route drives selection + fetches. #/debug is a read-only
// view of existing state, and it carries no project/path query — so re-deriving
// selection there would clear it (and reset openDoc.path), losing the live
// cross-route snapshot /debug must show (AC-4). The pre-refactor uistate.js was
// a write-only mirror untouched by navigation; gating here restores that: the
// last explore selection survives the #/debug route change, and the hash stays
// the navigational source of truth for the explore route (D-6).
function syncFromHash() {
  const { view, params } = parseHash();
  if (view !== "explore") return;
  const project = params.get("project") || "";
  const path = params.get("path") || "";
  const worktree = params.get("worktree") || "";
  dispatch({ type: "selection/set", project, path, worktree });

  const active = selectActiveProject(getState());
  if (active) fetchDocs(active);
  fetchOpenDoc();
}

// --- initStore: wire ALL side-effects --------------------------------------
//
// PHASE 1: defined but NOT called (the app must render identically — store
// dormant). PHASE 2 calls this once at startup before the first render.
let initialised = false;

export function initStore() {
  if (initialised) return;
  initialised = true;

  // conn state + reconnect self-heal. onStatus fires immediately with the
  // current status; track the previous status so the INITIAL open (covered by
  // the mount fetch via syncFromHash + fetchProjects) doesn't double-fetch — a
  // re-fetch only happens on a FRESH transition to "open".
  let prevStatus = null;
  onStatus((s) => {
    const was = prevStatus;
    prevStatus = s;
    dispatch({ type: "conn/set", status: s, reconnects: reconnectsFromRpc() });
    if (s === "open" && was !== null && was !== "open") {
      // Reconnect: invalidate the active project's cache so the re-list is fresh,
      // then re-fetch projects, docs, and the open doc (cache does not mask the
      // reconnect re-list — D-7 / AC-8 second clause).
      const active = selectActiveProject(getState());
      if (active) {
        dispatch({
          type: "docs/invalidate",
          project: active,
          worktree: getState().selection.worktree,
        });
      }
      fetchProjects();
      if (active) fetchDocs(active, { force: true });
      fetchOpenDoc({ force: true });
    }
  });

  // route → selection + fetch orchestration (single update flow — D-6).
  onRouteChange(syncFromHash);

  // ONE doc.changed subscription (parsed via docevents.js — ev.data.path).
  // Drives: re-list on an unseen path, refresh the open doc on a match, the
  // lastDocChanged flash/expand signal, and the /debug event log append.
  on("doc.changed", (ev) => {
    const c = parseDocChanged(ev);
    dispatch({
      type: "events/append",
      event: { t: Date.now(), method: "doc.changed", params: ev },
    });
    if (!c.path) return;

    // Flash/expand signal for the tree (the changed path, any project — the tree
    // filters to the active project when it reads the signal).
    dispatch({ type: "docChanged/signal", path: c.path });

    const active = selectActiveProject(getState());
    if (c.project === active) {
      const wt = getState().selection.worktree;
      const known = (getState().docsByProject[docsKey(active, wt)] || []).some(
        (d) => d.path === c.path
      );
      if (!known) {
        // New doc under the active project: force a re-list so the new node
        // appears (cache invalidation by doc.changed — D-7).
        fetchDocs(active, { force: true });
      } else if (c.path.endsWith(".html")) {
        // Known .html plan changed: re-list to pick up pd-meta changes (e.g. planning→executing).
        fetchDocs(active, { force: true });
      }
    }

    // Refresh the open doc if this event targets it (open-doc liveness).
    if (matchesDoc(ev, getState().selection)) {
      refreshOpenDoc();
    }
  });

  // Liveness + debug event log: subscribe to all bus notifications. Record
  // (project, branch) receipt time for liveness, and append non-doc.changed
  // events to the debug event ring (doc.changed is already appended by its
  // own on() handler above — appending here too would double it).
  onAny(({ method, params }) => {
    if (method !== "doc.changed") {
      dispatch({
        type: "events/append",
        event: { t: Date.now(), method, params },
      });
    }
    if (params && params.branch && params.project) {
      const b = params.branch;
      if (b !== "main" && b !== "master") {
        dispatch({ type: "liveness/note", project: params.project, branch: b, t: Date.now() });
      }
    }
  });

  // 1s tick drives the "Ns ago" / "Nm ago" display.
  setInterval(() => dispatch({ type: "liveness/tick", now: Date.now() }), 1000);

  // Initial load: mirror the hash and kick off project.list + dependent fetches.
  fetchProjects();
  syncFromHash();
}

// reconnectsFromRpc reads the current reconnect count from rpc.js's connInfo
// (onStatus only carries the status string, not the count) so the conn slice's
// reconnects stays accurate.
function reconnectsFromRpc() {
  return connInfo().reconnects;
}
