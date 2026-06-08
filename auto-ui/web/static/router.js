// router.js — hash-based routing. Hash shape: #/<view>?<query>
// All view + view-state lives in location.hash so it survives a reload.

// parseHash reads location.hash into a {view, params} pair.
// view defaults to "home"; params is a URLSearchParams.
export function parseHash() {
  const raw = location.hash.replace(/^#/, "") || "/";
  const [path, qs] = raw.split("?");
  return {
    view: path.replace(/^\//, "") || "home",
    params: new URLSearchParams(qs || ""),
  };
}

// setHash writes location.hash = "/<view>?<qs>" (the "?" is omitted when empty).
export function setHash(view, params) {
  const qs = params ? params.toString() : "";
  location.hash = `/${view}${qs ? "?" + qs : ""}`;
}

// onRouteChange subscribes cb to the hashchange event.
export function onRouteChange(cb) {
  addEventListener("hashchange", cb);
}
