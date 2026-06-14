// uistate.js — a single module-level cross-route snapshot of the explorer's
// current state. This is NOT a reactive store: nothing subscribes to it for
// rendering. The explorer / tree / content components WRITE to it as they
// fetch and render (via setUIState); the /debug "current state" section READS
// it on mount/refresh. It exists only because on #/debug the App re-renders the
// whole tree, so Explorer/DocTree/DocContent are unmounted and their DOM
// (data-revision / data-doc-count) can't be read cross-route.

export const uiState = {
  project: "",
  path: "",
  type: "",
  revision: 0,
  docCount: 0,
  lastUpdated: "",
};

// setUIState shallow-merges patch into the shared snapshot. Callers pass only
// the fields they own (e.g. tree writes docCount, content writes revision).
export function setUIState(patch) {
  Object.assign(uiState, patch);
}
