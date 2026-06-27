// auto-ui/web/static/docevents.js
// Normalize a received doc.changed notification (the full event envelope delivered
// in JSON-RPC params) into the fields liveness matches on. The changed path is
// ALWAYS under ev.data.path; project/worktree are top-level envelope fields but are
// also mirrored inside data — read data first, fall back to the envelope. Reading
// ev.path (top-level) was the original bug: it is always undefined.
export function parseDocChanged(ev) {
  const d = (ev && ev.data) || {};
  return {
    project: d.project ?? ev?.project,
    path: d.path, // the fix: NOT ev.path
    worktree: d.worktree ?? ev?.worktree,
    host: d.host ?? ev?.host ?? ev?.Host,
    branch: d.branch ?? ev?.branch,
  };
}

// matchesDoc is true when a doc.changed targets the given open/active doc.
// Match on {project, path}; a missing worktree on EITHER side matches any
// (parity with the retired doc.js semantics).
export function matchesDoc(ev, target) {
  const c = parseDocChanged(ev);
  if (!target || !target.path || !c.path) return false;
  if (c.project !== target.project) return false;
  if (c.path !== target.path) return false;
  if (target.worktree && c.worktree && c.worktree !== target.worktree) return false;
  if (target.host && c.host && c.host !== target.host) return false;
  return true;
}
